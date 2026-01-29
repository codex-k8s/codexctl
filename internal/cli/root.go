// Package cli defines the command-line interface for codexctl.
package cli

import (
	"context"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/logging"
)

const (
	// defaultConfigPath is the default path to the stack configuration file.
	defaultConfigPath = "services.yaml"
)

// Options stores global CLI options shared between commands.
type Options struct {
	ConfigPath string
	Env        string
	Namespace  string
	LogLevel   logging.Level
}

// Execute builds the root command, runs it with the provided args and logger, and returns any error.
func Execute(args []string, logger *slog.Logger) error {
	if logger == nil {
		logger = logging.NewLogger(os.Stderr, logging.LevelInfo)
	}

	rootOpts := &Options{
		ConfigPath: defaultConfigPath,
		LogLevel:   logging.LevelInfo,
	}

	rootCmd := newRootCommand(rootOpts, logger)
	rootCmd.SetArgs(args)

	return rootCmd.Execute()
}

// newRootCommand constructs the root cobra.Command with global flags and subcommands.
func newRootCommand(opts *Options, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codexctl",
		Short: "codexctl is a declarative Kubernetes deployment helper",
		Long:  "codexctl is a declarative tool for managing Kubernetes environments and ephemeral dev/AI slots based on a services.yaml definition.",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			level := logging.ParseLevel(cmd.Flag("log-level").Value.String())
			opts.LogLevel = level
			logger = logging.NewLogger(os.Stderr, level)
			cmd.SetContext(context.WithValue(cmd.Context(), loggerKey{}, logger))
			logger.Debug("logger initialized", "level", level)
			return nil
		},
	}

	cmd.PersistentFlags().StringVarP(&opts.ConfigPath, "config", "c", defaultConfigPath, "Path to services.yaml configuration file")
	cmd.PersistentFlags().StringVar(&opts.Env, "env", "", "Environment name (e.g. dev, staging, ai)")
	cmd.PersistentFlags().StringVar(&opts.Namespace, "namespace", "", "Target Kubernetes namespace override")
	cmd.PersistentFlags().String("log-level", "info", "Log level (debug, info, warn, error)")

	cmd.AddCommand(
		newApplyCommand(opts),
		newCICommand(opts),
		newImagesCommand(opts),
		newManageEnvCommand(opts),
		newRenderCommand(opts),
		newPromptCommand(opts),
		newPlanCommand(opts),
		newPRCommand(opts),
	)

	return cmd
}

// loggerKey is a private context key used to store a logger in command contexts.
type loggerKey struct{}

// LoggerFromContext extracts a logger from the context or falls back to a default logger.
func LoggerFromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return logging.NewLogger(os.Stderr, logging.LevelInfo)
	}
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return logging.NewLogger(os.Stderr, logging.LevelInfo)
}
