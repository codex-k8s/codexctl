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
	"github.com/codex-k8s/codexctl/internal/promptctx"
)

// StackConfig represents the high-level description of a deployable stack.
// It mirrors the structure of services.yaml after template rendering.
type StackConfig struct {
	// Project is the short project name used in namespaces and defaults.
	Project string `yaml:"project"`
	// EnvFiles lists .env files to load before rendering.
	EnvFiles []string `yaml:"envFiles,omitempty"`
	// Codex contains Codex-specific integration settings.
	Codex CodexConfig `yaml:"codex,omitempty"`
	// Namespace defines namespace naming patterns by environment.
	Namespace *NamespaceBlock `yaml:"namespace,omitempty"`
	// MaxSlots sets the default maximum number of environment slots.
	MaxSlots int `yaml:"maxSlots,omitempty"`
	// Registry is the default container registry for images.
	Registry string `yaml:"registry,omitempty"`
	// Images contains image definitions keyed by name.
	Images map[string]ImageSpec `yaml:"images,omitempty"`
	// BaseDomain maps environment name to base domain.
	BaseDomain map[string]string `yaml:"baseDomain,omitempty"`
	// Environments contains Kubernetes settings per environment.
	Environments map[string]Environment `yaml:"environments,omitempty"`
	// Infrastructure lists infra manifests applied before services.
	Infrastructure []InfraItem `yaml:"infrastructure,omitempty"`
	// Services lists service manifests and overlays.
	Services []Service `yaml:"services,omitempty"`
	// Hooks defines global hook steps around stack operations.
	Hooks HookSet `yaml:"hooks,omitempty"`
	// Storage defines PVC settings for workspace/data/registry.
	Storage *StorageConfig `yaml:"storage,omitempty"`
	// State describes how slot state is stored.
	State StateConfig `yaml:"state,omitempty"`
	// Versions provides named version strings available in templates.
	Versions map[string]string `yaml:"versions,omitempty"`
}

// CodexConfig describes Codex-specific configuration for a project.
// It allows projects to specify locations of Codex configuration templates and
// other integration-related settings without hardcoding them in the tool.
type CodexConfig struct {
	// ConfigTemplate is the path to a Codex config template (e.g. config.toml)
	// relative to the project root. The template is rendered with the same
	// template context as services.yaml.
	ConfigTemplate string `yaml:"configTemplate,omitempty"`
	// PromptLang defines the default prompt language (e.g. "en" or "ru").
	PromptLang string `yaml:"promptLang,omitempty"`
	// Model is the default model identifier used by Codex.
	Model string `yaml:"model,omitempty"`
	// ModelReasoningEffort configures the reasoning effort for the Codex model.
	ModelReasoningEffort string `yaml:"modelReasoningEffort,omitempty"`
	// Links defines project-specific helpful links for environment comments (e.g., Swagger, Admin panels).
	Links []Link `yaml:"links,omitempty"`
	// ExtraTools is an optional list of additional tools available in the
	// project environment (beyond the core toolset like kubectl/gh/curl/rg/jq/bash/Python3).
	// Additional tools must be installed in the project environment separately (in the codex docker image)
	ExtraTools []string `yaml:"extraTools,omitempty"`
	// ProjectContext is an optional free-form text block that describes
	// project-specific context (documentation, special entrypoints, URLs, etc.)
	// and can be injected into builtin prompts.
	ProjectContext string `yaml:"projectContext,omitempty"`
	// ServicesOverview is an optional free-form text block that describes
	// available infrastructure and application services with their URLs/ports.
	// It is intended to be rendered into prompts so that agents know what
	// services and endpoints are reachable.
	ServicesOverview string `yaml:"servicesOverview,omitempty"`
	// Timeouts configures operation timeouts related to Codex flows, such as
	// prompt execution and rollout waits.
	Timeouts CodexTimeouts `yaml:"timeouts,omitempty"`
}

// CodexTimeouts holds string-form durations for Codex-related operations.
// Empty values fall back to built-in defaults in cli/prompt commands.
type CodexTimeouts struct {
	// Exec is the overall timeout for a "prompt run" execution (e.g. "60m").
	Exec string `yaml:"exec,omitempty"`
	// Rollout is the timeout passed to "kubectl rollout status" (e.g. "1200s").
	Rollout string `yaml:"rollout,omitempty"`
	// DeployWait is the timeout used for "kubectl wait" after applying manifests.
	DeployWait string `yaml:"deployWait,omitempty"`
}

// Link describes a named link to expose in comments/UI (title + path).
type Link struct {
	// Title is the human-readable link label.
	Title string `yaml:"title,omitempty"`
	// Path is the path portion to append to the base host.
	Path string `yaml:"path,omitempty"`
}

