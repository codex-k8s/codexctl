package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/engine"
)

// newRenderCommand creates the "render" subcommand that prints rendered manifests.
func newRenderCommand(opts *Options) *cobra.Command {
	var (
		slot         int
		onlyServices string
		skipServices string
		onlyInfra    string
		skipInfra    string
	)

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render manifests for an environment and print YAML to stdout",
		RunE: func(cmd *cobra.Command, _ []string) error {
			stackCfg, ctxData, _, _, err := loadStackConfigFromCmd(opts, cmd, slot)
			if err != nil {
				return err
			}

			eng := engine.NewEngine()
			renderOpts := engine.RenderOptions{
				OnlyInfra:    parseNameSet(onlyInfra),
				SkipInfra:    parseNameSet(skipInfra),
				OnlyServices: parseNameSet(onlyServices),
				SkipServices: parseNameSet(skipServices),
			}

			manifests, err := eng.RenderStackWithOptions(stackCfg, ctxData, renderOpts)
			if err != nil {
				return err
			}

			if _, err := os.Stdout.Write(manifests); err != nil {
				return fmt.Errorf("write rendered manifests: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to render (dev, ai-staging, ai, etc.)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for rendering")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number for slot-based environments (e.g. ai)")
	addRenderFilterFlags(cmd, &onlyServices, &skipServices, &onlyInfra, &skipInfra, "Render", "Skip")
	addVarsFlags(cmd)
	_ = cmd.MarkFlagRequired("env")

	return cmd
}
