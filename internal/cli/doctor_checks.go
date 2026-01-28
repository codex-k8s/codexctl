package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

type doctorParams struct {
	stackCfg *config.StackConfig
	envName  string
}

func runDoctorChecks(_ context.Context, logger *slog.Logger, params doctorParams) error {
	if logger == nil {
		logger = slog.Default()
	}

	var required []string
	required = append(required, "kubectl", "bash")
	if params.stackCfg != nil && len(params.stackCfg.Images) > 0 {
		required = append(required, "docker")
	}
	required = append(required, "git", "gh")

	optional := []string{"rsync"}

	missing := make([]string, 0, len(required))
	for _, tool := range required {
		if _, err := exec.LookPath(tool); err != nil {
			logger.Error("doctor check failed: missing required tool", "tool", tool, "env", params.envName, "error", err)
			missing = append(missing, tool)
			continue
		}
		logger.Info("doctor check ok", "tool", tool, "env", params.envName)
	}

	for _, tool := range optional {
		if _, err := exec.LookPath(tool); err != nil {
			logger.Warn("optional tool not found; falling back to slower implementation", "tool", tool, "env", params.envName)
			continue
		}
		logger.Info("doctor check ok", "tool", tool, "env", params.envName)
	}

	if len(missing) > 0 {
		return fmt.Errorf("required tools missing from PATH: %s", strings.Join(missing, ", "))
	}

	return nil
}
