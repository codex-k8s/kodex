package authority

import (
	"context"
	"regexp"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// effectAuthorityTimeout caps each owner effect/report reconciliation RPC independently of the worker cycle budget.
// effectAuthorityTimeout ограничивает каждый owner RPC отчёта или сверки эффекта независимо от бюджета цикла worker.
const effectAuthorityTimeout = 3 * time.Second

var (
	// receiptRefPattern bounds owner receipt/decision references and report idempotency keys to the accepted opaque identifier grammar.
	// receiptRefPattern ограничивает owner receipt/decision и ключи идемпотентности отчётов допустимой грамматикой opaque-идентификатора.
	receiptRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	// externalRefPattern pins bridge-generated receipt IDs to exactly 16 bytes encoded as lowercase hexadecimal.
	// externalRefPattern закрепляет ID квитанций моста за ровно 16 байтами в шестнадцатеричном нижнем регистре.
	externalRefPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	// digestPattern accepts only bare lowercase SHA-256 text, without an algorithm prefix or alternate encoding.
	// digestPattern допускает только голый текст SHA-256 в нижнем регистре, без префикса алгоритма или иной кодировки.
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var _ receipt.EffectAuthority = (*Client)(nil)

// Report sends one exact invocation-bound effect observation and validates the owner's correlated receipt and nondecreasing version.
// Live reports inherit lease expiry; recovery requires the original expired binding and gets a fresh bounded RPC context, never a new effect grant.
// Report передаёт одно точное наблюдение эффекта с invocation и проверяет связанную квитанцию владельца и неубывающую версию.
// Живой отчёт наследует срок lease; восстановление требует исходный истёкший binding и получает новый ограниченный RPC-контекст, не grant нового эффекта.
func (c *Client) Report(ctx context.Context, input receipt.Report) (receipt.OwnerReceipt, error) {
	want := input.Receipt
	binding := Binding(input.Binding)
	outcome, ok := effectOutcome(want.Outcome)
	if c.API == nil || binding == nil || binding.Lease.ExpiresAt.CheckValid() != nil || binding.GetInvocationRef() != want.Invocation || want.Invocation == "" || !validReceipt(want, false) || !ok || !receiptRefPattern.MatchString(input.IdempotencyKey) {
		return receipt.OwnerReceipt{}, errs.Invalid
	}
	expired := !binding.Lease.ExpiresAt.AsTime().After(time.Now())
	if input.Replay != expired {
		return receipt.OwnerReceipt{}, errs.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, effectAuthorityTimeout)
	defer cancel()
	if !input.Replay {
		var cancelLease context.CancelFunc
		ctx, cancelLease = context.WithDeadline(ctx, binding.Lease.ExpiresAt.AsTime())
		defer cancelLease()
	}
	mutation := &cp.MutationContext{IdempotencyKey: input.IdempotencyKey}
	response, err := c.API.ReportEmailEffectReceipt(ctx, &cp.ReportEmailEffectReceiptRequest{Mutation: mutation, Binding: binding, ExternalReceiptRef: want.ExternalRef, ExternalReceiptDigest: want.ExternalDigest, SemanticInputDigest: want.InputDigest, Outcome: outcome})
	if err != nil {
		return receipt.OwnerReceipt{}, effectError(err)
	}
	got, ok := ownerReceipt(response.GetReceipt())
	if !ok || !sameReceipt(got, want) || got.Outcome != want.Outcome || (want.Ref != "" && got.Ref != want.Ref) || got.Version < want.Version {
		return receipt.OwnerReceipt{}, errs.Unavailable
	}
	return got, nil
}

// Reconcile resolves an unknown receipt's owner decision once and requires exact receipt version/digest/invocation and fresh conclusive decision metadata.
// An optional decision reference pins the recheck; missing decisions remain NotFound, and this adapter never resends mail.
// Reconcile однократно разрешает решение владельца по неопределённой квитанции и требует точные версию, digest, invocation и свежее определённое решение.
// Необязательная ссылка решения фиксирует повторную проверку; отсутствующее решение остаётся NotFound, а адаптер никогда не отправляет почту повторно.
func (c *Client) Reconcile(ctx context.Context, want receipt.OwnerReceipt, decisionRef string) (receipt.Decision, error) {
	if c.API == nil || !validReceipt(want, true) || want.Outcome != receipt.Unknown || (decisionRef != "" && !receiptRefPattern.MatchString(decisionRef)) {
		return receipt.Decision{}, errs.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, effectAuthorityTimeout)
	defer cancel()
	response, err := c.API.ResolveEmailReconciliation(ctx, &cp.ResolveEmailReconciliationRequest{ReceiptRef: want.Ref, DecisionRef: decisionRef, ExternalReceiptRef: want.ExternalRef, ExternalReceiptDigest: want.ExternalDigest})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return receipt.Decision{}, errs.NotFound
		}
		return receipt.Decision{}, effectError(err)
	}
	got, ok := ownerReceipt(response.GetReceipt())
	d := response.GetDecision()
	if !ok || !sameReceipt(got, want) || got.Ref != want.Ref || got.Version != want.Version || got.Outcome != receipt.Unknown || d == nil || !receiptRefPattern.MatchString(d.Ref) || (decisionRef != "" && d.Ref != decisionRef) || d.Version < 1 || d.ReceiptRef != got.Ref || d.ReceiptVersion != got.Version || d.ReceiptDigest != got.ExternalDigest || d.InvocationRef != got.Invocation || d.ActorRef == "" || d.GrantRef == "" || d.ExpiresAt == nil || d.ExpiresAt.CheckValid() != nil || !d.ExpiresAt.AsTime().After(time.Now()) || d.CreatedAt == nil || d.CreatedAt.CheckValid() != nil || d.CreatedAt.AsTime().After(time.Now()) || !d.ExpiresAt.AsTime().After(d.CreatedAt.AsTime()) {
		return receipt.Decision{}, errs.Unavailable
	}
	if d.Outcome != cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED && d.Outcome != cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED {
		return receipt.Decision{}, errs.Unavailable
	}
	outcome := receipt.EffectConfirmed
	if d.Outcome == cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED {
		outcome = receipt.NoEffectConfirmed
	}
	return receipt.Decision{Ref: d.Ref, Version: d.Version, Actor: d.ActorRef, Grant: d.GrantRef, Receipt: got, Outcome: outcome, ExpiresAt: d.ExpiresAt.AsTime()}, nil
}

