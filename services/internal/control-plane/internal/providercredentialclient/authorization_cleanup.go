package providercredentialclient

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/google/uuid"
)

// validAuthorizationCleanupIdentity checks bounded task/account/authorization references and a positive generation,
// then requires the exact materializer locator derived from the attempt/account pair; it does not authorize cleanup.
// validAuthorizationCleanupIdentity проверяет ограниченные ссылки задачи/аккаунта/авторизации и положительное поколение,
// затем требует точный указатель размещения, выведенный из пары попытки и аккаунта; очистка этим не авторизуется.
func validAuthorizationCleanupIdentity(target entity.ProviderAuthorizationCleanupTarget) bool {
	digest := sha256.Sum256([]byte(target.AuthorizationAttemptRef + "\x00" + target.AccountRef))
	return validProviderCredentialRef(target.TaskRef, "pcct_") &&
		validProviderCredentialRef(target.AccountRef, "pacc_") &&
		validProviderCredentialRef(target.AuthorizationAttemptRef, "pauth_") && target.Generation > 0 &&
		target.MaterializerAttemptRef == "pmat_"+hex.EncodeToString(digest[:16])
}

// ObserveAuthorizationCleanup sends a metadata-only request with exact owner pins and rejects mixed pending/absence identities or ordinary authorization fields.
// It distinguishes present, unfenced absence and confirmed absence, returning validated metadata and any separately produced credential descriptor without deleting it.
// ObserveAuthorizationCleanup отправляет запрос только метаданных с точными привязками владельца и отклоняет смешанные идентичности наличия/отсутствия или обычные поля авторизации.
// Различаются наличие, неограждённое и подтверждённое отсутствие; возвращаются проверенные метаданные и отдельно созданный дескриптор учётных данных без его удаления.
func (client *Client) ObserveAuthorizationCleanup(ctx context.Context, target entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupObservation, error) {
	var result entity.ProviderAuthorizationCleanupObservation
	if !validAuthorizationCleanupIdentity(target) || target.UID != "" || target.ResourceVersion != "" || target.Kind != "" {
		return result, errors.New("provider authorization metadata request is invalid")
	}
	response, err := client.client.ProviderCredentials.ObserveDeviceAuthorization(ctx,
		&controlplanev1.ProviderCredentialMaterializerServiceObserveDeviceAuthorizationRequest{
			MaterializerAttemptRef: target.MaterializerAttemptRef,
			Mode:                   controlplanev1.ProviderAuthorizationObservationMode_PROVIDER_AUTHORIZATION_OBSERVATION_MODE_METADATA_ONLY,
			AccountRef:             target.AccountRef, AuthorizationAttemptRef: target.AuthorizationAttemptRef,
			TaskRef: target.TaskRef, LeaseGeneration: target.Generation,
		})
	if err != nil {
		return result, err
	}
	if response == nil || response.GetExternalAccountMasked() != "" || response.GetSafeFailureCode() != "" ||
		response.GetState() != controlplanev1.ProviderAuthorizationState_PROVIDER_AUTHORIZATION_STATE_UNSPECIFIED {
		return result, errors.New("provider authorization metadata response is invalid")
	}
	switch response.GetObjectState() {
	case controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_PRESENT:
		value := response.GetPendingObject()
		if value == nil || response.GetAbsentObject() != nil || value.GetAccountRef() != target.AccountRef ||
			value.GetAuthorizationAttemptRef() != target.AuthorizationAttemptRef || value.GetMaterializerAttemptRef() != target.MaterializerAttemptRef {
			return result, errors.New("provider authorization metadata binding is invalid")
		}
		uid, uidErr := uuid.Parse(value.GetUid())
		if uidErr != nil || uid.String() != value.GetUid() || !validBoundedSafeText(value.GetResourceVersion(), maximumProviderCredentialResourceVersionBytes) {
			return result, errors.New("provider authorization metadata pins are invalid")
		}
		target.UID, target.ResourceVersion, target.Kind = value.GetUid(), value.GetResourceVersion(), "AUTHORIZATION_ATTEMPT"
		result.State = "PRESENT"
	case controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_ABSENT_UNFENCED,
		controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_CONFIRMED_ABSENT:
		value := response.GetAbsentObject()
		if value == nil || response.GetPendingObject() != nil || value.GetAccountRef() != target.AccountRef ||
			value.GetAuthorizationAttemptRef() != target.AuthorizationAttemptRef || value.GetMaterializerAttemptRef() != target.MaterializerAttemptRef {
			return result, errors.New("provider authorization absence binding is invalid")
		}
		target.Kind, result.State = "AUTHORIZATION_ABSENCE", "ABSENT_UNFENCED"
		if response.GetObjectState() == controlplanev1.ProviderAuthorizationObjectState_PROVIDER_AUTHORIZATION_OBJECT_STATE_CONFIRMED_ABSENT {
			result.State = "CONFIRMED_ABSENT"
		}
	default:
		return result, errors.New("provider authorization metadata state is invalid")
	}
	result.Target, result.ProducedCredential = target, credentialDescriptor(response.GetCredential())
	if result.ProducedCredential != nil && !validProviderCredentialDescriptor(*result.ProducedCredential) {
		return entity.ProviderAuthorizationCleanupObservation{}, errors.New("provider authorization produced credential is invalid")
	}
	return result, nil
}