// NamespaceBlock describes namespace patterns per environment.
type NamespaceBlock struct {
	// Patterns maps environment name to a namespace template.
	Patterns map[string]string `yaml:"patterns,omitempty"`
}

// Environment describes environment-level Kubernetes connection and behavior.
type Environment struct {
	// Kubeconfig is the path to the kubeconfig file to use.
	Kubeconfig string `yaml:"kubeconfig,omitempty"`
	// Context selects the kubeconfig context name.
	Context string `yaml:"context,omitempty"`
	// From references another environment to inherit from.
	From string `yaml:"from,omitempty"`
	// ImagePullPolicy overrides the default pull policy for workloads.
	ImagePullPolicy string `yaml:"imagePullPolicy,omitempty"`
	// LocalRegistry configures an optional local registry for dev.
	LocalRegistry *LocalRegistrySpec `yaml:"localRegistry,omitempty"`
}

// StorageConfig describes PVC settings for shared storage.
type StorageConfig struct {
	// Workspace defines the PVC for shared source workspaces.
	Workspace *PVCSpec `yaml:"workspace,omitempty"`
	// Data defines the PVC for stateful service data.
	Data *PVCSpec `yaml:"data,omitempty"`
	// Registry defines the PVC for the registry storage.
	Registry *PVCSpec `yaml:"registry,omitempty"`
}

// PVCSpec describes a PersistentVolumeClaim template.
type PVCSpec struct {
	// Size is the requested storage size (e.g. "20Gi").
	Size string `yaml:"size,omitempty"`
	// StorageClass sets the storage class name.
	StorageClass string `yaml:"storageClass,omitempty"`
	// AccessModes sets PVC access modes (e.g. ["ReadWriteMany"]).
	AccessModes []string `yaml:"accessModes,omitempty"`
}

// LocalRegistrySpec describes a local image registry used in development setups.
type LocalRegistrySpec struct {
	// Enabled toggles the local registry integration.
	Enabled bool `yaml:"enabled"`
	// Name is the registry name/host.
	Name string `yaml:"name,omitempty"`
	// Port is the registry port when running locally.
	Port int `yaml:"port,omitempty"`
}

// ImageSpec describes an image declared in the top-level images block.
// It is intended for shared metadata such as mirroring external images into a local registry.
type ImageSpec struct {
	// Type describes the image kind, e.g. "external" (pulled from a remote registry)
	// or "build" (built from a Dockerfile).
	Type string `yaml:"type,omitempty"`
	// From is the full remote image reference for external images
	// (e.g. "docker.io/library/busybox:1.37.0").
	From string `yaml:"from,omitempty"`
	// Local is the full local image reference in the development registry
	// (e.g. "localhost:5000/library/busybox:1.37.0").
	Local string `yaml:"local,omitempty"`
	// Repository is the base image repository (e.g. "localhost:5000/your-project/django-backend")
	// used primarily for build images.
	Repository string `yaml:"repository,omitempty"`
	// Tag is an optional explicit tag string for build images when templating is not needed.
	Tag string `yaml:"tag,omitempty"`
	// TagTemplate is an optional Go-template string for computing a tag based on the template context.
	TagTemplate string `yaml:"tagTemplate,omitempty"`
	// Dockerfile is an optional path to a Dockerfile (relative to project root) for build images.
	Dockerfile string `yaml:"dockerfile,omitempty"`
	// Context is an optional build context path (relative to project root) for build images.
	Context string `yaml:"context,omitempty"`
	// BuildArgs defines additional docker build arguments (key -> value template).
	BuildArgs map[string]string `yaml:"buildArgs,omitempty"`
	// BuildContexts defines additional docker build contexts (name -> path template).
	BuildContexts map[string]string `yaml:"buildContexts,omitempty"`
}

// InfraItem groups infrastructure manifests applied before services.
type InfraItem struct {
	// Name is the logical infra block name.
	Name string `yaml:"name"`
	// Manifests lists YAML manifests to apply for this infra block.
	Manifests []ManifestRef `yaml:"manifests"`
	// When is a template expression that enables this infra block.
	When string `yaml:"when,omitempty"`
	// Hooks contains infra-specific hook steps.
	Hooks ResourceHooks `yaml:"hooks,omitempty"`
}

// ManifestRef points to a YAML manifest file relative to the project root.
type ManifestRef struct {
	// Path is the manifest file path relative to the project root.
	Path string `yaml:"path"`
}

