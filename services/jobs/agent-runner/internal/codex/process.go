package codex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const (
	processGrace          = 10 * time.Second
	terminationGrace      = 2 * time.Second
	maximumArchiveBytes   = 64 << 20
	maximumDiagnosticSize = 1 << 20
	maximumRequestBytes   = 2 << 20
	mcpReadinessTimeout   = 15 * time.Second
	mcpReadinessPoll      = 100 * time.Millisecond
)

var rolloutPathPattern = regexp.MustCompile(`^\.kodex/state/codex-home/sessions/[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-[A-Za-z0-9._-]+\.jsonl$`)

var (
	ErrAuthorityRequestUnsupported = errors.New("Codex app-server authority request is unsupported")
	ErrRequiredMCPUnavailable      = errors.New("Codex required MCP runtime is unavailable")
	errAccountReadResponseInvalid  = errors.New("Codex app-server account/read response is invalid")
)

type streamEvent struct {
	message wireMessage
	err     error
}

type appServer struct {
	command     *exec.Cmd
	stdin       io.WriteCloser
	messages    <-chan streamEvent
	wait        <-chan error
	diagnostics <-chan error
	nextID      int64
}

func executeLocal(ctx context.Context, input model.Input, prompt []byte, mcpProxyToken string) (Result, error) {
	if err := verifyAccountPin(input); err != nil {
		return Result{}, err
	}
	if err := verifyRestoreArchive(input); err != nil {
		return Result{}, err
	}
	server, err := startAppServer(input, mcpProxyToken)
	if err != nil {
		return Result{}, err
	}
	state := newProtocolState(input.CodexSessionID)
	initialize := map[string]any{
		"clientInfo":   map[string]string{"name": "kodex-agent-runner", "title": "Kodex agent-runner", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": false, "optOutNotificationMethods": suppressedNotificationMethods},
	}
	raw, err := server.call(ctx, state, "initialize", initialize)
	if err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := state.initialize(raw, input.CodexHome); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := server.notifyInitialized(); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	raw, err = server.call(ctx, state, "account/read", map[string]bool{"refreshToken": false})
	if err := classifyAccountReadResponse(raw, err); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	threadParams := map[string]any{"approvalPolicy": input.CodexApprovalPolicy, "cwd": input.WorkspaceRoot,
		"model": input.Model}
	method := "thread/start"
	if input.CodexSessionID == "" {
		threadParams["ephemeral"] = false
		threadParams["sessionStartSource"] = "startup"
	} else {
		method = "thread/resume"
		threadParams["threadId"] = input.CodexSessionID
	}
	raw, err = server.call(ctx, state, method, threadParams)
	if err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := state.bindThread(raw, input.Model, input.WorkspaceRoot, input.CodexApprovalPolicy); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := server.waitRequiredMCP(ctx, state, requiredMCPToolNames(input)); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := state.captureUsageBaseline(); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	turnParams := map[string]any{"threadId": state.threadID, "cwd": input.WorkspaceRoot, "model": input.Model,
		"approvalPolicy": input.CodexApprovalPolicy, "input": []map[string]any{{"type": "text", "text": string(prompt)}}}
	raw, err = server.call(ctx, state, "turn/start", turnParams)
	if err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := state.bindTurn(raw); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := server.waitTerminal(ctx, state); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	raw, err = server.call(ctx, state, "thread/read", map[string]any{"threadId": state.threadID, "includeTurns": false})
	if err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := state.bindThreadRead(raw); err != nil {
		return Result{}, server.abort(ctx, state, err)
	}
	if err := server.stop(state); err != nil {
		return Result{}, err
	}
	result, err := state.terminalResult()
	if err != nil {
		return Result{}, err
	}
	archivePath, relativePath, digest, sizeBytes, err := captureRollout(input, state.threadPath)
	if err != nil {
		return Result{}, err
	}
	result.ArchivePath = archivePath
	result.ArchiveRelativePath = relativePath
	result.ArchiveSHA256 = digest
	result.ArchiveSizeBytes = sizeBytes
	return result, nil
}

func classifyAccountReadResponse(raw json.RawMessage, callErr error) error {
	if callErr != nil {
		return callErr
	}
	fields, err := decodeObject(raw, schema([]string{"requiresOpenaiAuth"}, "account", "requiresOpenaiAuth"))
	if err != nil {
		return errAccountReadResponseInvalid
	}
	var requiresOpenAIAuth bool
	if strictDecode(fields["requiresOpenaiAuth"], &requiresOpenAIAuth) != nil {
		return errAccountReadResponseInvalid
	}
	account, present := fields["account"]
	if !present || bytes.Equal(bytes.TrimSpace(account), []byte("null")) {
		if requiresOpenAIAuth {
			return ErrProviderAuthentication
		}
		return nil
	}
	if validateAccountReadAccount(account) != nil {
		return errAccountReadResponseInvalid
	}
	return nil
}

func validateAccountReadAccount(raw json.RawMessage) error {
	fields, err := decodeObject(raw, schema([]string{"type"}, "type", "email", "planType", "usesCodexManagedCredentials"))
	if err != nil {
		return err
	}
	accountType, err := decodeBoundedString(fields["type"], 64)
	if err != nil {
		return err
	}
	switch accountType {
	case "apiKey":
		if len(fields) != 1 {
			return errors.New("Codex app-server API key account is invalid")
		}
	case "chatgpt":
		email, hasEmail := fields["email"]
		planType, hasPlanType := fields["planType"]
		if len(fields) != 3 || !hasEmail || !hasPlanType || validateAccountEmail(email) != nil || !validAccountPlanType(planType) {
			return errors.New("Codex app-server ChatGPT account is invalid")
		}
	case "amazonBedrock":
		managed, hasManaged := fields["usesCodexManagedCredentials"]
		if len(fields) > 2 || hasManaged && strictDecode(managed, new(bool)) != nil {
			return errors.New("Codex app-server Amazon Bedrock account is invalid")
		}
	default:
		return errors.New("Codex app-server account type is invalid")
	}
	return nil
}

func validateAccountEmail(raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var email string
	if strictDecode(raw, &email) != nil || len(email) > 4096 {
		return errors.New("Codex app-server account email is invalid")
	}
	return nil
}

func validAccountPlanType(raw json.RawMessage) bool {
	planType, err := decodeBoundedString(raw, 64)
	if err != nil {
		return false
	}
	switch planType {
	case "free", "go", "plus", "pro", "prolite", "team", "self_serve_business_prolite",
		"self_serve_business_usage_based", "business", "ent26", "enterprise_cbp_automation",
		"enterprise_cbp_usage_based", "enterprise", "edu", "edu_plus", "edu_pro", "unknown":
		return true
	default:
		return false
	}
}

func (server *appServer) waitRequiredMCP(ctx context.Context, state *protocolState, requiredTools []string) error {
	readinessContext, cancel := context.WithTimeout(ctx, mcpReadinessTimeout)
	defer cancel()
	params := map[string]any{
		"threadId": state.threadID,
		"detail":   "toolsAndAuthOnly",
		"limit":    uint32(128),
	}
	for {
		raw, err := server.call(readinessContext, state, "mcpServerStatus/list", params)
		if err != nil {
			if readinessContext.Err() != nil {
				return ErrRequiredMCPUnavailable
			}
			return err
		}
		ready, err := state.bindRequiredMCPStatus(raw, requiredTools)
		if err != nil || ready {
			return err
		}
		timer := time.NewTimer(mcpReadinessPoll)
		select {
		case <-readinessContext.Done():
			timer.Stop()
			return ErrRequiredMCPUnavailable
		case <-timer.C:
		}
	}
}

func requiredMCPToolNames(input model.Input) []string {
	result := []string{"propose_run_metadata"}
	if input.SystemAssistant {
		result = append(result, "get_configuration_catalog", "propose_configuration_plan", "propose_assistant_metadata")
	}
	if len(input.DelegationTargets) != 0 {
		result = append(result, "delegate_agent")
	}
	if len(input.IntegrationGrants) != 0 {
		result = append(result, "invoke_integration")
	}
	return result
}

func startAppServer(input model.Input, mcpProxyToken string) (*appServer, error) {
	command := exec.Command("/usr/local/bin/codex", "app-server", "--strict-config", "--listen", "stdio://")
	command.Dir = input.WorkspaceRoot
	command.Env = appServerEnvironment(input, mcpProxyToken)
	if command.Env == nil {
		return nil, errors.New("runtime Secret projection is unavailable")
	}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, errors.New("create Codex app-server request stream")
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, errors.New("create Codex app-server response stream")
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, errors.New("create Codex app-server diagnostic stream")
	}
	if err := command.Start(); err != nil {
		return nil, errors.New("start Codex app-server process")
	}
	messages := make(chan streamEvent, 64)
	go readAppServerMessages(stdout, messages)
	diagnostics := make(chan error, 1)
	go func() {
		written, copyErr := io.Copy(io.Discard, io.LimitReader(stderr, maximumDiagnosticSize+1))
		if copyErr != nil || written > maximumDiagnosticSize {
			diagnostics <- errors.New("Codex app-server diagnostic stream exceeded its bound")
		} else {
			diagnostics <- nil
		}
	}()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	return &appServer{command: command, stdin: stdin, messages: messages, wait: wait, diagnostics: diagnostics}, nil
}

