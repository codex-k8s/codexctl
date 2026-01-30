package cli

import (
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

const defaultDeployWaitTimeout = "600s"

// resolveDeployWaitTimeout chooses the effective deploy wait timeout.
func resolveDeployWaitTimeout(stackCfg *config.StackConfig, explicit string, explicitSet bool) string {
	if explicitSet {
		if v := strings.TrimSpace(explicit); v != "" {
			return v
		}
	}
	if stackCfg == nil {
		return defaultDeployWaitTimeout
	}

	if v := strings.TrimSpace(stackCfg.Codex.Timeouts.DeployWait); v != "" {
		return v
	}

	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}

	return defaultDeployWaitTimeout
}
