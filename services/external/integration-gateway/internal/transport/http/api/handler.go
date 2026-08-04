package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/authorization/oidc"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/transport/http/generated"
)

const maximumBodyBytes = 16 << 10

type Handler struct {
	service *domainservice.Service
	auth    *oidc.Verifier
	router  http.Handler
}

type principalContextKey struct{}

var _ generated.ServerInterface = (*Handler)(nil)

func New(service *domainservice.Service, verifier *oidc.Verifier) (*Handler, error) {
	if service == nil || verifier == nil {
		return nil, errors.New("API handler dependencies are required")
	}
	handler := &Handler{service: service, auth: verifier}
	handler.router = generated.HandlerWithOptions(handler, generated.StdHTTPServerOptions{
		BaseURL: "/api/v1",
		ErrorHandlerFunc: func(response http.ResponseWriter, _ *http.Request, _ error) {
			writeError(response, http.StatusBadRequest, "INVALID_REQUEST")
		},
	})
	return handler, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	principal, err := handler.auth.Verify(request.Context(), request.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			writeError(response, http.StatusGatewayTimeout, "DEADLINE_EXCEEDED")
			return
		}
		writeError(response, http.StatusUnauthorized, "UNAUTHENTICATED")
		return
	}
	ctx := request.Context()
	ctx = contextWithPrincipal(ctx, principal)
	handler.router.ServeHTTP(response, request.WithContext(ctx))
}

func (handler *Handler) DecideIntegrationApproval(
	response http.ResponseWriter,
	request *http.Request,
	approvalID generated.ApprovalID,
) {
	principal, ok := requestPrincipal(request)
	if !ok || !slices.Contains(principal.Permissions, "integration.approval.decide") {
		writeError(response, http.StatusForbidden, "FORBIDDEN")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maximumBodyBytes)
	var input generated.ApprovalDecision
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var trailing any
	if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) ||
		!input.Decision.Valid() || !input.ReasonCode.Valid() {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	invocation, err := handler.service.Decide(
		request.Context(),
		principal.Scope,
		approvalID.String(),
		input.Decision == generated.APPROVE,
		string(input.ReasonCode),
		input.IdempotencyKey.String(),
	)
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, generated.InvocationStatus{
		InvocationId: invocationIDFromString(invocation.ID),
		Status:       generated.InvocationStatusStatus(invocation.Status),
		RequestHash:  invocation.CanonicalRequestHash,
	})
}

func (handler *Handler) CancelIntegrationInvocation(
	response http.ResponseWriter,
	request *http.Request,
	invocationID generated.InvocationID,
) {
	principal, ok := requestPrincipal(request)
	if !ok || !slices.Contains(principal.Permissions, "integration.invocation.cancel") {
		writeError(response, http.StatusForbidden, "FORBIDDEN")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maximumBodyBytes)
	var input generated.InvocationCancellation
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var trailing any
	if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) || !input.ReasonCode.Valid() {
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	receipt, err := handler.service.Cancel(request.Context(), principal.Scope, invocationID.String(), "", string(input.ReasonCode), input.IdempotencyKey.String())
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, generated.InvocationStatus{
		InvocationId: invocationIDFromString(receipt.InvocationID),
		Status:       generated.InvocationStatusStatus(receipt.Status),
		RequestHash:  receipt.RequestHash,
	})
}

func (handler *Handler) GetIntegrationInvocation(
	response http.ResponseWriter,
	request *http.Request,
	invocationID generated.InvocationID,
) {
	principal, ok := requestPrincipal(request)
	if !ok || !slices.Contains(principal.Permissions, "integration.invocation.read") {
		writeError(response, http.StatusForbidden, "FORBIDDEN")
		return
	}
	view, err := handler.service.ReadInvocation(request.Context(), principal.Scope, invocationID.String(), "")
	if err != nil {
		writeDomainError(response, err)
		return
	}
	view.Result = nil
	writeJSON(response, http.StatusOK, view)
}

func (handler *Handler) ValidateIntegrationConnection(
	response http.ResponseWriter,
	request *http.Request,
	connectionID generated.ConnectionID,
) {
	principal, ok := requestPrincipal(request)
	if !ok || !slices.Contains(principal.Permissions, "integration.connection.validate") {
		writeError(response, http.StatusForbidden, "FORBIDDEN")
		return
	}
	connection, err := handler.service.ValidateConnection(request.Context(), principal.Scope, connectionID.String())
	if err != nil {
		writeDomainError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"connection_id":   connection.ID,
		"status":          connection.Status,
		"validation_code": connection.ValidationCode,
		"validated_at":    connection.ValidatedAt,
	})
}

func (handler *Handler) GetIntegrationGatewayDiagnostics(response http.ResponseWriter, request *http.Request) {
	principal, ok := requestPrincipal(request)
	if !ok || !slices.Contains(principal.Permissions, "integration.diagnostics.read") {
		writeError(response, http.StatusForbidden, "FORBIDDEN")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"status": "OK"})
}

func contextWithPrincipal(ctx context.Context, principal oidc.Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func requestPrincipal(request *http.Request) (oidc.Principal, bool) {
	value, ok := request.Context().Value(principalContextKey{}).(oidc.Principal)
	return value, ok
}

func invocationIDFromString(value string) generated.InvocationID {
	var identifier generated.InvocationID
	_ = identifier.UnmarshalText([]byte(value))
	return identifier
}

func writeDomainError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(response, http.StatusGatewayTimeout, "DEADLINE_EXCEEDED")
	case errors.Is(err, errs.ErrInvalid):
		writeError(response, http.StatusBadRequest, "INVALID_REQUEST")
	case errors.Is(err, errs.ErrForbidden):
		writeError(response, http.StatusForbidden, "FORBIDDEN")
	case errors.Is(err, errs.ErrNotFound):
		writeError(response, http.StatusNotFound, "NOT_FOUND")
	case errors.Is(err, errs.ErrConflict):
		writeError(response, http.StatusConflict, "CONFLICT")
	case errors.Is(err, errs.ErrExpired):
		writeError(response, http.StatusGone, "EXPIRED")
	case errors.Is(err, errs.ErrQuotaExceeded):
		writeError(response, http.StatusTooManyRequests, "QUOTA_EXCEEDED")
	default:
		writeError(response, http.StatusInternalServerError, "INTERNAL")
	}
}

func writeError(response http.ResponseWriter, status int, code string) {
	writeJSON(response, status, generated.Error{Error: code})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
