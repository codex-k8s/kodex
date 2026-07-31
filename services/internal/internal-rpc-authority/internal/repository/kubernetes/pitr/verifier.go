package pitr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domainrepository "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	evidenceType       = "mattercodex-internal-rpc-restore-evidence+jws"
	evidenceIssuer     = "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-restore-pitr"
	evidenceAudience   = "urn:mattercodex:internal-rpc-authority-recovery:internal-rpc-authority-primary"
	evidenceDataKey    = "evidence.jws"
	maxResponseBytes   = 64 << 10
	maxCredentialBytes = 16 << 10
)

// Config задаёт exact Kubernetes и trust boundary внешнего PITR evidence.
type Config struct {
	Address            string
	TLSServerName      string
	CAFile             string
	TokenFile          string
	Namespace          string
	EvidenceSecretName string
	PublicJWKFile      string
	Timeout            time.Duration
}

// Verifier читает и проверяет независимо подписанный served evidence.
type Verifier struct {
	config      Config
	client      *http.Client
	evidenceURL string
	publicKey   internalrpcauth.ES256Key
	now         func() time.Time
}

type secretEnvelope struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		UID             string            `json:"uid"`
		ResourceVersion string            `json:"resourceVersion"`
		Annotations     map[string]string `json:"annotations"`
	} `json:"metadata"`
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

// NewVerifier создаёт fail-closed verifier с заранее закреплённым public JWK.
func NewVerifier(config Config) (*Verifier, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "mattercodex-system" ||
		config.EvidenceSecretName != "internal-rpc-authority-restore-evidence" ||
		config.Timeout <= 0 ||
		config.Timeout > 10*time.Second {
		return nil, errors.New("restore evidence verifier configuration is invalid")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API CA")
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("kubernetes API CA is invalid")
	}
	publicRaw, err := os.ReadFile(config.PublicJWKFile)
	if err != nil || len(publicRaw) == 0 || len(publicRaw) > 4096 {
		return nil, errors.New("read restore evidence public key")
	}
	publicKey, err := internalrpcauth.ParsePublicJWK(publicRaw)
	if err != nil {
		return nil, errors.New("parse restore evidence public key")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    rootCAs,
			ServerName: config.TLSServerName,
		},
		ForceAttemptHTTP2: true,
	}
	return &Verifier{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("kubernetes API redirect is forbidden")
			},
		},
		evidenceURL: config.Address + "/api/v1/namespaces/" +
			url.PathEscape(config.Namespace) + "/secrets/" +
			url.PathEscape(config.EvidenceSecretName),
		publicKey: publicKey,
		now:       time.Now,
	}, nil
}

// Close закрывает простаивающие Kubernetes API connections.
func (verifier *Verifier) Close() {
	verifier.client.CloseIdleConnections()
}

