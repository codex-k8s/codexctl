package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
	"github.com/codex-k8s/codexctl/internal/stringsutil"
)

type dataPathAction int

const (
	dataPathEnsure dataPathAction = iota
	dataPathClean
	dataPathDelete
)

// handleDataPaths performs create/clean/delete actions for resolved data paths.
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

// dataPathsTargets returns the list of paths to manage, deduplicated.
func dataPathsTargets(resolved config.ResolvedDataPaths) []string {
	var paths []string
	if resolved.EnvDir != "" {
		paths = append(paths, resolved.EnvDir)
	}
	paths = append(paths, resolved.Paths...)
	return stringsutil.DedupeStrings(paths)
}

// cleanDataDir removes all entries within a directory without removing the dir itself.
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

// safeDataPath enforces that path is within the configured root.
func safeDataPath(root, path string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || path == string(os.PathSeparator) {
		return false
	}
	// Without a root, only absolute paths are considered safe.
	if root == "" {
		return filepath.IsAbs(path)
	}
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." || root == string(os.PathSeparator) {
		return false
	}
	// Avoid deleting the root itself.
	if path == root {
		return false
	}
	rootWithSep := root + string(os.PathSeparator)
	return strings.HasPrefix(path, rootWithSep)
}
