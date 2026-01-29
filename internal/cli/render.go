package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
)

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
			inlineVars, err := env.ParseInlineVars(cmd.Flag("vars").Value.String())
			if err != nil {
				return err
			}

			varFile := cmd.Flag("var-file").Value.String()
			var varFiles []string
			if varFile != "" {
				varFiles = append(varFiles, varFile)
			}

			loadOpts := config.LoadOptions{
				Env:       opts.Env,
				Namespace: opts.Namespace,
				Slot:      slot,
				UserVars:  inlineVars,
				VarFiles:  varFiles,
			}

			stackCfg, ctxData, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
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

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to render (dev, staging, ai, etc.)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for rendering")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number for slot-based environments (e.g. ai)")
	cmd.Flags().StringVar(&onlyServices, "only-services", "", "Render only selected services (comma-separated names)")
	cmd.Flags().StringVar(&skipServices, "skip-services", "", "Skip selected services (comma-separated names)")
	cmd.Flags().StringVar(&onlyInfra, "only-infra", "", "Render only selected infra blocks (comma-separated names)")
	cmd.Flags().StringVar(&skipInfra, "skip-infra", "", "Skip selected infra blocks (comma-separated names)")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	_ = cmd.MarkFlagRequired("env")

	return cmd
}
