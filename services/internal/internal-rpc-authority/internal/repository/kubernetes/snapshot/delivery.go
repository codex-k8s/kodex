package snapshot

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	authoritysnapshot "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/snapshot"
)

// maxResponseBytes limits each Kubernetes Secret response to two MiB, with one extra byte read so oversized responses are rejected rather than silently truncated.
// maxResponseBytes ограничивает каждый ответ Kubernetes Secret двумя МиБ с чтением дополнительного байта для отказа при превышении вместо молчаливого усечения.
const maxResponseBytes = 2 << 20

// Config identifies the sole snapshot Secret, fixed Kubernetes API/TLS boundary, local CA/token paths and bounded request timeout; it does not carry caller-selected resource authority.
// Config указывает единственный snapshot Secret, фиксированную Kubernetes API и TLS boundary, локальные пути CA и токена и ограниченный timeout; полномочия на выбранный caller ресурс не передаются.
type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	Namespace     string
	SecretName    string
	Timeout       time.Duration
}

// Delivery reads and version-CAS-updates the configured pre-existing Secret through an owned HTTP client. It validates publication byte/pin equality, while cryptographic JWS trust belongs to downstream snapshot verification.
// Delivery читает и обновляет настроенный существующий Secret по version CAS через собственный HTTP-клиент. Проверяется равенство байтов и привязок публикации, а криптографическое доверие JWS принадлежит последующей проверке снимка.
type Delivery struct {
	config      Config
	client      *http.Client
	resourceURL string
}

// secretEnvelope models the Secret identity, resource version, annotations and Base64 data needed for CAS. JSON decoding tolerates unmodeled Kubernetes fields; readEnvelope separately checks the selected identity.
// secretEnvelope описывает идентичность Secret, resource version, annotations и данные Base64 для CAS. JSON-декодирование допускает неописанные Kubernetes-поля; readEnvelope отдельно проверяет выбранную идентичность.
type secretEnvelope struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		UID             string            `json:"uid,omitempty"`
		ResourceVersion string            `json:"resourceVersion"`
		Annotations     map[string]string `json:"annotations"`
	} `json:"metadata"`
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

// New restricts delivery to the fixed in-cluster API and snapshot Secret, validates the timeout and CA, then configures TLS 1.3, exact server name and redirect refusal. It does not read the token or contact the API during construction.
// New ограничивает доставку фиксированными внутрикластерными API и snapshot Secret, проверяет timeout и CA, затем задаёт TLS 1.3, точное серверное имя и запрет redirect. Токен не читается и обращения к API при создании нет.
func New(config Config) (*Delivery, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "kodex-system" ||
		config.SecretName != "internal-rpc-authority-snapshot" ||
		config.Timeout < time.Second ||
		config.Timeout > 10*time.Second {
		return nil, errors.New("authority snapshot Kubernetes configuration is invalid")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read authority snapshot Kubernetes CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("authority snapshot Kubernetes CA is invalid")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    roots,
			ServerName: config.TLSServerName,
		},
		ForceAttemptHTTP2: true,
	}
	return &Delivery{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("kubernetes API redirect is forbidden")
			},
		},
		resourceURL: config.Address + "/api/v1/namespaces/" +
			url.PathEscape(config.Namespace) + "/secrets/" +
			url.PathEscape(config.SecretName),
	}, nil
}

// Close releases idle client connections without deleting the Secret, reverting a publication or cancelling an active caller-owned request.
// Close освобождает простаивающие соединения клиента без удаления Secret, отмены публикации и прерывания активного запроса вызывающего.
func (delivery *Delivery) Close() {
	delivery.client.CloseIdleConnections()
}

