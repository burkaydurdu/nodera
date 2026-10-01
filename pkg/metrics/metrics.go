package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/piggystats/nodera/logger"
)

const DefaultAddr = ":9100"

// Serve exposes the Go runtime and process collectors on addr. Application
// metrics are a later phase; the listener exists so every binary already has
// a scrape target.
func Serve(ctx context.Context, log logger.ILog, addr string) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	const readHeaderTimeout = 5 * time.Second
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: readHeaderTimeout}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(err)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
}
