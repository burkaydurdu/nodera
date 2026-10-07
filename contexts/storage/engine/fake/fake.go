// Package fake is the engine the integration suite runs against: it keeps
// accounts, buckets, keys and files in memory, answers what a test told it
// a Storage holds, and can be made to fail as unreachable storage servers
// would.
package fake

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/piggystats/nodera/contexts/storage/engine"
	"github.com/piggystats/nodera/utils/str"
)

// Bucket is what the fake holds for one Storage, as a test reads it back.
type Bucket struct {
	Account      string
	SizeLimit    int64
	Stopped      bool
	BrowserRules int
	Keys         map[uuid.UUID]engine.Permission
	Objects      map[string]engine.Object
	// Public are the objects anyone may read.
	Public map[string]bool
	// Filled is what a test said the bucket holds, in place of its Objects.
	Filled  *engine.Stats
	Traffic []engine.Traffic
}

type Fake struct {
	mu       sync.Mutex
	accounts map[uuid.UUID]*engine.Account
	buckets  map[string]*Bucket
	failure  error
	refusal  error
	calls    []string
}

func New() *Fake {
	return &Fake{accounts: map[uuid.UUID]*engine.Account{}, buckets: map[string]*Bucket{}}
}

func (f *Fake) CreateAccount(_ context.Context, projectID uuid.UUID) (*engine.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateAccount"); err != nil {
		return nil, err
	}

	if a, ok := f.accounts[projectID]; ok {
		return a, nil
	}
	a := &engine.Account{
		ID:        "RGW" + strings.ToUpper(strings.ReplaceAll(projectID.String(), "-", ""))[:17],
		AccessKey: "AK" + strings.ToUpper(uuid.NewString()[:18]),
		SecretKey: "secret-" + uuid.NewString(),
	}
	f.accounts[projectID] = a

	return a, nil
}

func (f *Fake) OpenBucket(_ context.Context, account *engine.Account, bucket string, sizeBytes int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("OpenBucket " + bucket); err != nil {
		return err
	}

	b, ok := f.buckets[bucket]
	if !ok {
		b = &Bucket{Account: account.ID, Keys: map[uuid.UUID]engine.Permission{}, Objects: map[string]engine.Object{}, Public: map[string]bool{}}
		f.buckets[bucket] = b
	}
	b.SizeLimit = sizeBytes
	b.BrowserRules = 1

	return nil
}

func (f *Fake) SetSizeLimit(_ context.Context, _ *engine.Account, bucket string, sizeBytes int64) error {
	return f.change("SetSizeLimit "+bucket, bucket, func(b *Bucket) { b.SizeLimit = sizeBytes })
}

func (f *Fake) Stop(_ context.Context, _ *engine.Account, bucket string) error {
	return f.change("Stop "+bucket, bucket, func(b *Bucket) { b.Stopped = true })
}

func (f *Fake) Resume(_ context.Context, _ *engine.Account, bucket string) error {
	return f.change("Resume "+bucket, bucket, func(b *Bucket) { b.Stopped = false })
}

func (f *Fake) DeleteBucket(_ context.Context, _ *engine.Account, bucket string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("DeleteBucket " + bucket); err != nil {
		return err
	}
	delete(f.buckets, bucket)

	return nil
}

func (f *Fake) CreateAccessKey(_ context.Context, _ *engine.Account, bucket string, keyID uuid.UUID, permission engine.Permission) (*engine.Credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("CreateAccessKey " + bucket); err != nil {
		return nil, err
	}
	b, ok := f.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("fake engine: no bucket %q", bucket)
	}
	b.Keys[keyID] = permission

	return &engine.Credentials{AccessKeyID: "AKIA" + strings.ToUpper(strings.ReplaceAll(keyID.String(), "-", ""))[:16], SecretKey: "secret-" + keyID.String()}, nil
}

func (f *Fake) DeleteAccessKey(_ context.Context, _ *engine.Account, bucket string, keyID uuid.UUID) error {
	return f.change("DeleteAccessKey "+bucket, bucket, func(b *Bucket) { delete(b.Keys, keyID) })
}

