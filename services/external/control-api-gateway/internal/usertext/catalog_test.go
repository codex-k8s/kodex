package usertext

import (
	"bufio"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestCatalogContainsTheSameResolvedMessagesForEveryLocale(t *testing.T) {
	t.Parallel()

	english := catalogKeys(t, "messages/problems.en.yaml")
	russian := catalogKeys(t, "messages/problems.ru.yaml")
	if !reflect.DeepEqual(english, russian) {
		t.Fatalf("locale catalogs contain different message identifiers\nenglish=%v\nrussian=%v", english, russian)
	}

	texts, err := New()
	if err != nil {
		t.Fatalf("load locale catalogs: %v", err)
	}
	for _, messageID := range english {
		for _, locale := range []string{"en", "ru"} {
			if localized := texts.Localize(locale, messageID, nil); localized == messageID || strings.TrimSpace(localized) == "" {
				t.Errorf("message %s is unresolved for locale %s", messageID, locale)
			}
		}
	}
}

func catalogKeys(t *testing.T, path string) []string {
	t.Helper()
	raw, err := messages.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	keys := make([]string, 0, 128)
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, " ") || !strings.HasSuffix(line, ":") {
			continue
		}
		key := strings.TrimSuffix(line, ":")
		if key == "" || strings.Trim(key, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
			t.Fatalf("invalid top-level message identifier %q in %s", key, path)
		}
		keys = append(keys, key)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	sort.Strings(keys)
	return keys
}