func appServerEnvironment(input model.Input, mcpProxyToken string) []string {
	environment := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "HOME=" + input.CodexHome,
		"CODEX_HOME=" + input.CodexHome, "KODEX_MCP_PROXY_TOKEN=" + mcpProxyToken}
	for _, item := range input.EnvironmentValues {
		environment = append(environment, item.Name+"="+item.Value)
	}
	for _, item := range input.SecretProjections {
		value, present := os.LookupEnv(item.Name)
		if !present {
			return nil
		}
		environment = append(environment, item.Name+"="+value)
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"} {
		if value, ok := os.LookupEnv(name); ok && value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

func readAppServerMessages(reader io.Reader, events chan<- streamEvent) {
	defer close(events)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maximumJSONLLineBytes)
	count := 0
	for scanner.Scan() {
		count++
		if count > maximumJSONLMessages || len(scanner.Bytes()) == 0 {
			events <- streamEvent{err: errors.New("Codex app-server message budget exceeded")}
			return
		}
		message, err := parseWireMessage(scanner.Bytes())
		if err != nil {
			events <- streamEvent{err: err}
			return
		}
		events <- streamEvent{message: message}
	}
	if err := scanner.Err(); err != nil {
		events <- streamEvent{err: errors.New("read Codex app-server response stream")}
	}
}

