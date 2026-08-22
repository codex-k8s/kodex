// Package callback обслуживает только execution-scoped mTLS+ticket callbacks role runtime.
package callback

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/controlplaneclient"
	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/internal/runtime-controller/internal/workload"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

const maximumRequestBytes = 16 << 20

var progressCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)

type Config struct {
	Listen, CertificateFile, PrivateKeyFile, ClientCAFile, ExpectedClientSPIFFEID string
	RequestTimeout, WarmLongPoll                                                  time.Duration
}

// Coordinator связывает leader claim loop с callbacks, не становясь owner store.
// После restart leases истекают в control-plane и материализуются заново.
type Coordinator struct {
	mu   sync.Mutex
	warm []runtimecontract.RunnerInput
	wake chan struct{}
	done map[string]chan struct{}
}

func NewCoordinator() *Coordinator {
	return &Coordinator{wake: make(chan struct{}, 1), done: make(map[string]chan struct{})}
}

func (coordinator *Coordinator) Register(input runtimecontract.RunnerInput) <-chan struct{} {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if existing := coordinator.done[input.LeaseRef]; existing != nil {
		return existing
	}
	done := make(chan struct{})
	coordinator.done[input.LeaseRef] = done
	return done
}

func (coordinator *Coordinator) EnqueueWarm(input runtimecontract.RunnerInput) error {
	if input.Mode != runtimecontract.RunnerModeTurn || !input.SystemAssistant || input.Validate() != nil {
		return errors.New("warm execution input is invalid")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	for _, current := range coordinator.warm {
		if current.LeaseRef == input.LeaseRef {
			return nil
		}
	}
	if len(coordinator.warm) >= 16 {
		return errors.New("warm execution queue is full")
	}
	coordinator.warm = append(coordinator.warm, input)
	select {
	case coordinator.wake <- struct{}{}:
	default:
	}
	return nil
}

func (coordinator *Coordinator) NextWarm(ctx context.Context, revisionDigest string) (runtimecontract.RunnerInput, bool) {
	for {
		coordinator.mu.Lock()
		for index, input := range coordinator.warm {
			if input.RuntimeRevisionDigest == revisionDigest {
				coordinator.warm = append(coordinator.warm[:index], coordinator.warm[index+1:]...)
				coordinator.mu.Unlock()
				return input, true
			}
		}
		coordinator.mu.Unlock()
		select {
		case <-ctx.Done():
			return runtimecontract.RunnerInput{}, false
		case <-coordinator.wake:
		}
	}
}

func (coordinator *Coordinator) Complete(leaseRef string) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if done := coordinator.done[leaseRef]; done != nil {
		close(done)
		delete(coordinator.done, leaseRef)
	}
}

type Server struct {
	config      Config
	manager     *workload.Manager
	control     *controlplaneclient.Client
	coordinator *Coordinator
	logger      *slog.Logger
	http        *http.Server
}

func New(config Config, manager *workload.Manager, control *controlplaneclient.Client, coordinator *Coordinator, logger *slog.Logger) (*Server, error) {
	if manager == nil || control == nil || coordinator == nil || logger == nil || config.Listen == "" ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second ||
		config.WarmLongPoll < time.Second || config.WarmLongPoll > 30*time.Second {
		return nil, errors.New("runtime callback configuration is invalid")
	}
	tlsConfig, err := serverTLS(config)
	if err != nil {
		return nil, err
	}
	server := &Server{config: config, manager: manager, control: control, coordinator: coordinator, logger: logger}
	server.http = &http.Server{Addr: config.Listen, Handler: http.HandlerFunc(server.route), TLSConfig: tlsConfig,
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 35 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	return server, nil
}

func (server *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", server.config.Listen)
	if err != nil {
		return errors.New("listen runtime callback")
	}
	tlsListener := tls.NewListener(listener, server.http.TLSConfig)
	done := make(chan error, 1)
	go func() { done <- server.http.Serve(tlsListener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err := server.http.Shutdown(shutdown)
		serveErr := <-done
		if !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, serveErr)
		}
		return err
	}
}

func (server *Server) Shutdown(ctx context.Context) error { return server.http.Shutdown(ctx) }

