package authorityclient

import api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"

// ProvenancePointer requires a concrete pointer with the locator's four provenance getters.
// The pointer constraint makes a typed nil comparable to nil without interface boxing or
// reflection. It describes readable values and does not establish an identity or authority.
// ProvenancePointer требует конкретный указатель с четырьмя getters происхождения locator.
// Ограничение указателя позволяет сравнить типизированный nil без упаковки в interface или
// reflection. Описывает читаемые значения, а не подтверждённую identity или authority.
type ProvenancePointer[T any] interface {
	*T
	// GetSource returns the source number without normalization or eligibility validation.
	// GetSource возвращает номер источника без нормализации и проверки eligibility.
	GetSource() int32
	// GetReference returns the exact source reference without resolving or trimming it.
	// GetReference возвращает точную ссылку источника без разрешения или TrimSpace.
	GetReference() string
	// GetRevision returns the source revision without imposing a new range restriction.
	// GetRevision возвращает ревизию источника без нового ограничения диапазона.
	GetRevision() uint64
	// GetDigestSha256 returns the existing digest text without decoding or rewriting it.
	// GetDigestSha256 возвращает существующий текст дайджеста без декодирования или перезаписи.
	GetDigestSha256() string
}

// MatchesProvenance requires both pointers and compares source, reference, revision and digest
// in that order, stopping at the first mismatch. Empty/default values and unknown source numbers
// retain their previous equality semantics. Shape, signature, identity, permission, expiry and
// grant validation remain with the caller; this predicate reads values and grants no authority.
// MatchesProvenance требует оба указателя и сравнивает source, reference, revision и digest в
// таком порядке, останавливаясь при первом несовпадении. Пустые/default значения и неизвестные
// номера источника сохраняют прежнее равенство. Проверки формы, подписи, identity, permission,
// срока и grant остаются у вызывающего; предикат читает значения и не выдаёт полномочий.
func MatchesProvenance[T any, P ProvenancePointer[T]](locator P, verified *api.AuthorityProvenance) bool {
	return locator != nil && verified != nil && locator.GetSource() == int32(verified.GetSource()) &&
		locator.GetReference() == verified.GetReference() && locator.GetRevision() == verified.GetRevision() && locator.GetDigestSha256() == verified.GetDigestSha256()
}
