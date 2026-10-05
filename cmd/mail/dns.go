package main

import (
	"context"
	"time"

	mdns "github.com/miekg/dns"

	"github.com/piggystats/nodera/cmd/mail/app"
	"github.com/piggystats/nodera/cmd/mail/config"
	"github.com/piggystats/nodera/logger"
	"github.com/piggystats/nodera/pkg/metrics"
	"github.com/piggystats/nodera/pkg/recover"
)

// RunDNS answers the zone over UDP and TCP and reloads the published keys on
// an interval; a key added in between is found by the lookup on a miss.
func RunDNS(conf *config.Config) {
	log := logger.New(conf.Environment)
	ctx := context.Background()
	defer recover.Init(log)

	if conf.Metrics.Enabled {
		metrics.Serve(ctx, log, conf.Metrics.Addr())
	}

	deps, pool := newDeps(ctx, conf, log, "nodera-mail-dns", nil)
	defer pool.Close()

	server, err := app.BuildDNS(deps, app.NewHandlers(deps))
	if err != nil {
		log.Fatal(err)
	}
	if err := server.Refresh(ctx); err != nil {
		log.Fatal(err)
	}

	go func() {
		ticker := time.NewTicker(conf.DNS.Refresh())
		defer ticker.Stop()
		for range ticker.C {
			if err := server.Refresh(ctx); err != nil {
				log.Error(err)
			}
		}
	}()

	go func() {
		log.Fatal((&mdns.Server{Addr: conf.DNS.ListenAddr, Net: "tcp", Handler: server}).ListenAndServe())
	}()
	log.Fatal((&mdns.Server{Addr: conf.DNS.ListenAddr, Net: "udp", Handler: server}).ListenAndServe())
}
