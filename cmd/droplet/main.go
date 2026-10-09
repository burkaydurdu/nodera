package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/piggystats/nodera/cmd/droplet/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-droplet",
	Short: "Nodera droplet service",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Droplet HTTP server: catalog and Droplets",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer(config.InitConfig(viper.GetString("env")))
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Catalog sync, status poller and the customer-, billing- and kubernetes-events consumers",
	Run: func(cmd *cobra.Command, args []string) {
		RunWorker(config.InitConfig(viper.GetString("env")))
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply the droplet database migrations",
	Run: func(cmd *cobra.Command, args []string) {
		RunMigration(config.InitConfig(viper.GetString("env")))
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back every droplet migration (drops all tables)",
	Run: func(cmd *cobra.Command, args []string) {
		RunRollback(config.InitConfig(viper.GetString("env")))
	},
}

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Apply the launch catalog seed",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeed(config.InitConfig(viper.GetString("env")))
	},
}

var seedRollbackCmd = &cobra.Command{
	Use:   "seed-rollback",
	Short: "Roll back the droplet seeds",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeedRollback(config.InitConfig(viper.GetString("env")))
	},
}

var outboxCmd = &cobra.Command{
	Use:   "outbox",
	Short: "Relay outbox messages to RabbitMQ",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

		stop := RunOutbox(ctx, config.InitConfig(viper.GetString("env")))

		sig := <-sigChan
		log.Printf("Shutdown signal received (%v), starting graceful shutdown...\n", sig)
		cancel()

		const timeout = 30 * time.Second
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
		defer shutdownCancel()

		if err := stop(shutdownCtx); err != nil {
			log.Printf("Error during shutdown: %v\n", err)
			os.Exit(1)
		}
	},
}

var replayBillingCmd = &cobra.Command{
	Use:   "replay-billing",
	Short: "Republish DropletBillingStarted and DropletBillingStopped for every Droplet that was ever active, through the outbox",
	Run: func(cmd *cobra.Command, args []string) {
		RunReplayBilling(cmd.Context(), config.InitConfig(viper.GetString("env")))
	},
}

var replayCatalogCmd = &cobra.Command{
	Use:   "replay-catalog",
	Short: "Republish every Plan and Region as catalog events for the kubernetes context's read model, through the outbox",
	Run: func(cmd *cobra.Command, args []string) {
		RunReplayCatalog(cmd.Context(), config.InitConfig(viper.GetString("env")))
	},
}

func main() {
	rootCmd.AddCommand(serverCmd, workerCmd, migrateCmd, rollbackCmd, seedCmd, seedRollbackCmd, outboxCmd, replayBillingCmd, replayCatalogCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
