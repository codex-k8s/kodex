package httptransport

import (
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// personalEmailCreateBody keeps request syntax plain before explicit address validation; the
// generated email scalar's custom unmarshaler is not used as this strict DTO shape boundary.
// personalEmailCreateBody сохраняет обычную форму запроса до явной проверки адреса; custom
// unmarshaler generated email scalar не используется как граница строгой формы DTO.
type personalEmailCreateBody struct {
	Name   string `json:"name"`
	Sender string `json:"sender"`
}

// GetPersonalEmailMailboxMutationReceipt reconciles only a closed self setup command and validates the exclusive historical result shape; it cannot be used as a generic idempotency ledger browser.
// GetPersonalEmailMailboxMutationReceipt сверяет только закрытую собственную setup command и проверяет исключающий исторический result shape; не предоставляет универсальный просмотр idempotency ledger.
func (server *Server) GetPersonalEmailMailboxMutationReceipt(w http.ResponseWriter, r *http.Request, params generated.GetPersonalEmailMailboxMutationReceiptParams) {
	if !requirePersonalEmailClient(w, server) {
		return
	}
	for key, values := range r.URL.Query() {
		if key != "operation" && key != "idempotencyKey" || len(values) != 1 {
			writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
			return
		}
	}
	operation, ok := cp.PersonalEmailMailboxMutationOperation_value["PERSONAL_EMAIL_MAILBOX_MUTATION_OPERATION_"+string(params.Operation)]
	if !ok || operation == 0 || len(params.IdempotencyKey) < 8 || len(params.IdempotencyKey) > 128 || strings.ContainsAny(params.IdempotencyKey, "\x00\r\n") {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalEmail.GetPersonalEmailMailboxMutationReceipt(r.Context(), &cp.GetPersonalEmailMailboxMutationReceiptRequest{Operation: cp.PersonalEmailMailboxMutationOperation(operation), IdempotencyKey: params.IdempotencyKey})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		invalidSecretDraft(w)
		return
	}
	receipt := response.GetReceipt()
	if receipt == nil || len(receipt.ProtoReflect().GetUnknown()) != 0 || int32(receipt.GetOperation()) != operation {
		invalidSecretDraft(w)
		return
	}
	result := generated.PersonalEmailMailboxMutationReceipt{Operation: params.Operation}
	if params.Operation == "CREATE" {
		item, ok := personalEmailMailboxView(receipt.GetMailbox())
		if !ok || receipt.GetOwnership() != nil || item.Ownership.Policy != "INITIATOR_REQUIRED" || item.Ownership.State != "ACTIVE" || item.Ownership.Generation != 1 || item.Ownership.ConnectionVersion != 1 || item.Available {
			invalidSecretDraft(w)
			return
		}
		result.Mailbox = &item
	} else {
		item, ok := personalEmailOwnershipView(receipt.GetOwnership())
		if !ok || receipt.GetMailbox() != nil || params.Operation == "CONFIGURE" && item.State != "ACTIVE" || params.Operation == "REVOKE" && item.State != "REVOKED" {
			invalidSecretDraft(w)
			return
		}
		result.Ownership = &item
	}
	writeJSON(w, 200, result)
}

// requirePersonalEmailClient rejects a missing typed owner client before any request can fall back to generic token configuration or an unprotected endpoint.
// requirePersonalEmailClient отклоняет отсутствующий typed owner client до возможности fallback к generic token configuration либо незащищённому endpoint.
func requirePersonalEmailClient(w http.ResponseWriter, server *Server) bool {
	setRuntimeSecretHeaders(w)
	if server.control == nil || server.control.PersonalEmail == nil {
		invalidSecretDraft(w)
		return false
	}
	return true
}

// personalEmailOwnershipView validates the complete closed owner projection, separating unconfigured generation zero from active/revoked metadata and omitting personal subject identifiers.
// personalEmailOwnershipView проверяет полную закрытую owner projection, отделяет unconfigured generation zero от active/revoked metadata и не выдаёт личные subject identifiers.
func personalEmailOwnershipView(value *cp.EmailMailboxOwnership) (generated.EmailMailboxOwnership, bool) {
	var result generated.EmailMailboxOwnership
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || !opaqueHTTPReference.MatchString(value.GetConnectionRef()) || len(value.GetConnectionRef()) > 96 || !validManagedVersion(value.GetConnectionVersion()) || value.GetGeneration() < 0 || value.GetGeneration() > maximumSafeJSONInteger {
		return result, false
	}
	policy, ok := cp.IntegrationCredentialPolicy_name[int32(value.GetPolicy())]
	if !ok || value.GetPolicy() == cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_UNSPECIFIED {
		return result, false
	}
	state, ok := cp.EmailMailboxOwnershipState_name[int32(value.GetState())]
	if !ok || value.GetState() == cp.EmailMailboxOwnershipState_EMAIL_MAILBOX_OWNERSHIP_STATE_UNSPECIFIED {
		return result, false
	}
	result = generated.EmailMailboxOwnership{ConnectionRef: value.GetConnectionRef(), ConnectionVersion: value.GetConnectionVersion(), DeliveryKind: "MAILBOX_CONFIGURATION",
		Policy: generated.EmailMailboxOwnershipPolicy(strings.TrimPrefix(policy, "INTEGRATION_CREDENTIAL_POLICY_")), State: generated.EmailMailboxOwnershipState(strings.TrimPrefix(state, "EMAIL_MAILBOX_OWNERSHIP_STATE_")), Generation: value.GetGeneration(), OwnerEligible: value.GetOwnerEligible()}
	if result.OwnerEligible && result.State != "ACTIVE" {
		return generated.EmailMailboxOwnership{}, false
	}
	if result.State == "UNCONFIGURED" {
		if result.Generation != 0 || value.GetServiceSubjectRef() != "" {
			return generated.EmailMailboxOwnership{}, false
		}
	} else {
		if result.Generation < 1 {
			return generated.EmailMailboxOwnership{}, false
		}
		if result.Policy == "PROJECT_SHARED" {
			if !opaqueHTTPReference.MatchString(value.GetServiceSubjectRef()) || len(value.GetServiceSubjectRef()) > 96 {
				return generated.EmailMailboxOwnership{}, false
			}
			ref := generated.OpaqueRef(value.GetServiceSubjectRef())
			result.ServiceSubjectRef = &ref
		} else if value.GetServiceSubjectRef() != "" {
			return generated.EmailMailboxOwnership{}, false
		}
	}
	return result, true
}

// personalEmailMailboxView validates safe display metadata and the ownership envelope; availability is only an observation and never converted into a grant or proof of successful account login.
// personalEmailMailboxView проверяет безопасные display metadata и ownership envelope; availability остаётся наблюдением и не превращается в grant либо доказательство успешного входа аккаунта.
func personalEmailMailboxView(value *cp.MyEmailMailbox) (generated.MyEmailMailbox, bool) {
	var result generated.MyEmailMailbox
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 || !validSearchText(value.GetName(), 1, 160) {
		return result, false
	}
	owner, ok := personalEmailOwnershipView(value.GetOwnership())
	if !ok || value.GetAvailable() && (!value.GetEnabled() || owner.State != "ACTIVE") {
		return result, false
	}
	return generated.MyEmailMailbox{Ownership: owner, Name: value.GetName(), Enabled: value.GetEnabled(), Available: value.GetAvailable()}, true
}

// ListMyEmailMailboxes accepts only bounded self-catalog parameters and validates every returned ownership record and cursor before emitting the whole page.
// ListMyEmailMailboxes принимает только ограниченные self-catalog параметры и проверяет каждую ownership запись и cursor до выдачи целой страницы.
func (server *Server) ListMyEmailMailboxes(w http.ResponseWriter, r *http.Request, params generated.ListMyEmailMailboxesParams) {
	if !requirePersonalEmailClient(w, server) {
		return
	}
	for key, values := range r.URL.Query() {
		if key != "query" && key != "pageSize" && key != "pageToken" || len(values) != 1 {
			writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
			return
		}
	}
	if params.Query != nil && !validSearchText(*params.Query, 0, 200) || params.PageSize != nil && (*params.PageSize < 1 || *params.PageSize > 100) || params.PageToken != nil && len(*params.PageToken) > 4096 {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	pagination := page(params.PageSize, params.PageToken)
	response, err := server.control.PersonalEmail.ListMyEmailMailboxes(r.Context(), &cp.ListMyEmailMailboxesRequest{Query: stringValue(params.Query), PageSize: pagination.GetPageSize(), PageToken: pagination.GetPageToken()})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.GetItems()) > int(pagination.GetPageSize()) || len(response.GetNextPageToken()) > 4096 || response.GetNextPageToken() != "" && (len(response.GetItems()) == 0 || response.GetNextPageToken() == pagination.GetPageToken()) {
		invalidSecretDraft(w)
		return
	}
	result := generated.MyEmailMailboxPage{Items: make([]generated.MyEmailMailbox, 0, len(response.GetItems()))}
	seen := map[string]bool{}
	for _, raw := range response.GetItems() {
		item, ok := personalEmailMailboxView(raw)
		if !ok || seen[item.Ownership.ConnectionRef] {
			invalidSecretDraft(w)
			return
		}
		seen[item.Ownership.ConnectionRef] = true
		result.Items = append(result.Items, item)
	}
	if response.GetNextPageToken() != "" {
		next := response.GetNextPageToken()
		result.NextPageToken = &next
	}
	writeJSON(w, 200, result)
}

// CreatePersonalEmailMailboxConnection forwards only name and sender to the fixed self owner operation; arbitrary owner, package, project or ACL payload fields are rejected by the strict decoder.
// CreatePersonalEmailMailboxConnection передаёт только имя и sender фиксированной self owner operation; произвольные поля owner, package, project либо ACL отклоняет строгий decoder.
func (server *Server) CreatePersonalEmailMailboxConnection(w http.ResponseWriter, r *http.Request, params generated.CreatePersonalEmailMailboxConnectionParams) {
	if !requirePersonalEmailClient(w, server) {
		return
	}
	if len(r.URL.Query()) != 0 {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	body, mutation, ok := decodeMailboxMutation[personalEmailCreateBody](w, r, params.IdempotencyKey, "")
	if !ok {
		return
	}
	name := strings.TrimSpace(body.Name)
	if !validSearchText(body.Name, 1, 160) || name == "" || strings.ContainsAny(body.Name, "\r\n") || !utf8.ValidString(body.Sender) || !api.Address(body.Sender) {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalEmail.CreatePersonalEmailMailboxConnection(r.Context(), &cp.CreatePersonalEmailMailboxConnectionRequest{Mutation: mutation, Name: body.Name, Sender: body.Sender})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		invalidSecretDraft(w)
		return
	}
	item, ok := personalEmailMailboxView(response.GetMailbox())
	if !ok || item.Name != name || item.Ownership.Policy != "INITIATOR_REQUIRED" || item.Ownership.State != "ACTIVE" || !item.Ownership.OwnerEligible || item.Ownership.Generation != 1 || item.Ownership.ConnectionVersion != 1 || !item.Enabled || item.Available {
		invalidSecretDraft(w)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// GetEmailMailboxOwnership reads one exact connection and refuses foreign or malformed owner response coordinates before public projection.
// GetEmailMailboxOwnership читает одно точное подключение и отклоняет чужие либо повреждённые owner response coordinates до public projection.
func (server *Server) GetEmailMailboxOwnership(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef) {
	if !requirePersonalEmailClient(w, server) {
		return
	}
	if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 || len(r.URL.Query()) != 0 {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalEmail.GetEmailMailboxOwnership(r.Context(), &cp.GetEmailMailboxOwnershipRequest{ConnectionRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		invalidSecretDraft(w)
		return
	}
	item, ok := personalEmailOwnershipView(response.GetOwnership())
	if !ok || item.ConnectionRef != ref {
		invalidSecretDraft(w)
		return
	}
	setVersionETag(w, uint64(item.ConnectionVersion))
	writeJSON(w, 200, item)
}

// ConfigureEmailMailboxOwnership requires the closed self-or-service choice and exact mutation pins; selection remains a locator and independent service setup authority belongs to the owner.
// ConfigureEmailMailboxOwnership требует закрытый self-or-service выбор и точные mutation pins; выбор остаётся locator, независимые права service setup проверяет owner.
func (server *Server) ConfigureEmailMailboxOwnership(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.ConfigureEmailMailboxOwnershipParams) {
	if !requirePersonalEmailClient(w, server) {
		return
	}
	if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 || len(r.URL.Query()) != 0 {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	body, mutation, ok := decodeMailboxMutation[generated.EmailMailboxOwnershipInput](w, r, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if !validManagedVersion(mutation.GetExpectedVersion()) {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	policy := cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_UNSPECIFIED
	switch body.Policy {
	case "INITIATOR_REQUIRED":
		if body.ServiceSubjectRef != nil {
			writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
			return
		}
		policy = cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_INITIATOR_REQUIRED
	case "PROJECT_SHARED":
		if body.ServiceSubjectRef == nil || !opaqueHTTPReference.MatchString(*body.ServiceSubjectRef) || len(*body.ServiceSubjectRef) > 96 {
			writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
			return
		}
		policy = cp.IntegrationCredentialPolicy_INTEGRATION_CREDENTIAL_POLICY_PROJECT_SHARED
	default:
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalEmail.ConfigureEmailMailboxOwnership(r.Context(), &cp.ConfigureEmailMailboxOwnershipRequest{Mutation: mutation, ConnectionRef: ref, Policy: policy, ServiceSubjectRef: stringValue(body.ServiceSubjectRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		invalidSecretDraft(w)
		return
	}
	item, ok := personalEmailOwnershipView(response.GetOwnership())
	if !ok || item.ConnectionRef != ref || item.ConnectionVersion != mutation.GetExpectedVersion()+1 || item.State != "ACTIVE" || string(item.Policy) != string(body.Policy) || stringValue(item.ServiceSubjectRef) != stringValue(body.ServiceSubjectRef) {
		invalidSecretDraft(w)
		return
	}
	setVersionETag(w, uint64(item.ConnectionVersion))
	writeJSON(w, 200, item)
}

// RevokeEmailMailboxOwnership sends no mutable ownership selector or request body and checks the exact versioned revocation receipt; remote cancellation is never inferred.
// RevokeEmailMailboxOwnership не передаёт изменяемый ownership selector либо request body и проверяет точную versioned revocation receipt; remote cancellation не выводится.
func (server *Server) RevokeEmailMailboxOwnership(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, params generated.RevokeEmailMailboxOwnershipParams) {
	if !requirePersonalEmailClient(w, server) {
		return
	}
	if !opaqueHTTPReference.MatchString(ref) || len(ref) > 96 || len(r.URL.Query()) != 0 {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	if r.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(raw) != 0 {
			writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
			return
		}
	}
	mutation, ok := requireMutation(w, params.IdempotencyKey, params.IfMatch)
	if !ok {
		return
	}
	if !validManagedVersion(mutation.GetExpectedVersion()) {
		writeLocalProblem(w, 400, personalCredentialInvalidRequestCode, false)
		return
	}
	response, err := server.control.PersonalEmail.RevokeEmailMailboxOwnership(r.Context(), &cp.RevokeEmailMailboxOwnershipRequest{Mutation: mutation, ConnectionRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 {
		invalidSecretDraft(w)
		return
	}
	item, ok := personalEmailOwnershipView(response.GetOwnership())
	if !ok || item.ConnectionRef != ref || item.ConnectionVersion != mutation.GetExpectedVersion()+1 || item.State != "REVOKED" {
		invalidSecretDraft(w)
		return
	}
	setVersionETag(w, uint64(item.ConnectionVersion))
	writeJSON(w, 200, item)
}
