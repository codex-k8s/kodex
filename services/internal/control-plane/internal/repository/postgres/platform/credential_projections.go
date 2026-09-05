package platform

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	runtimeProjectionMethod   = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeRuntimeCredentials"
	assistantProjectionMethod = "/secretbroker.v1.RuntimeCredentialProjectionService/MaterializeSystemAssistantCredentials"
	sttProjectionMethod       = "/stt.v1.TranscriptionCredentialProjectionService/ProjectTranscriptionCredential"
	maximumProjectionItems    = 64
)

func (repository *Repository) ResolveRuntimeCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.RuntimeCredentialProjectionInput) (platformrepo.RuntimeCredentialProjection, error) {
	if !validCredentialProjectionOwner(principal, "platform.credential-projections.runtime.resolve") ||
		!validRuntimeProjectionAuthority(input.Authority) || input.Fence == "" {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrForbidden
	}
	return repository.resolveRuntimeCredentialProjection(ctx, principal, input)
}

func (repository *Repository) ValidateRuntimeCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.RuntimeCredentialProjectionInput) (bool, error) {
	if !validCredentialProjectionOwner(principal, "platform.credential-projections.runtime.validate") ||
		!validRuntimeProjectionAuthority(input.Authority) || input.Fence != "" {
		return false, errs.ErrForbidden
	}
	resolved, err := repository.resolveRuntimeCredentialProjection(ctx, principal, input)
	if errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return sameProviderBinding(resolved.ProviderCredential, input.ProviderCredential) &&
		sameRuntimeSecretDescriptors(resolved.RuntimeSecrets, input.RuntimeSecrets), nil
}

func (repository *Repository) resolveRuntimeCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.RuntimeCredentialProjectionInput) (platformrepo.RuntimeCredentialProjection, error) {
	if !validRuntimeProjectionInput(input) {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.RuntimeCredentialProjection{}, err
	}
	if input.Authority.TenantID != current.organizationID {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result platformrepo.RuntimeCredentialProjection
	var rawSecrets []byte
	err = tx.QueryRow(ctx, queryCredentialProjectionResolveRuntime, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": input.Authority.ActorID,
		"project_id": nullUUID(input.Authority.ProjectID), "lease_ref": input.LeaseRef,
		"system_assistant":  input.Authority.CallerFullMethod == assistantProjectionMethod,
		"workload_instance": input.WorkloadInstance, "generation": input.Generation, "fence": input.Fence,
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_digest": input.RuntimeRevisionDigest,
		"attempt": input.Attempt, "input_digest": input.InputDigest, "session_ref": input.SessionRef, "turn_ref": input.TurnRef,
	}).Scan(&result.ProviderCredential.AccountRef, &result.ProviderCredential.CredentialRevisionRef,
		&result.ProviderCredential.CredentialRevision, &result.ProviderCredential.SecretName,
		&result.ProviderCredential.SecretUID, &result.ProviderCredential.SecretResourceVersion,
		&result.ProviderCredential.ContentSHA256, &rawSecrets, &result.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrNotFound
	}
	if err != nil {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrUnavailable
	}
	var stored []runtimecontract.RuntimeSecretProjection
	if err := jsonUnmarshal(rawSecrets, &stored); err != nil || len(stored) > maximumProjectionItems {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrConflict
	}
	if input.Authority.ProjectID == "" && len(stored) != 0 {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrForbidden
	}
	for _, candidate := range stored {
		var descriptor entity.RuntimeSecretRevisionDescriptor
		var secretRef string
		err := tx.QueryRow(ctx, queryCredentialProjectionResolveRuntimeSecret, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_id": input.Authority.ProjectID,
			"secret_name": candidate.SecretName, "secret_key": candidate.SecretKey, "secret_uid": candidate.SecretUID,
			"secret_resource_version": candidate.SecretResourceVersion, "content_sha256": candidate.ContentSHA256,
		}).Scan(&secretRef, &descriptor.Revision, &descriptor.Namespace, &descriptor.SecretName, &descriptor.SecretKey,
			&descriptor.SecretUID, &descriptor.SecretResourceVersion, &descriptor.ContentSHA256)
		if errors.Is(err, pgx.ErrNoRows) {
			return platformrepo.RuntimeCredentialProjection{}, errs.ErrNotFound
		}
		if err != nil || candidate.Name == "" || secretRef == "" {
			return platformrepo.RuntimeCredentialProjection{}, errs.ErrUnavailable
		}
		result.RuntimeSecrets = append(result.RuntimeSecrets, platformrepo.RuntimeSecretProjectionBinding{
			Name: candidate.Name, SecretRef: secretRef, Descriptor: descriptor,
		})
	}
	if input.Authority.ExpiresAt.Before(result.ExpiresAt) {
		result.ExpiresAt = input.Authority.ExpiresAt
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.RuntimeCredentialProjection{}, errs.ErrConflict
	}
	return result, nil
}

