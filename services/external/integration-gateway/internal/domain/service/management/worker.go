package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/gitfetcher"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/managementeffect"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/providerauthorization"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	managementrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/management"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
)

var credentialNamespace = uuid.MustParse("5979616e-c052-4e61-b537-cbf67fbfd431")

type WorkerDependencies struct {
	Authorizer       providerauthorization.Client
	Secrets          secretstore.Store
	GitSecrets       secretstore.Store
	Effects          managementeffect.Client
	Git              gitfetcher.Client
	SecretPathPrefix string
	LeaseDuration    time.Duration
}

func (service *Service) ConfigureWorker(dependencies WorkerDependencies) error {
	if dependencies.Authorizer == nil || dependencies.Secrets == nil || dependencies.GitSecrets == nil || dependencies.Effects == nil || dependencies.Git == nil || dependencies.SecretPathPrefix == "" || dependencies.LeaseDuration < 5*time.Second || dependencies.LeaseDuration > time.Minute {
		return errors.New("integration management worker configuration is invalid")
	}
	service.worker = &dependencies
	return nil
}

func (service *Service) ProcessOne(ctx, finalizationBase context.Context) (bool, error) {
	if service.worker == nil {
		return false, errors.New("integration management worker is not configured")
	}
	if finalizationBase == nil {
		return false, errors.New("integration management finalization context is absent")
	}
	scope, found, err := service.repository.NextManagementScope(ctx)
	if err != nil || !found {
		return false, err
	}
	effect, found, err := service.repository.ClaimManagementEffect(ctx, scope, service.now().UTC(), service.worker.LeaseDuration)
	if err != nil || !found {
		return false, err
	}
	if effect.Attempts > 1 && singleDispatchEffect(effect.Kind) {
		if effect.Kind == "PROVIDER_AUTHORIZE" {
			return true, service.closeAmbiguousAuthorization(ctx, scope, effect)
		}
		return true, service.closeAmbiguousEffect(ctx, scope, effect)
	}
	effectCtx, cancelEffect := context.WithCancel(ctx)
	renewed := make(chan error, 1)
	go service.renewEffectLease(effectCtx, cancelEffect, renewed, scope, effect)
	switch effect.Kind {
	case "PROVIDER_AUTHORIZE":
		err = service.authorizeProvider(effectCtx, scope, effect)
	case "PROVIDER_REFERENCE_SYNC":
		err = service.syncProvider(effectCtx, scope, effect)
	case "PROVIDER_REVOKE":
		err = service.revokeProvider(effectCtx, scope, effect)
	case "PROVIDER_POOL_SYNC":
		err = service.syncPool(effectCtx, scope, effect)
	case "INTEGRATION_TEST":
		err = service.testProvider(effectCtx, scope, effect)
	case "GIT_FETCH":
		err = service.fetchGit(effectCtx, scope, effect)
	case "GIT_APPLY":
		err = service.applyGit(effectCtx, scope, effect)
	default:
		err = errors.New("management effect kind is unsupported")
	}
	cancelEffect()
	finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(finalizationBase), 5*time.Second)
	defer cancelFinalize()
	if renewErr := <-renewed; renewErr != nil {
		succeeded, checkErr := service.repository.ManagementEffectSucceeded(finalizeCtx, scope, effect.ID)
		if !succeeded || checkErr != nil || err != nil {
			err = errors.Join(managementeffect.ErrOutcomeUnknown, renewErr, checkErr, err)
		}
	}
	if err != nil && effect.Kind == "PROVIDER_REFERENCE_SYNC" {
		err = errors.Join(err, service.revokePendingCredential(finalizeCtx, scope, effect))
	}
	if err != nil {
		status := effectFailureStatus(err)
		if effect.Kind == "PROVIDER_AUTHORIZE" {
			_ = service.repository.FailAuthorization(finalizeCtx, scope, effect.ResourceID, effect.LeaseID, effect.LeaseFence, closedFailureCategory(err), service.now().UTC())
			_ = service.repository.CompleteManagementEffect(finalizeCtx, managementrepo.EffectCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, Status: status, FailureCategory: closedFailureCategory(err), At: service.now().UTC()})
		} else {
			_ = service.repository.FailManagementEffect(finalizeCtx, managementrepo.EffectFailure{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, Status: status, FailureCategory: closedFailureCategory(err), At: service.now().UTC()})
		}
	}
	return true, err
}

