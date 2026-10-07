// Package accesskey is the Access key: what software presents to reach one
// Storage over S3. Nodera keeps who made it and what it may do; the secret key
// leaves the engine once and is never kept.
package accesskey

import (
	"time"

	"github.com/google/uuid"
)

// MaxPerStorage bounds how many Access keys a Storage holds at once.
const MaxPerStorage = 20

type Permission string

const (
	Read      Permission = "read"
	ReadWrite Permission = "read_write"
)

type AccessKey struct {
	ID          uuid.UUID
	StorageID   uuid.UUID
	Name        string
	AccessKeyID string
	Permission  Permission
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
}
