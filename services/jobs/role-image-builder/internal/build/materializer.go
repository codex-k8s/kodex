package build

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// ErrMaterialization is the closed input-fetch sentinel used without upstream
// response bodies, credential bytes or underlying private filesystem diagnostics.
// ErrMaterialization — закрытый sentinel загрузки входов без upstream response body,
// реквизитов и исходной закрытой filesystem-диагностики.
var ErrMaterialization = errors.New("role image input materialization failed")

// maximumManifestBytes caps the accepted OCI manifest bytes, independently of the
// much larger context/package/tool blob limits; one extra byte detects overflow.
// maximumManifestBytes ограничивает принятый OCI manifest отдельно от больших
// context/package/tool blobs; один дополнительный байт обнаруживает превышение.
const maximumManifestBytes = int64(1 << 20)

// MaterializerConfig fixes the input repository and TLS SNI/CA/client-key paths
// plus Docker auth file; it does not carry staging push authority or BuildKit auth.
// MaterializerConfig фиксирует input repository и TLS SNI/CA/client-key пути
// с Docker auth file; staging push authority и BuildKit auth здесь нет.
type MaterializerConfig struct {
	DockerConfig, Repository, TLSServerName string
	CAFile, CertificateFile, PrivateKeyFile string
}

// Materializer retains a fixed input repository, private pull credentials and
// authenticated HTTP client to populate a caller-owned isolated workspace. These
// credentials stay in the fetch boundary, not in generated build context or BuildKit.
// Materializer хранит фиксированный input repository, закрытые pull credentials
// и HTTP-клиент для заполнения изолированного workspace вызывающего кода.
// Реквизиты остаются в fetch boundary, не в build context либо BuildKit.
type Materializer struct {
	client                 *http.Client
	repository, host, path string
	username, password     string
}

// newMaterializer validates path/repository/SNI shape, loads the exact client
// certificate/CA/auth and configures TLS1.3, no environment proxy or redirects,
// disabled keep-alive, a TCP dial budget and a total HTTP timeout. CA/auth file size
// checks occur after whole-file reads; this is not bounded-memory private-file I/O.
// Construction does not contact the registry or prove its operation permissions.
// newMaterializer проверяет форму path/repository/SNI, загружает точные client
// certificate/CA/auth и настраивает TLS1.3, отсутствие env proxy/redirect, отключённый
// keep-alive, TCP dial budget и общий HTTP timeout. Размер CA/auth проверяется после
// полного чтения; это не bounded-memory private-file I/O. Registry и права операций
// при создании не проверяются.
func newMaterializer(config MaterializerConfig) (*Materializer, error) {
	if !filepath.IsAbs(config.DockerConfig) || !filepath.IsAbs(config.CAFile) ||
		!filepath.IsAbs(config.CertificateFile) || !filepath.IsAbs(config.PrivateKeyFile) ||
		!validRepository(config.Repository) || !validDNSName(config.TLSServerName) {
		return nil, errors.New("role image materializer configuration is invalid")
	}
	host, repositoryPath, found := strings.Cut(config.Repository, "/")
	if !found || host == "" || repositoryPath == "" {
		return nil, errors.New("role image materializer repository is invalid")
	}
	ca, err := os.ReadFile(config.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, ErrMaterialization
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, ErrMaterialization
	}
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, ErrMaterialization
	}
	username, password, err := readRegistryCredential(config.DockerConfig, host)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: nil,
		TLSClientConfig: &tls.Config{ //nolint:gosec // fixed TLS 1.3 destination with exact SNI and CA.
			MinVersion: tls.VersionTLS13, ServerName: config.TLSServerName,
			RootCAs: pool, Certificates: []tls.Certificate{certificate},
		},
		ForceAttemptHTTP2: false, DisableKeepAlives: true,
		DialContext: (&netDialer{timeout: 3 * time.Second}).DialContext,
	}
	return &Materializer{client: &http.Client{Transport: transport, Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return ErrMaterialization }},
		repository: config.Repository, host: host, path: repositoryPath,
		username: username, password: password}, nil
}

// Check requires HTTP200 from authenticated GET /v2/ using the materialization
// client. It checks that transport/root route, not manifest/blob digest integrity
// or repository-level download eligibility; all other statuses fail closed.
// Check требует HTTP200 от authenticated GET /v2/ клиентом materialization.
// Проверяется transport/root route, не целостность manifest/blob и не допустимость
// скачивания repository; прочие статусы дают закрытый отказ.
func (materializer *Materializer) Check(ctx context.Context) error {
	request, err := materializer.request(ctx, http.MethodGet, "https://"+materializer.host+"/v2/")
	if err != nil {
		return ErrMaterialization
	}
	response, err := materializer.client.Do(request)
	if err != nil {
		return ErrMaterialization
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrMaterialization
	}
	return nil
}

