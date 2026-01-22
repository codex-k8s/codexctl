package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

type dataPathAction int

const (
	dataPathEnsure dataPathAction = iota
	dataPathClean
	dataPathDelete
)

func handleDataPaths(logger *slog.Logger, stackCfg *config.StackConfig, action dataPathAction) error {
	if stackCfg == nil {
		return nil
	}
	resolved := config.ResolveDataPaths(stackCfg)
	if resolved.Root == "" && resolved.EnvDir == "" && len(resolved.Paths) == 0 {
		return nil
	}

	paths := dataPathsTargets(resolved)
	if len(paths) == 0 {
		return nil
	}

	for _, path := range paths {
		if !safeDataPath(resolved.Root, path) {
			if logger != nil {
				logger.Warn("skip data path due to safety guard", "path", path, "root", resolved.Root)
			}
			continue
		}
		switch action {
		case dataPathEnsure:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create data dir %q: %w", path, err)
			}
			if err := os.Chmod(path, 0o777); err != nil && logger != nil {
				logger.Warn("failed to chmod data dir", "dir", path, "error", err)
			}
		case dataPathClean:
			if err := cleanDataDir(path); err != nil && logger != nil {
				logger.Warn("failed to clean data dir", "dir", path, "error", err)
			}
		case dataPathDelete:
			if err := os.RemoveAll(path); err != nil && logger != nil {
				logger.Warn("failed to delete data dir", "dir", path, "error", err)
			}
		}
	}
	return nil
}

func dataPathsTargets(resolved config.ResolvedDataPaths) []string {
	var paths []string
	if resolved.EnvDir != "" {
		paths = append(paths, resolved.EnvDir)
	}
	paths = append(paths, resolved.Paths...)
	return dedupeStrings(paths)
}

func cleanDataDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func safeDataPath(root, path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || path == string(os.PathSeparator) {
		return false
	}
	if root == "" {
		return filepath.IsAbs(path)
	}
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." || root == string(os.PathSeparator) {
		return false
	}
	if path == root {
		return false
	}
	rootWithSep := root + string(os.PathSeparator)
	return strings.HasPrefix(path, rootWithSep)
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