func (server *Server) route(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.URL.RawQuery != "" || request.URL.Fragment != "" {
		http.NotFound(writer, request)
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/v1/warm/next" {
		server.nextWarm(writer, request)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "executions" || parts[2] == "" {
		http.NotFound(writer, request)
		return
	}
	switch parts[3] {
	case "progress":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.progress(writer, request, parts[2])
	case "complete":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.complete(writer, request, parts[2])
	case "mcp":
		if request.Method != http.MethodPost {
			http.NotFound(writer, request)
			return
		}
		server.mcp(writer, request, parts[2])
	default:
		http.NotFound(writer, request)
	}
}

func (server *Server) nextWarm(writer http.ResponseWriter, request *http.Request) {
	revisionRef := request.Header.Get("X-MatterCodex-Runtime-Revision")
	token, ok := bearer(request)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	bound, err := server.manager.ResolveWarm(request.Context(), revisionRef, token)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	wait, cancel := context.WithTimeout(request.Context(), server.config.WarmLongPoll)
	defer cancel()
	input, available := server.coordinator.NextWarm(wait, bound.RuntimeRevisionDigest)
	writer.Header().Set("Content-Type", "application/json")
	if !available {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	_ = json.NewEncoder(writer).Encode(input)
}

func (server *Server) progress(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	var payload runtimecontract.RunnerProgressRequest
	if decode(request, &payload, runtimecontract.MaximumProgressTextBytes) != nil ||
		payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest || !progressCodePattern.MatchString(payload.Progress) {
		http.Error(writer, "invalid runtime progress", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.RequestTimeout)
	defer cancel()
	_, err := server.control.Runtime.ReportExecutionProgress(ctx, &controlplanev1.ReportExecutionProgressRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Progress: "i18n:" + payload.Progress})
	if err != nil {
		writeControlError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) complete(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	var payload runtimecontract.RunnerCompletionRequest
	if decode(request, &payload, maximumRequestBytes) != nil || payload.Validate() != nil || payload.RuntimeRevisionDigest != input.RuntimeRevisionDigest {
		http.Error(writer, "invalid runtime completion", http.StatusBadRequest)
		return
	}
	artifacts := make([]*controlplanev1.CompletedArtifactInput, 0, len(payload.Artifacts))
	for _, artifact := range payload.Artifacts {
		artifacts = append(artifacts, &controlplanev1.CompletedArtifactInput{FileName: artifact.FileName, MediaType: artifact.MediaType, SizeBytes: int64(len(artifact.Content)), Content: artifact.Content, Sha256: artifact.SHA256})
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(request.Context()), server.config.RequestTimeout)
	defer cancel()
	_, err := server.control.Runtime.CompleteExecution(ctx, &controlplanev1.CompleteExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, "complete")}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, Success: payload.Success, ResultSummary: payload.ResultSummary, SafeErrorCode: payload.SafeErrorCode, Artifacts: artifacts})
	if err != nil && status.Code(err) != codes.AlreadyExists {
		writeControlError(writer, err)
		return
	}
	server.coordinator.Complete(input.LeaseRef)
	go func() {
		cleanup, cleanupCancel := context.WithTimeout(context.WithoutCancel(request.Context()), 10*time.Second)
		defer cleanupCancel()
		if cleanupErr := server.manager.DeleteTurn(cleanup, input.LeaseRef); cleanupErr != nil {
			server.logger.ErrorContext(cleanup, "runtime resource cleanup failed", "error_class", "kubernetes")
		}
	}()
	writer.WriteHeader(http.StatusNoContent)
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (server *Server) mcp(writer http.ResponseWriter, request *http.Request, leaseRef string) {
	input, ok := server.authorize(request, leaseRef)
	if !ok {
		http.NotFound(writer, request)
		return
	}
	var rpc mcpRequest
	if decode(request, &rpc, 1<<20) != nil || rpc.JSONRPC != "2.0" || len(rpc.ID) == 0 {
		server.writeMCPError(writer, rpc.ID, -32600, "Invalid Request")
		return
	}
	switch rpc.Method {
	case "initialize":
		server.writeMCPResult(writer, rpc.ID, map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}}, "serverInfo": map[string]string{"name": "mattercodex-runtime-tools", "version": "1"}})
	case "tools/list":
		server.writeMCPResult(writer, rpc.ID, map[string]any{"tools": tools(input)})
	case "tools/call":
		server.callTool(writer, request, rpc, input)
	default:
		server.writeMCPError(writer, rpc.ID, -32601, "Method not found")
	}
}

