package httptransport

import (
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// mailboxDiagnostics admits at most 16 diagnostics with exact code/message pairs, empty paths and nonnegative source positions, returning a fresh empty array when none exist. Arbitrary upstream messages and file paths are rejected instead of redacted heuristically.
// mailboxDiagnostics допускает не более 16 diagnostics с точными code/message pairs, пустыми paths и неотрицательными позициями, при отсутствии возвращает новый пустой массив. Произвольные upstream messages и file paths отклоняются, а не эвристически очищаются.
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

// mailboxPublicationView validates optional publication identity/revision/digest/times, closed state and failure-code consistency, including READY/PENDING ready-time rules. It checks an owner's metadata snapshot, not recomputed content integrity or a live delivery readiness probe.
// mailboxPublicationView проверяет optional publication identity/revision/digest/time, закрытый state и согласованность failure code, включая ready-time правила READY/PENDING. Это owner metadata snapshot, не пересчёт целостности content и не live delivery readiness probe.
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
	result := &generated.EmailMailboxPublication{Ref: v.GetRef(), Revision: v.GetRevision(), Digest: v.GetDigest(), State: state, ConfigurationRevisionRef: v.GetConfigurationRevisionRef(), CreatedAt: v.GetCreatedAt().AsTime(), FailureCode: generated.EmailMailboxPublicationFailureCode(v.GetFailureCode())}
	if v.GetReadyAt() != nil {
		if v.GetReadyAt().CheckValid() != nil || v.GetReadyAt().AsTime().Before(result.CreatedAt) {
			return nil, false
		}
		ready := v.GetReadyAt().AsTime()
		result.ReadyAt = &ready
	}
	if state == "READY" && result.ReadyAt == nil || state == "PENDING" && result.ReadyAt != nil {
		return nil, false
	}
	if state == "FAILED" {
		switch result.FailureCode {
		case "EMAIL_MAILBOX_DELIVERY_EXPIRED", "EMAIL_MAILBOX_CONNECTION_CHANGED", "EMAIL_MAILBOX_DELIVERY_REJECTED":
		default:
			return nil, false
		}
	} else if result.FailureCode != "" {
		return nil, false
	}
	return result, true
}

// mailboxConfigurationView validates connection/mailbox refs and versions, the EMAIL_MAILBOX kind, JSON revision, bounded specification, optional publication, diagnostics and action availability. It constructs public metadata only; selected route pins are checked separately by the writer and owner eligibility is not reconstructed here.
// mailboxConfigurationView проверяет connection/mailbox refs и versions, EMAIL_MAILBOX kind, JSON revision, bounded specification, optional publication, diagnostics и action availability. Создаёт только публичные metadata; selected route pins отдельно сверяет writer, owner eligibility здесь не восстанавливается.
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
	actions, ok := mailboxActionViews(v.GetNextActions(), false)
	if !ok {
		return result, false
	}
	if v.GetBoundRevisionRef() != "" && (!opaqueHTTPReference.MatchString(v.GetBoundRevisionRef()) || len(v.GetBoundRevisionRef()) > 96) {
		return result, false
	}
	return generated.EmailMailboxConfigurationView{ConnectionRef: v.GetConnectionRef(), ConnectionVersion: v.GetConnectionVersion(), MailboxRef: v.GetMailboxRef(), Configuration: configuration, Revision: revision, Specification: *spec, Publication: publication, BoundRevisionRef: v.GetBoundRevisionRef(), Diagnostics: diagnostics, NextActions: actions}, true
}

// mailboxActionViews requires exactly one CREATE_DRAFT action for lists or nine unique valid actions for detail, each with a valid reason and enabled exactly when reason is NONE. It preserves order and describes availability without authorizing execution.
// mailboxActionViews требует ровно один CREATE_DRAFT для списков либо девять уникальных допустимых actions для detail, с допустимой reason и enabled ровно при NONE. Сохраняет порядок и описывает доступность, не разрешая исполнение.
func mailboxActionViews(values []*cp.EmailMailboxActionAvailability, list bool) ([]generated.EmailMailboxActionAvailability, bool) {
	expected := 9
	if list {
		expected = 1
	}
	if len(values) != expected {
		return nil, false
	}
	result := make([]generated.EmailMailboxActionAvailability, 0, len(values))
	seen := map[cp.EmailMailboxAction]bool{}
	for _, value := range values {
		if value == nil || seen[value.GetAction()] {
			return nil, false
		}
		action := generated.EmailMailboxActionAvailabilityAction(strings.TrimPrefix(value.GetAction().String(), "EMAIL_MAILBOX_ACTION_"))
		reason := generated.EmailMailboxActionAvailabilityReason(strings.TrimPrefix(value.GetReason().String(), "EMAIL_MAILBOX_ACTION_REASON_"))
		if !action.Valid() || !reason.Valid() || value.GetEnabled() != (reason == "NONE") || list && action != "CREATE_DRAFT" {
			return nil, false
		}
		seen[value.GetAction()] = true
		result = append(result, generated.EmailMailboxActionAvailability{Action: action, Enabled: value.GetEnabled(), Reason: reason})
	}
	return result, true
}

// writeMailboxConfiguration sets no-store headers, validates the owner view and any supplied exact connection/configuration/revision pins, then writes the configuration version ETag and requested status. It does not locally transition draft or publication state.
// writeMailboxConfiguration задаёт no-store headers, проверяет owner view и переданные exact connection/configuration/revision pins, затем выдаёт ETag версии configuration и заданный status. Локально draft и publication state не меняет.
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

// mailboxCredentialView requires the selected connection, safe connection version and equal generation, valid reference name and optional exact kind before projecting value-free metadata. It does not load the credential, check its secret contents or grant an employee access.
// mailboxCredentialView требует выбранный connection, safe connection version и равную generation, допустимое reference name и необязательный exact kind до проекции metadata без значения. Credential не загружает, secret contents не проверяет и доступ сотруднику не выдаёт.
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
