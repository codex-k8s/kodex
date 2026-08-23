package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
)

const (
	DefaultLocale = "en"
	RussianLocale = "ru"
)

var supportedLocales = []string{DefaultLocale, RussianLocale}

type Config struct {
	Locale       string
	MessageFS    fs.FS
	MessageFiles []string
}

type Localizer struct {
	bundle *goi18n.Bundle
	mu     sync.RWMutex
	locale string
}

func New(cfg Config) (*Localizer, error) {
	if cfg.MessageFS == nil {
		return nil, fmt.Errorf("message filesystem is required")
	}
	if len(cfg.MessageFiles) == 0 {
		return nil, fmt.Errorf("message files are required")
	}

	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	bundle.RegisterUnmarshalFunc("yaml", yaml.Unmarshal)
	bundle.RegisterUnmarshalFunc("yml", yaml.Unmarshal)

	for _, path := range cfg.MessageFiles {
		if _, err := bundle.LoadMessageFileFS(cfg.MessageFS, path); err != nil {
			return nil, fmt.Errorf("load locale messages %s: %w", path, err)
		}
	}

	resolved, ok := ResolveLocale(cfg.Locale)
	if !ok {
		return nil, fmt.Errorf("unsupported locale %q", cfg.Locale)
	}
	return &Localizer{bundle: bundle, locale: resolved}, nil
}

func (localizer *Localizer) T(messageID string, data map[string]any) string {
	if localizer == nil {
		return messageID
	}
	localizer.mu.RLock()
	locale := localizer.locale
	localizer.mu.RUnlock()

	return localizer.Localize(locale, messageID, data)
}

// Localize создаёт локализатор на один вызов. Благодаря этому выбранная одним
// пользователем локаль не меняет ответы параллельных запросов другого
// пользователя.
func (localizer *Localizer) Localize(locale string, messageID string, data map[string]any) string {
	if localizer == nil {
		return messageID
	}
	resolvedLocale := NormalizeLocale(locale)
	resolved := goi18n.NewLocalizer(localizer.bundle, resolvedLocale, DefaultLocale)
	text, err := resolved.Localize(&goi18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: data,
	})
	if err != nil {
		return messageID
	}
	return text
}

// ResolveAcceptLanguage выбирает поддерживаемую локаль из стандартного
// Accept-Language и не доверяет заголовку как источнику полномочий.
func ResolveAcceptLanguage(value string) string {
	if strings.TrimSpace(value) == "" {
		return DefaultLocale
	}
	matcher := language.NewMatcher([]language.Tag{language.English, language.Russian})
	tag, _ := language.MatchStrings(matcher, value)
	base, _ := tag.Base()
	return NormalizeLocale(base.String())
}

func (localizer *Localizer) Locale() string {
	if localizer == nil {
		return DefaultLocale
	}
	localizer.mu.RLock()
	defer localizer.mu.RUnlock()
	return localizer.locale
}

func (localizer *Localizer) SetLocale(locale string) (string, error) {
	if localizer == nil {
		return "", fmt.Errorf("localizer is not configured")
	}
	resolved, ok := ResolveLocale(locale)
	if !ok {
		return "", fmt.Errorf("unsupported locale %q", locale)
	}
	localizer.mu.Lock()
	localizer.locale = resolved
	localizer.mu.Unlock()
	return resolved, nil
}

func (localizer *Localizer) SupportedLocales() []string {
	return SupportedLocales()
}

func SupportedLocales() []string {
	return append([]string(nil), supportedLocales...)
}

func ResolveLocale(value string) (string, bool) {
	candidate := strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if candidate == "" {
		return DefaultLocale, true
	}

	tag, err := language.Parse(candidate)
	if err != nil {
		return "", false
	}
	base, _ := tag.Base()
	switch base.String() {
	case DefaultLocale:
		return DefaultLocale, true
	case RussianLocale:
		return RussianLocale, true
	default:
		return "", false
	}
}

func NormalizeLocale(value string) string {
	locale, ok := ResolveLocale(value)
	if !ok {
		return DefaultLocale
	}
	return locale
}
