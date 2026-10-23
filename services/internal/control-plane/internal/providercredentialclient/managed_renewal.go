package providercredentialclient

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// invalidCredentialRenewal supplies the closed error text for malformed renewal bindings, digests and responses.
// invalidCredentialRenewal задаёт закрытый текст ошибки неверных привязок, дайджестов и ответов обновления учётных данных.
const invalidCredentialRenewal = "provider credential renewal binding is invalid"

// credentialRenewalBinding validates task/account/fence/credential/claimant pins, positive bounded versions, prior credential metadata and timestamp syntax.
// It constructs the exact renewal DTO without checking future expiry or independently granting owner authority.
// credentialRenewalBinding проверяет привязки задачи/аккаунта/ограждения/учётных данных/заявителя, положительные ограниченные версии, прежние метаданные и синтаксис временной метки.
// Создаётся точный DTO обновления без проверки срока в будущем или самостоятельной выдачи полномочий владельца.
func credentialRenewalBinding(task repository.ProviderCredentialRenewalTask) (*cp.ProviderCredentialRenewalBinding, error) {
	if !validProviderCredentialRef(task.Ref, "pcrt_") || !validProviderCredentialRef(task.AccountRef, "pacc_") ||
		!validProviderCredentialRef(task.Fence, "prf_") || !validBoundedSafeText(task.CredentialRef, 96) ||
		!validBoundedSafeText(task.ClaimantID, 128) || !validProviderCredentialDescriptor(task.Previous) || task.ExpiresAt.IsZero() {
		return nil, errors.New(invalidCredentialRenewal)
	}
	for _, version := range []int64{task.AccountVersion, task.CredentialRevision, task.Generation} {
		if version < 1 || version > 9007199254740991 {
			return nil, errors.New(invalidCredentialRenewal)
		}
	}
	expires := timestamppb.New(task.ExpiresAt)
	if expires.CheckValid() != nil {
		return nil, errors.New(invalidCredentialRenewal)
	}
	return &cp.ProviderCredentialRenewalBinding{TaskRef: task.Ref, AccountRef: task.AccountRef, AccountVersion: task.AccountVersion,
		CredentialRevisionRef: task.CredentialRef, CredentialRevision: task.CredentialRevision, ClaimantId: task.ClaimantID, ClaimGeneration: task.Generation,
		Fence: task.Fence, ExpiresAt: expires, PreviousCredential: &cp.ProviderCredentialDescriptor{SecretName: task.Previous.SecretName,
			SecretUid: task.Previous.SecretUID, SecretResourceVersion: task.Previous.SecretResourceVersion, ContentSha256: task.Previous.ContentSHA256}}, nil
}

// CredentialRenewalRequestDigests deterministically hashes the exact writer request, then the owner-authorization envelope containing that writer digest.
// Invalid binding or serialization returns no usable digest pair; hashing does not issue authorization or mutate the task.
// CredentialRenewalRequestDigests детерминированно хеширует точный запрос записи, затем оболочку авторизации владельца с этим дайджестом.
// Неверная привязка или сериализация не возвращает пригодную пару дайджестов; хеширование не выдаёт разрешение и не меняет задачу.
func (client *Client) CredentialRenewalRequestDigests(task repository.ProviderCredentialRenewalTask) (string, string, error) {
	binding, err := credentialRenewalBinding(task)
	if err != nil {
		return "", "", err
	}
	writerDigest, err := deterministicRequestDigest(&cp.RenewManagedProviderCredentialRequest{Binding: binding})
	if err != nil {
		return "", "", errors.New(invalidCredentialRenewal)
	}
	ownerDigest, err := deterministicRequestDigest(&cp.AuthorizeProviderCredentialRenewalRequest{Binding: binding, RenewalRequestSha256: writerDigest})
	if err != nil {
		return "", "", errors.New(invalidCredentialRenewal)
	}
	return writerDigest, ownerDigest, nil
}

// RenewManagedProviderCredential validates both pinned request digests before a single renewal RPC and rejects nil/unknown top-level response fields.
// It converts supported results without retrying an ambiguous external effect.
// RenewManagedProviderCredential проверяет оба закреплённых дайджеста запроса перед одним RPC обновления и отклоняет nil/неизвестные поля верхнего уровня ответа.
// Поддержанные результаты преобразуются без повтора неоднозначного внешнего действия.
func (client *Client) RenewManagedProviderCredential(ctx context.Context, task repository.ProviderCredentialRenewalTask) (repository.ProviderCredentialRenewalResult, error) {
	binding, err := client.validRenewalTask(task)
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	response, err := client.client.ProviderCredentials.RenewManagedProviderCredential(ctx, &cp.RenewManagedProviderCredentialRequest{Binding: binding})
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return repository.ProviderCredentialRenewalResult{}, errors.New(invalidCredentialRenewal)
	}
	return credentialRenewalResult(response.GetState(), response.GetCredential(), response.GetMaterializerReceiptUid(), response.GetMaterializerReceiptResourceVersion())
}

