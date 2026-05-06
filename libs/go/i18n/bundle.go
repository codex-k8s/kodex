package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// Bundle owns system localization messages loaded during process startup.
type Bundle struct {
	bundle *goi18n.Bundle
}

// NewBundle creates a localization bundle with JSON message files enabled.
func NewBundle(defaultLanguage language.Tag) *Bundle {
	bundle := goi18n.NewBundle(defaultLanguage)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	return &Bundle{bundle: bundle}
}

// LoadJSONFS loads all JSON message files that match the supplied glob patterns.
func (b *Bundle) LoadJSONFS(source fs.FS, patterns ...string) error {
	for _, pattern := range patterns {
		matches, err := fs.Glob(source, pattern)
		if err != nil {
			return fmt.Errorf("glob i18n pattern %q: %w", pattern, err)
		}
		for _, match := range matches {
			content, err := fs.ReadFile(source, match)
			if err != nil {
				return fmt.Errorf("read i18n file %q: %w", match, err)
			}
			if _, err := b.bundle.ParseMessageFileBytes(content, filepath.Base(match)); err != nil {
				return fmt.Errorf("parse i18n file %q: %w", match, err)
			}
		}
	}
	return nil
}

// Localize resolves a system message id for the requested locale chain.
func (b *Bundle) Localize(messageID string, locales ...string) (string, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return "", fmt.Errorf("message id is required")
	}
	localizer := goi18n.NewLocalizer(b.bundle, locales...)
	return localizer.Localize(&goi18n.LocalizeConfig{MessageID: messageID})
}
