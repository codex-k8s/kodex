package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	privatehttp "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// executeResourceRequest sends one fresh HTTP/1 request after owner admission. A fresh transport,
// no replayable body and no keepalive prevent implicit retries, including HTTP/2 stream retries.
// Provider diagnostics remain private; ambiguous mutation transport/ACK failures become UNKNOWN.
// executeResourceRequest отправляет один fresh HTTP/1 request после owner admission. Новый
// transport без replayable body/keepalive исключает implicit retry, включая HTTP/2 stream retry.
// Provider diagnostics остаются private; ambiguous mutation transport/ACK даёт UNKNOWN.
func (prepared *PreparedResourceOperation) executeResourceRequest(ctx context.Context, authorizedUntil time.Time) (value.ResourceOperationResult, error) {
	failed := value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: managedkubernetes.ReasonResourceUnavailable}
	client, closeIdle, err := prepared.credential.NewClient(time.Until(authorizedUntil))
	if err != nil {
		return failed, errors.New(resourceOperationInvalid)
	}
	defer closeIdle()
	wrapper, ok := client.Transport.(*credentialTransport)
	if !ok {
		return failed, errors.New(resourceOperationInvalid)
	}
	transport, ok := wrapper.base.(*http.Transport)
	if !ok {
		return failed, errors.New(resourceOperationInvalid)
	}
	wrapper.validUntil = authorizedUntil
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	// Schema selection is re-read with the selected identity, never from caller or cache.
	// Schema selection повторно читается selected identity, не из caller либо cache.
	index, err := prepared.credential.DiscoveryRequest(ctx, managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPIIndex})
	if err != nil {
		return failed, errors.New(resourceOperationInvalid)
	}
	rawIndex, status, err := readResourceHTTPDocument(client, index, resourceResponseBytes)
	if err != nil || status != http.StatusOK {
		return failed, errors.New(resourceOperationInvalid)
	}
	document, err := decodeResourceJSON(rawIndex, resourceResponseBytes, 64)
	clear(rawIndex)
	if err != nil {
		return failed, errors.New(resourceOperationInvalid)
	}
	hash, err := resourceSchemaIndexHash(document, prepared.address)
	if err != nil || hash != prepared.schema.immutableHash {
		return value.ResourceOperationResult{Outcome: "FAILED", ReasonCode: managedkubernetes.ReasonScopeDenied}, errors.New(resourceOperationInvalid)
	}
	address := prepared.address
	if prepared.method == http.MethodPost {
		address.Name, address.UID, address.ResourceVersion = "", "", ""
	}
	request, err := prepared.credential.ReadRequest(ctx, address)
	if err != nil || ctx.Err() != nil || !time.Now().Before(authorizedUntil) {
		return failed, errors.New(resourceOperationInvalid)
	}
	request.Method, request.URL.RawQuery = prepared.method, prepared.query.Encode()
	request.Header.Set("User-Agent", "kodex-managed-kubernetes/1")
	if len(prepared.body) != 0 {
		request.Body = privatehttp.NewPrivateResourceRequestBody(prepared.body)
		request.ContentLength = int64(len(prepared.body))
		prepared.body = nil
		defer request.Body.Close()
		request.Header.Set("Content-Type", prepared.contentType)
	}
	request.GetBody = nil
	result := value.ResourceOperationResult{Outcome: "UNKNOWN", ReasonCode: managedkubernetes.ReasonTransportUnknown, Attempted: true}
	if prepared.operation.Risk == "READ" {
		result.Outcome, result.ReasonCode = "FAILED", managedkubernetes.ReasonTargetUnavailable
	}
	response, err := client.Do(request)
	if err != nil || response == nil || response.Body == nil {
		return result, errors.New(resourceOperationInvalid)
	}
	defer response.Body.Close()
	if reason := resourceRejectedStatusReason(response.StatusCode); reason != "" {
		result.Outcome, result.ReasonCode = "FAILED", reason
		return result, nil
	}
	if !resourceResponseStatusAllowed(prepared.method, response.StatusCode) {
		if response.StatusCode < 500 {
			return invalidResourceProjection(result)
		}
		return result, errors.New(resourceOperationInvalid)
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" && !strings.HasSuffix(media, "+json") {
		return invalidResourceProjection(result)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, resourceResponseBytes+1))
	defer clear(raw)
	if err != nil || ctx.Err() != nil || len(raw) > resourceResponseBytes {
		if ctx.Err() == nil {
			return invalidResourceProjection(result)
		}
		return result, errors.New(resourceOperationInvalid)
	}
	safe, uid, revision, err := prepared.projectResourceResponse(raw)
	if err != nil {
		return invalidResourceProjection(result)
	}
	return confirmResourceProjection(result, safe, uid, revision)
}

