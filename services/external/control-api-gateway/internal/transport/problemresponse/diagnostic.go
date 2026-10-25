package problemresponse

import (
	"net/http"

	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/availability"
)

// failureObserver is an optional request-local metadata sink; it never receives the original error or request data.
// failureObserver — необязательный приёмник метаданных запроса; исходные ошибки и данные запроса ему не передаются.
type failureObserver interface{ ObserveFailure(string, string) }

// problemObserver joins one public problem correlation with the request-local diagnostic sink.
// problemObserver связывает correlation публичной ошибки с диагностическим приёмником запроса.
type problemObserver interface{ ObserveProblem(int, string, string) }

// MarkFailure passes closed metadata through a bounded standard writer chain without logging or changing the response.
// MarkFailure передаёт закрытые метаданные по ограниченной цепочке writer без логирования и изменения ответа.
func MarkFailure(writer http.ResponseWriter, err error) {
	stage, class := availability.Describe(err)
	for range 16 {
		if observer, ok := writer.(failureObserver); ok {
			observer.ObserveFailure(stage, class)
			return
		}
		next, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return
		}
		writer = next.Unwrap()
	}
}

// observeProblem forwards only server-assigned correlation and status, retaining the same bounded wrapper traversal.
// observeProblem передаёт только назначенный сервером correlation и статус с тем же ограничением обхода wrappers.
func observeProblem(writer http.ResponseWriter, status int, code, correlation string) {
	for range 16 {
		if observer, ok := writer.(problemObserver); ok {
			observer.ObserveProblem(status, code, correlation)
			return
		}
		next, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return
		}
		writer = next.Unwrap()
	}
}
