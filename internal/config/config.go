// Package config contains the loader and strongly typed model for services.yaml.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/codex-k8s/codexctl/internal/env"
)

// StackConfig is a placeholder for the future stack configuration model.
// It will represent project, environments, infrastructure groups and services.
type StackConfig struct{}

// LoadOptions describes parameters that influence template rendering of services.yaml.
type LoadOptions struct {
	Env       string
	Namespace string
	Slot      int
	UserVars  env.Vars
	VarFiles  []string
}

// TemplateContext represents the data exposed to Go-templates when rendering services.yaml.
type TemplateContext struct {
	Env       string
	Namespace string
	Project   string
	Slot      int
	Now       time.Time
	UserVars  env.Vars
	EnvMap    env.Vars
}

// rawHeader is a minimal struct used to extract top-level fields before templating.
type rawHeader struct {
	Project  string   `yaml:"project"`
	EnvFiles []string `yaml:"envFiles"`
}

// LoadAndRender reads services.yaml, loads envFiles and user vars, and returns rendered YAML bytes
// together with the template context that was used.
func LoadAndRender(path string, opts LoadOptions) ([]byte, TemplateContext, error) {
	var zeroCtx TemplateContext

	if path == "" {
		return nil, zeroCtx, fmt.Errorf("config path is empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, zeroCtx, fmt.Errorf("resolve config path: %w", err)
	}

	rawBytes, err := os.ReadFile(absPath)
	if err != nil {
		return nil, zeroCtx, fmt.Errorf("read config %q: %w", absPath, err)
	}

	var header rawHeader
	if err := yaml.Unmarshal(rawBytes, &header); err != nil {
		return nil, zeroCtx, fmt.Errorf("parse top-level config fields: %w", err)
	}

	baseDir := filepath.Dir(absPath)
	osVars := env.FromOS()

	envFileVars, err := env.LoadEnvFiles(baseDir, header.EnvFiles)
	if err != nil {
		return nil, zeroCtx, err
	}

	varFileVars := make(env.Vars)
	for _, vf := range opts.VarFiles {
		if strings.TrimSpace(vf) == "" {
			continue
		}
		vp, err := env.LoadVarFile(vf)
		if err != nil {
			return nil, zeroCtx, fmt.Errorf("load var-file %q: %w", vf, err)
		}
		varFileVars = env.Merge(varFileVars, vp)
	}

	envMap := env.Merge(osVars, envFileVars, varFileVars, opts.UserVars)

	ctx := TemplateContext{
		Env:       opts.Env,
		Namespace: opts.Namespace,
		Project:   header.Project,
		Slot:      opts.Slot,
		Now:       time.Now().UTC(),
		UserVars:  opts.UserVars,
		EnvMap:    envMap,
	}

	rendered, err := executeTemplate(rawBytes, ctx)
	if err != nil {
		return nil, zeroCtx, err
	}

	return rendered, ctx, nil
}

func executeTemplate(raw []byte, ctx TemplateContext) ([]byte, error) {
	funcs := template.FuncMap{
		"default":  funcDef,
		"toLower":  strings.ToLower,
		"slug":     funcSlug,
		"truncSHA": funcTruncSHA,
		"envOr":    funcEnvOr(ctx.EnvMap),
		"ternary":  funcTernary,
		"now":      func() time.Time { return ctx.Now },
	}

	tmpl, err := template.New("services.yaml").Funcs(funcs).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	return buf.Bytes(), nil
}

func funcDef(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

func funcSlug(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, " ", "-")
	v = strings.ReplaceAll(v, "_", "-")
	return v
}

func funcTruncSHA(s string) string {
	const max = 12
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func funcEnvOr(envMap env.Vars) func(key, def string) string {
	return func(key, def string) string {
		if v, ok := envMap[key]; ok && v != "" {
			return v
		}
		return def
	}
}

func funcTernary(cond bool, a, b any) any {
	if cond {
		return a
	}
	return b
}
