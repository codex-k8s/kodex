package prototypematerial

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	Namespace           = "mattercodex-system"
	StaticRoleState     = "internal-rpc-authority-prototype-static-role-state"
	DeliveryMountRoot   = "/var/run/secrets/mattercodex/internal-rpc-authority/prototype-delivery"
	KubernetesCAFile    = "/var/run/config/kubernetes.io/serviceaccount/ca.crt"
	KubernetesTokenFile = "/var/run/secrets/tokens/kubernetes-api/token"
	staticRoleStateKey  = "state.json"
)

type storageMode uint8

const (
	storageModeDirect storageMode = iota + 1
	storageModeDocument
)

type fieldRule struct {
	physical string
	required bool
}

type deliveryTarget struct {
	path          string
	resourceName  string
	storageKey    string
	filePath      string
	mode          storageMode
	fields        map[string]fieldRule
	allowedKeys   map[string]struct{}
	directAliases map[string]struct{}
}

// DeliveryRegistry закрывает логические Vault paths до точных Secret и key.
// Реестр строится только из уже строго проверенного publisher registry.
type DeliveryRegistry struct {
	targets map[string]deliveryTarget
}

func NewPublisherRegistry(registry model.DeliveryTargetRegistry) (DeliveryRegistry, error) {
	if registry.Version != model.ContractVersion || registry.SourceRevision == 0 ||
		!validSHA256(registry.SourceDigest) ||
		len(registry.Targets) == 0 || len(registry.Targets) > 384 {
		return DeliveryRegistry{}, errors.New("prototype delivery registry is invalid")
	}
	result := DeliveryRegistry{targets: make(map[string]deliveryTarget, len(registry.Targets)*8)}
	for _, target := range registry.Targets {
		roleSlug, err := targetRoleSlug(target.Role)
		if err != nil {
			return DeliveryRegistry{}, err
		}
		resourcePrefix := "internal-rpc-authority-" + target.WorkloadID
		if target.AuthPrivateKeyVaultPath != "" {
			if err := result.addDirect(
				target.AuthPrivateKeyVaultPath,
				resourcePrefix+"-"+roleSlug+"-key",
				rotatingKeyFields(),
			); err != nil {
				return DeliveryRegistry{}, err
			}
		}
		if target.ManifestTrustVaultPath != "" {
			if err := result.addDirect(
				target.ManifestTrustVaultPath,
				resourcePrefix+"-manifest-trust",
				map[string]fieldRule{
					"manifest-trust.jws":   {physical: "bundle.jws", required: true},
					"source_revision":      {physical: "source_revision", required: true},
					"source_digest_sha256": {physical: "source_digest_sha256", required: true},
				},
			); err != nil {
				return DeliveryRegistry{}, err
			}
		}
		if target.ProofTrustVaultPath != "" {
			resourceName := resourcePrefix + "-proof-trust"
			if roleSlug == "resolver" {
				resourceName = resourcePrefix + "-resolver-trust"
			}
			if err := result.addDirect(
				target.ProofTrustVaultPath,
				resourceName,
				map[string]fieldRule{
					"proof-trust.jwk":      {physical: "jwks.json", required: true},
					"source_revision":      {physical: "source_revision", required: true},
					"source_digest_sha256": {physical: "source_digest_sha256", required: true},
				},
			); err != nil {
				return DeliveryRegistry{}, err
			}
		}
		if target.ProofPrivateKeyVaultPath != "" {
			if err := result.addDirect(
				target.ProofPrivateKeyVaultPath,
				resourcePrefix+"-resolver-key",
				rotatingKeyFields(),
			); err != nil {
				return DeliveryRegistry{}, err
			}
		}
		deliverySecret := resourcePrefix + "-" + roleSlug + "-delivery"
		for _, entry := range []struct {
			path string
			key  string
			kind materialKind
		}{
			{target.RestoreCredentialPath, "restore-credential.json", materialRestoreCredential},
			{target.RestoreACKKeyPath, "restore-ack.json", materialRestoreACK},
			{target.ReadbackCredentialPath, "readback-credential.json", materialReadbackCredential},
			{target.ReadbackPossessionKeyPath, "readback-possession.json", materialReadbackPossession},
		} {
			if err := result.addDocument(entry.path, deliverySecret, entry.key, "", materialFields(entry.kind)); err != nil {
				return DeliveryRegistry{}, err
			}
		}
	}
	return result, nil
}

