package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type packageManifestDocument struct {
	Identity              *packageManifestIdentity     `json:"identity"`
	Source                *packageManifestSource       `json:"source"`
	Capabilities          []string                     `json:"capabilities"`
	RequiredPlatformAPIs  []string                     `json:"required_platform_apis"`
	RequiredAccessActions []string                     `json:"required_access_actions"`
	Secrets               []value.PackageSecretField   `json:"secrets"`
	Runtime               json.RawMessage              `json:"runtime"`
	Pricing               *packageManifestPricing      `json:"pricing"`
	Verification          *packageManifestVerification `json:"verification"`
}

type packageManifestIdentity struct {
	Slug        string                `json:"slug"`
	Kind        enum.PackageKind      `json:"kind"`
	Publisher   string                `json:"publisher"`
	License     string                `json:"license"`
	Name        []value.LocalizedText `json:"name"`
	Description []value.LocalizedText `json:"description"`
}

type packageManifestSource struct {
	RefKind enum.PackageVersionSourceRefKind `json:"ref_kind"`
	Ref     string                           `json:"ref"`
	Version string                           `json:"version"`
	Digest  string                           `json:"digest"`
}

type packageManifestRuntime struct {
	Required     bool   `json:"required"`
	WorkloadKind string `json:"workload_kind,omitempty"`
}

type packageManifestPricing struct {
	CommercialStatus enum.PackageCommercialStatus `json:"commercial_status"`
}

type packageManifestVerification struct {
	TrustStatus        enum.PackageTrustStatus        `json:"trust_status"`
	VerificationStatus enum.PackageVerificationStatus `json:"verification_status,omitempty"`
	Restrictions       []packageManifestRestriction   `json:"restrictions,omitempty"`
}

type packageManifestRestriction struct {
	Code        string                `json:"code"`
	Description []value.LocalizedText `json:"description"`
}

func normalizePackageManifestPayload(parent CatalogPackageSnapshot, version CatalogVersionSnapshot) ([]byte, error) {
	trimmed := bytes.TrimSpace(version.ManifestPayload)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return nil, errs.ErrInvalidArgument
	}
	var document packageManifestDocument
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	if err := decoder.Decode(&document); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if err := decoder.Decode(&packageManifestDocument{}); !errors.Is(err, io.EOF) {
		return nil, errs.ErrInvalidArgument
	}
	if err := validatePackageManifestDocument(parent, version, document); err != nil {
		return nil, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	if err := requireManifestDigest(version.ManifestDigest, compact.Bytes()); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

func validatePackageManifestDocument(parent CatalogPackageSnapshot, version CatalogVersionSnapshot, document packageManifestDocument) error {
	if err := validatePackageManifestIdentity(parent, document.Identity); err != nil {
		return err
	}
	if err := validatePackageManifestSource(version, document.Source); err != nil {
		return err
	}
	if err := requireStringList(document.Capabilities, true); err != nil {
		return err
	}
	if err := requireStringList(document.RequiredPlatformAPIs, false); err != nil {
		return err
	}
	if err := validateRequiredAccessActions(document.RequiredAccessActions); err != nil {
		return err
	}
	if err := validatePackageManifestSecrets(document.Secrets); err != nil {
		return err
	}
	if err := validatePackageManifestRuntime(document.Runtime); err != nil {
		return err
	}
	if err := validatePackageManifestPricing(parent, document.Pricing); err != nil {
		return err
	}
	return validatePackageManifestVerification(parent, document.Verification)
}

func validatePackageManifestIdentity(parent CatalogPackageSnapshot, identity *packageManifestIdentity) error {
	if identity == nil {
		return errs.ErrInvalidArgument
	}
	identity.Slug = strings.TrimSpace(identity.Slug)
	identity.Publisher = strings.TrimSpace(identity.Publisher)
	identity.License = strings.TrimSpace(identity.License)
	if identity.Slug != parent.Slug || identity.Kind != parent.Kind {
		return errs.ErrInvalidArgument
	}
	if err := requireText(identity.Publisher); err != nil {
		return err
	}
	if err := requireText(identity.License); err != nil {
		return err
	}
	if err := requireLocalizedTexts(identity.Name, true); err != nil {
		return err
	}
	return requireLocalizedTexts(identity.Description, true)
}

func validatePackageManifestSource(version CatalogVersionSnapshot, source *packageManifestSource) error {
	if source == nil {
		return errs.ErrInvalidArgument
	}
	source.Ref = strings.TrimSpace(source.Ref)
	source.Version = strings.TrimSpace(source.Version)
	source.Digest = strings.TrimSpace(source.Digest)
	if source.RefKind != version.SourceRef.Kind || source.Ref != version.SourceRef.Ref || source.Version != version.VersionLabel {
		return errs.ErrInvalidArgument
	}
	return requireText(source.Digest)
}

func validateRequiredAccessActions(actions []string) error {
	if err := requireStringList(actions, false); err != nil {
		return err
	}
	for _, action := range actions {
		if _, ok := accesscatalog.SystemActionByKey(action); !ok {
			return errs.ErrInvalidArgument
		}
	}
	return nil
}

func validatePackageManifestSecrets(secrets []value.PackageSecretField) error {
	seen := make(map[string]struct{}, len(secrets))
	for _, secret := range secrets {
		key := strings.TrimSpace(secret.Key)
		if err := requireText(key); err != nil {
			return err
		}
		if _, exists := seen[key]; exists {
			return errs.ErrInvalidArgument
		}
		seen[key] = struct{}{}
		if err := requireSecretFieldKind(secret.Kind); err != nil {
			return err
		}
		if err := requireLocalizedTexts(secret.DisplayName, true); err != nil {
			return err
		}
		if err := requireLocalizedTexts(secret.Description, false); err != nil {
			return err
		}
	}
	return nil
}

func validatePackageManifestRuntime(payload json.RawMessage) error {
	runtime, _, err := parsePackageManifestRuntime(payload)
	if err != nil {
		return err
	}
	if runtime.Required {
		return requireText(runtime.WorkloadKind)
	}
	return nil
}

func parsePackageManifestRuntime(payload json.RawMessage) (packageManifestRuntime, []byte, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' || !json.Valid(trimmed) {
		return packageManifestRuntime{}, nil, errs.ErrInvalidArgument
	}
	var runtime packageManifestRuntime
	if err := json.Unmarshal(trimmed, &runtime); err != nil {
		return packageManifestRuntime{}, nil, errs.ErrInvalidArgument
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return packageManifestRuntime{}, nil, errs.ErrInvalidArgument
	}
	return runtime, compact.Bytes(), nil
}

