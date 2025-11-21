package cli

import (
	"strings"

	"github.com/codex-k8s/codexctl/internal/config"
)

// serviceEnabled determines whether to include the service and its hooks for the current context.
func serviceEnabled(svc config.Service, ctx config.TemplateContext) (bool, error) {
	expr := strings.TrimSpace(svc.When)
	if expr == "" {
		return true, nil
	}

	rendered, err := config.RenderTemplate("service-when", []byte(expr), ctx)
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
