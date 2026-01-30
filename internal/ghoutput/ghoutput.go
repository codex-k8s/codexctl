package ghoutput

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Write appends GitHub Actions outputs to the GITHUB_OUTPUT file when available.
func Write(values map[string]string) error {
	path := strings.TrimSpace(os.Getenv("GITHUB_OUTPUT"))
	if path == "" {
		return nil
	}
	if len(values) == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	keys := make([]string, 0, len(values))
	for k := range values {
		if strings.TrimSpace(k) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := sanitize(values[key])
		if _, err := fmt.Fprintf(f, "%s=%s\n", key, value); err != nil {
			return err
		}
	}
	return nil
}

func sanitize(value string) string {
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\r", "%0D")
	value = strings.ReplaceAll(value, "\n", "%0A")
	return value
}
