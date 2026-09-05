package httptransport

import (
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func mailboxDiagnostics(values []*cp.EmailMailboxDiagnostic) ([]generated.EmailMailboxDiagnostic, bool) {
	result := []generated.EmailMailboxDiagnostic{}
	if len(values) > 16 {
		return nil, false
	}
	for _, v := range values {
		if v == nil || v.GetPath() != "" || v.GetLine() < 0 || v.GetColumn() < 0 {
			return nil, false
		}
		expected := ""
		switch v.GetCode() {
		case "EMAIL_MAILBOX_SYNTAX_INVALID":
			expected = "Mailbox document syntax or fields are invalid"
		case "EMAIL_MAILBOX_CONFIGURATION_INVALID":
			expected = "Mailbox configuration is incomplete or invalid"
		case "EMAIL_MAILBOX_CREDENTIAL_MISMATCH":
			expected = "Mailbox credential reference is unavailable"
		default:
			return nil, false
		}
		if v.GetMessage() != expected {
			return nil, false
		}
		result = append(result, generated.EmailMailboxDiagnostic{Code: generated.EmailMailboxDiagnosticCode(v.GetCode()), Path: v.GetPath(), Message: expected, Line: v.GetLine(), Column: v.GetColumn()})
	}
	return result, true
}

func mailboxPublicationView(v *cp.EmailMailboxPublication) (*generated.EmailMailboxPublication, bool) {
	if v == nil {
		return nil, true
	}
	if !opaqueHTTPReference.MatchString(v.GetRef()) || len(v.GetRef()) > 96 || !validManagedVersion(v.GetRevision()) || !validManagedDigest(v.GetDigest()) || v.GetCreatedAt() == nil || v.GetCreatedAt().CheckValid() != nil || v.GetConfigurationRevisionRef() != "" && (!opaqueHTTPReference.MatchString(v.GetConfigurationRevisionRef()) || len(v.GetConfigurationRevisionRef()) > 96) {
		return nil, false
	}
	state := generated.EmailMailboxPublicationState(strings.TrimPrefix(v.GetState().String(), "EMAIL_MAILBOX_PUBLICATION_STATE_"))
	if !state.Valid() {
		return nil, false
	}
	result := &generated.EmailMailboxPublication{Ref: v.GetRef(), Revision: v.GetRevision(), Digest: v.GetDigest(), State: state, ConfigurationRevisionRef: v.GetConfigurationRevisionRef(), CreatedAt: v.GetCreatedAt().AsTime(), FailureCode: v.GetFailureCode()}
	if v.GetReadyAt() != nil {
		if v.GetReadyAt().CheckValid() != nil || v.GetReadyAt().AsTime().Before(result.CreatedAt) {
			return nil, false
		}
		ready := v.GetReadyAt().AsTime()
		result.ReadyAt = &ready
	}
	if state == "READY" && (result.ReadyAt == nil || result.FailureCode != "") || state == "PENDING" && result.ReadyAt != nil || len(result.FailureCode) > 64 {
		return nil, false
	}
	for _, r := range result.FailureCode {
		if r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return nil, false
		}
	}
	return result, true
}

func mailboxConfigurationView(v *cp.EmailMailboxConfigurationView) (generated.EmailMailboxConfigurationView, bool) {
	result := generated.EmailMailboxConfigurationView{}
	if v == nil || !opaqueHTTPReference.MatchString(v.GetConnectionRef()) || len(v.GetConnectionRef()) > 96 || !validManagedVersion(v.GetConnectionVersion()) || !opaqueHTTPReference.MatchString(v.GetMailboxRef()) || len(v.GetMailboxRef()) > 96 || v.GetConfiguration().GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_EMAIL_MAILBOX {
		return result, false
	}
	configuration, err := managedConfigurationView(v.GetConfiguration())
	if err != nil {
		return result, false
	}
	revision, err := managedRevisionView(v.GetRevision())
	if err != nil || revision.ContentFormat != "JSON" || len(revision.Content) > 262144 {
		return result, false
	}
	spec, ok := mailboxSpecificationView(v.GetSpecification())
	if !ok || spec == nil {
		return result, false
	}
	publication, ok := mailboxPublicationView(v.GetPublication())
	if !ok {
		return result, false
	}
	diagnostics, ok := mailboxDiagnostics(v.GetDiagnostics())
	if !ok {
		return result, false
	}
	if v.GetBoundRevisionRef() != "" && (!opaqueHTTPReference.MatchString(v.GetBoundRevisionRef()) || len(v.GetBoundRevisionRef()) > 96) {
		return result, false
	}
	return generated.EmailMailboxConfigurationView{ConnectionRef: v.GetConnectionRef(), ConnectionVersion: v.GetConnectionVersion(), MailboxRef: v.GetMailboxRef(), Configuration: configuration, Revision: revision, Specification: *spec, Publication: publication, BoundRevisionRef: v.GetBoundRevisionRef(), Diagnostics: diagnostics}, true
}

func writeMailboxConfiguration(w http.ResponseWriter, status int, v *cp.EmailMailboxConfigurationView, connection, configuration, revision string) {
	setRuntimeSecretHeaders(w)
	result, ok := mailboxConfigurationView(v)
	if !ok || connection != "" && result.ConnectionRef != connection || configuration != "" && result.Configuration.Ref != configuration || revision != "" && result.Revision.Ref != revision {
		invalidSecretDraft(w)
		return
	}
	setVersionETag(w, uint64(result.Configuration.Version))
	writeJSON(w, status, result)
}

func mailboxCredentialView(v *cp.EmailMailboxCredential, connection string, kind cp.EmailMailboxCredentialKind) (generated.EmailMailboxCredential, bool) {
	result := generated.EmailMailboxCredential{}
	if v == nil || v.GetConnectionRef() != connection || !validManagedVersion(v.GetConnectionVersion()) || v.GetGeneration() != v.GetConnectionVersion() || !opaqueHTTPReference.MatchString(v.GetName()) || len(v.GetName()) > 128 || kind != 0 && v.GetKind() != kind {
		return result, false
	}
	name := generated.EmailMailboxCredentialKind(strings.TrimPrefix(v.GetKind().String(), "EMAIL_MAILBOX_CREDENTIAL_KIND_"))
	if !name.Valid() {
		return result, false
	}
	return generated.EmailMailboxCredential{Name: v.GetName(), Generation: v.GetGeneration(), ConnectionRef: v.GetConnectionRef(), ConnectionVersion: v.GetConnectionVersion(), Kind: name}, true
}
