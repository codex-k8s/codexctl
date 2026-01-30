package cli

import (
	"context"
	"fmt"
	"log/slog"
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
	// Check if the image is already present in the local registry.
	checkCmd := exec.CommandContext(ctx, "docker", "manifest", "inspect", local)
	if err := checkCmd.Run(); err == nil {
		logger.Info("image already present in local registry", "name", name, "image", local)
		return nil
	}

	logger.Info("mirroring external image into local registry", "name", name, "from", remote, "to", local)

	if err := runLogged(ctx, logger, "docker", "pull", remote); err != nil {
		return fmt.Errorf("docker pull %q failed: %w", remote, err)
	}

	if err := runLogged(ctx, logger, "docker", "tag", remote, local); err != nil {
		return fmt.Errorf("docker tag %q %q failed: %w", remote, local, err)
	}

	if err := runLogged(ctx, logger, "docker", "push", local); err != nil {
		return fmt.Errorf("docker push %q failed: %w", local, err)
	}

	return nil
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

// buildImages builds and pushes all images with type=build using Docker.
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

	args := []string{"build", "-t", fullRef}
	if dockerfile != "" {
		args = append(args, "-f", dockerfile)
	}

	// Build arguments (templated).
	for key, raw := range img.BuildArgs {
		rendered, err := config.RenderTemplate("image-build-arg-"+name+"-"+key, []byte(raw), tmplCtx)
		if err != nil {
			return fmt.Errorf("render buildArg %q for image %q: %w", key, name, err)
		}
		value := strings.TrimSpace(string(rendered))
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	// Build contexts (templated, path may be relative to project root).
	for key, raw := range img.BuildContexts {
		rendered, err := config.RenderTemplate("image-build-context-"+name+"-"+key, []byte(raw), tmplCtx)
		if err != nil {
			return fmt.Errorf("render buildContext %q for image %q: %w", key, name, err)
		}
		path := strings.TrimSpace(string(rendered))
		if path != "" && !filepath.IsAbs(path) && tmplCtx.ProjectRoot != "" {
			path = filepath.Join(tmplCtx.ProjectRoot, path)
		}
		args = append(args, "--build-context", fmt.Sprintf("%s=%s", key, path))
	}

	args = append(args, contextPath)

	if err := runLogged(ctx, logger, "docker", args...); err != nil {
		return fmt.Errorf("docker build for image %q failed: %w", name, err)
	}

	if err := runLogged(ctx, logger, "docker", "push", fullRef); err != nil {
		return fmt.Errorf("docker push for image %q failed: %w", name, err)
	}

	return nil
}
