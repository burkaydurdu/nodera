package main

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/piggystats/nodera/cmd/notification/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-notification",
	Short: "Nodera notification service",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Inbox HTTP server",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer(config.InitConfig(viper.GetString("env")))
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Consume email and domain events; record Notifications and send them",
	Run: func(cmd *cobra.Command, args []string) {
		RunWorker(config.InitConfig(viper.GetString("env")))
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply the notification database migrations",
	Run: func(cmd *cobra.Command, args []string) {
		RunMigration(config.InitConfig(viper.GetString("env")))
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Roll back every notification migration (drops all tables)",
	Run: func(cmd *cobra.Command, args []string) {
		RunRollback(config.InitConfig(viper.GetString("env")))
	},
}

var sampleCmd = &cobra.Command{
	Use:   "sample",
	Short: "Dev only: send a sample Notification to a Team through RabbitMQ, as its real event would",
	Run: func(cmd *cobra.Command, args []string) {
		RunSample(config.InitConfig(viper.GetString("env")), sampleType, sampleTeam)
	},
}

var sampleType, sampleTeam string

func main() {
	sampleCmd.Flags().StringVar(&sampleType, "type", "droplet_active", "Notification type, or \"all\" for every one about a Team")
	sampleCmd.Flags().StringVar(&sampleTeam, "team", "", "Team id, shown on the web app's Team page")
	cobra.CheckErr(sampleCmd.MarkFlagRequired("team"))
	rootCmd.AddCommand(serverCmd, workerCmd, migrateCmd, rollbackCmd, sampleCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
