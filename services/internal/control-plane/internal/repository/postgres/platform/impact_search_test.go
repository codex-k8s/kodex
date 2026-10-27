package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestImpactSearchRejectsInvalidInput rejects oversized, NUL-containing or invalid UTF-8 searches
// across managed configuration, environment and secret impact before dereferencing the repository.
// TestImpactSearchRejectsInvalidInput отклоняет чрезмерный поиск, NUL и неверный UTF-8 в анализе
// влияния управляемой конфигурации, окружения и секрета до обращения к repository.
func TestImpactSearchRejectsInvalidInput(t *testing.T) {
	var repository *Repository
	for _, search := range []string{strings.Repeat("a", 201), "a\x00b", string([]byte{0xff})} {
		if _, err := repository.GetManagedConfigurationImpact(t.Context(), value.Principal{}, "", "", query.Filter{Query: search}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("managed search validation: %v", err)
		}
		if _, err := repository.GetRuntimeEnvironmentImpact(t.Context(), value.Principal{}, "", "", search, query.Page{}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("environment search validation: %v", err)
		}
		if _, err := repository.GetRuntimeSecretImpact(t.Context(), value.Principal{}, "", 1, search, query.Page{}); !errors.Is(err, errs.ErrInvalid) {
			t.Fatalf("secret search validation: %v", err)
		}
	}
}