func (server *appServer) call(ctx context.Context, state *protocolState, method string, params any) (json.RawMessage, error) {
	server.nextID++
	id := server.nextID
	if err := server.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, context.Canceled
		case event, open := <-server.messages:
			if !open {
				return nil, errors.New("Codex app-server closed its response stream")
			}
			if event.err != nil {
				return nil, event.err
			}
			switch event.message.kind {
			case messageNotification:
				if err := state.notification(event.message.method, event.message.payload); err != nil {
					return nil, err
				}
			case messageRequest:
				if err := server.handleRequest(state, event.message); err != nil {
					return nil, err
				}
			case messageResponse, messageError:
				responseID, err := numericRequestID(event.message.id)
				if err != nil || responseID != id {
					return nil, errors.New("Codex app-server response correlation failed")
				}
				if event.message.kind == messageError {
					return nil, protocolError(method, event.message.payload)
				}
				return event.message.payload, nil
			default:
				return nil, errors.New("Codex app-server message kind is invalid")
			}
		}
	}
}

func protocolError(method string, raw json.RawMessage) error {
	code, err := decodeRPCError(raw)
	if err != nil {
		return errors.New("Codex app-server returned an invalid protocol error")
	}
	return fmt.Errorf("Codex app-server returned a protocol error for %s (code %d)", method, code)
}

func (server *appServer) waitTerminal(ctx context.Context, state *protocolState) error {
	for state.terminals == 0 {
		select {
		case <-ctx.Done():
			return context.Canceled
		case event, open := <-server.messages:
			if !open {
				return errors.New("Codex app-server closed before a terminal notification")
			}
			if event.err != nil {
				return event.err
			}
			switch event.message.kind {
			case messageNotification:
				if err := state.notification(event.message.method, event.message.payload); err != nil {
					return err
				}
			case messageRequest:
				if err := server.handleRequest(state, event.message); err != nil {
					return err
				}
			default:
				return errors.New("Codex app-server emitted an uncorrelated response")
			}
		}
	}
	return nil
}

func (server *appServer) notifyInitialized() error {
	return server.write(map[string]any{"method": "initialized"})
}

func (server *appServer) rejectRequest(message wireMessage) error {
	if _, allowed := serverRequestMethods[message.method]; !allowed {
		return fmt.Errorf("%w: request method is not allowed", ErrAuthorityRequestUnsupported)
	}
	_ = server.write(map[string]any{"id": message.id, "error": map[string]any{
		"code": int64(-32000), "message": "Server requests are not authorized in agent-runner"}})
	return fmt.Errorf("%w: %s", ErrAuthorityRequestUnsupported, message.method)
}

func (server *appServer) handleRequest(state *protocolState, message wireMessage) error {
	if message.method != "mcpServer/elicitation/request" {
		return server.rejectRequest(message)
	}
	response, err := trustedMCPToolApproval(state, message.payload)
	if err != nil {
		return server.rejectRequest(message)
	}
	return server.write(map[string]any{"id": message.id, "result": response})
}

