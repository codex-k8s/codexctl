// Package hooks contains the hook execution layer used by codexctl.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/kube"
	"github.com/codex-k8s/codexctl/internal/stringsutil"
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

	// Log only the hook step name at info level to avoid noisy multi-line command output.
	// Full command text is available at debug level if needed for troubleshooting.
	e.logger.Info("running hook shell step", "step", step.Name)
	e.logger.Debug("hook shell command", "step", step.Name, "command", cmdText)

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
	case "codex.ensure-data-dirs":
		return e.runEnsureDataDirs(ctx, step, stepCtx)
	case "codex.ensure-codex-secrets":
		return e.runEnsureCodexSecrets(ctx, step, stepCtx)
	case "codex.check-dev-host-ports":
		return e.runCheckDevHostPorts(ctx, step, stepCtx)
	case "codex.reuse-dev-tls-secret":
		return e.runReuseDevTLSSecret(ctx, step, stepCtx)
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
		timeout = "1200s"
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

func (e *Executor) runPreflight(_ context.Context) error {
	// Minimal preflight for hooks: verify that kubectl binary is available.
	_, err := exec.LookPath("kubectl")
	if err != nil {
		return fmt.Errorf("kubectl not found in PATH: %w", err)
	}
	// doctor/advanced preflight logic is implemented separately.
	e.logger.Info("preflight hook: kubectl binary is available")
	return nil
}

// RunPreflightBasic exposes the minimal kubectl presence check for reuse in doctor.
func (e *Executor) RunPreflightBasic(ctx context.Context) error {
	return e.runPreflight(ctx)
}

func (e *Executor) runEnsureDataDirs(_ context.Context, step config.HookStep, stepCtx StepContext) error {
	if stepCtx.Stack == nil {
		return fmt.Errorf("codex.ensure-data-dirs requires stack configuration")
	}
	resolved := config.ResolveDataPaths(stepCtx.Stack)
	if resolved.Root == "" && resolved.EnvDir == "" && len(resolved.Paths) == 0 {
		e.logger.Info("no data paths configured; skipping", "step", step.Name)
		return nil
	}

	dirs := make([]string, 0, len(resolved.Paths)+2)
	if resolved.Root != "" {
		dirs = append(dirs, resolved.Root)
	}
	if resolved.EnvDir != "" {
		dirs = append(dirs, resolved.EnvDir)
	}
	dirs = append(dirs, resolved.Paths...)
	dirs = stringsutil.DedupeStrings(dirs)

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create data dir %q: %w", dir, err)
		}
	}
	for _, dir := range dirs {
		if err := os.Chmod(dir, 0o777); err != nil {
			e.logger.Warn("failed to chmod data dir", "dir", dir, "error", err)
		}
	}
	return nil
}

func (e *Executor) runEnsureCodexSecrets(ctx context.Context, step config.HookStep, stepCtx StepContext) error {
	if stepCtx.KubeClient == nil {
		return fmt.Errorf("codex.ensure-codex-secrets requires a Kubernetes client")
	}
	ns := strings.TrimSpace(stepCtx.Template.Namespace)
	if ns == "" {
		e.logger.Info("namespace is empty, skipping codex secrets", "step", step.Name)
		return nil
	}

	openAI := strings.TrimSpace(stepCtx.Template.EnvMap["OPENAI_API_KEY"])
	context7 := strings.TrimSpace(stepCtx.Template.EnvMap["CONTEXT7_API_KEY"])
	ghToken := strings.TrimSpace(stepCtx.Template.EnvMap["CODEX_GH_PAT"])

	if openAI != "" {
		if err := applyGenericSecret(ctx, stepCtx.KubeClient, ns, "openai-secret", map[string]string{
			"OPENAI_API_KEY": openAI,
		}); err != nil {
			return fmt.Errorf("apply openai-secret: %w", err)
		}
	}

	if ghToken != "" {
		if err := applyGenericSecret(ctx, stepCtx.KubeClient, ns, "github-secret", map[string]string{
			"CODEX_GH_PAT": ghToken,
		}); err != nil {
			return fmt.Errorf("apply github-secret: %w", err)
		}
	} else {
		e.logger.Warn("CODEX_GH_PAT not set, Codex GitHub auth may fail")
	}

	if context7 != "" {
		if err := applyGenericSecret(ctx, stepCtx.KubeClient, ns, "context7-secret", map[string]string{
			"CONTEXT7_API_KEY": context7,
		}); err != nil {
			return fmt.Errorf("apply context7-secret: %w", err)
		}
	}

	return nil
}

func (e *Executor) runCheckDevHostPorts(ctx context.Context, step config.HookStep, stepCtx StepContext) error {
	host := ""
	if raw, ok := step.With["host"]; ok {
		if v, ok := raw.(string); ok {
			host = strings.TrimSpace(v)
		}
	}
	if host == "" {
		base := strings.TrimSpace(stepCtx.Template.BaseDomain["ai"])
		if base != "" && stepCtx.Template.Slot > 0 {
			host = fmt.Sprintf("dev-%d.%s", stepCtx.Template.Slot, base)
		}
	}

	for _, port := range []int{80, 443} {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", addr)
		if err != nil {
			e.logger.Warn("port not reachable on localhost", "port", port, "error", err)
			continue
		}
		_ = conn.Close()
	}

	if host != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/", nil)
		if err != nil {
			e.logger.Warn("failed to build HTTP probe request", "host", host, "error", err)
			return nil
		}
		req.Host = host
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			e.logger.Warn("HTTP probe via ingress failed", "host", host, "error", err)
			return nil
		}
		_ = resp.Body.Close()
	}

	return nil
}

