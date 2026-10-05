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

	"github.com/piggystats/nodera/cmd/mail/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-mail",
	Short: "Nodera mail service",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Mail HTTP listeners: the management API, the sending API and the MTA's internal API",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer(config.InitConfig(viper.GetString("env")))
	},
}

var smtpCmd = &cobra.Command{
	Use:   "smtp",
	Short: "The SMTP relay: STARTTLS on the submission and alternative ports, TLS on the TLS port",
	Run: func(cmd *cobra.Command, args []string) {
		RunSMTP(config.InitConfig(viper.GetString("env")))
	},
}

var dnsCmd = &cobra.Command{
	Use:   "dns",
	Short: "The authoritative server of the DKIM zone (docs/adr/0011)",
	Run: func(cmd *cobra.Command, args []string) {
		RunDNS(config.InitConfig(viper.GetString("env")))
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Sending domain checks, key rotation, Period rollover, hand-over, limits, Webhooks, retention and the customer- and billing-events consumers",
	Run: func(cmd *cobra.Command, args []string) {
		RunWorker(config.InitConfig(viper.GetString("env")))
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply the mail database migrations",
	Run: func(cmd *cobra.Command, args []string) {
		RunMigration(config.InitConfig(viper.GetString("env")))
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back every mail migration (drops all tables)",
	Run: func(cmd *cobra.Command, args []string) {
		RunRollback(config.InitConfig(viper.GetString("env")))
	},
}

var seedCmd = &cobra.Command{
	Use:   "seed",
	Short: "Apply the launch Package catalog",
	Run: func(cmd *cobra.Command, args []string) {
		RunSeed(config.InitConfig(viper.GetString("env")))
	},
}

var seedRollbackCmd = &cobra.Command{
	Use:   "seed-rollback",
	Short: "Roll back the mail seeds",
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

var unpauseCmd = &cobra.Command{
	Use:   "unpause",
	Short: "Lift a Project's Pause (an operator's decision until the Admin side exists)",
	Run: func(cmd *cobra.Command, args []string) {
		project, _ := cmd.Flags().GetString("project")
		RunUnpause(cmd.Context(), config.InitConfig(viper.GetString("env")), project)
	},
}

var verifySendingDomainCmd = &cobra.Command{
	Use:   "verify-sending-domain",
	Short: "Dev only: verify a Sending domain as if its DNS records were published, since dev's zone is not public",
	Run: func(cmd *cobra.Command, args []string) {
		sendingDomain, _ := cmd.Flags().GetString("sending-domain")
		RunVerifySendingDomain(cmd.Context(), config.InitConfig(viper.GetString("env")), sendingDomain)
	},
}

func main() {
	unpauseCmd.Flags().String("project", "", "Project id")
	cobra.CheckErr(unpauseCmd.MarkFlagRequired("project"))
	verifySendingDomainCmd.Flags().String("sending-domain", "", "Sending domain id, the last part of its page's URL in the web app")
	cobra.CheckErr(verifySendingDomainCmd.MarkFlagRequired("sending-domain"))

	rootCmd.AddCommand(serverCmd, smtpCmd, dnsCmd, workerCmd, migrateCmd, rollbackCmd, seedCmd, seedRollbackCmd, outboxCmd, unpauseCmd, verifySendingDomainCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
