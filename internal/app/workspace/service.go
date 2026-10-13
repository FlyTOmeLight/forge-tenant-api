package workspace

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/account"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/app/authorization"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
	"github.com/FlyTOmeLight/forge-tenant-api/internal/config"
)

type Service struct {
	DB            *gorm.DB
	Config        config.Config
	Casdoor       *client.Casdoor
	Accounts      *account.Service
	Authorization *authorization.Service
	cipher        cipher.AEAD
}

func New(
	db *gorm.DB,
	c config.Config,
	casdoor *client.Casdoor,
	accounts *account.Service,
	authz *authorization.Service,
) (*Service, error) {
	key, err := hex.DecodeString(c.AppKey)
	if err != nil || len(key) != 32 {
		return nil, errors.New("APP_KEY must be a 32-byte hex key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Service{
		DB:            db,
		Config:        c,
		Casdoor:       casdoor,
		Accounts:      accounts,
		Authorization: authz,
		cipher:        aead,
	}, nil
}

func (a *Service) seal(b []byte) []byte {
	nonce := make([]byte, a.cipher.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		panic(e)
	}
	return a.cipher.Seal(nonce, nonce, b, nil)
}

func (a *Service) open(b []byte) ([]byte, error) {
	n := a.cipher.NonceSize()
	if len(b) < n {
		return nil, errors.New("invalid encrypted result")
	}
	return a.cipher.Open(nil, b[:n], b[n:], nil)
}

type Object = map[string]any
