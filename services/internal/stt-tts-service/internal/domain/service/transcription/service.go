// Package transcription реализует stateless-сценарий распознавания речи.
package transcription

import (
	"context"
	"crypto/subtle"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// maximumTranscriptBytes rejects oversized provider text before returning a result.
// maximumTranscriptBytes отклоняет чрезмерный текст провайдера до выдачи результата.
const maximumTranscriptBytes = 256 << 10

// Service orchestrates one stateless STT request across verified policy,
// complete audio validation, managed credentials and a bounded provider call.
// RU: Service проводит один STT-запрос без собственного состояния через
// проверенную политику, полную проверку аудио, управляемые реквизиты и ограниченный вызов модели.
type Service struct {
	policies    PolicyResolver
	credentials CredentialProjector
	provider    Provider
	decoder     AudioDecoder
	observer    Observer
	now         func() time.Time
	requestTTL  time.Duration
}

// Input carries the verified principal and seekable audio for one exact request;
// the service still rechecks permission, expiry and request binding.
// RU: Input содержит проверенного субъекта и аудио с перемоткой для одного
// точного запроса; сервис повторно проверяет допуск, срок и привязку запроса.
type Input struct {
	Principal                value.Principal
	RequestID, CorrelationID string
	AudioReader              interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	}
	AudioSizeBytes int64
	MediaType      string
}

// New rejects missing ports and a request lifetime outside the bounded service range.
// New отклоняет отсутствующие порты и срок запроса вне ограниченного диапазона сервиса.
func New(policies PolicyResolver, credentials CredentialProjector, provider Provider, observer Observer, requestTTL time.Duration, decoder AudioDecoder) (*Service, error) {
	if policies == nil || credentials == nil || provider == nil || observer == nil || decoder == nil || requestTTL < time.Second || requestTTL > 20*time.Second {
		return nil, errors.New("transcription service configuration is invalid")
	}
	return &Service{policies: policies, credentials: credentials, provider: provider, decoder: decoder, observer: observer, now: time.Now, requestTTL: requestTTL}, nil
}

