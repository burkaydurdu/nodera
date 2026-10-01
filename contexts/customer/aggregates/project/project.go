package project

import (
	"time"

	"github.com/google/uuid"
)

type Project struct {
	ID          uuid.UUID
	TeamID      uuid.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type Projects []*Project
