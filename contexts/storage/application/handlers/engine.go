package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/engine"
)

// Engine is the object store behind the Storages (docs/adr/0012). Every call
// but CreateAccessKey may be repeated: the reconcile job asks again until the
// engine holds what the database says, so asking for what already holds is
// not an error. A failure to reach the engine wraps engine.ErrUnavailable.
//
//go:generate mockgen -destination=../../mock/handlers/engine.go -package=mockhandlers github.com/piggystats/nodera/contexts/storage/application/handlers Engine
type Engine interface {
	// CreateAccount answers the Project's account and its root key, the same
	// ones when called again for the Project.
	CreateAccount(ctx context.Context, projectID uuid.UUID) (*engine.Account, error)

	// OpenBucket makes the Storage's bucket in the account, limited to
	// sizeBytes, rate limited and open to the web app's uploads.
	OpenBucket(ctx context.Context, account *engine.Account, bucket string, sizeBytes int64) error
	SetSizeLimit(ctx context.Context, account *engine.Account, bucket string, sizeBytes int64) error

	// Stop makes the Storage refuse every read and write, by any Access key
	// and by anyone reading what it published; Resume gives it back exactly
	// as it was, its own policies included.
	Stop(ctx context.Context, account *engine.Account, bucket string) error
	Resume(ctx context.Context, account *engine.Account, bucket string) error

	// DeleteBucket removes the bucket with every object and version in it and
	// every Access key made for it.
	DeleteBucket(ctx context.Context, account *engine.Account, bucket string) error

	// CreateAccessKey makes a key that reaches only this bucket; keyID is the
	// storage service's own id for it.
	CreateAccessKey(ctx context.Context, account *engine.Account, bucket string, keyID uuid.UUID, permission engine.Permission) (*engine.Credentials, error)
	DeleteAccessKey(ctx context.Context, account *engine.Account, bucket string, keyID uuid.UUID) error

	Stats(ctx context.Context, account *engine.Account, bucket string) (*engine.Stats, error)
	// Traffic answers the hours in [from, to) that saw any request.
	Traffic(ctx context.Context, account *engine.Account, bucket string, from, to time.Time) ([]engine.Traffic, error)

	// ListObjects pages a folder with "/" as the delimiter.
	ListObjects(ctx context.Context, account *engine.Account, bucket, prefix, cursor string, limit int) (*engine.Listing, error)
	DeleteObjects(ctx context.Context, account *engine.Account, bucket string, keys []string) error
	PresignDownload(ctx context.Context, account *engine.Account, bucket, key string, ttl time.Duration) (string, error)
	// PresignUpload signs the file's type with the URL when one is given: the
	// browser sends the type with the file, and the engine refuses a presigned
	// request carrying a header it did not sign. An upload is always private;
	// publishing is SetObjectAccess, and it opens reading alone.
	PresignUpload(ctx context.Context, account *engine.Account, bucket, key, contentType string, ttl time.Duration) (string, error)

	// ObjectAccess answers whether anyone may read the object without a key;
	// SetObjectAccess publishes it for reading or takes it back. Writing always
	// needs an Access key.
	ObjectAccess(ctx context.Context, account *engine.Account, bucket, key string) (bool, error)
	SetObjectAccess(ctx context.Context, account *engine.Account, bucket, key string, public bool) error

	// AllowBrowser puts the web app's CORS rule back beside the bucket's own.
	AllowBrowser(ctx context.Context, account *engine.Account, bucket string) error
}
