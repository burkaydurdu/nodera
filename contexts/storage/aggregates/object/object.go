// Package object is the file browser's side of a Storage: how the web app
// asks to list, fetch, put and delete files. It owns no table; the files are
// the engine's.
package object

import "time"

const (
	// PageSize is how many entries one page of a folder carries.
	PageSize = 200
	// MaxDelete is how many files one request deletes, S3's own ceiling.
	MaxDelete = 1000
	// MaxUploadBytes is one S3 PUT; the web app points to S3 tools for more.
	MaxUploadBytes = 5 << 30
	DownloadTTL    = 5 * time.Minute
	UploadTTL      = 15 * time.Minute
)
