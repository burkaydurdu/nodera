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

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Consume email events and send the emails",
	Run: func(cmd *cobra.Command, args []string) {
		RunWorker(config.InitConfig(viper.GetString("env")))
	},
}

func main() {
	rootCmd.AddCommand(workerCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
