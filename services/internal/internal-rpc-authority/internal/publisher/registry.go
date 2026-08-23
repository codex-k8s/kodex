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
	registryPrincipalPattern = regexp.MustCompile(`^ira_[a-z0-9_]+_g[1-9][0-9]*$`)
)

type registryDocument struct {
	Version        int              `yaml:"version"`
	SourceRevision uint64           `yaml:"source_revision"`
	Targets        []registryTarget `yaml:"targets"`
}

type registryTarget struct {
	WorkloadID         string `yaml:"workload_id"`
	Role               string `yaml:"role"`
	SPIFFEID           string `yaml:"spiffe_id"`
	Namespace          string `yaml:"namespace"`
	ServiceAccount     string `yaml:"service_account"`
	WorkloadGeneration uint64 `yaml:"workload_generation"`
	AuthoritySnapshot  struct {
		SecretName string `yaml:"secret_name"`
		MountPath  string `yaml:"mount_path"`
	} `yaml:"authority_snapshot"`
	AuthPrivateKey struct {
		VaultPath  string `yaml:"vault_path"`
		SecretName string `yaml:"secret_name"`
		MountPath  string `yaml:"mount_path"`
	} `yaml:"auth_private_key"`
	ManifestTrust struct {
		VaultPath  string `yaml:"vault_path"`
		SecretName string `yaml:"secret_name"`
		MountPath  string `yaml:"mount_path"`
	} `yaml:"manifest_trust"`
	AuthorityProofTrust struct {
		VaultPath  string `yaml:"vault_path"`
		SecretName string `yaml:"secret_name"`
		MountPath  string `yaml:"mount_path"`
	} `yaml:"authority_proof_trust"`
	AuthorityProofPrivateKey struct {
		VaultPath  string `yaml:"vault_path"`
		SecretName string `yaml:"secret_name"`
		MountPath  string `yaml:"mount_path"`
	} `yaml:"authority_proof_private_key"`
	DatabaseIdentity struct {
		LoginPrincipal       string `yaml:"login_principal"`
		VaultDatabaseRole    string `yaml:"vault_database_role"`
		DSNMountPath         string `yaml:"dsn_mount_path"`
		CredentialGeneration uint64 `yaml:"credential_generation"`
	} `yaml:"database_identity"`
	RestoreCoordination struct {
		RoleCredentialID        string `yaml:"role_credential_id"`
		RoleCredentialVaultPath string `yaml:"role_credential_vault_path"`
		RoleCredentialMountPath string `yaml:"role_credential_mount_path"`
		ACKKeyID                string `yaml:"ack_key_id"`
		ACKKeyGeneration        uint64 `yaml:"ack_key_generation"`
		ACKKeyVaultPath         string `yaml:"ack_key_vault_path"`
		ACKKeyMountPath         string `yaml:"ack_key_mount_path"`
		ACKPublicJWKSource      string `yaml:"ack_public_jwk_source"`
		ControllerAddress       string `yaml:"controller_address"`
		ControllerTLSServerName string `yaml:"controller_tls_server_name"`
		ControllerTrustBundleID string `yaml:"controller_trust_bundle_id"`
		ControllerCAMountPath   string `yaml:"controller_ca_mount_path"`
		ControllerAudience      string `yaml:"controller_audience"`
		ControllerFullMethod    string `yaml:"controller_full_method"`
		NetworkPolicy           string `yaml:"network_policy"`
	} `yaml:"restore_coordination"`
	Readback struct {
		ReadbackID              string `yaml:"readback_id"`
		CredentialVaultPath     string `yaml:"credential_vault_path"`
		CredentialID            string `yaml:"credential_id"`
		CredentialMountPath     string `yaml:"credential_mount_path"`
		CredentialProtectedType string `yaml:"credential_protected_type"`
		CredentialSchema        string `yaml:"credential_schema"`
		PossessionKeyID         string `yaml:"possession_key_id"`
		PossessionKeyGeneration uint64 `yaml:"possession_key_generation"`
		PossessionKeyVaultPath  string `yaml:"possession_key_vault_path"`
		PossessionKeyMountPath  string `yaml:"possession_key_mount_path"`
		PossessionJWKSource     string `yaml:"possession_public_jwk_source"`
		AttestorAddress         string `yaml:"attestor_address"`
		AttestorTLSServerName   string `yaml:"attestor_tls_server_name"`
		AttestorTrustBundleID   string `yaml:"attestor_trust_bundle_id"`
		AttestorCAMountPath     string `yaml:"attestor_ca_mount_path"`
		AttestorAudience        string `yaml:"attestor_audience"`
		AttestorChallengeMethod string `yaml:"attestor_challenge_full_method"`
		AttestorFullMethod      string `yaml:"attestor_full_method"`
		ExpectedRole            string `yaml:"expected_role"`
		NetworkPolicy           string `yaml:"network_policy"`
		IntentRevision          uint64 `yaml:"intent_revision"`
		MaterialGeneration      uint64 `yaml:"material_generation"`
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
		document.SourceRevision == 0 ||
		document.SourceRevision > 9_007_199_254_740_991 ||
		len(document.Targets) == 0 ||
		len(document.Targets) > 384 {
		return model.DeliveryTargetRegistry{}, errors.New("publisher target registry is invalid")
	}
	digest := sha256.Sum256(raw)
	registry := model.DeliveryTargetRegistry{
		Version: model.ContractVersion, SourceRevision: document.SourceRevision,
		SourceDigest: hex.EncodeToString(digest[:]),
		Targets:      make(map[string]model.DeliveryTarget, len(document.Targets)),
	}
	seenTuple := make(map[string]struct{}, len(document.Targets))
	seenPath := make(map[string]struct{}, len(document.Targets)*2)
	for _, entry := range document.Targets {
		targetID := targetID(entry.WorkloadID, entry.Role)
		tuple := entry.WorkloadID + "\x00" + entry.Role
		expectedSPIFFE := "spiffe://mattercodex.local/ns/" +
			entry.Namespace + "/sa/" + entry.ServiceAccount
		if !registryWorkloadPattern.MatchString(entry.WorkloadID) ||
			entry.Namespace != "mattercodex-system" ||
			entry.ServiceAccount != entry.WorkloadID ||
			entry.SPIFFEID != expectedSPIFFE ||
			(entry.Role != "AUTHORIZATION_ISSUER" &&
				entry.Role != "AUTHORIZATION_VERIFIER" &&
				entry.Role != "AUTHORITY_PROOF_RESOLVER") ||
			entry.WorkloadGeneration == 0 ||
			entry.AuthoritySnapshot.SecretName != "internal-rpc-authority-snapshot" ||
			entry.AuthoritySnapshot.MountPath !=
				"/var/run/config/mattercodex/internal-rpc-authority/snapshot" ||
			!validOptionalAuthKey(entry) ||
			!registryVaultPathPattern.MatchString(entry.ManifestTrust.VaultPath) ||
			entry.ManifestTrust.SecretName != "internal-rpc-authority-manifest-trust" ||
			entry.ManifestTrust.MountPath !=
				"/var/run/config/mattercodex/internal-rpc-authority/manifest-trust" ||
			!validOptionalProofTrust(entry) ||
			!validOptionalProofPrivateKey(entry) ||
			!registryPrincipalPattern.MatchString(entry.DatabaseIdentity.LoginPrincipal) ||
			entry.DatabaseIdentity.VaultDatabaseRole == "" ||
			entry.DatabaseIdentity.DSNMountPath !=
				"/var/run/secrets/mattercodex/internal-rpc-authority/postgres/dsn" ||
			entry.DatabaseIdentity.CredentialGeneration == 0 ||
			entry.RestoreCoordination.ACKKeyGeneration == 0 ||
			!registryVaultPathPattern.MatchString(entry.RestoreCoordination.RoleCredentialVaultPath) ||
			!registryVaultPathPattern.MatchString(entry.RestoreCoordination.ACKKeyVaultPath) ||
			entry.RestoreCoordination.RoleCredentialVaultPath ==
				entry.RestoreCoordination.ACKKeyVaultPath ||
			entry.RestoreCoordination.RoleCredentialID == "" ||
			entry.RestoreCoordination.ACKKeyID == "" ||
			entry.RestoreCoordination.RoleCredentialMountPath !=
				"/var/run/secrets/mattercodex/internal-rpc-authority/restore/credential" ||
			entry.RestoreCoordination.ACKKeyMountPath !=
				"/var/run/secrets/mattercodex/internal-rpc-authority/restore/ack" ||
			entry.RestoreCoordination.ACKPublicJWKSource != "SIGNED_ROLE_CREDENTIAL" ||
			entry.RestoreCoordination.ControllerAddress !=
				"internal-rpc-authority-restore-controller.mattercodex-system.svc:8443" ||
			entry.RestoreCoordination.ControllerTLSServerName !=
				"internal-rpc-authority-restore-controller.mattercodex-system.svc" ||
			entry.RestoreCoordination.ControllerTrustBundleID !=
				"internal-rpc-authority-restore-controller-ca" ||
			entry.RestoreCoordination.ControllerCAMountPath !=
				"/var/run/config/mattercodex/internal-rpc-authority/restore/controller-ca.pem" ||
			entry.RestoreCoordination.ControllerAudience !=
				"urn:mattercodex:internal-rpc-authority-restore-controller" ||
			entry.RestoreCoordination.ControllerFullMethod !=
				"/internalrpcauthority.v1.RestoreControllerService/GetRestoreDirective" ||
			entry.RestoreCoordination.NetworkPolicy == "" ||
			entry.Readback.ReadbackID == "" ||
			entry.Readback.IntentRevision == 0 ||
			entry.Readback.MaterialGeneration == 0 ||
			entry.Readback.PossessionKeyGeneration != entry.Readback.MaterialGeneration ||
			entry.Readback.CredentialID == "" ||
			entry.Readback.PossessionKeyID == "" ||
			entry.Readback.CredentialMountPath !=
				"/var/run/secrets/mattercodex/internal-rpc-authority/readback/credential" ||
			entry.Readback.PossessionKeyMountPath !=
				"/var/run/secrets/mattercodex/internal-rpc-authority/readback/possession" ||
			entry.Readback.CredentialProtectedType !=
				"mattercodex-internal-rpc-readback-credential+jws" ||
			entry.Readback.CredentialSchema !=
				"contracts/authorization/v1/readback-credential.schema.json" ||
			entry.Readback.PossessionJWKSource !=
				"SIGNED_NORMAL_READBACK_CREDENTIAL" ||
			entry.Readback.AttestorAddress !=
				"internal-rpc-authority-readback-attestor.mattercodex-system.svc:8443" ||
			entry.Readback.AttestorTLSServerName !=
				"internal-rpc-authority-readback-attestor.mattercodex-system.svc" ||
			entry.Readback.AttestorTrustBundleID !=
				"internal-rpc-authority-readback-attestor-ca" ||
			entry.Readback.AttestorCAMountPath !=
				"/var/run/config/mattercodex/internal-rpc-authority/readback/attestor-ca.pem" ||
			entry.Readback.AttestorAudience !=
				"urn:mattercodex:internal-rpc-authority-readback-attestor" ||
			entry.Readback.AttestorChallengeMethod !=
				"/internalrpcauthority.v1.AuthorityReadbackAttestorService/IssueAttestationChallenge" ||
			entry.Readback.AttestorFullMethod !=
				"/internalrpcauthority.v1.AuthorityReadbackAttestorService/AttestServedState" ||
			entry.Readback.ExpectedRole != entry.Role ||
			entry.Readback.NetworkPolicy == "" {
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
			entry.ManifestTrust.VaultPath,
			entry.AuthPrivateKey.VaultPath,
			entry.AuthorityProofTrust.VaultPath,
			entry.AuthorityProofPrivateKey.VaultPath,
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
			Namespace:                  entry.Namespace,
			ServiceAccount:             entry.ServiceAccount,
			WorkloadGeneration:         entry.WorkloadGeneration,
			CredentialGeneration:       entry.DatabaseIdentity.CredentialGeneration,
			AuthoritySnapshotSecret:    entry.AuthoritySnapshot.SecretName,
			AuthoritySnapshotMountPath: entry.AuthoritySnapshot.MountPath,
			AuthPrivateKeyVaultPath:    entry.AuthPrivateKey.VaultPath,
			AuthPrivateKeySecret:       entry.AuthPrivateKey.SecretName,
			AuthPrivateKeyMountPath:    entry.AuthPrivateKey.MountPath,
			ManifestTrustVaultPath:     entry.ManifestTrust.VaultPath,
			ManifestTrustSecret:        entry.ManifestTrust.SecretName,
			ManifestTrustMountPath:     entry.ManifestTrust.MountPath,
			ProofTrustVaultPath:        entry.AuthorityProofTrust.VaultPath,
			ProofTrustSecret:           entry.AuthorityProofTrust.SecretName,
			ProofTrustMountPath:        entry.AuthorityProofTrust.MountPath,
			ProofPrivateKeyVaultPath:   entry.AuthorityProofPrivateKey.VaultPath,
			ProofPrivateKeySecret:      entry.AuthorityProofPrivateKey.SecretName,
			ProofPrivateKeyMountPath:   entry.AuthorityProofPrivateKey.MountPath,
			DatabaseLoginPrincipal:     entry.DatabaseIdentity.LoginPrincipal,
			DatabaseVaultRole:          entry.DatabaseIdentity.VaultDatabaseRole,
			DatabaseDSNMountPath:       entry.DatabaseIdentity.DSNMountPath,
			RestoreCredentialID:        entry.RestoreCoordination.RoleCredentialID,
			RestoreCredentialPath:      entry.RestoreCoordination.RoleCredentialVaultPath,
			RestoreCredentialMountPath: entry.RestoreCoordination.RoleCredentialMountPath,
			RestoreACKKeyID:            entry.RestoreCoordination.ACKKeyID,
			RestoreACKKeyGeneration:    entry.RestoreCoordination.ACKKeyGeneration,
			RestoreACKKeyPath:          entry.RestoreCoordination.ACKKeyVaultPath,
			RestoreACKKeyMountPath:     entry.RestoreCoordination.ACKKeyMountPath,
			RestoreControllerAddress:   entry.RestoreCoordination.ControllerAddress,
			RestoreControllerSNI:       entry.RestoreCoordination.ControllerTLSServerName,
			RestoreControllerCAPath:    entry.RestoreCoordination.ControllerCAMountPath,
			RestoreControllerAudience:  entry.RestoreCoordination.ControllerAudience,
			RestoreControllerMethod:    entry.RestoreCoordination.ControllerFullMethod,
			RestoreNetworkPolicy:       entry.RestoreCoordination.NetworkPolicy,
			ReadbackID:                 entry.Readback.ReadbackID,
			ReadbackCredentialID:       entry.Readback.CredentialID,
			ReadbackCredentialPath:     entry.Readback.CredentialVaultPath,
			ReadbackCredentialMount:    entry.Readback.CredentialMountPath,
			ReadbackPossessionKeyID:    entry.Readback.PossessionKeyID,
			ReadbackPossessionKeyPath:  entry.Readback.PossessionKeyVaultPath,
			ReadbackPossessionKeyMount: entry.Readback.PossessionKeyMountPath,
			ReadbackIntentRevision:     entry.Readback.IntentRevision,
			ReadbackMaterialGeneration: entry.Readback.MaterialGeneration,
			ReadbackAttestorAddress:    entry.Readback.AttestorAddress,
			ReadbackAttestorSNI:        entry.Readback.AttestorTLSServerName,
			ReadbackAttestorCAPath:     entry.Readback.AttestorCAMountPath,
			ReadbackAttestorAudience:   entry.Readback.AttestorAudience,
			ReadbackChallengeMethod:    entry.Readback.AttestorChallengeMethod,
			ReadbackAttestationMethod:  entry.Readback.AttestorFullMethod,
			ReadbackNetworkPolicy:      entry.Readback.NetworkPolicy,
		}
	}
	return registry, nil
}

func validOptionalAuthKey(entry registryTarget) bool {
	if entry.Role != "AUTHORIZATION_ISSUER" {
		return entry.AuthPrivateKey.VaultPath == "" &&
			entry.AuthPrivateKey.SecretName == "" &&
			entry.AuthPrivateKey.MountPath == ""
	}
	return registryVaultPathPattern.MatchString(entry.AuthPrivateKey.VaultPath) &&
		entry.AuthPrivateKey.SecretName == "internal-rpc-authority-issuer-key" &&
		entry.AuthPrivateKey.MountPath ==
			"/var/run/secrets/mattercodex/internal-rpc-authority/issuer"
}

func validOptionalProofTrust(entry registryTarget) bool {
	if entry.Role == "AUTHORIZATION_VERIFIER" {
		return entry.AuthorityProofTrust.VaultPath == "" &&
			entry.AuthorityProofTrust.SecretName == "" &&
			entry.AuthorityProofTrust.MountPath == ""
	}
	return registryVaultPathPattern.MatchString(entry.AuthorityProofTrust.VaultPath) &&
		entry.AuthorityProofTrust.SecretName == "internal-rpc-authority-proof-trust" &&
		entry.AuthorityProofTrust.MountPath ==
			"/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust"
}

func validOptionalProofPrivateKey(entry registryTarget) bool {
	if entry.Role != "AUTHORITY_PROOF_RESOLVER" {
		return entry.AuthorityProofPrivateKey.VaultPath == "" &&
			entry.AuthorityProofPrivateKey.SecretName == "" &&
			entry.AuthorityProofPrivateKey.MountPath == ""
	}
	return registryVaultPathPattern.MatchString(
		entry.AuthorityProofPrivateKey.VaultPath,
	) &&
		entry.AuthorityProofPrivateKey.SecretName ==
			"internal-rpc-authority-proof-signer-key" &&
		entry.AuthorityProofPrivateKey.MountPath ==
			"/var/run/secrets/mattercodex/internal-rpc-authority/proof-signer"
}

func targetID(workloadID, role string) string {
	roleValue := strings.ToLower(strings.ReplaceAll(role, "_", "-"))
	return workloadID + "." + roleValue
}