func trustedMCPToolApproval(state *protocolState, raw json.RawMessage) (map[string]any, error) {
	var request struct {
		ThreadID        string  `json:"threadId"`
		TurnID          *string `json:"turnId"`
		ServerName      string  `json:"serverName"`
		Mode            string  `json:"mode"`
		Message         string  `json:"message"`
		RequestedSchema struct {
			Type       string                     `json:"type"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"requestedSchema"`
		Metadata map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, errors.New("decode MCP tool approval request")
	}
	if request.ThreadID != state.threadID || request.ServerName != "kodex" || request.Mode != "form" ||
		request.Message == "" || len(request.Message) > 4096 || request.RequestedSchema.Type != "object" ||
		request.RequestedSchema.Properties == nil || len(request.RequestedSchema.Properties) != 0 {
		return nil, errors.New("MCP tool approval request is outside runtime authority")
	}
	if request.TurnID != nil && *request.TurnID != state.turnID {
		return nil, errors.New("MCP tool approval turn does not match runtime authority")
	}
	var approvalKind string
	if err := json.Unmarshal(request.Metadata["codex_approval_kind"], &approvalKind); err != nil || approvalKind != "mcp_tool_call" {
		return nil, errors.New("MCP elicitation is not a tool approval")
	}
	return map[string]any{"action": "accept", "content": map[string]any{}}, nil
}

func (server *appServer) write(message any) error {
	raw, err := json.Marshal(message)
	if err != nil || len(raw) == 0 || len(raw) > maximumRequestBytes {
		return errors.New("encode Codex app-server request")
	}
	raw = append(raw, '\n')
	if _, err := server.stdin.Write(raw); err != nil {
		return errors.New("write Codex app-server request")
	}
	return nil
}

func (server *appServer) stop(state *protocolState) error {
	if err := server.stdin.Close(); err != nil {
		return errors.New("close Codex app-server request stream")
	}
	timer := time.NewTimer(processGrace)
	defer timer.Stop()
	for {
		select {
		case event, open := <-server.messages:
			if !open {
				server.messages = nil
				continue
			}
			if event.err != nil {
				return server.terminate(event.err)
			}
			if event.message.kind == messageNotification {
				if err := state.notification(event.message.method, event.message.payload); err != nil {
					return server.terminate(err)
				}
			} else if event.message.kind == messageRequest {
				if err := server.handleRequest(state, event.message); err != nil {
					return server.terminate(err)
				}
			} else {
				return server.terminate(errors.New("Codex app-server emitted a late response"))
			}
		case waitErr := <-server.wait:
			diagnosticErr := <-server.diagnostics
			if waitErr != nil {
				return errors.New("Codex app-server exited unsuccessfully")
			}
			return diagnosticErr
		case <-timer.C:
			return server.terminate(errors.New("Codex app-server shutdown deadline exceeded"))
		}
	}
}

func (server *appServer) abort(ctx context.Context, state *protocolState, cause error) error {
	if state.threadID != "" && state.turnID != "" && state.terminals == 0 {
		interruptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), processGrace)
		defer cancel()
		if _, err := server.call(interruptContext, state, "turn/interrupt", map[string]string{
			"threadId": state.threadID, "turnId": state.turnID}); err != nil {
			return server.terminate(cause)
		}
		if state.terminals == 0 {
			if err := server.waitTerminal(interruptContext, state); err != nil {
				return server.terminate(cause)
			}
		}
	}
	return server.terminate(cause)
}

func (server *appServer) terminate(cause error) error {
	_ = server.stdin.Close()
	if server.command.Process != nil {
		_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGTERM)
	}
	timer := time.NewTimer(terminationGrace)
	defer timer.Stop()
	select {
	case <-server.wait:
	case <-timer.C:
		if server.command.Process != nil {
			_ = syscall.Kill(-server.command.Process.Pid, syscall.SIGKILL)
		}
		<-server.wait
	}
	select {
	case <-server.diagnostics:
	default:
	}
	return cause
}

func verifyAccountPin(input model.Input) error {
	path := filepath.Join(input.CodexHome, "auth.json")
	file, info, err := openProtectedFile(input.WorkspaceRoot, path)
	if err != nil {
		return errors.New("Codex authentication snapshot is unavailable")
	}
	defer file.Close()
	if info.Size() <= 0 || info.Size() > 1<<20 {
		return errors.New("Codex authentication snapshot metadata is invalid")
	}
	digest := sha256.New()
	expectedDigest, expectedErr := pinnedProviderDigest(input)
	if _, err := io.Copy(digest, io.LimitReader(file, 1<<20+1)); err != nil || expectedErr != nil ||
		hex.EncodeToString(digest.Sum(nil)) != expectedDigest {
		return errors.New("Codex authentication snapshot does not match the pinned provider account")
	}
	return nil
}

