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

	"github.com/piggystats/nodera/cmd/customer/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-customer",
	Short: "Nodera customer service",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Customer HTTP server",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer(config.InitConfig(viper.GetString("env")))
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply the customer database migrations",
	Run: func(cmd *cobra.Command, args []string) {
		RunMigration(config.InitConfig(viper.GetString("env")))
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back every customer migration (drops all tables)",
	Run: func(cmd *cobra.Command, args []string) {
		RunRollback(config.InitConfig(viper.GetString("env")))
	},
}

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Apply the customer seeds",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeed(config.InitConfig(viper.GetString("env")))
	},
}

var seedRollbackCmd = &cobra.Command{
	Use:   "seed-rollback",
	Short: "Roll back the customer seeds",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeedRollback(config.InitConfig(viper.GetString("env")))
	},
}

var replayProjectsCmd = &cobra.Command{
	Use:   "replay-projects",
	Short: "Republish TeamCreated, TeamMemberAdded and ProjectCreated for every existing row through the outbox",
	Run: func(cmd *cobra.Command, args []string) {
		RunReplayProjects(cmd.Context(), config.InitConfig(viper.GetString("env")))
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

func main() {
	rootCmd.AddCommand(serverCmd, migrateCmd, rollbackCmd, seedCmd, seedRollbackCmd, outboxCmd, replayProjectsCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
