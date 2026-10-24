package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

const (
	// maximumProviderAPIKeyBytes limits provider API-key material to sixteen KiB before principal
	// resolution, durable reservation or external materialization.
	// maximumProviderAPIKeyBytes ограничивает API key провайдера шестнадцатью KiB до разрешения
	// principal, устойчивого резервирования либо внешней материализации.
	maximumProviderAPIKeyBytes = 16 << 10
	// providerCompensationTimeBudget bounds reference readback and exact materialization discard
	// together to fifteen seconds independently of request cancellation.
	// providerCompensationTimeBudget ограничивает совместный бюджет чтения ссылки и точного удаления
	// материала пятнадцатью секундами независимо от отмены запроса.
	providerCompensationTimeBudget = 15 * time.Second
)

// ProviderDeviceAuthorizationMaterialization carries materializer attempt identity/version, device
// verification instructions and expiry for owner persistence without storing API-key plaintext
// here.
// ProviderDeviceAuthorizationMaterialization содержит identity и версию попытки materializer,
// инструкции проверки устройства и срок для сохранения владельцем без хранения plaintext API key
// здесь.
type ProviderDeviceAuthorizationMaterialization struct {
	MaterializerAttemptRef, VerificationURI, UserCode  string
	MaterializerAttemptUID, MaterializerAttemptVersion string
	ExpiresAt                                          time.Time
}

// ProviderMaterializationDiscard pins the exact account/authorization attempt and either
// materializer attempt identity or credential descriptor eligible for compensating cleanup after
// owner readback.
// ProviderMaterializationDiscard закрепляет точные аккаунт и попытку авторизации и identity
// попытки materializer либо дескриптор реквизитов для компенсирующей очистки после чтения
// владельца.
type ProviderMaterializationDiscard struct {
	AttemptRef, AccountRef                         string
	MaterializerAttemptRef, MaterializerAttemptUID string
	MaterializerAttemptVersion                     string
	Credential                                     *entity.ProviderCredentialDescriptor
}

// ProviderAuthorizationObservation carries the observed device-authorization state, masked
// account, safe failure code and optional immutable credential descriptor.
// ProviderAuthorizationObservation содержит наблюдаемое состояние авторизации устройства, скрытый
// аккаунт, безопасный код ошибки и необязательный immutable-дескриптор реквизитов.
type ProviderAuthorizationObservation struct {
	State, ExternalAccountMasked, SafeFailureCode string
	Credential                                    *entity.ProviderCredentialDescriptor
}

// ProviderCredentialMaterializer defines the optional provider materialization boundary; it
// creates or observes external material while durable reservations, authority and ownership remain
// with control-plane.
// ProviderCredentialMaterializer задаёт необязательную границу материализации провайдера; создаёт
// либо наблюдает внешний материал, а устойчивые reservations, полномочия и владение остаются у
// control-plane.
type ProviderCredentialMaterializer interface {
	// Check checks the provider materializer dependency path independently of repository-owned global
	// control-plane readiness.
	// Check проверяет путь зависимости materializer провайдера независимо от общей готовности
	// control-plane, принадлежащей репозиторию.
	Check(context.Context) error
	// StartDeviceAuthorization starts materialization for a reserved authorization attempt and
	// account, returning the exact external attempt metadata.
	// StartDeviceAuthorization начинает материализацию для зарезервированной попытки авторизации и
	// аккаунта, возвращая точные метаданные внешней попытки.
	StartDeviceAuthorization(context.Context, string, string) (ProviderDeviceAuthorizationMaterialization, error)
	// ObserveDeviceAuthorization observes the exact materializer attempt and returns safe
	// authorization state and an optional credential descriptor.
	// ObserveDeviceAuthorization наблюдает точную попытку materializer и возвращает безопасное
	// состояние авторизации и необязательный дескриптор реквизитов.
	ObserveDeviceAuthorization(context.Context, string) (ProviderAuthorizationObservation, error)
	// MaterializeAPIKey materializes the supplied mutable key bytes for the reserved attempt/account
	// and returns a descriptor and masked account rather than raw key material.
	// MaterializeAPIKey материализует переданные изменяемые байты ключа для зарезервированных попытки
	// и аккаунта и возвращает дескриптор и скрытый аккаунт вместо исходного ключа.
	MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error)
	// Discard removes only the exactly pinned materializer attempt or credential selected by
	// compensating owner readback.
	// Discard удаляет только точно закреплённую попытку materializer либо реквизиты, выбранные
	// компенсирующим чтением владельца.
	Discard(context.Context, ProviderMaterializationDiscard) error
}

