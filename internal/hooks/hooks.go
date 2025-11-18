// Package hooks contains the hook execution layer used by codexctl.
package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/kube"
)

// Executor executes hook steps defined in services.yaml.
type Executor struct {
	logger *slog.Logger
}

// StepContext provides runtime information available during hook execution.
type StepContext struct {
	Stack      *config.StackConfig
	Template   config.TemplateContext
	EnvName    string
	KubeClient *kube.Client
}

// NewExecutor constructs a new Executor instance with the given logger.
func NewExecutor(logger *slog.Logger) *Executor {
	return &Executor{
		logger: logger,
	}
}

// RunSteps executes the provided hook steps sequentially using the given context.
func (e *Executor) RunSteps(ctx context.Context, steps []config.HookStep, stepCtx StepContext) error {
	for _, step := range steps {
		if err := e.runStep(ctx, step, stepCtx); err != nil {
			if step.ContinueOnError {
				e.logger.Warn("hook step failed but continueOnError is true", "step", step.Name, "error", err)
				continue
			}
			return err
		}
	}
	return nil
}

func (e *Executor) runStep(parentCtx context.Context, step config.HookStep, stepCtx StepContext) error {
	if strings.TrimSpace(step.When) != "" {
		ok, err := e.evaluateWhen(step.When, stepCtx.Template)
		if err != nil {
			return fmt.Errorf("evaluate hook when expression for %q: %w", step.Name, err)
		}
		if !ok {
			e.logger.Debug("skipping hook step due to when=false", "step", step.Name)
			return nil
		}
	}

	ctx := parentCtx
	if strings.TrimSpace(step.Timeout) != "" {
		d, err := time.ParseDuration(step.Timeout)
		if err != nil {
			return fmt.Errorf("parse hook timeout %q for %q: %w", step.Timeout, step.Name, err)
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parentCtx, d)
		defer cancel()
	}

	switch {
	case strings.TrimSpace(step.Run) != "":
		return e.runShell(ctx, step, stepCtx)
	case strings.TrimSpace(step.Use) != "":
		return e.runBuiltin(ctx, step, stepCtx)
	default:
		e.logger.Debug("hook step has neither run nor use, skipping", "step", step.Name)
		return nil
	}
}

func (e *Executor) evaluateWhen(expr string, tmplCtx config.TemplateContext) (bool, error) {
	rendered, err := config.RenderTemplate("when", []byte(expr), tmplCtx)
	if err != nil {
		return false, err
	}
	s := strings.TrimSpace(string(rendered))
	if s == "" {
		return false, nil
	}
	ls := strings.ToLower(s)
	if ls == "false" || ls == "0" || ls == "no" {
		return false, nil
	}
	return true, nil
}

func (e *Executor) runShell(ctx context.Context, step config.HookStep, stepCtx StepContext) error {
	cmdTextBytes, err := config.RenderTemplate("hook-run", []byte(step.Run), stepCtx.Template)
	if err != nil {
		return fmt.Errorf("render hook run template for %q: %w", step.Name, err)
	}
	cmdText := strings.TrimSpace(string(cmdTextBytes))
	if cmdText == "" {
		return nil
	}

	e.logger.Info("running hook shell step", "step", step.Name, "command", cmdText)

	cmd := exec.CommandContext(ctx, "bash", "-lc", cmdText)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook run step %q failed: %w", step.Name, err)
	}
	return nil
}

func (e *Executor) runBuiltin(ctx context.Context, step config.HookStep, stepCtx StepContext) error {
	switch step.Use {
	case "kubectl.wait":
		return e.runKubectlWait(ctx, step, stepCtx)
	case "github.comment":
		return e.runGitHubComment(ctx, step, stepCtx)
	case "sleep":
		return e.runSleep(ctx, step)
	case "preflight":
		return e.runPreflight(ctx)
	default:
		return fmt.Errorf("unknown hook use %q for step %q", step.Use, step.Name)
	}
}

