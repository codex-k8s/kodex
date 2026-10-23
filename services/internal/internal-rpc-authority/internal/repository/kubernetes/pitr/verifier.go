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
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	domainrepository "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	// evidenceType pins the protected JWS type for independently signed restore completion evidence, distinct from directives, ACKs and authorization contexts.
	// evidenceType закрепляет protected JWS type независимо подписанного restore completion evidence отдельно от directives, ACK и authorization contexts.
	evidenceType = "kodex-internal-rpc-restore-evidence+jws"
	// evidenceIssuer fixes the expected PITR workload SPIFFE issuer in signed claims; claim text alone is insufficient without the separately pinned verification key.
	// evidenceIssuer фиксирует ожидаемого SPIFFE-издателя PITR workload в подписанных claims; одного текста claims без отдельно закреплённого проверяющего ключа недостаточно.
	evidenceIssuer = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-restore-pitr"
	// evidenceAudience restricts accepted evidence to recovery of the fixed primary authority database rather than a request-selected database or audience.
	// evidenceAudience ограничивает принимаемое evidence восстановлением фиксированной primary authority database, а не выбранными запросом БД и аудиторией.
	evidenceAudience = "urn:kodex:internal-rpc-authority-recovery:internal-rpc-authority-primary"
	// evidenceDataKey is the sole allowed Secret data entry carrying Base64-encoded compact evidence; VerifyCompletedEvidence rejects additional data entries.
	// evidenceDataKey — единственная допустимая data entry Secret с Base64-кодированным compact evidence; VerifyCompletedEvidence отклоняет дополнительные data entries.
	evidenceDataKey = "evidence.jws"
	// maxResponseBytes caps evidence and shared PITR HTTP response bodies at 64 KiB; callers read one extra byte to reject oversized content rather than accept a truncated document.
	// maxResponseBytes ограничивает evidence и общие PITR HTTP response bodies 64 КиБ; callers читают дополнительный байт для отказа при превышении вместо принятия усечённого документа.
	maxResponseBytes = 64 << 10
	// maxCredentialBytes bounds projected service-account token loading and the final trimmed token to sixteen KiB; it does not relax secure-file ownership or mode checks.
	// maxCredentialBytes ограничивает загрузку projected service-account token и итоговый обрезанный токен шестнадцатью КиБ без ослабления secure-file ownership и mode checks.
	maxCredentialBytes = 16 << 10
)

// Config identifies the fixed evidence Secret and Kubernetes TLS endpoint, local CA/token and public-JWK files, and a bounded request timeout. The caller supplies trusted mounted paths; key values are not part of the config.
// Config указывает фиксированный evidence Secret и Kubernetes TLS endpoint, локальные файлы CA, токена и public JWK и ограниченный timeout. Доверенные смонтированные пути задаёт вызывающий; значения ключей конфигурацией не передаются.
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

// Verifier owns a fixed Secret read URL, HTTP client, pinned public evidence key and clock. It validates signed served evidence against expected restore state, not a live CNPG cluster re-query or a new restore operation.
// Verifier владеет фиксированным URL чтения Secret, HTTP-клиентом, закреплённым публичным evidence key и часами. Подписанное served evidence сверяется с ожидаемым restore state без повторного живого опроса CNPG cluster и нового восстановления.
type Verifier struct {
	config      Config
	client      *http.Client
	evidenceURL string
	publicKey   internalrpcauth.ES256Key
	now         func() time.Time
}

// secretEnvelope models evidence Secret identity, annotations and Base64 data for strict unknown-field decoding. Only fields represented here are accepted before separate identity/data/signature checks.
// secretEnvelope описывает идентичность evidence Secret, annotations и данные Base64 для строгого декодирования неизвестных полей. До отдельных проверок identity, data и signature допускаются только описанные здесь поля.
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