func (service *Service) renewEffectLease(ctx context.Context, cancel context.CancelFunc, result chan<- error, scope domainrepo.Scope, effect entity.ManagementEffect) {
	ticker := time.NewTicker(service.worker.LeaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			result <- nil
			return
		case <-ticker.C:
			if err := service.repository.RenewManagementEffect(ctx, scope, effect.ID, effect.LeaseID, effect.LeaseFence, service.worker.LeaseDuration); err != nil {
				cancel()
				result <- errors.New("management effect lease renewal failed")
				return
			}
		}
	}
}

func singleDispatchEffect(kind string) bool {
	switch kind {
	case "PROVIDER_AUTHORIZE", "PROVIDER_REFERENCE_SYNC", "PROVIDER_REVOKE", "PROVIDER_POOL_SYNC", "INTEGRATION_TEST", "GIT_APPLY":
		return true
	default:
		return false
	}
}

func effectFailureStatus(err error) string {
	if errors.Is(err, managementeffect.ErrOutcomeUnknown) {
		return "UNKNOWN"
	}
	return "FAILED"
}

func (service *Service) closeAmbiguousEffect(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	var cleanupErr error
	if effect.Kind == "PROVIDER_REFERENCE_SYNC" {
		cleanupErr = service.revokePendingCredential(ctx, scope, effect)
	}
	if effect.Kind == "PROVIDER_REVOKE" {
		connection, err := service.repository.GetManagedConnection(ctx, scope, effect.ResourceID)
		if err == nil && connection.Status == "REVOKED" {
			cleanupErr = service.revokeCredentialSecrets(ctx, scope, connection.ID)
		} else if err != nil {
			cleanupErr = err
		}
	}
	failErr := service.repository.FailManagementEffect(ctx, managementrepo.EffectFailure{
		Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence,
		Status: "UNKNOWN", FailureCategory: "OUTCOME_UNKNOWN", At: service.now().UTC(),
	})
	return errors.Join(cleanupErr, failErr)
}

func (service *Service) revokePendingCredential(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	connection, err := service.repository.GetManagedConnection(ctx, scope, effect.ResourceID)
	if err != nil || connection.ActiveCredential == effect.ResourceGeneration {
		return nil
	}
	credential, err := service.repository.GetCredentialGeneration(ctx, scope, effect.ResourceID, effect.ResourceGeneration)
	if err != nil || credential.Status != "PENDING" {
		return nil
	}
	return service.worker.Secrets.Revoke(ctx, credential.SecretRef, credential.SecretVersion)
}

func (service *Service) closeAmbiguousAuthorization(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	now := service.now().UTC()
	var cleanupErr error
	if raw, err := service.repository.GetEffectResource(ctx, scope, effect); err == nil {
		var authorization entity.ProviderAuthorization
		if json.Unmarshal(raw, &authorization) == nil {
			secretRef := strings.Trim(service.worker.SecretPathPrefix, "/") + "/" + scope.TenantID + "/" + authorization.ConnectionID + "/" + fmt.Sprintf("%d", authorization.Generation)
			secret, version, getErr := service.worker.Secrets.Get(ctx, secretRef)
			if getErr == nil {
				zeroBytes(secret)
				cleanupErr = service.worker.Secrets.Revoke(ctx, secretRef, version.Version)
			}
		}
	}
	failErr := service.repository.FailAuthorization(ctx, scope, effect.ResourceID, effect.LeaseID, effect.LeaseFence, "OUTCOME_UNKNOWN", now)
	completeErr := service.repository.CompleteManagementEffect(ctx, managementrepo.EffectCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, Status: "UNKNOWN", FailureCategory: "OUTCOME_UNKNOWN", At: now})
	return errors.Join(cleanupErr, failErr, completeErr)
}

