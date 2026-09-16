package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/transport/http/casters"
)

const CallerSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway"

type Observer interface{ Record(api.Operation, string) }
type Handler struct {
	RPCProfile string
	Service    *mail.Service
	Current    func() *mail.Service
	Metrics    Observer
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if !h.admit(r) {
		writeError(w, errs.Denied)
		return
	}
	if len(r.Header.Values("Authorization")) != 1 || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		writeError(w, errs.Denied)
		return
	}
	if len(r.Header.Values(api.ExecutionHeader)) != 1 {
		writeError(w, errs.Denied)
		return
	}
	binding, err := api.ParseExecutionHeader(r.Header.Get(api.ExecutionHeader))
	if err != nil || r.Header.Get("Authorization") != "Bearer "+binding.Lease.Fence {
		writeError(w, errs.Denied)
		return
	}
	r = r.WithContext(api.WithExecutionBinding(r.Context(), binding))
	service := h.Service
	if h.Current != nil {
		service = h.Current()
	}
	if service == nil {
		writeError(w, errs.Unavailable)
		return
	}
	cmd, legacy, e := casters.Command(r, service.Config)
	if e != nil {
		writeError(w, e)
		return
	}
	result, e := service.Execute(r.Context(), CallerSPIFFE, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), cmd)
	outcome := "success"
	if e != nil {
		outcome = "error"
	}
	if result.Status == "unknown" {
		outcome = "unknown"
	}
	if h.Metrics != nil {
		h.Metrics.Record(cmd.Operation, outcome)
	}
	if e != nil {
		writeError(w, e)
		return
	}
	if legacy {
		if cmd.Operation == api.OperationHealth {
			_ = json.NewEncoder(w).Encode(api.Health{Status: api.HealthStatus(result.Status)})
			return
		}
		if cmd.Operation == api.OperationSend {
			w.WriteHeader(http.StatusAccepted)
		}
		_ = json.NewEncoder(w).Encode(api.MessageStatus{MessageId: result.MessageId, Status: api.MessageStatusStatus(result.Status)})
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func (h Handler) admit(r *http.Request) bool {
	if h.RPCProfile == transportprofile.TrustedCluster {
		profiles, callers := r.Header.Values("X-Kodex-Rpc-Profile"), r.Header.Values("X-Kodex-Trusted-Caller")
		return len(profiles) == 1 && profiles[0] == transportprofile.TrustedCluster &&
			len(callers) == 1 && callers[0] == CallerSPIFFE && len(r.Header.Values("X-Kodex-Authorization")) == 0
	}
	return h.RPCProfile == "" && len(r.Header.Values("X-Kodex-Rpc-Profile")) == 0 &&
		r.TLS != nil && len(r.TLS.VerifiedChains) > 0 &&
		len(r.TLS.PeerCertificates) > 0 && len(r.TLS.PeerCertificates[0].URIs) == 1 &&
		r.TLS.PeerCertificates[0].URIs[0].String() == CallerSPIFFE
}
func writeError(w http.ResponseWriter, e error) {
	status := http.StatusServiceUnavailable
	code := "UNAVAILABLE"
	for _, v := range []struct {
		err    error
		status int
	}{{errs.Invalid, 400}, {errs.Denied, 403}, {errs.Gate, 403}, {errs.NotFound, 404}, {errs.Conflict, 409}, {errs.Unsupported, 422}} {
		if errors.Is(e, v.err) {
			status = v.status
			code = v.err.Error()
			break
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Code: api.ErrorCode(code)})
}