// VerifyCompletedEvidence проверяет root, exact intent, forward-only
// predecessor, served annotations и фактическую CNPG identity/timeline.
func (verifier *Verifier) VerifyCompletedEvidence(
	ctx context.Context,
	expected model.RestoreState,
) (model.RestoreCompletionEvidence, error) {
	request, err := verifier.request(ctx)
	if err != nil {
		return model.RestoreCompletionEvidence{}, err
	}
	response, err := verifier.client.Do(request)
	if err != nil {
		return model.RestoreCompletionEvidence{}, errors.New(
			"read restore evidence from Kubernetes API",
		)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil ||
		response.StatusCode != http.StatusOK ||
		len(raw) == 0 ||
		len(raw) > maxResponseBytes {
		return model.RestoreCompletionEvidence{}, errors.New(
			"restore evidence Kubernetes response rejected",
		)
	}
	var secret secretEnvelope
	if err := decodeStrictJSON(raw, &secret); err != nil ||
		secret.APIVersion != "v1" ||
		secret.Kind != "Secret" ||
		secret.Metadata.Name != verifier.config.EvidenceSecretName ||
		secret.Metadata.Namespace != verifier.config.Namespace ||
		secret.Metadata.UID == "" ||
		!validResourceVersion(secret.Metadata.ResourceVersion) ||
		(secret.Type != "" && secret.Type != "Opaque") {
		return model.RestoreCompletionEvidence{}, errors.New(
			"restore evidence Secret identity rejected",
		)
	}
	encoded, ok := secret.Data[evidenceDataKey]
	if !ok || len(secret.Data) != 1 {
		return model.RestoreCompletionEvidence{}, errors.New(
			"restore evidence Secret data rejected",
		)
	}
	compactRaw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(compactRaw) == 0 ||
		len(compactRaw) > internalrpcauth.MaxCompactJWSBytes {
		return model.RestoreCompletionEvidence{}, errors.New(
			"restore evidence compact JWS rejected",
		)
	}
	compact := string(compactRaw)
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		verifier.publicKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: evidenceType, KeyID: verifier.publicKey.KeyID,
		},
	)
	if err != nil {
		return model.RestoreCompletionEvidence{}, errors.New(
			"restore evidence signature rejected",
		)
	}
	var claims model.RestoreFenceEvidenceClaims
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&claims,
	); err != nil {
		return model.RestoreCompletionEvidence{}, errors.New(
			"restore evidence claims rejected",
		)
	}
	digest := sha256.Sum256(compactRaw)
	digestHex := hex.EncodeToString(digest[:])
	expectedTargetsDigest, err := internalrpcauth.CanonicalJSONSHA256(
		expected.ExpectedTargets,
	)
	if err != nil {
		return model.RestoreCompletionEvidence{}, errors.New(
			"digest expected restore target set",
		)
	}
	ackSetDigest, err := internalrpcauth.CanonicalJSONSHA256(expected.ACKs)
	if err != nil {
		return model.RestoreCompletionEvidence{}, errors.New(
			"digest accepted restore ACK set",
		)
	}
	now := verifier.now().UTC().Unix()
	if !validRestoreID(expected.RestoreID) {
		return model.RestoreCompletionEvidence{}, errors.New(
			"expected restore identity rejected",
		)
	}
	clusterPrefix := "internal-rpc-authority-restore-" +
		strings.ReplaceAll(expected.RestoreID[:8], "_", "-") + "-"
	if claims.Version != model.ContractVersion ||
		claims.Issuer != evidenceIssuer ||
		claims.Audience != evidenceAudience ||
		claims.Phase != "COMPLETED" ||
		claims.AnchorRevision != expected.AnchorRevision+1 ||
		claims.RestoreEpoch != expected.RestoreEpoch ||
		claims.DatabaseClusterID != expected.DatabaseClusterID ||
		claims.RestoreID != expected.RestoreID ||
		claims.BackupManifestDigestSHA256 != expected.BackupManifestDigest ||
		claims.BackupResourceName == "" ||
		len(claims.BackupResourceUID) < 8 ||
		!validResourceVersion(claims.BackupResourceVersion) ||
		claims.BackupResourceGeneration == 0 ||
		claims.ProviderBackupID == "" ||
		claims.ProviderBackupName == "" ||
		len(claims.SourceClusterUID) < 8 ||
		!validResourceVersion(claims.SourceClusterResourceVersion) ||
		claims.SourceClusterGeneration == 0 ||
		len(claims.SourceClusterSpecSHA256) != sha256.Size*2 ||
		claims.BarmanObjectName == "" ||
		claims.BarmanServerName != expected.DatabaseClusterID ||
		claims.SourceTimelineID == 0 ||
		claims.RecoveryTargetTime != expected.RecoveryTargetUnix ||
		claims.ControllerSignerGeneration != expected.ControllerGeneration ||
		claims.WorkloadSetRevision != expected.WorkloadSetRevision ||
		claims.ExpectedWorkloadRoleGenerationsSHA256 != expectedTargetsDigest ||
		claims.QuiescenceACKSetSHA256 != ackSetDigest ||
		claims.ExpectedACKCount != uint32(len(expected.ExpectedTargets)) ||
		claims.AcceptedACKCount != uint32(len(expected.ACKs)) ||
		claims.SemanticTransition != "EXACT_INCREMENT_WITH_PREDECESSOR_DIGEST" ||
		claims.Predecessor.Revision != expected.AnchorRevision ||
		claims.Predecessor.DigestSHA256 != expected.EvidenceDigest ||
		claims.IssuedAt <= 0 ||
		claims.IssuedAt > now+30 ||
		claims.RestoreCompletedAt < claims.IssuedAt ||
		claims.RestoreCompletedAt > now+30 ||
		len(claims.RestoredClusterUID) < 8 ||
		len(claims.RestoredClusterUID) > 128 ||
		!strings.HasPrefix(claims.RestoredPrimary, clusterPrefix) ||
		claims.RestoredTimelineID == 0 ||
		claims.RestoredTimelineID < claims.SourceTimelineID ||
		claims.ProviderObservedGeneration == 0 ||
		!validResourceVersion(claims.RestoredClusterResourceVersion) {
		return model.RestoreCompletionEvidence{}, errors.New(
			"restore evidence intent or provider binding rejected",
		)
	}
	if err := verifyAnnotations(secret.Metadata.Annotations, claims, digestHex); err != nil {
		return model.RestoreCompletionEvidence{}, err
	}
	return model.RestoreCompletionEvidence{
		CompactJWSDigestSHA256: digestHex,
		AnchorRevision:         claims.AnchorRevision,
		RestoreEpoch:           claims.RestoreEpoch,
		RestoredClusterUID:     claims.RestoredClusterUID,
		RestoredTimelineID:     claims.RestoredTimelineID,
		RestoreCompletedAt:     time.Unix(claims.RestoreCompletedAt, 0).UTC(),
	}, nil
}