func (service *Service) authorizeProvider(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	var authorization entity.ProviderAuthorization
	raw, err := service.repository.GetEffectResource(ctx, scope, effect)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, &authorization) != nil || authorization.Version != effect.ResourceVersion || authorization.Generation != effect.ResourceGeneration {
		return errors.New("provider authorization effect binding is invalid")
	}
	if !service.now().UTC().Before(authorization.ExpiresAt) {
		_ = service.repository.FailAuthorization(ctx, scope, authorization.ID, effect.LeaseID, effect.LeaseFence, "EXPIRED", service.now().UTC())
		return errors.New("provider authorization expired")
	}
	result, err := service.worker.Authorizer.Authorize(ctx, func(code providerauthorization.DeviceCode) error {
		codeExpiresAt := code.ExpiresAt
		if authorization.ExpiresAt.Before(codeExpiresAt) {
			codeExpiresAt = authorization.ExpiresAt
		}
		deviceRaw, _ := json.Marshal(map[string]string{"verification_url": code.VerificationURL, "user_code": code.UserCode})
		encryptedLoginID, encryptErr := service.cipher.Encrypt(ctx, []byte(code.LoginID))
		if encryptErr != nil {
			return encryptErr
		}
		encryptedDeviceResult, encryptErr := service.cipher.Encrypt(ctx, deviceRaw)
		if encryptErr != nil {
			return encryptErr
		}
		return service.repository.MarkAuthorizationCode(ctx, scope, authorization.ID, effect.LeaseID, effect.LeaseFence, encryptedLoginID, encryptedDeviceResult, codeExpiresAt, service.now().UTC())
	}, func(checkCtx context.Context) (bool, error) {
		return service.repository.AuthorizationCancelled(checkCtx, scope, authorization.ID, effect.LeaseID, effect.LeaseFence)
	})
	if err != nil {
		category := closedFailureCategory(err)
		_ = service.repository.FailAuthorization(ctx, scope, authorization.ID, effect.LeaseID, effect.LeaseFence, category, service.now().UTC())
		return err
	}
	defer zeroBytes(result.Credential)
	secretRef := strings.Trim(service.worker.SecretPathPrefix, "/") + "/" + scope.TenantID + "/" + authorization.ConnectionID + "/" + fmt.Sprintf("%d", authorization.Generation)
	version, err := service.worker.Secrets.Put(ctx, secretRef, result.Credential)
	if err != nil {
		return service.failAuthorizationAfterProvider(ctx, scope, effect, authorization.ID, err)
	}
	bindingID := uuid.NewSHA1(credentialNamespace, []byte(authorization.ConnectionID+"\x00"+fmt.Sprint(authorization.Generation))).String()
	bindingDigest := digest([]any{bindingID, authorization.Generation, version.Ref, version.Version, version.ContentDigest})
	credential := entity.CredentialGeneration{ConnectionID: authorization.ConnectionID, Generation: authorization.Generation, AuthorizationID: authorization.ID, Status: "PENDING", SecretRef: version.Ref, SecretVersion: version.Version, SecretContentDigest: version.ContentDigest, CredentialBindingID: bindingID, CredentialBindingVersion: authorization.Generation, CredentialBindingDigest: bindingDigest, MaskedAccount: result.MaskedAccount, MaskedLabel: result.MaskedLabel}
	if err = service.repository.CompleteAuthorization(ctx, scope, authorization.ID, effect.ID, effect.LeaseID, effect.LeaseFence, credential, result.MaskedLabel, effect.IntentDigest, service.now().UTC()); err != nil {
		_ = service.worker.Secrets.Revoke(ctx, version.Ref, version.Version)
		return service.failAuthorizationAfterProvider(ctx, scope, effect, authorization.ID, err)
	}
	return nil
}

func (service *Service) failAuthorizationAfterProvider(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect, authorizationID string, cause error) error {
	failErr := service.repository.FailAuthorization(ctx, scope, authorizationID, effect.LeaseID, effect.LeaseFence, "OUTCOME_UNKNOWN", service.now().UTC())
	return errors.Join(cause, failErr)
}

