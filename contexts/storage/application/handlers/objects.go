package handlers

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/aggregates/access"
	"github.com/piggystats/nodera/contexts/storage/aggregates/object"
	"github.com/piggystats/nodera/contexts/storage/aggregates/storage"
	"github.com/piggystats/nodera/contexts/storage/application/dto"
	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/pkg/authz"
	"github.com/piggystats/nodera/utils/str"
)

// Objects is the file browser: it lists and deletes through the engine with
// the account's root key and hands out short-lived URLs for the browser to
// fetch and put files itself, never through Nodera.
type Objects struct {
	authorizer
	engine    Engine
	accounts  *accounts
	addresses Addresses
	now       Clock
}

func NewObjects(accessService access.IService, storageService storage.IService, eng Engine, accounts *accounts, addresses Addresses, now Clock) *Objects {
	return &Objects{authorizer: authorizer{access: accessService, storages: storageService}, engine: eng, accounts: accounts, addresses: addresses, now: now}
}

func (h *Objects) List(ctx context.Context, req *object.ListRequest) (*dto.Listing, error) {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageRead)
	if err != nil {
		return nil, err
	}
	if err := usable(s); err != nil {
		return nil, err
	}
	prefix := strings.TrimPrefix(req.Prefix, "/")
	if str.IsNotEmpty(prefix) && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return nil, err
	}
	listing, err := h.engine.ListObjects(ctx, account, s.BucketName, prefix, req.Cursor, object.PageSize)
	if err != nil {
		return nil, engineError(err)
	}

	return dto.ToListing(prefix, listing), nil
}

func (h *Objects) Delete(ctx context.Context, req *object.DeleteRequest) error {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageWrite)
	if err != nil {
		return err
	}
	if err := usable(s); err != nil {
		return err
	}
	for _, key := range req.Keys {
		if !validKey(key, true) {
			return object.ErrorInvalidKey
		}
	}
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if err := h.engine.DeleteObjects(ctx, account, s.BucketName, req.Keys); err != nil {
		return engineError(err)
	}

	return nil
}

func (h *Objects) DownloadURL(ctx context.Context, req *object.URLRequest) (*dto.PresignedURL, error) {
	return h.presign(ctx, req, authz.StorageRead, "GET", object.DownloadTTL, "", h.engine.PresignDownload)
}

// UploadURL signs the file's type with the URL and tells the browser to send
// it exactly so.
func (h *Objects) UploadURL(ctx context.Context, req *object.URLRequest) (*dto.PresignedURL, error) {
	contentType := strings.TrimSpace(req.ContentType)

	return h.presign(ctx, req, authz.StorageWrite, "PUT", object.UploadTTL, contentType, func(ctx context.Context, account *engine.Account, bucket, key string, ttl time.Duration) (string, error) {
		return h.engine.PresignUpload(ctx, account, bucket, key, contentType, ttl)
	})
}

type presigner func(ctx context.Context, account *engine.Account, bucket, key string, ttl time.Duration) (string, error)

func (h *Objects) presign(ctx context.Context, req *object.URLRequest, permission authz.Permission, method string, ttl time.Duration, contentType string, sign presigner) (*dto.PresignedURL, error) {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), permission)
	if err != nil {
		return nil, err
	}
	if err := usable(s); err != nil {
		return nil, err
	}
	if !validKey(req.Key, false) {
		return nil, object.ErrorInvalidKey
	}
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return nil, err
	}
	signed, err := sign(ctx, account, s.BucketName, req.Key, ttl)
	if err != nil {
		return nil, engineError(err)
	}

	return &dto.PresignedURL{URL: signed, Method: method, ContentType: contentType, ExpiresAt: h.now().Add(ttl)}, nil
}

// Access says whether anyone may read the object.
func (h *Objects) Access(ctx context.Context, req *object.AccessRequest) (*dto.ObjectAccess, error) {
	s, account, err := h.reach(ctx, req.ProjectID, req.StorageID, authz.StorageRead)
	if err != nil {
		return nil, err
	}
	if !validKey(req.Key, false) {
		return nil, object.ErrorInvalidKey
	}
	public, err := h.engine.ObjectAccess(ctx, account, s.BucketName, req.Key)
	if err != nil {
		return nil, objectError(err)
	}

	return &dto.ObjectAccess{Key: req.Key, URL: h.addresses.BucketURL(s.Region, s.BucketName) + "/" + escapeKey(req.Key), Public: public}, nil
}

// SetAccess lets anyone read the object, or takes that back; uploads are
// always private and writing stays with the Storage's keys.
func (h *Objects) SetAccess(ctx context.Context, req *object.SetAccessRequest) (*dto.ObjectAccess, error) {
	s, account, err := h.reach(ctx, req.ProjectID, req.StorageID, authz.StorageWrite)
	if err != nil {
		return nil, err
	}
	if !validKey(req.Key, false) {
		return nil, object.ErrorInvalidKey
	}
	if err := h.engine.SetObjectAccess(ctx, account, s.BucketName, req.Key, req.Public); err != nil {
		return nil, objectError(err)
	}

	return &dto.ObjectAccess{Key: req.Key, URL: h.addresses.BucketURL(s.Region, s.BucketName) + "/" + escapeKey(req.Key), Public: req.Public}, nil
}

// reach is a usable Storage the caller may act on, with its Project's account.
func (h *Objects) reach(ctx context.Context, projectID, storageID string, permission authz.Permission) (*storage.Storage, *engine.Account, error) {
	s, err := h.storage(ctx, uuid.MustParse(projectID), uuid.MustParse(storageID), permission)
	if err != nil {
		return nil, nil, err
	}
	if err := usable(s); err != nil {
		return nil, nil, err
	}
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return nil, nil, err
	}

	return s, account, nil
}

// objectError is an engine failure about one object: a key the bucket does
// not hold is the customer's 404, anything else the engine's.
func objectError(err error) error {
	if errors.Is(err, engine.ErrNoSuchObject) {
		return object.ErrorNotFound
	}

	return engineError(err)
}

// escapeKey writes a key into a URL the way a browser needs it, keeping the
// folders readable.
func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}

	return strings.Join(parts, "/")
}

// AllowBrowser puts Nodera's CORS rule back when the Team's own rules
// replaced it and the browser's uploads stopped.
func (h *Objects) AllowBrowser(ctx context.Context, req *object.BrowserAccessRequest) error {
	s, err := h.storage(ctx, uuid.MustParse(req.ProjectID), uuid.MustParse(req.StorageID), authz.StorageWrite)
	if err != nil {
		return err
	}
	if err := usable(s); err != nil {
		return err
	}
	account, err := h.accounts.find(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if err := h.engine.AllowBrowser(ctx, account, s.BucketName); err != nil {
		return engineError(err)
	}

	return nil
}

// validKey is an object key as S3 takes it: anything but empty or control
// characters. A key ending in "/" is the empty object S3 tools leave as a
// folder marker: it can be deleted, but a file is never put under that name.
func validKey(key string, folderMarker bool) bool {
	if str.IsBlank(key) || (!folderMarker && strings.HasSuffix(key, "/")) {
		return false
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}

	return true
}
