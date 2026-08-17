package management

import (
	"context"
	"slices"
	"sort"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	managementrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/management"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
)

type PoolMemberInput struct {
	ConnectionID                        string
	ExpectedVersion, ExpectedGeneration uint64
	Weight                              uint32
}

type ManagePoolInput struct {
	Scope                                                      domainrepo.Scope
	Action, ID, StableKey, DisplayName, Policy, IdempotencyKey string
	ExpectedVersion                                            uint64
	Members                                                    []PoolMemberInput
}

func (service *Service) ManagePool(ctx context.Context, input ManagePoolInput) (entity.ManagedProviderPool, error) {
	if !validScope(input.Scope) {
		return entity.ManagedProviderPool{}, errsForbidden()
	}
	if !slices.Contains([]string{"CREATE", "UPDATE", "ARCHIVE", "DELETE"}, input.Action) ||
		input.Action == "CREATE" && input.ID != "" || input.Action != "CREATE" && !validID(input.ID) ||
		input.Action != "CREATE" && input.ExpectedVersion == 0 {
		return entity.ManagedProviderPool{}, errsInvalid()
	}
	if (input.Action == "CREATE" || input.Action == "UPDATE") &&
		(!validStableKey(input.StableKey) || input.DisplayName == "" || len(input.DisplayName) > maximumDisplayNameSize ||
			!slices.Contains([]string{"LEAST_USED", "WEIGHTED"}, input.Policy) || len(input.Members) == 0 || len(input.Members) > 64) {
		return entity.ManagedProviderPool{}, errsInvalid()
	}
	retainedMembers := map[string]entity.ProviderPoolMember{}
	var current entity.ManagedProviderPool
	if input.Action != "CREATE" {
		var getErr error
		current, getErr = service.repository.GetPool(ctx, input.Scope, input.ID)
		if getErr != nil {
			return entity.ManagedProviderPool{}, getErr
		}
		if input.Action == "ARCHIVE" || input.Action == "DELETE" {
			input.StableKey, input.DisplayName, input.Policy = current.StableKey, current.DisplayName, current.Policy
			input.Members = make([]PoolMemberInput, 0, len(current.Members))
			for _, member := range current.Members {
				input.Members = append(input.Members, PoolMemberInput{ConnectionID: member.ConnectionID, ExpectedVersion: member.ConnectionVersion, ExpectedGeneration: member.ConnectionGeneration, Weight: member.Weight})
				member.Eligible = false
				retainedMembers[member.ConnectionID] = member
			}
		}
	}
	idempotencyHash, err := hashIdempotency(input.Scope, "provider_pool."+input.Action, input.IdempotencyKey)
	if err != nil {
		return entity.ManagedProviderPool{}, err
	}
	now := service.now().UTC()
	id := input.ID
	if id == "" {
		id = uuid.NewString()
	}
	desiredMembers := make([]PoolMemberInput, 0, len(input.Members))
	seen := make(map[string]struct{}, len(input.Members))
	for _, requested := range input.Members {
		if !validID(requested.ConnectionID) || requested.ExpectedVersion == 0 || requested.ExpectedGeneration == 0 || requested.Weight == 0 || requested.Weight > 10000 {
			return entity.ManagedProviderPool{}, errsInvalid()
		}
		if _, duplicate := seen[requested.ConnectionID]; duplicate {
			return entity.ManagedProviderPool{}, errsInvalid()
		}
		seen[requested.ConnectionID] = struct{}{}
		desiredMembers = append(desiredMembers, requested)
	}
	sort.Slice(desiredMembers, func(left, right int) bool {
		return desiredMembers[left].ConnectionID < desiredMembers[right].ConnectionID
	})
	requestHash := digest([]any{input.Scope.TenantID, input.Scope.ProjectID, input.Action, input.ID, input.ExpectedVersion, input.StableKey, input.DisplayName, input.Policy, desiredMembers})
	if replay, found, replayErr := replayManagement[entity.ManagedProviderPool](ctx, service.repository, input.Scope, "provider_pool."+input.Action, idempotencyHash, requestHash); replayErr != nil || found {
		return replay, replayErr
	}
	if input.Action != "CREATE" && (current.Version != input.ExpectedVersion ||
		input.Action == "UPDATE" && (current.Status != "ACTIVE" || current.StableKey != input.StableKey) ||
		input.Action == "ARCHIVE" && current.Status != "ACTIVE" ||
		input.Action == "DELETE" && current.Status != "ARCHIVED") {
		return entity.ManagedProviderPool{}, errsConflict()
	}
	members := make([]entity.ProviderPoolMember, 0, len(input.Members))
	for _, requested := range input.Members {
		if retained, ok := retainedMembers[requested.ConnectionID]; ok {
			members = append(members, retained)
			continue
		}
		connection, getErr := service.repository.GetManagedConnection(ctx, input.Scope, requested.ConnectionID)
		if getErr != nil {
			return entity.ManagedProviderPool{}, getErr
		}
		if connection.Version != requested.ExpectedVersion || connection.Generation != requested.ExpectedGeneration {
			return entity.ManagedProviderPool{}, errsConflict()
		}
		members = append(members, entity.ProviderPoolMember{
			ConnectionID: connection.ID, ConnectionStableKey: connection.StableKey, ConnectionVersion: connection.Version,
			ConnectionGeneration: connection.Generation, ObservationDigest: connection.ObservationDigest,
			Capacity: connection.Capacity,
			Weight:   requested.Weight, Eligible: connectionEligible(connection, now),
			ControlPlaneID: connection.ControlPlaneID, ControlPlaneVersion: connection.ControlPlaneVersion,
			ControlPlaneDigest: connection.ControlPlaneDigest,
		})
	}
	sort.Slice(members, func(left, right int) bool { return members[left].ConnectionID < members[right].ConnectionID })
	desiredDigest := digest(struct {
		StableKey, DisplayName, Policy string
		Members                        []PoolMemberInput
	}{input.StableKey, input.DisplayName, input.Policy, desiredMembers})
	observationDigest := digest(members)
	status := "PENDING"
	if input.Action == "ARCHIVE" {
		status = "ARCHIVED"
	}
	if input.Action == "DELETE" {
		status = "DELETED"
	}
	pool := entity.ManagedProviderPool{
		ID: id, StableKey: input.StableKey, DisplayName: input.DisplayName, Policy: input.Policy,
		Version: 1, DesiredDigest: desiredDigest, ObservationVersion: 1,
		ObservationDigest: observationDigest, EffectiveDigest: digest([]string{desiredDigest, observationDigest}),
		Status: status, Members: members, CreatedAt: now, UpdatedAt: now,
	}
	value, _, err := service.repository.ManagePool(ctx, managementrepo.ManagePoolCommand{
		Scope: input.Scope, Action: input.Action, ExpectedVersion: input.ExpectedVersion, Pool: pool,
		IdempotencyHash: idempotencyHash, RequestHash: requestHash,
		Audit: managementAudit(input.Scope, "provider_pool."+input.Action, "PROVIDER_POOL", id, requestHash, status, now),
	})
	return value, err
}

