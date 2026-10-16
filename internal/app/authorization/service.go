package authorization

import (
	"gorm.io/gorm"
)

type Service struct{ DB *gorm.DB }

func New(db *gorm.DB) *Service { return &Service{DB: db} }