func (f *Fake) Stats(_ context.Context, _ *engine.Account, bucket string) (*engine.Stats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Stats " + bucket); err != nil {
		return nil, err
	}
	b, ok := f.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("fake engine: no bucket %q", bucket)
	}
	if b.Filled != nil {
		return &engine.Stats{Bytes: b.Filled.Bytes, Objects: b.Filled.Objects}, nil
	}
	var stats engine.Stats
	for _, o := range b.Objects {
		stats.Bytes += o.Size
		stats.Objects++
	}

	return &stats, nil
}

func (f *Fake) Traffic(_ context.Context, _ *engine.Account, bucket string, from, to time.Time) ([]engine.Traffic, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("Traffic " + bucket); err != nil {
		return nil, err
	}
	b, ok := f.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("fake engine: no bucket %q", bucket)
	}
	var out []engine.Traffic
	for _, t := range b.Traffic {
		if !t.Hour.Before(from) && t.Hour.Before(to) {
			out = append(out, t)
		}
	}

	return out, nil
}

// ListObjects pages the way S3 does: folders are the distinct first segments
// under the prefix, files the keys with no further "/", both in key order.
func (f *Fake) ListObjects(_ context.Context, _ *engine.Account, bucket, prefix, cursor string, limit int) (*engine.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ListObjects " + bucket); err != nil {
		return nil, err
	}
	b, ok := f.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("fake engine: no bucket %q", bucket)
	}

	type entry struct {
		name   string
		folder bool
		object engine.Object
	}
	seen := map[string]bool{}
	var entries []entry
	for key, o := range b.Objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rest := strings.TrimPrefix(key, prefix)
		if i := strings.Index(rest, "/"); i >= 0 {
			folder := prefix + rest[:i+1]
			if !seen[folder] {
				seen[folder] = true
				entries = append(entries, entry{name: folder, folder: true})
			}

			continue
		}
		entries = append(entries, entry{name: key, object: o})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	start := 0
	if str.IsNotEmpty(cursor) {
		n, err := strconv.Atoi(cursor)
		if err != nil {
			return nil, fmt.Errorf("fake engine: bad cursor %q", cursor)
		}
		start = n
	}
	listing := &engine.Listing{Folders: []string{}, Objects: []engine.Object{}}
	end := min(start+limit, len(entries))
	for _, e := range entries[start:end] {
		if e.folder {
			listing.Folders = append(listing.Folders, e.name)
		} else {
			listing.Objects = append(listing.Objects, e.object)
		}
	}
	if end < len(entries) {
		listing.Next = strconv.Itoa(end)
	}

	return listing, nil
}

func (f *Fake) DeleteObjects(_ context.Context, _ *engine.Account, bucket string, keys []string) error {
	return f.change("DeleteObjects "+bucket, bucket, func(b *Bucket) {
		for _, k := range keys {
			delete(b.Objects, k)
			delete(b.Public, k)
		}
	})
}

func (f *Fake) PresignDownload(_ context.Context, _ *engine.Account, bucket, key string, ttl time.Duration) (string, error) {
	return f.presign("PresignDownload "+bucket, bucket, key, "get", ttl)
}

func (f *Fake) PresignUpload(_ context.Context, _ *engine.Account, bucket, key, contentType string, ttl time.Duration) (string, error) {
	signed, err := f.presign("PresignUpload "+bucket, bucket, key, "put", ttl)
	if err != nil || str.IsEmpty(contentType) {
		return signed, err
	}

	return signed + "&content-type=" + contentType, nil
}

func (f *Fake) ObjectAccess(_ context.Context, _ *engine.Account, bucket, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("ObjectAccess " + bucket); err != nil {
		return false, err
	}
	b, ok := f.buckets[bucket]
	if !ok {
		return false, fmt.Errorf("fake engine: no bucket %q", bucket)
	}
	if _, ok := b.Objects[key]; !ok {
		return false, engine.ErrNoSuchObject
	}

	return b.Public[key], nil
}