func tools(input runtimecontract.RunnerInput) []map[string]any {
	result := []map[string]any{}
	if input.SystemAssistant {
		result = append(result, assistantPlanTool())
	}
	if len(input.DelegationTargets) != 0 {
		result = append(result, map[string]any{"name": "delegate_agent", "description": "Start an allowed child AI employee.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"target_agent_ref", "task"}, "properties": map[string]any{"target_agent_ref": map[string]string{"type": "string"}, "task": map[string]string{"type": "string"}, "input": map[string]string{"type": "object"}}}})
	}
	if len(input.IntegrationGrants) != 0 {
		result = append(result, map[string]any{"name": "invoke_integration", "description": "Invoke an allowed typed integration capability.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"connection_ref", "capability_key", "input"}, "properties": map[string]any{"connection_ref": map[string]string{"type": "string"}, "capability_key": map[string]string{"type": "string"}, "input": map[string]string{"type": "object"}}}})
	}
	return result
}

func (server *Server) callTool(writer http.ResponseWriter, request *http.Request, rpc mcpRequest, input runtimecontract.RunnerInput) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	decoder := json.NewDecoder(bytes.NewReader(rpc.Params))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&params) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		server.writeMCPError(writer, rpc.ID, -32602, "Invalid params")
		return
	}
	var result any
	var err error
	switch params.Name {
	case "propose_configuration_plan":
		result, err = server.proposeAssistantPlan(request.Context(), input, params.Arguments, rpc.ID)
	case "delegate_agent":
		result, err = server.delegate(request.Context(), input, params.Arguments, rpc.ID)
	case "invoke_integration":
		result, err = server.invoke(request.Context(), input, params.Arguments, rpc.ID)
	default:
		err = errors.New("tool is not available")
	}
	encoded, _ := json.Marshal(result)
	if err != nil {
		encoded, _ = json.Marshal(map[string]string{"error_code": "TOOL_UNAVAILABLE"})
	}
	server.writeMCPResult(writer, rpc.ID, map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "isError": err != nil})
}

func (server *Server) proposeAssistantPlan(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	if !input.SystemAssistant || !onlyKeys(arguments, "summary", "operations") {
		return nil, errors.New("assistant plan tool is not available")
	}
	summary, _ := arguments["summary"].(string)
	rawOperations, _ := arguments["operations"].([]any)
	if strings.TrimSpace(summary) == "" || len(summary) > 2000 || len(rawOperations) == 0 || len(rawOperations) > 32 {
		return nil, errors.New("assistant plan is invalid")
	}
	operations := make([]*controlplanev1.AssistantPlanOperation, 0, len(rawOperations))
	for index, raw := range rawOperations {
		operation, ok := raw.(map[string]any)
		if !ok || !onlyKeys(operation, "type", "summary", "input") {
			return nil, errors.New("assistant plan operation is invalid")
		}
		kind, _ := operation["type"].(string)
		operationSummary, _ := operation["summary"].(string)
		bounded, _ := operation["input"].(map[string]any)
		typeValue, exists := controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+kind]
		if !exists || typeValue == 0 || strings.TrimSpace(operationSummary) == "" || len(operationSummary) > 500 || bounded == nil {
			return nil, errors.New("assistant plan operation is invalid")
		}
		structure, err := structpb.NewStruct(bounded)
		if err != nil {
			return nil, errors.New("assistant plan operation input is invalid")
		}
		operations = append(operations, &controlplanev1.AssistantPlanOperation{Ref: fmt.Sprintf("operation-%03d", index+1),
			Type: controlplanev1.AssistantPlanOperation_Type(typeValue), Summary: strings.TrimSpace(operationSummary), BoundedInput: structure})
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.ProposeAssistantPlan(requestContext, &controlplanev1.ProposeAssistantPlanRequest{
		Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))},
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration,
		Summary: strings.TrimSpace(summary), Operations: operations,
	})
	if err != nil || response.GetPlan().GetRef() == "" || response.GetConversation().GetRef() == "" {
		return nil, errors.New("propose assistant plan")
	}
	return map[string]any{"ok": true, "plan_ref": response.GetPlan().GetRef(), "plan_version": response.GetPlan().GetVersion(),
		"conversation_ref": response.GetConversation().GetRef()}, nil
}

func onlyKeys(values map[string]any, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range values {
		if _, ok := known[key]; !ok {
			return false
		}
	}
	return true
}

func (server *Server) delegate(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	target, _ := arguments["target_agent_ref"].(string)
	task, _ := arguments["task"].(string)
	allowed := false
	for _, item := range input.DelegationTargets {
		if item.Ref == target {
			allowed = true
			break
		}
	}
	if !allowed || strings.TrimSpace(task) == "" || len(task) > 64<<10 {
		return nil, errors.New("delegation is not allowed")
	}
	bounded, _ := arguments["input"].(map[string]any)
	structure, err := structpb.NewStruct(bounded)
	if err != nil {
		return nil, errors.New("delegation input is invalid")
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.DelegateExecution(requestContext, &controlplanev1.DelegateExecutionRequest{Mutation: &controlplanev1.MutationContext{IdempotencyKey: stableKey(input.LeaseRef, string(callID))}, LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, TargetAgentRef: target, Task: task, Input: structure})
	if err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "child_run_ref": response.GetChildRun().GetRef(), "callback_edge_ref": response.GetCallbackEdgeRef()}, nil
}