func (e *Executor) runReuseDevTLSSecret(ctx context.Context, step config.HookStep, stepCtx StepContext) error {
	if stepCtx.KubeClient == nil {
		return fmt.Errorf("codex.reuse-dev-tls-secret requires a Kubernetes client")
	}
	ns := strings.TrimSpace(stepCtx.Template.Namespace)
	if ns == "" {
		return nil
	}
	if stepCtx.Template.Slot <= 0 {
		return nil
	}
	project := strings.TrimSpace(stepCtx.Template.Project)
	if project == "" {
		return nil
	}

	secretName := fmt.Sprintf("%s-dev-%d-tls", project, stepCtx.Template.Slot)
	if raw, ok := step.With["secretName"]; ok {
		if v, ok := raw.(string); ok && strings.TrimSpace(v) != "" {
			secretName = strings.TrimSpace(v)
		}
	}

	stagingNS := ""
	if raw, ok := step.With["stagingNamespace"]; ok {
		if v, ok := raw.(string); ok {
			stagingNS = strings.TrimSpace(v)
		}
	}
	if stagingNS == "" {
		stagingNS = resolveNamespaceForEnv(stepCtx.Stack, stepCtx.Template, "staging")
	}
	if stagingNS == "" {
		return nil
	}

	if err := copySecret(ctx, stepCtx.KubeClient, stagingNS, ns, secretName); err != nil {
		e.logger.Warn("failed to copy TLS secret from staging", "secret", secretName, "error", err)
	}

	ready := waitForSecret(ctx, stepCtx.KubeClient, ns, secretName, 120, 2*time.Second)
	if !ready {
		e.logger.Warn("TLS secret not ready yet", "secret", secretName, "namespace", ns)
		return nil
	}

	if err := copySecret(ctx, stepCtx.KubeClient, ns, stagingNS, secretName); err != nil {
		e.logger.Warn("failed to persist TLS secret into staging", "secret", secretName, "error", err)
	}
	return nil
}

func applyGenericSecret(ctx context.Context, client *kube.Client, namespace, name string, data map[string]string) error {
	if client == nil {
		return fmt.Errorf("kubernetes client is nil")
	}
	if namespace == "" || name == "" || len(data) == 0 {
		return nil
	}

	args := []string{"-n", namespace, "create", "secret", "generic", name}
	for k, v := range data {
		if strings.TrimSpace(k) == "" {
			continue
		}
		args = append(args, fmt.Sprintf("--from-literal=%s=%s", k, v))
	}
	args = append(args, "--dry-run=client", "-o", "yaml")

	manifest, err := client.RunAndCapture(ctx, nil, args...)
	if err != nil {
		return err
	}
	return client.RunRaw(ctx, manifest, "-n", namespace, "apply", "-f", "-")
}

func waitForSecret(ctx context.Context, client *kube.Client, namespace, name string, attempts int, delay time.Duration) bool {
	if client == nil || namespace == "" || name == "" {
		return false
	}
	for i := 0; i < attempts; i++ {
		raw, err := client.RunAndCapture(ctx, nil, "-n", namespace, "get", "secret", name, "-o", "json", "--ignore-not-found")
		if err == nil && len(bytes.TrimSpace(raw)) > 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(delay):
		}
	}
	return false
}

func copySecret(ctx context.Context, client *kube.Client, srcNamespace, dstNamespace, name string) error {
	if client == nil {
		return fmt.Errorf("kubernetes client is nil")
	}
	if srcNamespace == "" || dstNamespace == "" || name == "" {
		return nil
	}

	raw, err := client.RunAndCapture(ctx, nil, "-n", srcNamespace, "get", "secret", name, "-o", "json", "--ignore-not-found")
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("decode secret %q: %w", name, err)
	}
	sanitizeSecretMetadata(obj)

	payload, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encode secret %q: %w", name, err)
	}

	return client.RunRaw(ctx, payload, "-n", dstNamespace, "apply", "-f", "-")
}

func sanitizeSecretMetadata(obj map[string]any) {
	if obj == nil {
		return
	}
	if meta, ok := obj["metadata"].(map[string]any); ok {
		delete(meta, "namespace")
		delete(meta, "resourceVersion")
		delete(meta, "uid")
		delete(meta, "creationTimestamp")
		delete(meta, "annotations")
		delete(meta, "ownerReferences")
		delete(meta, "managedFields")
	}
	delete(obj, "status")
}

func resolveNamespaceForEnv(stack *config.StackConfig, baseCtx config.TemplateContext, envName string) string {
	if stack == nil || envName == "" {
		return ""
	}
	ctx := baseCtx
	ctx.Env = envName
	ctx.Namespace = ""
	ns, err := config.ResolveNamespace(stack, ctx, envName)
	if err == nil && strings.TrimSpace(ns) != "" {
		return strings.TrimSpace(ns)
	}
	if strings.TrimSpace(baseCtx.Project) == "" {
		return ""
	}
	switch envName {
	case "staging":
		return fmt.Sprintf("%s-staging", baseCtx.Project)
	case "dev":
		return fmt.Sprintf("%s-dev", baseCtx.Project)
	default:
		return ""
	}
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
