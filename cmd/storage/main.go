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

	"github.com/piggystats/nodera/cmd/storage/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-storage",
	Short: "Nodera storage service",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "The management API behind the customer gateway",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer(config.InitConfig(viper.GetString("env")))
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Measurement, traffic, Period rollover, purge and reconcile jobs, and the customer- and billing-events consumers",
	Run: func(cmd *cobra.Command, args []string) {
		RunWorker(config.InitConfig(viper.GetString("env")))
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply the storage database migrations",
	Run: func(cmd *cobra.Command, args []string) {
		RunMigration(config.InitConfig(viper.GetString("env")))
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back every storage migration (drops all tables)",
	Run: func(cmd *cobra.Command, args []string) {
		RunRollback(config.InitConfig(viper.GetString("env")))
	},
}

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Apply the launch Region and Package catalog",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeed(config.InitConfig(viper.GetString("env")))
	},
}

var seedRollbackCmd = &cobra.Command{
	Use:   "seed-rollback",
	Short: "Roll back the storage seeds",
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

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause a Storage after a complaint (an operator's decision until the Admin side exists)",
	Run: func(cmd *cobra.Command, args []string) {
		storageID, _ := cmd.Flags().GetString("storage")
		reason, _ := cmd.Flags().GetString("reason")
		RunPause(cmd.Context(), config.InitConfig(viper.GetString("env")), storageID, reason)
	},
}

var unpauseCmd = &cobra.Command{
	Use:   "unpause",
	Short: "Lift a Storage's Pause",
	Run: func(cmd *cobra.Command, args []string) {
		storageID, _ := cmd.Flags().GetString("storage")
		RunUnpause(cmd.Context(), config.InitConfig(viper.GetString("env")), storageID)
	},
}

func main() {
	pauseCmd.Flags().String("storage", "", "Storage id")
	pauseCmd.Flags().String("reason", "", "abuse_complaint, malware, phishing or legal")
	cobra.CheckErr(pauseCmd.MarkFlagRequired("storage"))
	cobra.CheckErr(pauseCmd.MarkFlagRequired("reason"))
	unpauseCmd.Flags().String("storage", "", "Storage id")
	cobra.CheckErr(unpauseCmd.MarkFlagRequired("storage"))

	rootCmd.AddCommand(serverCmd, workerCmd, migrateCmd, rollbackCmd, seedCmd, seedRollbackCmd, outboxCmd, pauseCmd, unpauseCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
