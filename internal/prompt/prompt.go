// Package prompt contains helpers for rendering AI prompts and integrating with Codex agents.
package prompt

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

// Builtin prompt kinds used by Codex integrations.
const (
	PromptKindDevIssue  = "dev_issue"
	PromptKindReviewFix = "review_fix"

	defaultPromptLang  = "en"
	builtinTemplateDir = "templates"
	builtinTemplateExt = ".tmpl"
)

//go:embed templates/*.tmpl
var builtinTemplates embed.FS

// Renderer renders prompt and configuration templates using stack template context.
type Renderer struct {
	stack *config.StackConfig
	ctx   config.TemplateContext
}

// NewRenderer constructs a new Renderer instance for a given stack configuration.
func NewRenderer(stack *config.StackConfig, ctx config.TemplateContext) *Renderer {
	return &Renderer{
		stack: stack,
		ctx:   ctx,
	}
}

// RenderPrompt renders a prompt template file using the stack template context.
// The templatePath can be absolute or project-relative; when relative, it is
// resolved against ctx.ProjectRoot.
func (r *Renderer) RenderPrompt(templatePath string) ([]byte, error) {
	if templatePath == "" {
		return nil, fmt.Errorf("prompt template path is empty")
	}

	path, err := r.resolveProjectPath(templatePath)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prompt template %q: %w", path, err)
	}

	rendered, err := config.RenderTemplate(filepath.Base(path), raw, r.ctx)
	if err != nil {
		return nil, fmt.Errorf("render prompt template %q: %w", path, err)
	}

	return rendered, nil
}

// RenderBuiltinPrompt renders one of the builtin prompt templates identified by kind and language.
// When the requested language is not available, it falls back to English and reports this via
// the usedFallback return value.
func (r *Renderer) RenderBuiltinPrompt(kind, lang string) ([]byte, bool, error) {
	if strings.TrimSpace(kind) == "" {
		return nil, false, fmt.Errorf("builtin prompt kind is empty")
	}

	effectiveLang := strings.ToLower(strings.TrimSpace(lang))
	if effectiveLang == "" {
		effectiveLang = defaultPromptLang
	}

	candidates := []struct {
		file         string
		usedFallback bool
	}{
		{
			file:         builtinTemplatePath(kind, effectiveLang),
			usedFallback: false,
		},
	}
	if effectiveLang != defaultPromptLang {
		candidates = append(candidates, struct {
			file         string
			usedFallback bool
		}{
			file:         builtinTemplatePath(kind, defaultPromptLang),
			usedFallback: true,
		})
	}

	var raw []byte
	var lastErr error
	var chosen string
	for _, candidate := range candidates {
		data, readErr := builtinTemplates.ReadFile(candidate.file)
		if readErr != nil {
			lastErr = readErr
			continue
		}
		raw = data
		chosen = candidate.file
		out, _, renderErr := r.renderBuiltin(chosen, raw)
		if renderErr != nil {
			return nil, false, renderErr
		}
		return out, candidate.usedFallback, nil
	}
	if raw == nil {
		if lastErr == nil {
			lastErr = fmt.Errorf("no builtin prompt template candidates found")
		}
		return nil, false, fmt.Errorf("load builtin prompt kind=%q lang=%q: %w", kind, effectiveLang, lastErr)
	}

	return nil, false, fmt.Errorf("no builtin prompt template selected for kind=%q lang=%q", kind, effectiveLang)
}

// renderBuiltin renders the given builtin template bytes with the stack template context.
func (r *Renderer) renderBuiltin(path string, raw []byte) ([]byte, bool, error) {
	rendered, err := config.RenderTemplate(filepath.Base(path), raw, r.ctx)
	if err != nil {
		return nil, false, fmt.Errorf("render builtin prompt %q: %w", path, err)
	}
	// The caller is responsible for tracking whether a fallback language was used.
	return rendered, false, nil
}

// CodexConfigTemplatePath returns the configured Codex config template path
// from the loaded stack configuration, if any.
func (r *Renderer) CodexConfigTemplatePath() string {
	if r.stack == nil {
		return ""
	}
	return r.stack.Codex.ConfigTemplate
}

// RenderCodexConfig renders the Codex config template defined in services.yaml
// under codex.configTemplate. It returns an error when no template is configured.
func (r *Renderer) RenderCodexConfig() ([]byte, error) {
	path := r.CodexConfigTemplatePath()
	if path == "" {
		return nil, fmt.Errorf("codex.configTemplate is not configured in services.yaml")
	}

	resolved, err := r.resolveProjectPath(path)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read Codex config template %q: %w", resolved, err)
	}

	rendered, err := config.RenderTemplate(filepath.Base(resolved), raw, r.ctx)
	if err != nil {
		return nil, fmt.Errorf("render Codex config template %q: %w", resolved, err)
	}

	return rendered, nil
}

// resolveProjectPath resolves a path against the project root when it is relative.
func (r *Renderer) resolveProjectPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}

	root := r.ctx.ProjectRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve project root: %w", err)
		}
	}
	return filepath.Join(root, path), nil
}

// builtinTemplatePath constructs the embedded template path for a given kind and language.
func builtinTemplatePath(kind, lang string) string {
	base := strings.ToLower(strings.TrimSpace(kind))
	locale := strings.ToLower(strings.TrimSpace(lang))
	filename := base + "_" + locale + builtinTemplateExt
	return filepath.ToSlash(filepath.Join(builtinTemplateDir, filename))
}