// Materialize checks input/root/source, downloads the exact archive, invokes the
// caller's reporting hook before extraction, validates context/source bindings and
// removes the archive before ordered package/tool downloads. Package digest prefix
// handling and separate payload directories remain caller-visible; errors retain
// their stage/reason. Workspace isolation and cleanup of partially written output
// belong to the caller rather than an implicit rollback in this method.
// Materialize проверяет input/root/source, скачивает точный архив, вызывает reporting
// hook до извлечения, проверяет context/source и удаляет архив перед последовательной
// загрузкой package/tool. Обработка prefix package digest и отдельные каталоги
// сохранены; ошибки имеют прежние stage/reason. Изоляция workspace и очистка
// partial output принадлежат вызывающему, не скрытому rollback этого метода.
func (materializer *Materializer) Materialize(
	ctx context.Context,
	root string,
	input *controlplanev1.RoleImageBuildInput,
	beforeContextValidation func() error,
) (string, error) {
	if input == nil {
		return inputFetchRejectedDiagnostic, rejectInput(inputReasonOwnerScope, ErrMaterialization)
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return inputFetchRejectedDiagnostic, rejectInput(inputReasonWorkspaceCreate, ErrMaterialization)
	}
	if !materializer.allowedRef(input.GetContextRef()) {
		return inputFetchRejectedDiagnostic, rejectInput(inputReasonContextRef, ErrMaterialization)
	}
	archive := filepath.Join(root, "context.tar")
	if err := materializer.downloadExact(ctx, input.GetContextRef(), archive, input.GetContextSha256(), maximumContextBytes); err != nil {
		return inputDigestMismatchDiagnostic, err
	}
	if beforeContextValidation == nil {
		return "CONTEXT_REPORT_REJECTED", ErrMaterialization
	}
	if err := beforeContextValidation(); err != nil {
		return "CONTEXT_REPORT_REJECTED", err
	}
	contextDirectory := filepath.Join(root, "context")
	if err := os.MkdirAll(contextDirectory, 0o700); err != nil {
		return "ARCHIVE_REJECTED", ErrMaterialization
	}
	if err := ExtractContext(archive, contextDirectory, input.GetContextSha256(), input.GetSourceSha256()); err != nil {
		return "ARCHIVE_REJECTED", err
	}
	if err := os.Remove(archive); err != nil {
		return "ARCHIVE_REJECTED", ErrMaterialization
	}
	for _, item := range input.GetPackages() {
		digest := strings.TrimPrefix(item.GetDigest(), "sha256:")
		if diagnostic, err := materializer.materializePinnedInput(ctx, item.GetSourceRef(), digest, filepath.Join(contextDirectory, ".kodex", "packages")); err != nil {
			return diagnostic, err
		}
	}
	for _, item := range input.GetTools() {
		if diagnostic, err := materializer.materializePinnedInput(ctx, item.GetSourceRef(), item.GetSha256(), filepath.Join(contextDirectory, ".kodex", "tools")); err != nil {
			return diagnostic, err
		}
	}
	return "", nil
}

// materializePinnedInput preserves the common package/tool sequence: exact source
// reference first, bare payload SHA second, then bounded download into the caller's
// fixed directory under that digest. The context archive uses a separate larger
// budget and callback/extraction path and deliberately does not use this helper.
// materializePinnedInput сохраняет общий порядок package/tool: точный source ref,
// затем payload SHA без префикса, затем ограниченное скачивание в заданный каталог
// под именем хеша. Архив контекста имеет отдельные больший лимит и callback/extraction
// и намеренно не проходит через этот helper.
func (materializer *Materializer) materializePinnedInput(ctx context.Context, reference, expectedSHA256, directory string) (string, error) {
	if !materializer.allowedRef(reference) {
		return inputFetchRejectedDiagnostic, rejectInput(inputReasonContextRef, ErrMaterialization)
	}
	if !plainSHA256(expectedSHA256) {
		return inputFetchRejectedDiagnostic, rejectInput(inputReasonSHASchema, ErrMaterialization)
	}
	if err := materializer.downloadExact(ctx, reference, filepath.Join(directory, expectedSHA256), expectedSHA256, maximumFileBytes); err != nil {
		return inputDigestMismatchDiagnostic, err
	}
	return "", nil
}