// Service describes a single logical service in the stack.
type Service struct {
	// Name is the service identifier used in manifests and overlays.
	Name string `yaml:"name"`
	// Manifests lists YAML manifests for this service.
	Manifests []ManifestRef `yaml:"manifests"`
	// Image describes how to compute the container image reference.
	Image ServiceImage `yaml:"image,omitempty"`
	// Ingress defines host mapping for the service.
	Ingress *ServiceIngress `yaml:"ingress,omitempty"`
	// When is a template expression that enables this service.
	When string `yaml:"when,omitempty"`
	// Overlays contains per-environment overrides.
	Overlays map[string]Overlay `yaml:"overlays,omitempty"`
	// Hooks contains service-specific hook steps.
	Hooks ResourceHooks `yaml:"hooks,omitempty"`
}

// ServiceImage describes how to construct the container image for a service.
// Tag is treated as a template string (tagTemplate) that can contain Go-template expressions.
type ServiceImage struct {
	// Repository is the image repository (e.g. "org/service").
	Repository string `yaml:"repository"`
	// Tag is a Go-template string that resolves to an image tag.
	Tag string `yaml:"tagTemplate"`
}

// ServiceIngress describes host mapping per environment for a service.
type ServiceIngress struct {
	// Hosts maps environment name to host.
	Hosts map[string]string `yaml:"hosts,omitempty"`
}

// Overlay describes per-environment overrides for a service.
type Overlay struct {
	// PVCMounts inject persistent volume claim mounts into workloads.
	PVCMounts []PVCMount `yaml:"pvcMounts,omitempty"`
	// DropKinds lists Kubernetes kinds to exclude for the environment.
	DropKinds []string `yaml:"dropKinds,omitempty"`
}

// PVCMount describes a persistent volume claim mount injected into workloads.
type PVCMount struct {
	// Name is the volume name to create.
	Name string `yaml:"name"`
	// ClaimName is the PVC name to mount.
	ClaimName string `yaml:"claimName"`
	// MountPath is the container path to mount into.
	MountPath string `yaml:"mountPath"`
	// SubPath is an optional path within the PVC to mount.
	SubPath string `yaml:"subPath,omitempty"`
	// ReadOnly toggles read-only mounts.
	ReadOnly bool `yaml:"readOnly,omitempty"`
}

// StateConfig describes how environment state (slots, metadata) is stored.
// For the initial implementation, only a ConfigMap-based backend is supported.
type StateConfig struct {
	Backend            string `yaml:"backend,omitempty"`            // e.g. "configmap"
	ConfigMapNamespace string `yaml:"configmapNamespace,omitempty"` // namespace where state ConfigMaps live
	ConfigMapPrefix    string `yaml:"configmapPrefix,omitempty"`    // prefix for state ConfigMap names
}

// HookSet describes global hooks executed around stack operations.
type HookSet struct {
	// BeforeAll runs before any apply/destroy operations.
	BeforeAll []HookStep `yaml:"beforeAll,omitempty"`
	// AfterAll runs after all apply/destroy operations.
	AfterAll []HookStep `yaml:"afterAll,omitempty"`
}

// ResourceHooks describes hooks bound to a particular infrastructure item or service.
type ResourceHooks struct {
	// BeforeApply runs before applying resource manifests.
	BeforeApply []HookStep `yaml:"beforeApply,omitempty"`
	// AfterApply runs after applying resource manifests.
	AfterApply []HookStep `yaml:"afterApply,omitempty"`
	// BeforeDestroy runs before deleting resource manifests.
	BeforeDestroy []HookStep `yaml:"beforeDestroy,omitempty"`
	// AfterDestroy runs after deleting resource manifests.
	AfterDestroy []HookStep `yaml:"afterDestroy,omitempty"`
}

// HookStep describes a single hook execution step.
// It can either run a shell command or invoke a built-in action via Use.
type HookStep struct {
	// Name is the identifier used in logs.
	Name string `yaml:"name,omitempty"`
	// Run is a shell command template to execute.
	Run string `yaml:"run,omitempty"`
	// Use selects a built-in hook implementation.
	Use string `yaml:"use,omitempty"`
	// With provides parameters to built-in hooks.
	With map[string]any `yaml:"with,omitempty"`
	// When is a template expression that enables the hook.
	When string `yaml:"when,omitempty"`
	// ContinueOnError skips failures when set.
	ContinueOnError bool `yaml:"continueOnError,omitempty"`
	// Timeout is a duration string for the hook execution.
	Timeout string `yaml:"timeout,omitempty"`
}

// LoadOptions describes parameters that influence template rendering of services.yaml.
type LoadOptions struct {
	// Env is the target environment name.
	Env string
	// Namespace overrides the derived namespace.
	Namespace string
	// Slot is the slot number for slot-based envs.
	Slot int
	// UserVars are inline variables for template rendering.
	UserVars env.Vars
	// VarFiles lists additional var-files to load.
	VarFiles []string
}