func validRestoreID(value string) bool {
	if len(value) != 36 ||
		value[8] != '-' ||
		value[13] != '-' ||
		value[18] != '-' ||
		value[23] != '-' ||
		value[14] < '1' ||
		value[14] > '8' ||
		!strings.ContainsRune("89ab", rune(value[19])) {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validResourceVersion(value string) bool {
	if len(value) == 0 || len(value) > 64 || value[0] == '0' {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func (verifier *Verifier) request(ctx context.Context) (*http.Request, error) {
	token, err := readToken(verifier.config.TokenFile)
	if err != nil {
		return nil, errors.New("read Kubernetes API workload token")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		verifier.evidenceURL,
		nil,
	)
	if err != nil {
		return nil, errors.New("construct restore evidence request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return request, nil
}

func verifyAnnotations(
	annotations map[string]string,
	claims model.RestoreFenceEvidenceClaims,
	digest string,
) error {
	expected := map[string]string{
		"mattercodex.dev/restore-anchor-revision":              strconv.FormatUint(claims.AnchorRevision, 10),
		"mattercodex.dev/restore-epoch":                        strconv.FormatUint(claims.RestoreEpoch, 10),
		"mattercodex.dev/restore-evidence-digest-sha256":       digest,
		"mattercodex.dev/restore-predecessor-revision":         strconv.FormatUint(claims.Predecessor.Revision, 10),
		"mattercodex.dev/restore-predecessor-digest-sha256":    claims.Predecessor.DigestSHA256,
		"mattercodex.dev/restored-cluster-uid":                 claims.RestoredClusterUID,
		"mattercodex.dev/restored-timeline-id":                 strconv.FormatUint(claims.RestoredTimelineID, 10),
		"mattercodex.dev/cnpg-backup-uid":                      claims.BackupResourceUID,
		"mattercodex.dev/cnpg-backup-resource-version":         claims.BackupResourceVersion,
		"mattercodex.dev/cnpg-backup-id":                       claims.ProviderBackupID,
		"mattercodex.dev/cnpg-source-cluster-uid":              claims.SourceClusterUID,
		"mattercodex.dev/cnpg-source-cluster-resource-version": claims.SourceClusterResourceVersion,
	}
	for name, value := range expected {
		if annotations[name] != value {
			return errors.New("restore evidence served annotation mismatch")
		}
	}
	return nil
}

func readToken(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() ||
		info.Mode().Perm()&0o007 != 0 ||
		info.Size() <= 0 ||
		info.Size() > maxCredentialBytes {
		return "", errors.New("workload token file is unsafe")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || len(token) > maxCredentialBytes {
		return "", errors.New("workload token is invalid")
	}
	return token, nil
}

func decodeStrictJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}

var _ domainrepository.RestoreEvidenceVerifier = (*Verifier)(nil)