// providerMaterializationReferenceReader provides authoritative owner reference checks used to
// preserve material after ambiguous commits rather than guessing whether cleanup is safe.
// providerMaterializationReferenceReader предоставляет авторитетную проверку ссылок владельца для
// сохранения материала после неопределённого commit вместо предположений о допустимости очистки.
type providerMaterializationReferenceReader interface {
	// ProviderMaterializationReferenced reports whether owner state references the exact account,
	// authorization/materializer attempt and credential descriptor before any compensation.
	// ProviderMaterializationReferenced сообщает о ссылке состояния владельца на точные аккаунт,
	// попытки авторизации и materializer и дескриптор реквизитов до компенсации.
	ProviderMaterializationReferenced(
		context.Context,
		value.Principal,
		string,
		string,
		string,
		*entity.ProviderCredentialDescriptor,
	) (bool, error)
}

// providerAuthorizationReserver requires owner-side durable reservation and replay/version
// handling before external provider materialization starts.
// providerAuthorizationReserver требует устойчивого резервирования и обработки replay и версии
// владельцем до начала внешней материализации провайдера.
type providerAuthorizationReserver interface {
	// ReserveProviderAuthorization reserves the exact account/method request digest under the resolved
	// principal and mutation, returning a stable attempt, reserved version or applied replay.
	// ReserveProviderAuthorization резервирует точный дайджест запроса аккаунта и метода для
	// разрешённого principal и изменения, возвращая устойчивую попытку, reserved version либо
	// применённый replay.
	ReserveProviderAuthorization(context.Context, value.Principal, value.Mutation, string, string, string) (entity.ProviderAuthorizationReservation, error)
}

// reserveProviderAuthorization resolves principal, requires both reservation and materializer
// ports and binds account, method and content hash into the durable owner reservation request.
// reserveProviderAuthorization разрешает principal, требует порты резервирования и materializer и
// связывает аккаунт, метод и хеш содержимого в запросе устойчивого резервирования владельца.
func (service *Service) reserveProviderAuthorization(ctx context.Context, principal value.Principal, mutation value.Mutation, accountRef, method string, content []byte) (value.Principal, entity.ProviderAuthorizationReservation, error) {
	resolved, err := service.principal(ctx, principal)
	if err != nil {
		return value.Principal{}, entity.ProviderAuthorizationReservation{}, err
	}
	reserver, ok := service.repository.(providerAuthorizationReserver)
	if !ok || service.providerCredentialMaterializer == nil {
		return resolved, entity.ProviderAuthorizationReservation{}, errs.ErrUnavailable
	}
	contentDigest := sha256.Sum256(content)
	requestDigest := sha256.Sum256([]byte(accountRef + "\x00" + method + "\x00" + hex.EncodeToString(contentDigest[:])))
	reservation, err := reserver.ReserveProviderAuthorization(ctx, resolved, mutation, accountRef, method, hex.EncodeToString(requestDigest[:]))
	return resolved, reservation, err
}

// WithProviderCredentialMaterializer installs the optional provider materializer without probing
// it or changing the repository-owned readiness dependency.
// WithProviderCredentialMaterializer назначает необязательный materializer провайдера без его
// проверки и изменения зависимости готовности, принадлежащей репозиторию.
func WithProviderCredentialMaterializer(materializer ProviderCredentialMaterializer) Option {
	return func(service *Service) { service.providerCredentialMaterializer = materializer }
}

// StartProviderAccountDeviceAuthorization reserves authorization before starting device flow,
// skips materialization for applied replay and commits exact attempt metadata with authoritative
// compensation on failure.
// StartProviderAccountDeviceAuthorization резервирует авторизацию до device flow, пропускает
// материализацию применённого replay и сохраняет точные метаданные попытки с авторитетной
// компенсацией при ошибке.
func (service *Service) StartProviderAccountDeviceAuthorization(
	ctx context.Context,
	principal value.Principal,
	mutation value.Mutation,
	accountRef string,
) (entity.ProviderAccount, error) {
	principal, reservation, err := service.reserveProviderAuthorization(ctx, principal, mutation, accountRef, "DEVICE_CODE", nil)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if reservation.Applied {
		return service.repository.GetProviderAccount(ctx, principal, accountRef)
	}
	mutation.ExpectedVersion = &reservation.ReservedVersion
	attemptRef := reservation.AttemptRef
	materialized, err := service.providerCredentialMaterializer.StartDeviceAuthorization(ctx, attemptRef, accountRef)
	if err != nil {
		return entity.ProviderAccount{}, errors.Join(errs.ErrUnavailable, err)
	}
	result, err := service.executeResolved(ctx, command.Command{
		Kind: command.StartProviderDeviceAuth, Principal: principal, Mutation: mutation,
		Payload: command.ProviderAccountInput{
			AccountRef: accountRef, AuthorizationRef: attemptRef, AuthorizationMethod: "DEVICE_CODE",
			AuthorizationState: "PENDING", MaterializerAttemptRef: materialized.MaterializerAttemptRef,
			MaterializerAttemptUID:             materialized.MaterializerAttemptUID,
			MaterializerAttemptResourceVersion: materialized.MaterializerAttemptVersion,
			VerificationURI:                    materialized.VerificationURI, UserCode: materialized.UserCode,
			AuthorizationExpiresAt: &materialized.ExpiresAt,
		},
	})
	if err != nil {
		cleanupErr := service.compensateProviderMaterialization(ctx, principal, ProviderMaterializationDiscard{
			AttemptRef: attemptRef, AccountRef: accountRef,
			MaterializerAttemptRef:     materialized.MaterializerAttemptRef,
			MaterializerAttemptUID:     materialized.MaterializerAttemptUID,
			MaterializerAttemptVersion: materialized.MaterializerAttemptVersion,
		})
		return entity.ProviderAccount{}, errors.Join(err, cleanupErr)
	}
	return providerAccountResult(result, err)
}