func (service *Service) GetPool(ctx context.Context, scope domainrepo.Scope, id string) (entity.ManagedProviderPool, error) {
	if !validScope(scope) {
		return entity.ManagedProviderPool{}, errsForbidden()
	}
	if !validID(id) {
		return entity.ManagedProviderPool{}, errsInvalid()
	}
	return service.repository.GetPool(ctx, scope, id)
}

func (service *Service) ListPools(ctx context.Context, scope domainrepo.Scope, size uint32, token string) ([]entity.ManagedProviderPool, string, error) {
	if !validScope(scope) {
		return nil, "", errsForbidden()
	}
	limit, after, err := normalizePage(size, token, service.config.MaximumPageSize)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListPools(ctx, scope, limit, after)
}

func (service *Service) ListDefinitions(scope domainrepo.Scope) ([]entity.Definition, error) {
	if !validScope(scope) {
		return nil, errsForbidden()
	}
	return service.definitions.List(), nil
}

func (service *Service) GetDefinition(scope domainrepo.Scope, id string, version uint64, expectedDigest string) (entity.Definition, error) {
	if !validScope(scope) {
		return entity.Definition{}, errsForbidden()
	}
	if id == "" || version == 0 {
		return entity.Definition{}, errsInvalid()
	}
	value, ok := service.definitions.Get(id, version)
	if !ok {
		return value, errsNotFound()
	}
	if expectedDigest != "" && expectedDigest != value.Digest {
		return entity.Definition{}, errsConflict()
	}
	return value, nil
}

