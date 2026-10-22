package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

const (
	leaseKey      = "forge.io/bootstrap-lease"
	leaseUntilKey = "forge.io/bootstrap-lease-until"
)

type SecretState struct {
	Metadata metav1.ObjectMeta
	Data     map[string]string
}

// SecretStore only addresses the single pre-created output Secret. It requires
// get/patch, never create/list/update/delete permission.
type SecretStore struct {
	secrets typedcorev1.SecretInterface
	name    string
	holder  string
}

func InClusterStore(namespace, name string) (*SecretStore, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("cannot load Kubernetes in-cluster configuration")
	}
	config.Host = "https://kubernetes.default.svc"
	client, err := typedcorev1.NewForConfig(config)
	if err != nil {
		return nil, errors.New("cannot initialize Kubernetes Secret client")
	}
	return &SecretStore{secrets: client.Secrets(namespace), name: name}, nil
}

func secretState(secret *corev1.Secret) *SecretState {
	state := &SecretState{Metadata: secret.ObjectMeta, Data: map[string]string{}}
	for key, value := range secret.Data {
		state.Data[key] = string(value)
	}
	return state
}

func (s *SecretStore) get(ctx context.Context) (*SecretState, error) {
	secret, err := s.secrets.Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.New("cannot read output Secret")
	}
	return secretState(secret), nil
}

func (s *SecretStore) patch(ctx context.Context, body any) (*SecretState, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, errors.New("cannot encode output Secret patch")
	}
	secret, err := s.secrets.Patch(ctx, s.name, types.MergePatchType, data, metav1.PatchOptions{})
	if err != nil {
		// API errors can contain request data; do not disclose credentials.
		return nil, errors.New("cannot patch output Secret")
	}
	return secretState(secret), nil
}

func (s *SecretStore) Acquire(ctx context.Context, duration time.Duration) (*SecretState, error) {
	state, err := s.get(ctx)
	if err != nil {
		return nil, err
	}
	if state.Metadata.Annotations[leaseKey] != "" {
		until, err := time.Parse(time.RFC3339, state.Metadata.Annotations[leaseUntilKey])
		if err != nil || time.Now().Before(until) {
			return nil, errors.New("external bootstrap is already running; retry after its bounded lease")
		}
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	s.holder = hex.EncodeToString(random)
	body := map[string]any{
		"metadata": map[string]any{
			"resourceVersion": state.Metadata.ResourceVersion,
			"annotations": map[string]string{
				leaseKey:      s.holder,
				leaseUntilKey: time.Now().Add(duration).UTC().Format(time.RFC3339),
			},
		},
	}
	result, err := s.patch(ctx, body)
	if err != nil {
		// A response can be lost after the compare-and-swap succeeded.
		result, err = s.get(ctx)
		if err != nil || result.Metadata.Annotations[leaseKey] != s.holder {
			return nil, errors.New("cannot acquire external bootstrap lease")
		}
	}
	return result, nil
}

func (s *SecretStore) Save(ctx context.Context, state *SecretState) error {
	result, err := s.patch(ctx, map[string]any{
		"metadata":   map[string]any{"resourceVersion": state.Metadata.ResourceVersion},
		"stringData": state.Data,
	})
	if err != nil {
		return err
	}
	*state = *result
	return nil
}

func (s *SecretStore) Release(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	state, err := s.get(ctx)
	if err != nil || state.Metadata.Annotations[leaseKey] != s.holder {
		return
	}
	_, _ = s.patch(ctx, map[string]any{
		"metadata": map[string]any{
			"resourceVersion": state.Metadata.ResourceVersion,
			"annotations":     map[string]any{leaseKey: nil, leaseUntilKey: nil},
		},
	})
}
