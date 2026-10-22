package platform

import (
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// normalizePlatformSearchQuery preserves the shared platform/VFS search rule: trim surrounding Unicode whitespace and require two through two hundred runes, returning ErrInvalid outside that range. It retains interior text and original bytes and does not perform Unicode normalization, UTF-8 validation, authorization or pagination changes.
// normalizePlatformSearchQuery сохраняет общее правило поиска платформы и VFS: убрать внешние Unicode-пробелы и потребовать от двух до двухсот рун с ErrInvalid вне диапазона. Внутренний текст и исходные байты сохраняются; Unicode-нормализация, проверка UTF-8, авторизация и изменение пагинации не выполняются.
func normalizePlatformSearchQuery(text string) (string, error) {
	text = strings.TrimSpace(text)
	if len([]rune(text)) < 2 || len([]rune(text)) > 200 {
		return "", errs.ErrInvalid
	}
	return text, nil
}
