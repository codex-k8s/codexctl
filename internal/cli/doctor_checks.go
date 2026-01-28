package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

func runDoctorChecks(_ context.Context, logger *slog.Logger, stackCfg *config.StackConfig, _ config.TemplateContext, _ config.Environment, envName string) error {
	if logger == nil {
		logger = slog.Default()
	}

	var required []string
	required = append(required, "kubectl", "bash")
	if stackCfg != nil && len(stackCfg.Images) > 0 {
		required = append(required, "docker")
	}
	required = append(required, "git", "gh")

	optional := []string{"rsync"}

	missing := make([]string, 0, len(required))
	for _, tool := range required {
		if _, err := exec.LookPath(tool); err != nil {
			logger.Error("doctor check failed: missing required tool", "tool", tool, "env", envName, "error", err)
			missing = append(missing, tool)
			continue
		}
		logger.Info("doctor check ok", "tool", tool, "env", envName)
	}

	for _, tool := range optional {
		if _, err := exec.LookPath(tool); err != nil {
			logger.Warn("optional tool not found; falling back to slower implementation", "tool", tool, "env", envName)
			continue
		}
		logger.Info("doctor check ok", "tool", tool, "env", envName)
	}

	if len(missing) > 0 {
		return fmt.Errorf("required tools missing from PATH: %s", strings.Join(missing, ", "))
	}

	return nil
}
