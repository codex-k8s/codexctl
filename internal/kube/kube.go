// Package kube provides low-level integration with Kubernetes via kubectl and related helpers.
package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Client wraps kubectl execution with optional kubeconfig and context selection.
type Client struct {
	Kubeconfig string
	Context    string
}

// NewClient constructs a new Kubernetes client wrapper.
func NewClient(kubeconfig, context string) *Client {
	kubeconfig = expandKubeconfigPath(kubeconfig)
	return &Client{
		Kubeconfig: kubeconfig,
		Context:    context,
	}
}

// Apply applies the given multi-document YAML to the cluster using kubectl apply -f -.
func (c *Client) Apply(ctx context.Context, yaml []byte) error {
	args := []string{"apply", "-f", "-"}
	return c.runKubectl(ctx, yaml, args...)
}

// Delete deletes resources described by the given YAML using kubectl delete -f -.
// When ignoreNotFound is true, NotFound errors are ignored via --ignore-not-found.
func (c *Client) Delete(ctx context.Context, yaml []byte, ignoreNotFound bool) error {
	args := []string{"delete", "-f", "-"}
	if ignoreNotFound {
		args = append(args, "--ignore-not-found")
	}
	return c.runKubectl(ctx, yaml, args...)
}

// WaitForDeployments waits until all deployments in the given namespace are Available.
func (c *Client) WaitForDeployments(ctx context.Context, namespace string, timeout string) error {
	if timeout == "" {
		timeout = "1200s"
	}
	args := []string{"wait", "--for=condition=Available", "deployment", "--all", fmt.Sprintf("--timeout=%s", timeout)}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	return c.runKubectl(ctx, nil, args...)
}

// Status prints a simple status view for deployments, services and pods in a namespace.
func (c *Client) Status(ctx context.Context, namespace string, watch bool) error {
	args := []string{"get", "deploy,svc,pods"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	if watch {
		args = append(args, "-w")
	}
	return c.runKubectl(ctx, nil, args...)
}

// RunRaw executes kubectl with the provided arguments and optional stdin payload.
// It is primarily intended for hook implementations.
func (c *Client) RunRaw(ctx context.Context, stdin []byte, args ...string) error {
	return c.runKubectl(ctx, stdin, args...)
}

// RunAndCapture executes kubectl and returns stdout bytes (stderr streamed).
func (c *Client) RunAndCapture(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmdArgs := make([]string, 0, len(args)+4)
	if c.Context != "" {
		cmdArgs = append(cmdArgs, "--context", c.Context)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	cmd.Stderr = os.Stderr

	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	if c.Kubeconfig != "" {
		env := os.Environ()
		env = append(env, "KUBECONFIG="+c.Kubeconfig)
		cmd.Env = env
	}

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl %v failed: %w", args, err)
	}
	return out, nil
}

func (c *Client) runKubectl(ctx context.Context, stdin []byte, args ...string) error {
	cmdArgs := make([]string, 0, len(args)+4)
	if c.Context != "" {
		cmdArgs = append(cmdArgs, "--context", c.Context)
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	cmd.Stdout = os.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	if c.Kubeconfig != "" {
		env := os.Environ()
		env = append(env, "KUBECONFIG="+c.Kubeconfig)
		cmd.Env = env
	}
	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("kubectl %v failed: %w; stderr: %s", args, err, stderr.String())
		}
		return fmt.Errorf("kubectl %v failed: %w", args, err)
	}
	return nil
}

// expandKubeconfigPath expands leading ~ to the user home directory.
func expandKubeconfigPath(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
