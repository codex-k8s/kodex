package codex

import (
	"crypto/subtle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"io"
	"net/http"
)

// invalidModelMeasurementMessage keeps malformed private payload diagnostics fixed and secret-free.
// invalidModelMeasurementMessage сохраняет фиксированную несекретную диагностику неверного private payload.
const invalidModelMeasurementMessage = "invalid model measurement"

// serveModelMeasurement accepts only the private provider bridge bearer and exact immutable revision, then forwards a bounded typed observation.
// It neither proxies arbitrary URLs nor accepts execution identity from the body; owner validation remains downstream.
// serveModelMeasurement принимает лишь bearer закрытого provider bridge и точную immutable revision, затем передаёт ограниченное typed измерение.
// Не проксирует произвольные URL и не принимает execution identity из body; owner validation остаётся downstream.
func serveModelMeasurement(writer http.ResponseWriter, request *http.Request, token, revision string, observe func(runtimecontract.RuntimeActivity) error) {
	if observe == nil || len(token) != 64 || len(revision) != 64 || request.Method != http.MethodPost || request.URL.Path != "/model-measurements" || request.URL.RawPath != "" || request.URL.RawQuery != "" || request.URL.ForceQuery || request.Header.Get("Content-Type") != "application/json" || subtle.ConstantTimeCompare([]byte(request.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 || request.Header.Get("X-Kodex-Runtime-Revision") != revision {
		http.Error(writer, "invalid model measurement binding", http.StatusBadRequest)
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, runtimecontract.MaximumModelMeasurementBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, invalidModelMeasurementMessage, http.StatusBadRequest)
		return
	}
	measurement, err := runtimecontract.DecodeModelMeasurement(raw)
	if err != nil || measurement.Source != "HTTP_REQUEST" {
		http.Error(writer, invalidModelMeasurementMessage, http.StatusBadRequest)
		return
	}
	if observe(runtimecontract.RuntimeActivity{ModelMeasurement: &measurement}) != nil {
		http.Error(writer, "model measurement delivery failed", http.StatusServiceUnavailable)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