func (service *Service) syncProvider(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	var connection entity.ManagedProviderConnection
	raw, err := service.repository.GetEffectResource(ctx, scope, effect)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, &connection) != nil || connection.Version != effect.ResourceVersion || connection.Generation != effect.ResourceGeneration || connection.Status == "REVOKED" {
		return errors.New("provider sync effect binding is stale")
	}
	credential, err := service.repository.GetCredentialGeneration(ctx, scope, connection.ID, effect.ResourceGeneration)
	if err != nil || credential.Status != "PENDING" {
		return errors.New("provider credential candidate binding is stale")
	}
	candidate := connection
	candidate.ActiveCredential = credential.Generation
	candidate.CredentialBindingID = credential.CredentialBindingID
	candidate.CredentialBindingVersion = credential.CredentialBindingVersion
	candidate.CredentialBindingDigest = credential.CredentialBindingDigest
	candidate.MaskedAccount = credential.MaskedAccount
	candidate.MaskedLabel = credential.MaskedLabel
	readback, err := service.worker.Effects.SyncProvider(ctx, scope, candidate, effect.IntentDigest)
	if err != nil {
		return err
	}
	observationDigest := digest([]any{candidate.ID, candidate.Version + 1, candidate.Generation, candidate.CredentialBindingDigest, readback.ResourceID, readback.Version, readback.Digest})
	_, err = service.repository.CompleteProviderSync(ctx, managementrepo.ProviderSyncCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, ExpectedVersion: connection.Version, ExpectedGeneration: connection.Generation, ControlPlaneID: readback.ResourceID, ControlPlaneVersion: readback.Version, ControlPlaneDigest: readback.Digest, ObservationDigest: observationDigest, ObservedAt: service.now().UTC()})
	return err
}

func (service *Service) revokeProvider(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	connection, err := service.repository.GetManagedConnection(ctx, scope, effect.ResourceID)
	if err != nil {
		return err
	}
	if connection.Status != "REVOKED" || connection.Version != effect.ResourceVersion || connection.Generation != effect.ResourceGeneration {
		return errors.New("provider revoke effect binding is stale")
	}
	var providerErr, secretErr, controlErr error
	credentials, credentialListErr := service.repository.ListCredentialGenerations(ctx, scope, connection.ID)
	if credentialListErr != nil {
		return credentialListErr
	}
	if connection.ActiveCredential > 0 {
		var active *entity.CredentialGeneration
		for index := range credentials {
			if credentials[index].Generation == connection.ActiveCredential {
				active = &credentials[index]
				break
			}
		}
		if active != nil {
			raw, _, getErr := service.worker.Secrets.Get(ctx, active.SecretRef)
			if getErr == nil {
				providerErr = service.worker.Authorizer.Revoke(ctx, raw)
				zeroBytes(raw)
			} else {
				providerErr = getErr
			}
		} else {
			providerErr = errors.New("active provider credential generation is absent")
		}
	}
	secretErr = service.revokeCredentialSecretsFromList(ctx, credentials)
	if connection.ControlPlaneID != "" {
		_, controlErr = service.worker.Effects.SyncProvider(ctx, scope, connection, effect.IntentDigest)
	}
	if err = errors.Join(providerErr, secretErr, controlErr); err != nil {
		return err
	}
	return service.repository.CompleteManagementEffect(ctx, managementrepo.EffectCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, Status: "SUCCEEDED", At: service.now().UTC()})
}

func (service *Service) revokeCredentialSecrets(ctx context.Context, scope domainrepo.Scope, connectionID string) error {
	credentials, err := service.repository.ListCredentialGenerations(ctx, scope, connectionID)
	if err != nil {
		return err
	}
	return service.revokeCredentialSecretsFromList(ctx, credentials)
}

func (service *Service) revokeCredentialSecretsFromList(ctx context.Context, credentials []entity.CredentialGeneration) error {
	var result error
	for _, credential := range credentials {
		result = errors.Join(result, service.worker.Secrets.Revoke(ctx, credential.SecretRef, credential.SecretVersion))
	}
	return result
}