// CleanupAuthorization validates the target identity, optional recovery and kind-specific UID/version or absence pins before one broker RPC.
// It returns bounded terminal-receipt text and any valid produced-credential descriptor; retries, signature verification and deletion of that produced credential are not performed here.
// CleanupAuthorization проверяет идентичность цели, необязательное восстановление и привязки UID/версии либо отсутствия по виду перед одним RPC брокера.
// Возвращаются ограниченный текст итоговой квитанции и корректный дескриптор созданных учётных данных; повторы, проверка подписи и удаление этих учётных данных здесь не выполняются.
func (client *Client) CleanupAuthorization(ctx context.Context, target entity.ProviderAuthorizationCleanupTarget) (entity.ProviderAuthorizationCleanupResult, error) {
	var result entity.ProviderAuthorizationCleanupResult
	if !validAuthorizationCleanupIdentity(target) {
		return result, errors.New("provider authorization cleanup identity is invalid")
	}
	request := &controlplanev1.ProviderCredentialMaterializerServiceCleanupProviderCredentialRequest{
		TaskRef: target.TaskRef, AccountRef: target.AccountRef, LeaseGeneration: target.Generation,
	}
	var err error
	request.RecoveryIdentity, err = cleanupRecoveryIdentity(target.Recovery)
	if err != nil {
		return result, err
	}
	switch target.Kind {
	case "AUTHORIZATION_ATTEMPT":
		uid, err := uuid.Parse(target.UID)
		if err != nil || uid.String() != target.UID || !validBoundedSafeText(target.ResourceVersion, maximumProviderCredentialResourceVersionBytes) {
			return result, errors.New("provider authorization cleanup pins are invalid")
		}
		request.TargetKind = controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ATTEMPT
		request.PendingObject = &controlplanev1.ProviderAuthorizationObjectDescriptor{
			MaterializerAttemptRef: target.MaterializerAttemptRef, AccountRef: target.AccountRef,
			AuthorizationAttemptRef: target.AuthorizationAttemptRef, Uid: target.UID, ResourceVersion: target.ResourceVersion,
		}
	case "AUTHORIZATION_ABSENCE":
		if target.UID != "" || target.ResourceVersion != "" {
			return result, errors.New("provider authorization absence pins are invalid")
		}
		request.TargetKind = controlplanev1.ProviderCredentialCleanupTargetKind_PROVIDER_CREDENTIAL_CLEANUP_TARGET_KIND_AUTHORIZATION_ABSENCE
		request.AbsentObject = &controlplanev1.ProviderAuthorizationAbsenceDescriptor{
			MaterializerAttemptRef: target.MaterializerAttemptRef, AccountRef: target.AccountRef, AuthorizationAttemptRef: target.AuthorizationAttemptRef,
		}
	default:
		return result, errors.New("provider authorization cleanup target is invalid")
	}
	response, err := client.client.ProviderCredentials.CleanupProviderCredential(ctx, request)
	if err != nil {
		return result, err
	}
	result.TerminalReceipt, result.ProducedCredential = response.GetTerminalReceipt(), credentialDescriptor(response.GetProducedCredential())
	if !validBoundedSafeText(result.TerminalReceipt, maximumProviderCredentialTerminalReceiptBytes) ||
		(result.ProducedCredential != nil && !validProviderCredentialDescriptor(*result.ProducedCredential)) {
		return entity.ProviderAuthorizationCleanupResult{}, errors.New("provider authorization cleanup receipt is invalid")
	}
	return result, nil
}

// cleanupRecoveryIdentity preserves nil or maps a validated recovery task/generation and bounded historical generation range.
// A nonzero historical range requires generation one; this serialization check is not proof of recovery lineage or an owner grant.
// cleanupRecoveryIdentity сохраняет nil либо преобразует проверенные задачу/поколение восстановления и ограниченный исторический диапазон поколений.
// Ненулевой исторический диапазон требует первого поколения; проверка сериализации не доказывает происхождение восстановления или разрешение владельца.
func cleanupRecoveryIdentity(value *entity.ProviderCleanupRecoveryIdentity) (*controlplanev1.ProviderCredentialCleanupRecoveryIdentity, error) {
	if value == nil {
		return nil, nil
	}
	if !validProviderCredentialRef(value.TaskRef, "pcct_") || value.Generation < 1 || value.LegacyLastGeneration < 0 || value.LegacyLastGeneration > 32 || value.LegacyLastGeneration > 0 && value.Generation != 1 {
		return nil, errors.New("provider cleanup recovery identity is invalid")
	}
	return &controlplanev1.ProviderCredentialCleanupRecoveryIdentity{TaskRef: value.TaskRef, LeaseGeneration: value.Generation, LegacyLastGeneration: value.LegacyLastGeneration}, nil
}