// TemplateContext represents the data exposed to Go-templates when rendering services.yaml
// and other project templates (manifests, prompts, Codex config).
type TemplateContext struct {
	// Env is the selected environment name.
	Env string
	// Namespace is the resolved or overridden namespace.
	Namespace string
	// Project is the project identifier.
	Project string
	// ProjectRoot is the path to the project root on disk.
	ProjectRoot string
	// Slot is the slot number used for ai environments.
	Slot int
	// Now is the timestamp captured for template rendering.
	Now time.Time
	// UserVars contains inline user variables.
	UserVars env.Vars
	// EnvMap merges OS env, envFiles, and user variables.
	EnvMap env.Vars
	// Versions contains version strings from services.yaml.
	Versions map[string]string
	// BaseDomain maps environment name to base domain.
	BaseDomain map[string]string
	// Codex provides Codex-specific configuration for templates.
	Codex CodexConfig
	// Storage provides PVC configuration for templates.
	Storage *StorageConfig
	// IssueComments contains related GitHub issue comments.
	IssueComments []promptctx.IssueComment
	// ReviewComments contains related PR review comments.
	ReviewComments []promptctx.ReviewComment
}

// rawHeader is a minimal struct used to extract top-level fields before templating.
type rawHeader struct {
	Project    string            `yaml:"project"`
	EnvFiles   []string          `yaml:"envFiles"`
	Versions   map[string]string `yaml:"versions"`
	BaseDomain map[string]string `yaml:"baseDomain"`
	Namespace  struct {
		Patterns map[string]string `yaml:"patterns"`
	} `yaml:"namespace"`
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
		Versions:    header.Versions,
		BaseDomain:  header.BaseDomain,
	}

	if strings.TrimSpace(ctx.Namespace) == "" && ctx.Env != "" {
		if header.Namespace.Patterns != nil {
			if pattern, ok := header.Namespace.Patterns[ctx.Env]; ok && strings.TrimSpace(pattern) != "" {
				rendered, err := RenderTemplate("namespace", []byte(pattern), ctx)
				if err != nil {
					return nil, zeroCtx, fmt.Errorf("render namespace pattern for env %q: %w", ctx.Env, err)
				}
				if ns := strings.TrimSpace(string(rendered)); ns != "" {
					ctx.Namespace = ns
				}
			}
		}
		if ctx.Namespace == "" && ctx.Env == "ai" && ctx.Project != "" && ctx.Slot > 0 {
			ctx.Namespace = fmt.Sprintf("%s-dev-%d", ctx.Project, ctx.Slot)
		}
		if ctx.Namespace == "" && ctx.Env == "ai-repair" && ctx.Project != "" {
			ctx.Namespace = fmt.Sprintf("%s-ai-staging", ctx.Project)
		}
		if ctx.Namespace == "" && ctx.Project != "" && ctx.Env != "ai" && ctx.Env != "ai-repair" {
			ctx.Namespace = fmt.Sprintf("%s-%s", ctx.Project, ctx.Env)
		}
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

	ctx.Versions = cfg.Versions
	ctx.BaseDomain = cfg.BaseDomain
	ctx.Codex = cfg.Codex
	ctx.Storage = cfg.Storage

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
		"default":    funcDef,
		"toLower":    strings.ToLower,
		"slug":       funcSlug,
		"truncSHA":   funcTruncSHA,
		"envOr":      funcEnvOr(ctx.EnvMap),
		"ternary":    funcTernary,
		"now":        func() time.Time { return ctx.Now },
		"join":       funcJoin,
		"trimPrefix": funcTrimPrefix,
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

// funcTruncSHA truncates an SHA-like string to a shorter length for display.
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

// funcJoin joins a slice of strings with the given separator.
func funcJoin(values []string, sep string) string {
	return strings.Join(values, sep)
}

// funcTrimPrefix removes the prefix from value when present.
func funcTrimPrefix(value, prefix string) string {
	return strings.TrimPrefix(value, prefix)
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
	// For ai environments, derive namespace directly from project and slot
	// to ensure stable mapping: <project>-dev-<slot>.
	if envName == "ai" && ctx.Project != "" && ctx.Slot > 0 {
		return fmt.Sprintf("%s-dev-%d", ctx.Project, ctx.Slot), nil
	}
	// For ai-repair environments, reuse the ai-staging namespace.
	if envName == "ai-repair" && ctx.Project != "" {
		return fmt.Sprintf("%s-ai-staging", ctx.Project), nil
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