func (service *Service) syncPool(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	var pool entity.ManagedProviderPool
	raw, err := service.repository.GetEffectResource(ctx, scope, effect)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, &pool) != nil || pool.Version != effect.ResourceVersion {
		return errors.New("provider pool effect binding is stale")
	}
	readback, err := service.worker.Effects.SyncPool(ctx, scope, pool, effect.IntentDigest)
	if err != nil {
		return err
	}
	_, err = service.repository.CompletePoolSync(ctx, managementrepo.PoolSyncCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, ExpectedVersion: pool.Version, ControlPlaneID: readback.ResourceID, ControlPlaneVersion: readback.Version, ControlPlaneDigest: readback.Digest, At: service.now().UTC()})
	return err
}

func (service *Service) testProvider(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	receipt, err := service.repository.GetTest(ctx, scope, effect.ResourceID)
	if err != nil {
		return err
	}
	testedAt := service.now().UTC()
	if !testedAt.Before(receipt.ExpiresAt) {
		testDigest := digest([]any{receipt.ID, receipt.ConnectionID, receipt.ConnectionVersion, receipt.ConnectionGeneration, receipt.DefinitionID, receipt.DefinitionVersion, "TIMEOUT", testedAt})
		_, completeErr := service.repository.CompleteTest(ctx, managementrepo.TestCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, TestID: receipt.ID, Category: "TIMEOUT", Digest: testDigest, At: testedAt})
		return completeErr
	}
	connection, err := service.repository.GetManagedConnection(ctx, scope, receipt.ConnectionID)
	if err != nil {
		return err
	}
	if connection.Version != receipt.ConnectionVersion || connection.Generation != receipt.ConnectionGeneration || connection.Status != "VALID" {
		return errors.New("integration test connection binding is stale")
	}
	credential, err := service.repository.GetCredentialGeneration(ctx, scope, connection.ID, connection.ActiveCredential)
	if err != nil {
		return err
	}
	raw, version, err := service.worker.Secrets.Get(ctx, credential.SecretRef)
	if err != nil {
		return err
	}
	defer zeroBytes(raw)
	if version.Version != credential.SecretVersion || version.ContentDigest != credential.SecretContentDigest {
		return errors.New("integration test credential readback mismatch")
	}
	category := "OK"
	testErr := service.worker.Authorizer.Test(ctx, raw)
	if testErr != nil {
		category = closedFailureCategory(testErr)
	}
	testedAt = service.now().UTC()
	testDigest := digest([]any{receipt.ID, connection.ID, connection.Version, connection.Generation, receipt.DefinitionID, receipt.DefinitionVersion, category, testedAt})
	_, completeErr := service.repository.CompleteTest(ctx, managementrepo.TestCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, TestID: receipt.ID, Category: category, Digest: testDigest, At: testedAt})
	return completeErr
}

func (service *Service) fetchGit(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	reconciliation, err := service.repository.GetGitReconciliation(ctx, scope, effect.ResourceID)
	if err != nil {
		return err
	}
	binding, err := service.repository.GetGitBinding(ctx, scope, reconciliation.BindingID)
	if err != nil {
		return err
	}
	if binding.Version != effect.ResourceVersion || binding.Status != "ACTIVE" || reconciliation.State != "PENDING" {
		return errors.New("Git fetch effect binding is stale")
	}
	fetched, err := service.worker.Git.Fetch(ctx, binding.RepositoryKey, binding.RefKey, binding.PathKey)
	if err != nil {
		return err
	}
	allowlistedRef, ok := service.gitSources.SourceRef(binding.RepositoryKey, binding.RefKey, binding.PathKey)
	sourceRef, pinned := pinGitSourceRef(allowlistedRef, fetched.Commit)
	if !ok || !pinned || fetched.SourceRef != sourceRef {
		return errors.New("Git fetched source readback mismatch")
	}
	sourceDigest := digestBytes(fetched.Content)
	now := service.now().UTC()
	binding.FetchedCommit, binding.SourceRevision, binding.SourceDigest, binding.FetchedAt, binding.UpdatedAt = fetched.Commit, binding.SourceRevision+1, sourceDigest, &now, now
	reconciliation.FetchedCommit, reconciliation.SourceRevision, reconciliation.SourceDigest, reconciliation.UpdatedAt = fetched.Commit, binding.SourceRevision, sourceDigest, now
	reconciliation.CommandIntentDigest, err = service.worker.Effects.GitIntentSHA256(scope, binding, reconciliation, fetched.Content, sourceRef)
	if err != nil {
		return err
	}
	encrypted, err := service.cipher.Encrypt(ctx, fetched.Content)
	if err != nil {
		return err
	}
	reconciliation.State, reconciliation.EncryptedSnapshot = "FETCHED", encrypted
	reconciliation.ReceiptDigest = digest([]any{reconciliation.ReceiptID, reconciliation.SourceRevision, sourceDigest, reconciliation.CommandIntentDigest})
	_, err = service.repository.CompleteGitFetch(ctx, managementrepo.GitFetchCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, Binding: binding, Reconciliation: reconciliation, At: now})
	return err
}