// NewWorkloadFileRegistry создаёт read-only реестр только для одного sidecar.
func NewWorkloadFileRegistry(
	primary map[string]string,
	resolver map[string]string,
) (DeliveryRegistry, error) {
	result := DeliveryRegistry{targets: make(map[string]deliveryTarget, len(primary)+len(resolver))}
	for path, key := range primary {
		if err := result.addConsumerDocument(path, "primary", key); err != nil {
			return DeliveryRegistry{}, err
		}
	}
	for path, key := range resolver {
		if err := result.addConsumerDocument(path, "resolver", key); err != nil {
			return DeliveryRegistry{}, err
		}
	}
	if len(result.targets) != 4 && len(result.targets) != 6 {
		return DeliveryRegistry{}, errors.New("prototype workload delivery registry is incomplete")
	}
	return result, nil
}

func (registry *DeliveryRegistry) addConsumerDocument(path, directory, key string) error {
	kind, ok := materialKindForStorageKey(key)
	if !ok || (directory != "primary" && directory != "resolver") {
		return errors.New("prototype workload delivery resource is unknown")
	}
	filePath := filepath.Join(DeliveryMountRoot, directory, key)
	return registry.addDocument(path, "", key, filePath, materialFields(kind))
}

func (registry *DeliveryRegistry) addDirect(path, resource string, fields map[string]fieldRule) error {
	return registry.add(deliveryTarget{
		path: path, resourceName: resource, mode: storageModeDirect, fields: fields,
	})
}

func (registry *DeliveryRegistry) addDocument(
	path, resource, key, filePath string,
	fields map[string]fieldRule,
) error {
	return registry.add(deliveryTarget{
		path: path, resourceName: resource, storageKey: key, filePath: filePath,
		mode: storageModeDocument, fields: fields,
	})
}

func (registry *DeliveryRegistry) add(target deliveryTarget) error {
	if registry == nil || target.path == "" || len(target.path) > 512 ||
		!strings.HasPrefix(target.path, "kv/data/mattercodex/internal-rpc-authority/") ||
		len(target.fields) == 0 || len(target.fields) > 16 {
		return errors.New("prototype delivery target is outside the registry boundary")
	}
	if target.mode == storageModeDirect {
		if !dnsLabel(target.resourceName) || target.storageKey != "" || target.filePath != "" {
			return errors.New("prototype direct delivery target is invalid")
		}
	} else if target.mode == storageModeDocument {
		if target.storageKey == "" || (target.resourceName == "" && !filepath.IsAbs(target.filePath)) {
			return errors.New("prototype document delivery target is invalid")
		}
	} else {
		return errors.New("prototype delivery storage mode is invalid")
	}
	if _, duplicate := registry.targets[target.path]; duplicate {
		return errors.New("duplicate prototype delivery path")
	}
	if target.resourceName != "" {
		for _, registered := range registry.targets {
			if registered.resourceName == target.resourceName {
				if target.mode == storageModeDirect &&
					(registered.mode != storageModeDirect ||
						!equalFieldRules(target.fields, registered.fields)) {
					return errors.New("prototype direct delivery aliases have different schemas")
				}
				target.allowedKeys = registered.allowedKeys
				target.directAliases = registered.directAliases
				break
			}
		}
		if target.allowedKeys == nil {
			target.allowedKeys = make(map[string]struct{})
		}
		if target.mode == storageModeDocument {
			target.allowedKeys[target.storageKey] = struct{}{}
		} else {
			if target.directAliases == nil {
				target.directAliases = make(map[string]struct{})
			}
			target.directAliases[target.path] = struct{}{}
			for _, rule := range target.fields {
				target.allowedKeys[rule.physical] = struct{}{}
			}
		}
	}
	registry.targets[target.path] = target
	return nil
}

