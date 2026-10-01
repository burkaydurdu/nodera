package sshkey

import (
	"time"

	"github.com/google/uuid"
)

type SSHKey struct {
	ID          uuid.UUID
	TeamID      uuid.UUID
	Name        string
	PublicKey   string
	Fingerprint string
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
}

type SSHKeys []*SSHKey
