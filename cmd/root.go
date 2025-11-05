package cmd

import (
	"os"

	"github.com/marcorentap/esw7004-serverless-runtime/internal/slrun"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	host    string
	port    int
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "slrun",
	Short: "ESW7004 Serverless Runtime",
	Long:  "ESW7004 Serverless Runtime",

	// Uncomment the following line if your bare application
	// has an action associated with it:
	// Run: func(cmd *cobra.Command, args []string) { },
	RunE: func(cmd *cobra.Command, args []string) error {
		return slrun.Start(cfgFile, host, port)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVar(&cfgFile, "config", "slrun.json", "config file (default ./slrun.json)")
	rootCmd.Flags().StringVar(&host, "host", "0.0.0.0", "host to listen on")
	rootCmd.Flags().IntVar(&port, "port", 8080, "port to listen on")
}