type ConfigureIntegrationInput struct {
	Scope                                   domainrepo.Scope
	ID                                      string
	ExpectedVersion                         uint64
	StableKey, DefinitionID                 string
	DefinitionVersion                       uint64
	DefinitionDigest, ConnectionID          string
	ConnectionVersion, ConnectionGeneration uint64
	Capabilities                            []string
	EffectKind, IdempotencyKey              string
}

func (service *Service) ConfigureIntegration(ctx context.Context, input ConfigureIntegrationInput) (entity.IntegrationConfiguration, error) {
	if !validScope(input.Scope) {
		return entity.IntegrationConfiguration{}, errsForbidden()
	}
	if input.ID != "" && !validID(input.ID) || (input.ID == "") != (input.ExpectedVersion == 0) ||
		!validStableKey(input.StableKey) || input.DefinitionID == "" || input.DefinitionVersion == 0 ||
		!validID(input.ConnectionID) || input.ConnectionVersion == 0 || input.ConnectionGeneration == 0 ||
		!slices.Contains([]string{"MCP_TOOL", "CLI", "ENVIRONMENT"}, input.EffectKind) || len(input.Capabilities) == 0 || len(input.Capabilities) > 64 {
		return entity.IntegrationConfiguration{}, errsInvalid()
	}
	definition, ok := service.definitions.Get(input.DefinitionID, input.DefinitionVersion)
	if !ok {
		return entity.IntegrationConfiguration{}, errsNotFound()
	}
	if definition.Digest != input.DefinitionDigest {
		return entity.IntegrationConfiguration{}, errsConflict()
	}
	capabilities := slices.Clone(input.Capabilities)
	sort.Strings(capabilities)
	for index, capability := range capabilities {
		if index > 0 && capabilities[index-1] == capability {
			return entity.IntegrationConfiguration{}, errsInvalid()
		}
	}
	idempotencyHash, err := hashIdempotency(input.Scope, "integration.configure", input.IdempotencyKey)
	if err != nil {
		return entity.IntegrationConfiguration{}, err
	}
	requestHash := digest([]any{input.Scope.TenantID, input.Scope.ProjectID, input.ID, input.ExpectedVersion, input.StableKey, input.DefinitionID, input.DefinitionVersion, input.DefinitionDigest, input.ConnectionID, input.ConnectionVersion, input.ConnectionGeneration, capabilities, input.EffectKind})
	var current entity.IntegrationConfiguration
	if input.ID != "" {
		current, err = service.repository.GetIntegrationConfiguration(ctx, input.Scope, input.ID)
		if err != nil {
			return entity.IntegrationConfiguration{}, err
		}
	}
	if replay, found, replayErr := replayManagement[entity.IntegrationConfiguration](ctx, service.repository, input.Scope, "integration.configure", idempotencyHash, requestHash); replayErr != nil || found {
		return replay, replayErr
	}
	connection, err := service.repository.GetManagedConnection(ctx, input.Scope, input.ConnectionID)
	if err != nil {
		return entity.IntegrationConfiguration{}, err
	}
	if connection.Version != input.ConnectionVersion || connection.Generation != input.ConnectionGeneration || !connectionEligible(connection, service.now().UTC()) {
		return entity.IntegrationConfiguration{}, errsConflict()
	}
	allowedCapabilities := make(map[string]struct{})
	effectAllowed := input.EffectKind == "MCP_TOOL"
	for _, tool := range definition.Tools {
		if slices.Contains(connection.Capabilities, tool.Capability) {
			allowedCapabilities[tool.Capability] = struct{}{}
		}
		providerAllows := slices.Contains(connection.Capabilities, tool.Capability)
		if tool.DirectDelivery != nil && providerAllows && input.EffectKind == "CLI" && len(tool.DirectDelivery.CLINames) > 0 ||
			tool.DirectDelivery != nil && providerAllows && input.EffectKind == "ENVIRONMENT" && len(tool.DirectDelivery.EnvironmentNames) > 0 {
			effectAllowed = true
		}
	}
	if !effectAllowed {
		return entity.IntegrationConfiguration{}, errsForbidden()
	}
	for _, capability := range capabilities {
		if _, allowed := allowedCapabilities[capability]; !allowed {
			return entity.IntegrationConfiguration{}, errsForbidden()
		}
	}
	now := service.now().UTC()
	id := input.ID
	if id == "" {
		id = uuid.NewString()
	}
	version := uint64(1)
	createdAt := now
	if input.ID != "" {
		version, createdAt = current.Version+1, current.CreatedAt
	}
	configuration := entity.IntegrationConfiguration{
		ID: id, StableKey: input.StableKey, Version: version, DefinitionID: definition.ID,
		DefinitionVersion: definition.Version, DefinitionDigest: definition.Digest,
		ConnectionID: connection.ID, ConnectionVersion: connection.Version,
		ConnectionGeneration: connection.Generation, Capabilities: capabilities,
		CapabilityDigest: digest(capabilities), EffectKind: input.EffectKind, Status: "ACTIVE", CreatedAt: createdAt, UpdatedAt: now,
	}
	configuration.Digest = digest(configuration)
	if input.ID != "" && (current.Version != input.ExpectedVersion || current.StableKey != input.StableKey || current.Status != "ACTIVE") {
		return entity.IntegrationConfiguration{}, errsConflict()
	}
	value, _, err := service.repository.ConfigureIntegration(ctx, managementrepo.ConfigureIntegrationCommand{
		Scope: input.Scope, ExpectedVersion: input.ExpectedVersion, Configuration: configuration,
		IdempotencyHash: idempotencyHash, RequestHash: requestHash,
		Audit: managementAudit(input.Scope, "integration.configure", "INTEGRATION_CONFIGURATION", id, requestHash, "ACTIVE", now),
	})
	return value, err
}

