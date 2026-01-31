package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/logging"
)

// newImagesCommand creates the "images" subtree used to manage images declared in services.yaml.
func newImagesCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "images",
		Short: "Manage container images declared in services.yaml",
	}

	cmd.AddCommand(
		newImagesMirrorCommand(opts),
		newImagesBuildCommand(opts),
	)
	return cmd
}

// newImagesMirrorCommand creates the "images mirror" subcommand that mirrors external images
// into the local registry (e.g. MicroK8s registry) before builds and deployments.
func newImagesMirrorCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mirror",
		Short: "Mirror external images into the local registry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			stackCfg, _, _, _, err := loadStackConfigFromCmd(opts, cmd, 0)
			if err != nil {
				return err
			}

			if err := mirrorExternalImages(cmd.Context(), logger, stackCfg); err != nil {
				return err
			}

			return nil
		},
	}

	addVarsFlags(cmd)

	return cmd
}

// mirrorExternalImages ensures that all images with type=external are present in the local registry.
func mirrorExternalImages(ctx context.Context, logger *slog.Logger, cfg *config.StackConfig) error {
	if cfg == nil {
		return fmt.Errorf("stack config is nil")
	}
	if len(cfg.Images) == 0 {
		logger.Info("no images block defined in services.yaml; nothing to mirror")
		return nil
	}

	for name, img := range cfg.Images {
		if strings.ToLower(strings.TrimSpace(img.Type)) != "external" {
			continue
		}

		remote := strings.TrimSpace(img.From)
		local := strings.TrimSpace(img.Local)
		if remote == "" || local == "" {
			return fmt.Errorf("image %q of type=external must define both from and local", name)
		}

		if err := ensureImageMirrored(ctx, logger, name, remote, local); err != nil {
			return err
		}
	}

	return nil
}

// ensureImageMirrored checks if the local image reference exists, and if not,
// pulls it from the remote reference and pushes it to the local registry.
func ensureImageMirrored(ctx context.Context, logger *slog.Logger, name, remote, local string) error {
	logger.Info("mirroring external image into local registry", "name", name, "from", remote, "to", local)
	return runKanikoMirror(ctx, logger, remote, local)
}

// runLogged runs a command and logs it at info level.
func runLogged(ctx context.Context, logger *slog.Logger, name string, args ...string) error {
	logger.Info("running command", "cmd", name, "args", args)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = logging.NewWriter(logger)
	cmd.Stderr = logging.NewWriter(logger)
	return cmd.Run()
}

// newImagesBuildCommand creates the "images build" subcommand that builds and pushes
// images with type=build based on the images block in services.yaml.
func newImagesBuildCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build and push images defined in the images block",
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := LoggerFromContext(cmd.Context())

			slot, err := cmd.Flags().GetInt("slot")
			if err != nil {
				return err
			}

			stackCfg, tmplCtx, _, _, err := loadStackConfigFromCmd(opts, cmd, slot)
			if err != nil {
				return err
			}

			if err := buildImages(cmd.Context(), logger, stackCfg, tmplCtx); err != nil {
				return err
			}

			return nil
		},
	}

	addVarsFlags(cmd)
	cmd.Flags().Int("slot", 0, "Slot number for slot-based environments (e.g. ai)")

	return cmd
}

// buildImages builds and pushes all images with type=build using Kaniko.
func buildImages(ctx context.Context, logger *slog.Logger, cfg *config.StackConfig, tmplCtx config.TemplateContext) error {
	if cfg == nil {
		return fmt.Errorf("stack config is nil")
	}
	if len(cfg.Images) == 0 {
		logger.Info("no images block defined in services.yaml; nothing to build")
		return nil
	}

	for name, img := range cfg.Images {
		if strings.ToLower(strings.TrimSpace(img.Type)) != "build" {
			continue
		}

		if err := buildSingleImage(ctx, logger, name, img, tmplCtx); err != nil {
			return err
		}
	}

	return nil
}

// buildSingleImage builds and pushes one image definition.
func buildSingleImage(ctx context.Context, logger *slog.Logger, name string, img config.ImageSpec, tmplCtx config.TemplateContext) error {
	repo := strings.TrimSpace(img.Repository)
	if repo == "" {
		return fmt.Errorf("image %q of type=build must define repository", name)
	}

	tag := strings.TrimSpace(img.Tag)
	if tag == "" && strings.TrimSpace(img.TagTemplate) != "" {
		rendered, err := config.RenderTemplate("image-tag", []byte(img.TagTemplate), tmplCtx)
		if err != nil {
			return fmt.Errorf("render tagTemplate for image %q: %w", name, err)
		}
		tag = strings.TrimSpace(string(rendered))
	}
	if tag == "" {
		return fmt.Errorf("image %q of type=build must define tag or tagTemplate", name)
	}

	fullRef := fmt.Sprintf("%s:%s", repo, tag)

	dockerfile := strings.TrimSpace(img.Dockerfile)
	contextPath := strings.TrimSpace(img.Context)
	if contextPath == "" {
		contextPath = "."
	}

	logger.Info("building image", "name", name, "image", fullRef, "dockerfile", dockerfile, "context", contextPath)

	kanikoCfg, err := resolveKanikoConfig()
	if err != nil {
		return err
	}

	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if !filepath.IsAbs(dockerfile) && tmplCtx.ProjectRoot != "" {
		dockerfile = filepath.Join(tmplCtx.ProjectRoot, dockerfile)
	}

	// Build arguments (templated).
	buildArgs := make(map[string]string, len(img.BuildArgs))
	for key, raw := range img.BuildArgs {
		rendered, err := config.RenderTemplate("image-build-arg-"+name+"-"+key, []byte(raw), tmplCtx)
		if err != nil {
			return fmt.Errorf("render buildArg %q for image %q: %w", key, name, err)
		}
		value := strings.TrimSpace(string(rendered))
		buildArgs[key] = value
	}

	// Build contexts (templated, path may be relative to project root).
	buildContexts := make(map[string]string, len(img.BuildContexts))
	for key, raw := range img.BuildContexts {
		rendered, err := config.RenderTemplate("image-build-context-"+name+"-"+key, []byte(raw), tmplCtx)
		if err != nil {
			return fmt.Errorf("render buildContext %q for image %q: %w", key, name, err)
		}
		path := strings.TrimSpace(string(rendered))
		if path != "" && !filepath.IsAbs(path) && tmplCtx.ProjectRoot != "" {
			path = filepath.Join(tmplCtx.ProjectRoot, path)
		}
		buildContexts[key] = path
	}

	if err := runKanikoBuild(ctx, logger, kanikoCfg, contextPath, dockerfile, fullRef, buildArgs, buildContexts); err != nil {
		return fmt.Errorf("kaniko build for image %q failed: %w", name, err)
	}

	return nil
}