func (f *Fake) SetObjectAccess(_ context.Context, _ *engine.Account, bucket, key string, public bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call("SetObjectAccess " + bucket); err != nil {
		return err
	}
	b, ok := f.buckets[bucket]
	if !ok {
		return fmt.Errorf("fake engine: no bucket %q", bucket)
	}
	if _, ok := b.Objects[key]; !ok {
		return engine.ErrNoSuchObject
	}
	b.Public[key] = public

	return nil
}

func (f *Fake) AllowBrowser(_ context.Context, _ *engine.Account, bucket string) error {
	return f.change("AllowBrowser "+bucket, bucket, func(b *Bucket) { b.BrowserRules++ })
}

// --- what a test does to the fake -------------------------------------------

// FailWith makes every call fail until it is called with nil, as storage
// servers that cannot be reached would.
func (f *Fake) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failure = err
}

// RefuseWith makes every call fail as storage servers that answer no would:
// not unavailable, so repeating will not help.
func (f *Fake) RefuseWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusal = err
}

// Fill tells the fake what a bucket holds, as the real engine would count it.
func (f *Fake) Fill(bucket string, bytes, objects int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.buckets[bucket]; ok {
		b.Filled = &engine.Stats{Bytes: bytes, Objects: objects}
	}
}

// Put is a file software uploaded straight to the Storage.
func (f *Fake) Put(bucket, key string, size int64, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.buckets[bucket]; ok {
		b.Objects[key] = engine.Object{Key: key, Size: size, ModifiedAt: at}
	}
}

// AddTraffic is an hour as the usage log reports it for the bucket: the log
// keeps one running total per hour, so an hour given again is replaced.
func (f *Fake) AddTraffic(bucket string, t engine.Traffic) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.buckets[bucket]
	if !ok {
		return
	}
	for i, existing := range b.Traffic {
		if existing.Hour.Equal(t.Hour) {
			b.Traffic[i] = t

			return
		}
	}
	b.Traffic = append(b.Traffic, t)
}

// ReplaceBrowserRules is the Team overwriting the bucket's CORS rules with
// its own, which drops Nodera's.
func (f *Fake) ReplaceBrowserRules(bucket string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b, ok := f.buckets[bucket]; ok {
		b.BrowserRules = 0
	}
}

// Bucket is a copy of what the fake holds for the name, and false when it
// holds nothing.
func (f *Fake) Bucket(name string) (Bucket, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.buckets[name]
	if !ok {
		return Bucket{}, false
	}
	out := *b
	out.Keys = make(map[uuid.UUID]engine.Permission, len(b.Keys))
	for k, v := range b.Keys {
		out.Keys[k] = v
	}
	out.Objects = make(map[string]engine.Object, len(b.Objects))
	for k, v := range b.Objects {
		out.Objects[k] = v
	}
	out.Public = make(map[string]bool, len(b.Public))
	for k, v := range b.Public {
		out.Public[k] = v
	}

	return out, true
}

// Calls is every call made, in order, for a test that asserts what the engine
// was asked.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.accounts = map[uuid.UUID]*engine.Account{}
	f.buckets = map[string]*Bucket{}
	f.failure = nil
	f.refusal = nil
	f.calls = nil
}

func (f *Fake) call(name string) error {
	f.calls = append(f.calls, name)
	if f.failure != nil {
		return fmt.Errorf("%w: %w", engine.ErrUnavailable, f.failure)
	}
	if f.refusal != nil {
		return f.refusal
	}

	return nil
}

func (f *Fake) change(call, bucket string, fn func(b *Bucket)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call(call); err != nil {
		return err
	}
	if b, ok := f.buckets[bucket]; ok {
		fn(b)
	}

	return nil
}

func (f *Fake) presign(call, bucket, key, op string, ttl time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.call(call); err != nil {
		return "", err
	}
	if _, ok := f.buckets[bucket]; !ok {
		return "", fmt.Errorf("fake engine: no bucket %q", bucket)
	}

	return fmt.Sprintf("https://fake.storage.test/%s/%s?op=%s&ttl=%d", bucket, key, op, int(ttl.Seconds())), nil
}
