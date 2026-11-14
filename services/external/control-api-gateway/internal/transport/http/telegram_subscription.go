package httptransport

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

const (
	// telegramInvalidRequest rejects malformed proposals without returning their contents.
	// telegramInvalidRequest отклоняет неверные proposals без выдачи их содержимого.
	telegramInvalidRequest = "INVALID_REQUEST"
	// telegramInvalidUpstream refuses malformed safe projections instead of returning private owner messages.
	// telegramInvalidUpstream отвергает неверные safe projections вместо выдачи private owner messages.
	telegramInvalidUpstream = "INVALID_UPSTREAM_RESPONSE"
)

// decodeTelegramSubscriptionInput requires an explicit boolean and one exact generated proposal shape; duplicate/case-aliased fields and caller authority are rejected before RPC.
// decodeTelegramSubscriptionInput требует явный boolean и одну точную generated proposal shape; duplicate/case-alias fields и caller authority отклоняются до RPC.
func decodeTelegramSubscriptionInput(w http.ResponseWriter, r *http.Request) (*cp.ConfigureTelegramSubscriptionRequest, bool) {
	if r.Body == nil {
		writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
	var fields map[string]json.RawMessage
	if err != nil || len(raw) > 4096 || internalrpcauth.DecodeStrictJSON(raw, &fields) != nil || fields == nil {
		writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
		return nil, false
	}
	allowed := map[string]bool{"enabled": true, "connectionRef": true, "eventTypes": true, "durationSeconds": true, "maxEffects": true}
	for key := range fields {
		if !allowed[key] {
			writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
			return nil, false
		}
	}
	enabled := bytes.TrimSpace(fields["enabled"])
	if bytes.Equal(enabled, []byte("false")) {
		var input generated.TelegramSubscriptionDisableInput
		if len(fields) != 1 || internalrpcauth.DecodeStrictJSON(raw, &input) != nil {
			writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
			return nil, false
		}
		return &cp.ConfigureTelegramSubscriptionRequest{Enabled: false}, true
	}
	var input generated.TelegramSubscriptionEnableInput
	if !bytes.Equal(enabled, []byte("true")) || len(fields) != 5 || internalrpcauth.DecodeStrictJSON(raw, &input) != nil ||
		!opaqueHTTPReference.MatchString(input.ConnectionRef) || input.DurationSeconds < 60 || input.DurationSeconds > 604800 || input.MaxEffects < 1 || input.MaxEffects > 10000 {
		writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
		return nil, false
	}
	events := make([]string, 0, len(input.EventTypes))
	for _, event := range input.EventTypes {
		events = append(events, string(event))
	}
	if !telegramPublicEvents(events) {
		writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
		return nil, false
	}
	return &cp.ConfigureTelegramSubscriptionRequest{Enabled: true, ConnectionRef: input.ConnectionRef, EventTypes: events,
		DurationSeconds: int32(input.DurationSeconds), MaxEffects: int32(input.MaxEffects)}, true
}

// telegramPublicEvents admits only a unique nonempty subset of fixed server notification templates; no arbitrary text or decision event is accepted.
// telegramPublicEvents допускает лишь уникальное непустое подмножество fixed server notification templates; произвольный текст либо decision event не принимается.
func telegramPublicEvents(events []string) bool {
	if len(events) < 1 || len(events) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, event := range events {
		if seen[event] || event != "FINISHED" && event != "FAILED" && event != "HUMAN_GATE" {
			return false
		}
		seen[event] = true
	}
	return true
}

// telegramSubscriptionView constructs a closed public DTO and validates every required scalar without allowing future private protobuf fields into a browser response.
// telegramSubscriptionView создаёт закрытый public DTO и проверяет каждый required scalar, не пропуская будущие private protobuf fields в browser response.
func telegramSubscriptionView(item *cp.TelegramSubscription, project string) (generated.TelegramSubscription, bool) {
	var view generated.TelegramSubscription
	if item == nil || item.GetProjectRef() != project || !opaqueHTTPReference.MatchString(item.GetRef()) || !opaqueHTTPReference.MatchString(item.GetConnectionRef()) ||
		!validManagedVersion(item.GetVersion()) || !validManagedDigest(item.GetScopeDigest()) || item.GetBotId() < 1 || item.GetBotId() > 4503599627370495 ||
		!telegramPublicEvents(item.GetEventTypes()) || item.GetMaxEffects() < 1 || item.GetMaxEffects() > 10000 || item.GetUsedEffects() < 0 || item.GetUsedEffects() > item.GetMaxEffects() ||
		item.GetExpiresAt() == nil || item.GetExpiresAt().CheckValid() != nil || item.GetExpiresAt().AsTime().IsZero() ||
		item.GetUpdatedAt() == nil || item.GetUpdatedAt().CheckValid() != nil || item.GetUpdatedAt().AsTime().IsZero() {
		return view, false
	}
	switch item.GetState() {
	case "ACTIVE", "DISABLED", "EXPIRED", "EXHAUSTED", "SCOPE_REVOKED":
	default:
		return view, false
	}
	if !strings.HasPrefix(item.GetChatDisplay(), "…") || len(item.GetChatDisplay()) > 12 {
		return view, false
	}
	for _, character := range strings.TrimPrefix(item.GetChatDisplay(), "…") {
		if character != '-' && (character < '0' || character > '9') {
			return view, false
		}
	}
	if item.GetTopicDisplay() != "NONE" {
		topic, err := strconv.ParseInt(item.GetTopicDisplay(), 10, 32)
		if err != nil || topic < 1 || strconv.FormatInt(topic, 10) != item.GetTopicDisplay() {
			return view, false
		}
	}
	events := make([]generated.TelegramSubscriptionEventTypes, 0, len(item.GetEventTypes()))
	for _, event := range item.GetEventTypes() {
		events = append(events, generated.TelegramSubscriptionEventTypes(event))
	}
	view = generated.TelegramSubscription{Ref: item.GetRef(), ProjectRef: project, ConnectionRef: item.GetConnectionRef(), Version: item.GetVersion(),
		Enabled: item.GetEnabled(), State: generated.TelegramSubscriptionState(item.GetState()), EventTypes: events, ScopeDigest: item.GetScopeDigest(), BotId: item.GetBotId(),
		ChatDisplay: item.GetChatDisplay(), TopicDisplay: item.GetTopicDisplay(), ExpiresAt: item.GetExpiresAt().AsTime(), UpdatedAt: item.GetUpdatedAt().AsTime(),
		MaxEffects: int(item.GetMaxEffects()), UsedEffects: int(item.GetUsedEffects()), CanManage: item.GetCanManage()}
	return view, true
}

// writeTelegramData writes only an explicitly constructed public value under no-store; it never serializes an internal owner message or descriptor.
// writeTelegramData выдаёт лишь явно построенное public value под no-store и никогда не сериализует internal owner message либо descriptor.
func writeTelegramData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(data)
}