// kanikoConfig describes the execution settings for Kaniko builds.
type kanikoConfig struct {
	// Executor is the kaniko executor binary path.
	Executor string
	// Insecure enables pushing to insecure registries.
	Insecure bool
	// SkipTLSVerify disables TLS verification when pushing.
	SkipTLSVerify bool
	// SkipTLSVerifyPull disables TLS verification when pulling.
	SkipTLSVerifyPull bool
}

// resolveKanikoConfig reads Kaniko settings from the environment.
func resolveKanikoConfig() (kanikoConfig, error) {
	execPath := strings.TrimSpace(os.Getenv("CODEXCTL_KANIKO_EXECUTOR"))
	if execPath == "" {
		execPath = "/kaniko/executor"
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return kanikoConfig{}, fmt.Errorf("kaniko executor not found: %w", err)
	}
	return kanikoConfig{
		Executor:          execPath,
		Insecure:          lookupEnvBool("CODEXCTL_KANIKO_INSECURE"),
		SkipTLSVerify:     lookupEnvBool("CODEXCTL_KANIKO_SKIP_TLS_VERIFY"),
		SkipTLSVerifyPull: lookupEnvBool("CODEXCTL_KANIKO_SKIP_TLS_VERIFY_PULL"),
	}, nil
}

// runKanikoMirror builds a scratch Dockerfile that mirrors a remote image.
func runKanikoMirror(ctx context.Context, logger *slog.Logger, remote, local string) error {
	kanikoCfg, err := resolveKanikoConfig()
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "codexctl-mirror-*")
	if err != nil {
		return fmt.Errorf("create temp dir for mirror: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfile := filepath.Join(tmpDir, "Dockerfile")
	content := fmt.Sprintf("FROM %s\n", remote)
	if err := os.WriteFile(dockerfile, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write mirror Dockerfile: %w", err)
	}

	return runKanikoBuild(ctx, logger, kanikoCfg, tmpDir, dockerfile, local, nil, nil)
}

// runKanikoBuild runs the kaniko executor for a build context.
func runKanikoBuild(
	ctx context.Context,
	logger *slog.Logger,
	cfg kanikoConfig,
	contextPath string,
	dockerfile string,
	destination string,
	buildArgs map[string]string,
	buildContexts map[string]string,
) error {
	ctxPath := strings.TrimSpace(contextPath)
	if ctxPath == "" {
		return fmt.Errorf("kaniko context is empty")
	}
	if !filepath.IsAbs(ctxPath) {
		abs, err := filepath.Abs(ctxPath)
		if err != nil {
			return fmt.Errorf("resolve build context %q: %w", ctxPath, err)
		}
		ctxPath = abs
	}
	ctxArg := formatKanikoContext(ctxPath)

	args := []string{
		"--context", ctxArg,
		"--destination", destination,
	}
	if dockerfile != "" {
		args = append(args, "--dockerfile", dockerfile)
	}
	if cfg.Insecure {
		args = append(args, "--insecure")
	}
	if cfg.SkipTLSVerify {
		args = append(args, "--skip-tls-verify")
	}
	if cfg.SkipTLSVerifyPull {
		args = append(args, "--skip-tls-verify-pull")
	}

	for key, value := range buildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}
	for key, path := range buildContexts {
		if strings.TrimSpace(path) == "" {
			continue
		}
		ctxRef := formatKanikoContext(path)
		args = append(args, "--build-context", fmt.Sprintf("%s=%s", key, ctxRef))
	}

	return runLogged(ctx, logger, cfg.Executor, args...)
}

// formatKanikoContext ensures the context path has a scheme understood by kaniko.
func formatKanikoContext(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	if strings.Contains(path, "://") {
		return path
	}
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	return "dir://" + path
}

// lookupEnvBool reads a boolean env var using the shared parser.
func lookupEnvBool(key string) bool {
	raw, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	parsed, ok := parseEnvBool(raw)
	if !ok {
		return false
	}
	return parsed
}