func (repository *Repository) ResolveTranscriptionCredentialProjection(ctx context.Context, principal value.Principal, input platformrepo.TranscriptionCredentialProjectionInput) (platformrepo.TranscriptionCredentialProjection, error) {
	if !validCredentialProjectionOwner(principal, "platform.credential-projections.stt.resolve") ||
		!validProjectionAuthority(input.Authority, "stt-tts-service", sttProjectionMethod) ||
		!validRuntimeSecretSHA256(input.ConfigDigestSHA256) || input.ConfigRevision == 0 || input.ProviderCredentialGeneration == 0 || input.ProviderAccountRef == "" {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return platformrepo.TranscriptionCredentialProjection{}, err
	}
	if input.Authority.TenantID != current.organizationID {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrForbidden
	}
	var result platformrepo.TranscriptionCredentialProjection
	user, err := repository.ResolvePrincipal(ctx, value.Principal{ActorID: input.Authority.ActorID, AuthorityTenant: input.Authority.TenantID})
	if err != nil {
		return result, err
	}
	userScope, err := repository.resolveScope(ctx, user)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	configuration, err := repository.getSystemSTTConfigurationTx(ctx, tx, userScope)
	if err != nil {
		return result, err
	}
	if !configuration.Ready || uint64(configuration.Revision) != input.ConfigRevision || configuration.Digest != input.ConfigDigestSHA256 ||
		configuration.ProviderAccountRef != input.ProviderAccountRef || uint64(configuration.ProviderCredentialGeneration) != input.ProviderCredentialGeneration {
		return result, errs.ErrNotFound
	}
	var model, language string
	var rawProviderCapabilities []byte
	err = tx.QueryRow(ctx, queryCredentialProjectionResolveSTT, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "config_revision": input.ConfigRevision,
		"config_digest": input.ConfigDigestSHA256, "account_ref": input.ProviderAccountRef,
		"credential_generation": input.ProviderCredentialGeneration,
	}).Scan(&result.ProviderCredential.AccountRef, &result.ProviderCredential.CredentialRevisionRef,
		&result.ProviderCredential.CredentialRevision, &result.ProviderCredential.SecretName,
		&result.ProviderCredential.SecretUID, &result.ProviderCredential.SecretResourceVersion,
		&result.ProviderCredential.ContentSHA256, &model, &language, &rawProviderCapabilities)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrNotFound
	}
	if err != nil {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrUnavailable
	}
	var providerCapabilities map[string]any
	if jsonUnmarshal(rawProviderCapabilities, &providerCapabilities) != nil {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrUnavailable
	}
	if !systemSTTModelSupported(model, language) {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrNotFound
	}
	result.ExpiresAt = time.Now().UTC().Add(time.Minute)
	if input.Authority.ExpiresAt.Before(result.ExpiresAt) {
		result.ExpiresAt = input.Authority.ExpiresAt
	}
	if err := tx.Commit(ctx); err != nil {
		return platformrepo.TranscriptionCredentialProjection{}, errs.ErrConflict
	}
	return result, nil
}

func validCredentialProjectionOwner(principal value.Principal, permission string) bool {
	return principal.CallerWorkload == "secret-broker" && principal.Permission == permission
}

func validProjectionAuthority(authority platformrepo.CredentialProjectionAuthority, workload, method string) bool {
	now := time.Now().UTC()
	return uuid.Validate(authority.ActorID) == nil && uuid.Validate(authority.TenantID) == nil &&
		(uuid.Validate(authority.ProjectID) == nil || (method == assistantProjectionMethod || method == sttProjectionMethod) && authority.ProjectID == "") && uuid.Validate(authority.ProofJTI) == nil &&
		authority.SourceRevision > 0 && authority.CallerCredentialRevision > 0 && validRuntimeSecretSHA256(authority.SourceDigestSHA256) &&
		authority.CallerWorkloadID == workload && authority.CallerFullMethod == method &&
		authority.ExpiresAt.After(now) && !authority.ExpiresAt.After(now.Add(5*time.Minute))
}

func validRuntimeProjectionAuthority(authority platformrepo.CredentialProjectionAuthority) bool {
	method := runtimeProjectionMethod
	if authority.ProjectID == "" {
		method = assistantProjectionMethod
	}
	return validProjectionAuthority(authority, "runtime-controller", method)
}

func validRuntimeProjectionInput(input platformrepo.RuntimeCredentialProjectionInput) bool {
	return input.WorkloadInstance != "" && len(input.WorkloadInstance) <= 128 && input.LeaseRef != "" &&
		input.Generation > 0 && input.Attempt > 0 && input.RuntimeRevisionRef != "" && input.SessionRef != "" &&
		input.TurnRef != "" && validRuntimeSecretSHA256(input.RuntimeRevisionDigest) && validRuntimeSecretSHA256(input.InputDigest)
}

func sameProviderBinding(left, right platformrepo.ProviderCredentialBinding) bool {
	return left == right
}

func sameRuntimeSecretDescriptors(left, right []platformrepo.RuntimeSecretProjectionBinding) bool {
	if len(left) != len(right) {
		return false
	}
	leftCopy := append([]platformrepo.RuntimeSecretProjectionBinding(nil), left...)
	rightCopy := append([]platformrepo.RuntimeSecretProjectionBinding(nil), right...)
	sort.Slice(leftCopy, func(i, j int) bool {
		return projectionDescriptorKey(leftCopy[i]) < projectionDescriptorKey(leftCopy[j])
	})
	sort.Slice(rightCopy, func(i, j int) bool {
		return projectionDescriptorKey(rightCopy[i]) < projectionDescriptorKey(rightCopy[j])
	})
	for index := range leftCopy {
		if leftCopy[index] != rightCopy[index] {
			return false
		}
	}
	return true
}

func projectionDescriptorKey(value platformrepo.RuntimeSecretProjectionBinding) string {
	descriptor := value.Descriptor
	return strings.Join([]string{value.Name, value.SecretRef, descriptor.Namespace, descriptor.SecretName, descriptor.SecretKey, descriptor.SecretUID, descriptor.SecretResourceVersion, descriptor.ContentSHA256}, "\x00")
}