// AuthorizeProviderAccountAPIKey validates exact key bytes, reserves the operation and clears its
// copied buffer, preserving referenced material or compensating only after authoritative
// nonreference on commit failure.
// AuthorizeProviderAccountAPIKey проверяет точные байты ключа, резервирует операцию и очищает свою
// копию буфера, сохраняя используемый материал либо компенсируя только после авторитетного
// отсутствия ссылки при ошибке commit.
func (service *Service) AuthorizeProviderAccountAPIKey(
	ctx context.Context,
	principal value.Principal,
	mutation value.Mutation,
	accountRef string,
	apiKey []byte,
) (entity.ProviderAccount, error) {
	if len(apiKey) < 8 || len(apiKey) > maximumProviderAPIKeyBytes ||
		len(bytes.TrimSpace(apiKey)) != len(apiKey) || bytes.IndexAny(apiKey, "\r\n\x00") >= 0 {
		return entity.ProviderAccount{}, errs.ErrInvalid
	}
	principal, reservation, err := service.reserveProviderAuthorization(ctx, principal, mutation, accountRef, "API_KEY", apiKey)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if reservation.Applied {
		return service.repository.GetProviderAccount(ctx, principal, accountRef)
	}
	mutation.ExpectedVersion = &reservation.ReservedVersion
	attemptRef := reservation.AttemptRef
	credentialCopy := append([]byte(nil), apiKey...)
	defer clear(credentialCopy)
	descriptor, masked, err := service.providerCredentialMaterializer.MaterializeAPIKey(ctx, attemptRef, accountRef, credentialCopy)
	if err != nil {
		return entity.ProviderAccount{}, errors.Join(errs.ErrUnavailable, err)
	}
	result, err := service.executeResolved(ctx, command.Command{
		Kind: command.AuthorizeProviderAPIKey, Principal: principal, Mutation: mutation,
		Payload: command.ProviderAccountInput{
			AccountRef: accountRef, AuthorizationRef: attemptRef, AuthorizationMethod: "API_KEY",
			AuthorizationState: "AUTHORIZED", ExternalAccountMasked: masked, Credential: &descriptor,
		},
	})
	if err != nil {
		cleanupErr := service.compensateProviderMaterialization(ctx, principal, ProviderMaterializationDiscard{
			AttemptRef: attemptRef, AccountRef: accountRef, Credential: &descriptor,
		})
		return entity.ProviderAccount{}, errors.Join(err, cleanupErr)
	}
	return providerAccountResult(result, err)
}

// compensateProviderMaterialization uses a fresh bounded uncancelled context to read exact owner
// references; missing reader preserves material, read failure returns unavailable and only
// confirmed nonreference permits discard.
// compensateProviderMaterialization читает точные ссылки владельца в новом ограниченном контексте
// без отмены запроса; отсутствие reader сохраняет материал, ошибка чтения даёт unavailable, и
// только подтверждённое отсутствие ссылки разрешает discard.
func (service *Service) compensateProviderMaterialization(
	ctx context.Context,
	principal value.Principal,
	target ProviderMaterializationDiscard,
) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), providerCompensationTimeBudget)
	defer cancel()
	referenceReader, ok := service.repository.(providerMaterializationReferenceReader)
	if !ok {
		return nil
	}
	referenced, err := referenceReader.ProviderMaterializationReferenced(
		cleanup,
		principal,
		target.AccountRef,
		target.AttemptRef,
		target.MaterializerAttemptRef,
		target.Credential,
	)
	if err != nil {
		return errors.Join(errs.ErrUnavailable, err)
	}
	if referenced {
		return nil
	}
	if err := service.providerCredentialMaterializer.Discard(cleanup, target); err != nil {
		return errors.Join(errs.ErrUnavailable, err)
	}
	return nil
}

