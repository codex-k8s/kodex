package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/planner"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
)

type Config struct {
	Mode                        string           `env:"LEGACY_DATA_MIGRATION_MODE,required"`
	PlanID                      string           `env:"LEGACY_DATA_MIGRATION_PLAN_ID,required"`
	SourceRootReference         string           `env:"LEGACY_DATA_MIGRATION_SOURCE_ROOT_REFERENCE,required"`
	SourceRootSHA256            string           `env:"LEGACY_DATA_MIGRATION_SOURCE_ROOT_SHA256,required"`
	SourceDSNFile               string           `env:"LEGACY_DATA_MIGRATION_SOURCE_DSN_FILE,required"`
	SourceTLSServerName         string           `env:"LEGACY_DATA_MIGRATION_SOURCE_TLS_SERVER_NAME,required"`
	SourceCAFile                string           `env:"LEGACY_DATA_MIGRATION_SOURCE_CA_FILE,required"`
	ControlPlaneTarget          string           `env:"LEGACY_DATA_MIGRATION_CONTROL_PLANE_TARGET,required"`
	ControlPlaneTLSServerName   string           `env:"LEGACY_DATA_MIGRATION_CONTROL_PLANE_TLS_SERVER_NAME,required"`
	ControlPlaneCAFile          string           `env:"LEGACY_DATA_MIGRATION_CONTROL_PLANE_CA_FILE,required"`
	ControlPlaneCertificateFile string           `env:"LEGACY_DATA_MIGRATION_CONTROL_PLANE_CERTIFICATE_FILE,required"`
	ControlPlanePrivateKeyFile  string           `env:"LEGACY_DATA_MIGRATION_CONTROL_PLANE_PRIVATE_KEY_FILE,required"`
	ApplicationGrantFile        string           `env:"LEGACY_DATA_MIGRATION_APPLICATION_GRANT_FILE,required"`
	OwnerEvidenceFile           string           `env:"LEGACY_DATA_MIGRATION_OWNER_EVIDENCE_FILE,required"`
	OwnerEvidence               planner.Evidence `env:"-"`
	AuthorityPolicyFile         string           `env:"LEGACY_DATA_MIGRATION_AUTHORITY_POLICY_FILE,required"`
	AuthorityPolicyRevision     uint64           `env:"-"`
	AuthorityPolicySHA256       string           `env:"-"`
	ImagePolicyRevision         uint64           `env:"LEGACY_DATA_MIGRATION_IMAGE_POLICY_REVISION,required"`
	ImagePolicySHA256           string           `env:"LEGACY_DATA_MIGRATION_IMAGE_POLICY_SHA256,required"`
	RoleRuntimeContractRevision uint64           `env:"LEGACY_DATA_MIGRATION_ROLE_RUNTIME_CONTRACT_REVISION,required"`
	RoleRuntimeContractSHA256   string           `env:"LEGACY_DATA_MIGRATION_ROLE_RUNTIME_CONTRACT_SHA256,required"`
	RoleImageInputRepository    string           `env:"LEGACY_DATA_MIGRATION_ROLE_IMAGE_INPUT_REPOSITORY,required"`
	TrustedRoleBaseRepository   string           `env:"LEGACY_DATA_MIGRATION_TRUSTED_ROLE_BASE_REPOSITORY,required"`
	TrustedRoleBaseDigest       string           `env:"LEGACY_DATA_MIGRATION_TRUSTED_ROLE_BASE_DIGEST,required"`
	ControlPlaneRPCDeadline     time.Duration    `env:"LEGACY_DATA_MIGRATION_CONTROL_PLANE_RPC_DEADLINE"`
	RestoreDSNFile              string           `env:"LEGACY_DATA_MIGRATION_RESTORE_DSN_FILE"`
	RestoreTLSServerName        string           `env:"LEGACY_DATA_MIGRATION_RESTORE_TLS_SERVER_NAME"`
	RestoreCAFile               string           `env:"LEGACY_DATA_MIGRATION_RESTORE_CA_FILE"`
	BackupDirectory             string           `env:"LEGACY_DATA_MIGRATION_BACKUP_DIRECTORY,required"`
	BackupKeyFile               string           `env:"LEGACY_DATA_MIGRATION_BACKUP_KEY_FILE,required"`
	ReportPath                  string           `env:"LEGACY_DATA_MIGRATION_REPORT_PATH,required"`
	TechnicalListen             string           `env:"LEGACY_DATA_MIGRATION_TECHNICAL_LISTEN"`
	StartupTimeout              time.Duration    `env:"LEGACY_DATA_MIGRATION_STARTUP_TIMEOUT"`
	OperationTimeout            time.Duration    `env:"LEGACY_DATA_MIGRATION_OPERATION_TIMEOUT"`
	ShutdownTimeout             time.Duration    `env:"LEGACY_DATA_MIGRATION_SHUTDOWN_TIMEOUT"`
	TerminalScrapeHold          time.Duration    `env:"LEGACY_DATA_MIGRATION_TERMINAL_SCRAPE_HOLD"`
	MaximumStagingBytes         int64            `env:"LEGACY_DATA_MIGRATION_MAXIMUM_STAGING_BYTES"`
}

