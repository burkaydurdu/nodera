package main

import (
	"github.com/piggystats/nodera/cmd/storage/config"
	"github.com/piggystats/nodera/contexts/storage/application/handlers"
	"github.com/piggystats/nodera/contexts/storage/engine/ceph"
)

// newEngine is the one Region's object store (docs/adr/0012).
func newEngine(conf *config.Config) (handlers.Engine, error) {
	e := conf.Engine

	return ceph.New(ceph.Config{
		AdminEndpoint:  e.AdminEndpoint,
		S3Endpoint:     e.S3Endpoint,
		PublicEndpoint: e.PublicEndpoint,
		VirtualHosted:  !e.PathStyle,
		AdminAccessKey: e.AdminAccessKey,
		AdminSecretKey: e.AdminSecretKey,
		Region:         e.S3Region,
		WebOrigin:      e.WebOrigin,
		RateLimit: ceph.RateLimit{
			ReadOps: e.RateLimit.ReadOps, WriteOps: e.RateLimit.WriteOps,
			ReadBytes: e.RateLimit.ReadBytes, WriteBytes: e.RateLimit.WriteBytes,
		},
		Timeout: e.Timeout(),
	})
}
