package main

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/piggystats/nodera/cmd/kubernetes/app"
	"github.com/piggystats/nodera/cmd/kubernetes/config"
	"github.com/piggystats/nodera/contexts/kubernetes/application/handlers"
	"github.com/piggystats/nodera/contexts/kubernetes/engine"
	"github.com/piggystats/nodera/contexts/kubernetes/engine/fake"
	"github.com/piggystats/nodera/contexts/kubernetes/engine/kamaji"
	"github.com/piggystats/nodera/logger"
)

// newEngine is the one Region's control plane fleet (docs/adr/0013), or the
// fake in a development environment without one.
func newEngine(conf *config.Config, log logger.ILog, pool *pgxpool.Pool) (handlers.Engine, error) {
	if conf.Engine.Kind == config.EngineFake {
		log.Warn("kubernetes engine is the fake: control planes come up by themselves and no node ever joins")

		clusters := app.NewServices(pool).Clusters

		return fake.NewSelfRunning(func(clusterID uuid.UUID) engine.ControlPlaneSpec {
			c, err := clusters.FindByID(context.Background(), clusterID)
			if err != nil {
				log.ErrorWithFields(err, logger.NewFields().Add("cluster_id", clusterID))

				return engine.ControlPlaneSpec{ClusterID: clusterID}
			}

			return handlers.ControlPlaneSpec(c)
		}), nil
	}
	k := conf.Engine.Kamaji

	return kamaji.New(kamaji.Config{
		APIServer:     k.APIServer,
		Token:         k.Token,
		CACertificate: k.CACertificate,
		Namespace:     k.Namespace,
		DataStore:     k.DataStore,
		Replicas:      k.Replicas,
		AddonsDir:     k.AddonsDir,
		Timeout:       conf.Engine.Timeout(),
	})
}
