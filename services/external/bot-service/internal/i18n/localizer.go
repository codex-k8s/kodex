package i18n

import (
	"embed"

	basei18n "github.com/codex-k8s/matter-codex/libs/go/i18n"
)

const (
	DefaultLocale = basei18n.DefaultLocale
	RussianLocale = basei18n.RussianLocale
)

type Localizer = basei18n.Localizer

//go:embed locales/*.json
var localeFS embed.FS

func New(locale string) (*Localizer, error) {
	return basei18n.New(basei18n.Config{
		Locale:       locale,
		MessageFS:    localeFS,
		MessageFiles: []string{"locales/en.json", "locales/ru.json"},
	})
}

func SupportedLocales() []string {
	return basei18n.SupportedLocales()
}

func ResolveLocale(value string) (string, bool) {
	return basei18n.ResolveLocale(value)
}

func NormalizeLocale(value string) string {
	return basei18n.NormalizeLocale(value)
}
