package config

import (
	"path/filepath"
	"strings"
)

// DataPaths describes how to locate data directories for services (e.g. postgres/redis).
type DataPaths struct {
	Root   string   `yaml:"root,omitempty"`
	EnvDir string   `yaml:"envDir,omitempty"`
	Dirs   []string `yaml:"dirs,omitempty"`
	Paths  []string `yaml:"paths,omitempty"`
}

// ResolvedDataPaths provides a normalized view of data paths for the current env/slot.
type ResolvedDataPaths struct {
	Root   string
	EnvDir string
	Paths  []string
}

// ResolveDataPaths resolves and normalizes data paths defined in services.yaml.
func ResolveDataPaths(cfg *StackConfig) ResolvedDataPaths {
	var res ResolvedDataPaths
	if cfg == nil || cfg.DataPaths == nil {
		return res
	}

	root := strings.TrimSpace(cfg.DataPaths.Root)
	envDir := strings.TrimSpace(cfg.DataPaths.EnvDir)

	paths := make([]string, 0, len(cfg.DataPaths.Paths))
	for _, p := range cfg.DataPaths.Paths {
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}

	if len(paths) == 0 {
		base := envDir
		if base == "" {
			base = root
		}
		if base != "" {
			if len(cfg.DataPaths.Dirs) > 0 {
				for _, d := range cfg.DataPaths.Dirs {
					d = strings.TrimSpace(d)
					if d == "" {
						continue
					}
					paths = append(paths, filepath.Join(base, d))
				}
			} else {
				paths = append(paths, base)
			}
		}
	}

	res.Root = root
	res.EnvDir = envDir
	res.Paths = dedupeStrings(paths)
	return res
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
