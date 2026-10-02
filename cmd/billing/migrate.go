package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/piggystats/nodera/cmd/billing/config"
	"github.com/piggystats/nodera/logger"
)

const (
	migrationsPath = "contexts/billing/db/migrations"
	seedPath       = "contexts/billing/db/seed"
)

func RunMigration(conf *config.Config) {
	runMigrate(conf, migrationsPath, "schema_migrations", "up")
}

func RunRollback(conf *config.Config) {
	runMigrate(conf, migrationsPath, "schema_migrations", "down", "-all")
}

func RunSeed(conf *config.Config) {
	runMigrate(conf, seedPath, "schema_seeds", "up")
}

func RunSeedRollback(conf *config.Config) {
	runMigrate(conf, seedPath, "schema_seeds", "down", "-all")
}

// runMigrate shells out to the golang-migrate CLI, which must be on PATH.
func runMigrate(conf *config.Config, path, table string, args ...string) {
	log := logger.New(conf.Environment)

	separator := "?"
	if strings.Contains(conf.PostgreSQL.URI, "?") {
		separator = "&"
	}
	database := fmt.Sprintf("%s%sx-migrations-table=%s", conf.PostgreSQL.URI, separator, table)
	cmdArgs := append([]string{"-path", path, "-database", database}, args...)
	cmd := exec.CommandContext(context.Background(), "migrate", cmdArgs...) // #nosec G204 - arguments are ours
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Infof("Running: migrate -path %s %v", path, args)
	if err := cmd.Run(); err != nil {
		log.Error(err)
		os.Exit(1)
	}

	log.Info("migrate completed")
}