// GetTelegramSubscription carries a checked project selector to the authoritative owner and preserves the explicit absent-configuration result.
// GetTelegramSubscription передаёт проверенный project selector авторитетному владельцу и сохраняет явный результат отсутствующей настройки.
func (server *Server) GetTelegramSubscription(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef) {
	if len(r.URL.Query()) != 0 {
		writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
		return
	}
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	response, err := server.control.Query.GetTelegramSubscription(r.Context(), &cp.GetTelegramSubscriptionRequest{ProjectRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil {
		writeLocalProblem(w, http.StatusBadGateway, telegramInvalidUpstream, false)
		return
	}
	data := map[string]any{"canManage": response.GetCanManage()}
	if response.GetSubscription() != nil {
		view, ok := telegramSubscriptionView(response.GetSubscription(), ref)
		if !ok || view.CanManage != response.GetCanManage() {
			writeLocalProblem(w, http.StatusBadGateway, telegramInvalidUpstream, false)
			return
		}
		data["subscription"] = view
	}
	writeTelegramData(w, data)
}

// ConfigureTelegramSubscription submits one explicit bounded owner proposal with optional first-create OCC and no caller-selected authority or destination coordinates.
// ConfigureTelegramSubscription передаёт один явный bounded owner proposal с optional first-create OCC и без выбранных caller authority либо destination coordinates.
func (server *Server) ConfigureTelegramSubscription(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, params generated.ConfigureTelegramSubscriptionParams) {
	if len(r.URL.Query()) != 0 || !httpIdempotencyKey.MatchString(params.IdempotencyKey) || len(r.Header.Values("Idempotency-Key")) != 1 || len(r.Header.Values("If-Match")) > 1 {
		writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
		return
	}
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	request, ok := decodeTelegramSubscriptionInput(w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, params.IdempotencyKey, stringValue(params.IfMatch))
	if !ok {
		return
	}
	if params.IfMatch != nil {
		mutation, ok = requireVersionedMutation(w, params.IdempotencyKey, *params.IfMatch)
		if !ok {
			return
		}
	}
	request.ProjectRef = ref
	request.Mutation = mutation
	response, err := server.control.Command.ConfigureTelegramSubscription(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	view, ok := telegramSubscriptionView(response.GetSubscription(), ref)
	if !ok || view.Enabled != request.GetEnabled() || request.GetEnabled() && view.ConnectionRef != request.GetConnectionRef() {
		writeLocalProblem(w, http.StatusBadGateway, telegramInvalidUpstream, false)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(view.Version, 10)+`"`)
	writeTelegramData(w, view)
}

// ListInteractionDeliveries returns only bounded owner-filtered safe rows, retaining unknown outcomes as terminal history instead of offering a send retry action.
// ListInteractionDeliveries возвращает лишь bounded owner-filtered safe rows, сохраняя unknown outcomes как terminal history без действия повторной отправки.
func (server *Server) ListInteractionDeliveries(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, params generated.ListInteractionDeliveriesParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	for key, values := range r.URL.Query() {
		if (key != "pageSize" && key != "pageToken") || len(values) != 1 {
			writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
			return
		}
	}
	limit := 50
	if params.PageSize != nil {
		limit = int(*params.PageSize)
	}
	if limit < 1 || limit > 100 || params.PageToken != nil && len(*params.PageToken) > 512 {
		writeLocalProblem(w, http.StatusBadRequest, telegramInvalidRequest, false)
		return
	}
	response, err := server.control.Query.ListInteractionDeliveries(r.Context(), &cp.ListInteractionDeliveriesRequest{ProjectRef: ref, Page: page(params.PageSize, params.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.GetItems()) > limit || len(response.GetPage().GetNextPageToken()) > 512 {
		writeLocalProblem(w, http.StatusBadGateway, telegramInvalidUpstream, false)
		return
	}
	items := []generated.InteractionDeliveryView{}
	seen := map[string]bool{}
	for _, item := range response.GetItems() {
		view, ok := interactionDeliveryView(item)
		if !ok || seen[view.Ref] {
			writeLocalProblem(w, http.StatusBadGateway, telegramInvalidUpstream, false)
			return
		}
		seen[view.Ref] = true
		items = append(items, view)
	}
	data := map[string]any{"items": items}
	if next := response.GetPage().GetNextPageToken(); next != "" {
		data["nextPageToken"] = next
	}
	writeTelegramData(w, data)
}

// interactionDeliveryView validates a safe status receipt while excluding raw provider results, private coordinates and owner snapshots from HTTP.
// interactionDeliveryView проверяет безопасную status receipt, исключая raw provider results, private coordinates и owner snapshots из HTTP.
func interactionDeliveryView(item *cp.InteractionDeliveryView) (generated.InteractionDeliveryView, bool) {
	var view generated.InteractionDeliveryView
	if item == nil || !opaqueHTTPReference.MatchString(item.GetRef()) || !opaqueHTTPReference.MatchString(item.GetConnectionRef()) ||
		!opaqueHTTPReference.MatchString(item.GetRunRef()) || !validManagedVersion(item.GetVersion()) || item.GetAttempt() < 0 || item.GetAttempt() > 10 ||
		item.GetCreatedAt() == nil || item.GetCreatedAt().CheckValid() != nil || item.GetAvailableAt() == nil || item.GetAvailableAt().CheckValid() != nil ||
		len(item.GetSafeErrorCode()) > 96 {
		return view, false
	}
	if item.GetGateRef() != "" && !opaqueHTTPReference.MatchString(item.GetGateRef()) || item.GetReceiptRef() != "" && !opaqueHTTPReference.MatchString(item.GetReceiptRef()) {
		return view, false
	}
	switch item.GetEventType() {
	case "", "FINISHED", "FAILED", "HUMAN_GATE":
	default:
		return view, false
	}
	switch item.GetState() {
	case "WAITING_APPROVAL", "DUE", "CLAIMED", "SUCCEEDED", "FAILED", "CANCELLED", "UNKNOWN_OUTCOME":
	default:
		return view, false
	}
	if item.GetRetryable() && item.GetState() != "FAILED" {
		return view, false
	}
	for _, character := range item.GetSafeErrorCode() {
		if character != '_' && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return view, false
		}
	}
	view = generated.InteractionDeliveryView{Ref: item.GetRef(), Version: item.GetVersion(), ConnectionRef: item.GetConnectionRef(), RunRef: item.GetRunRef(), GateRef: item.GetGateRef(),
		EventType: generated.InteractionDeliveryViewEventType(item.GetEventType()), State: generated.InteractionDeliveryViewState(item.GetState()), Attempt: int(item.GetAttempt()),
		Retryable: item.GetRetryable(), SafeErrorCode: item.GetSafeErrorCode(), CreatedAt: item.GetCreatedAt().AsTime(), AvailableAt: item.GetAvailableAt().AsTime(), ReceiptRef: item.GetReceiptRef()}
	if item.GetCompletedAt() != nil {
		if item.GetCompletedAt().CheckValid() != nil {
			return view, false
		}
		stamp := item.GetCompletedAt().AsTime()
		view.CompletedAt = &stamp
	}
	return view, true
}
