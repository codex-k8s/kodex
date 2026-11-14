package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

const (
	// ObservabilityQualificationFile is the immutable operator mount, never a Connection/input field or an employee-selected file.
	// ObservabilityQualificationFile задаёт immutable operator mount, не Connection/input field и не employee-selected file.
	ObservabilityQualificationFile = "/var/run/kodex/observability-qualifications/profiles.json"
	// qualificationGrafanaAnnotations isolates the legacy SQL numeric-ID ownership contract from newer retargetable annotation storage.
	// qualificationGrafanaAnnotations отделяет legacy SQL numeric-ID ownership contract от нового retargetable annotation storage.
	qualificationGrafanaAnnotations = "GRAFANA_LEGACY_SQL_ANNOTATIONS_V1"
	// qualificationGrafanaRuleDelete requires a whole organization without file/Git alert-rule writers, because native DELETE lacks atomic provenance/version preconditions.
	// qualificationGrafanaRuleDelete требует whole organization без file/Git alert-rule writers: native DELETE не имеет atomic provenance/version preconditions.
	qualificationGrafanaRuleDelete = "GRAFANA_API_ONLY_RULE_DELETE_V1"
	// qualificationLokiRuler binds API-managed namespace writes to an operator-qualified remote ruler store and a real two-key/two-tenant evidence matrix.
	// qualificationLokiRuler связывает API-managed namespace writes с operator-qualified remote ruler store и настоящей two-key/two-tenant evidence matrix.
	qualificationLokiRuler = "LOKI_API_ONLY_RULER_NAMESPACE_V1"
	// operatorQualificationRequiredSummary is a closed safe UI reason, without target addresses, fingerprints, proof payloads or credential values.
	// operatorQualificationRequiredSummary задаёт closed safe UI reason без target addresses, fingerprints, proof payloads и credential values.
	operatorQualificationRequiredSummary = "i18n:INTEGRATION_OPERATOR_QUALIFICATION_REQUIRED"
)

// errOperatorQualificationConfiguration is the fixed process-input diagnostic; runtime proof failures use a separate safe unsupported reason.
// errOperatorQualificationConfiguration задаёт fixed process-input diagnostic; runtime proof failures используют отдельный safe unsupported reason.
var errOperatorQualificationConfiguration = errors.New("observability qualification configuration is invalid")

// operatorQualificationDocument is a bounded trusted deployment artifact. It grants no owner permission and is never accepted through ordinary product/configuration JSON.
// operatorQualificationDocument задаёт bounded trusted deployment artifact. Owner permission он не выдаёт и через ordinary product/configuration JSON не принимается.
type operatorQualificationDocument struct {
	Schema   string                  `json:"schema"`
	Profiles []operatorQualification `json:"profiles"`
}

