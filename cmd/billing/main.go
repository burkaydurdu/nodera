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

	"github.com/piggystats/nodera/cmd/billing/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-billing",
	Short: "Nodera billing service",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Billing HTTP server: profiles, payment methods, usage, invoices, payments, status and provider webhooks",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer(config.InitConfig(viper.GetString("env")))
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Exchange rate fetch, Period close, Thresholds, Dunning, reconciliation, card expiry and the event consumers",
	Run: func(cmd *cobra.Command, args []string) {
		RunWorker(config.InitConfig(viper.GetString("env")))
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

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply the billing database migrations",
	Run: func(cmd *cobra.Command, args []string) {
		RunMigration(config.InitConfig(viper.GetString("env")))
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back every billing migration (drops all tables)",
	Run: func(cmd *cobra.Command, args []string) {
		RunRollback(config.InitConfig(viper.GetString("env")))
	},
}

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Apply the payment routes, tax rules and thresholds seed",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeed(config.InitConfig(viper.GetString("env")))
	},
}

var seedRollbackCmd = &cobra.Command{
	Use:   "seed-rollback",
	Short: "Roll back the billing seeds",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeedRollback(config.InitConfig(viper.GetString("env")))
	},
}

var replayStatusCmd = &cobra.Command{
	Use:   "replay-status",
	Short: "Republish TeamBillingStatusChanged with every Team's current status through the outbox",
	Run: func(cmd *cobra.Command, args []string) {
		RunReplayStatus(cmd.Context(), config.InitConfig(viper.GetString("env")))
	},
}

var samplePaymentMethodCmd = &cobra.Command{
	Use:   "sample-payment-method",
	Short: "Dev only: save a Payment method no Payment provider holds for a Team, as a verified card would be",
	Run: func(cmd *cobra.Command, args []string) {
		RunSamplePaymentMethod(cmd.Context(), config.InitConfig(viper.GetString("env")), sampleTeam)
	},
}

var sampleTeam string

func main() {
	samplePaymentMethodCmd.Flags().StringVar(&sampleTeam, "team", "", "Team id, shown on the web app's Team page")
	cobra.CheckErr(samplePaymentMethodCmd.MarkFlagRequired("team"))
	rootCmd.AddCommand(serverCmd, workerCmd, outboxCmd, migrateCmd, rollbackCmd, seedCmd, seedRollbackCmd, replayStatusCmd, samplePaymentMethodCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
