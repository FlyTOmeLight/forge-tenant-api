package synchronization

import (
	"gorm.io/gorm"

	"github.com/FlyTOmeLight/forge-tenant-api/internal/client"
)

type Service struct {
	DB     *gorm.DB
	GitLab *client.GitLab
}

func New(db *gorm.DB, gitlab *client.GitLab) *Service { return &Service{DB: db, GitLab: gitlab} }
