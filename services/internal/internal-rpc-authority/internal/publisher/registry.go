package publisher

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	"go.yaml.in/yaml/v2"
)

var (
	registryWorkloadPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{1,94}[a-z0-9])$`)
	registryVaultPathPattern = regexp.MustCompile(`^kv/data/mattercodex/[a-z0-9][a-z0-9./_-]*[a-z0-9]$`)
	registryDigestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type registryDocument struct {
	Version int              `yaml:"version"`
	Targets []registryTarget `yaml:"targets"`
}

type registryTarget struct {
	WorkloadID         string `yaml:"workload_id"`
	Role               string `yaml:"role"`
	SPIFFEID           string `yaml:"spiffe_id"`
	WorkloadGeneration uint64 `yaml:"workload_generation"`
	DatabaseIdentity   struct {
		CredentialGeneration uint64 `yaml:"credential_generation"`
	} `yaml:"database_identity"`
	RestoreCoordination struct {
		RoleCredentialID        string `yaml:"role_credential_id"`
		RoleCredentialVaultPath string `yaml:"role_credential_vault_path"`
		ACKKeyID                string `yaml:"ack_key_id"`
		ACKKeyGeneration        uint64 `yaml:"ack_key_generation"`
		ACKKeyVaultPath         string `yaml:"ack_key_vault_path"`
	} `yaml:"restore_coordination"`
	Readback struct {
		CredentialVaultPath     string `yaml:"credential_vault_path"`
		PossessionKeyVaultPath  string `yaml:"possession_key_vault_path"`
		IntentRevision          uint64 `yaml:"intent_revision"`
		MaterialGeneration      uint64 `yaml:"material_generation"`
		SourceRevision          uint64 `yaml:"source_revision"`
		ServedStateDigestSHA256 string `yaml:"served_state_digest_sha256"`
	} `yaml:"readback"`
}

// LoadRegistry читает и строго проверяет реестр целей доставки.
func LoadRegistry(path string) (model.DeliveryTargetRegistry, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return model.DeliveryTargetRegistry{}, errors.New("publisher target registry file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return model.DeliveryTargetRegistry{}, errors.New("read publisher target registry")
	}
	var document registryDocument
	if err := yaml.UnmarshalStrict(raw, &document); err != nil ||
		document.Version != model.ContractVersion ||
		len(document.Targets) == 0 ||
		len(document.Targets) > 384 {
		return model.DeliveryTargetRegistry{}, errors.New("publisher target registry is invalid")
	}
	digest := sha256.Sum256(raw)
	registry := model.DeliveryTargetRegistry{
		Version: model.ContractVersion, SourceRevision: uint64(document.Version),
		SourceDigest: hex.EncodeToString(digest[:]),
		Targets:      make(map[string]model.DeliveryTarget, len(document.Targets)),
	}
	seenTuple := make(map[string]struct{}, len(document.Targets))
	seenPath := make(map[string]struct{}, len(document.Targets)*2)
	for _, entry := range document.Targets {
		targetID := targetID(entry.WorkloadID, entry.Role)
		tuple := entry.WorkloadID + "\x00" + entry.Role
		if !registryWorkloadPattern.MatchString(entry.WorkloadID) ||
			!strings.HasPrefix(entry.SPIFFEID, "spiffe://mattercodex.local/") ||
			(entry.Role != "AUTHORIZATION_ISSUER" &&
				entry.Role != "AUTHORIZATION_VERIFIER" &&
				entry.Role != "AUTHORITY_PROOF_RESOLVER") ||
			entry.WorkloadGeneration == 0 ||
			entry.DatabaseIdentity.CredentialGeneration == 0 ||
			entry.RestoreCoordination.ACKKeyGeneration == 0 ||
			!registryVaultPathPattern.MatchString(entry.RestoreCoordination.RoleCredentialVaultPath) ||
			!registryVaultPathPattern.MatchString(entry.RestoreCoordination.ACKKeyVaultPath) ||
			entry.RestoreCoordination.RoleCredentialVaultPath ==
				entry.RestoreCoordination.ACKKeyVaultPath ||
			entry.RestoreCoordination.RoleCredentialID == "" ||
			entry.RestoreCoordination.ACKKeyID == "" ||
			entry.Readback.IntentRevision == 0 ||
			entry.Readback.MaterialGeneration == 0 ||
			entry.Readback.SourceRevision == 0 ||
			!registryDigestPattern.MatchString(
				entry.Readback.ServedStateDigestSHA256,
			) {
			return model.DeliveryTargetRegistry{}, fmt.Errorf(
				"publisher target %q is outside the registry boundary",
				targetID,
			)
		}
		if _, duplicate := seenTuple[tuple]; duplicate {
			return model.DeliveryTargetRegistry{}, errors.New("duplicate publisher target tuple")
		}
		seenTuple[tuple] = struct{}{}
		for _, pathValue := range []string{
			entry.RestoreCoordination.RoleCredentialVaultPath,
			entry.RestoreCoordination.ACKKeyVaultPath,
			entry.Readback.CredentialVaultPath,
			entry.Readback.PossessionKeyVaultPath,
		} {
			if pathValue == "" {
				continue
			}
			if !registryVaultPathPattern.MatchString(pathValue) {
				return model.DeliveryTargetRegistry{}, errors.New("publisher target Vault path is invalid")
			}
			if _, duplicate := seenPath[pathValue]; duplicate {
				return model.DeliveryTargetRegistry{}, errors.New("publisher target Vault path is reused")
			}
			seenPath[pathValue] = struct{}{}
		}
		registry.Targets[targetID] = model.DeliveryTarget{
			TargetID:                   targetID,
			WorkloadID:                 entry.WorkloadID,
			WorkloadSPIFFEID:           entry.SPIFFEID,
			Role:                       entry.Role,
			WorkloadGeneration:         entry.WorkloadGeneration,
			CredentialGeneration:       entry.DatabaseIdentity.CredentialGeneration,
			RestoreCredentialPath:      entry.RestoreCoordination.RoleCredentialVaultPath,
			RestoreACKKeyPath:          entry.RestoreCoordination.ACKKeyVaultPath,
			ReadbackCredentialPath:     entry.Readback.CredentialVaultPath,
			ReadbackPossessionKeyPath:  entry.Readback.PossessionKeyVaultPath,
			ReadbackIntentRevision:     entry.Readback.IntentRevision,
			ReadbackMaterialGeneration: entry.Readback.MaterialGeneration,
			ReadbackSourceRevision:     entry.Readback.SourceRevision,
			ReadbackServedStateDigest:  entry.Readback.ServedStateDigestSHA256,
		}
	}
	return registry, nil
}

func targetID(workloadID, role string) string {
	roleValue := strings.ToLower(strings.ReplaceAll(role, "_", "-"))
	return workloadID + "." + roleValue
}
