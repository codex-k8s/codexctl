package prompt

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/codex-k8s/codexctl/internal/config"
)

func (r *Renderer) renderCodexConfig() ([]byte, error) {
	raw, name, err := r.loadCodexConfigTemplate()
	if err != nil {
		return nil, err
	}

	renderCtx := r.ctx
	if r.CodexConfigTemplatePath() == "" && hasMCPServer(r.stack, "context7") {
		renderCtx = maskContext7Env(renderCtx)
	}

	rendered, err := config.RenderTemplate(name, raw, renderCtx)
	if err != nil {
		return nil, fmt.Errorf("render Codex config template %q: %w", name, err)
	}

	blocks, err := renderCodexConfigBlocks(r.ctx, r.stack, r.resolveProjectPath)
	if err != nil {
		return nil, err
	}

	mcpBlock, err := renderMCPServersTOML(r.ctx, r.stack)
	if err != nil {
		return nil, err
	}

	combined := joinTomlSections(rendered, blocks, []byte(mcpBlock))
	if err := validateToml(combined); err != nil {
		return nil, err
	}
	return combined, nil
}

func (r *Renderer) loadCodexConfigTemplate() ([]byte, string, error) {
	path := r.CodexConfigTemplatePath()
	if path == "" {
		raw, err := builtinTemplates.ReadFile(defaultCodexConfigTemplate)
		if err != nil {
			return nil, "", fmt.Errorf("load builtin Codex config template: %w", err)
		}
		return raw, "codex-config.toml", nil
	}

	resolved, err := r.resolveProjectPath(path)
	if err != nil {
		return nil, "", err
	}

	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("read Codex config template %q: %w", resolved, err)
	}
	return raw, filepath.Base(resolved), nil
}

func renderCodexConfigBlocks(ctx config.TemplateContext, stack *config.StackConfig, resolvePath func(string) (string, error)) ([]byte, error) {
	if stack == nil {
		return nil, nil
	}
	blocks := stack.Codex.ConfigBlocks
	if len(blocks) == 0 {
		return nil, nil
	}

	var rendered []string
	for idx, block := range blocks {
		raw, name, err := readConfigBlock(block, ctx, resolvePath, idx)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}
		out, err := config.RenderTemplate(name, raw, ctx)
		if err != nil {
			return nil, fmt.Errorf("render codex config block %q: %w", name, err)
		}
		if strings.TrimSpace(string(out)) == "" {
			continue
		}
		if block.Name != "" {
			rendered = append(rendered, fmt.Sprintf("# codex.configBlocks: %s\n%s", block.Name, strings.TrimRight(string(out), "\n")))
		} else {
			rendered = append(rendered, strings.TrimRight(string(out), "\n"))
		}
	}
	if len(rendered) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(rendered, "\n\n")), nil
}

func readConfigBlock(block config.CodexConfigBlock, ctx config.TemplateContext, resolvePath func(string) (string, error), idx int) ([]byte, string, error) {
	kind := strings.TrimSpace(block.Name)
	name := kind
	if name == "" {
		name = fmt.Sprintf("config_block_%d", idx+1)
	}

	hasInline := strings.TrimSpace(block.TOML) != ""
	hasFile := strings.TrimSpace(block.File) != ""
	if hasInline && hasFile {
		return nil, name, fmt.Errorf("codex config block %q: both toml and file are set", name)
	}
	if !hasInline && !hasFile {
		return nil, name, fmt.Errorf("codex config block %q: toml or file is required", name)
	}
	if hasInline {
		return []byte(block.TOML), name, nil
	}

	path, err := resolvePath(block.File)
	if err != nil {
		return nil, name, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, name, fmt.Errorf("read codex config block file %q: %w", path, err)
	}
	return raw, name, nil
}