func (service *Service) GetConfiguration(ctx context.Context, scope domainrepo.Scope, id string) (entity.IntegrationConfiguration, error) {
	if !validScope(scope) {
		return entity.IntegrationConfiguration{}, errsForbidden()
	}
	if !validID(id) {
		return entity.IntegrationConfiguration{}, errsInvalid()
	}
	return service.repository.GetIntegrationConfiguration(ctx, scope, id)
}

func (service *Service) ListConfigurations(ctx context.Context, scope domainrepo.Scope, size uint32, token string) ([]entity.IntegrationConfiguration, string, error) {
	if !validScope(scope) {
		return nil, "", errsForbidden()
	}
	limit, after, err := normalizePage(size, token, service.config.MaximumPageSize)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListIntegrationConfigurations(ctx, scope, limit, after)
}

type TestConnectionInput struct {
	Scope                                                         domainrepo.Scope
	ConnectionID, DefinitionID, DefinitionDigest, ConfigurationID string
	ConnectionVersion, ConnectionGeneration, DefinitionVersion    uint64
	ConfigurationVersion                                          uint64
	ConfigurationDigest, IdempotencyKey                           string
}

func (service *Service) TestConnection(ctx context.Context, input TestConnectionInput) (entity.IntegrationTestReceipt, error) {
	if !validScope(input.Scope) {
		return entity.IntegrationTestReceipt{}, errsForbidden()
	}
	if !validID(input.ConnectionID) || input.ConnectionVersion == 0 || input.ConnectionGeneration == 0 || input.DefinitionID == "" || input.DefinitionVersion == 0 ||
		len(input.DefinitionDigest) != 64 || !validID(input.ConfigurationID) || input.ConfigurationVersion == 0 || len(input.ConfigurationDigest) != 64 {
		return entity.IntegrationTestReceipt{}, errsInvalid()
	}
	definition, ok := service.definitions.Get(input.DefinitionID, input.DefinitionVersion)
	if !ok {
		return entity.IntegrationTestReceipt{}, errsNotFound()
	}
	if definition.Digest != input.DefinitionDigest {
		return entity.IntegrationTestReceipt{}, errsConflict()
	}
	idempotencyHash, err := hashIdempotency(input.Scope, "integration.test", input.IdempotencyKey)
	if err != nil {
		return entity.IntegrationTestReceipt{}, err
	}
	requestHash := digest([]any{input.Scope.TenantID, input.Scope.ProjectID, input.ConnectionID, input.ConnectionVersion, input.ConnectionGeneration, definition.ID, definition.Version, definition.Digest, input.ConfigurationID, input.ConfigurationVersion, input.ConfigurationDigest})
	if replay, found, replayErr := replayManagement[entity.IntegrationTestReceipt](ctx, service.repository, input.Scope, "integration.test", idempotencyHash, requestHash); replayErr != nil || found {
		return replay, replayErr
	}
	configuration, err := service.repository.GetIntegrationConfiguration(ctx, input.Scope, input.ConfigurationID)
	if err != nil {
		return entity.IntegrationTestReceipt{}, err
	}
	if configuration.Version != input.ConfigurationVersion || configuration.Digest != input.ConfigurationDigest || configuration.Status != "ACTIVE" ||
		configuration.DefinitionID != definition.ID || configuration.DefinitionVersion != definition.Version || configuration.DefinitionDigest != definition.Digest ||
		configuration.ConnectionID != input.ConnectionID || configuration.ConnectionVersion != input.ConnectionVersion || configuration.ConnectionGeneration != input.ConnectionGeneration {
		return entity.IntegrationTestReceipt{}, errsConflict()
	}
	connection, err := service.repository.GetManagedConnection(ctx, input.Scope, input.ConnectionID)
	if err != nil {
		return entity.IntegrationTestReceipt{}, err
	}
	if connection.Version != input.ConnectionVersion || connection.Generation != input.ConnectionGeneration || !connectionEligible(connection, service.now().UTC()) {
		return entity.IntegrationTestReceipt{}, errsConflict()
	}
	now := service.now().UTC()
	receipt := entity.IntegrationTestReceipt{
		ID: uuid.NewString(), ConnectionID: connection.ID, ConnectionVersion: connection.Version, ConnectionGeneration: connection.Generation,
		DefinitionID: definition.ID, DefinitionVersion: definition.Version, DefinitionDigest: definition.Digest,
		ConfigurationID: configuration.ID, ConfigurationVersion: configuration.Version, ConfigurationDigest: configuration.Digest,
		CredentialGeneration: connection.ActiveCredential, CredentialBindingID: connection.CredentialBindingID,
		CredentialBindingVersion: connection.CredentialBindingVersion, CredentialBindingDigest: connection.CredentialBindingDigest,
		Category: "PENDING", Digest: requestHash, ExpiresAt: now.Add(5 * time.Minute),
	}
	value, _, err := service.repository.CreateTest(ctx, managementrepo.CreateTestCommand{
		Scope: input.Scope, Receipt: receipt, Connection: connection, IdempotencyHash: idempotencyHash, RequestHash: requestHash, At: now,
		Audit: managementAudit(input.Scope, "integration.test", "INTEGRATION_TEST", receipt.ID, requestHash, "PENDING", now),
	})
	return value, err
}

