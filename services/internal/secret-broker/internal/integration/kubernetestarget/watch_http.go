package kubernetestarget

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// watchOperation recognizes only the two registered collection watch capabilities.
// watchOperation распознаёт только две зарегистрированные collection watch capabilities.
func watchOperation(operation string) bool {
	return operation == "kubernetes.watch" || operation == "kubernetes.watch.all-namespaces"
}

// watchRequest builds the exact collection GET from typed coordinates and pinned ListOptions semantics.
// Initial events require NotOlderThan; pagination and caller-selected transport options are absent.
// watchRequest строит exact collection GET из typed coordinates по закреплённой семантике ListOptions.
// Initial events требуют NotOlderThan; pagination и caller-selected transport options отсутствуют.
func (credential Credential) watchRequest(ctx context.Context, operation string, input managedkubernetes.OperationInput, address ResourceAddress) (*http.Request, time.Duration, error) {
	raw, err := managedkubernetes.EncodeOperationInput(operation, input)
	defer clear(raw)
	if err != nil || !watchOperation(operation) || address.Validate() != nil || address.Name != "" || address.UID != "" || address.ResourceVersion != "" || address.Subresource != "" || address.AllNamespaces != (operation == "kubernetes.watch.all-namespaces") || !resourceInputAddressMatches(input, address) {
		return nil, 0, errNativeStreamUnknown
	}
	options, err := resourceOperationOptions(input)
	if err != nil {
		return nil, 0, errNativeStreamUnknown
	}
	seconds, ok := resourceInteger(options["timeoutSeconds"])
	if !ok || seconds < 1 || seconds > 300 {
		return nil, 0, errNativeStreamUnknown
	}
	request, err := credential.ReadRequest(ctx, address)
	if err != nil {
		return nil, 0, errNativeStreamUnknown
	}
	query := request.URL.Query()
	query.Set("watch", "true")
	query.Set("timeoutSeconds", strconv.FormatInt(seconds, 10))
	if input.Selector != "" {
		query.Set("labelSelector", input.Selector)
	}
	if version, ok := options["resourceVersion"].(string); ok {
		query.Set("resourceVersion", version)
	}
	if selector, ok := options["fieldSelector"].(string); ok {
		query.Set("fieldSelector", selector)
	}
	for _, key := range []string{"allowWatchBookmarks", "sendInitialEvents"} {
		if option, present := options[key].(bool); present {
			query.Set(key, strconv.FormatBool(option))
			if key == "sendInitialEvents" {
				query.Set("resourceVersionMatch", "NotOlderThan")
			}
		}
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Accept", "application/json")
	return request, time.Duration(seconds) * time.Second, nil
}

// nativeWatchHTTP decodes bounded private JSON events before any frame can leave the broker.
// It retains at most one raw event and one sanitized event; it never reconnects or resumes by RV.
// nativeWatchHTTP разбирает bounded private JSON events до выхода любого frame из broker.
// Хранятся не более одного raw event и одного sanitized event; reconnect либо resume по RV отсутствуют.
type nativeWatchHTTP struct {
	*nativeReadHTTP
	decoder        *json.Decoder
	frame, wire    *io.LimitedReader
	previousOffset int64
	pending        []byte
	address        ResourceAddress
	schema         *registeredResourceSchema
	protected      []string
}

// openWatchHTTP shares the exact private TLS and cancellation lifetime with logs, but never exposes its raw read method.
// Schema and protected scope come from owner-bound discovery, not from untrusted event metadata.
// openWatchHTTP использует exact private TLS и cancellation lifetime logs, но не выдаёт его raw read method.
// Schema и protected scope получены из owner-bound discovery, а не недоверенных event metadata.
func (credential Credential) openWatchHTTP(lifecycle, attempt context.Context, operation string, input managedkubernetes.OperationInput, address ResourceAddress, schema *registeredResourceSchema, protected []string, expiresAt, admissionUntil time.Time, maximum uint64) (*nativeWatchHTTP, error) {
	if schema == nil || schema.compiled == nil || !managedkubernetes.ValidDigest(schema.digest) {
		return nil, errNativeStreamUnknown
	}
	request, duration, err := credential.watchRequest(lifecycle, operation, input, address)
	if err != nil {
		return nil, err
	}
	base, err := credential.openReadHTTP(lifecycle, attempt, request, duration, expiresAt, admissionUntil, maximum, operation)
	if err != nil {
		return nil, err
	}
	wire := &io.LimitedReader{R: base.body, N: int64(maximum) + 1}
	frame := &io.LimitedReader{R: wire, N: resourceResponseBytes + 1}
	return &nativeWatchHTTP{nativeReadHTTP: base, decoder: json.NewDecoder(frame), frame: frame, wire: wire, address: address, schema: schema, protected: append([]string(nil), protected...)}, nil
}

// ReadStream emits only validated, projected event bytes through the existing bounded frame ledger.
// Raw and emitted bytes both consume finite budgets; filtered objects do not bypass the raw limit.
// ReadStream выдаёт только проверенные projected event bytes через existing bounded frame ledger.
// Raw и emitted bytes расходуют конечные budgets; filtered objects не обходят raw limit.
func (stream *nativeWatchHTTP) ReadStream() (value.KubernetesStreamRead, error) {
	stream.readMu.Lock()
	defer stream.readMu.Unlock()
	for len(stream.pending) == 0 {
		if stream.ctx.Err() != nil {
			return value.KubernetesStreamRead{}, errNativeStreamUnknown
		}
		buffered, err := io.Copy(io.Discard, stream.decoder.Buffered())
		if err != nil || buffered > resourceResponseBytes {
			return stream.failed(errNativeStreamLimit)
		}
		stream.frame.N = resourceResponseBytes + 1 - buffered
		var raw json.RawMessage
		err = stream.decoder.Decode(&raw)
		size := stream.decoder.InputOffset() - stream.previousOffset
		stream.previousOffset = stream.decoder.InputOffset()
		if stream.ctx.Err() != nil {
			clear(raw)
			return stream.failed(errNativeStreamUnknown)
		}
		if stream.wire.N <= 0 || size > resourceResponseBytes {
			clear(raw)
			return stream.failed(errNativeStreamLimit)
		}
		if err == io.EOF {
			clear(raw)
			_ = stream.Close()
			return value.KubernetesStreamRead{CleanEOF: true}, nil
		}
		if err != nil {
			clear(raw)
			return stream.failed(errNativeStreamUnknown)
		}
		safe, err := stream.projectEvent(raw)
		clear(raw)
		if err != nil {
			clear(safe)
			return stream.failed(err)
		}
		if len(safe) == 0 {
			continue
		}
		safe = append(safe, '\n')
		if uint64(len(safe)) > stream.maximum-stream.spent {
			clear(safe)
			return stream.failed(errNativeStreamLimit)
		}
		stream.pending = safe
	}
	if stream.ctx.Err() != nil {
		return stream.failed(errNativeStreamUnknown)
	}
	n := min(len(stream.pending), managedkubernetes.MaximumFrameBytes)
	result := value.KubernetesStreamRead{Channel: "STDOUT", Payload: append([]byte(nil), stream.pending[:n]...)}
	clear(stream.pending[:n])
	stream.pending = stream.pending[n:]
	stream.spent += uint64(n)
	return result, nil
}

// failed clears a queued safe event and cancels the only connection without exposing upstream diagnostics.
// failed очищает queued safe event и отменяет единственное соединение без выдачи upstream diagnostics.
func (stream *nativeWatchHTTP) failed(err error) (value.KubernetesStreamRead, error) {
	clear(stream.pending)
	stream.pending = nil
	_ = stream.Close()
	return value.KubernetesStreamRead{}, err
}

// projectEvent validates the exact registered GVK and scope before using the shared resource/Secret projection.
// Bookmarks expose only an opaque RV and the standardized initial-events marker; ERROR never becomes successful EOF.
// projectEvent проверяет exact registered GVK и scope перед общей resource/Secret projection.
// Bookmarks выдают лишь opaque RV и штатный initial-events marker; ERROR не становится successful EOF.
func (stream *nativeWatchHTTP) projectEvent(raw []byte) ([]byte, error) {
	envelope, err := decodeResourceJSON(raw, resourceResponseBytes, 64)
	if err != nil || len(envelope) != 2 {
		return nil, errNativeStreamUnknown
	}
	kind, ok := envelope["type"].(string)
	object, objectOK := envelope["object"].(map[string]any)
	if !ok || !objectOK {
		return nil, errNativeStreamUnknown
	}
	metadata, ok := object["metadata"].(map[string]any)
	if !ok || object["apiVersion"] != stream.schema.kind.GroupVersion().String() || object["kind"] != stream.schema.kind.Kind {
		return nil, errNativeStreamUnknown
	}
	if kind == "BOOKMARK" {
		rv, ok := metadata["resourceVersion"].(string)
		if !ok || rv == "" || len(rv) > 256 || !utf8.ValidString(rv) || strings.ContainsAny(rv, " \t\r\n\x00") {
			return nil, errNativeStreamUnknown
		}
		safeMeta := map[string]any{"resourceVersion": rv}
		if annotations, ok := metadata["annotations"].(map[string]any); ok && annotations["k8s.io/initial-events-end"] == "true" {
			safeMeta["annotations"] = map[string]any{"k8s.io/initial-events-end": "true"}
		}
		return marshalResourceSafeResult(map[string]any{"type": kind, "object": map[string]any{"apiVersion": object["apiVersion"], "kind": object["kind"], "metadata": safeMeta}})
	}
	if kind != "ADDED" && kind != "MODIFIED" && kind != "DELETED" || stream.schema.compiled.Validate(object) != nil {
		return nil, errNativeStreamUnknown
	}
	address := stream.address
	address.Name, _ = metadata["name"].(string)
	address.AllNamespaces = false
	if stream.address.AllNamespaces {
		address.Namespace, _ = metadata["namespace"].(string)
	}
	observed, err := resourceObjectIdentity(object, address, stream.schema)
	if err != nil {
		return nil, errNativeStreamUnknown
	}
	if resourceNamespaceProtected(observed, stream.protected) || resourceReadReferencesProtectedNamespace(object, stream.protected) {
		return nil, nil
	}
	safe, err := projectResourceReadObject(object, observed)
	if err != nil {
		return nil, errNativeStreamUnknown
	}
	defer clear(safe)
	return marshalResourceSafeResult(map[string]any{"type": kind, "object": json.RawMessage(safe)})
}

// String hides pending event bytes and private transport coordinates.
// String скрывает pending event bytes и private transport coordinates.
func (*nativeWatchHTTP) String() string { return "[managed Kubernetes native watch transport]" }

// GoString preserves redaction for detailed formatting.
// GoString сохраняет redaction при подробном форматировании.
func (stream *nativeWatchHTTP) GoString() string { return stream.String() }
