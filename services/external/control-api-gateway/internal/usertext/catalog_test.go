package usertext

import (
	"bufio"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var controlPlaneMessagePattern = regexp.MustCompile(`i18n:([A-Z][A-Z0-9_]*)`)

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

func TestCatalogContainsEveryStaticControlPlaneMessage(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve user text test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../../../"))
	controlPlaneRoot := filepath.Join(root, "services/internal/control-plane")
	available := stringSet(catalogKeys(t, "messages/problems.en.yaml"))
	used := map[string]struct{}{}
	err := filepath.WalkDir(controlPlaneRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" && filepath.Ext(path) != ".sql" {
			return nil
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range controlPlaneMessagePattern.FindAllSubmatch(raw, -1) {
			used[string(match[1])] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan control-plane user text identifiers: %v", err)
	}
	missing := make([]string, 0)
	for messageID := range used {
		if _, ok := available[messageID]; !ok {
			missing = append(missing, messageID)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("control-plane messages are absent from the owner locale catalog: %v", missing)
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

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