func (service *Service) GetTestReceipt(ctx context.Context, scope domainrepo.Scope, id string) (entity.IntegrationTestReceipt, error) {
	if !validScope(scope) {
		return entity.IntegrationTestReceipt{}, errsForbidden()
	}
	if !validID(id) {
		return entity.IntegrationTestReceipt{}, errsInvalid()
	}
	return service.repository.GetTest(ctx, scope, id)
}

func (service *Service) ListApprovals(ctx context.Context, scope domainrepo.Scope, states []string, size uint32, token string) ([]entity.Approval, string, error) {
	if !validScope(scope) {
		return nil, "", errsForbidden()
	}
	limit, after, err := normalizePage(size, token, service.config.MaximumPageSize)
	if err != nil {
		return nil, "", err
	}
	allowed := []string{"PENDING", "APPROVED", "REJECTED", "EXPIRED", "CANCELLED"}
	for _, state := range states {
		if !slices.Contains(allowed, state) {
			return nil, "", errsInvalid()
		}
	}
	return service.repository.ListApprovals(ctx, scope, states, limit, after)
}

func (service *Service) GetApproval(ctx context.Context, scope domainrepo.Scope, id string) (entity.Approval, error) {
	if !validScope(scope) {
		return entity.Approval{}, errsForbidden()
	}
	if !validID(id) {
		return entity.Approval{}, errsInvalid()
	}
	return service.repository.GetApproval(ctx, scope, id)
}

