package prototypematerial

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

const staticRoleStateVersion = 1

type staticRoleDefinition struct {
	Role         string
	Principal    string
	DatabaseName string
	SecretName   string
	Capability   string
	Generation   uint64
	Status       string
}

type staticRoleRecord struct {
	Principal            string `json:"principal"`
	DatabaseName         string `json:"database_name"`
	Capability           string `json:"capability"`
	Generation           uint64 `json:"generation"`
	Status               string `json:"status"`
	CredentialDigest     string `json:"credential_digest_sha256,omitempty"`
	CredentialSecretName string `json:"credential_secret_name,omitempty"`
}

type staticRoleState struct {
	Version             int                         `json:"version"`
	Revision            uint64                      `json:"revision"`
	SourceRevision      uint64                      `json:"source_revision"`
	SourceDigest        string                      `json:"source_digest_sha256"`
	GenerationHighWater map[string]uint64           `json:"generation_high_water"`
	Roles               map[string]staticRoleRecord `json:"roles"`
}

// StaticRoleManager отображает Vault static-role lifecycle на pre-materialized
// PostgreSQL generations и exact Kubernetes Secrets prototype-профиля.
type StaticRoleManager struct {
	api            *KubernetesDelivery
	registry       map[string]staticRoleDefinition
	sourceRevision uint64
	sourceDigest   string
}

func NewStaticRoleManager(
	config KubernetesConfig,
	sourceRevision uint64,
	sourceDigest string,
) (*StaticRoleManager, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != Namespace ||
		config.CAFile != KubernetesCAFile || config.TokenFile != KubernetesTokenFile ||
		config.Timeout < time.Second || config.Timeout > 10*time.Second ||
		sourceRevision == 0 || !validSHA256(sourceDigest) {
		return nil, errors.New("prototype static role manager configuration is invalid")
	}
	client, err := newKubernetesHTTPClient(config)
	if err != nil {
		return nil, err
	}
	registry := staticRoleRegistry()
	if len(registry) != 10 {
		return nil, errors.New("prototype static role registry is incomplete")
	}
	return &StaticRoleManager{
		api: &KubernetesDelivery{config: config, client: client}, registry: registry,
		sourceRevision: sourceRevision, sourceDigest: sourceDigest,
	}, nil
}

func (manager *StaticRoleManager) VerifyStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	definitions, err := manager.activeDefinitions(roles)
	if err != nil {
		return err
	}
	digests, err := manager.readDigests(ctx, definitions)
	if err != nil {
		return err
	}
	state, found, secret, err := manager.readState(ctx)
	if err != nil {
		return err
	}
	if !found {
		state = manager.initialState(digests)
		return manager.writeState(ctx, secret, state)
	}
	if err := manager.validateState(state); err != nil {
		return err
	}
	for _, definition := range definitions {
		record := state.Roles[definition.Role]
		if record.Status == "RETIRED" || record.CredentialDigest != digests[definition.Role] {
			return errors.New("prototype active static role readback mismatch")
		}
	}
	return nil
}

func (manager *StaticRoleManager) RotateStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	if len(roles) == 0 {
		return nil
	}
	definitions, err := manager.activeDefinitions(roles)
	if err != nil {
		return err
	}
	state, found, _, err := manager.readState(ctx)
	if err != nil || !found {
		return errors.New("prototype static role state is absent before rotation")
	}
	if err := manager.validateState(state); err != nil {
		return err
	}
	for _, definition := range definitions {
		record := state.Roles[definition.Role]
		highWater := state.GenerationHighWater[definition.Capability]
		if definition.Generation+1 < highWater || record.Status == "RETIRED" {
			return errors.New("prototype static role generation rollback rejected")
		}
		if definition.Status != "CURRENT" || record.Status != "CURRENT" {
			return errors.New("prototype static role rotation transition is invalid")
		}
	}
	digests, err := manager.readDigests(ctx, definitions)
	if err != nil {
		return err
	}
	for _, definition := range definitions {
		if state.Roles[definition.Role].CredentialDigest != digests[definition.Role] {
			return errors.New("prototype static role rotation readback mismatch")
		}
	}
	return nil
}

func (manager *StaticRoleManager) RevokeStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	definitions, err := manager.retiredDefinitions(roles)
	if err != nil {
		return err
	}
	if len(definitions) == 0 {
		return nil
	}
	state, found, secret, err := manager.readState(ctx)
	if err != nil || !found {
		return errors.New("prototype static role state is absent before revoke")
	}
	if err := manager.validateState(state); err != nil {
		return err
	}
	changed := false
	for _, definition := range definitions {
		if definition.SecretName != "" {
			if _, secretFound, readErr := manager.api.readSecret(ctx, definition.SecretName); readErr != nil || secretFound {
				return errors.New("prototype retired credential Secret remains reachable")
			}
		}
		record := state.Roles[definition.Role]
		if record.Status != "RETIRED" {
			record.Status = "RETIRED"
			record.CredentialDigest = ""
			record.CredentialSecretName = ""
			state.Roles[definition.Role] = record
			changed = true
		}
	}
	if !changed {
		return nil
	}
	state.Revision++
	return manager.writeState(ctx, secret, state)
}

