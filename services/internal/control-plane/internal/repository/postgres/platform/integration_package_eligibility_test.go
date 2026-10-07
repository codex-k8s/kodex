package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

type packageEligibilityFixture struct {
	format, content string
	err             error
}

func (fixture packageEligibilityFixture) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (fixture packageEligibilityFixture) QueryRow(context.Context, string, ...any) pgx.Row {
	return fixture
}

func (fixture packageEligibilityFixture) Scan(targets ...any) error {
	if fixture.err != nil {
		return fixture.err
	}
	*targets[0].(*string), *targets[1].(*string) = fixture.format, fixture.content
	return nil
}

func TestIntegrationPackageEligibilityMarkerDoesNotHideCorruption(t *testing.T) {
	t.Parallel()
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{integrationDefinitions: definitions}
	for _, item := range []struct {
		name    string
		fixture packageEligibilityFixture
		marker  bool
		code    error
	}{
		{"unresolved unbound", packageEligibilityFixture{err: pgx.ErrNoRows}, true, errs.ErrForbidden},
		{"SQL unavailable", packageEligibilityFixture{err: errors.New("database read failed")}, false, errs.ErrUnavailable},
		{"malformed JSON", packageEligibilityFixture{format: "JSON", content: "{"}, false, errs.ErrForbidden},
		{"found current pins mismatch", packageEligibilityFixture{format: "JSON", content: string(asJSON(definitions["github"]))}, false, errs.ErrForbidden},
	} {
		t.Run(item.name, func(t *testing.T) {
			_, err := repository.integrationPackage(t.Context(), item.fixture, "tenant", "int_fixture01", "github", "2.4.0", strings.Repeat("9", 64))
			if !errors.Is(err, item.code) || errors.Is(err, errIntegrationPackageUnavailable) != item.marker {
				t.Fatalf("package classification lost closed failure: %v", err)
			}
		})
	}
}