// Transcribe checks the authority pins, resolves and validates policy, decodes
// the whole audio, projects a matching credential and bounds the provider call
// by the earliest relevant expiry; failure never returns a partial transcript.
// RU: Transcribe проверяет закреплённые полномочия, получает и валидирует политику,
// декодирует всё аудио, получает совпадающий реквизит и ограничивает вызов модели
// ближайшим сроком истечения; при отказе частичный текст не возвращается.
func (service *Service) Transcribe(ctx context.Context, input Input) (value.TranscriptionResult, error) {
	fail := func(stage value.Stage, class value.ErrorClass, err error) (value.TranscriptionResult, error) {
		service.observer.Observe(stage, class)
		return value.TranscriptionResult{}, err
	}
	if ctx == nil || input.RequestID == "" || input.CorrelationID == "" || input.Principal.RequestID != input.RequestID ||
		input.Principal.Permission != value.PermissionTranscribe || input.Principal.ActorID == "" ||
		input.Principal.TenantID == "" || input.Principal.AuthorityRevision == 0 ||
		!validSHA256(input.Principal.AuthorityDigestSHA256) || !service.now().Before(input.Principal.ExpiresAt) {
		return fail(value.StageAuthority, value.ErrorDenied, errs.ErrPermissionDenied)
	}
	requestDeadline := service.now().Add(service.requestTTL)
	if input.Principal.ExpiresAt.Before(requestDeadline) {
		requestDeadline = input.Principal.ExpiresAt
	}
	requestCtx, cancelRequest := context.WithDeadline(ctx, requestDeadline)
	defer cancelRequest()
	policy, err := service.policies.Resolve(requestCtx, input.Principal, input.RequestID, input.CorrelationID)
	if err != nil {
		return fail(value.StagePolicy, projectionClass(err), errors.Join(errs.ErrPolicyUnavailable, err))
	}
	if err := validatePolicy(policy, service.now()); err != nil {
		return fail(value.StagePolicy, value.ErrorRejected, err)
	}
	policyCtx, cancelPolicy := context.WithDeadline(requestCtx, policy.ExpiresAt)
	defer cancelPolicy()
	audio, err := ValidateAudio(policyCtx, input.AudioReader, input.AudioSizeBytes, input.MediaType, policy.MaximumAudioBytes, policy.MaximumAudioDuration, service.decoder)
	if err != nil {
		return fail(value.StageAudio, value.ErrorInvalid, err)
	}
	credential, err := service.credentials.Project(policyCtx, input.Principal, input.RequestID, input.CorrelationID, policy)
	if err != nil {
		return fail(value.StageCredential, projectionClass(err), errors.Join(errs.ErrCredentialUnavailable, err))
	}
	defer clear(credential.APIKey)
	if err := validateCredential(credential, policy, service.now()); err != nil {
		return fail(value.StageCredential, value.ErrorRejected, err)
	}
	credentialCtx, cancelCredential := context.WithDeadline(policyCtx, credential.ExpiresAt)
	defer cancelCredential()
	providerCtx, cancelProvider := context.WithTimeout(credentialCtx, policy.ProviderTimeout)
	defer cancelProvider()
	transcript, err := service.provider.Transcribe(providerCtx, value.ProviderRequest{Audio: audio, Model: policy.Model, Language: policy.Language, Parameters: policy.Parameters, APIKey: credential.APIKey})
	if err != nil {
		stage, class := value.StageProvider, value.ErrorUnavailable
		if errors.Is(err, errs.ErrEgressUnavailable) {
			stage = value.StageEgress
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(providerCtx.Err(), context.DeadlineExceeded) {
			class = value.ErrorTimeout
		} else if errors.Is(err, errs.ErrProviderRejected) {
			class = value.ErrorRejected
		}
		return fail(stage, class, err)
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" || len(transcript) > maximumTranscriptBytes {
		return fail(value.StageProvider, value.ErrorRejected, errs.ErrProviderUnavailable)
	}
	service.observer.Observe(value.StageSuccess, value.ErrorNone)
	return value.TranscriptionResult{Text: transcript, Receipt: value.TranscriptionReceipt{
		RequestID: input.RequestID, CorrelationID: input.CorrelationID,
		ActorID: input.Principal.ActorID, TenantID: input.Principal.TenantID, ProjectID: input.Principal.ProjectID,
		AuthoritySourceRevision:     input.Principal.AuthorityRevision,
		AuthoritySourceDigestSHA256: input.Principal.AuthorityDigestSHA256,
		ConfigRevision:              policy.Revision, ConfigDigestSHA256: policy.DigestSHA256,
		Model: policy.Model, Language: policy.Language, ProviderAccountRef: policy.ProviderAccountRef,
		ProviderCredentialGeneration: policy.ProviderCredentialGeneration, CompletedStage: value.StageSuccess,
	}}, nil
}

// CheckLocal checks decoder and provider adapter prerequisites without proving
// delegated policy, credential or a successful external provider request.
// RU: CheckLocal проверяет локальные зависимости декодера и адаптера модели,
// не доказывая делегированную политику, реквизит или успешный внешний вызов.
func (service *Service) CheckLocal(ctx context.Context) error {
	if ctx == nil {
		return errors.New("transcription local readiness context is required")
	}
	return errors.Join(service.decoder.CheckLocal(ctx), service.provider.CheckLocal(ctx))
}

// CheckProtectedPath can verify a local policy adapter path, but deliberately
// remains pending because it cannot prove an end-to-end delegated user request.
// RU: CheckProtectedPath может проверить локальный путь адаптера политики, но
// остаётся в ожидании: он не доказывает полный делегированный запрос пользователя.
func (service *Service) CheckProtectedPath(ctx context.Context) error {
	checker, ok := service.policies.(interface{ Check(context.Context) error })
	if !ok {
		return errs.ErrDelegatedProofPending
	}
	if err := checker.Check(ctx); err != nil {
		return err
	}
	// Локальный issuer не доказывает user policy, credential или provider path.
	return errs.ErrDelegatedProofPending
}

// Availability describes the furthest checked boundary and the earliest expiry
// of a non-billable provider readiness observation.
// RU: Availability содержит достигнутую границу проверки и ближайший срок
// действия наблюдения готовности провайдера без платного распознавания.
type Availability struct {
	Stage      value.Stage
	ValidUntil time.Time
}

// Catalog returns an optional local provider catalog or an empty value when
// the adapter has no catalog capability; it does not perform remote discovery.
// RU: Catalog возвращает необязательный локальный каталог адаптера либо пустое
// значение без этой возможности; удалённое обнаружение не выполняется.
func (service *Service) Catalog() modelprofile.Catalog {
	if provider, ok := service.provider.(interface{ Catalog() modelprofile.Catalog }); ok {
		return provider.Catalog()
	}
	return modelprofile.Catalog{}
}

// CheckAvailability uses the same verified root authority as Transcribe,
// but accepts no audio and calls only a bounded model probe, never recognition;
// its validity ends at the earliest principal, policy or credential expiry.
// RU: CheckAvailability действует под тем же проверенным корневым полномочием,
// что Transcribe, но не принимает аудио и вызывает только ограниченную проверку
// модели; срок результата не позже истечения субъекта, политики или реквизита.
func (service *Service) CheckAvailability(ctx context.Context, principal value.Principal, correlationID string) (Availability, error) {
	if ctx == nil || principal.Permission != value.PermissionTranscribe || principal.ActorID == "" || principal.TenantID == "" ||
		principal.RequestID == "" || correlationID == "" || principal.AuthorityRevision == 0 || !validSHA256(principal.AuthorityDigestSHA256) || !service.now().Before(principal.ExpiresAt) {
		return Availability{Stage: value.StageAuthority}, errs.ErrPermissionDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx, cancelAuthority := context.WithDeadline(ctx, principal.ExpiresAt)
	defer cancelAuthority()
	if err := service.CheckLocal(ctx); err != nil {
		return Availability{Stage: value.StageAudio}, err
	}
	policy, err := service.policies.Resolve(ctx, principal, principal.RequestID, correlationID)
	if err != nil {
		return Availability{Stage: value.StagePolicy}, err
	}
	if err := validatePolicy(policy, service.now()); err != nil {
		return Availability{Stage: value.StagePolicy}, err
	}
	ctx, cancelPolicy := context.WithDeadline(ctx, policy.ExpiresAt)
	defer cancelPolicy()
	credential, err := service.credentials.Project(ctx, principal, principal.RequestID, correlationID, policy)
	defer clear(credential.APIKey)
	if err != nil {
		return Availability{Stage: value.StageCredential}, err
	}
	if err := validateCredential(credential, policy, service.now()); err != nil {
		return Availability{Stage: value.StageCredential}, err
	}
	ctx, cancelCredential := context.WithDeadline(ctx, credential.ExpiresAt)
	defer cancelCredential()
	probe, ok := service.provider.(interface {
		CheckModel(context.Context, string, []byte) error
	})
	if !ok {
		return Availability{Stage: value.StageProvider}, errs.ErrProviderUnavailable
	}
	if err := probe.CheckModel(ctx, policy.Model, credential.APIKey); err != nil {
		stage := value.StageProvider
		if errors.Is(err, errs.ErrEgressUnavailable) {
			stage = value.StageEgress
		}
		return Availability{Stage: stage}, err
	}
	validUntil := service.now().Add(30 * time.Second)
	for _, expiry := range []time.Time{principal.ExpiresAt, policy.ExpiresAt, credential.ExpiresAt} {
		if expiry.Before(validUntil) {
			validUntil = expiry
		}
	}
	if !service.now().Before(validUntil) {
		return Availability{Stage: value.StageCredential}, errs.ErrGrantRevoked
	}
	return Availability{Stage: value.StageSuccess, ValidUntil: validUntil}, nil
}

// projectionClass keeps revoked or pending authority distinct from temporary
// projection failure while exposing only a closed error class to telemetry.
// RU: projectionClass отличает отозванное или ожидающее полномочие от временного
// сбоя проекции, передавая наблюдаемости только закрытый класс ошибки.
func projectionClass(err error) value.ErrorClass {
	if errors.Is(err, errs.ErrGrantRevoked) || errors.Is(err, errs.ErrDelegatedProofPending) {
		return value.ErrorDenied
	}
	return value.ErrorUnavailable
}

// validatePolicy rejects stale or inconsistent model, account, limit and
// digest pins before any credential projection or billable provider call.
// RU: validatePolicy отклоняет устаревшие или несогласованные параметры модели,
// аккаунта, лимитов и дайджеста до получения реквизита и платного вызова.
func validatePolicy(policy value.Policy, now time.Time) error {
	if policy.Revision == 0 || !validSHA256(policy.DigestSHA256) || modelprofile.Validate(policy.Model, policy.Language, policy.Parameters) != nil ||
		policy.MaximumAudioBytes < modelprofile.MinimumAudioBytes || policy.MaximumAudioBytes > modelprofile.MaximumAudioBytes ||
		policy.MaximumAudioDuration < modelprofile.MinimumAudioDuration || policy.MaximumAudioDuration > modelprofile.MaximumAudioDuration ||
		policy.ProviderTimeout < modelprofile.MinimumProviderTimeout || policy.ProviderTimeout > modelprofile.MaximumProviderTimeout ||
		policy.ProviderAccountRef == "" || len(policy.ProviderAccountRef) > 96 || policy.ProviderCredentialGeneration == 0 || !now.Before(policy.ExpiresAt) {
		return errs.ErrGrantRevoked
	}
	return nil
}

// validateCredential requires the exact policy account, credential generation
// and digest, plus unexpired independent policy and credential lifetimes.
// RU: validateCredential требует точного совпадения аккаунта, поколения
// реквизита и дайджеста политики, а также действующих независимых сроков.
func validateCredential(credential value.Credential, policy value.Policy, now time.Time) error {
	if len(credential.APIKey) < 8 || len(credential.APIKey) > 16<<10 || credential.ProviderAccountRef != policy.ProviderAccountRef ||
		credential.ProviderCredentialGeneration != policy.ProviderCredentialGeneration ||
		subtle.ConstantTimeCompare([]byte(credential.ConfigDigestSHA256), []byte(policy.DigestSHA256)) != 1 ||
		!now.Before(credential.ExpiresAt) || !now.Before(policy.ExpiresAt) {
		return errs.ErrGrantRevoked
	}
	// Policy и credential получены по отдельным continuation одного родителя.
	// Их сроки могут различаться; вложенные contexts ограничивают вызов минимумом.
	return nil
}

// validSHA256 accepts only 64 lowercase hexadecimal characters for a pinned digest.
// validSHA256 допускает для закреплённого дайджеста только 64 строчные шестнадцатеричные цифры.
func validSHA256(input string) bool {
	if len(input) != 64 {
		return false
	}
	for _, character := range input {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}