// NewVerifier validates the fixed API/TLS/namespace/Secret and timeout, loads a bounded public JWK and CA, then configures TLS 1.3 and redirect rejection. It performs no token read, evidence fetch or restore-state transition during construction.
// NewVerifier проверяет фиксированные API, TLS, namespace, Secret и timeout, загружает ограниченный public JWK и CA, затем задаёт TLS 1.3 и запрет redirect. При создании нет чтения токена, получения evidence и перехода restore state.
func NewVerifier(config Config) (*Verifier, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "kodex-system" ||
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

// Close releases idle verifier HTTP connections without removing evidence, changing the durable restore anchor or cancelling a caller's active request.
// Close освобождает простаивающие HTTP-соединения verifier без удаления evidence, изменения устойчивого restore anchor и отмены активного запроса вызывающего.
func (verifier *Verifier) Close() {
	verifier.client.CloseIdleConnections()
}

// VerifyCompletedEvidence reads the fixed Secret, validates its envelope/data and canonical ES256 JWS with the pinned key, then checks exact expected intent/ACK/target/predecessor bindings and served annotations. It returns evidence without advancing state or re-reading CNPG; time checks bound future timestamps, not a general maximum evidence age.
// VerifyCompletedEvidence читает фиксированный Secret, проверяет оболочку, данные и canonical ES256 JWS закреплённым ключом, затем точные ожидаемые intent, ACK, target, predecessor bindings и served annotations. Evidence возвращается без продвижения состояния и повторного чтения CNPG; time checks ограничивают будущие метки, а не общий максимальный возраст evidence.
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

// validRestoreID requires a 36-byte lowercase UUID-shaped restore ID with exact hyphens, version one through eight and RFC variant; it does not establish existence, ownership or authorization of that restore.
// validRestoreID требует 36-байтный строчный UUID-подобный restore ID с точными дефисами, версией от одного до восьми и вариантом RFC; существование, владение и разрешение восстановления не устанавливаются.
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

// validResourceVersion accepts one to 64 ASCII decimal digits without a leading zero; it validates text only and neither compares versions nor proves Kubernetes object identity.
// validResourceVersion допускает от одной до 64 ASCII-десятичных цифр без ведущего нуля; проверяется только текст без сравнения версий и доказательства идентичности Kubernetes object.
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

// request rereads the safe projected token and constructs an authenticated GET for the fixed evidence URL using the caller's context. It does not send the request or follow a URL from evidence payloads.
// request перечитывает безопасный projected token и строит аутентифицированный GET фиксированного evidence URL с контекстом вызывающего. Запрос не отправляется и URL из evidence payloads не используются.
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

// verifyAnnotations compares the required twelve served annotation values with signed claims and the exact compact-byte digest, allowing unrelated annotations. It performs equality checks, not signing or durable anchor advancement.
// verifyAnnotations сравнивает двенадцать обязательных served annotations с подписанными claims и дайджестом точных compact-байтов, допуская посторонние annotations. Выполняется сравнение, а не подпись и продвижение устойчивого anchor.
func verifyAnnotations(
	annotations map[string]string,
	claims model.RestoreFenceEvidenceClaims,
	digest string,
) error {
	expected := evidenceAnnotations(claims, digest)
	for name, value := range expected {
		if annotations[name] != value {
			return errors.New("restore evidence served annotation mismatch")
		}
	}
	return nil
}

// readToken delegates to the projected-token secure-file reader, trims whitespace and rejects an empty or oversized result. It returns the credential only to trusted HTTP adapters and replaces file errors with fixed diagnostics.
// readToken использует secure-file reader projected token, обрезает пробелы и отклоняет пустой либо чрезмерный результат. Credential возвращается лишь доверенным HTTP-адаптерам, а ошибки файла заменяются фиксированной диагностикой.
func readToken(path string) (string, error) {
	raw, err := securefile.ReadProjectedServiceAccountToken(path, maxCredentialBytes)
	if err != nil {
		return "", errors.New("workload token file is unsafe")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || len(token) > maxCredentialBytes {
		return "", errors.New("workload token is invalid")
	}
	return token, nil
}

// decodeStrictJSON decodes one JSON value with unknown fields rejected and requires EOF afterward. It can partially mutate the destination on failure and does not reject duplicate keys or impose its own byte/depth limits; callers own those constraints.
// decodeStrictJSON декодирует одно JSON-значение с запретом неизвестных полей и требует EOF после него. При отказе destination может быть частично изменён; повторные ключи не отклоняются и собственных byte и depth limits нет — эти ограничения задают callers.
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
