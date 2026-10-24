package httpserver

import "net/http"

// ProbeMux registers the existing method-agnostic health/live/startup probes and snapshot-ready
// response, including its unready reason. Callers provide a nonnil local snapshot without I/O and
// own server budgets, extra routes, listeners and shutdown; this does not use New's strict GET policy.
// ProbeMux регистрирует прежние health/live/startup probes для всех методов и snapshot-ready ответ,
// включая причину неготовности. Caller передаёт непустой локальный snapshot без I/O и владеет server
// бюджетами, дополнительными routes, listener и shutdown; строгая GET policy New здесь не применяется.
func ProbeMux(snapshot func() (bool, string)) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/startupz", func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		ready, reason := snapshot()
		if !ready {
			http.Error(response, reason, http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	return mux
}
