package githubratelimit

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"
)

var (
	//go:embed templates/*.tmpl
	messageTemplatesFS embed.FS
	messageTemplates   = template.Must(template.New("github-rate-limit-messages").ParseFS(messageTemplatesFS, "templates/*.tmpl"))
)

func renderMessageTemplate(name string, data messageTemplateData) (string, error) {
	var out bytes.Buffer
	if err := messageTemplates.ExecuteTemplate(&out, name, data); err != nil {
		return "", fmt.Errorf("render github rate-limit template %s: %w", name, err)
	}
	return strings.TrimSpace(out.String()), nil
}

func formatTemplateTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
