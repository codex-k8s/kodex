package httptransport

import (
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
)

func (server *Server) GetRuntimeRevisionDiff(w http.ResponseWriter, r *http.Request, ref generated.RunRef, p generated.GetRuntimeRevisionDiffParams) {
	pin := stringValue(p.CurrentRevisionRef)
	if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 || p.CurrentRevisionRef != nil && (!opaqueHTTPReference.MatchString(pin) || len(pin) > 96) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetRuntimeRevisionDiff(r.Context(), &cp.GetRuntimeRevisionDiffRequest{RunRef: ref, CurrentRevisionRef: pin})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, ok := runtimeRevisionDiffView(response, ref, pin)
	if !ok {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func runtimeRevisionDiffView(response *cp.GetRuntimeRevisionDiffResponse, ref, pin string) (generated.RuntimeRevisionDiff, bool) {
	result := generated.RuntimeRevisionDiff{Changes: []generated.RuntimeRevisionDiffChange{}}
	current, ok := publicRuntimeRevisionIdentity(response.GetCurrent())
	if !ok || current.RunRef != ref || pin != "" && current.Ref != pin || len(response.GetChanges()) > 11 {
		return result, false
	}
	result.Current = current
	if response.GetPrevious() != nil {
		previous, valid := publicRuntimeRevisionIdentity(response.GetPrevious())
		if !valid || previous.SessionRef != current.SessionRef || previous.Ref == current.Ref || previous.CreatedAt.After(current.CreatedAt) || previous.CreatedAt.Equal(current.CreatedAt) && previous.Ref >= current.Ref {
			return result, false
		}
		result.Previous = &previous
	}
	seen := map[cp.RuntimeRevisionDiffComponent]bool{}
	for _, change := range response.GetChanges() {
		if change == nil {
			return result, false
		}
		component := generated.RuntimeRevisionDiffChangeComponent(strings.TrimPrefix(change.GetComponent().String(), "RUNTIME_REVISION_DIFF_COMPONENT_"))
		if !component.Valid() || seen[change.GetComponent()] || (result.Previous != nil) != (change.GetPrevious() != nil) {
			return result, false
		}
		seen[change.GetComponent()] = true
		value, valid := runtimeRevisionDiffValue(component, change.GetCurrent())
		if !valid {
			return result, false
		}
		item := generated.RuntimeRevisionDiffChange{Component: component, Current: value}
		if change.GetPrevious() != nil {
			previous, valid := runtimeRevisionDiffValue(component, change.GetPrevious())
			if !valid || proto.Equal(change.GetCurrent(), change.GetPrevious()) {
				return result, false
			}
			item.Previous = &previous
		}
		result.Changes = append(result.Changes, item)
	}
	return result, true
}

func publicRuntimeRevisionIdentity(v *cp.PublicRuntimeRevisionIdentity) (generated.PublicRuntimeRevisionIdentity, bool) {
	if v == nil || !validManagedVersion(v.GetVersion()) || v.GetAttempt() < 1 || !validManagedDigest(v.GetRevisionDigest()) || v.GetCreatedAt() == nil || v.GetCreatedAt().CheckValid() != nil {
		return generated.PublicRuntimeRevisionIdentity{}, false
	}
	for _, ref := range []string{v.GetRef(), v.GetRunRef(), v.GetSessionRef()} {
		if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 {
			return generated.PublicRuntimeRevisionIdentity{}, false
		}
	}
	if v.GetTurnRef() != "" && (!opaqueHTTPReference.MatchString(v.GetTurnRef()) || len(v.GetTurnRef()) > 96) {
		return generated.PublicRuntimeRevisionIdentity{}, false
	}
	return generated.PublicRuntimeRevisionIdentity{Ref: v.GetRef(), Version: v.GetVersion(), RunRef: v.GetRunRef(), SessionRef: v.GetSessionRef(), TurnRef: optionalManagedString(v.GetTurnRef()), Attempt: int(v.GetAttempt()), RevisionDigest: v.GetRevisionDigest(), CreatedAt: v.GetCreatedAt().AsTime()}, true
}

func runtimeRevisionDiffValue(component generated.RuntimeRevisionDiffChangeComponent, v *cp.RuntimeRevisionDiffValue) (generated.RuntimeRevisionDiffValue, bool) {
	result := generated.RuntimeRevisionDiffValue{}
	if v == nil {
		return result, false
	}
	ref, revision, digest, version := v.GetRef(), v.GetRevision(), v.GetDigest(), v.GetVersion()
	if ref != "" && !boundedModelText(ref, 160) || revision != "" && !boundedModelText(revision, 160) || version < 0 || version > maximumSafeJSONInteger {
		return result, false
	}
	switch component {
	case "PROVIDER", "MODEL":
		if ref == "" || revision != "" || digest != "" || version != 0 {
			return result, false
		}
	case "RUNTIME_PROFILE":
		if ref == "" || revision == "" || digest != "" || version != 0 {
			return result, false
		}
	case "RUNTIME_CONFIGURATION", "PROVIDER_POLICY", "CONFIG_OVERLAY", "ENVIRONMENT", "ENVIRONMENT_BINDING":
		if revision != "" || (ref != "" || version != 0 || digest != "") && (!opaqueHTTPReference.MatchString(ref) || len(ref) > 96 || !validManagedVersion(version) || !validManagedDigest(digest)) {
			return result, false
		}
	case "INSTRUCTION":
		if ref == "" || !validManagedDigest(digest) || revision != "" || version != 0 {
			return result, false
		}
	case "INTEGRATION_GRANTS":
		if ref != "" || revision != "" || version != 0 || !validManagedDigest(digest) {
			return result, false
		}
	case "IMAGE":
		if ref != "" || revision != "" || version != 0 || digest != "" && !validManagedDigest(strings.TrimPrefix(digest, "sha256:")) {
			return result, false
		}
	default:
		return result, false
	}
	result.Ref, result.Revision, result.Digest = optionalManagedString(ref), optionalManagedString(revision), optionalManagedString(digest)
	if version != 0 {
		result.Version = &version
	}
	return result, true
}
