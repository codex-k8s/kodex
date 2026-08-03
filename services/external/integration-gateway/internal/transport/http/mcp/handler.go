package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	controlclient "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/clients/controlplane"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	domainservice "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/service/gateway"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	authorityExtraKey  = "mattercodex_authority"
	idempotencyMetaKey = "mattercodex/idempotency_key"
)

var receiptSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "invocation_id":{"type":"string"},
    "status":{"type":"string"},
    "request_hash":{"type":"string"},
    "approval_id":{"type":"string"},
    "poll_path":{"type":"string"}
  },
  "required":["invocation_id","status","request_hash","poll_path"]
}`)

var invocationViewSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "invocation_id":{"type":"string"},
    "status":{"type":"string","enum":["PENDING_APPROVAL","APPROVED","REJECTED","EXECUTING","SUCCEEDED","FAILED","UNKNOWN","CANCELLED","EXPIRED"]},
    "request_hash":{"type":"string","pattern":"^[0-9a-f]{64}$"},
    "preview":{"type":"object"},
    "approval":{
      "type":"object",
      "additionalProperties":false,
      "properties":{
        "approval_id":{"type":"string"},
        "status":{"type":"string","enum":["PENDING","APPROVED","REJECTED","CANCELLED","EXPIRED"]},
        "request_hash":{"type":"string","pattern":"^[0-9a-f]{64}$"},
        "preview":{"type":"object"},
        "expires_at":{"type":"string"},
        "decided_at":{"type":"string"},
        "reason_code":{"type":"string"}
      },
      "required":["approval_id","status","request_hash","preview","expires_at"]
    },
    "result":{},
    "result_digest":{"type":"string","pattern":"^[0-9a-f]{64}$"},
    "completed_at":{"type":"string"}
  },
  "required":["invocation_id","status","request_hash","preview"]
}`)

var directDeliverySchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "deliveries":{
      "type":"array",
      "items":{
        "type":"object",
        "additionalProperties":false,
        "properties":{
          "tool":{"type":"string"},
          "description":{"type":"string"},
          "reference":{"type":"string"},
          "cli_names":{"type":"array","items":{"type":"string"}},
          "environment_names":{"type":"array","items":{"type":"string"}}
        },
        "required":["tool","description","reference","cli_names","environment_names"]
      }
    }
  },
  "required":["deliveries"]
}`)

var connectionValidationSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "properties":{
    "integration_id":{"type":"string"},
    "status":{"type":"string","enum":["VALID","INVALID"]},
    "validation_code":{"type":"string","enum":["OK","CREDENTIAL_UNAVAILABLE","UNAUTHORIZED","FORBIDDEN","ENDPOINT_UNAVAILABLE","TIMEOUT","PROTOCOL_ERROR"]},
    "validated_at":{"type":"string"}
  },
  "required":["integration_id","status","validation_code","validated_at"]
}`)

type Config struct {
	MaximumBodyBytes         int64
	RequestDeadline          time.Duration
	MaximumGlobalConcurrency int
}

type Handler struct {
	service    *domainservice.Service
	control    *controlclient.Client
	streamable http.Handler
	config     Config
	semaphore  chan struct{}
}

type admission struct {
	authority          domainservice.Authority
	scope              domainrepo.Scope
	transportSessionID string
}

type admissionContextKey struct{}

