// Package kube provides low-level integration with Kubernetes via kubectl and related helpers.
package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Client wraps kubectl execution for in-cluster workloads.
type Client struct {
	// StdoutToStderr redirects kubectl stdout to stderr (useful for machine-readable outputs).
	StdoutToStderr bool
}

// NewClient constructs a new Kubernetes client wrapper.
func NewClient() *Client {
	return &Client{}
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

const defaultWaitTimeout = "600s"

// WaitForDeployments waits until all deployments in the given namespace are Available.
func (c *Client) WaitForDeployments(ctx context.Context, namespace string, timeout string) error {
	if timeout == "" {
		timeout = defaultWaitTimeout
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
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stderr = os.Stderr

	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl %v failed: %w", args, err)
	}
	return out, nil
}

// runKubectl executes kubectl and streams output.
func (c *Client) runKubectl(ctx context.Context, stdin []byte, args ...string) error {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	if c.StdoutToStderr {
		cmd.Stdout = os.Stderr
	} else {
		cmd.Stdout = os.Stdout
	}
	var stderr bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)

	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("kubectl %v failed: %w; stderr: %s", args, err, stderr.String())
		}
		return fmt.Errorf("kubectl %v failed: %w", args, err)
	}
	return nil
}
