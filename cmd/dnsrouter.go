package cmd

import (
	"github.com/anonvector/slipgate/internal/dnsrouter"
	"github.com/spf13/cobra"
)

func init() {
	dnsrouterCmd := &cobra.Command{
		Use:   "dnsrouter",
		Short: "DNS router management",
	}

	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the DNS router (used by systemd)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadOrDefaultConfig()
			if err != nil {
				return err
			}
			return dnsrouter.Serve(cfg)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	dnsrouterCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(dnsrouterCmd)
}