func New(service *domainservice.Service, control *controlclient.Client, config Config) (*Handler, error) {
	if service == nil || control == nil || config.MaximumBodyBytes < 1024 || config.MaximumBodyBytes > 1<<20 ||
		config.RequestDeadline < time.Second || config.RequestDeadline > time.Minute ||
		config.MaximumGlobalConcurrency < 1 || config.MaximumGlobalConcurrency > 1024 {
		return nil, errors.New("MCP handler configuration is invalid")
	}
	handler := &Handler{
		service: service, control: control, config: config,
		semaphore: make(chan struct{}, config.MaximumGlobalConcurrency),
	}
	handler.streamable = mcpsdk.NewStreamableHTTPHandler(handler.server, &mcpsdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	return handler, nil
}

func (handler *Handler) HTTPHandler() http.Handler {
	protected := auth.RequireBearerToken(handler.verifyBearer, &auth.RequireBearerTokenOptions{
		Scopes: []string{"mcp:invoke"},
	})(http.HandlerFunc(handler.admit))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, handler.config.MaximumBodyBytes)
		select {
		case handler.semaphore <- struct{}{}:
			defer func() { <-handler.semaphore }()
		default:
			http.Error(response, "gateway concurrency limit exceeded", http.StatusTooManyRequests)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), handler.config.RequestDeadline)
		defer cancel()
		protected.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (handler *Handler) verifyBearer(ctx context.Context, bearer string, _ *http.Request) (*auth.TokenInfo, error) {
	authority, err := handler.control.Resolve(ctx, bearer)
	if err != nil {
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return nil, errors.New("authorization dependency is unavailable")
		}
		return nil, fmt.Errorf("%w: application grant is invalid", auth.ErrInvalidToken)
	}
	return &auth.TokenInfo{
		Scopes:     []string{"mcp:invoke"},
		Expiration: time.Now().UTC().Add(time.Minute),
		UserID:     authority.SessionID + "/" + authority.TurnID,
		Extra:      map[string]any{authorityExtraKey: &authority},
	}, nil
}

func (handler *Handler) admit(response http.ResponseWriter, request *http.Request) {
	tokenInfo := auth.TokenInfoFromContext(request.Context())
	authority, ok := tokenInfo.Extra[authorityExtraKey].(*domainservice.Authority)
	if !ok || authority == nil {
		http.Error(response, "authorization context is invalid", http.StatusUnauthorized)
		return
	}
	scope := domainrepo.Scope{TenantID: authority.TenantID, ProjectID: authority.ProjectID, ActorID: authority.OwnerActorID}
	sessionID := request.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		if request.Method != http.MethodPost {
			http.Error(response, "MCP session is required", http.StatusBadRequest)
			return
		}
		method, err := readRequestMethod(request)
		if err != nil || method != "initialize" {
			http.Error(response, "MCP initialize request is required", http.StatusBadRequest)
			return
		}
		sessionID = uuid.NewString()
		if _, err := handler.service.AdmitSession(request.Context(), *authority, sessionID); err != nil {
			writeAdmissionError(response, err)
			return
		}
		defer func() { _ = handler.service.ReleaseSession(request.Context(), scope, sessionID) }()
	} else {
		if uuid.Validate(sessionID) != nil {
			http.Error(response, "MCP session is not permitted", http.StatusForbidden)
			return
		}
		if request.Method == http.MethodPost {
			method, err := readRequestMethod(request)
			if err != nil {
				http.Error(response, "MCP request is invalid", http.StatusBadRequest)
				return
			}
			if method == "initialize" {
				http.Error(response, "MCP session is already initialized", http.StatusConflict)
				return
			}
		}
		if _, err := handler.service.TouchSession(request.Context(), scope, sessionID, authority.TokenDigest, authority.ApplicationGrantExpiresAt); err != nil {
			writeAdmissionError(response, err)
			return
		}
		defer func() { _ = handler.service.ReleaseSession(request.Context(), scope, sessionID) }()
	}
	if request.Method == http.MethodDelete {
		if err := handler.service.CloseSession(request.Context(), scope, sessionID); err != nil {
			writeAdmissionError(response, err)
			return
		}
	}
	ctx := context.WithValue(request.Context(), admissionContextKey{}, admission{
		authority: *authority, scope: scope, transportSessionID: sessionID,
	})
	handler.streamable.ServeHTTP(response, request.WithContext(ctx))
}

