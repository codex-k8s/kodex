package integration

import (
	"strconv"
	"strings"
)

const (
	// jiraServerDefinition selects the separately versioned Server/DC contract, never a guessed Cloud fallback.
	// jiraServerDefinition выбирает отдельный Server/DC контракт, не угаданный fallback с Cloud.
	jiraServerDefinition = "jira-server-dc"
	// confluenceServerDefinition binds Server/DC routing to a validated published definition key.
	// confluenceServerDefinition связывает Server/DC маршрут с проверенным ключом опубликованного определения.
	confluenceServerDefinition = "confluence-server-dc"
)

// atlassianServerContext accepts an explicit root slash or bounded unescaped path segments from Connection configuration.
// It cannot introduce an authority, query, fragment, traversal or an ambiguous trailing slash into a fixed API route.
// atlassianServerContext принимает явный корень или ограниченные сегменты пути из конфигурации Connection.
// Он запрещает authority, query, fragment, traversal и неоднозначный конечный slash перед фиксированным API-маршрутом.
func atlassianServerContext(configuration map[string]string) (string, error) {
	value := configuration["context_path"]
	if value == "/" {
		return "", nil
	}
	if len(value) < 2 || len(value) > 128 || !strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	for _, segment := range strings.Split(value[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
		}
		for _, character := range segment {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || strings.ContainsRune("._~-", character)) {
				return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
			}
		}
	}
	return value, nil
}

// atlassianServerOffset parses only a canonical bounded decimal offset; an opaque Cloud cursor never changes API mode.
// atlassianServerOffset разбирает только ограниченное десятичное смещение; Cloud cursor не переключает режим API.
func atlassianServerOffset(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(cursor)
	if err != nil || value < 0 || value > 10000 || strconv.Itoa(value) != cursor {
		return 0, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return value, nil
}

// atlassianServerNext validates one live offset page without claiming snapshot consistency across later calls.
// A contradictory empty page or an unrepresentable successor fails closed rather than silently truncating results.
// atlassianServerNext проверяет одну страницу без обещания snapshot между последующими вызовами.
// Противоречивая пустая страница и непредставимое продолжение закрыто отклоняются вместо скрытого усечения.
func atlassianServerNext(offset, count, limit, total int) (string, error) {
	if offset < 0 || offset > 10000 || count < 0 || limit < 1 || limit > 50 || count > limit || total < 0 ||
		(count > 0 && total < offset+count) || count == 0 && total > offset {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	next := offset + count
	if count > 0 && next < total {
		if next > 10000 {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return strconv.Itoa(next), nil
	}
	return "", nil
}