func (server *Server) invoke(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	connection, _ := arguments["connection_ref"].(string)
	capability, _ := arguments["capability_key"].(string)
	allowed := false
	for _, grant := range input.IntegrationGrants {
		if grant.ConnectionRef == connection && grant.CapabilityKey == capability && grant.Risk != "HIGH" {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, errors.New("integration capability is not allowed")
	}
	bounded, _ := arguments["input"].(map[string]any)
	structure, err := structpb.NewStruct(bounded)
	if err != nil {
		return nil, errors.New("integration input is invalid")
	}
	requestContext, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	resolved, err := server.control.Runtime.ResolveIntegrationInvocation(requestContext, &controlplanev1.ResolveIntegrationInvocationRequest{RunRef: input.RunRef, NodeRef: input.NodeRef, ConnectionRef: connection, CapabilityKey: capability, BoundedInput: structure, IdempotencyKey: stableKey(input.LeaseRef, string(callID))})
	cancel()
	if err != nil || resolved.GetInvocationRef() == "" {
		return nil, errors.New("resolve integration invocation")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		readContext, readCancel := context.WithTimeout(ctx, server.config.RequestTimeout)
		state, readErr := server.control.Runtime.GetIntegrationInvocation(readContext, &controlplanev1.GetIntegrationInvocationRequest{InvocationRef: resolved.GetInvocationRef()})
		readCancel()
		if readErr != nil {
			return nil, errors.New("read integration invocation")
		}
		switch state.GetState() {
		case "SUCCEEDED":
			return map[string]any{"ok": true, "result": state.GetResultSummary()}, nil
		case "FAILED", "CANCELLED":
			return map[string]any{"ok": false, "error_code": state.GetSafeErrorCode()}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (server *Server) authorize(request *http.Request, leaseRef string) (runtimecontract.RunnerInput, bool) {
	token, ok := bearer(request)
	if !ok {
		return runtimecontract.RunnerInput{}, false
	}
	input, err := server.manager.ResolveTurn(request.Context(), leaseRef, token)
	return input, err == nil
}

func bearer(request *http.Request) (string, bool) {
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || len(header) != len("Bearer ")+64 {
		return "", false
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if _, err := hex.DecodeString(token); err != nil {
		return "", false
	}
	return token, true
}

func decode(request *http.Request, target any, maximum int64) error {
	defer request.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(request.Body, maximum+1))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return errors.New("request body is invalid")
	}
	return nil
}

func stableKey(left, right string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(left+"\x00"+right)).String()
}

func writeControlError(writer http.ResponseWriter, err error) {
	switch status.Code(err) {
	case codes.InvalidArgument:
		http.Error(writer, "invalid runtime request", http.StatusBadRequest)
	case codes.NotFound:
		http.Error(writer, "not found", http.StatusNotFound)
	case codes.PermissionDenied, codes.Unauthenticated:
		http.Error(writer, "not found", http.StatusNotFound)
	case codes.Aborted, codes.AlreadyExists, codes.FailedPrecondition:
		http.Error(writer, "runtime state conflict", http.StatusConflict)
	case codes.DeadlineExceeded:
		http.Error(writer, "runtime owner timed out", http.StatusGatewayTimeout)
	default:
		http.Error(writer, "runtime owner unavailable", http.StatusServiceUnavailable)
	}
}

func (server *Server) writeMCPResult(writer http.ResponseWriter, id json.RawMessage, result any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (server *Server) writeMCPError(writer http.ResponseWriter, id json.RawMessage, code int, message string) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func serverTLS(config Config) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(config.CertificateFile, config.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("load runtime callback server identity")
	}
	ca, err := os.ReadFile(config.ClientCAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read runtime callback client CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse runtime callback client CA")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
				return errors.New("runtime callback client certificate is unverified")
			}
			for _, identity := range state.VerifiedChains[0][0].URIs {
				if subtle.ConstantTimeCompare([]byte(identity.String()), []byte(config.ExpectedClientSPIFFEID)) == 1 {
					return nil
				}
			}
			return errors.New("runtime callback client identity is invalid")
		}}, nil
}
