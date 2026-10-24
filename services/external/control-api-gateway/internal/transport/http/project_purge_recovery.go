package httptransport

import (
	"net/http"
	"strconv"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// GetProjectPurgeRecovery reads the owner recovery snapshot for one project under no-store and
// delegates identity/version/digest checks to the common recovery writer without restarting
// deletion.
// GetProjectPurgeRecovery читает recovery снимок владельца одного проекта под no-store и поручает
// проверки identity/version/digest общему writer без перезапуска удаления.
func (s *Server) GetProjectPurgeRecovery(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef) {
	w.Header().Set("Cache-Control", "no-store")
	response, err := s.control.Query.GetProjectPurgeRecovery(r.Context(), &cp.GetProjectPurgeRecoveryRequest{ProjectRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeProjectPurgeRecovery(w, http.StatusOK, ref, response.GetRecovery())
}

// ResolveProjectPurgeRecovery requires explicit acknowledgement of unknown outcome, the snapshot
// digest and a positive expected recovery version before delegating resolution. HTTP 202 reports
// accepted recovery work, not completed purge or restoration of deleted content.
// ResolveProjectPurgeRecovery требует явное подтверждение неизвестного исхода, digest снимка и
// положительную ожидаемую recovery версию до решения владельца. HTTP 202 означает принятую работу
// recovery, а не завершённый purge или восстановление удалённого содержимого.
func (s *Server) ResolveProjectPurgeRecovery(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.ResolveProjectPurgeRecoveryParams) {
	w.Header().Set("Cache-Control", "no-store")
	body, ok := decodeJSON[generated.ProjectPurgeRecoveryResolutionInput](w, r)
	if !ok {
		return
	}
	if !bool(body.AcknowledgeUnknownOutcome) || !validManagedDigest(body.SnapshotDigest) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok || mutation.GetExpectedVersion() < 1 || mutation.GetExpectedVersion() > maximumSafeJSONInteger || p.IdempotencyKey == "" {
		if ok {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		}
		return
	}
	response, err := s.control.Command.ResolveProjectPurgeRecovery(r.Context(), &cp.ResolveProjectPurgeRecoveryRequest{
		Mutation: mutation, ProjectRef: ref, SnapshotDigest: body.SnapshotDigest, AcknowledgeUnknownOutcome: bool(body.AcknowledgeUnknownOutcome),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeProjectPurgeRecovery(w, http.StatusAccepted, ref, response.GetRecovery())
}

// writeProjectPurgeRecovery validates the owner snapshot against the requested project and emits
// its authoritative recovery-version ETag with the caller-selected status. Invalid owner data
// becomes a safe 502.
// writeProjectPurgeRecovery проверяет снимок владельца по запрошенному проекту и выдаёт ETag
// авторитетной recovery версии со статусом caller. Неверные данные владельца дают безопасный 502.
func writeProjectPurgeRecovery(w http.ResponseWriter, status int, ref string, v *cp.ProjectPurgeRecovery) {
	view, ok := projectPurgeRecoveryView(ref, v)
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(view.Version, 10)))
	writeJSON(w, status, view)
}

// projectPurgeRecoveryView binds recovery identity/digest/version to the requested project and
// checks nonnegative pending/unknown counts, project-presence/version consistency and resolvable
// WAITING_OWNER state. It preserves unknown content rather than interpreting it as successful
// deletion.
// projectPurgeRecoveryView связывает recovery identity/digest/version с проектом и проверяет
// неотрицательные pending/unknown счётчики, согласованность наличия/версии проекта и разрешимый
// WAITING_OWNER. Неизвестное содержимое сохраняется без объявления успешного удаления.
func projectPurgeRecoveryView(ref string, v *cp.ProjectPurgeRecovery) (generated.ProjectPurgeRecovery, bool) {
	result := generated.ProjectPurgeRecovery{}
	if v == nil || v.GetProjectRef() != ref || !validManagedVersion(v.GetVersion()) || !validManagedDigest(v.GetSnapshotDigest()) ||
		v.GetUnknownContentCount() < 0 || v.GetCleanupPendingCount() < 0 || v.GetProjectVersion() < 0 || v.GetProjectVersion() > maximumSafeJSONInteger ||
		v.GetProjectPresent() != (v.GetProjectVersion() > 0) {
		return result, false
	}
	state := generated.ProjectPurgeRecoveryState(v.GetState())
	if !state.Valid() || v.GetCanResolve() && (state != "WAITING_OWNER" || v.GetUnknownContentCount() == 0 && v.GetCleanupPendingCount() == 0) ||
		v.GetResolutionRef() != "" && (!opaqueHTTPReference.MatchString(v.GetResolutionRef()) || len(v.GetResolutionRef()) > 96) {
		return result, false
	}
	return generated.ProjectPurgeRecovery{
		ProjectRef: ref, Version: v.GetVersion(), SnapshotDigest: v.GetSnapshotDigest(), State: state,
		UnknownContentCount: v.GetUnknownContentCount(), CleanupPendingCount: v.GetCleanupPendingCount(),
		ProjectPresent: v.GetProjectPresent(), CanResolve: v.GetCanResolve(), ResolutionRef: v.GetResolutionRef(), ProjectVersion: v.GetProjectVersion(),
	}, true
}
