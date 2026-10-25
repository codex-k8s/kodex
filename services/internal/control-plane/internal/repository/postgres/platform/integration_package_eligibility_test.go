package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// TestAssistantRecipientCatalogUsesExactSourceRegistryPins loads shipped integration definitions
// and requires deterministic closed version/digest tuples for every source entry. An absent source
// registry must be Unavailable rather than an empty successful catalog; no remote package or
// credentials are read.
// TestAssistantRecipientCatalogUsesExactSourceRegistryPins читает shipped integration definitions
// и требует deterministic closed version/digest tuples каждой source entry. Отсутствующий source
// registry даёт Unavailable вместо успешного пустого catalog; remote package и credentials не
// читаются.
func TestAssistantRecipientCatalogUsesExactSourceRegistryPins(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{integrationDefinitions: definitions}
	encoded, err := repository.assistantRecipientCatalogShippedRevisions()
	if err != nil {
		t.Fatal(err)
	}
	var pins map[string]map[string]string
	if json.Unmarshal([]byte(encoded), &pins) != nil || len(pins) != len(definitions) {
		t.Fatal("source registry pins lost canonical closed shape")
	}
	for key, definition := range definitions {
		if len(pins[key]) != 2 || pins[key]["version"] != definition.Metadata.Version || pins[key]["digest"] != definition.Digest {
			t.Fatal("source registry tuple changed")
		}
	}
	if repeated, err := repository.assistantRecipientCatalogShippedRevisions(); err != nil || repeated != encoded {
		t.Fatal("source registry pins are nondeterministic")
	}
	if _, err := (&Repository{}).assistantRecipientCatalogShippedRevisions(); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing source registry became an empty successful catalog")
	}
}

// TestAssistantRecipientCatalogReadsRetiredPackageWithoutExecution creates an exact parsed
// historical GitHub package with incompatible review output bounds, allows its metadata read and
// keeps execution Forbidden without an unavailable-package marker.
// Digest/version/definition/content/format/backend failures remain closed under table tests; no
// provider action is executed.
// TestAssistantRecipientCatalogReadsRetiredPackageWithoutExecution создаёт exact parsed historical
// GitHub package с несовместимыми review output bounds, разрешает metadata read и сохраняет
// execution Forbidden без unavailable-package marker.
// Digest/version/definition/content/format/backend failures закрыто отклоняются table tests;
// provider action не выполняется.
func TestAssistantRecipientCatalogReadsRetiredPackageWithoutExecution(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	repository := &Repository{integrationDefinitions: definitions}
	current := definitions["github"]
	previous, err := integrationpackage.Parse(asJSON(current))
	if err != nil {
		t.Fatal(err)
	}
	previous.Metadata.Version, previous.Metadata.Origin = "2.4.0", integrationpackage.OriginUI
	for index := range previous.Spec.Capabilities {
		if previous.Spec.Capabilities[index].Key == "github.pull_request.review.create" {
			for field := range previous.Spec.Capabilities[index].OutputFields {
				if previous.Spec.Capabilities[index].OutputFields[field].Key == "id" {
					previous.Spec.Capabilities[index].OutputFields[field].Maximum = 2147483647
				}
			}
		}
	}
	previous, err = integrationpackage.Parse(asJSON(previous))
	if err != nil || integrationpackage.ValidateExecutableRevision(previous, current) == nil {
		t.Fatal("fixture did not retain exact incompatible review output contract")
	}
	fixture := packageEligibilityFixture{format: "JSON", content: string(asJSON(previous))}
	read, executable, err := repository.assistantRecipientCatalogPackage(t.Context(), fixture, "tenant", "int_fixture01", "github", previous.Metadata.Version, previous.Digest)
	if err != nil || executable || read.Digest != previous.Digest || len(read.Spec.Capabilities) != len(previous.Spec.Capabilities) {
		t.Fatal("exact retired package metadata read lost closed eligibility", err)
	}
	if _, err := repository.integrationPackage(t.Context(), fixture, "tenant", "int_fixture01", "github", previous.Metadata.Version, previous.Digest); !errors.Is(err, errs.ErrForbidden) || errors.Is(err, errIntegrationPackageUnavailable) {
		t.Fatal("metadata read weakened existing execution rejection", err)
	}
	for _, item := range []struct {
		name, key, version, digest string
		fixture                    packageEligibilityFixture
		code                       error
	}{
		{"digest mismatch", "github", previous.Metadata.Version, strings.Repeat("9", 64), fixture, errs.ErrForbidden},
		{"version mismatch", "github", "2.3.0", previous.Digest, fixture, errs.ErrForbidden},
		{"unknown definition", "unknown", previous.Metadata.Version, previous.Digest, fixture, errs.ErrForbidden},
		{"malformed published content", "github", previous.Metadata.Version, previous.Digest, packageEligibilityFixture{format: "JSON", content: "{"}, errs.ErrForbidden},
		{"unknown content format", "github", previous.Metadata.Version, previous.Digest, packageEligibilityFixture{format: "UNKNOWN", content: fixture.content}, errs.ErrForbidden},
		{"SQL unavailable", "github", previous.Metadata.Version, previous.Digest, packageEligibilityFixture{err: errors.New("database read failed")}, errs.ErrUnavailable},
	} {
		t.Run(item.name, func(t *testing.T) {
			_, executable, err := repository.assistantRecipientCatalogPackage(t.Context(), item.fixture, "tenant", "int_fixture01", item.key, item.version, item.digest)
			if executable || !errors.Is(err, item.code) {
				t.Fatal("invalid metadata read became unavailable success", err)
			}
		})
	}
}

