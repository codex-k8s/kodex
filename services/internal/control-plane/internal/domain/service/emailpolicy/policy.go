package emailpolicy

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	// PermissionReconcile names the integration-management permission required by owner-side email
	// outcome reconciliation.
	// PermissionReconcile задаёт permission управления интеграцией для согласования исхода почтового
	// эффекта владельцем.
	PermissionReconcile = "integration.manage"
	// PermissionView names the integration-view permission used for authorized email integration
	// policy visibility.
	// PermissionView задаёт permission просмотра интеграции для разрешённой видимости policy почтовой
	// интеграции.
	PermissionView = "integration.view"
	// PermissionRunView names the run-view permission used when email execution evidence is exposed
	// through its owning run.
	// PermissionRunView задаёт permission просмотра запуска при выдаче свидетельств почтового
	// выполнения через его запуск-владелец.
	PermissionRunView = "run.view"
	// MaximumNoteRunes bounds a reconciliation note in Unicode code points rather than encoded bytes.
	// MaximumNoteRunes ограничивает примечание согласования в кодовых точках Unicode, а не
	// закодированных байтах.
	MaximumNoteRunes = 2000
	// MaximumEffectKeyBytes bounds an opaque email effect key by encoded byte length without imposing
	// a locator grammar.
	// MaximumEffectKeyBytes ограничивает непрозрачный ключ почтового эффекта длиной в байтах без
	// навязывания грамматики locator.
	MaximumEffectKeyBytes = 128
	// FreshAuthenticationMaximumAge sets the five-minute freshness window for verified interactive
	// authentication during reconciliation.
	// FreshAuthenticationMaximumAge задаёт пятиминутное окно свежести проверенной
	// interactive-аутентификации при согласовании.
	FreshAuthenticationMaximumAge = 5 * time.Minute
	// AuthorizationMaximumAge sets the two-minute maximum age used by owner-side email authorization
	// lifecycle checks.
	// AuthorizationMaximumAge задаёт максимальный возраст в две минуты для проверок lifecycle почтовой
	// авторизации владельцем.
	AuthorizationMaximumAge = 2 * time.Minute
	// OutcomeUnknown represents an email effect whose external outcome still requires authoritative
	// confirmation.
	// OutcomeUnknown обозначает почтовый эффект, внешний исход которого ещё требует авторитетного
	// подтверждения.
	OutcomeUnknown = "UNKNOWN_OUTCOME"
	// OutcomeEffectConfirmed represents authoritative confirmation that the external email effect
	// occurred.
	// OutcomeEffectConfirmed обозначает авторитетное подтверждение совершения внешнего почтового
	// эффекта.
	OutcomeEffectConfirmed = "EFFECT_CONFIRMED"
	// OutcomeNoEffectConfirmed represents authoritative confirmation that no external email effect
	// occurred.
	// OutcomeNoEffectConfirmed обозначает авторитетное подтверждение отсутствия внешнего почтового
	// эффекта.
	OutcomeNoEffectConfirmed = "NO_EFFECT_CONFIRMED"
)

var (
	// digestPattern requires exactly sixty-four lowercase hexadecimal characters for email semantic
	// and receipt digests.
	// digestPattern требует ровно шестьдесят четыре строчных шестнадцатеричных символа для почтовых
	// semantic digests и digests квитанций.
	digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// externalReceiptPattern requires exactly thirty-two lowercase hexadecimal characters for an
	// external email receipt reference.
	// externalReceiptPattern требует ровно тридцать два строчных шестнадцатеричных символа для ссылки
	// внешней почтовой квитанции.
	externalReceiptPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
)

// ValidDigest checks only the lowercase sixty-four-hex digest shape without proving its source,
// ownership or semantic input.
// ValidDigest проверяет только форму digest из шестидесяти четырёх строчных hex-символов без
// доказательства источника, владения либо semantic input.
func ValidDigest(digest string) bool { return digestPattern.MatchString(digest) }

// ValidateEffectKey accepts opaque nonempty UTF-8 keys through the byte limit and rejects NUL
// while preserving permitted whitespace and punctuation.
// ValidateEffectKey принимает непрозрачные непустые UTF-8 ключи до байтового лимита и отклоняет
// NUL с сохранением допустимых пробелов и пунктуации.
func ValidateEffectKey(key string) error {
	if len(key) == 0 || len(key) > MaximumEffectKeyBytes || !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
		return errs.ErrInvalid
	}
	return nil
}

// ValidateReceiptTransition rejects unknown target states, permits initial unknown, confirmation
// from unknown and identical replay, and conflicts on other confirmed-state changes.
// ValidateReceiptTransition отклоняет неизвестные целевые состояния, допускает начальное unknown,
// подтверждение из unknown и одинаковый replay, возвращая conflict для иных изменений
// подтверждённого состояния.
func ValidateReceiptTransition(previous, next string) error {
	switch next {
	case OutcomeUnknown, OutcomeEffectConfirmed, OutcomeNoEffectConfirmed:
	default:
		return errs.ErrInvalid
	}
	if previous == "" && next == OutcomeUnknown || previous == OutcomeUnknown || previous == next {
		return nil
	}
	return errs.ErrConflict
}

// ValidateExternalReceipt checks external receipt reference and semantic digest syntax together
// without authorizing or reading the receipt.
// ValidateExternalReceipt проверяет совместно синтаксис ссылки внешней квитанции и semantic digest
// без авторизации либо чтения квитанции.
func ValidateExternalReceipt(ref, digest string) error {
	if !externalReceiptPattern.MatchString(ref) || !ValidDigest(digest) {
		return errs.ErrInvalid
	}
	return nil
}

// ValidateReconciliation requires a valid digest and confirmed effect/no-effect outcome with a
// UTF-8 NUL-free note within the Unicode length limit.
// ValidateReconciliation требует правильный digest и подтверждённый исход наличия либо отсутствия
// эффекта с UTF-8 примечанием без NUL в лимите длины Unicode.
func ValidateReconciliation(digest, outcome, note string) error {
	if !ValidDigest(digest) || outcome != OutcomeEffectConfirmed && outcome != OutcomeNoEffectConfirmed ||
		!utf8.ValidString(note) || utf8.RuneCountInString(note) > MaximumNoteRunes || strings.ContainsRune(note, 0) {
		return errs.ErrInvalid
	}
	return nil
}

// RequireFreshAuthentication delegates verified interactive context freshness to Principal with
// the reconciliation age budget and returns the dedicated freshness error on rejection.
// RequireFreshAuthentication передаёт Principal проверку свежести подтверждённого
// interactive-контекста с бюджетом согласования и возвращает отдельную ошибку свежести при отказе.
func RequireFreshAuthentication(principal value.Principal, now time.Time) error {
	if !principal.InteractiveAuthenticationIsFresh(now, FreshAuthenticationMaximumAge) {
		return errs.ErrFreshAuthenticationRequired
	}
	return nil
}
