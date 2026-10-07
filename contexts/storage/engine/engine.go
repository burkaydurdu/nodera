// Package engine is what the storage context and the object store that keeps
// its Storages exchange (docs/adr/0012). The storage service is the source of
// truth and the engine enforces it, so the types speak Nodera's language:
// Ceph is one adapter behind them and the fake another.
package engine

import (
	"errors"
	"time"
)

// ErrUnavailable is an engine that could not be asked, as opposed to one
// that answered no. Adapters wrap it so the handlers can tell the two apart.
var ErrUnavailable = errors.New("storage engine unavailable")

// ErrNoSuchObject is an object key the bucket does not hold.
var ErrNoSuchObject = errors.New("no such object")

// Account is a Project in the engine and the root key the storage service
// acts with on the Project's behalf. The secret is kept sealed by the caller.
type Account struct {
	ID        string
	AccessKey string
	SecretKey string
}

// Permission is what an Access key may do in the one Storage it reaches.
type Permission string

const (
	Read      Permission = "read"
	ReadWrite Permission = "read_write"
)

func (p Permission) IsValid() bool {
	return p == Read || p == ReadWrite
}

// Credentials are an Access key as S3 tools take it. The secret key leaves
// the engine once, here.
type Credentials struct {
	AccessKeyID string
	SecretKey   string
}

// Stats is what a Storage holds right now, as the engine counts it.
type Stats struct {
	Bytes   int64
	Objects int64
}

// Traffic is one UTC hour of a Storage's requests.
type Traffic struct {
	Hour          time.Time
	BytesSent     int64
	BytesReceived int64
	Requests      int64
}

// Object is one file of a listing.
type Object struct {
	Key        string
	Size       int64
	ModifiedAt time.Time
}

// Listing is one page of a folder: the folders right under the prefix, the
// files in it, and where the next page starts (empty on the last).
type Listing struct {
	Folders []string
	Objects []Object
	Next    string
}
