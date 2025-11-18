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

// StackConfig represents the high-level description of a deployable stack.
// It mirrors the structure of services.yaml after template rendering.
type StackConfig struct {
	Project        string                 `yaml:"project"`
	EnvFiles       []string               `yaml:"envFiles,omitempty"`
	Namespace      *NamespaceBlock        `yaml:"namespace,omitempty"`
	MaxSlots       int                    `yaml:"maxSlots,omitempty"`
	Registry       string                 `yaml:"registry,omitempty"`
	BaseDomain     map[string]string      `yaml:"baseDomain,omitempty"`
	Environments   map[string]Environment `yaml:"environments,omitempty"`
	Infrastructure []InfraItem            `yaml:"infrastructure,omitempty"`
	Services       []Service              `yaml:"services,omitempty"`
}

// NamespaceBlock describes namespace patterns per environment.
type NamespaceBlock struct {
	Patterns map[string]string `yaml:"patterns,omitempty"`
}

// Environment describes environment-level Kubernetes connection and behavior.
type Environment struct {
	Kubeconfig      string             `yaml:"kubeconfig,omitempty"`
	Context         string             `yaml:"context,omitempty"`
	From            string             `yaml:"from,omitempty"`
	ImagePullPolicy string             `yaml:"imagePullPolicy,omitempty"`
	LocalRegistry   *LocalRegistrySpec `yaml:"localRegistry,omitempty"`
}

// LocalRegistrySpec describes a local image registry used in development setups.
type LocalRegistrySpec struct {
	Enabled bool   `yaml:"enabled"`
	Name    string `yaml:"name,omitempty"`
	Port    int    `yaml:"port,omitempty"`
}

// InfraItem groups infrastructure manifests applied before services.
type InfraItem struct {
	Name      string        `yaml:"name"`
	Manifests []ManifestRef `yaml:"manifests"`
}

// ManifestRef points to a YAML manifest file relative to the project root.
type ManifestRef struct {
	Path string `yaml:"path"`
}

// Service describes a single logical service in the stack.
type Service struct {
	Name      string             `yaml:"name"`
	Manifests []ManifestRef      `yaml:"manifests"`
	Image     ServiceImage       `yaml:"image,omitempty"`
	Ingress   *ServiceIngress    `yaml:"ingress,omitempty"`
	Overlays  map[string]Overlay `yaml:"overlays,omitempty"`
}

// ServiceImage describes how to construct the container image for a service.
// Tag is treated as a template string (tagTemplate) that can contain Go-template expressions.
type ServiceImage struct {
	Repository string `yaml:"repository"`
	Tag        string `yaml:"tagTemplate"`
}

// ServiceIngress describes host mapping per environment for a service.
type ServiceIngress struct {
	Hosts map[string]string `yaml:"hosts,omitempty"`
}

// Overlay describes per-environment overrides for a service.
type Overlay struct {
	HostMounts []HostMount `yaml:"hostMounts,omitempty"`
	DropKinds  []string    `yaml:"dropKinds,omitempty"`
}

// HostMount describes a hostPath mount injected into workloads.
type HostMount struct {
	Name      string `yaml:"name"`
	HostPath  string `yaml:"hostPath"`
	MountPath string `yaml:"mountPath"`
}

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
	Env         string
	Namespace   string
	Project     string
	ProjectRoot string
	Slot        int
	Now         time.Time
	UserVars    env.Vars
	EnvMap      env.Vars
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
		Env:         opts.Env,
		Namespace:   opts.Namespace,
		Project:     header.Project,
		ProjectRoot: baseDir,
		Slot:        opts.Slot,
		Now:         time.Now().UTC(),
		UserVars:    opts.UserVars,
		EnvMap:      envMap,
	}

	rendered, err := executeTemplate(rawBytes, ctx)
	if err != nil {
		return nil, zeroCtx, err
	}

	return rendered, ctx, nil
}

// LoadStackConfig loads, templates and parses services.yaml into StackConfig and TemplateContext.
func LoadStackConfig(path string, opts LoadOptions) (*StackConfig, TemplateContext, error) {
	rendered, ctx, err := LoadAndRender(path, opts)
	if err != nil {
		return nil, TemplateContext{}, err
	}

	var cfg StackConfig
	if err := yaml.Unmarshal(rendered, &cfg); err != nil {
		return nil, TemplateContext{}, fmt.Errorf("parse rendered services.yaml: %w", err)
	}

	ns, err := ResolveNamespace(&cfg, ctx, opts.Env)
	if err != nil {
		return nil, TemplateContext{}, err
	}
	if ns != "" {
		ctx.Namespace = ns
	}

	return &cfg, ctx, nil
}