func verifyRestoreArchive(input model.Input) error {
	// Session continuation uses the retained session PVC and server-owned
	// context. A caller-provided archive locator is intentionally absent.
	return nil
}

func captureRollout(input model.Input, returnedPath string) (string, string, string, int64, error) {
	if !filepath.IsAbs(returnedPath) || filepath.Clean(returnedPath) != returnedPath ||
		!strings.HasPrefix(returnedPath, input.CodexHome+string(os.PathSeparator)) {
		return "", "", "", 0, errors.New("Codex app-server returned an unsafe rollout path")
	}
	relativePath, err := filepath.Rel(input.WorkspaceRoot, returnedPath)
	if err != nil {
		return "", "", "", 0, errors.New("resolve Codex rollout path")
	}
	relativePath = filepath.ToSlash(relativePath)
	if !validRolloutRelativePath(relativePath) {
		return "", "", "", 0, errors.New("Codex app-server rollout identity changed")
	}
	file, info, err := openProtectedFile(input.WorkspaceRoot, returnedPath)
	if err != nil {
		return "", "", "", 0, errors.New("open Codex app-server rollout")
	}
	digest, hashErr := digestArchive(file, info)
	groupErr := file.Chown(-1, 29000)
	modeErr := file.Chmod(0o640)
	closeErr := file.Close()
	if hashErr != nil || groupErr != nil || modeErr != nil || closeErr != nil {
		return "", "", "", 0, errors.New("verify Codex app-server rollout")
	}
	return returnedPath, relativePath, digest, info.Size(), nil
}

func validRolloutRelativePath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	return clean == value && rolloutPathPattern.MatchString(value) && !strings.Contains(value, "..")
}

func openProtectedFile(root, path string) (*os.File, os.FileInfo, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, nil, errors.New("path is outside the workspace")
	}
	components := strings.Split(filepath.Clean(relative), string(os.PathSeparator))
	if len(components) == 0 || len(components) > 32 {
		return nil, nil, errors.New("path depth is invalid")
	}
	directory, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." || component == ".." {
			unix.Close(directory)
			return nil, nil, errors.New("path component is invalid")
		}
		next, openErr := unix.Openat(directory, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(directory)
		if openErr != nil {
			return nil, nil, openErr
		}
		directory = next
	}
	fd, err := unix.Openat(directory, components[len(components)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	unix.Close(directory)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("file is not regular")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		file.Close()
		return nil, nil, errors.New("file link count is invalid")
	}
	return file, info, nil
}

func digestArchive(file *os.File, info os.FileInfo) (string, error) {
	if info.Size() <= 0 || info.Size() > maximumArchiveBytes {
		return "", errors.New("Codex rollout size is invalid")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, maximumArchiveBytes+1))
	if err != nil || written != info.Size() || written > maximumArchiveBytes {
		return "", errors.New("Codex rollout content is invalid")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func ReadCredential(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return "", errors.New("read runtime credential")
	}
	value := string(bytes.TrimSpace(raw))
	if value == "" {
		return "", errors.New("runtime credential is empty")
	}
	return value, nil
}

var suppressedNotificationMethods = []string{
	"thread/status/changed", "skills/changed", "thread/name/updated", "thread/goal/updated", "thread/goal/cleared",
	"thread/settings/updated", "hook/started", "hook/completed", "turn/diff/updated",
	"turn/plan/updated", "item/autoApprovalReview/started", "item/autoApprovalReview/completed", "item/agentMessage/delta",
	"item/plan/delta", "command/exec/outputDelta", "process/outputDelta", "process/exited",
	"item/commandExecution/outputDelta", "item/commandExecution/terminalInteraction", "item/fileChange/outputDelta",
	"item/fileChange/patchUpdated", "serverRequest/resolved", "item/mcpToolCall/progress", "account/rateLimits/updated",
	"app/list/updated", "remoteControl/status/changed", "externalAgentConfig/import/progress",
	"externalAgentConfig/import/completed", "fs/changed", "item/reasoning/summaryTextDelta",
	"item/reasoning/summaryPartAdded", "item/reasoning/textDelta", "thread/compacted", "model/rerouted",
	"model/verification", "turn/moderationMetadata", "model/safetyBuffering/updated", "fuzzyFileSearch/sessionUpdated",
	"fuzzyFileSearch/sessionCompleted", "thread/realtime/started", "thread/realtime/itemAdded",
	"thread/realtime/transcript/delta", "thread/realtime/transcript/done", "thread/realtime/outputAudio/delta",
	"thread/realtime/sdp", "thread/realtime/error", "thread/realtime/closed",
}