func readRequestMethod(request *http.Request) (string, error) {
	if request.Body == nil {
		return "", errors.New("MCP request body is required")
	}
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return "", errors.New("read MCP request body")
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	var envelope struct {
		Method string `json:"method"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&envelope); err != nil {
		return "", errors.New("decode MCP request envelope")
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return "", errors.New("trailing MCP request data is forbidden")
	}
	return envelope.Method, nil
}

func (handler *Handler) server(request *http.Request) *mcpsdk.Server {
	admitted, ok := request.Context().Value(admissionContextKey{}).(admission)
	if !ok {
		return nil
	}
	bindings, err := handler.service.ListTools(request.Context(), admitted.scope, admitted.transportSessionID)
	if err != nil {
		return nil
	}
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "mattercodex-integration-gateway", Version: "v1"}, &mcpsdk.ServerOptions{
		GetSessionID: func() string { return admitted.transportSessionID },
	})
	server.AddTool(&mcpsdk.Tool{
		Name:         "mattercodex-validate-connection",
		Description:  "Validate one integration connection from this exact runtime revision without exposing credential values.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"integration_id":{"type":"string"}},"required":["integration_id"]}`),
		OutputSchema: connectionValidationSchema,
	}, func(ctx context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var input struct {
			IntegrationID string `json:"integration_id"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(request.Params.Arguments)))
		decoder.DisallowUnknownFields()
		var trailing any
		if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) || uuid.Validate(input.IntegrationID) != nil {
			return toolError(errs.ErrInvalid), nil
		}
		connectionID := ""
		for _, connection := range admitted.authority.Connections {
			if connection.IntegrationID == input.IntegrationID {
				connectionID = connection.ID
				break
			}
		}
		if connectionID == "" {
			return toolError(errs.ErrForbidden), nil
		}
		connection, err := handler.service.ValidateConnection(ctx, admitted.scope, connectionID)
		if err != nil {
			return toolError(err), nil
		}
		structured := map[string]any{
			"integration_id": input.IntegrationID, "status": connection.Status,
			"validation_code": connection.ValidationCode, "validated_at": connection.ValidatedAt,
		}
		raw, _ := json.Marshal(structured)
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(raw)}}, StructuredContent: structured}, nil
	})
	for _, binding := range bindings {
		bound := binding
		server.AddTool(&mcpsdk.Tool{
			Name:         bound.Tool.Name,
			Description:  bound.Tool.Description,
			InputSchema:  bound.Tool.InputSchema,
			OutputSchema: receiptSchema,
		}, func(ctx context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			semanticKey, _ := request.Params.Meta[idempotencyMetaKey].(string)
			receipt, err := handler.service.Invoke(ctx, domainservice.InvokeInput{
				Scope: admitted.scope, TransportSessionID: admitted.transportSessionID,
				Authority: admitted.authority,
				Tool:      bound.Tool, Connection: bound.Connection, Grant: bound.Grant,
				DefinitionDigest: bound.DefinitionDigest,
				Arguments:        request.Params.Arguments, SemanticKey: semanticKey,
			})
			if err != nil {
				return toolError(err), nil
			}
			structured := map[string]any{
				"invocation_id": receipt.InvocationID, "status": receipt.Status,
				"request_hash": receipt.RequestHash, "poll_path": receipt.PollPath,
			}
			if receipt.ApprovalID != "" {
				structured["approval_id"] = receipt.ApprovalID
			}
			raw, _ := json.Marshal(structured)
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(raw)}}, StructuredContent: structured}, nil
		})
	}
	server.AddTool(&mcpsdk.Tool{
		Name:         "mattercodex-list-safe-delivery",
		Description:  "List allowed safe CLI and environment references without credential values.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
		OutputSchema: directDeliverySchema,
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		deliveries := make([]map[string]any, 0)
		for _, binding := range bindings {
			if binding.Tool.DirectDelivery == nil {
				continue
			}
			deliveries = append(deliveries, map[string]any{
				"tool": binding.Tool.Name, "description": binding.Tool.Description,
				"reference":         binding.Tool.DirectDelivery.Reference,
				"cli_names":         binding.Tool.DirectDelivery.CLINames,
				"environment_names": binding.Tool.DirectDelivery.EnvironmentNames,
			})
		}
		structured := map[string]any{"deliveries": deliveries}
		raw, _ := json.Marshal(structured)
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(raw)}}, StructuredContent: structured}, nil
	})
	server.AddTool(&mcpsdk.Tool{
		Name:         "mattercodex-cancel-invocation",
		Description:  "Cancel a pending invocation owned by this exact MCP transport session before execution starts.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"invocation_id":{"type":"string"}},"required":["invocation_id"]}`),
		OutputSchema: receiptSchema,
	}, func(ctx context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var input struct {
			InvocationID string `json:"invocation_id"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(request.Params.Arguments)))
		decoder.DisallowUnknownFields()
		var trailing any
		if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) || uuid.Validate(input.InvocationID) != nil {
			return toolError(errs.ErrInvalid), nil
		}
		semanticKey, _ := request.Params.Meta[idempotencyMetaKey].(string)
		receipt, err := handler.service.Cancel(ctx, admitted.scope, input.InvocationID, admitted.transportSessionID, "OWNER_CANCELLED", semanticKey)
		if err != nil {
			return toolError(err), nil
		}
		raw, _ := json.Marshal(receipt)
		var structured map[string]any
		_ = json.Unmarshal(raw, &structured)
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(raw)}}, StructuredContent: structured}, nil
	})
	server.AddTool(&mcpsdk.Tool{
		Name:         "mattercodex-get-invocation",
		Description:  "Read the durable status or validated structured result of an invocation from this MCP session.",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"invocation_id":{"type":"string"}},"required":["invocation_id"]}`),
		OutputSchema: invocationViewSchema,
	}, func(ctx context.Context, request *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
		var input struct {
			InvocationID string `json:"invocation_id"`
		}
		decoder := json.NewDecoder(strings.NewReader(string(request.Params.Arguments)))
		decoder.DisallowUnknownFields()
		var trailing any
		if decoder.Decode(&input) != nil || !errors.Is(decoder.Decode(&trailing), io.EOF) || uuid.Validate(input.InvocationID) != nil {
			return toolError(errs.ErrInvalid), nil
		}
		view, err := handler.service.ReadInvocation(ctx, admitted.scope, input.InvocationID, admitted.transportSessionID)
		if err != nil {
			return toolError(err), nil
		}
		raw, _ := json.Marshal(view)
		var structured map[string]any
		_ = json.Unmarshal(raw, &structured)
		return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(raw)}}, StructuredContent: structured}, nil
	})
	return server
}