// operatorQualificationProof references one operator-owned captured request/response pair; status and subject coordinates are checked as a finite evidence profile, not interpreted as arbitrary commands.
// operatorQualificationProof ссылается на одну operator-owned captured request/response pair; status и subject coordinates проверяются как finite evidence profile, не arbitrary commands.
type operatorQualificationProof struct {
	Case             string `json:"case"`
	RequestSHA256    string `json:"requestSha256"`
	ResponseSHA256   string `json:"responseSha256"`
	HTTPStatus       int    `json:"httpStatus"`
	CredentialSHA256 string `json:"credentialSha256,omitempty"`
	TenantID         string `json:"tenantId,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
}

// operatorQualification binds exact target/resource/version/key and bounded validity to immutable operator deployment/writer evidence. Hashes attest provenance; runtime never claims it independently observed those deployment or isolation checks.
// operatorQualification связывает exact target/resource/version/key и bounded validity с immutable operator deployment/writer evidence. Hashes удостоверяют provenance; runtime не заявляет независимого наблюдения deployment либо isolation checks.
type operatorQualification struct {
	Kind                        string                       `json:"kind"`
	Origin                      string                       `json:"origin"`
	ProviderVersion             string                       `json:"providerVersion"`
	OrganizationID              string                       `json:"organizationId,omitempty"`
	DashboardUID                string                       `json:"dashboardUid,omitempty"`
	TenantID                    string                       `json:"tenantId,omitempty"`
	Namespace                   string                       `json:"namespace,omitempty"`
	CredentialSHA256            string                       `json:"credentialSha256"`
	ImageSHA256                 string                       `json:"imageSha256"`
	ConfigurationSHA256         string                       `json:"configurationSha256"`
	WriterInventorySHA256       string                       `json:"writerInventorySha256"`
	RuntimeReadbackSHA256       string                       `json:"runtimeReadbackSha256"`
	QualifiedAt                 string                       `json:"qualifiedAt"`
	ValidUntil                  string                       `json:"validUntil"`
	WriterMode                  string                       `json:"writerMode"`
	AnnotationStoreBackend      string                       `json:"annotationStoreBackend,omitempty"`
	AnnotationAPIMigrationPhase string                       `json:"annotationApiMigrationPhase,omitempty"`
	RulerStorageBackend         string                       `json:"rulerStorageBackend,omitempty"`
	Proofs                      []operatorQualificationProof `json:"proofs"`
}

// validQualificationDigest rejects placeholders and malformed content pins before any operator artifact can affect capability readiness.
// validQualificationDigest отклоняет placeholders и malformed content pins до влияния operator artifact на capability readiness.
func validQualificationDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(value) == 64 && len(decoded) == 32 && strings.ToLower(value) == value && value != strings.Repeat("0", 64)
}

// ValidateObservabilityQualificationConfiguration accepts only the exact optional read-only mount and content digest; credentials or arbitrary paths cannot be supplied through the process option.
// ValidateObservabilityQualificationConfiguration допускает только exact optional read-only mount и content digest; credentials либо arbitrary paths через process option не принимаются.
func ValidateObservabilityQualificationConfiguration(file, digest string) error {
	if file == "" && digest == "" {
		return nil
	}
	if file != ObservabilityQualificationFile || !validQualificationDigest(digest) {
		return errOperatorQualificationConfiguration
	}
	return nil
}

// qualificationUnavailable closes readiness/effects with a known user-facing reason without treating missing operator evidence as an invalid personal key.
// qualificationUnavailable закрывает readiness/effects с known user-facing reason, не считая missing operator evidence неверным personal key.
func qualificationUnavailable() error {
	return &SafeError{Code: providerCapabilityUnsupportedCode, HealthSummary: operatorQualificationRequiredSummary}
}

// operatorQualificationFor rereads the exact bounded immutable artifact on every protected health/effect, verifies its expected bytes and the current owner-selected credential/resource tuple, and rejects expiry or withdrawal before HTTP.
// operatorQualificationFor повторно читает exact bounded immutable artifact при каждом protected health/effect, проверяет expected bytes и current owner-selected credential/resource tuple, отклоняя expiry либо withdrawal до HTTP.
func (adapter *Adapter) operatorQualificationFor(request Request, configuration map[string]string, kind string) (operatorQualification, error) {
	if adapter.observabilityQualificationFile == "" || !validQualificationDigest(adapter.observabilityQualificationSHA256) || request.Credential == nil {
		return operatorQualification{}, qualificationUnavailable()
	}
	file, err := os.Open(adapter.observabilityQualificationFile)
	if err != nil {
		return operatorQualification{}, qualificationUnavailable()
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0222 != 0 || info.Size() < 1 || info.Size() > 128<<10 {
		return operatorQualification{}, qualificationUnavailable()
	}
	raw, err := io.ReadAll(io.LimitReader(file, 128<<10+1))
	if err != nil || len(raw) > 128<<10 {
		return operatorQualification{}, qualificationUnavailable()
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != adapter.observabilityQualificationSHA256 || integrationpackage.ValidateCredentialPermissionJSON(raw) != nil {
		return operatorQualification{}, qualificationUnavailable()
	}
	var document operatorQualificationDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !observabilityJSONFields(raw, reflect.TypeFor[operatorQualificationDocument]()) || decoder.Decode(&document) != nil || document.Schema != "kodex-observability-qualifications/v1" || len(document.Profiles) < 1 || len(document.Profiles) > 32 {
		return operatorQualification{}, qualificationUnavailable()
	}
	seen := map[string]bool{}
	now := time.Now().UTC()
	var selected operatorQualification
	matches := 0
	for _, profile := range document.Profiles {
		if !validOperatorQualification(profile, now) {
			return operatorQualification{}, qualificationUnavailable()
		}
		identity := profile.Kind + "\x00" + profile.Origin + "\x00" + profile.OrganizationID + "\x00" + profile.DashboardUID + "\x00" + profile.TenantID + "\x00" + profile.Namespace + "\x00" + profile.CredentialSHA256
		if seen[identity] {
			return operatorQualification{}, qualificationUnavailable()
		}
		seen[identity] = true
		if profile.Kind != kind || profile.Origin != configuration["base_url"] || profile.CredentialSHA256 != request.Credential.ContentSHA256 {
			continue
		}
		if profile.OrganizationID != configuration["organization_id"] || profile.DashboardUID != configuration["dashboard_uid"] || profile.TenantID != configuration["tenant_id"] || profile.Namespace != configuration["rule_namespace"] {
			continue
		}
		selected = profile
		matches++
	}
	if matches != 1 {
		return operatorQualification{}, qualificationUnavailable()
	}
	return selected, nil
}

// validOperatorQualification checks the operator artifact's exact closed mode, compatible version, immutable source pins and finite proof cases; this is an operator attestation, not an owner grant or runtime isolation observation.
// validOperatorQualification проверяет exact closed mode, compatible version, immutable source pins и finite proof cases operator artifact; это operator attestation, не owner grant либо runtime isolation observation.
func validOperatorQualification(profile operatorQualification, now time.Time) bool {
	if _, err := parseProviderBaseURL(profile.Origin); err != nil {
		return false
	}
	for _, digest := range []string{profile.CredentialSHA256, profile.ImageSHA256, profile.ConfigurationSHA256, profile.WriterInventorySHA256, profile.RuntimeReadbackSHA256} {
		if !validQualificationDigest(digest) {
			return false
		}
	}
	qualified, err := time.Parse(time.RFC3339, profile.QualifiedAt)
	if err != nil {
		return false
	}
	expiry, err := time.Parse(time.RFC3339, profile.ValidUntil)
	if err != nil || !strings.HasSuffix(profile.QualifiedAt, "Z") || !strings.HasSuffix(profile.ValidUntil, "Z") || qualified.After(now.Add(time.Minute)) || !expiry.After(qualified) || expiry.Sub(qualified) > 24*time.Hour || !expiry.After(now) {
		return false
	}
	proofs := map[string]operatorQualificationProof{}
	if len(profile.Proofs) < 3 || len(profile.Proofs) > 16 {
		return false
	}
	for _, proof := range profile.Proofs {
		if proofs[proof.Case].Case != "" || !validQualificationDigest(proof.RequestSHA256) || !validQualificationDigest(proof.ResponseSHA256) || proof.Case == "" || len(proof.Case) > 80 || proof.CredentialSHA256 != "" && !validQualificationDigest(proof.CredentialSHA256) || proof.TenantID != "" && !integrationpackage.ValidSentrySlug(proof.TenantID) {
			return false
		}
		proofs[proof.Case] = proof
	}
	for _, name := range []string{"deployment-readback", "target-config", "writer-inventory"} {
		if proofs[name].HTTPStatus != http.StatusOK {
			return false
		}
	}
	switch profile.Kind {
	case qualificationGrafanaAnnotations:
		return profile.ProviderVersion == "13.2.0" && grafanaIntegerID(profile.OrganizationID) > 0 && integrationpackage.ValidSentrySlug(profile.DashboardUID) && profile.TenantID == "" && profile.Namespace == "" && profile.WriterMode == "LEGACY_SQL_ANNOTATIONS" && profile.AnnotationStoreBackend == "legacy-sql" && profile.AnnotationAPIMigrationPhase == "off" && profile.RulerStorageBackend == "" && len(proofs) == 3
	case qualificationGrafanaRuleDelete:
		return profile.ProviderVersion == "13.2.0" && grafanaIntegerID(profile.OrganizationID) > 0 && profile.DashboardUID == "" && profile.TenantID == "" && profile.Namespace == "" && profile.WriterMode == "API_ONLY_ORGANIZATION_ALERT_RULES" && profile.AnnotationStoreBackend == "" && profile.AnnotationAPIMigrationPhase == "" && profile.RulerStorageBackend == "" && len(proofs) == 3
	case qualificationLokiRuler:
		return profile.ProviderVersion == "3.6.21" && profile.OrganizationID == "" && profile.DashboardUID == "" && integrationpackage.ValidSentrySlug(profile.TenantID) && integrationpackage.ValidSentrySlug(profile.Namespace) && profile.WriterMode == "API_ONLY_RULER_NAMESPACE" && profile.RulerStorageBackend == "S3" && profile.AnnotationStoreBackend == "" && profile.AnnotationAPIMigrationPhase == "" && validLokiQualificationProofs(profile, proofs)
	}
	return false
}

// validLokiQualificationProofs requires separate real-key positive/cross-tenant cases and all three ruler effects; an invalid-key401 pair cannot satisfy the operator isolation profile.
// validLokiQualificationProofs требует отдельные real-key positive/cross-tenant cases и все три ruler effects; пара invalid-key401 не удовлетворяет operator isolation profile.
func validLokiQualificationProofs(profile operatorQualification, proofs map[string]operatorQualificationProof) bool {
	if len(proofs) != 10 {
		return false
	}
	a, b := proofs["tenant-positive-a"], proofs["tenant-positive-b"]
	if a.HTTPStatus != http.StatusOK || b.HTTPStatus != http.StatusOK || a.CredentialSHA256 != profile.CredentialSHA256 || a.TenantID != profile.TenantID || !validQualificationDigest(b.CredentialSHA256) || b.CredentialSHA256 == a.CredentialSHA256 || !integrationpackage.ValidSentrySlug(b.TenantID) || b.TenantID == a.TenantID {
		return false
	}
	for _, name := range []string{"tenant-cross-a-b", "tenant-cross-b-a"} {
		proof := proofs[name]
		if proof.HTTPStatus != http.StatusForbidden && proof.HTTPStatus != http.StatusNotFound {
			return false
		}
		key, tenant := a.CredentialSHA256, b.TenantID
		if name == "tenant-cross-b-a" {
			key, tenant = b.CredentialSHA256, a.TenantID
		}
		if proof.CredentialSHA256 != key || proof.TenantID != tenant {
			return false
		}
	}
	for _, name := range []string{"ruler-create", "ruler-update", "ruler-delete"} {
		proof := proofs[name]
		if proof.HTTPStatus != http.StatusAccepted || proof.CredentialSHA256 != profile.CredentialSHA256 || proof.TenantID != profile.TenantID || proof.Namespace != profile.Namespace {
			return false
		}
	}
	return true
}