// Publish validates supplied revision/digest/size, prevents observed rollback and same-revision mutation, and attempts at most four resourceVersion PUTs against an existing Secret. Conflicts retry from a new read; success requires served pin/byte equality, not signature verification, and no independent post-PUT GET is issued.
// Publish проверяет переданные revision, digest и размер, запрещает наблюдённый rollback и изменение той же ревизии и выполняет до четырёх resourceVersion PUT существующего Secret. Конфликт повторяется с новым чтением; успех требует совпадения served pins и байтов, а не подписи; отдельного GET после PUT нет.
func (delivery *Delivery) Publish(
	ctx context.Context,
	publication model.AuthoritySnapshotPublication,
) (model.AuthoritySnapshotPublication, error) {
	if publication.SourceRevision == 0 ||
		!validDigest(publication.SourceDigestSHA256) ||
		publication.SnapshotCompactJWS == "" ||
		len(publication.SnapshotCompactJWS) > authoritysnapshot.MaxPublisherSnapshotBytes {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority snapshot publication is invalid",
		)
	}
	for attempt := 0; attempt < 4; attempt++ {
		current, err := delivery.readEnvelope(ctx)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, err
		}
		currentRevision, _ := strconv.ParseUint(
			current.Metadata.Annotations["kodex.dev/source-revision"],
			10,
			64,
		)
		currentDigest := current.Metadata.Annotations["kodex.dev/source-digest-sha256"]
		if currentRevision > publication.SourceRevision ||
			currentRevision == publication.SourceRevision &&
				currentDigest != "" &&
				currentDigest != publication.SourceDigestSHA256 {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"authority snapshot rollback or same-revision mutation rejected",
			)
		}
		if currentRevision == publication.SourceRevision &&
			currentDigest == publication.SourceDigestSHA256 {
			return delivery.publication(current, publication)
		}
		current.Metadata.Annotations = map[string]string{
			"kodex.dev/source-revision": strconv.FormatUint(
				publication.SourceRevision,
				10,
			),
			"kodex.dev/source-digest-sha256": publication.SourceDigestSHA256,
			"kodex.dev/signer-generation": strconv.FormatUint(
				publication.SignerGeneration,
				10,
			),
		}
		current.Type = "Opaque"
		current.Data = map[string]string{
			"snapshot.jws": base64.StdEncoding.EncodeToString(
				[]byte(publication.SnapshotCompactJWS),
			),
		}
		body, err := json.Marshal(current)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"encode authority snapshot Secret",
			)
		}
		response, raw, err := delivery.do(ctx, http.MethodPut, body)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, err
		}
		if response.StatusCode == http.StatusConflict {
			continue
		}
		if response.StatusCode != http.StatusOK {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"authority snapshot Secret CAS rejected",
			)
		}
		var served secretEnvelope
		if err := json.Unmarshal(raw, &served); err != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"decode authority snapshot Secret CAS response",
			)
		}
		return delivery.publication(served, publication)
	}
	return model.AuthoritySnapshotPublication{}, errors.New(
		"authority snapshot Secret CAS retries exhausted",
	)
}

// Read validates the Secret envelope, parses revision and lowercase digest, requires exactly one data key and strictly decodes bounded nonempty Base64 JWS bytes. It does not verify the JWS signature or populate signer generation from annotations.
// Read проверяет оболочку Secret, разбирает revision и строчный digest, требует ровно один data key и строго декодирует ограниченные непустые Base64-байты JWS. Подпись JWS не проверяется и signer generation из annotations не заполняется.
func (delivery *Delivery) Read(
	ctx context.Context,
) (model.AuthoritySnapshotPublication, error) {
	envelope, err := delivery.readEnvelope(ctx)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	revision, err := strconv.ParseUint(
		envelope.Metadata.Annotations["kodex.dev/source-revision"],
		10,
		64,
	)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"served authority snapshot revision rejected",
		)
	}
	digest := envelope.Metadata.Annotations["kodex.dev/source-digest-sha256"]
	encoded, ok := envelope.Data["snapshot.jws"]
	if !ok || len(envelope.Data) != 1 || !validDigest(digest) {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"served authority snapshot data rejected",
		)
	}
	compactRaw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil ||
		len(compactRaw) == 0 ||
		len(compactRaw) > authoritysnapshot.MaxPublisherSnapshotBytes {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"served authority snapshot compact JWS rejected",
		)
	}
	return model.AuthoritySnapshotPublication{
		SourceRevision:          revision,
		SourceDigestSHA256:      digest,
		SnapshotCompactJWS:      string(compactRaw),
		SnapshotResourceVersion: envelope.Metadata.ResourceVersion,
	}, nil
}