func (service *Service) DecideApproval(ctx context.Context, scope domainrepo.Scope, id string, version uint64, requestHash string, approve bool, reasonCode, idempotencyKey string) (entity.Approval, error) {
	if !validScope(scope) {
		return entity.Approval{}, errsForbidden()
	}
	_, err := service.invocations.DecideExact(ctx, scope, id, version, requestHash, approve, reasonCode, idempotencyKey)
	if err != nil {
		return entity.Approval{}, err
	}
	return service.repository.GetApproval(ctx, scope, id)
}

type ManageGitBindingInput struct {
	Scope                                                                       domainrepo.Scope
	Action, ID, StableKey                                                       string
	ExpectedVersion                                                             uint64
	RepositoryKey, RefKey, PathKey, TargetKind, TargetStableKey, IdempotencyKey string
}

func (service *Service) ManageGitBinding(ctx context.Context, input ManageGitBindingInput) (entity.GitSourceBinding, error) {
	if !validScope(input.Scope) {
		return entity.GitSourceBinding{}, errsForbidden()
	}
	if !slices.Contains([]string{"CREATE", "UPDATE", "ARCHIVE"}, input.Action) ||
		input.Action == "CREATE" && input.ID != "" || input.Action != "CREATE" && !validID(input.ID) ||
		input.Action != "CREATE" && input.ExpectedVersion == 0 {
		return entity.GitSourceBinding{}, errsInvalid()
	}
	var previous entity.GitSourceBinding
	if input.Action != "CREATE" {
		current, getErr := service.repository.GetGitBinding(ctx, input.Scope, input.ID)
		if getErr != nil {
			return entity.GitSourceBinding{}, getErr
		}
		if input.Action == "ARCHIVE" {
			input.StableKey, input.RepositoryKey, input.RefKey, input.PathKey = current.StableKey, current.RepositoryKey, current.RefKey, current.PathKey
			input.TargetKind, input.TargetStableKey = current.TargetKind, current.TargetStableKey
		}
		previous = current
	}
	if !validStableKey(input.StableKey) || !validStableKey(input.TargetStableKey) ||
		!slices.Contains([]string{"ROLE_DEFINITION", "AGENT", "INSTRUCTION_SET", "PROVIDER_POOL"}, input.TargetKind) {
		return entity.GitSourceBinding{}, errsInvalid()
	}
	var source entity.GitSource
	var ok bool
	if input.Action == "ARCHIVE" {
		source = entity.GitSource{
			RepositoryKey: input.RepositoryKey, RefKey: input.RefKey, PathKey: input.PathKey,
			RepositoryConnectionID: previous.RepositoryConnectionID, RepositoryConnectionVersion: previous.RepositoryConnectionVersion,
			RepositoryConnectionDigest: previous.RepositoryConnectionDigest, CredentialBindingID: previous.CredentialBindingID,
			CredentialBindingVersion: previous.CredentialBindingVersion, CredentialBindingDigest: previous.CredentialBindingDigest,
		}
		ok = true
	} else {
		source, ok = service.gitSources.Resolve(input.RepositoryKey, input.RefKey, input.PathKey)
	}
	if !ok {
		return entity.GitSourceBinding{}, errsInvalid()
	}
	idempotencyHash, err := hashIdempotency(input.Scope, "git_binding."+input.Action, input.IdempotencyKey)
	if err != nil {
		return entity.GitSourceBinding{}, err
	}
	now := service.now().UTC()
	id := input.ID
	if id == "" {
		id = uuid.NewString()
	}
	binding := entity.GitSourceBinding{
		ID: id, StableKey: input.StableKey, Version: 1, Generation: 1, Status: "ACTIVE",
		RepositoryKey: source.RepositoryKey, RefKey: source.RefKey, PathKey: source.PathKey,
		RepositoryConnectionID: source.RepositoryConnectionID, RepositoryConnectionVersion: source.RepositoryConnectionVersion,
		RepositoryConnectionDigest: source.RepositoryConnectionDigest,
		CredentialBindingID:        source.CredentialBindingID, CredentialBindingVersion: source.CredentialBindingVersion,
		CredentialBindingDigest: source.CredentialBindingDigest, TargetKind: input.TargetKind,
		TargetStableKey: input.TargetStableKey, CreatedAt: now, UpdatedAt: now,
	}
	if previous.ID != "" && previous.RepositoryKey == input.RepositoryKey &&
		previous.RefKey == input.RefKey && previous.PathKey == input.PathKey {
		binding.FetchedCommit, binding.SourceRevision, binding.SourceDigest = previous.FetchedCommit, previous.SourceRevision, previous.SourceDigest
		binding.FetchedAt = previous.FetchedAt
	}
	if input.Action == "ARCHIVE" {
		binding.Status = "ARCHIVED"
	}
	requestHash := digest([]any{input.Scope.TenantID, input.Scope.ProjectID, input.Action, input.ID, input.ExpectedVersion, input.StableKey, input.RepositoryKey, input.RefKey, input.PathKey, source.RepositoryConnectionID, source.RepositoryConnectionVersion, source.RepositoryConnectionDigest, source.CredentialBindingID, source.CredentialBindingVersion, source.CredentialBindingDigest, input.TargetKind, input.TargetStableKey})
	if replay, found, replayErr := replayManagement[entity.GitSourceBinding](ctx, service.repository, input.Scope, "git_binding."+input.Action, idempotencyHash, requestHash); replayErr != nil || found {
		return replay, replayErr
	}
	if input.Action != "CREATE" && (previous.Version != input.ExpectedVersion || previous.Status != "ACTIVE" ||
		input.Action == "UPDATE" && (previous.StableKey != input.StableKey || previous.TargetKind != input.TargetKind || previous.TargetStableKey != input.TargetStableKey)) {
		return entity.GitSourceBinding{}, errsConflict()
	}
	value, _, err := service.repository.ManageGitBinding(ctx, managementrepo.ManageGitBindingCommand{
		Scope: input.Scope, Action: input.Action, ExpectedVersion: input.ExpectedVersion, Binding: binding,
		IdempotencyHash: idempotencyHash, RequestHash: requestHash,
		Audit: managementAudit(input.Scope, "git_binding."+input.Action, "GIT_SOURCE_BINDING", id, requestHash, binding.Status, now),
	})
	return value, err
}

