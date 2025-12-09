package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/engine"
	"github.com/codex-k8s/codexctl/internal/env"
)

// newRenderCommand creates the "render" subcommand that renders manifests from services.yaml.
func newRenderCommand(opts *Options) *cobra.Command {
	var slot int

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render Kubernetes manifests from services.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

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

			stackCfg, ctx, err := config.LoadStackConfig(opts.ConfigPath, loadOpts)
			if err != nil {
				return err
			}

			eng := engine.NewEngine()
			rendered, err := eng.RenderStack(stackCfg, ctx)
			if err != nil {
				return err
			}

			outputDir := cmd.Flag("output").Value.String()
			toStdout, _ := cmd.Flags().GetBool("stdout")

			if outputDir == "" || toStdout {
				_, writeErr := os.Stdout.Write(rendered)
				return writeErr
			}

			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return fmt.Errorf("create output directory %q: %w", outputDir, err)
			}

			outPath := filepath.Join(outputDir, "rendered.yaml")
			if err := os.WriteFile(outPath, rendered, 0o644); err != nil {
				return fmt.Errorf("write rendered config to %q: %w", outPath, err)
			}

			logger.Info("rendered manifests", "path", outPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment to render (dev, staging, ai)")
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace override for rendered manifests")
	cmd.Flags().IntVar(&slot, "slot", 0, "Slot number for slot-based environments (e.g. ai)")
	cmd.Flags().StringP("output", "o", "", "Output directory for rendered manifests (if empty, prints to stdout)")
	cmd.Flags().Bool("stdout", false, "Force output to stdout instead of files")
	cmd.Flags().String("vars", "", "Additional variables in k=v,k2=v2 format")
	cmd.Flags().String("var-file", "", "Path to YAML/ENV file with additional variables")
	_ = cmd.MarkFlagRequired("env")

	return cmd
}
