// Package env contains helpers for loading and merging environment variables from multiple sources.
package env

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// Vars represents a simple string-to-string map of variables.
type Vars map[string]string

// FromOS builds a Vars map from the current process environment.
func FromOS() Vars {
	out := make(Vars)
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		out[parts[0]] = parts[1]
	}
	return out
}

// Merge merges several Vars maps into one, later maps overriding earlier keys.
func Merge(sets ...Vars) Vars {
	out := make(Vars)
	for _, s := range sets {
		for k, v := range s {
			out[k] = v
		}
	}
	return out
}

// LoadEnvFile loads a single .env-style file into Vars.
func LoadEnvFile(path string) (Vars, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	envMap, err := godotenv.Parse(f)
	if err != nil {
		return nil, err
	}
	out := make(Vars, len(envMap))
	for k, v := range envMap {
		out[k] = v
	}
	return out, nil
}

// LoadEnvFiles loads multiple .env-style files and merges them in order.
func LoadEnvFiles(baseDir string, files []string) (Vars, error) {
	var result Vars
	for _, name := range files {
		if name == "" {
			continue
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(baseDir, name)
		}
		vars, err := LoadEnvFile(path)
		if err != nil {
			return nil, fmt.Errorf("load env file %q: %w", path, err)
		}
		result = Merge(result, vars)
	}
	return result, nil
}

// ParseInlineVars parses a comma-separated k=v list (e.g. "A=1,B=2") into Vars.
func ParseInlineVars(s string) (Vars, error) {
	out := make(Vars)
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid inline var %q, expected key=value", part)
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		if key == "" {
			return nil, fmt.Errorf("empty key in inline var %q", part)
		}
		out[key] = value
	}
	return out, nil
}

// LoadVarFile loads a var-file which can be either YAML-like key: value per line
// or .env-style key=value lines, returning Vars.
func LoadVarFile(path string) (Vars, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	result := make(Vars)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sep := ":"
		if strings.Contains(line, "=") {
			sep = "="
		}
		parts := strings.SplitN(line, sep, 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		value = strings.TrimPrefix(value, "\"")
		value = strings.TrimSuffix(value, "\"")
		value = strings.TrimPrefix(value, "'")
		value = strings.TrimSuffix(value, "'")
		if key != "" {
			result[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
