package cli

import (
	"os"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

func applyKubeconfigOverride(envCfg *config.Environment) {
	if envCfg == nil {
		return
	}
	if override := strings.TrimSpace(os.Getenv("CODEXCTL_KUBECONFIG")); override != "" {
		envCfg.Kubeconfig = override
	}
	if override := strings.TrimSpace(os.Getenv("CODEXCTL_KUBE_CONTEXT")); override != "" {
		envCfg.Context = override
	}
}
