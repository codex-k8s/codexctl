// Package engine contains the high-level orchestration logic for stack operations.
package engine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/codex-k8s/codexctl/internal/config"
)

// Engine is a placeholder for the high-level orchestration layer.
// It will coordinate rendering, applying, destroying and inspecting stack resources.
type Engine struct{}

// NewEngine constructs a new Engine instance with default dependencies.
func NewEngine() *Engine {
	return &Engine{}
}

// RenderOptions allows filtering infra/services when rendering a stack.
type RenderOptions struct {
	OnlyInfra    map[string]struct{}
	SkipInfra    map[string]struct{}
	OnlyServices map[string]struct{}
	SkipServices map[string]struct{}
}

// RenderStack renders infrastructure and service manifests for the given stack into a single YAML stream.
// The result is a multi-document YAML containing all resources for the selected environment.
func (e *Engine) RenderStack(cfg *config.StackConfig, ctx config.TemplateContext) ([]byte, error) {
	return e.RenderStackWithOptions(cfg, ctx, RenderOptions{})
}

// RenderStackWithOptions renders infrastructure and service manifests for the given stack with filters applied.
func (e *Engine) RenderStackWithOptions(cfg *config.StackConfig, ctx config.TemplateContext, opts RenderOptions) ([]byte, error) {
	var documents []map[string]any

	// Render infrastructure manifests first.
	for _, infra := range cfg.Infrastructure {
		if !resourceIncluded(infra.Name, opts.OnlyInfra, opts.SkipInfra) {
			continue
		}
		ok, err := evaluateInfraWhen(infra.When, ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluate when for infra %q: %w", infra.Name, err)
		}
		if !ok {
			continue
		}
		for _, ref := range infra.Manifests {
			docs, err := e.loadManifestDocuments(ref.Path, ctx)
			if err != nil {
				return nil, fmt.Errorf("render infra %q (%s): %w", infra.Name, ref.Path, err)
			}
			documents = append(documents, docs...)
		}
	}

	// Render service manifests with per-environment overlays.
	for _, svc := range cfg.Services {
		if !resourceIncluded(svc.Name, opts.OnlyServices, opts.SkipServices) {
			continue
		}
		ok, err := evaluateServiceWhen(svc.When, ctx)
		if err != nil {
			return nil, fmt.Errorf("evaluate when for service %q: %w", svc.Name, err)
		}
		if !ok {
			continue
		}
		overlay := svc.Overlays[ctx.Env]
		for _, ref := range svc.Manifests {
			docs, err := e.loadManifestDocuments(ref.Path, ctx)
			if err != nil {
				return nil, fmt.Errorf("render service %q (%s): %w", svc.Name, ref.Path, err)
			}
			for _, doc := range docs {
				if shouldDropKind(doc, overlay.DropKinds) {
					continue
				}
				applyNamespace(doc, ctx.Namespace)
				if err := applyServiceImage(doc, svc, ctx); err != nil {
					return nil, fmt.Errorf("render image for service %q: %w", svc.Name, err)
				}
				applyHostMounts(doc, svc, overlay)
				documents = append(documents, doc)
			}
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, doc := range documents {
		if err := enc.Encode(doc); err != nil {
			_ = enc.Close()
			return nil, fmt.Errorf("encode manifest: %w", err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("finalize manifest stream: %w", err)
	}

	return buf.Bytes(), nil
}

func resourceIncluded(name string, only, skip map[string]struct{}) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	if len(only) > 0 {
		if _, ok := only[key]; !ok {
			return false
		}
	}
	if _, ok := skip[key]; ok {
		return false
	}
	return true
}

func evaluateWhen(kind, expr string, ctx config.TemplateContext) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return true, nil
	}
	rendered, err := config.RenderTemplate(kind+"-when", []byte(expr), ctx)
	if err != nil {
		return false, err
	}
	s := strings.TrimSpace(string(rendered))
	if s == "" {
		return true, nil
	}
	ls := strings.ToLower(s)
	if ls == "false" || ls == "0" || ls == "no" {
		return false, nil
	}
	return true, nil
}

func evaluateServiceWhen(expr string, ctx config.TemplateContext) (bool, error) {
	return evaluateWhen("service", expr, ctx)
}

func evaluateInfraWhen(expr string, ctx config.TemplateContext) (bool, error) {
	return evaluateWhen("infra", expr, ctx)
}

func (e *Engine) loadManifestDocuments(path string, ctx config.TemplateContext) ([]map[string]any, error) {
	if path == "" {
		return nil, fmt.Errorf("manifest path is empty")
	}
	fullPath := path
	if !filepath.IsAbs(fullPath) && ctx.ProjectRoot != "" {
		fullPath = filepath.Join(ctx.ProjectRoot, path)
	}

	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest %q: %w", fullPath, err)
	}

	rendered, err := config.RenderTemplate(fullPath, raw, ctx)
	if err != nil {
		return nil, err
	}

	var docs []map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(rendered))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode manifest %q: %w", fullPath, err)
		}
		if len(doc) == 0 {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func shouldDropKind(doc map[string]any, dropKinds []string) bool {
	if len(dropKinds) == 0 {
		return false
	}
	kind, _ := doc["kind"].(string)
	if kind == "" {
		return false
	}
	for _, k := range dropKinds {
		if strings.EqualFold(strings.TrimSpace(k), kind) {
			return true
		}
	}
	return false
}

func applyNamespace(doc map[string]any, ns string) {
	if ns == "" {
		return
	}
	kind, _ := doc["kind"].(string)
	if kind == "" {
		return
	}
	switch kind {
	case "Namespace", "ClusterRole", "ClusterRoleBinding", "PersistentVolume", "ValidatingWebhookConfiguration", "MutatingWebhookConfiguration":
		return
	}
	meta := getOrCreateMap(doc, "metadata")
	if existing, _ := meta["namespace"].(string); strings.TrimSpace(existing) != "" {
		return
	}
	meta["namespace"] = ns
}

// applyServiceImage sets the container image for the main deployment and its init containers.
// It evaluates the image tag template for the current context when provided.
func applyServiceImage(doc map[string]any, svc config.Service, ctx config.TemplateContext) error {
	fullRepo := strings.TrimSpace(svc.Image.Repository)
	if fullRepo == "" {
		return nil
	}
	image := fullRepo
	tagTemplate := strings.TrimSpace(svc.Image.Tag)
	if tagTemplate != "" {
		renderedTag, err := config.RenderTemplate("tag", []byte(tagTemplate), ctx)
		if err != nil {
			return fmt.Errorf("render tag template: %w", err)
		}
		tag := strings.TrimSpace(string(renderedTag))
		if tag != "" {
			image = fullRepo + ":" + tag
		}
	}

	kind, _ := doc["kind"].(string)
	if kind != "Deployment" {
		return nil
	}

	meta := getOrCreateMap(doc, "metadata")
	name, _ := meta["name"].(string)
	if name == "" || name != svc.Name {
		return nil
	}

	spec := getOrCreateMap(doc, "spec")
	template := getOrCreateMap(spec, "template")
	podSpec := getOrCreateMap(template, "spec")

	containers := getSliceOfMaps(podSpec, "containers")
	if len(containers) == 0 {
		return nil
	}
	main := containers[0]
	oldImage, _ := main["image"].(string)
	main["image"] = image
	containers[0] = main
	podSpec["containers"] = containers

	initContainers := getSliceOfMaps(podSpec, "initContainers")
	for i, ic := range initContainers {
		icImg, _ := ic["image"].(string)
		if icImg == "" || icImg == oldImage {
			ic["image"] = image
			initContainers[i] = ic
		}
	}
	if len(initContainers) > 0 {
		podSpec["initContainers"] = initContainers
	}
	return nil
}

// applyHostMounts injects hostPath volumes and mounts into a deployment according to overlay.
func applyHostMounts(doc map[string]any, svc config.Service, overlay config.Overlay) {
	if len(overlay.HostMounts) == 0 {
		return
	}

	kind, _ := doc["kind"].(string)
	if kind != "Deployment" {
		return
	}

	meta := getOrCreateMap(doc, "metadata")
	name, _ := meta["name"].(string)
	if name == "" || name != svc.Name {
		return
	}

	spec := getOrCreateMap(doc, "spec")
	template := getOrCreateMap(spec, "template")
	podSpec := getOrCreateMap(template, "spec")

	containers := getSliceOfMaps(podSpec, "containers")
	if len(containers) == 0 {
		return
	}
	main := containers[0]

	volumes := getSliceOfMaps(podSpec, "volumes")
	volumes = applyVolumes(volumes, overlay.HostMounts)
	podSpec["volumes"] = volumes

	main = applyVolumeMounts(main, overlay.HostMounts)
	containers[0] = main

	initContainers := getSliceOfMaps(podSpec, "initContainers")
	for i, ic := range initContainers {
		initContainers[i] = applyVolumeMounts(ic, overlay.HostMounts)
	}
	if len(initContainers) > 0 {
		podSpec["initContainers"] = initContainers
	}
}

// getOrCreateMap returns an existing nested map or creates a new one at the given key.
func getOrCreateMap(parent map[string]any, key string) map[string]any {
	if parent == nil {
		return map[string]any{}
	}
	if val, ok := parent[key]; ok {
		if m, ok := val.(map[string]any); ok && m != nil {
			return m
		}
	}
	m := make(map[string]any)
	parent[key] = m
	return m
}

// getSliceOfMaps returns a normalized slice of maps stored under the given key.
func getSliceOfMaps(parent map[string]any, key string) []map[string]any {
	if parent == nil {
		return nil
	}
	val, ok := parent[key]
	if !ok || val == nil {
		return nil
	}
	return normalizeMapSlice(val)
}

// applyVolumes merges existing volumes with hostPath volumes derived from mounts.
func applyVolumes(volumes []map[string]any, mounts []config.HostMount) []map[string]any {
	out := make([]map[string]any, 0, len(volumes)+len(mounts))
	excluded := make(map[string]struct{})
	for _, m := range mounts {
		excluded[m.Name] = struct{}{}
	}
	for _, v := range volumes {
		name, _ := v["name"].(string)
		if _, skip := excluded[name]; skip {
			continue
		}
		out = append(out, v)
	}
	for _, m := range mounts {
		if m.Name == "" || m.HostPath == "" || m.MountPath == "" {
			continue
		}
		out = append(out, map[string]any{
			"name": m.Name,
			"hostPath": map[string]any{
				"path": m.HostPath,
				"type": "Directory",
			},
		})
	}
	return out
}

// applyVolumeMounts merges existing volumeMounts with mounts derived from hostPath settings.
func applyVolumeMounts(container map[string]any, mounts []config.HostMount) map[string]any {
	if container == nil {
		return container
	}
	excluded := make(map[string]struct{})
	for _, m := range mounts {
		excluded[m.Name] = struct{}{}
	}

	// Normalize existing mounts.
	existing := normalizeMapSlice(container["volumeMounts"])

	out := make([]map[string]any, 0, len(existing)+len(mounts))
	for _, vm := range existing {
		name, _ := vm["name"].(string)
		if _, skip := excluded[name]; skip {
			continue
		}
		out = append(out, vm)
	}

	for _, m := range mounts {
		if m.Name == "" || m.MountPath == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":      m.Name,
			"mountPath": m.MountPath,
		})
	}

	if len(out) > 0 {
		container["volumeMounts"] = out
	}
	return container
}
