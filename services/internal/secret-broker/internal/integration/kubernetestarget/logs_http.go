package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"k8s.io/apimachinery/pkg/util/validation"
)

// logsRequest derives only the registered Pod log endpoint and closed query fields; UID remains an owner witness, not a fictitious API precondition.
// The caller must freshly recheck the original Pod UID and permissions before admitting every output frame.
// logsRequest выводит только registered Pod log endpoint и closed query fields; UID остаётся owner witness, не вымышленным API precondition.
// Caller заново проверяет original Pod UID и permissions перед допуском каждого output frame.
func (credential Credential) logsRequest(ctx context.Context, input managedkubernetes.OperationInput, address ResourceAddress, container string) (*http.Request, time.Duration, uint64, error) {
	raw, err := managedkubernetes.EncodeOperationInput("kubernetes.logs", input)
	if err != nil {
		return nil, 0, 0, errNativeStreamUnknown
	}
	defer clear(raw)
	owned, canonical, err := managedkubernetes.DecodeOperationInput("kubernetes.logs", raw)
	defer clear(canonical)
	if err != nil || address.Validate() != nil || address.Group != "" || address.Version != "v1" || address.Resource != "pods" || !address.Namespaced || address.AllNamespaces || address.UID == "" || address.Subresource != "" ||
		owned.Group != address.Group || owned.Version != address.Version || owned.Resource != address.Resource || owned.Namespace != address.Namespace || owned.Name != address.Name || owned.UID != "" && owned.UID != address.UID ||
		len(validation.IsDNS1123Label(container)) != 0 {
		return nil, 0, 0, errNativeStreamUnknown
	}
	options, err := resourceOperationOptions(owned)
	if err != nil {
		return nil, 0, 0, errNativeStreamUnknown
	}
	if selected, ok := options["container"].(string); ok && selected != container {
		return nil, 0, 0, errNativeStreamUnknown
	}
	seconds, valid := resourceInteger(options["timeoutSeconds"])
	if !valid || seconds < 1 || seconds > 300 {
		return nil, 0, 0, errNativeStreamUnknown
	}
	address.Subresource = "log"
	request, err := credential.ReadRequest(ctx, address)
	if err != nil {
		return nil, 0, 0, errNativeStreamUnknown
	}
	query := request.URL.Query()
	query.Set("container", container)
	for _, key := range []string{"follow", "previous", "timestamps"} {
		if option, ok := options[key].(bool); ok {
			query.Set(key, strconv.FormatBool(option))
		}
	}
	maximum := uint64(managedkubernetes.MaximumStreamBytes)
	for _, key := range []string{"sinceSeconds", "tailLines", "limitBytes"} {
		if option, exists := options[key]; exists {
			number, valid := resourceInteger(option)
			if !valid {
				return nil, 0, 0, errNativeStreamUnknown
			}
			query.Set(key, strconv.FormatInt(number, 10))
			if key == "limitBytes" {
				maximum = uint64(number)
			}
		}
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Accept", "text/plain")
	return request, time.Duration(seconds) * time.Second, maximum, nil
}

// nativeReadHTTP owns one pinned HTTPS response, bounded reader and cancellation lifetime without reconnect.
// It retains no kubeconfig; response bytes still need the existing fresh owner/native output credit.
// nativeReadHTTP владеет одним pinned HTTPS response, bounded reader и cancellation lifetime без reconnect.
// Kubeconfig не сохраняется; response bytes требуют existing fresh owner/native output credit.
type nativeReadHTTP struct {
	ctx            context.Context
	cancel         context.CancelFunc
	body           io.ReadCloser
	closeIdle      func()
	closeOnce      sync.Once
	readMu         sync.Mutex
	maximum, spent uint64
	eof            bool
	targetRef      string
	targetRevision int64
	peerDigest     string
	operation      string
}

// openLogsHTTP separates the unary opening attempt from the handed-off worker response and never retries an uncertain GET.
// Native identity/discovery/Pod checks belong to preparation and frame authorization, not to these transport coordinates.
// openLogsHTTP отделяет unary opening attempt от переданного worker response и не повторяет uncertain GET.
// Native identity/discovery/Pod checks принадлежат preparation и frame authorization, не transport coordinates.
func (credential Credential) openLogsHTTP(lifecycle, attempt context.Context, input managedkubernetes.OperationInput, address ResourceAddress, container string, expiresAt, admissionUntil time.Time, maximum uint64) (*nativeReadHTTP, error) {
	if maximum == 0 || maximum > managedkubernetes.MaximumStreamBytes {
		return nil, errNativeStreamUnknown
	}
	request, duration, requestedMaximum, err := credential.logsRequest(lifecycle, input, address, container)
	if err != nil {
		return nil, errNativeStreamUnknown
	}
	return credential.openReadHTTP(lifecycle, attempt, request, duration, expiresAt, admissionUntil, min(maximum, requestedMaximum), "kubernetes.logs")
}

// openReadHTTP opens one internally constructed API read, with a bounded unary handshake and an independent worker lifetime.
// Both callers construct their closed route before this helper; no URL or authorization header comes from a prompt.
// openReadHTTP открывает один внутренне построенный API read с bounded unary handshake и независимым worker lifetime.
// Оба callers заранее строят закрытый route; URL либо authorization header из prompt не поступают.
func (credential Credential) openReadHTTP(lifecycle, attempt context.Context, request *http.Request, duration time.Duration, expiresAt, admissionUntil time.Time, maximum uint64, operation string) (*nativeReadHTTP, error) {
	now := time.Now()
	if lifecycle == nil || attempt == nil || request == nil || request.Method != http.MethodGet || request.Body != nil || lifecycle.Err() != nil || attempt.Err() != nil || !admissionUntil.After(now) || admissionUntil.After(expiresAt) || !expiresAt.After(now) || expiresAt.Sub(now) > maximumNativeDuration || expiresAt.After(now.Add(duration)) || maximum == 0 || maximum > managedkubernetes.MaximumStreamBytes || operation != "kubernetes.logs" && !watchOperation(operation) {
		return nil, errNativeStreamUnknown
	}
	work, cancel := context.WithDeadline(lifecycle, expiresAt)
	request = request.WithContext(work)
	client, closeIdle, err := credential.NewClient(duration)
	if err != nil {
		cancel()
		return nil, errNativeStreamUnknown
	}
	wrapper, ok := client.Transport.(*credentialTransport)
	if !ok {
		cancel()
		closeIdle()
		return nil, errNativeStreamUnknown
	}
	base, ok := wrapper.base.(*http.Transport)
	if !ok {
		cancel()
		closeIdle()
		return nil, errNativeStreamUnknown
	}
	base.MaxResponseHeaderBytes = 64 << 10
	base.DisableCompression = true
	opening, stopOpening := context.WithDeadline(attempt, admissionUntil)
	defer stopOpening()
	joined := make(chan struct{})
	stop := context.AfterFunc(opening, func() { defer close(joined); cancel() })
	response, err := client.Do(request)
	if !stop() {
		<-joined
	}
	validMedia := true
	if watchOperation(operation) && response != nil {
		mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		validMedia = mediaErr == nil && mediaType == "application/json"
	}
	if err != nil || !validMedia || opening.Err() != nil || work.Err() != nil || !admissionUntil.After(time.Now()) || response == nil || response.StatusCode != http.StatusOK || response.Body == nil || response.TLS == nil || len(response.TLS.PeerCertificates) == 0 || response.Header.Get("Content-Encoding") != "" {
		cancel()
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		closeIdle()
		return nil, errNativeStreamUnknown
	}
	peer := sha256.Sum256(response.TLS.PeerCertificates[0].RawSubjectPublicKeyInfo)
	return &nativeReadHTTP{ctx: work, cancel: cancel, body: response.Body, closeIdle: closeIdle, maximum: maximum, operation: operation, targetRef: credential.target.ID, targetRevision: int64(credential.target.Revision), peerDigest: hex.EncodeToString(peer[:])}, nil
}

// ReadStream emits bounded opaque log bytes, distinguishing a clean HTTP EOF from expiry or truncated transport.
// ReadStream выдаёт bounded opaque log bytes, отличая clean HTTP EOF от expiry либо truncated transport.
func (stream *nativeReadHTTP) ReadStream() (value.KubernetesStreamRead, error) {
	stream.readMu.Lock()
	defer stream.readMu.Unlock()
	if stream.ctx.Err() != nil {
		return value.KubernetesStreamRead{}, errNativeStreamUnknown
	}
	if stream.eof {
		_ = stream.Close()
		return value.KubernetesStreamRead{CleanEOF: true}, nil
	}
	var buffer [managedkubernetes.MaximumFrameBytes]byte
	defer clear(buffer[:])
	limit := min(uint64(len(buffer)), stream.maximum-stream.spent+1)
	n, err := stream.body.Read(buffer[:limit])
	if stream.ctx.Err() != nil {
		err = errNativeStreamUnknown
	}
	if uint64(n) > stream.maximum-stream.spent {
		_ = stream.Close()
		return value.KubernetesStreamRead{}, errNativeStreamLimit
	}
	stream.spent += uint64(n)
	if err == io.EOF {
		stream.eof = true
		err = nil
	}
	result := value.KubernetesStreamRead{}
	if n > 0 {
		result.Channel = "STDOUT"
		result.Payload = append([]byte(nil), buffer[:n]...)
	}
	if err != nil {
		_ = stream.Close()
		return result, errNativeStreamUnknown
	}
	if stream.eof && n == 0 {
		_ = stream.Close()
		result.CleanEOF = true
	}
	return result, nil
}

// WriteFrame rejects input for the read-only Pod logs transport before any I/O.
// WriteFrame отклоняет input для read-only Pod logs transport до любого I/O.
func (*nativeReadHTTP) WriteFrame(context.Context, value.KubernetesStreamFrame) error {
	return errNativeStreamUnknown
}

// HandshakeReceipt binds the observed API TLS response to the original API read operation and target revision.
// HandshakeReceipt связывает observed API TLS response с original API read operation и target revision.
func (stream *nativeReadHTTP) HandshakeReceipt(origin managedkubernetes.StreamReceiptOrigin) (managedkubernetes.HandshakeReceipt, error) {
	receipt := managedkubernetes.HandshakeReceipt{Origin: origin, TransportRef: stream.targetRef, TransportGeneration: stream.targetRevision, Protocol: managedkubernetes.HTTPLogsV1, HTTPStatus: 200, PeerSPKISHA256: stream.peerDigest}
	if watchOperation(stream.operation) {
		receipt.Protocol = managedkubernetes.HTTPWatchV1
	}
	if origin.Operation != stream.operation || origin.TargetRef != stream.targetRef || origin.TargetRevision != stream.targetRevision || receipt.Validate() != nil {
		return managedkubernetes.HandshakeReceipt{}, errNativeStreamUnknown
	}
	return receipt, nil
}

// Close cancels the exact response and closes its private client; the service joins its reader before releasing capacity.
// Close отменяет exact response и закрывает private client; service присоединяет reader до освобождения capacity.
func (stream *nativeReadHTTP) Close() error {
	if stream != nil {
		stream.closeOnce.Do(func() { stream.cancel(); _ = stream.body.Close(); stream.closeIdle() })
	}
	return nil
}

// String hides the live response and private log contents.
// String скрывает live response и private log contents.
func (*nativeReadHTTP) String() string { return "[managed Kubernetes native API read transport]" }

// GoString preserves redaction in detailed formatting.
// GoString сохраняет redaction при подробном форматировании.
func (*nativeReadHTTP) GoString() string { return "[managed Kubernetes native API read transport]" }
