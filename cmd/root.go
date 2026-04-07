package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/L1ttlebear/ippool/cmd/flags"
)

var rootCmd = &cobra.Command{
	Use:   "ippool",
	Short: "IP Pool State Machine Monitor",
	Run: func(cmd *cobra.Command, args []string) {
		RunServer()
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// RunServer is an alias so server.go can call it directly.
func RunServer() {
	runServer(nil, nil)
}

func init() {
	listen := getEnv("PORT", "0.0.0.0:8080")
	dbFile := getEnv("DB_PATH", "./data/ippool.db")
	rootCmd.PersistentFlags().StringVarP(&flags.Listen, "listen", "l", listen, "Listen address [env: PORT]")
	rootCmd.PersistentFlags().StringVarP(&flags.DatabaseFile, "db", "d", dbFile, "SQLite database path [env: DB_PATH]")
	flags.DatabaseType = "sqlite"
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
