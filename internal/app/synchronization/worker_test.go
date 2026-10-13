package synchronization

import (
	"testing"
	"time"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

func TestRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		attempts int
		status   string
	}{{name: "success", err: nil, attempts: 1, status: "succeeded"}, {name: "stale", err: errSuperseded, attempts: 1, status: "superseded"}, {name: "credential", err: &client.Error{Status: 401}, attempts: 1, status: "failed"}, {name: "effective conflict", err: &client.Error{Status: 409}, attempts: 1, status: "conflict"}, {name: "limited", err: &client.Error{Status: 429, RetryAfter: time.Minute}, attempts: 1, status: "retrying"}, {name: "exhausted", err: &client.Error{Status: 503}, attempts: 8, status: "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, _, delay := syncResult(tc.err, tc.attempts)
			if status != tc.status {
				t.Fatalf("status %s", status)
			}
			if tc.name == "limited" && delay != time.Minute {
				t.Fatal("Retry-After not respected")
			}
		})
	}
}
