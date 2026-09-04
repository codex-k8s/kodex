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
	maximumProviderAPIKeyBytes     = 16 << 10
	providerCompensationTimeBudget = 15 * time.Second
)

type ProviderDeviceAuthorizationMaterialization struct {
	MaterializerAttemptRef, VerificationURI, UserCode  string
	MaterializerAttemptUID, MaterializerAttemptVersion string
	ExpiresAt                                          time.Time
}

type ProviderMaterializationDiscard struct {
	AttemptRef, AccountRef                         string
	MaterializerAttemptRef, MaterializerAttemptUID string
	MaterializerAttemptVersion                     string
	Credential                                     *entity.ProviderCredentialDescriptor
}

type ProviderAuthorizationObservation struct {
	State, ExternalAccountMasked, SafeFailureCode string
	Credential                                    *entity.ProviderCredentialDescriptor
}

type ProviderCredentialMaterializer interface {
	Check(context.Context) error
	StartDeviceAuthorization(context.Context, string, string) (ProviderDeviceAuthorizationMaterialization, error)
	ObserveDeviceAuthorization(context.Context, string) (ProviderAuthorizationObservation, error)
	MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error)
	Discard(context.Context, ProviderMaterializationDiscard) error
}

type providerMaterializationReferenceReader interface {
	ProviderMaterializationReferenced(
		context.Context,
		value.Principal,
		string,
		string,
		string,
		*entity.ProviderCredentialDescriptor,
	) (bool, error)
}

func WithProviderCredentialMaterializer(materializer ProviderCredentialMaterializer) Option {
	return func(service *Service) { service.providerCredentialMaterializer = materializer }
}

func (service *Service) StartProviderAccountDeviceAuthorization(
	ctx context.Context,
	principal value.Principal,
	mutation value.Mutation,
	accountRef string,
) (entity.ProviderAccount, error) {
	principal, account, err := service.providerAccountForAuthorization(ctx, principal, mutation, accountRef)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if service.providerCredentialMaterializer == nil || !containsString(account.NextActions, "CONFIGURE_CREDENTIAL") {
		return entity.ProviderAccount{}, errs.ErrForbidden
	}
	attemptRef := providerAuthorizationRef(accountRef, mutation.IdempotencyKey, "DEVICE_CODE")
	materialized, err := service.providerCredentialMaterializer.StartDeviceAuthorization(ctx, attemptRef, accountRef)
	if err != nil {
		return entity.ProviderAccount{}, errors.Join(errs.ErrUnavailable, err)
	}
	result, err := service.executeResolved(ctx, command.Command{
		Kind: command.StartProviderDeviceAuth, Principal: principal, Mutation: mutation,
		Payload: command.ProviderAccountInput{
			AccountRef: accountRef, AuthorizationRef: attemptRef, AuthorizationMethod: "DEVICE_CODE",
			AuthorizationState: "PENDING", MaterializerAttemptRef: materialized.MaterializerAttemptRef,
			VerificationURI: materialized.VerificationURI, UserCode: materialized.UserCode,
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
	principal, account, err := service.providerAccountForAuthorization(ctx, principal, mutation, accountRef)
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if service.providerCredentialMaterializer == nil || !containsString(account.NextActions, "CONFIGURE_CREDENTIAL") {
		return entity.ProviderAccount{}, errs.ErrForbidden
	}
	attemptRef := providerAuthorizationRef(accountRef, mutation.IdempotencyKey, "API_KEY")
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

func providerAuthorizationRef(accountRef, idempotencyKey, method string) string {
	digest := sha256.Sum256([]byte(accountRef + "\x00" + idempotencyKey + "\x00" + method))
	return "pauth_" + hex.EncodeToString(digest[:16])
}

func providerAccountResult(result command.Result, err error) (entity.ProviderAccount, error) {
	if err != nil {
		return entity.ProviderAccount{}, err
	}
	if result.ProviderAccount == nil {
		return entity.ProviderAccount{}, errs.ErrUnavailable
	}
	return *result.ProviderAccount, nil
}
