// Package management реализует owner lifecycle Issue #236 поверх единственного
// PostgreSQL runtime integration-gateway.
package management

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/payloadcipher"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	managementrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/management"
	gatewayservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/google/uuid"
)

const (
	defaultPageSize        = 50
	maximumPageSize        = 100
	authorizationLifetime  = 15 * time.Minute
	maximumDisplayNameSize = 128
)

var (
	stableKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	digestPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern    = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

type DefinitionCatalog interface {
	Get(string, uint64) (entity.Definition, bool)
	List() []entity.Definition
}

type GitSourceCatalog interface {
	Resolve(string, string, string) (entity.GitSource, bool)
	SourceRef(string, string, string) (string, bool)
	Check(context.Context) error
}

type Config struct {
	Providers        []entity.ProviderDescriptor
	AuthorizationTTL time.Duration
	MaximumPageSize  int
}

type Service struct {
	repository     managementrepo.Repository
	cipher         payloadcipher.Cipher
	definitions    DefinitionCatalog
	gitSources     GitSourceCatalog
	invocations    *gatewayservice.Service
	providers      map[string]entity.ProviderDescriptor
	providerList   []entity.ProviderDescriptor
	catalogVersion uint64
	catalogDigest  string
	config         Config
	now            func() time.Time
	worker         *WorkerDependencies
}

func New(repository managementrepo.Repository, cipher payloadcipher.Cipher, definitions DefinitionCatalog, gitSources GitSourceCatalog, invocations *gatewayservice.Service, config Config) (*Service, error) {
	if repository == nil || cipher == nil || definitions == nil || gitSources == nil || invocations == nil ||
		config.AuthorizationTTL < time.Minute || config.AuthorizationTTL > authorizationLifetime ||
		config.MaximumPageSize < 1 || config.MaximumPageSize > maximumPageSize || len(config.Providers) == 0 {
		return nil, errors.New("integration management service configuration is invalid")
	}
	providers := make(map[string]entity.ProviderDescriptor, len(config.Providers))
	providerList := slices.Clone(config.Providers)
	sort.Slice(providerList, func(left, right int) bool { return providerList[left].ID < providerList[right].ID })
	for index := range providerList {
		provider := providerList[index]
		if !validStableKey(provider.ID) || provider.Version == 0 || provider.DisplayName == "" || len(provider.AuthorizationModes) == 0 || len(provider.Capabilities) == 0 {
			return nil, errors.New("provider catalog entry is invalid")
		}
		provider.Digest = digest(providerCatalogDigestInput(provider))
		if _, duplicate := providers[provider.ID]; duplicate {
			return nil, errors.New("provider catalog entry is duplicated")
		}
		providers[provider.ID] = provider
		providerList[index] = provider
	}
	return &Service{
		repository: repository, cipher: cipher, definitions: definitions, gitSources: gitSources,
		invocations: invocations, providers: providers, providerList: providerList,
		catalogVersion: 1, catalogDigest: digest(providerList), config: config, now: time.Now,
	}, nil
}

func providerCatalogDigestInput(value entity.ProviderDescriptor) any {
	value.Digest = ""
	return value
}

func digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hashIdempotency(scope domainrepo.Scope, operation, key string) (string, error) {
	if uuid.Validate(key) != nil {
		return "", errs.ErrInvalid
	}
	return digest([]string{scope.TenantID, scope.ProjectID, scope.ActorID, operation, key}), nil
}

func validScope(scope domainrepo.Scope) bool {
	return uuid.Validate(scope.TenantID) == nil && uuid.Validate(scope.ProjectID) == nil && uuid.Validate(scope.ActorID) == nil
}

func validStableKey(value string) bool {
	return len(value) <= 96 && stableKeyPattern.MatchString(value)
}

func validID(value string) bool { return uuid.Validate(value) == nil }

func (service *Service) ListProviders(scope domainrepo.Scope) ([]entity.ProviderDescriptor, uint64, string, error) {
	if !validScope(scope) {
		return nil, 0, "", errs.ErrForbidden
	}
	return slices.Clone(service.providerList), service.catalogVersion, service.catalogDigest, nil
}

func (service *Service) GetProvider(scope domainrepo.Scope, id string, version uint64, expectedDigest string) (entity.ProviderDescriptor, error) {
	if !validScope(scope) {
		return entity.ProviderDescriptor{}, errs.ErrForbidden
	}
	value, ok := service.providers[id]
	if !ok {
		return value, errs.ErrNotFound
	}
	if version != 0 && version != value.Version || expectedDigest != "" && expectedDigest != value.Digest {
		return entity.ProviderDescriptor{}, errs.ErrConflict
	}
	return value, nil
}

type StartAuthorizationInput struct {
	Scope                                              domainrepo.Scope
	ProviderID, StableKey, DisplayName, IdempotencyKey string
}

func (service *Service) StartAuthorization(ctx context.Context, input StartAuthorizationInput) (entity.ProviderAuthorization, error) {
	provider, ok := service.providers[input.ProviderID]
	if !validScope(input.Scope) {
		return entity.ProviderAuthorization{}, errs.ErrForbidden
	}
	if !ok || !validStableKey(input.StableKey) || input.DisplayName == "" || len(input.DisplayName) > maximumDisplayNameSize {
		return entity.ProviderAuthorization{}, errs.ErrInvalid
	}
	idempotencyHash, err := hashIdempotency(input.Scope, "provider_authorization.start", input.IdempotencyKey)
	if err != nil {
		return entity.ProviderAuthorization{}, err
	}
	now := service.now().UTC()
	connectionID, authorizationID := uuid.NewString(), uuid.NewString()
	capabilities := make([]string, 0, len(provider.Capabilities))
	for _, capability := range provider.Capabilities {
		capabilities = append(capabilities, capability.Name)
	}
	sort.Strings(capabilities)
	capabilityDigest := digest(capabilities)
	requestHash := digest([]any{input.Scope.TenantID, input.Scope.ProjectID, input.ProviderID, input.StableKey, input.DisplayName, provider.Version, provider.Digest})
	connection := entity.ManagedProviderConnection{
		ID: connectionID, StableKey: input.StableKey, ProviderID: provider.ID, DisplayName: input.DisplayName,
		Version: 1, Generation: 1, Status: "PENDING", Capabilities: capabilities,
		CapabilityDigest: capabilityDigest, ObservationDigest: digest(struct{}{}), CreatedAt: now, UpdatedAt: now,
	}
	authorization := entity.ProviderAuthorization{
		ID: authorizationID, ProviderID: provider.ID, ConnectionID: connectionID, Attempt: 1,
		Version: 1, Generation: 1, State: "PENDING", IntentDigest: requestHash,
		ExpiresAt: now.Add(service.config.AuthorizationTTL), CreatedAt: now, UpdatedAt: now,
	}
	audit := managementAudit(input.Scope, "provider_authorization.start", "PROVIDER_AUTHORIZATION", authorizationID, requestHash, "PENDING", now)
	value, _, err := service.repository.StartAuthorization(ctx, managementrepo.StartAuthorizationCommand{
		Scope: input.Scope, Connection: connection, Authorization: authorization,
		IdempotencyHash: idempotencyHash, RequestHash: requestHash, Audit: audit,
	})
	return value, err
}

func (service *Service) GetAuthorization(ctx context.Context, scope domainrepo.Scope, id string) (entity.ProviderAuthorization, error) {
	if !validScope(scope) {
		return entity.ProviderAuthorization{}, errs.ErrForbidden
	}
	if !validID(id) {
		return entity.ProviderAuthorization{}, errs.ErrInvalid
	}
	value, err := service.repository.GetAuthorization(ctx, scope, id)
	if err != nil {
		return value, err
	}
	if value.State == "CODE_ISSUED" && len(value.EncryptedDeviceResult) > 0 && value.CodeExpiresAt != nil && service.now().UTC().Before(*value.CodeExpiresAt) {
		raw, decryptErr := service.cipher.Decrypt(ctx, value.EncryptedDeviceResult)
		if decryptErr != nil {
			return entity.ProviderAuthorization{}, decryptErr
		}
		var device struct {
			VerificationURL string `json:"verification_url"`
			UserCode        string `json:"user_code"`
		}
		if json.Unmarshal(raw, &device) != nil || !strings.HasPrefix(device.VerificationURL, "https://") || device.UserCode == "" || len(device.UserCode) > 128 {
			return entity.ProviderAuthorization{}, errors.New("stored device authorization result is invalid")
		}
		value.VerificationURL, value.UserCode = device.VerificationURL, device.UserCode
	}
	value.EncryptedDeviceResult = nil
	return value, nil
}

type RestartAuthorizationInput struct {
	Scope           domainrepo.Scope
	AuthorizationID string
	ExpectedVersion uint64
	IdempotencyKey  string
}

func (service *Service) RestartAuthorization(ctx context.Context, input RestartAuthorizationInput) (entity.ProviderAuthorization, error) {
	if !validScope(input.Scope) {
		return entity.ProviderAuthorization{}, errs.ErrForbidden
	}
	if !validID(input.AuthorizationID) || input.ExpectedVersion == 0 {
		return entity.ProviderAuthorization{}, errs.ErrInvalid
	}
	previous, err := service.repository.GetAuthorization(ctx, input.Scope, input.AuthorizationID)
	if err != nil {
		return entity.ProviderAuthorization{}, err
	}
	idempotencyHash, err := hashIdempotency(input.Scope, "provider_authorization.restart", input.IdempotencyKey)
	if err != nil {
		return entity.ProviderAuthorization{}, err
	}
	now := service.now().UTC()
	requestHash := digest([]any{input.Scope.TenantID, input.Scope.ProjectID, previous.ID, input.ExpectedVersion, previous.IntentDigest})
	next := entity.ProviderAuthorization{
		ID: uuid.NewString(), ProviderID: previous.ProviderID, ConnectionID: previous.ConnectionID,
		Attempt: previous.Attempt + 1, Version: 1, Generation: previous.Generation + 1,
		State: "PENDING", IntentDigest: requestHash, ExpiresAt: now.Add(service.config.AuthorizationTTL), CreatedAt: now, UpdatedAt: now,
	}
	audit := managementAudit(input.Scope, "provider_authorization.restart", "PROVIDER_AUTHORIZATION", next.ID, requestHash, "PENDING", now)
	value, _, err := service.repository.RestartAuthorization(ctx, managementrepo.RestartAuthorizationCommand{
		Scope: input.Scope, PreviousID: previous.ID, ExpectedVersion: input.ExpectedVersion,
		Authorization: next, IdempotencyHash: idempotencyHash, RequestHash: requestHash, Audit: audit,
	})
	return value, err
}

func (service *Service) ReauthorizeConnection(ctx context.Context, scope domainrepo.Scope, connectionID string, expectedVersion, expectedGeneration uint64, idempotencyKey string) (entity.ProviderAuthorization, error) {
	if !validScope(scope) {
		return entity.ProviderAuthorization{}, errs.ErrForbidden
	}
	connection, err := service.repository.GetManagedConnection(ctx, scope, connectionID)
	if err != nil {
		return entity.ProviderAuthorization{}, err
	}
	if connection.Version != expectedVersion || connection.Generation != expectedGeneration || connection.Status == "REVOKED" {
		return entity.ProviderAuthorization{}, errs.ErrConflict
	}
	previous, err := service.repository.GetLatestAuthorization(ctx, scope, connectionID)
	if err != nil {
		return entity.ProviderAuthorization{}, err
	}
	return service.RestartAuthorization(ctx, RestartAuthorizationInput{scope, previous.ID, previous.Version, idempotencyKey})
}

func (service *Service) CancelAuthorization(ctx context.Context, scope domainrepo.Scope, id string, expectedVersion uint64, idempotencyKey string) (entity.ProviderAuthorization, error) {
	if !validScope(scope) {
		return entity.ProviderAuthorization{}, errs.ErrForbidden
	}
	if !validID(id) || expectedVersion == 0 {
		return entity.ProviderAuthorization{}, errs.ErrInvalid
	}
	idempotencyHash, err := hashIdempotency(scope, "provider_authorization.cancel", idempotencyKey)
	if err != nil {
		return entity.ProviderAuthorization{}, err
	}
	now := service.now().UTC()
	requestHash := digest([]any{scope.TenantID, scope.ProjectID, id, expectedVersion, "CANCELLED"})
	value, _, err := service.repository.CancelAuthorization(ctx, managementrepo.CancelAuthorizationCommand{
		Scope: scope, AuthorizationID: id, ExpectedVersion: expectedVersion,
		IdempotencyHash: idempotencyHash, RequestHash: requestHash, At: now,
		Audit: managementAudit(scope, "provider_authorization.cancel", "PROVIDER_AUTHORIZATION", id, requestHash, "CANCELLED", now),
	})
	return value, err
}

func (service *Service) GetConnection(ctx context.Context, scope domainrepo.Scope, id string) (entity.ManagedProviderConnection, error) {
	if !validScope(scope) {
		return entity.ManagedProviderConnection{}, errs.ErrForbidden
	}
	if !validID(id) {
		return entity.ManagedProviderConnection{}, errs.ErrInvalid
	}
	return service.repository.GetManagedConnection(ctx, scope, id)
}

func normalizePage(size uint32, token string, maximum int) (int, string, error) {
	if size == 0 {
		size = defaultPageSize
	}
	if size > uint32(maximum) || token != "" && uuid.Validate(token) != nil {
		return 0, "", errs.ErrInvalid
	}
	return int(size), token, nil
}

func (service *Service) ListConnections(ctx context.Context, scope domainrepo.Scope, states []string, size uint32, token string) ([]entity.ManagedProviderConnection, string, error) {
	if !validScope(scope) {
		return nil, "", errs.ErrForbidden
	}
	limit, after, err := normalizePage(size, token, service.config.MaximumPageSize)
	if err != nil {
		return nil, "", err
	}
	allowed := []string{"PENDING", "VALID", "INVALID", "REVOKED"}
	for _, state := range states {
		if !slices.Contains(allowed, state) {
			return nil, "", errs.ErrInvalid
		}
	}
	return service.repository.ListConnections(ctx, scope, states, limit, after)
}

func (service *Service) RevokeConnection(ctx context.Context, scope domainrepo.Scope, id string, expectedVersion, expectedGeneration uint64, idempotencyKey string) (entity.ManagedProviderConnection, error) {
	if !validScope(scope) {
		return entity.ManagedProviderConnection{}, errs.ErrForbidden
	}
	if !validID(id) || expectedVersion == 0 || expectedGeneration == 0 {
		return entity.ManagedProviderConnection{}, errs.ErrInvalid
	}
	idempotencyHash, err := hashIdempotency(scope, "provider_connection.revoke", idempotencyKey)
	if err != nil {
		return entity.ManagedProviderConnection{}, err
	}
	now := service.now().UTC()
	requestHash := digest([]any{scope.TenantID, scope.ProjectID, id, expectedVersion, expectedGeneration, "REVOKED"})
	value, _, err := service.repository.RevokeConnection(ctx, managementrepo.RevokeConnectionCommand{
		Scope: scope, ConnectionID: id, ExpectedVersion: expectedVersion, ExpectedGeneration: expectedGeneration,
		IdempotencyHash: idempotencyHash, RequestHash: requestHash, At: now,
		Audit: managementAudit(scope, "provider_connection.revoke", "PROVIDER_CONNECTION", id, requestHash, "REVOKED", now),
	})
	return value, err
}

func managementAudit(scope domainrepo.Scope, action, kind, id, requestHash, outcome string, at time.Time) entity.AuditEvent {
	return entity.AuditEvent{
		ID: uuid.NewString(), TenantID: scope.TenantID, ProjectID: scope.ProjectID,
		ActorID: scope.ActorID, Action: action, ResourceKind: kind, ResourceID: id,
		RequestHash: requestHash, Outcome: outcome, ReasonCode: "OWNER_COMMAND", OccurredAt: at,
	}
}
