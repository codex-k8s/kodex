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

// controlPlaneMessagePattern extracts literal i18n markers with uppercase/digit/underscore IDs from source bytes for catalog coverage checks; it is not a Go/SQL parser or runtime translation resolver.
// controlPlaneMessagePattern извлекает literal i18n markers с uppercase, digit и underscore IDs из source bytes для проверки catalog coverage; это не Go и SQL parser и не runtime translation resolver.
var controlPlaneMessagePattern = regexp.MustCompile(`i18n:([A-Z][A-Z0-9_]*)`)

// TestCatalogContainsTheSameResolvedMessagesForEveryLocale compares sorted catalog key lists and requires each key to resolve to nonblank non-ID text in both embedded locales. It checks resource coverage, not translation quality or every interpolation path.
// TestCatalogContainsTheSameResolvedMessagesForEveryLocale сравнивает отсортированные catalog key lists и требует разрешения каждого key в непустой отличный от ID текст обоих встроенных locales. Проверяется resource coverage, а не качество перевода и каждый interpolation path.
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

// TestCatalogContainsEveryStaticControlPlaneMessage locates the repository from the test file, scans control-plane Go/SQL source bytes for literal markers and reports sorted IDs absent from the embedded English catalog. It does not run SQL, interpret dynamic IDs or read runtime credentials.
// TestCatalogContainsEveryStaticControlPlaneMessage находит repository относительно test file, сканирует control-plane Go и SQL source bytes на literal markers и сообщает отсортированные IDs, отсутствующие во встроенном English catalog. SQL не исполняется, dynamic IDs не интерпретируются и runtime credentials не читаются.
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

// catalogKeys scans an embedded catalog for unindented colon-terminated uppercase/digit/underscore keys, rejects invalid candidates and returns a sorted list without deduplication. This unit helper is not a general YAML parser.
// catalogKeys сканирует встроенный catalog на неотступленные uppercase, digit и underscore keys с завершающим двоеточием, отклоняет неверные candidates и возвращает отсортированный список без deduplication. Этот unit helper не является общим YAML parser.
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

// stringSet copies strings into a fresh membership map, naturally collapsing duplicates while preserving exact spelling without normalization.
// stringSet копирует строки в новую membership map, естественно объединяя повторы и сохраняя точное написание без нормализации.
func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
