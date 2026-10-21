package usertext

import (
	"embed"

	texti18n "github.com/codex-k8s/kodex/libs/go/i18n"
)

// messages embeds the gateway's translation catalogs without loading runtime files or external content.
// RU: messages встраивает переводы шлюза без загрузки файлов во время исполнения или внешнего содержимого.
//
//go:embed messages/*.yaml
var messages embed.FS

// New constructs a localizer from the embedded English and Russian message files with the shared default locale.
// RU: New создаёт локализатор из встроенных английских и русских сообщений с общей локалью по умолчанию.
func New() (*texti18n.Localizer, error) {
	return texti18n.New(texti18n.Config{
		Locale: texti18n.DefaultLocale, MessageFS: messages,
		MessageFiles: []string{"messages/interaction.en.yaml", "messages/interaction.ru.yaml"},
	})
}
