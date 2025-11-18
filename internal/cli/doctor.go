package cli

import "github.com/spf13/cobra"

// newDoctorCommand creates the "doctor" subcommand that runs environment preflight checks.
func newDoctorCommand(_ *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run environment preflight checks",
		RunE:  notImplemented,
	}

	return cmd
}
