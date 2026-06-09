package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
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

	resolved := goi18n.NewLocalizer(localizer.bundle, locale, DefaultLocale)
	text, err := resolved.Localize(&goi18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: data,
	})
	if err != nil {
		return messageID
	}
	return text
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