func (service *Service) GetGitBinding(ctx context.Context, scope domainrepo.Scope, id string) (entity.GitSourceBinding, error) {
	if !validScope(scope) {
		return entity.GitSourceBinding{}, errsForbidden()
	}
	if !validID(id) {
		return entity.GitSourceBinding{}, errsInvalid()
	}
	return service.repository.GetGitBinding(ctx, scope, id)
}

func (service *Service) ListGitBindings(ctx context.Context, scope domainrepo.Scope, size uint32, token string) ([]entity.GitSourceBinding, string, error) {
	if !validScope(scope) {
		return nil, "", errsForbidden()
	}
	limit, after, err := normalizePage(size, token, service.config.MaximumPageSize)
	if err != nil {
		return nil, "", err
	}
	return service.repository.ListGitBindings(ctx, scope, limit, after)
}

func (service *Service) ReconcileGitBinding(ctx context.Context, scope domainrepo.Scope, id string, expectedVersion, expectedSourceRevision uint64, idempotencyKey string) (entity.GitReconciliation, error) {
	if !validScope(scope) {
		return entity.GitReconciliation{}, errsForbidden()
	}
	if !validID(id) || expectedVersion == 0 {
		return entity.GitReconciliation{}, errsInvalid()
	}
	binding, err := service.repository.GetGitBinding(ctx, scope, id)
	if err != nil {
		return entity.GitReconciliation{}, err
	}
	idempotencyHash, err := hashIdempotency(scope, "git_binding.reconcile", idempotencyKey)
	if err != nil {
		return entity.GitReconciliation{}, err
	}
	now := service.now().UTC()
	requestHash := digest([]any{scope.TenantID, scope.ProjectID, binding.ID, expectedVersion, expectedSourceRevision, binding.RepositoryConnectionDigest, binding.CredentialBindingDigest})
	if replay, found, replayErr := replayManagement[entity.GitReconciliation](ctx, service.repository, scope, "git_binding.reconcile", idempotencyHash, requestHash); replayErr != nil || found {
		return replay, replayErr
	}
	if binding.Version != expectedVersion || binding.SourceRevision != expectedSourceRevision || binding.Status != "ACTIVE" {
		return entity.GitReconciliation{}, errsConflict()
	}
	reconciliation := entity.GitReconciliation{
		ID: uuid.NewString(), BindingID: binding.ID, BindingVersion: binding.Version, State: "PENDING",
		ReceiptID: uuid.NewString(), ReceiptDigest: digest([]string{requestHash, binding.ID}), UpdatedAt: now,
	}
	value, _, err := service.repository.CreateGitReconciliation(ctx, managementrepo.ReconcileGitCommand{
		Scope: scope, BindingID: binding.ID, ExpectedVersion: expectedVersion, ExpectedSourceRevision: expectedSourceRevision,
		Reconciliation: reconciliation, IdempotencyHash: idempotencyHash, RequestHash: requestHash,
		Audit: managementAudit(scope, "git_binding.reconcile", "GIT_RECONCILIATION", reconciliation.ID, requestHash, "PENDING", now),
	})
	return value, err
}