// resourceRejectedStatusReason maps only explicit remote refusals, never raw Status messages.
// resourceRejectedStatusReason отображает лишь explicit remote refusals, не raw Status messages.
func resourceRejectedStatusReason(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return managedkubernetes.ReasonAccessDenied
	case http.StatusNotFound:
		return managedkubernetes.ReasonNotFound
	case http.StatusConflict:
		return managedkubernetes.ReasonConflict
	case http.StatusUnprocessableEntity:
		return managedkubernetes.ReasonResourceUnavailable
	default:
		return ""
	}
}

// invalidResourceProjection preserves uncertainty after writes and never returns malformed data.
// invalidResourceProjection сохраняет uncertainty после writes, не возвращая malformed data.
func invalidResourceProjection(result value.ResourceOperationResult) (value.ResourceOperationResult, error) {
	clear(result.SafeJSON)
	result.SafeJSON = nil
	result.ReasonCode = managedkubernetes.ReasonResponseInvalid
	return result, errors.New(resourceOperationInvalid)
}

// confirmResourceProjection validates actual returned bytes with the same strict owner receipt
// validator before assigning SUCCEEDED. Lower provider decoding permits deeper source objects;
// an incompatible safe projection becomes UNKNOWN after write or FAILED after READ, never data.
// confirmResourceProjection проверяет actual returned bytes тем же strict owner receipt
// validator до назначения SUCCEEDED. Lower provider decoder допускает deeper source objects;
// несовместимый safe projection даёт UNKNOWN после write либо FAILED после READ, без данных.
func confirmResourceProjection(result value.ResourceOperationResult, safe []byte, uid, revision string) (value.ResourceOperationResult, error) {
	sum := sha256.Sum256(safe)
	if managedkubernetes.ValidateEffectResult(safe, hex.EncodeToString(sum[:]), "SUCCEEDED", "") != nil {
		clear(safe)
		return invalidResourceProjection(result)
	}
	result.Outcome, result.ReasonCode, result.SafeJSON = "SUCCEEDED", "", safe
	result.ObservedUID, result.ResourceVersion = uid, revision
	return result, nil
}

// resourceResponseStatusAllowed accepts only a registered method's concrete success statuses.
// resourceResponseStatusAllowed принимает только concrete success statuses registered method.
func resourceResponseStatusAllowed(method string, status int) bool {
	switch method {
	case http.MethodPost:
		return status == http.StatusCreated
	case http.MethodDelete:
		return status == http.StatusOK || status == http.StatusAccepted
	default:
		return status == http.StatusOK
	}
}

// marshalResourceSafeResult bounds the final owner-credit candidate without truncation.
// marshalResourceSafeResult ограничивает final owner-credit candidate без усечения.
func marshalResourceSafeResult(object any) ([]byte, error) {
	raw, err := json.Marshal(object)
	if err != nil || len(raw) > maximumSafeProjectionBytes {
		clear(raw)
		return nil, errors.New(resourceOperationInvalid)
	}
	return raw, nil
}