func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen: ":9090", StartupTimeout: 30 * time.Second,
		OperationTimeout: 30 * time.Minute, ShutdownTimeout: 10 * time.Second,
		TerminalScrapeHold: 20 * time.Second, MaximumStagingBytes: 1920 << 20,
		ControlPlaneRPCDeadline: 10 * time.Second,
	}
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, err
	}
	policyRevision, policySHA256, err := loadAuthorityPolicyEvidence(config.AuthorityPolicyFile)
	if err != nil {
		return Config{}, err
	}
	config.AuthorityPolicyRevision, config.AuthorityPolicySHA256 = policyRevision, policySHA256
	config.OwnerEvidence, err = loadOwnerEvidence(config.OwnerEvidenceFile)
	if err != nil {
		return Config{}, err
	}
	config.OwnerEvidence.AuthorityPolicyRevision = policyRevision
	config.OwnerEvidence.AuthorityPolicySHA256 = policySHA256
	config.OwnerEvidence.ImagePolicyRevision = config.ImagePolicyRevision
	config.OwnerEvidence.ImagePolicySHA256 = config.ImagePolicySHA256
	config.OwnerEvidence.RuntimeContractRevision = config.RoleRuntimeContractRevision
	config.OwnerEvidence.RuntimeContractSHA256 = config.RoleRuntimeContractSHA256
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	switch config.Mode {
	case "dry-run", "pre-commit", "commit", "rollback", "restore-verify":
	default:
		return errors.New("legacy migration mode is invalid")
	}
	if _, err := uuid.Parse(config.PlanID); err != nil {
		return errors.New("legacy migration plan identifier is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("legacy migration technical endpoint is invalid")
	}
	if _, _, err := net.SplitHostPort(config.ControlPlaneTarget); err != nil {
		return errors.New("legacy migration control-plane endpoint is invalid")
	}
	if strings.TrimSpace(config.SourceRootReference) == "" || len(config.SourceRootReference) > 512 ||
		!validSHA256(config.SourceRootSHA256) {
		return errors.New("legacy migration source root evidence is invalid")
	}
	for _, path := range []string{config.SourceDSNFile, config.SourceCAFile,
		config.ControlPlaneCAFile, config.ControlPlaneCertificateFile,
		config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile, config.AuthorityPolicyFile,
		config.OwnerEvidenceFile,
		config.BackupDirectory, config.BackupKeyFile, config.ReportPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("legacy migration path is invalid")
		}
	}
	if filepath.Base(config.BackupDirectory) != "backups" || filepath.Base(filepath.Dir(config.ReportPath)) != "reports" ||
		filepath.Dir(config.BackupDirectory) != filepath.Dir(filepath.Dir(config.ReportPath)) ||
		filepath.Ext(config.ReportPath) != ".json" {
		return errors.New("legacy migration storage boundary is invalid")
	}
	if config.Mode == "restore-verify" {
		for _, path := range []string{config.RestoreDSNFile, config.RestoreCAFile} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return errors.New("legacy migration restore path is invalid")
			}
		}
		if strings.TrimSpace(config.RestoreTLSServerName) == "" {
			return errors.New("legacy migration restore TLS server name is invalid")
		}
	} else if config.RestoreDSNFile != "" || config.RestoreTLSServerName != "" || config.RestoreCAFile != "" {
		return errors.New("legacy migration restore configuration is unexpected")
	}
	for _, serverName := range []string{config.SourceTLSServerName, config.ControlPlaneTLSServerName} {
		if strings.TrimSpace(serverName) == "" || strings.ContainsAny(serverName, "*/") {
			return errors.New("legacy migration TLS server name is invalid")
		}
	}
	if config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute ||
		config.OperationTimeout < time.Minute || config.OperationTimeout > 2*time.Hour ||
		config.ShutdownTimeout < time.Second || config.ShutdownTimeout > time.Minute ||
		config.TerminalScrapeHold < 16*time.Second || config.TerminalScrapeHold > time.Minute ||
		config.MaximumStagingBytes < 1<<20 || config.MaximumStagingBytes > 1920<<20 ||
		config.ControlPlaneRPCDeadline < 500*time.Millisecond || config.ControlPlaneRPCDeadline > 30*time.Second {
		return errors.New("legacy migration lifecycle timeout is invalid")
	}
	if config.AuthorityPolicyRevision == 0 || !validSHA256(config.AuthorityPolicySHA256) ||
		config.ImagePolicyRevision == 0 || !validSHA256(config.ImagePolicySHA256) ||
		config.RoleRuntimeContractRevision == 0 || !validSHA256(config.RoleRuntimeContractSHA256) ||
		strings.TrimSpace(config.RoleImageInputRepository) == "" ||
		strings.TrimSpace(config.TrustedRoleBaseRepository) == "" ||
		!strings.HasPrefix(config.TrustedRoleBaseDigest, "sha256:") ||
		!validSHA256(strings.TrimPrefix(config.TrustedRoleBaseDigest, "sha256:")) {
		return errors.New("legacy migration served policy evidence is invalid")
	}
	if config.OwnerEvidence.RoleImage == nil ||
		config.OwnerEvidence.RoleImage.BaseImageReference != config.TrustedRoleBaseRepository ||
		config.OwnerEvidence.RoleImage.BaseImageDigest != config.TrustedRoleBaseDigest ||
		!strings.HasPrefix(config.OwnerEvidence.RoleImage.ContextRef,
			"oci://"+config.RoleImageInputRepository+"@sha256:") {
		return errors.New("legacy migration role image evidence does not match served owner configuration")
	}
	return nil
}

