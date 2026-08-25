// Package usertext загружает неизменяемый каталог пользовательских текстов.
package usertext

import (
	"embed"

	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
)

//go:embed messages/*.yaml
var messages embed.FS

func New() (*texti18n.Localizer, error) {
	return texti18n.New(texti18n.Config{
		Locale:       texti18n.DefaultLocale,
		MessageFS:    messages,
		MessageFiles: []string{"messages/problems.en.yaml", "messages/problems.ru.yaml"},
	})
}