// RefreshProviderAccountAuthorization reads exact owner/version pins, observes only a pending
// device attempt and commits its safe state, compensating newly observed credentials after failed
// commit only through owner readback.
// RefreshProviderAccountAuthorization читает точные pins владельца и версии, наблюдает только
// ожидающую device-попытку и сохраняет безопасное состояние, компенсируя новые реквизиты после
// ошибки commit только через чтение владельца.
func (service *Service) RefreshProviderAccountAuthorization(
	ctx context.Context,
	principal value.Principal,
	mutation value.Mutation,
	accountRef string,
) (entity.ProviderAccount, error) {
	principal, account, err := service.providerAccountForAuthorization(ctx, principal, mutation, accountRef)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if service.providerCredentialMaterializer == nil || account.Authorization == nil ||
		account.Authorization.Method != "DEVICE_CODE" || account.Authorization.MaterializerAttemptRef == "" ||
		account.Authorization.State != "PENDING" {
		return entity.ProviderAccount{}, errs.ErrConflict
	}
	observed, err := service.providerCredentialMaterializer.ObserveDeviceAuthorization(ctx, account.Authorization.MaterializerAttemptRef)
	if err != nil {
		return entity.ProviderAccount{}, errors.Join(errs.ErrUnavailable, err)
	}
	result, err := service.executeResolved(ctx, command.Command{
		Kind: command.RefreshProviderAuthorization, Principal: principal, Mutation: mutation,
		Payload: command.ProviderAccountInput{
			AccountRef: accountRef, AuthorizationRef: account.Authorization.Ref,
			AuthorizationMethod: "DEVICE_CODE", AuthorizationState: observed.State,
			MaterializerAttemptRef: account.Authorization.MaterializerAttemptRef,
			ExternalAccountMasked:  observed.ExternalAccountMasked, SafeFailureCode: observed.SafeFailureCode,
			Credential: observed.Credential,
		},
	})
	if err != nil && observed.Credential != nil {
		cleanupErr := service.compensateProviderMaterialization(ctx, principal, ProviderMaterializationDiscard{
			AttemptRef: account.Authorization.Ref, AccountRef: accountRef, Credential: observed.Credential,
		})
		return entity.ProviderAccount{}, errors.Join(err, cleanupErr)
	}
	return providerAccountResult(result, err)
}

// providerAccountForAuthorization validates mutation shape, resolves principal and reads the
// owner-scoped account before comparing its exact expected version.
// providerAccountForAuthorization проверяет форму изменения, разрешает principal и читает аккаунт
// в области владельца до сравнения точной ожидаемой версии.
func (service *Service) providerAccountForAuthorization(
	ctx context.Context,
	principal value.Principal,
	mutation value.Mutation,
	accountRef string,
) (value.Principal, entity.ProviderAccount, error) {
	if mutation.ExpectedVersion == nil || strings.TrimSpace(accountRef) == "" ||
		len(mutation.IdempotencyKey) < 8 || len(mutation.IdempotencyKey) > 128 {
		return value.Principal{}, entity.ProviderAccount{}, errs.ErrInvalid
	}
	resolved, err := service.principal(ctx, principal)
	if err != nil {
		return value.Principal{}, entity.ProviderAccount{}, err
	}
	account, err := service.repository.GetProviderAccount(ctx, resolved, accountRef)
	if err != nil {
		return value.Principal{}, entity.ProviderAccount{}, err
	}
	if account.Version != *mutation.ExpectedVersion {
		return value.Principal{}, entity.ProviderAccount{}, errs.ErrVersionMismatch
	}
	return resolved, account, nil
}

// providerAuthorizationRef derives a stable prefixed reference from account, idempotency key and
// method using the first sixteen SHA-256 bytes without granting authority.
// providerAuthorizationRef получает устойчивую ссылку с префиксом из аккаунта, ключа
// идемпотентности и метода по первым шестнадцати байтам SHA-256 без выдачи полномочий.
func providerAuthorizationRef(accountRef, idempotencyKey, method string) string {
	digest := sha256.Sum256([]byte(accountRef + "\x00" + idempotencyKey + "\x00" + method))
	return "pauth_" + hex.EncodeToString(digest[:16])
}

// providerAccountResult preserves a command error and requires an explicit provider-account result
// rather than fabricating a successful empty account.
// providerAccountResult сохраняет ошибку команды и требует явный результат аккаунта провайдера
// вместо создания успешного пустого аккаунта.
func providerAccountResult(result command.Result, err error) (entity.ProviderAccount, error) {
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if result.ProviderAccount == nil {
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	return *result.ProviderAccount, nil
}