func (manager *StaticRoleManager) VerifyRevokedStaticRoles(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) error {
	definitions, err := manager.retiredDefinitions(roles)
	if err != nil || len(definitions) == 0 {
		return err
	}
	state, found, _, err := manager.readState(ctx)
	if err != nil || !found {
		return errors.New("prototype retired static role state is absent")
	}
	if err := manager.validateState(state); err != nil {
		return err
	}
	for _, definition := range definitions {
		if state.Roles[definition.Role].Status != "RETIRED" {
			return errors.New("prototype retired static role remains active")
		}
		if definition.SecretName != "" {
			if _, secretFound, readErr := manager.api.readSecret(ctx, definition.SecretName); readErr != nil || secretFound {
				return errors.New("prototype retired credential Secret remains reachable")
			}
		}
	}
	return nil
}

func (manager *StaticRoleManager) ReadStaticCredentialDigests(
	ctx context.Context,
	roles []repository.VaultStaticRoleExpectation,
) (map[string]string, error) {
	definitions, err := manager.activeDefinitions(roles)
	if err != nil {
		return nil, err
	}
	state, found, _, err := manager.readState(ctx)
	if err != nil || !found {
		return nil, errors.New("prototype static role state is absent")
	}
	if err := manager.validateState(state); err != nil {
		return nil, err
	}
	digests, err := manager.readDigests(ctx, definitions)
	if err != nil {
		return nil, err
	}
	for _, definition := range definitions {
		if state.Roles[definition.Role].CredentialDigest != digests[definition.Role] {
			return nil, errors.New("prototype static credential monotonic readback mismatch")
		}
	}
	return digests, nil
}

func (manager *StaticRoleManager) activeDefinitions(
	roles []repository.VaultStaticRoleExpectation,
) ([]staticRoleDefinition, error) {
	if len(roles) == 0 || len(roles) > 8 {
		return nil, errors.New("prototype active static role set is invalid")
	}
	return manager.definitions(roles, false)
}

func (manager *StaticRoleManager) retiredDefinitions(
	roles []repository.VaultStaticRoleExpectation,
) ([]staticRoleDefinition, error) {
	if len(roles) == 0 {
		return nil, nil
	}
	if len(roles) > 4 {
		return nil, errors.New("prototype retired static role set is unbounded")
	}
	return manager.definitions(roles, true)
}

func (manager *StaticRoleManager) definitions(
	roles []repository.VaultStaticRoleExpectation,
	retired bool,
) ([]staticRoleDefinition, error) {
	result := make([]staticRoleDefinition, 0, len(roles))
	seen := make(map[string]struct{}, len(roles))
	for _, expectation := range roles {
		definition, ok := manager.registry[expectation.Role]
		if !ok || definition.Principal != expectation.Principal ||
			definition.DatabaseName != expectation.DatabaseName ||
			(definition.Status == "RETIRED") != retired {
			return nil, errors.New("prototype static role expectation is not registered")
		}
		if _, duplicate := seen[definition.Role]; duplicate {
			return nil, errors.New("duplicate prototype static role expectation")
		}
		seen[definition.Role] = struct{}{}
		result = append(result, definition)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Role < result[right].Role })
	return result, nil
}

func (manager *StaticRoleManager) readDigests(
	ctx context.Context,
	definitions []staticRoleDefinition,
) (map[string]string, error) {
	result := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		secret, found, err := manager.api.readSecret(ctx, definition.SecretName)
		if err != nil || !found || secret.Metadata.Name != definition.SecretName ||
			secret.Metadata.Namespace != Namespace || secret.Type != "Opaque" {
			return nil, errors.New("prototype static credential Secret readback rejected")
		}
		username := string(secret.Data["username"])
		password := string(secret.Data["password"])
		if username != definition.Principal || len(password) < 16 || len(password) > 4096 ||
			len(secret.Data) != 2 {
			return nil, errors.New("prototype static credential Secret binding is invalid")
		}
		digest, err := internalrpcauth.CanonicalJSONSHA256(struct {
			Role      string `json:"role"`
			Principal string `json:"principal"`
			Password  string `json:"password"`
		}{Role: definition.Role, Principal: username, Password: password})
		if err != nil {
			return nil, errors.New("digest prototype static credential")
		}
		result[definition.Role] = digest
	}
	return result, nil
}

func (manager *StaticRoleManager) readState(
	ctx context.Context,
) (staticRoleState, bool, secretDocument, error) {
	secret, found, err := manager.api.readSecret(ctx, StaticRoleState)
	if err != nil || !found {
		return staticRoleState{}, false, secret, errors.New("read prototype static role state Secret")
	}
	if secret.Metadata.Name != StaticRoleState || secret.Metadata.Namespace != Namespace || secret.Type != "Opaque" {
		return staticRoleState{}, false, secret, errors.New("prototype static role state Secret binding is invalid")
	}
	raw := secret.Data[staticRoleStateKey]
	if len(raw) == 0 {
		return staticRoleState{}, false, secret, nil
	}
	var state staticRoleState
	if err := decodeStrictJSON(raw, &state); err != nil {
		return staticRoleState{}, false, secret, errors.New("decode prototype static role state")
	}
	return state, true, secret, nil
}

