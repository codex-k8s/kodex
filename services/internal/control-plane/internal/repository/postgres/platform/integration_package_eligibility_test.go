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