// packageEligibilityFixture supplies a fixed format/content row or configured database error to
// package readers. It deliberately omits real SQL predicates, tenant isolation and transaction
// semantics, which these unit tests cannot establish.
// packageEligibilityFixture передаёт fixed format/content row либо configured DB error package
// readers. Намеренно не моделирует реальные SQL predicates, tenant isolation и transaction
// semantics, которые эти unit tests не доказывают.
type packageEligibilityFixture struct {
	format, content string
	err             error
}

// Query rejects every multi-row query with an explicit unexpected-query error. Only the fixture
// single-row path is supported; nil rows must not be consumed after the error.
// Query отклоняет любой multi-row query явной unexpected-query error. Поддерживает только fixture
// single-row path; nil rows нельзя использовать после ошибки.
func (fixture packageEligibilityFixture) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

// QueryRow returns the fixture itself as a row regardless of SQL, context or parameters. This
// isolates reader parsing/error classification and does not verify query binding or resource
// ownership.
// QueryRow возвращает саму fixture как row независимо от SQL, context либо parameters. Изолирует
// reader parsing/error classification и не проверяет query binding либо resource ownership.
func (fixture packageEligibilityFixture) QueryRow(context.Context, string, ...any) pgx.Row {
	return fixture
}

// Scan returns the configured error first or assigns format/content into two assumed string
// pointers. It has no destination validation or database side effects.
// Scan сначала возвращает configured error либо назначает format/content двум ожидаемым string
// pointers. Не проверяет destinations и не создаёт DB side effects.
func (fixture packageEligibilityFixture) Scan(targets ...any) error {
	if fixture.err != nil {
		return fixture.err
	}
	*targets[0].(*string), *targets[1].(*string) = fixture.format, fixture.content
	return nil
}

// TestIntegrationPackageEligibilityMarkerDoesNotHideCorruption requires only a missing/unbound
// package row to carry the unavailable eligibility marker alongside Forbidden. SQL failure,
// malformed content and found-but-mismatched pins retain their distinct errors without that
// marker, preventing corruption from becoming unsupported-package success.
// TestIntegrationPackageEligibilityMarkerDoesNotHideCorruption требует unavailable eligibility
// marker вместе с Forbidden только для missing/unbound package row. SQL failure, malformed content
// и found-but-mismatched pins сохраняют разные errors без marker, не превращая corruption в
// unsupported-package success.
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