func toolError(err error) *mcpsdk.CallToolResult {
	code := "INTERNAL"
	switch {
	case errors.Is(err, errs.ErrInvalid):
		code = "INVALID_ARGUMENTS"
	case errors.Is(err, errs.ErrForbidden):
		code = "FORBIDDEN"
	case errors.Is(err, errs.ErrConflict):
		code = "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, errs.ErrQuotaExceeded):
		code = "QUOTA_EXCEEDED"
	case errors.Is(err, errs.ErrExpired):
		code = "SESSION_EXPIRED"
	}
	return &mcpsdk.CallToolResult{
		IsError: true, Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: `{"error":"` + code + `"}`}},
		StructuredContent: map[string]any{"error": code},
	}
}

func writeAdmissionError(response http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	message := "internal server error"
	switch {
	case errors.Is(err, errs.ErrForbidden), errors.Is(err, errs.ErrNotFound):
		statusCode, message = http.StatusForbidden, "MCP session is not permitted"
	case errors.Is(err, errs.ErrExpired):
		statusCode, message = http.StatusNotFound, "MCP session expired"
	case errors.Is(err, errs.ErrQuotaExceeded):
		statusCode, message = http.StatusTooManyRequests, "MCP session quota exceeded"
	case errors.Is(err, context.DeadlineExceeded):
		statusCode, message = http.StatusGatewayTimeout, "MCP request deadline exceeded"
	}
	http.Error(response, message, statusCode)
}