func equalFieldRules(left, right map[string]fieldRule) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftRule := range left {
		if right[key] != leftRule {
			return false
		}
	}
	return true
}

func (registry DeliveryRegistry) target(path string) (deliveryTarget, error) {
	target, ok := registry.targets[path]
	if !ok {
		return deliveryTarget{}, errors.New("prototype delivery path is not registered")
	}
	return target, nil
}

func (target deliveryTarget) validateData(data map[string]string) error {
	if len(data) == 0 || len(data) > len(target.fields) {
		return errors.New("prototype delivery material is outside the registered schema")
	}
	for key, value := range data {
		rule, ok := target.fields[key]
		if !ok || rule.physical == "" || value == "" || len(value) > 1<<20 {
			return errors.New("prototype delivery field is invalid")
		}
	}
	for key, rule := range target.fields {
		if rule.required && data[key] == "" {
			return errors.New("prototype delivery required field is absent")
		}
	}
	return nil
}

func targetRoleSlug(role string) (string, error) {
	switch role {
	case "AUTHORIZATION_ISSUER":
		return "issuer", nil
	case "AUTHORIZATION_VERIFIER":
		return "verifier", nil
	case "AUTHORITY_PROOF_RESOLVER":
		return "resolver", nil
	default:
		return "", errors.New("prototype delivery role is not registered")
	}
}

func rotatingKeyFields() map[string]fieldRule {
	return map[string]fieldRule{
		"private.jwk":          {physical: "private.jwk", required: true},
		"current_private_jwk":  {physical: "current_private_jwk", required: true},
		"next_private_jwk":     {physical: "next_private_jwk", required: true},
		"current_generation":   {physical: "current_generation", required: true},
		"next_generation":      {physical: "next_generation", required: true},
		"previous_public_jwk":  {physical: "previous_public_jwk"},
		"previous_generation":  {physical: "previous_generation"},
		"source_revision":      {physical: "source_revision", required: true},
		"source_digest_sha256": {physical: "source_digest_sha256", required: true},
	}
}

type materialKind uint8

const (
	materialRestoreCredential materialKind = iota + 1
	materialRestoreACK
	materialReadbackCredential
	materialReadbackPossession
)

func materialKindForStorageKey(key string) (materialKind, bool) {
	switch key {
	case "restore-credential.json":
		return materialRestoreCredential, true
	case "restore-ack.json":
		return materialRestoreACK, true
	case "readback-credential.json":
		return materialReadbackCredential, true
	case "readback-possession.json":
		return materialReadbackPossession, true
	default:
		return 0, false
	}
}

func materialFields(kind materialKind) map[string]fieldRule {
	keys := []string{}
	switch kind {
	case materialRestoreCredential:
		keys = []string{"semantic_digest_sha256", "issuance_directive_jti", "role_credential_compact_jws", "role_credential_digest_sha256", "delivery_receipt_jti", "issued_at"}
	case materialRestoreACK:
		keys = []string{"semantic_digest_sha256", "issuance_directive_jti", "ack_private_jwk", "ack_key_kid", "ack_key_thumbprint_sha256", "role_credential_jti", "delivery_receipt_jti", "issued_at"}
	case materialReadbackCredential:
		keys = []string{"pinned_intent_id", "readback_credential_compact_jws", "readback_credential_jti", "readback_credential_digest_sha256", "intent_digest_sha256", "expires_at"}
	case materialReadbackPossession:
		keys = []string{"possession_private_jwk", "possession_key_kid", "possession_key_generation", "possession_key_thumbprint_sha256"}
	}
	result := make(map[string]fieldRule, len(keys))
	for _, key := range keys {
		result[key] = fieldRule{physical: key, required: true}
	}
	return result
}

func metadataKeys(path string) (string, string) {
	digest := sha256.Sum256([]byte(path))
	id := hex.EncodeToString(digest[:16])
	return "mattercodex.dev/delivery-" + id + "-version", "mattercodex.dev/delivery-" + id + "-digest"
}

func dnsLabel(value string) bool {
	if value == "" || len(value) > 253 || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
