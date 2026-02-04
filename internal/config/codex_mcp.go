// Package config contains the loader and strongly typed model for services.yaml.
package config

// CodexConfigBlock defines an extra TOML fragment appended to the Codex config.
type CodexConfigBlock struct {
	// Name is an optional identifier used for error reporting.
	Name string `yaml:"name,omitempty"`
	// TOML is an inline TOML fragment.
	TOML string `yaml:"toml,omitempty"`
	// File references a TOML fragment file relative to the project root.
	File string `yaml:"file,omitempty"`
}

// CodexMCPConfig describes MCP servers exposed to Codex.
type CodexMCPConfig struct {
	// Servers defines MCP servers available to the agent.
	Servers []MCPServer `yaml:"servers,omitempty"`
}

// MCPServer defines a single MCP server entry.
type MCPServer struct {
	// Name is the unique server key in config.toml.
	Name string `yaml:"name"`
	// Type is one of: stdio, http, https, cluster.
	Type string `yaml:"type"`
	// Description is an optional human-readable summary for prompts.
	Description string `yaml:"description,omitempty"`
	// ToolTimeoutSec is the per-tool timeout in seconds.
	ToolTimeoutSec int `yaml:"tool_timeout_sec,omitempty"`
	// StartupTimeoutSec is the server startup timeout in seconds (stdio only).
	StartupTimeoutSec int `yaml:"startup_timeout_sec,omitempty"`

	// Command is the stdio command (for type=stdio).
	Command string `yaml:"command,omitempty"`
	// Args are the stdio command arguments.
	Args []string `yaml:"args,omitempty"`
	// Env defines stdio environment variables.
	Env map[string]ValueRef `yaml:"env,omitempty"`

	// Endpoint is the HTTP(S) endpoint URL for type=http/https.
	Endpoint string `yaml:"endpoint,omitempty"`
	// Headers defines HTTP headers for type=http/https (value/envRef/varRef).
	Headers map[string]ValueRef `yaml:"headers,omitempty"`
	// BearerTokenEnvVar provides bearer token env var for HTTP servers.
	BearerTokenEnvVar string `yaml:"bearer_token_env_var,omitempty"`

	// Service defines a cluster service endpoint for type=cluster.
	Service *MCPServiceRef `yaml:"service,omitempty"`

	// Tools lists known tools for prompt rendering.
	Tools []MCPTool `yaml:"tools,omitempty"`
}

// MCPServiceRef describes an in-cluster service endpoint.
type MCPServiceRef struct {
	// Name is the Kubernetes Service name.
	Name string `yaml:"name"`
	// Namespace overrides the namespace for the service.
	Namespace string `yaml:"namespace,omitempty"`
	// Port is the service port.
	Port int `yaml:"port,omitempty"`
	// Path is the MCP path (default: /mcp).
	Path string `yaml:"path,omitempty"`
	// Scheme is the URL scheme (http/https).
	Scheme string `yaml:"scheme,omitempty"`
}

// MCPTool describes a tool exposed by an MCP server.
type MCPTool struct {
	// Name is the tool name.
	Name string `yaml:"name"`
	// Description is a short description shown in prompts.
	Description string `yaml:"description,omitempty"`
}

// ValueRef references a value without embedding secrets in services.yaml.
type ValueRef struct {
	// Value is a literal value (avoid for secrets).
	Value string `yaml:"value,omitempty"`
	// EnvRef reads from an environment variable (including envFiles).
	EnvRef string `yaml:"envRef,omitempty"`
	// VarRef reads from vars/varFiles.
	VarRef string `yaml:"varRef,omitempty"`
	// Optional allows missing values without failing.
	Optional bool `yaml:"optional,omitempty"`
}