func renderMCPServersTOML(ctx config.TemplateContext, stack *config.StackConfig) (string, error) {
	if stack == nil {
		return "", nil
	}
	servers := stack.Codex.MCP.Servers
	if len(servers) == 0 {
		return "", nil
	}

	seen := make(map[string]struct{})
	for _, server := range servers {
		name := strings.ToLower(strings.TrimSpace(server.Name))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			return "", fmt.Errorf("mcp server %q is defined more than once", server.Name)
		}
		seen[name] = struct{}{}
	}

	var out []string
	for _, server := range servers {
		block, err := buildMCPServerBlock(ctx, server)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(block) != "" {
			out = append(out, block)
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	return "# MCP servers (from services.yaml)\n" + strings.Join(out, "\n\n"), nil
}

func buildMCPServerBlock(ctx config.TemplateContext, server config.MCPServer) (string, error) {
	name := strings.TrimSpace(server.Name)
	if name == "" {
		return "", fmt.Errorf("mcp server name is empty")
	}

	typ := strings.ToLower(strings.TrimSpace(server.Type))
	switch typ {
	case "stdio":
		return buildStdioServerBlock(ctx, name, server)
	case "http", "https":
		return buildHTTPServerBlock(ctx, name, server, typ)
	case "cluster":
		return buildClusterServerBlock(ctx, name, server)
	default:
		return "", fmt.Errorf("mcp server %q: unknown type %q", name, server.Type)
	}
}

func buildStdioServerBlock(ctx config.TemplateContext, name string, server config.MCPServer) (string, error) {
	command := strings.TrimSpace(server.Command)
	if command == "" {
		return "", fmt.Errorf("mcp server %q: command is required for stdio", name)
	}

	var buf strings.Builder
	writeTableHeader(&buf, name)
	writeTomlString(&buf, "command", command)
	if len(server.Args) > 0 {
		writeTomlStringArray(&buf, "args", server.Args)
	}
	writeTomlInt(&buf, "startup_timeout_sec", server.StartupTimeoutSec)
	writeTomlInt(&buf, "tool_timeout_sec", server.ToolTimeoutSec)

	if len(server.Env) > 0 {
		buf.WriteString("\n")
		buf.WriteString(fmt.Sprintf("[mcp_servers.%s.env]\n", name))
		if err := writeValueRefMap(ctx, &buf, "env", server.Env); err != nil {
			return "", fmt.Errorf("mcp server %q: %w", name, err)
		}
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func buildHTTPServerBlock(ctx config.TemplateContext, name string, server config.MCPServer, typ string) (string, error) {
	endpoint := strings.TrimSpace(server.Endpoint)
	if endpoint == "" {
		return "", fmt.Errorf("mcp server %q: endpoint is required for http", name)
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		if typ == "https" {
			endpoint = "https://" + endpoint
		} else {
			endpoint = "http://" + endpoint
		}
	}

	var buf strings.Builder
	writeTableHeader(&buf, name)
	writeTomlString(&buf, "url", endpoint)
	writeTomlInt(&buf, "tool_timeout_sec", server.ToolTimeoutSec)
	if strings.TrimSpace(server.BearerTokenEnvVar) != "" {
		writeTomlString(&buf, "bearer_token_env_var", strings.TrimSpace(server.BearerTokenEnvVar))
	}

	staticHeaders, envHeaders, err := splitHTTPHeaders(ctx, server.Headers)
	if err != nil {
		return "", fmt.Errorf("mcp server %q: %w", name, err)
	}
	writeTomlInlineMap(&buf, "http_headers", staticHeaders)
	writeTomlInlineMap(&buf, "env_http_headers", envHeaders)
	return strings.TrimRight(buf.String(), "\n"), nil
}

func buildClusterServerBlock(ctx config.TemplateContext, name string, server config.MCPServer) (string, error) {
	if server.Service == nil {
		return "", fmt.Errorf("mcp server %q: service is required for cluster type", name)
	}
	serviceName := strings.TrimSpace(server.Service.Name)
	if serviceName == "" {
		return "", fmt.Errorf("mcp server %q: service.name is required", name)
	}
	namespace := strings.TrimSpace(server.Service.Namespace)
	if namespace == "" {
		namespace = strings.TrimSpace(ctx.Namespace)
	}
	if namespace == "" {
		return "", fmt.Errorf("mcp server %q: service.namespace is empty and template namespace is not set", name)
	}
	scheme := strings.TrimSpace(server.Service.Scheme)
	if scheme == "" {
		scheme = "http"
	}
	path := strings.TrimSpace(server.Service.Path)
	if path == "" {
		path = "/mcp"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	port := server.Service.Port
	if port == 0 {
		port = 80
	}
	endpoint := fmt.Sprintf("%s://%s.%s.svc.cluster.local:%d%s", scheme, serviceName, namespace, port, path)

	return buildHTTPServerBlock(ctx, name, config.MCPServer{
		Endpoint:       endpoint,
		Headers:        server.Headers,
		ToolTimeoutSec: server.ToolTimeoutSec,
	}, "http")
}

func writeTableHeader(buf *strings.Builder, name string) {
	buf.WriteString(fmt.Sprintf("[mcp_servers.%s]\n", name))
}

func writeTomlString(buf *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	buf.WriteString(fmt.Sprintf("%s = %s\n", key, tomlQuote(value)))
}

func writeTomlStringArray(buf *strings.Builder, key string, values []string) {
	if len(values) == 0 {
		return
	}
	var items []string
	for _, v := range values {
		items = append(items, tomlQuote(v))
	}
	buf.WriteString(fmt.Sprintf("%s = [%s]\n", key, strings.Join(items, ", ")))
}

func writeTomlInt(buf *strings.Builder, key string, value int) {
	if value <= 0 {
		return
	}
	buf.WriteString(fmt.Sprintf("%s = %d\n", key, value))
}

func writeTomlInlineMap(buf *strings.Builder, key string, values map[string]string) {
	if len(values) == 0 {
		return
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s = %s", tomlQuote(k), tomlQuote(values[k])))
	}
	buf.WriteString(fmt.Sprintf("%s = { %s }\n", key, strings.Join(parts, ", ")))
}

func writeValueRefMap(ctx config.TemplateContext, buf *strings.Builder, field string, refs map[string]config.ValueRef) error {
	for key, ref := range refs {
		value, ok, err := resolveValueRef(ctx, ref)
		if err != nil {
			return fmt.Errorf("%s %q: %w", field, key, err)
		}
		if !ok {
			continue
		}
		buf.WriteString(fmt.Sprintf("%s = %s\n", key, tomlQuote(value)))
	}
	return nil
}

func splitHTTPHeaders(ctx config.TemplateContext, refs map[string]config.ValueRef) (map[string]string, map[string]string, error) {
	staticHeaders := make(map[string]string)
	envHeaders := make(map[string]string)
	for key, ref := range refs {
		value, envVar, ok, err := resolveHeaderRef(ctx, ref)
		if err != nil {
			return nil, nil, fmt.Errorf("header %q: %w", key, err)
		}
		if !ok {
			continue
		}
		if envVar != "" {
			envHeaders[key] = envVar
			continue
		}
		staticHeaders[key] = value
	}
	return staticHeaders, envHeaders, nil
}

func resolveHeaderRef(ctx config.TemplateContext, ref config.ValueRef) (string, string, bool, error) {
	used := 0
	if strings.TrimSpace(ref.Value) != "" {
		used++
	}
	if strings.TrimSpace(ref.EnvRef) != "" {
		used++
	}
	if strings.TrimSpace(ref.VarRef) != "" {
		used++
	}
	if used == 0 {
		if ref.Optional {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("value/envRef/varRef is required")
	}
	if used > 1 {
		return "", "", false, fmt.Errorf("only one of value/envRef/varRef can be set")
	}
	if v := strings.TrimSpace(ref.Value); v != "" {
		return v, "", true, nil
	}
	if key := strings.TrimSpace(ref.EnvRef); key != "" {
		if strings.TrimSpace(ctx.EnvMap[key]) == "" {
			if ref.Optional {
				return "", "", false, nil
			}
			return "", "", false, fmt.Errorf("envRef %q is empty", key)
		}
		return "", key, true, nil
	}
	if key := strings.TrimSpace(ref.VarRef); key != "" {
		val := strings.TrimSpace(ctx.Vars[key])
		if val == "" {
			if ref.Optional {
				return "", "", false, nil
			}
			return "", "", false, fmt.Errorf("varRef %q is empty", key)
		}
		return val, "", true, nil
	}
	return "", "", false, fmt.Errorf("invalid value reference")
}

func resolveValueRef(ctx config.TemplateContext, ref config.ValueRef) (string, bool, error) {
	used := 0
	if strings.TrimSpace(ref.Value) != "" {
		used++
	}
	if strings.TrimSpace(ref.EnvRef) != "" {
		used++
	}
	if strings.TrimSpace(ref.VarRef) != "" {
		used++
	}
	if used == 0 {
		if ref.Optional {
			return "", false, nil
		}
		return "", false, fmt.Errorf("value/envRef/varRef is required")
	}
	if used > 1 {
		return "", false, fmt.Errorf("only one of value/envRef/varRef can be set")
	}
	if v := strings.TrimSpace(ref.Value); v != "" {
		return v, true, nil
	}
	if key := strings.TrimSpace(ref.EnvRef); key != "" {
		val := strings.TrimSpace(ctx.EnvMap[key])
		if val == "" {
			if ref.Optional {
				return "", false, nil
			}
			return "", false, fmt.Errorf("envRef %q is empty", key)
		}
		return val, true, nil
	}
	if key := strings.TrimSpace(ref.VarRef); key != "" {
		val := strings.TrimSpace(ctx.Vars[key])
		if val == "" {
			if ref.Optional {
				return "", false, nil
			}
			return "", false, fmt.Errorf("varRef %q is empty", key)
		}
		return val, true, nil
	}
	return "", false, fmt.Errorf("invalid value reference")
}

func tomlQuote(value string) string {
	return strconv.Quote(value)
}

func joinTomlSections(parts ...[]byte) []byte {
	var out bytes.Buffer
	for _, part := range parts {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\n\n")
		}
		out.Write(bytes.TrimRight(part, "\n"))
	}
	out.WriteString("\n")
	return out.Bytes()
}

func validateToml(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("codex config is empty")
	}
	var target map[string]any
	if _, err := toml.Decode(string(data), &target); err != nil {
		return fmt.Errorf("invalid Codex config TOML: %w", err)
	}
	return nil
}

func hasMCPServer(stack *config.StackConfig, name string) bool {
	if stack == nil {
		return false
	}
	for _, server := range stack.Codex.MCP.Servers {
		if strings.EqualFold(strings.TrimSpace(server.Name), name) {
			return true
		}
	}
	return false
}

func maskContext7Env(ctx config.TemplateContext) config.TemplateContext {
	if ctx.EnvMap == nil {
		return ctx
	}
	clone := make(map[string]string, len(ctx.EnvMap))
	for k, v := range ctx.EnvMap {
		clone[k] = v
	}
	clone["CONTEXT7_API_KEY"] = ""
	ctx.EnvMap = clone
	return ctx
}