// effectOutcome maps only the three domain evidence outcomes to their exact Proto values, rejecting every unknown string.
// effectOutcome переводит только три доменных исхода свидетельства в точные Proto-значения, отвергая любую неизвестную строку.
func effectOutcome(value receipt.Outcome) (cp.EmailEffectOutcome, bool) {
	switch value {
	case receipt.Unknown:
		return cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME, true
	case receipt.EffectConfirmed:
		return cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED, true
	case receipt.NoEffectConfirmed:
		return cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED, true
	default:
		return 0, false
	}
}

// ownerReceipt rejects nil/unknown enums, maps the safe owner receipt fields and validates owner reference/version syntax.
// ownerReceipt отвергает nil и неизвестные enum, преобразует безопасные поля квитанции и проверяет формат owner reference/version.
func ownerReceipt(r *cp.EmailEffectReceipt) (receipt.OwnerReceipt, bool) {
	if r == nil {
		return receipt.OwnerReceipt{}, false
	}
	outcome := receipt.Unknown
	switch r.Outcome {
	case cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_UNKNOWN_OUTCOME:
	case cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_EFFECT_CONFIRMED:
		outcome = receipt.EffectConfirmed
	case cp.EmailEffectOutcome_EMAIL_EFFECT_OUTCOME_NO_EFFECT_CONFIRMED:
		outcome = receipt.NoEffectConfirmed
	default:
		return receipt.OwnerReceipt{}, false
	}
	value := receipt.OwnerReceipt{Ref: r.Ref, Version: r.Version, Invocation: r.InvocationRef, ExternalRef: r.ExternalReceiptRef, ExternalDigest: r.ExternalReceiptDigest, InputDigest: r.SemanticInputDigest, EffectKey: r.EffectKey, Mailbox: r.MailboxRef, Connection: r.ConnectionRef, ConfigurationRevision: r.ConfigurationRevision, Outcome: outcome}
	return value, validReceipt(value, true)
}

// validReceipt checks effect identity grammar, required correlations and positive configuration revision, adding owner ref/version requirements when requested.
// It validates shape only; authenticated owner RPC and comparison to the expected receipt establish provenance separately.
// validReceipt проверяет грамматику эффекта, обязательные связи и положительную ревизию конфигурации, при необходимости требуя owner ref/version.
// Проверяется лишь форма; происхождение отдельно устанавливают аутентифицированный owner RPC и сравнение с ожидаемой квитанцией.
func validReceipt(r receipt.OwnerReceipt, owner bool) bool {
	return (!owner || receiptRefPattern.MatchString(r.Ref) && r.Version > 0) && externalRefPattern.MatchString(r.ExternalRef) && digestPattern.MatchString(r.ExternalDigest) && digestPattern.MatchString(r.InputDigest) && r.Invocation != "" && r.EffectKey != "" && len(r.EffectKey) <= 128 && r.Mailbox != "" && r.Connection != "" && r.ConfigurationRevision > 0
}

// sameReceipt compares immutable effect correlations only; callers separately check owner reference, version and outcome under their own rule.
// sameReceipt сравнивает только неизменяемые связи эффекта; owner reference, версию и исход вызывающий отдельно проверяет по своему правилу.
func sameReceipt(a, b receipt.OwnerReceipt) bool {
	return a.Invocation == b.Invocation && a.ExternalRef == b.ExternalRef && a.ExternalDigest == b.ExternalDigest && a.InputDigest == b.InputDigest && a.EffectKey == b.EffectKey && a.Mailbox == b.Mailbox && a.Connection == b.Connection && a.ConfigurationRevision == b.ConfigurationRevision
}

// effectError preserves denied authority as a domain denial and maps all other RPC failures to a safe unavailable sentinel.
// effectError сохраняет запрет полномочий как доменный отказ, а прочие RPC-ошибки переводит в безопасный признак недоступности.
func effectError(err error) error {
	if status.Code(err) == codes.PermissionDenied || status.Code(err) == codes.Unauthenticated {
		return errs.Denied
	}
	return errs.Unavailable
}