func (e *Executor) runKubectlWait(ctx context.Context, step config.HookStep, stepCtx StepContext) error {
	if stepCtx.KubeClient == nil {
		return fmt.Errorf("kubectl.wait requires a Kubernetes client")
	}

	kind, _ := step.With["kind"].(string)
	name, _ := step.With["name"].(string)
	ns, _ := step.With["namespace"].(string)
	condition, _ := step.With["condition"].(string)
	timeout, _ := step.With["timeout"].(string)

	if kind == "" || name == "" {
		return fmt.Errorf("kubectl.wait requires kind and name in with")
	}
	if condition == "" {
		condition = "Available"
	}
	if timeout == "" {
		timeout = "300s"
	}

	e.logger.Info("running kubectl.wait hook", "step", step.Name, "kind", kind, "name", name, "namespace", ns, "condition", condition, "timeout", timeout)

	args := []string{
		"wait",
		fmt.Sprintf("--for=condition=%s", condition),
		fmt.Sprintf("%s/%s", kind, name),
		fmt.Sprintf("--timeout=%s", timeout),
	}
	if ns != "" {
		args = append(args, "-n", ns)
	}

	return stepCtx.KubeClient.RunRaw(ctx, nil, args...)
}

func (e *Executor) runGitHubComment(ctx context.Context, step config.HookStep, stepCtx StepContext) error {
	token, ok := stepCtx.Template.EnvMap["CODEX_GH_PAT"]
	if !ok || strings.TrimSpace(token) == "" {
		return fmt.Errorf("github.comment requires CODEX_GH_PAT in configuration")
	}
	username := strings.TrimSpace(stepCtx.Template.EnvMap["CODEX_GH_USERNAME"])

	bodyRaw, _ := step.With["body"].(string)
	if strings.TrimSpace(bodyRaw) == "" {
		return fmt.Errorf("github.comment requires body in with")
	}
	bodyBytes, err := config.RenderTemplate("github-comment-body", []byte(bodyRaw), stepCtx.Template)
	if err != nil {
		return fmt.Errorf("render github.comment body for %q: %w", step.Name, err)
	}
	body := strings.TrimSpace(string(bodyBytes))
	if body == "" {
		return fmt.Errorf("github.comment body is empty after rendering")
	}

	var issueNumber, prNumber int
	if v, ok := step.With["issue"]; ok {
		issueNumber, _ = toInt(v)
	}
	if v, ok := step.With["pr"]; ok {
		prNumber, _ = toInt(v)
	}
	if issueNumber == 0 && prNumber == 0 {
		return fmt.Errorf("github.comment requires issue or pr in with")
	}

	var args []string
	if prNumber != 0 {
		args = []string{"pr", "comment", strconv.Itoa(prNumber), "--body", body}
	} else {
		args = []string{"issue", "comment", strconv.Itoa(issueNumber), "--body", body}
	}

	e.logger.Info("running github.comment hook", "step", step.Name, "args", args, "username", username)

	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	envVars := os.Environ()
	// gh CLI accepts both GH_TOKEN and GITHUB_TOKEN; we derive them from CODEX_GH_PAT.
	envVars = append(envVars, "GITHUB_TOKEN="+token)
	envVars = append(envVars, "GH_TOKEN="+token)
	cmd.Env = envVars

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("github.comment step %q failed: %w", step.Name, err)
	}
	return nil
}

func (e *Executor) runSleep(ctx context.Context, step config.HookStep) error {
	raw, _ := step.With["duration"].(string)
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("sleep hook requires duration in with")
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse sleep duration %q: %w", raw, err)
	}
	e.logger.Info("running sleep hook", "step", step.Name, "duration", d.String())
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (e *Executor) runPreflight(ctx context.Context) error {
	// Minimal preflight for hooks: verify that kubectl binary is available.
	_, err := exec.LookPath("kubectl")
	if err != nil {
		return fmt.Errorf("kubectl not found in PATH: %w", err)
	}
	// doctor/advanced preflight logic is implemented separately.
	e.logger.Info("preflight hook: kubectl binary is available")
	return nil
}

func toInt(v any) (int, error) {
	switch t := v.(type) {
	case int:
		return t, nil
	case int64:
		return int(t), nil
	case float64:
		return int(t), nil
	case string:
		t = strings.TrimSpace(t)
		if t == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(t)
		if err != nil {
			return 0, err
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", v)
	}
}