// readEnvelope requires HTTP 200 and validates API kind, configured name/namespace, nonempty UID/resourceVersion and empty-or-Opaque type after permissive JSON decoding. It does not validate snapshot data or signatures.
// readEnvelope требует HTTP 200 и после допускающего лишние поля JSON-декодирования проверяет API kind, настроенные имя и namespace, непустые UID и resourceVersion и пустой либо Opaque type. Данные снимка и подписи не проверяются.
func (delivery *Delivery) readEnvelope(
	ctx context.Context,
) (secretEnvelope, error) {
	response, raw, err := delivery.do(ctx, http.MethodGet, nil)
	if err != nil {
		return secretEnvelope{}, err
	}
	if response.StatusCode != http.StatusOK {
		return secretEnvelope{}, errors.New("read authority snapshot Secret rejected")
	}
	var envelope secretEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil ||
		envelope.APIVersion != "v1" ||
		envelope.Kind != "Secret" ||
		envelope.Metadata.Name != delivery.config.SecretName ||
		envelope.Metadata.Namespace != delivery.config.Namespace ||
		envelope.Metadata.UID == "" ||
		envelope.Metadata.ResourceVersion == "" ||
		(envelope.Type != "" && envelope.Type != "Opaque") {
		return secretEnvelope{}, errors.New("authority snapshot Secret identity rejected")
	}
	return envelope, nil
}

// publication requires exact expected revision/digest/signer annotations and decoded snapshot bytes, then returns the expected publication with the served resource version. It does not independently authenticate the signer or reject extra data keys.
// publication требует точных ожидаемых revision, digest и signer annotations и декодированных байтов снимка, затем возвращает ожидаемую публикацию с served resource version. Подписант независимо не аутентифицируется и лишние data keys не отклоняются.
func (delivery *Delivery) publication(
	envelope secretEnvelope,
	expected model.AuthoritySnapshotPublication,
) (model.AuthoritySnapshotPublication, error) {
	if envelope.Metadata.Annotations["kodex.dev/source-revision"] !=
		strconv.FormatUint(expected.SourceRevision, 10) ||
		envelope.Metadata.Annotations["kodex.dev/source-digest-sha256"] !=
			expected.SourceDigestSHA256 ||
		envelope.Metadata.Annotations["kodex.dev/signer-generation"] !=
			strconv.FormatUint(expected.SignerGeneration, 10) {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority snapshot Secret annotations rejected",
		)
	}
	encoded := envelope.Data["snapshot.jws"]
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || !bytes.Equal(raw, []byte(expected.SnapshotCompactJWS)) {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority snapshot Secret readback mismatch",
		)
	}
	expected.SnapshotResourceVersion = envelope.Metadata.ResourceVersion
	return expected, nil
}

// do rereads and trims the token for every fixed-resource request, sets JSON headers, performs the HTTP call and reads a bounded body before closing it. It returns status handling to callers and does not retry ambiguous transport failures.
// do перечитывает и обрезает токен для каждого запроса фиксированного ресурса, задаёт JSON-заголовки, выполняет HTTP-вызов и читает ограниченное тело до закрытия. Статус обрабатывают callers; неопределённый транспортный отказ не повторяется.
func (delivery *Delivery) do(
	ctx context.Context,
	method string,
	body []byte,
) (*http.Response, []byte, error) {
	tokenRaw, err := os.ReadFile(delivery.config.TokenFile)
	token := strings.TrimSpace(string(tokenRaw))
	if err != nil || token == "" || len(token) > 16<<10 {
		return nil, nil, errors.New("read authority snapshot Kubernetes token")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		delivery.resourceURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, nil, errors.New("create authority snapshot Kubernetes request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := delivery.client.Do(request)
	if err != nil {
		return nil, nil, errors.New("perform authority snapshot Kubernetes request")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return nil, nil, errors.New("read authority snapshot Kubernetes response")
	}
	return response, raw, nil
}

// validDigest accepts exactly sixty-four lowercase ASCII hexadecimal characters, without normalizing text or checking that it hashes any particular snapshot bytes.
// validDigest допускает ровно шестьдесят четыре строчных ASCII hex-символа без нормализации текста и проверки их соответствия хешу конкретных байтов снимка.
func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}
