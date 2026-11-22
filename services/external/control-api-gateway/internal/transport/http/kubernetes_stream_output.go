package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// ReadManagedKubernetesStreamOutput forwards an exact original-user cursor through the protected owner client; it never acknowledges data or retries a native operation.
// ReadManagedKubernetesStreamOutput передаёт exact original-user cursor через protected owner client; не подтверждает data и не повторяет native operation.
func (server *Server) ReadManagedKubernetesStreamOutput(w http.ResponseWriter, r *http.Request, invocationRef generated.IntegrationInvocationRef, streamRef generated.KubernetesStreamInputRef, params generated.ReadManagedKubernetesStreamOutputParams) {
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	generation, generationErr := strconv.ParseInt(params.Generation, 10, 64)
	after, sequenceErr := strconv.ParseUint(params.AfterSequence, 10, 63)
	if parseErr != nil || len(values) != 2 || len(values["generation"]) != 1 || len(values["afterSequence"]) != 1 || generationErr != nil || generation < 1 || strconv.FormatInt(generation, 10) != params.Generation || sequenceErr != nil || strconv.FormatUint(after, 10) != params.AfterSequence || !validKubernetesInputHTTPRef(invocationRef, "inv_") || !validKubernetesInputHTTPRef(streamRef, "kst_") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if server.control == nil || server.control.KubernetesOutput == nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return
	}
	response, err := server.control.KubernetesOutput.ReadManagedKubernetesStreamOutput(r.Context(), &cp.ReadManagedKubernetesStreamOutputRequest{InvocationRef: invocationRef, StreamRef: streamRef, Generation: generation, AfterSequence: after})
	if response != nil && response.Frame != nil {
		defer clear(response.Frame.Payload)
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, valid := kubernetesOutputPublicView(response, invocationRef, streamRef, generation, after, time.Now())
	if !valid || r.Context().Err() != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeKubernetesOutputBeforeDeadline(w, r, view)
}

// writeKubernetesOutputBeforeDeadline bounds native writes and flushing by the original owner disclosure window and request lifetime; buffered success never acknowledges delivery.
// writeKubernetesOutputBeforeDeadline ограничивает native write и flush исходным сроком выдачи владельца и временем запроса; успешная буферизация не подтверждает доставку.
func writeKubernetesOutputBeforeDeadline(w http.ResponseWriter, r *http.Request, view generated.KubernetesStreamOutputReadback) {
	body, err := json.Marshal(view)
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	defer clear(body)
	deadline := view.AuthorizedUntil
	if requestDeadline, ok := r.Context().Deadline(); ok && requestDeadline.Before(deadline) {
		deadline = requestDeadline
	}
	if !time.Now().Before(deadline) || r.Context().Err() != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	controller := http.NewResponseController(w)
	if controller.SetWriteDeadline(deadline) != nil {
		writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", false)
		return
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(r.Context(), func() {
		defer close(joined)
		_ = controller.SetWriteDeadline(time.Now())
	})
	var joinOnce sync.Once
	joinCancel := func() {
		joinOnce.Do(func() {
			if !stop() {
				<-joined
			}
		})
	}
	defer joinCancel()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	n, writeErr := w.Write(body)
	if writeErr != nil || n != len(body) || controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
	joinCancel()
	if !time.Now().Before(deadline) || r.Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}
	// Reset only after a successful bounded flush; a failed response is aborted with its deadline intact.
	// Сбрасываем срок только после успешного ограниченного flush; неудачный ответ прерывается с сохранённым сроком.
	if controller.SetWriteDeadline(time.Time{}) != nil {
		panic(http.ErrAbortHandler)
	}
}

