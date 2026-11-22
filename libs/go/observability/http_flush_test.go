package observability

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// failedFlushWriter returns an exact synthetic transport failure without retaining response bytes.
// failedFlushWriter возвращает точную синтетическую transport ошибку без удержания байтов ответа.
type failedFlushWriter struct {
	*httptest.ResponseRecorder
	failure error
}

// FlushError exposes the transport failure to the standard response controller.
// FlushError передаёт transport ошибку стандартному response controller.
func (writer *failedFlushWriter) FlushError() error { return writer.failure }

// unwrapFlushWriter exercises capability discovery through an intermediate wrapper that only exposes Unwrap.
// unwrapFlushWriter проверяет поиск возможностей через промежуточную обёртку только с Unwrap.
type unwrapFlushWriter struct{ http.ResponseWriter }

// Unwrap preserves access to the original transport without masking its flush result.
// Unwrap сохраняет доступ к исходному transport без скрытия результата flush.
func (writer unwrapFlushWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// TestHTTPStatusWriterPreservesFlushError rejects treating a failed native flush as successful delivery through telemetry and Unwrap layers.
// TestHTTPStatusWriterPreservesFlushError не допускает трактовку неудачного native flush как успешной доставки через telemetry и Unwrap.
func TestHTTPStatusWriterPreservesFlushError(t *testing.T) {
	want := errors.New("synthetic transport failure")
	base := &failedFlushWriter{ResponseRecorder: httptest.NewRecorder(), failure: want}
	writer := &httpStatusWriter{ResponseWriter: unwrapFlushWriter{base}}
	if err := http.NewResponseController(writer).Flush(); !errors.Is(err, want) || writer.status != http.StatusOK {
		t.Fatal("flush error or implicit status was lost")
	}
}