func validatePackageManifestPricing(parent CatalogPackageSnapshot, pricing *packageManifestPricing) error {
	if pricing == nil {
		return errs.ErrInvalidArgument
	}
	if pricing.CommercialStatus != parent.CommercialStatus {
		return errs.ErrInvalidArgument
	}
	return requireCommercialStatus(pricing.CommercialStatus)
}

func validatePackageManifestVerification(parent CatalogPackageSnapshot, verification *packageManifestVerification) error {
	if verification == nil {
		return errs.ErrInvalidArgument
	}
	if verification.TrustStatus != parent.TrustStatus {
		return errs.ErrInvalidArgument
	}
	if err := requireTrustStatus(verification.TrustStatus); err != nil {
		return err
	}
	if verification.VerificationStatus != "" {
		if err := requireVerificationStatus(verification.VerificationStatus); err != nil {
			return err
		}
	}
	for _, restriction := range verification.Restrictions {
		if err := requireText(restriction.Code); err != nil {
			return err
		}
		if err := requireLocalizedTexts(restriction.Description, false); err != nil {
			return err
		}
	}
	return nil
}

func requireStringList(items []string, requireNonEmpty bool) error {
	if requireNonEmpty && len(items) == 0 {
		return errs.ErrInvalidArgument
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || item != trimmed {
			return errs.ErrInvalidArgument
		}
		if _, exists := seen[trimmed]; exists {
			return errs.ErrInvalidArgument
		}
		seen[trimmed] = struct{}{}
	}
	return nil
}

func requireManifestDigest(expected string, payload []byte) error {
	expected = strings.TrimSpace(expected)
	actual := sha256Digest(payload)
	if expected != actual {
		return errs.ErrInvalidArgument
	}
	return nil
}

type packageInstallationRequirements struct {
	RuntimeRequirementDigest string
	SecretBindingStatus      enum.PackageSecretBindingStatus
}

func packageInstallationRequirementsFromManifest(payload []byte) (packageInstallationRequirements, error) {
	var document packageManifestDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return packageInstallationRequirements{}, errs.ErrInvalidArgument
	}
	result := packageInstallationRequirements{SecretBindingStatus: enum.PackageSecretBindingStatusNotRequired}
	for _, secret := range document.Secrets {
		if secret.Required {
			result.SecretBindingStatus = enum.PackageSecretBindingStatusMissing
			break
		}
	}
	runtime, runtimePayload, err := parsePackageManifestRuntime(document.Runtime)
	if err != nil {
		return packageInstallationRequirements{}, err
	}
	if runtime.Required {
		result.RuntimeRequirementDigest = sha256Digest(runtimePayload)
	}
	return result, nil
}

func packageSecretSchemaFromManifest(id uuid.UUID, packageVersionID uuid.UUID, payload []byte, createdAt time.Time) (entity.PackageSecretSchema, error) {
	var document packageManifestDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		return entity.PackageSecretSchema{}, errs.ErrInvalidArgument
	}
	fields := normalizePackageSecretFields(document.Secrets)
	payloadBytes, err := json.Marshal(fields)
	if err != nil {
		return entity.PackageSecretSchema{}, errs.ErrInvalidArgument
	}
	return entity.PackageSecretSchema{
		ID:               id,
		PackageVersionID: packageVersionID,
		SchemaDigest:     sha256Digest(payloadBytes),
		Fields:           fields,
		CreatedAt:        createdAt,
	}, nil
}

func normalizePackageSecretFields(secrets []value.PackageSecretField) []value.PackageSecretField {
	fields := make([]value.PackageSecretField, len(secrets))
	for index, secret := range secrets {
		fields[index] = value.PackageSecretField{
			Key:         strings.TrimSpace(secret.Key),
			Kind:        secret.Kind,
			Required:    secret.Required,
			DisplayName: normalizeLocalizedTexts(secret.DisplayName),
			Description: normalizeLocalizedTexts(secret.Description),
		}
	}
	return fields
}

func sha256Digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}