// kubernetesOutputPublicView validates untrusted owner wire metadata and bytes before emitting a secret-free locator projection; integer coordinates remain lossless decimal strings.
// kubernetesOutputPublicView проверяет owner wire metadata и bytes до выдачи projection без private locators; integer coordinates остаются decimal strings без потери точности.
func kubernetesOutputPublicView(raw *cp.ReadManagedKubernetesStreamOutputResponse, invocation, stream string, generation int64, after uint64, now time.Time) (generated.KubernetesStreamOutputReadback, bool) {
	var zero generated.KubernetesStreamOutputReadback
	if raw == nil || len(raw.ProtoReflect().GetUnknown()) != 0 || raw.InvocationRef != invocation || raw.StreamRef != stream || raw.Generation != generation || raw.AfterSequence != after || raw.OutputWatermark < after || raw.OutputWatermark > managedkubernetes.MaximumFrameSequence || raw.DroppedBytes > managedkubernetes.MaximumStreamBytes || raw.TransportJoined && !raw.NativeTerminalKnown || raw.AuthorizedUntil == nil || raw.AuthorizedUntil.CheckValid() != nil || len(raw.AuthorizedUntil.ProtoReflect().GetUnknown()) != 0 {
		return zero, false
	}
	until := raw.AuthorizedUntil.AsTime()
	if !until.After(now) || until.After(now.Add(5*time.Second)) {
		return zero, false
	}
	status := ""
	switch raw.Status {
	case cp.ManagedKubernetesStreamOutputStatus_MANAGED_KUBERNETES_STREAM_OUTPUT_STATUS_WAITING:
		status = "WAITING"
	case cp.ManagedKubernetesStreamOutputStatus_MANAGED_KUBERNETES_STREAM_OUTPUT_STATUS_INCOMPLETE:
		status = "INCOMPLETE"
	case cp.ManagedKubernetesStreamOutputStatus_MANAGED_KUBERNETES_STREAM_OUTPUT_STATUS_ENDED:
		if after != raw.OutputWatermark || raw.DroppedBytes != 0 || !raw.NativeTerminalKnown || !raw.TransportJoined {
			return zero, false
		}
		status = "ENDED"
	case cp.ManagedKubernetesStreamOutputStatus_MANAGED_KUBERNETES_STREAM_OUTPUT_STATUS_FRAME:
		status = "FRAME"
	default:
		return zero, false
	}
	view := generated.KubernetesStreamOutputReadback{InvocationRef: invocation, StreamRef: stream, Generation: strconv.FormatInt(generation, 10), AfterSequence: strconv.FormatUint(after, 10), OutputWatermark: strconv.FormatUint(raw.OutputWatermark, 10), DroppedBytes: strconv.FormatUint(raw.DroppedBytes, 10), Status: generated.KubernetesStreamOutputReadbackStatus(status), AuthorizedUntil: until, NativeTerminalKnown: raw.NativeTerminalKnown, TransportJoined: raw.TransportJoined}
	if status != "FRAME" {
		return view, raw.Frame == nil
	}
	f := raw.Frame
	if f == nil || len(f.ProtoReflect().GetUnknown()) != 0 || !validKubernetesInputHTTPRef(f.StageRef, "kof_") || f.Sequence != after+1 || f.Sequence > raw.OutputWatermark || len(f.Payload) == 0 || len(f.Payload) > managedkubernetes.MaximumFrameBytes || !managedkubernetes.ValidDigest(f.PayloadSha256) {
		return zero, false
	}
	switch f.Channel {
	case "STDOUT", "STDERR", "CONTROL", "PORT_FORWARD", "FILE_CONTENT":
	default:
		return zero, false
	}
	digest := sha256.Sum256(f.Payload)
	if hex.EncodeToString(digest[:]) != f.PayloadSha256 {
		return zero, false
	}
	view.Frame = &generated.KubernetesStreamOutputFrame{StageRef: f.StageRef, Sequence: strconv.FormatUint(f.Sequence, 10), Channel: generated.KubernetesStreamOutputFrameChannel(f.Channel), PayloadDigest: f.PayloadSha256, PayloadBase64: f.Payload}
	return view, true
}
