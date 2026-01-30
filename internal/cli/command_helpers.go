package cli

import "github.com/spf13/cobra"

// newGroupCommand builds a cobra.Command that groups subcommands.
func newGroupCommand(use, short string, subcommands ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
	}
	if len(subcommands) > 0 {
		cmd.AddCommand(subcommands...)
	}
	return cmd
}