// executeTemplate renders the given YAML content using the stack template context.
func executeTemplate(raw []byte, ctx TemplateContext) ([]byte, error) {
	funcs := buildFuncMap(ctx)

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

// RenderTemplate renders arbitrary YAML or text content using the same template context and helpers.
func RenderTemplate(name string, raw []byte, ctx TemplateContext) ([]byte, error) {
	funcs := buildFuncMap(ctx)

	tmpl, err := template.New(name).Funcs(funcs).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("execute template %q: %w", name, err)
	}
	return buf.Bytes(), nil
}

// buildFuncMap constructs the common set of template functions available in services.yaml and manifests.
func buildFuncMap(ctx TemplateContext) template.FuncMap {
	return template.FuncMap{
		"default":  funcDef,
		"toLower":  strings.ToLower,
		"slug":     funcSlug,
		"truncSHA": funcTruncSHA,
		"envOr":    funcEnvOr(ctx.EnvMap),
		"ternary":  funcTernary,
		"now":      func() time.Time { return ctx.Now },
	}
}

// funcDef returns def when value is empty or whitespace, otherwise value.
func funcDef(value, def string) string {
	if strings.TrimSpace(value) == "" {
		return def
	}
	return value
}

// funcSlug normalizes a value into a lower-case dash-separated slug.
func funcSlug(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, " ", "-")
	v = strings.ReplaceAll(v, "_", "-")
	return v
}

// funcTruncSHA truncates a SHA-like string to a shorter length for display.
func funcTruncSHA(s string) string {
	const max = 12
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// funcEnvOr returns a function that looks up a key in envMap and falls back to def.
func funcEnvOr(envMap env.Vars) func(key, def string) string {
	return func(key, def string) string {
		if v, ok := envMap[key]; ok && v != "" {
			return v
		}
		return def
	}
}

// funcTernary returns a when cond is true, otherwise b.
func funcTernary(cond bool, a, b any) any {
	if cond {
		return a
	}
	return b
}

// ResolveEnvironment returns the effective environment configuration for the given name,
// following optional "from" links and applying overrides.
func ResolveEnvironment(cfg *StackConfig, name string) (Environment, error) {
	if cfg == nil {
		return Environment{}, fmt.Errorf("stack config is nil")
	}

	visited := make(map[string]struct{})
	var resolve func(current string) (Environment, error)

	resolve = func(current string) (Environment, error) {
		if _, seen := visited[current]; seen {
			return Environment{}, fmt.Errorf("environment inheritance cycle detected at %q", current)
		}
		visited[current] = struct{}{}

		envCfg, ok := cfg.Environments[current]
		if !ok {
			return Environment{}, fmt.Errorf("environment %q not defined in services.yaml", current)
		}

		if envCfg.From == "" {
			return envCfg, nil
		}

		base, err := resolve(envCfg.From)
		if err != nil {
			return Environment{}, err
		}

		merged := base
		if envCfg.Kubeconfig != "" {
			merged.Kubeconfig = envCfg.Kubeconfig
		}
		if envCfg.Context != "" {
			merged.Context = envCfg.Context
		}
		if envCfg.ImagePullPolicy != "" {
			merged.ImagePullPolicy = envCfg.ImagePullPolicy
		}
		if envCfg.LocalRegistry != nil {
			merged.LocalRegistry = envCfg.LocalRegistry
		}
		return merged, nil
	}

	return resolve(name)
}

// ResolveNamespace derives the namespace for the given environment using namespace patterns
// and the current template context when explicit ctx.Namespace is empty.
func ResolveNamespace(cfg *StackConfig, ctx TemplateContext, envName string) (string, error) {
	if ctx.Namespace != "" {
		return ctx.Namespace, nil
	}
	if cfg == nil || cfg.Namespace == nil {
		return "", nil
	}
	if cfg.Namespace.Patterns == nil {
		return "", nil
	}

	pattern, ok := cfg.Namespace.Patterns[envName]
	if !ok || strings.TrimSpace(pattern) == "" {
		return "", nil
	}

	rendered, err := RenderTemplate("namespace", []byte(pattern), ctx)
	if err != nil {
		return "", fmt.Errorf("render namespace pattern for env %q: %w", envName, err)
	}
	ns := strings.TrimSpace(string(rendered))
	return ns, nil
}