// downloadExact verifies a bounded manifest against its reference digest and exact
// one-layer media type/payload hash/size, then streams that blob through a bounded
// file+hash writer. Only matching length/hash is renamed from exclusive 0600 .part;
// mismatches/rename errors attempt partial cleanup. Rename may replace an existing
// target and no file/directory fsync is performed, so this is not no-clobber or
// crash-durable publication and still requires the caller's isolated destination.
// downloadExact проверяет ограниченный manifest по reference digest и точным
// one-layer media type/payload hash/size, затем пишет blob через ограниченный
// file+hash writer. Лишь совпадение длины/хеша публикуется rename из exclusive 0600
// .part; при mismatch/rename error предпринимается очистка. Rename может заменить
// существующий target, file/directory fsync нет: это не no-clobber/crash-durable
// публикация и по-прежнему требует изолированного destination вызывающего кода.
func (materializer *Materializer) downloadExact(
	ctx context.Context,
	reference, destination, expectedSHA256 string,
	maximumBytes int64,
) error {
	manifestDigest, ok := materializer.referenceDigest(reference)
	if !ok || !plainSHA256(expectedSHA256) {
		return ErrMaterialization
	}
	manifestURL := "https://" + materializer.host + "/v2/" + materializer.path + "/manifests/" + manifestDigest
	request, err := materializer.request(ctx, http.MethodGet, manifestURL)
	if err != nil {
		return ErrMaterialization
	}
	request.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	response, err := materializer.client.Do(request)
	if err != nil {
		return ErrMaterialization
	}
	manifest, readErr := io.ReadAll(io.LimitReader(response.Body, maximumManifestBytes+1))
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusOK || readErr != nil || closeErr != nil || int64(len(manifest)) > maximumManifestBytes ||
		"sha256:"+sha256Hex(manifest) != manifestDigest {
		return ErrMaterialization
	}
	var document struct {
		SchemaVersion int    `json:"schemaVersion"`
		MediaType     string `json:"mediaType"`
		Layers        []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
		} `json:"layers"`
	}
	if json.Unmarshal(manifest, &document) != nil || document.SchemaVersion != 2 ||
		document.MediaType != "application/vnd.oci.image.manifest.v1+json" || len(document.Layers) != 1 ||
		document.Layers[0].MediaType != "application/vnd.kodex.role-image-input.v1" ||
		document.Layers[0].Digest != "sha256:"+expectedSHA256 || document.Layers[0].Size < 1 ||
		document.Layers[0].Size > maximumBytes {
		return ErrMaterialization
	}
	blobURL := "https://" + materializer.host + "/v2/" + materializer.path + "/blobs/" + document.Layers[0].Digest
	request, err = materializer.request(ctx, http.MethodGet, blobURL)
	if err != nil {
		return ErrMaterialization
	}
	response, err = materializer.client.Do(request)
	if err != nil {
		return ErrMaterialization
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength > maximumBytes ||
		(response.ContentLength >= 0 && response.ContentLength != document.Layers[0].Size) {
		return ErrMaterialization
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return ErrMaterialization
	}
	temporary, err := os.OpenFile(destination+".part", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ErrMaterialization
	}
	digest := sha256.New()
	limited := &boundedWriter{writer: io.MultiWriter(temporary, digest), remaining: maximumBytes}
	written, copyErr := io.Copy(limited, response.Body)
	closeErr = temporary.Close()
	if copyErr != nil || closeErr != nil || limited.exceeded || written != document.Layers[0].Size ||
		hex.EncodeToString(digest.Sum(nil)) != expectedSHA256 {
		_ = os.Remove(destination + ".part")
		return fmt.Errorf("%w: immutable payload mismatch", ErrMaterialization)
	}
	if err := os.Rename(destination+".part", destination); err != nil {
		_ = os.Remove(destination + ".part")
		return ErrMaterialization
	}
	return nil
}

// request builds the caller-context request with fixed User-Agent and retained
// Basic auth. It does not validate target eligibility itself: callers must construct
// the fixed registry URL before passing it, rather than accept arbitrary payload URLs.
// request создаёт запрос с caller context, фиксированным User-Agent и сохранённым
// Basic auth. Допустимость target здесь не проверяется: вызывающий обязан построить
// фиксированный registry URL, а не передавать произвольный URL из payload.
func (materializer *Materializer) request(ctx context.Context, method, target string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(materializer.username, materializer.password)
	request.Header.Set("User-Agent", "kodex-role-image-materializer/1")
	return request, nil
}

// referenceDigest returns the manifest digest only after allowedRef accepts the exact
// repository URI; this digest is distinct from the single layer's expected payload hash.
// referenceDigest возвращает manifest digest лишь после проверки точного repository
// URI в allowedRef; это не ожидаемый payload hash единственного layer.
func (materializer *Materializer) referenceDigest(reference string) (string, bool) {
	if !materializer.allowedRef(reference) {
		return "", false
	}
	_, digest, _ := strings.Cut(strings.TrimPrefix(reference, "oci://"), "@")
	return digest, true
}

