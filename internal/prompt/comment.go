package prompt

import (
	"fmt"
	"strings"
	"text/template"

	"github.com/codex-k8s/codexctl/internal/config"
)

// RenderEnvComment renders an environment comment in the requested language.
// Links are taken from project config; paths are appended to https://<host>.
func RenderEnvComment(lang, host string, slot int, links []config.Link) (string, error) {
	tmplName := "templates/env_comment_en.tmpl"
	switch strings.ToLower(lang) {
	case "ru":
		tmplName = "templates/env_comment_ru.tmpl"
	}

	type link struct {
		Title string
		URL   string
	}
	var renderedLinks []link
	for _, l := range links {
		title := strings.TrimSpace(l.Title)
		p := strings.TrimSpace(l.Path)
		if title == "" || p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		renderedLinks = append(renderedLinks, link{
			Title: title,
			URL:   fmt.Sprintf("https://%s%s", host, p),
		})
	}

	data := struct {
		Host  string
		Slot  int
		Links []link
	}{
		Host:  host,
		Slot:  slot,
		Links: renderedLinks,
	}

	tmplData, err := commentTemplates.ReadFile(tmplName)
	if err != nil {
		return "", fmt.Errorf("load comment template %s: %w", tmplName, err)
	}

	tmpl, err := template.New(tmplName).Parse(string(tmplData))
	if err != nil {
		return "", fmt.Errorf("parse comment template: %w", err)
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("execute comment template: %w", err)
	}

	return sb.String(), nil
}
