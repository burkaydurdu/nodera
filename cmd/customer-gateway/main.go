package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/piggystats/nodera/cmd/customer-gateway/config"
)

var rootCmd = &cobra.Command{
	Use:   "nodera-customer-gateway",
	Short: "Nodera customer gateway",
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Customer gateway server",
	Run: func(cmd *cobra.Command, args []string) {
		const timeout = 30 * time.Second
		appConfig := config.InitConfig(viper.GetString("env"))
		gateway := config.InitGatewayConfig(viper.GetString("env"))

		server, err := CreateServer(appConfig, gateway)
		cobra.CheckErr(err)

		go func() {
			if err := server.Start(appConfig.Server.Port); err != nil && err != http.ErrServerClosed {
				server.Logger.Fatal("shutting down the server")
			}
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		<-quit

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			server.Logger.Fatal(err)
		}
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
