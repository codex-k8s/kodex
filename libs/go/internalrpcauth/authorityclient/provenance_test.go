package authorityclient_test

import (
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/protobuf/proto"
)

// provenanceFixture holds synthetic provenance values and records getter order. Its getters
// intentionally dereference the receiver so an incorrect typed-nil guard fails the test.
// provenanceFixture хранит синтетические provenance values и порядок getters. Getters намеренно
// разыменовывают receiver: неправильная проверка типизированного nil приводит к ошибке теста.
type provenanceFixture struct {
	source    int32
	reference string
	revision  uint64
	digest    string
	accessed  []string
}

// GetSource records the source read and returns its unchanged fixture value.
// GetSource отмечает чтение source и возвращает неизменённое тестовое значение.
func (value *provenanceFixture) GetSource() int32 {
	value.accessed = append(value.accessed, "source")
	return value.source
}

// GetReference records the reference read without resolving or normalizing it.
// GetReference отмечает чтение reference без разрешения или нормализации.
func (value *provenanceFixture) GetReference() string {
	value.accessed = append(value.accessed, "reference")
	return value.reference
}

// GetRevision records the revision read without validating its range.
// GetRevision отмечает чтение revision без проверки диапазона.
func (value *provenanceFixture) GetRevision() uint64 {
	value.accessed = append(value.accessed, "revision")
	return value.revision
}

// GetDigestSha256 records the digest read and returns the supplied synthetic text verbatim.
// GetDigestSha256 отмечает чтение digest и возвращает переданный синтетический текст без изменений.
func (value *provenanceFixture) GetDigestSha256() string {
	value.accessed = append(value.accessed, "digest")
	return value.digest
}

// matchingProvenanceFixture creates independent matching locator/expected records with fresh
// getter trace storage; it neither verifies a context nor creates an authority grant.
// matchingProvenanceFixture создаёт независимые совпадающие locator/expected records с новым
// хранилищем порядка getters; не проверяет контекст и не создаёт grant полномочий.
func matchingProvenanceFixture() (*provenanceFixture, *api.AuthorityProvenance) {
	return &provenanceFixture{source: int32(api.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE), reference: "fixture/ref", revision: 7, digest: "fixture-digest"},
		&api.AuthorityProvenance{Source: api.AuthoritySource_AUTHORITY_SOURCE_DOMAIN_STATE, Reference: "fixture/ref", Revision: 7, DigestSha256: "fixture-digest"}
}

// TestMatchesProvenanceExactFields rejects each isolated expected-record mismatch while keeping
// the matching case; these synthetic values exercise equality, not signed-context validation.
// TestMatchesProvenanceExactFields отклоняет каждое отдельное несовпадение ожидаемой записи и
// сохраняет положительный случай; синтетические значения проверяют равенство, а не подпись контекста.
func TestMatchesProvenanceExactFields(t *testing.T) {
	locator, expected := matchingProvenanceFixture()
	if !authorityclient.MatchesProvenance(locator, expected) {
		t.Fatal("matching provenance was rejected")
	}
	for _, test := range []struct {
		name   string
		mutate func(*api.AuthorityProvenance)
	}{
		{"source", func(value *api.AuthorityProvenance) { value.Source++ }},
		{"reference", func(value *api.AuthorityProvenance) { value.Reference += "/other" }},
		{"revision", func(value *api.AuthorityProvenance) { value.Revision++ }},
		{"digest", func(value *api.AuthorityProvenance) { value.DigestSha256 += "-other" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := proto.Clone(expected).(*api.AuthorityProvenance)
			test.mutate(changed)
			if authorityclient.MatchesProvenance(locator, changed) {
				t.Fatal("different provenance field was accepted")
			}
		})
	}
}

// TestMatchesProvenancePreservesGetterOrder proves typed nil/absent expected values prevent all
// getter calls and that field comparisons stop in the original order at the first mismatch.
// TestMatchesProvenancePreservesGetterOrder доказывает отсутствие вызовов getters при typed nil
// или отсутствующей expected записи и прежнюю остановку на первом несовпадении в исходном порядке.
func TestMatchesProvenancePreservesGetterOrder(t *testing.T) {
	locator, expected := matchingProvenanceFixture()
	var absent *provenanceFixture
	if authorityclient.MatchesProvenance(absent, expected) || authorityclient.MatchesProvenance(absent, nil) || authorityclient.MatchesProvenance(locator, nil) || len(locator.accessed) != 0 {
		t.Fatal("absent provenance reached a getter or comparison")
	}
	ordered := []string{"source", "reference", "revision", "digest"}
	for step, name := range []string{"source", "reference", "revision", "digest", "equal"} {
		t.Run(name, func(t *testing.T) {
			value := &api.AuthorityProvenance{}
			if step > 0 {
				value.Source = expected.Source
			}
			if step > 1 {
				value.Reference = expected.Reference
			}
			if step > 2 {
				value.Revision = expected.Revision
			}
			if step > 3 {
				value.DigestSha256 = expected.DigestSha256
			}
			locator.accessed = nil
			got := authorityclient.MatchesProvenance(locator, value)
			if got != (step == 4) || !reflect.DeepEqual(locator.accessed, ordered[:min(step+1, len(ordered))]) {
				t.Fatal("provenance comparison order changed")
			}
		})
	}
}

// TestMatchesProvenancePreservesValueOnlyComparison keeps the previous equality of nonnil empty
// records and unknown source numbers. Such a match does not validate an authority source.
// TestMatchesProvenancePreservesValueOnlyComparison сохраняет прежнее равенство существующих
// пустых записей и неизвестных номеров source. Совпадение не подтверждает допустимый источник прав.
func TestMatchesProvenancePreservesValueOnlyComparison(t *testing.T) {
	for _, source := range []api.AuthoritySource{0, 999, -1} {
		locator := &provenanceFixture{source: int32(source)}
		if !authorityclient.MatchesProvenance(locator, &api.AuthorityProvenance{Source: source}) {
			t.Fatal("value-only comparison changed")
		}
	}
}