// allowedRef requires oci://, the exact configured repository, a single @ with the
// registered digest grammar and no query/fragment/whitespace. It is lexical input
// validation, not DNS/IP authorization, registry authentication or proof of ownership.
// allowedRef требует oci://, точный repository, единственный @ с установленной
// грамматикой digest и отсутствие query/fragment/whitespace. Это лексическая
// проверка, не DNS/IP authorization, аутентификация registry и не proof владения.
func (materializer *Materializer) allowedRef(reference string) bool {
	value := strings.TrimPrefix(reference, "oci://")
	return strings.HasPrefix(reference, "oci://") && strings.HasPrefix(value, materializer.repository+"@") &&
		strings.Count(value, "@") == 1 && digestPattern.MatchString(strings.SplitN(value, "@", 2)[1]) &&
		!strings.ContainsAny(value, "?# \r\n\t")
}

// readRegistryCredential reads a Docker config with exactly one auth entry for host,
// rejects external credential helpers and decodes nonempty Basic user/password without
// CR/LF. Whole-file and base64 decoding happen before their size checks, and ordinary
// JSON unmarshal is not a strict duplicate/unknown-field or private-file validator.
// readRegistryCredential читает Docker config с одной auth-записью точного host,
// запрещает внешние credential helpers и получает непустые Basic user/password без
// CR/LF. Полное чтение/base64 decode предшествуют лимитам; обычный JSON unmarshal
// не является strict duplicate/unknown-field и private-file проверкой.
func readRegistryCredential(path, host string) (string, string, error) {
	value, err := os.ReadFile(path)
	if err != nil || len(value) == 0 || len(value) > 1<<20 {
		return "", "", ErrMaterialization
	}
	var document struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
		CredsStore  string            `json:"credsStore"`
		CredHelpers map[string]string `json:"credHelpers"`
	}
	if json.Unmarshal(value, &document) != nil || document.CredsStore != "" || len(document.CredHelpers) != 0 || len(document.Auths) != 1 {
		return "", "", ErrMaterialization
	}
	entry, exists := document.Auths[host]
	if !exists || entry.Auth == "" {
		return "", "", ErrMaterialization
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil || len(decoded) > 8192 {
		return "", "", ErrMaterialization
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found || username == "" || password == "" || strings.ContainsAny(username, "\r\n") || strings.ContainsAny(password, "\r\n") {
		return "", "", ErrMaterialization
	}
	return username, password, nil
}

// boundedWriter tracks accepted byte counts and remaining blob budget around the
// file+hash writer, flagging an oversized chunk without buffering or truncating it.
// boundedWriter учитывает принятые байты и остаток blob-бюджета вокруг file+hash
// writer, отмечая oversized chunk без буферизации либо усечения.
type boundedWriter struct {
	writer             io.Writer
	remaining, written int64
	exceeded           bool
}

// Write rejects a chunk larger than the remaining budget before any write; otherwise
// it delegates once, updates counters by actual bytes and preserves its error/short
// write result without retry, leaving final integrity checks to downloadExact.
// Write отклоняет chunk сверх остатка до записи; иначе один раз вызывает writer,
// обновляет счётчики по фактическим байтам и сохраняет error/short write без повтора.
// Итоговую целостность проверяет downloadExact.
func (writer *boundedWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > writer.remaining {
		writer.exceeded = true
		return 0, ErrMaterialization
	}
	n, err := writer.writer.Write(value)
	writer.remaining -= int64(n)
	writer.written += int64(n)
	return n, err
}

// sha256Hex returns the lowercase bare SHA-256 of the supplied bytes without
// normalization or an OCI algorithm prefix, for exact manifest/payload comparisons.
// sha256Hex возвращает строчный SHA-256 точных байтов без нормализации и OCI-prefix
// для точного сравнения manifest/payload.
func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// netDialer carries only TCP connection timing; it does not independently enforce
// repository, host or IP allowlists and relies on the caller's fixed request target.
// netDialer хранит лишь TCP timing, не проверяет repository/host/IP allowlist
// самостоятельно и полагается на фиксированный request target вызывающего кода.
type netDialer struct{ timeout time.Duration }

// DialContext accepts nonempty TCP destinations with the configured connect timeout
// and disabled keep-alive, preserving caller cancellation without a fallback network.
// DialContext принимает непустой TCP destination с заданным connect timeout
// и отключённым keep-alive, сохраняя caller cancellation без fallback-сети.
func (dialer *netDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address == "" {
		return nil, ErrMaterialization
	}
	return (&net.Dialer{Timeout: dialer.timeout, KeepAlive: -1}).DialContext(ctx, network, address)
}