func loadOwnerEvidence(path string) (planner.Evidence, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return planner.Evidence{}, errors.New("read legacy migration owner evidence")
	}
	var document struct {
		SchemaVersion string `json:"schemaVersion"`
		Archive       struct {
			StoragePrefix      string    `json:"storagePrefix"`
			StorageVersion     string    `json:"storageVersion"`
			RetentionRef       string    `json:"retentionRef"`
			ScanPolicyRevision uint64    `json:"scanPolicyRevision"`
			ScanEvidenceSHA256 string    `json:"scanEvidenceSha256"`
			ScannerWorkloadID  string    `json:"scannerWorkloadId"`
			ScannedAt          time.Time `json:"scannedAt"`
		} `json:"archive"`
		Provider struct {
			ObservedAt          time.Time `json:"observedAt"`
			ObservationRevision uint64    `json:"observationRevision"`
			ObservedLimit       uint64    `json:"observedLimit"`
		} `json:"provider"`
		RoleImage struct {
			Input                          json.RawMessage `json:"input"`
			Generation                     uint64          `json:"generation"`
			SpecSHA256                     string          `json:"specSha256"`
			BuildStagingReference          string          `json:"buildStagingReference"`
			BuildManifestDigest            string          `json:"buildManifestDigest"`
			BuildProvenanceSHA256          string          `json:"buildProvenanceSha256"`
			PromotedReference              string          `json:"promotedReference"`
			AdmissionRevision              uint64          `json:"admissionRevision"`
			AdmissionReceiptSHA256         string          `json:"admissionReceiptSha256"`
			AdmissionReceiptManifestDigest string          `json:"admissionReceiptManifestDigest"`
			SignatureSHA256                string          `json:"signatureSha256"`
			PromotionReadbackSHA256        string          `json:"promotionReadbackSha256"`
			SBOMSHA256                     string          `json:"sbomSha256"`
			VulnerabilityEvidenceSHA256    string          `json:"vulnerabilityEvidenceSha256"`
			SignatureIdentity              string          `json:"signatureIdentity"`
			PromotedAt                     time.Time       `json:"promotedAt"`
		} `json:"roleImage"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || document.SchemaVersion != "mattercodex.legacy-data-owner-evidence.v1" {
		return planner.Evidence{}, errors.New("decode legacy migration owner evidence")
	}
	input := new(controlplanev1.RoleImageRecipeInput)
	if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(document.RoleImage.Input, input); err != nil {
		return planner.Evidence{}, errors.New("decode legacy role image evidence")
	}
	return planner.Evidence{
		ArchiveStoragePrefix: document.Archive.StoragePrefix, ArchiveStorageVersion: document.Archive.StorageVersion,
		ArchiveRetentionRef: document.Archive.RetentionRef, ArchiveScanPolicyRevision: document.Archive.ScanPolicyRevision,
		ArchiveScanEvidenceSHA256: document.Archive.ScanEvidenceSHA256, ArchiveScannerWorkloadID: document.Archive.ScannerWorkloadID,
		ArchiveScannedAt: document.Archive.ScannedAt, ProviderObservedAt: document.Provider.ObservedAt,
		ProviderObservationRevision: document.Provider.ObservationRevision, ProviderObservedLimit: document.Provider.ObservedLimit,
		RoleImage: input, RoleImageGeneration: document.RoleImage.Generation, RoleImageSpecSHA256: document.RoleImage.SpecSHA256,
		ImageBuildStagingReference: document.RoleImage.BuildStagingReference, ImageBuildManifestDigest: document.RoleImage.BuildManifestDigest,
		ImageBuildProvenanceSHA256:     document.RoleImage.BuildProvenanceSHA256,
		ImageArtifactPromotedReference: document.RoleImage.PromotedReference, ImageAdmissionRevision: document.RoleImage.AdmissionRevision,
		ImageAdmissionReceiptSHA256:         document.RoleImage.AdmissionReceiptSHA256,
		ImageAdmissionReceiptManifestDigest: document.RoleImage.AdmissionReceiptManifestDigest,
		ImageSignatureSHA256:                document.RoleImage.SignatureSHA256, ImagePromotionReadbackSHA256: document.RoleImage.PromotionReadbackSHA256,
		ImageSBOMSHA256: document.RoleImage.SBOMSHA256, ImageVulnerabilityEvidenceSHA256: document.RoleImage.VulnerabilityEvidenceSHA256,
		ImageSignatureIdentity: document.RoleImage.SignatureIdentity, ImagePromotedAt: document.RoleImage.PromotedAt,
	}, nil
}

func loadAuthorityPolicyEvidence(path string) (uint64, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return 0, "", errors.New("read legacy migration authority policy")
	}
	var document struct {
		Version  int             `json:"v"`
		Revision uint64          `json:"policy_revision"`
		Policy   json.RawMessage `json:"policy"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || document.Version != 1 || document.Revision == 0 || len(document.Policy) == 0 {
		return 0, "", errors.New("decode legacy migration authority policy")
	}
	var policy any
	if json.Unmarshal(document.Policy, &policy) != nil {
		return 0, "", errors.New("decode legacy migration authority policy body")
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(policy)
	if err != nil {
		return 0, "", errors.New("digest legacy migration authority policy")
	}
	return document.Revision, digest, nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}

func readDSN(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("read database configuration")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil {
		return "", errors.New("read database configuration")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > 8192 {
		return "", errors.New("database configuration is invalid")
	}
	return value, nil
}