func (service *Service) Diagnostics(ctx context.Context, scope domainrepo.Scope) (map[string]string, error) {
	if !validHealthScope(scope) {
		return nil, errsForbidden()
	}
	result := map[string]string{
		"postgres": "READY", "git_catalog": "READY", "provider_catalog": "READY",
		"provider_secret_boundary": "READY", "git_secret_boundary": "READY", "provider_adapter": "READY",
		"git_adapter": "READY", "control_plane": "READY",
	}
	if err := service.repository.CheckManagement(ctx); err != nil {
		result["postgres"] = "UNAVAILABLE"
	}
	if err := service.gitSources.Check(ctx); err != nil {
		result["git_catalog"] = "UNAVAILABLE"
	}
	if service.worker == nil {
		result["provider_secret_boundary"], result["git_secret_boundary"], result["provider_adapter"], result["git_adapter"], result["control_plane"] = "UNAVAILABLE", "UNAVAILABLE", "UNAVAILABLE", "UNAVAILABLE", "UNAVAILABLE"
		return result, nil
	}
	if err := service.worker.Secrets.Check(ctx); err != nil {
		result["provider_secret_boundary"] = "UNAVAILABLE"
	}
	if err := service.worker.GitSecrets.Check(ctx); err != nil {
		result["git_secret_boundary"] = "UNAVAILABLE"
	}
	if err := service.worker.Authorizer.Check(ctx); err != nil {
		result["provider_adapter"] = "UNAVAILABLE"
	}
	if err := service.worker.Git.Check(ctx); err != nil {
		result["git_adapter"] = "UNAVAILABLE"
	}
	if err := service.worker.Effects.Check(ctx); err != nil {
		result["control_plane"] = "UNAVAILABLE"
	}
	return result, nil
}

func definitionCapabilities(definition entity.Definition) []entity.ProviderCapability {
	seen := make(map[string]entity.ProviderCapability)
	for _, tool := range definition.Tools {
		seen[tool.Capability] = entity.ProviderCapability{Name: tool.Capability, Risk: string(tool.Risk), RequiresApproval: tool.ApprovalPolicy == enum.ApprovalAlways || tool.Risk.RequiresApproval()}
	}
	values := make([]entity.ProviderCapability, 0, len(seen))
	for _, value := range seen {
		values = append(values, value)
	}
	sort.Slice(values, func(left, right int) bool { return values[left].Name < values[right].Name })
	return values
}

func connectionEligible(connection entity.ManagedProviderConnection, now time.Time) bool {
	return connection.Status == "VALID" && connection.Generation > 0 && connection.ActiveCredential == connection.Generation &&
		validID(connection.CredentialBindingID) && connection.CredentialBindingVersion > 0 && digestPattern.MatchString(connection.CredentialBindingDigest) &&
		validID(connection.ControlPlaneID) && connection.ControlPlaneVersion > 0 && digestPattern.MatchString(connection.ControlPlaneDigest) &&
		connection.ObservedAt != nil && !connection.ObservedAt.After(now.Add(time.Second)) && now.Sub(*connection.ObservedAt) <= 5*time.Minute &&
		connection.Capacity.Limit > 0 && connection.Capacity.Usage <= connection.Capacity.Limit &&
		connection.Capacity.Revision > 0 && digestPattern.MatchString(connection.Capacity.Digest) && connection.Capacity.ExpiresAt.After(now)
}

func errsForbidden() error { return errs.ErrForbidden }
func errsInvalid() error   { return errs.ErrInvalid }
func errsConflict() error  { return errs.ErrConflict }
func errsNotFound() error  { return errs.ErrNotFound }
