// Package usertext loads the binary-owned user-message catalogs through the
// shared localizer; it contains text resources rather than runtime credentials.
// Пакет usertext загружает принадлежащие бинарному файлу user-message catalogs
// через общий localizer; содержит текстовые ресурсы, а не runtime credentials.
package usertext

import (
	"embed"

	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
)

// messages embeds both locale YAML files in the binary; the embed directive fixes source ownership and avoids runtime filesystem or environment lookup.
// messages встраивает оба locale YAML files в бинарный файл; embed directive фиксирует source ownership без поиска runtime filesystem и environment.
//
//go:embed messages/*.yaml
var messages embed.FS

// New constructs the shared localizer from the two embedded English/Russian catalogs and the library's default locale. It delegates YAML/message validation to i18n and neither reads external files nor changes message identifiers.
// New создаёт общий localizer из двух встроенных English и Russian catalogs и default locale библиотеки. YAML и message validation выполняет i18n без чтения внешних файлов и изменения message identifiers.
func New() (*texti18n.Localizer, error) {
	return texti18n.New(texti18n.Config{
		Locale:       texti18n.DefaultLocale,
		MessageFS:    messages,
		MessageFiles: []string{"messages/problems.en.yaml", "messages/problems.ru.yaml"},
	})
}