func (service *Service) applyGit(ctx context.Context, scope domainrepo.Scope, effect entity.ManagementEffect) error {
	reconciliation, err := service.repository.GetGitReconciliation(ctx, scope, effect.ResourceID)
	if err != nil {
		return err
	}
	binding, err := service.repository.GetGitBinding(ctx, scope, reconciliation.BindingID)
	if err != nil {
		return err
	}
	if reconciliation.State != "FETCHED" || binding.Version != effect.ResourceVersion || binding.SourceRevision != reconciliation.SourceRevision || binding.SourceDigest != reconciliation.SourceDigest {
		return errors.New("Git apply effect binding is stale")
	}
	snapshot, err := service.cipher.Decrypt(ctx, reconciliation.EncryptedSnapshot)
	if err != nil {
		return err
	}
	defer zeroBytes(snapshot)
	allowlistedRef, ok := service.gitSources.SourceRef(binding.RepositoryKey, binding.RefKey, binding.PathKey)
	sourceRef, pinned := pinGitSourceRef(allowlistedRef, reconciliation.FetchedCommit)
	if !ok || !pinned {
		return errors.New("Git source binding is no longer allowlisted")
	}
	readback, err := service.worker.Effects.ReconcileGit(ctx, scope, binding, reconciliation, snapshot, sourceRef)
	if err != nil {
		return err
	}
	_, err = service.repository.CompleteGitApply(ctx, managementrepo.GitApplyCompletion{Scope: scope, EffectID: effect.ID, LeaseID: effect.LeaseID, LeaseFence: effect.LeaseFence, ReconciliationID: reconciliation.ID, ReadbackID: readback.ResourceID, ReadbackVersion: readback.Version, ReadbackDigest: readback.Digest, At: service.now().UTC()})
	return err
}

func pinGitSourceRef(allowlistedRef, commit string) (string, bool) {
	hash := strings.IndexByte(allowlistedRef, '#')
	separator := strings.LastIndexByte(allowlistedRef, ':')
	commit = strings.ToLower(commit)
	if hash < 8 || separator <= hash+1 || !commitPattern.MatchString(commit) {
		return "", false
	}
	return allowlistedRef[:hash+1] + commit + allowlistedRef[separator:], true
}

func closedFailureCategory(err error) string {
	value := strings.ToLower(err.Error())
	switch {
	case strings.Contains(value, "denied"):
		return "DENIED"
	case strings.Contains(value, "expired") || strings.Contains(value, "timeout"):
		return "EXPIRED"
	case strings.Contains(value, "credential"):
		return "CREDENTIAL_UNAVAILABLE"
	case strings.Contains(value, "unauthorized"):
		return "UNAUTHORIZED"
	case strings.Contains(value, "forbidden"):
		return "FORBIDDEN"
	case strings.Contains(value, "unavailable"):
		return "ENDPOINT_UNAVAILABLE"
	default:
		return "PROTOCOL_ERROR"
	}
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
