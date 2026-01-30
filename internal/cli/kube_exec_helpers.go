package cli

import (
	"context"

	"github.com/codex-k8s/codexctl/internal/kube"
)

// runCodexPodShell executes a shell command inside the codex deployment pod.
func runCodexPodShell(ctx context.Context, client *kube.Client, namespace, command string) error {
	return client.RunRaw(
		ctx,
		nil,
		"-n", namespace,
		"exec", "deploy/codex",
		"--", "sh", "-lc",
		command,
	)
}
