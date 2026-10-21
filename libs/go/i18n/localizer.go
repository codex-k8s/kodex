// Package i18n loads shared message catalogs and resolves English/Russian locales
// while keeping per-request translation separate from the mutable default locale.
// Пакет i18n загружает каталоги и выбирает английскую/русскую локаль, отделяя
// перевод конкретного запроса от изменяемой локали по умолчанию.
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
	// DefaultLocale is the English fallback used for empty or unsupported requests.
	// DefaultLocale — английский fallback для пустого или неподдерживаемого запроса.
	DefaultLocale = "en"
	// RussianLocale identifies the second explicitly supported language.
	// RussianLocale определяет второй явно поддерживаемый язык.
	RussianLocale = "ru"
)

// supportedLocales fixes the public locale order; callers receive a copied slice.
// supportedLocales задаёт порядок публичных локалей; вызывающий получает копию.
var supportedLocales = []string{DefaultLocale, RussianLocale}

// Config supplies a message filesystem, exact catalog paths and the initial
// default locale. New requires both filesystem and a nonempty file list.
// Config задаёт файловую систему, точные пути каталогов и начальную локаль.
// New требует файловую систему и непустой список файлов.
type Config struct {
	Locale       string
	MessageFS    fs.FS
	MessageFiles []string
}

// Localizer shares a loaded bundle while protecting its mutable default locale
// with a mutex; explicit Localize calls do not modify that default.
// Localizer использует общий загруженный bundle и защищает изменяемую локаль
// mutex; явный вызов Localize не меняет эту локаль по умолчанию.
type Localizer struct {
	bundle *goi18n.Bundle
	mu     sync.RWMutex
	locale string
}

// New registers JSON/YAML loaders, reads every configured catalog and validates
// the initial locale before publishing a Localizer. A load or locale failure
// returns no partially configured instance.
// New регистрирует JSON/YAML-разбор, читает все каталоги и проверяет начальную
// локаль до возврата Localizer. Ошибка загрузки или локали не возвращает
// частично настроенный экземпляр.
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

// T reads the synchronized default locale once and translates with that snapshot;
// a nil receiver returns the message ID rather than attempting catalog access.
// T один раз читает защищённую локаль по умолчанию и переводит с этим снимком;
// nil-получатель возвращает ID сообщения без обращения к каталогу.
func (localizer *Localizer) T(messageID string, data map[string]any) string {
	if localizer == nil {
		return messageID
	}
	localizer.mu.RLock()
	locale := localizer.locale
	localizer.mu.RUnlock()

	return localizer.Localize(locale, messageID, data)
}

// Localize creates a request-local translator with the normalized requested
// locale and English fallback. It leaves the shared default unchanged and
// returns messageID on a nil receiver or localization error.
// Localize создаёт локальный переводчик с нормализованной запрошенной локалью
// и английским fallback. Общая локаль не меняется; nil или ошибка перевода
// возвращает messageID, не влияя на параллельные запросы другого пользователя.
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

// ResolveAcceptLanguage matches the header against the two supported languages
// and returns the default for empty input. The header selects presentation only,
// never user identity or application permissions.
// ResolveAcceptLanguage сопоставляет заголовок с двумя поддерживаемыми языками,
// возвращая default при пустом вводе. Заголовок выбирает только представление,
// а не identity пользователя или прикладные полномочия.
func ResolveAcceptLanguage(value string) string {
	if strings.TrimSpace(value) == "" {
		return DefaultLocale
	}
	matcher := language.NewMatcher([]language.Tag{language.English, language.Russian})
	tag, _ := language.MatchStrings(matcher, value)
	base, _ := tag.Base()
	return NormalizeLocale(base.String())
}

// Locale reads the synchronized default locale, using DefaultLocale for nil.
// Locale читает защищённую локаль по умолчанию; для nil возвращает DefaultLocale.
func (localizer *Localizer) Locale() string {
	if localizer == nil {
		return DefaultLocale
	}
	localizer.mu.RLock()
	defer localizer.mu.RUnlock()
	return localizer.locale
}

// SetLocale validates the requested language before atomically replacing the
// shared default; unsupported input or a nil receiver leaves state unchanged.
// SetLocale проверяет язык до атомарной замены общей локали; неподдерживаемый
// ввод или nil-получатель не изменяет состояние.
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

// SupportedLocales returns the package's copied supported list without consulting
// receiver state; even a nil receiver cannot expose the backing slice.
// SupportedLocales возвращает копию списка пакета независимо от состояния
// получателя; даже nil не раскрывает общий изменяемый slice.
func (localizer *Localizer) SupportedLocales() []string {
	return SupportedLocales()
}

// SupportedLocales copies the fixed language list so callers cannot mutate the
// catalog-wide policy by changing their returned slice.
// SupportedLocales копирует список языков, чтобы изменение возвращённого slice
// не меняло общую политику каталога.
func SupportedLocales() []string {
	return append([]string(nil), supportedLocales...)
}

// ResolveLocale trims whitespace, maps underscore separators to hyphens and
// accepts only English/Russian base languages. Empty input is a supported default;
// malformed or other language tags return false instead of a silent selection.
// ResolveLocale обрезает пробелы, заменяет underscore на hyphen и допускает
// только английский/русский базовый язык. Пустой ввод — допустимый default;
// неверный tag или другой язык возвращает false вместо скрытого выбора.
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

// NormalizeLocale resolves a supported base language and deliberately falls back
// to DefaultLocale when a presentation-only caller supplies an unsupported tag.
// NormalizeLocale выбирает поддерживаемый базовый язык и явно возвращает
// DefaultLocale при неподдерживаемом tag в запросе на представление.
func NormalizeLocale(value string) string {
	locale, ok := ResolveLocale(value)
	if !ok {
		return DefaultLocale
	}
	return locale
}
