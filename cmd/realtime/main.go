package main

import (
	"log"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/piggystats/nodera/cmd/realtime/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-realtime",
	Short: "Nodera realtime service: the web app's live socket",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "WebSocket server, consumer of the contexts' realtime messages and member of the replicas' fan-out",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer(config.InitConfig(viper.GetString("env")))
	},
}

func main() {
	rootCmd.AddCommand(serverCmd)
	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Config file name under .conf (dev|test|alpha|prod)")
	cobra.CheckErr(rootCmd.MarkPersistentFlagRequired("env"))
	cobra.CheckErr(viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env")))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