func (manager *StaticRoleManager) writeState(
	ctx context.Context,
	secret secretDocument,
	state staticRoleState,
) error {
	if err := manager.validateState(state); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return errors.New("encode prototype static role state")
	}
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[staticRoleStateKey] = raw
	oldResourceVersion := secret.Metadata.ResourceVersion
	if err := manager.api.putSecret(ctx, secret); err != nil {
		return err
	}
	served, found, _, err := manager.readState(ctx)
	servedDigest, servedDigestErr := internalrpcauth.CanonicalJSONSHA256(served)
	expectedDigest, expectedDigestErr := internalrpcauth.CanonicalJSONSHA256(state)
	if err != nil || !found || servedDigestErr != nil || expectedDigestErr != nil ||
		served.Revision != state.Revision || servedDigest != expectedDigest {
		return errors.New("prototype static role state readback rejected")
	}
	readbackSecret, _, readbackErr := manager.api.readSecret(ctx, StaticRoleState)
	if readbackErr != nil || readbackSecret.Metadata.ResourceVersion == oldResourceVersion {
		return errors.New("prototype static role state resourceVersion did not advance")
	}
	return nil
}

func (manager *StaticRoleManager) initialState(digests map[string]string) staticRoleState {
	state := staticRoleState{
		Version: staticRoleStateVersion, Revision: 1,
		SourceRevision: manager.sourceRevision, SourceDigest: manager.sourceDigest,
		GenerationHighWater: map[string]uint64{"publisher": 5, "readback-attestor": 5},
		Roles:               make(map[string]staticRoleRecord, len(manager.registry)),
	}
	for _, definition := range manager.registry {
		record := staticRoleRecord{
			Principal: definition.Principal, DatabaseName: definition.DatabaseName,
			Capability: definition.Capability, Generation: definition.Generation,
			Status: definition.Status,
		}
		if definition.Status != "RETIRED" {
			record.CredentialDigest = digests[definition.Role]
			record.CredentialSecretName = definition.SecretName
		}
		state.Roles[definition.Role] = record
	}
	return state
}

func (manager *StaticRoleManager) validateState(state staticRoleState) error {
	if state.Version != staticRoleStateVersion || state.Revision == 0 ||
		state.SourceRevision != manager.sourceRevision || state.SourceDigest != manager.sourceDigest ||
		len(state.Roles) != len(manager.registry) || len(state.GenerationHighWater) != 2 ||
		state.GenerationHighWater["publisher"] != 5 ||
		state.GenerationHighWater["readback-attestor"] != 5 {
		return errors.New("prototype static role state high-watermark is invalid")
	}
	for role, definition := range manager.registry {
		record, ok := state.Roles[role]
		if !ok || record.Principal != definition.Principal ||
			record.DatabaseName != definition.DatabaseName ||
			record.Capability != definition.Capability ||
			record.Generation != definition.Generation ||
			record.Generation > state.GenerationHighWater[definition.Capability] ||
			record.Status != definition.Status ||
			(record.Status != "PREVIOUS" && record.Status != "CURRENT" &&
				record.Status != "NEXT" && record.Status != "RETIRED") {
			return errors.New("prototype static role state registry mismatch")
		}
		if record.Status == "RETIRED" {
			if record.CredentialDigest != "" || record.CredentialSecretName != "" {
				return errors.New("prototype retired static role retains credential binding")
			}
		} else if len(record.CredentialDigest) != 64 || record.CredentialSecretName != definition.SecretName {
			return errors.New("prototype active static role credential binding is invalid")
		}
	}
	return nil
}

func staticRoleRegistry() map[string]staticRoleDefinition {
	result := make(map[string]staticRoleDefinition, 10)
	add := func(capability, rolePrefix, principalPrefix, secretPrefix string, generation uint64, status string) {
		generationText := strconv.FormatUint(generation, 10)
		role := rolePrefix + "-g" + generationText
		principal := principalPrefix + "_g" + generationText
		secret := secretPrefix + "-g" + generationText
		result[role] = staticRoleDefinition{
			Role: role, Principal: principal, DatabaseName: "internal-rpc-authority",
			SecretName: secret, Capability: capability, Generation: generation, Status: status,
		}
	}
	for _, definition := range []struct {
		generation uint64
		status     string
	}{{1, "RETIRED"}, {2, "RETIRED"}, {3, "PREVIOUS"}, {4, "CURRENT"}, {5, "NEXT"}} {
		add("publisher", "internal-rpc-authority-publisher", "ira_publisher", "internal-rpc-authority-publisher-database", definition.generation, definition.status)
		add("readback-attestor", "internal-rpc-authority-readback-attestor", "ira_readback_attestor", "internal-rpc-authority-readback-database", definition.generation, definition.status)
	}
	return result
}

func (manager *StaticRoleManager) Close() {
	manager.api.Close()
}

var _ repository.VaultStaticRoleManager = (*StaticRoleManager)(nil)