// ObserveManagedProviderCredentialRenewal verifies the exact task before observation and applies the same result conversion as renewal.
// After conversion it marks Recovered even when conversion returns an error; callers must honor that error rather than treating the flag as success.
// ObserveManagedProviderCredentialRenewal проверяет точную задачу перед наблюдением и применяет то же преобразование результата, что обновление.
// После преобразования ставится Recovered даже при его ошибке; потребитель обязан учитывать ошибку, а не считать признак успехом.
func (client *Client) ObserveManagedProviderCredentialRenewal(ctx context.Context, task repository.ProviderCredentialRenewalTask) (repository.ProviderCredentialRenewalResult, error) {
	binding, err := client.validRenewalTask(task)
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	response, err := client.client.ProviderCredentials.ObserveManagedProviderCredentialRenewal(ctx, &cp.ObserveManagedProviderCredentialRenewalRequest{Binding: binding})
	if err != nil {
		return repository.ProviderCredentialRenewalResult{}, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		return repository.ProviderCredentialRenewalResult{}, errors.New(invalidCredentialRenewal)
	}
	result, err := credentialRenewalResult(response.GetState(), response.GetCredential(), response.GetMaterializerReceiptUid(), response.GetMaterializerReceiptResourceVersion())
	result.Recovered = true
	return result, err
}

// validRenewalTask recomputes and compares writer and owner-envelope digests, then returns the validated binding.
// Equality preserves request pins but is not by itself transport identity or owner permission.
// validRenewalTask заново вычисляет и сравнивает дайджесты записи и оболочки владельца, затем возвращает проверенную привязку.
// Совпадение сохраняет привязки запроса, но само не является идентичностью транспорта или разрешением владельца.
func (client *Client) validRenewalTask(task repository.ProviderCredentialRenewalTask) (*cp.ProviderCredentialRenewalBinding, error) {
	writer, owner, err := client.CredentialRenewalRequestDigests(task)
	if err != nil || writer != task.RequestDigest || owner != task.OwnerRequestDigest {
		return nil, errors.New(invalidCredentialRenewal)
	}
	return credentialRenewalBinding(task)
}

// credentialRenewalResult accepts success only with valid credential and receipt UID/version metadata; running and unknown outcomes must carry no credential.
// Those uncertain states remain UNKNOWN_OUTCOME, with receipt fields forwarded without success validation; errors may retain partial receipt metadata and never imply successful renewal.
// credentialRenewalResult принимает успех только с корректными метаданными учётных данных и UID/версии квитанции; выполнение и неизвестный исход не должны содержать учётные данные.
// Неопределённые состояния остаются UNKNOWN_OUTCOME, а поля квитанции передаются без проверки успеха; ошибки могут сохранять частичные метаданные и никогда не означают успешное обновление.
func credentialRenewalResult(state cp.ProviderCredentialRenewalState, credential *cp.ProviderCredentialDescriptor, uid, version string) (repository.ProviderCredentialRenewalResult, error) {
	result := repository.ProviderCredentialRenewalResult{ReceiptUID: uid, ReceiptVersion: version}
	switch state {
	case cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_SUCCEEDED:
		if credential == nil || len(credential.ProtoReflect().GetUnknown()) != 0 {
			return result, errors.New(invalidCredentialRenewal)
		}
		value := entity.ProviderCredentialDescriptor{SecretName: credential.GetSecretName(), SecretUID: credential.GetSecretUid(), SecretResourceVersion: credential.GetSecretResourceVersion(), ContentSHA256: credential.GetContentSha256()}
		if !validProviderCredentialDescriptor(value) || !validProviderCredentialDescriptor(entity.ProviderCredentialDescriptor{SecretName: value.SecretName, SecretUID: uid, SecretResourceVersion: version, ContentSHA256: value.ContentSHA256}) {
			return result, errors.New(invalidCredentialRenewal)
		}
		result.State, result.Credential = "SUCCEEDED", &value
	case cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_RUNNING, cp.ProviderCredentialRenewalState_PROVIDER_CREDENTIAL_RENEWAL_STATE_UNKNOWN_OUTCOME:
		if credential != nil {
			return result, errors.New(invalidCredentialRenewal)
		}
		result.State = "UNKNOWN_OUTCOME"
	default:
		return result, errors.New(invalidCredentialRenewal)
	}
	return result, nil
}
