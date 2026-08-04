package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	runtimeInputSchema           = "mattercodex.agent-runner-input.v1"
	runtimeHandoffAnnotation     = "runtime.mattercodex.dev/turn-handoff"
	runtimeArchiveGateAnnotation = "runtime.mattercodex.dev/archive-gate"
	runtimeNextInputAnnotation   = "runtime.mattercodex.dev/next-input-config"
)

var runtimeDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type runtimeSessionContract struct {
	Schema                   string            `json:"schema"`
	ExecutionID              string            `json:"execution_id"`
	ExecutionVersion         uint64            `json:"execution_version"`
	Fence                    uint64            `json:"fence"`
	GrantGeneration          uint64            `json:"grant_generation"`
	RuntimeRevisionID        string            `json:"runtime_revision_id"`
	RuntimeRevisionVersion   uint64            `json:"runtime_revision_version"`
	RuntimeRevisionSHA256    string            `json:"runtime_revision_sha256"`
	EffectiveRuntimeSHA256   string            `json:"effective_runtime_sha256"`
	ImmutableInputSHA256     string            `json:"immutable_input_sha256"`
	SessionKey               string            `json:"session_key"`
	AgentSessionID           int64             `json:"agent_session_id"`
	AgentSessionTurnID       int64             `json:"agent_session_turn_id"`
	AgentRunID               string            `json:"agent_run_id"`
	AgentBindingSHA256       string            `json:"agent_binding_sha256"`
	CredentialSnapshotSHA256 string            `json:"credential_snapshot_sha256"`
	WorkloadTicketSHA256     string            `json:"workload_ticket_sha256"`
	AgentProfile             string            `json:"agent_profile"`
	BotServiceURL            string            `json:"bot_service_url"`
	MCPURL                   string            `json:"mcp_url"`
	BotServiceTLS            runtimeTLSBinding `json:"bot_service_tls"`
	MCPTLS                   runtimeTLSBinding `json:"mcp_tls"`
	CredentialFiles          struct {
		SessionToken string `json:"session_token"`
		MCPToken     string `json:"mcp_token"`
		CodexAuth    string `json:"codex_auth"`
	} `json:"credential_files"`
	sessionToken   string
	mcpToken       string
	credentialRoot string
}

type runtimeTLSBinding struct {
	ServerName      string `json:"server_name"`
	CAFile          string `json:"ca_file"`
	CertificateFile string `json:"certificate_file"`
	PrivateKeyFile  string `json:"private_key_file"`
	BindingSHA256   string `json:"binding_sha256"`
}

type runtimeSnapshot struct {
	Execution      json.RawMessage        `json:"execution"`
	Revision       json.RawMessage        `json:"runtime_revision"`
	RunnerInput    runtimeSessionContract `json:"runner_input"`
	WorkloadTicket string                 `json:"workload_ticket"`
	DesiredPod     json.RawMessage        `json:"desired_pod"`
}

type runtimeRevisionSnapshot struct {
	Credentials []runtimeCredentialSnapshot `json:"Credentials"`
}

type runtimeCredentialSnapshot struct {
	ResourceID             string `json:"ResourceID"`
	Version                uint64 `json:"Version"`
	Purpose                string `json:"Purpose"`
	ProviderContentVersion string `json:"ProviderContentVersion"`
	ContentSHA256          string `json:"ContentSHA256"`
}

type runtimeHandoff = runtimecontract.HandoffV1

func (r *runner) runRuntimeSession(ctx context.Context) error {
	contract, err := loadRuntimeSessionContract(os.Getenv("MATTERCODEX_RUNTIME_REVISION_FILE"))
	if err != nil {
		return err
	}
	r.runtimeContract = contract
	r.runtimeContractReadback.Store(contract)
	r.codexAuthFile = contract.CredentialFiles.CodexAuth
	return r.serveRuntimeSession(ctx)
}

func (r *runner) serveRuntimeSession(ctx context.Context) error {
	handler := http.NewServeMux()
	handler.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	})
	handler.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		if !r.runtimeReady.Load() {
			http.Error(response, "runtime dependencies are not ready", http.StatusServiceUnavailable)
			return
		}
		response.WriteHeader(http.StatusNoContent)
	})
	mcpTarget, err := url.Parse(r.runtimeContract.MCPURL)
	if err != nil {
		return errors.New("runtime MCP endpoint is invalid")
	}
	mcpTarget.Path, mcpTarget.RawPath = "", ""
	proxy := httputil.NewSingleHostReverseProxy(mcpTarget)
	proxy.Transport = runtimeMCPRoundTripper{contract: &r.runtimeContractReadback}
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(response, "runtime MCP upstream unavailable", http.StatusBadGateway)
	}
	handler.Handle("/mcp/", proxy)
	server := &http.Server{Addr: ":9090", Handler: handler, ReadHeaderTimeout: 2 * time.Second}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	serveErrors := make(chan error, 1)
	runErrors := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErrors <- err
	}()
	go func() {
		if err := r.awaitRuntimeDependencies(runCtx); err != nil {
			runErrors <- err
			return
		}
		r.runtimeReady.Store(true)
		sessionErrors := make(chan error, 1)
		go func() { sessionErrors <- r.runSession(runCtx) }()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case err := <-sessionErrors:
				runErrors <- err
				return
			case <-ticker.C:
				if err := r.probeRuntimeDependencies(runCtx); err != nil {
					r.runtimeReady.Store(false)
					cancel()
					runErrors <- errors.New("runtime mTLS and bearer readback failed")
					return
				}
			}
		}
	}()
	var result error
	runCompleted, serverCompleted := false, false
	select {
	case result = <-serveErrors:
		serverCompleted = true
		if result == nil {
			result = errors.New("runtime health server stopped unexpectedly")
		}
	case result = <-runErrors:
		runCompleted = true
	}
	cancel()
	r.runtimeReady.Store(false)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
		result = errors.Join(result, errors.New("shutdown runtime health server"))
	}
	shutdownCancel()
	if !runCompleted {
		joinTimer := time.NewTimer(5 * time.Second)
		select {
		case runErr := <-runErrors:
			result = errors.Join(result, runErr)
		case <-joinTimer.C:
			result = errors.Join(result, errors.New("join runtime session worker"))
		}
		joinTimer.Stop()
	}
	if !serverCompleted {
		joinTimer := time.NewTimer(5 * time.Second)
		select {
		case serverErr := <-serveErrors:
			result = errors.Join(result, serverErr)
		case <-joinTimer.C:
			result = errors.Join(result, errors.New("join runtime health server"))
		}
		joinTimer.Stop()
	}
	return result
}

func loadRuntimeSessionContract(path string) (*runtimeSessionContract, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return nil, errors.New("runtime session input is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read runtime session input")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot runtimeSnapshot
	if decoder.Decode(&snapshot) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		len(snapshot.Execution) == 0 || len(snapshot.Revision) == 0 || len(snapshot.DesiredPod) == 0 || snapshot.WorkloadTicket == "" {
		return nil, errors.New("runtime session input is invalid")
	}
	contract := &snapshot.RunnerInput
	if err := contract.validate(); err != nil {
		return nil, err
	}
	sessionToken, err := readRuntimeCredential(contract.CredentialFiles.SessionToken, 16<<10)
	if err != nil {
		return nil, err
	}
	mcpToken, err := readRuntimeCredential(contract.CredentialFiles.MCPToken, 16<<10)
	if err != nil {
		return nil, err
	}
	if err := validateRuntimeJSONCredential(contract.CredentialFiles.CodexAuth, 2<<20); err != nil {
		return nil, err
	}
	contract.sessionToken = sessionToken
	contract.mcpToken = mcpToken
	for name, value := range map[string]string{
		"MATTERCODEX_SESSION_KEY":     contract.SessionKey,
		"MATTERCODEX_AGENT_PROFILE":   contract.AgentProfile,
		"MATTERCODEX_BOT_SERVICE_URL": contract.BotServiceURL,
		"MATTERCODEX_SESSION_TOKEN":   sessionToken,
		"MATTERCODEX_MCP_URL":         "http://127.0.0.1:9090/mcp/sessions/" + url.PathEscape(contract.SessionKey),
		"MATTERCODEX_MCP_TOKEN":       mcpToken,
	} {
		if err := os.Setenv(name, value); err != nil {
			return nil, errors.New("apply runtime session environment")
		}
	}
	return contract, nil
}

func materializeRuntimeSuccessorInput(
	ctx context.Context,
	client kubernetes.Interface,
	namespace string,
	raw []byte,
) (string, string, error) {
	var snapshot runtimeSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&snapshot) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) ||
		len(snapshot.Execution) == 0 || len(snapshot.Revision) == 0 || len(snapshot.DesiredPod) == 0 || snapshot.RunnerInput.ExecutionID == "" {
		return "", "", errors.New("runtime successor snapshot is invalid")
	}
	var revision runtimeRevisionSnapshot
	if json.Unmarshal(snapshot.Revision, &revision) != nil || len(revision.Credentials) == 0 || len(revision.Credentials) > 64 {
		return "", "", errors.New("runtime successor credential set is invalid")
	}
	root, err := os.MkdirTemp("", "mattercodex-runtime-credentials-")
	if err != nil {
		return "", "", errors.New("create runtime successor credential directory")
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(root)
		}
	}()
	for index, credential := range revision.Credentials {
		name := executionCredentialSecretName(snapshot.RunnerInput.ExecutionID, index)
		secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil || secret.Immutable == nil || !*secret.Immutable ||
			secret.Annotations["runtime.mattercodex.dev/execution-id"] != snapshot.RunnerInput.ExecutionID ||
			secret.Annotations["runtime.mattercodex.dev/snapshot-sha256"] != snapshot.RunnerInput.CredentialSnapshotSHA256 ||
			secret.Annotations["runtime.mattercodex.dev/credential-resource-id"] != credential.ResourceID ||
			secret.Annotations["runtime.mattercodex.dev/credential-version"] != strconv.FormatUint(credential.Version, 10) ||
			secret.Annotations["runtime.mattercodex.dev/provider-content-version"] != credential.ProviderContentVersion ||
			secret.Annotations["runtime.mattercodex.dev/content-sha256"] != credential.ContentSHA256 ||
			secret.Annotations["runtime.mattercodex.dev/purpose"] != credential.Purpose ||
			!runtimeDigestPattern.MatchString(credential.ContentSHA256) ||
			runtimeSecretDataSHA256(secret.Data) != credential.ContentSHA256 ||
			!runtimeCredentialPurposeDataMatches(credential.Purpose, secret.Data) {
			return "", "", errors.New("read exact runtime successor credential")
		}
		directory := filepath.Join(root, strconv.Itoa(index))
		if err := os.Mkdir(directory, 0o700); err != nil {
			return "", "", errors.New("create runtime successor credential path")
		}
		for key, value := range secret.Data {
			if !validRuntimeCredentialKey(key) || len(value) == 0 || len(value) > 2<<20 {
				return "", "", errors.New("runtime successor credential data is invalid")
			}
			if err := os.WriteFile(filepath.Join(directory, key), value, 0o600); err != nil {
				return "", "", errors.New("write runtime successor credential")
			}
		}
		switch credential.Purpose {
		case "session-token":
			snapshot.RunnerInput.CredentialFiles.SessionToken = filepath.Join(directory, "token")
		case "mcp-token":
			snapshot.RunnerInput.CredentialFiles.MCPToken = filepath.Join(directory, "token")
		case "codex-auth":
			snapshot.RunnerInput.CredentialFiles.CodexAuth = filepath.Join(directory, "auth.json")
		case "bot-client-tls":
			snapshot.RunnerInput.BotServiceTLS.CAFile = filepath.Join(directory, "ca.pem")
			snapshot.RunnerInput.BotServiceTLS.CertificateFile = filepath.Join(directory, "tls.crt")
			snapshot.RunnerInput.BotServiceTLS.PrivateKeyFile = filepath.Join(directory, "tls.key")
		case "mcp-client-tls":
			snapshot.RunnerInput.MCPTLS.CAFile = filepath.Join(directory, "ca.pem")
			snapshot.RunnerInput.MCPTLS.CertificateFile = filepath.Join(directory, "tls.crt")
			snapshot.RunnerInput.MCPTLS.PrivateKeyFile = filepath.Join(directory, "tls.key")
		}
	}
	prepared, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", errors.New("encode runtime successor snapshot")
	}
	file, err := os.CreateTemp("", "runtime-successor-*.json")
	if err != nil {
		return "", "", errors.New("create runtime successor input")
	}
	path := file.Name()
	if _, err := file.Write(prepared); err != nil || file.Chmod(0o600) != nil || file.Close() != nil {
		_ = os.Remove(path)
		return "", "", errors.New("write runtime successor input")
	}
	failed = false
	return path, root, nil
}

func validRuntimeCredentialKey(value string) bool {
	switch value {
	case "token", "auth.json", "ca.pem", "tls.crt", "tls.key":
		return true
	default:
		return false
	}
}

func runtimeCredentialPurposeDataMatches(purpose string, data map[string][]byte) bool {
	var required []string
	switch purpose {
	case "session-token", "mcp-token":
		required = []string{"token"}
	case "codex-auth":
		required = []string{"auth.json"}
	case "bot-client-tls", "mcp-client-tls":
		required = []string{"ca.pem", "tls.crt", "tls.key"}
	default:
		return false
	}
	if len(data) != len(required) {
		return false
	}
	for _, key := range required {
		if len(data[key]) == 0 {
			return false
		}
	}
	return true
}

func runtimeSecretDataSHA256(data map[string][]byte) string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	digest := sha256.New()
	for _, key := range keys {
		_, _ = digest.Write([]byte(key))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(data[key])
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func executionCredentialSecretName(executionID string, index int) string {
	parsed := strings.ReplaceAll(executionID, "-", "")
	if len(parsed) < 20 {
		return ""
	}
	return "runtime-credential-" + parsed[:20] + "-" + strconv.Itoa(index)
}

func validateRuntimeJSONCredential(path string, maximum int64) error {
	if !validRuntimeCredentialPath(path) {
		return errors.New("runtime credential path is invalid")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o007 != 0 {
		return errors.New("runtime credential file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil || !json.Valid(raw) {
		return errors.New("runtime JSON credential is invalid")
	}
	return nil
}

func (contract runtimeSessionContract) validate() error {
	botURL, botErr := url.Parse(contract.BotServiceURL)
	mcpURL, mcpErr := url.Parse(contract.MCPURL)
	if contract.Schema != runtimeInputSchema || contract.ExecutionID == "" ||
		contract.ExecutionVersion == 0 || contract.Fence == 0 || contract.GrantGeneration == 0 ||
		contract.RuntimeRevisionID == "" || contract.RuntimeRevisionVersion == 0 ||
		!runtimeDigestPattern.MatchString(contract.RuntimeRevisionSHA256) ||
		!runtimeDigestPattern.MatchString(contract.EffectiveRuntimeSHA256) ||
		!runtimeDigestPattern.MatchString(contract.ImmutableInputSHA256) ||
		contract.SessionKey == "" || len(contract.SessionKey) > 256 || contract.AgentSessionID <= 0 ||
		contract.AgentSessionTurnID <= 0 || contract.AgentRunID == "" || len(contract.AgentRunID) > 256 ||
		!runtimeDigestPattern.MatchString(contract.AgentBindingSHA256) ||
		!runtimeDigestPattern.MatchString(contract.CredentialSnapshotSHA256) ||
		!runtimeDigestPattern.MatchString(contract.WorkloadTicketSHA256) || contract.AgentProfile == "" ||
		botErr != nil || !validRuntimeBotURL(botURL) ||
		mcpErr != nil || !validRuntimeMCPURL(mcpURL, contract.SessionKey) ||
		contract.BotServiceTLS.validate(botURL.Hostname()) != nil ||
		contract.MCPTLS.validate(mcpURL.Hostname()) != nil ||
		contract.CredentialFiles.SessionToken == "" || contract.CredentialFiles.MCPToken == "" ||
		contract.CredentialFiles.CodexAuth == "" {
		return errors.New("runtime session contract is invalid")
	}
	return nil
}

func validRuntimeBotURL(endpoint *url.URL) bool {
	return endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.Path == "" &&
		endpoint.RawQuery == "" && endpoint.Fragment == "" && endpoint.User == nil &&
		strings.HasSuffix(endpoint.Hostname(), ".svc.cluster.local")
}

func validRuntimeMCPURL(endpoint *url.URL, sessionKey string) bool {
	return endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.RawQuery == "" &&
		endpoint.Fragment == "" && endpoint.User == nil &&
		strings.HasSuffix(endpoint.Hostname(), ".svc.cluster.local") &&
		endpoint.EscapedPath() == "/mcp/sessions/"+url.PathEscape(sessionKey)
}

func (binding runtimeTLSBinding) validate(hostname string) error {
	if binding.ServerName == "" || binding.ServerName != hostname ||
		!runtimeDigestPattern.MatchString(binding.BindingSHA256) {
		return errors.New("runtime TLS binding is invalid")
	}
	for _, path := range []string{binding.CAFile, binding.CertificateFile, binding.PrivateKeyFile} {
		if !validRuntimeCredentialPath(path) {
			return errors.New("runtime TLS credential path is invalid")
		}
	}
	return nil
}

func runtimeMTLSTransport(binding runtimeTLSBinding) (*http.Transport, error) {
	if err := binding.validate(binding.ServerName); err != nil {
		return nil, err
	}
	caRaw, err := os.ReadFile(binding.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read runtime TLS CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse runtime TLS CA")
	}
	certificateRaw, err := os.ReadFile(binding.CertificateFile)
	if err != nil || len(certificateRaw) == 0 || len(certificateRaw) > 1<<20 {
		return nil, errors.New("read runtime TLS client certificate")
	}
	privateKeyRaw, err := os.ReadFile(binding.PrivateKeyFile)
	if err != nil || len(privateKeyRaw) == 0 || len(privateKeyRaw) > 1<<20 {
		return nil, errors.New("read runtime TLS client private key")
	}
	digest := sha256.New()
	for _, item := range []struct {
		key   string
		value []byte
	}{{"ca.pem", caRaw}, {"tls.crt", certificateRaw}, {"tls.key", privateKeyRaw}} {
		_, _ = digest.Write([]byte(item.key))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write(item.value)
		_, _ = digest.Write([]byte{0})
	}
	if hex.EncodeToString(digest.Sum(nil)) != binding.BindingSHA256 {
		return nil, errors.New("runtime TLS credential snapshot digest mismatch")
	}
	certificate, err := tls.X509KeyPair(certificateRaw, privateKeyRaw)
	if err != nil {
		return nil, errors.New("load runtime TLS client identity")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, ServerName: binding.ServerName,
		RootCAs: roots, Certificates: []tls.Certificate{certificate}}
	return transport, nil
}

type runtimeMCPRoundTripper struct {
	contract *atomic.Pointer[runtimeSessionContract]
}

func (roundTripper runtimeMCPRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	contract := roundTripper.contract.Load()
	if contract == nil {
		return nil, errors.New("runtime MCP authority is unavailable")
	}
	transport, err := runtimeMTLSTransport(contract.MCPTLS)
	if err != nil {
		return nil, err
	}
	// Каждый запрос использует текущий immutable credential snapshot; connection
	// reuse через смену binding мог бы сохранить прежнюю mTLS identity.
	transport.DisableKeepAlives = true
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+contract.mcpToken)
	response, err := transport.RoundTrip(request)
	if err != nil {
		transport.CloseIdleConnections()
	}
	return response, err
}

func (r *runner) awaitRuntimeDependencies(ctx context.Context) error {
	delay := time.Second
	for attempt := 0; attempt < 30; attempt++ {
		if err := r.probeRuntimeDependencies(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 10*time.Second {
			delay *= 2
		}
	}
	return errors.New("runtime mTLS and bearer startup barrier failed")
}

func (r *runner) probeRuntimeDependencies(ctx context.Context) error {
	contract := r.runtimeContractReadback.Load()
	if contract == nil {
		return errors.New("runtime dependency contract is unavailable")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	botTransport, err := runtimeMTLSTransport(contract.BotServiceTLS)
	if err != nil {
		return err
	}
	botEndpoint := strings.TrimRight(contract.BotServiceURL, "/") + "/internal/agent-sessions/" + url.PathEscape(contract.SessionKey) + "/readiness"
	botRequest, err := http.NewRequestWithContext(probeCtx, http.MethodPost, botEndpoint, nil)
	if err != nil {
		return errors.New("create runtime bot readiness request")
	}
	botRequest.Header.Set("Authorization", "Bearer "+contract.sessionToken)
	botResponse, err := (&http.Client{Transport: botTransport, Timeout: 10 * time.Second}).Do(botRequest)
	if err != nil {
		return errors.New("runtime bot mTLS readiness failed")
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(botResponse.Body, 4096))
	botResponse.Body.Close()
	if botResponse.StatusCode != http.StatusNoContent {
		return errors.New("runtime bot bearer readiness failed")
	}
	mcpTransport, err := runtimeMTLSTransport(contract.MCPTLS)
	if err != nil {
		return err
	}
	mcpRequest, err := http.NewRequestWithContext(probeCtx, http.MethodPost, contract.MCPURL,
		strings.NewReader(`{"jsonrpc":"2.0","id":"runtime-readiness","method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"mattercodex-runtime-readiness","version":"1"}}}`))
	if err != nil {
		return errors.New("create runtime MCP readiness request")
	}
	mcpRequest.Header.Set("Authorization", "Bearer "+contract.mcpToken)
	mcpRequest.Header.Set("Content-Type", "application/json")
	mcpRequest.Header.Set("Accept", "application/json, text/event-stream")
	mcpResponse, err := (&http.Client{Transport: mcpTransport, Timeout: 10 * time.Second}).Do(mcpRequest)
	if err != nil {
		return errors.New("runtime MCP mTLS readiness failed")
	}
	body, readErr := io.ReadAll(io.LimitReader(mcpResponse.Body, 64<<10))
	mcpResponse.Body.Close()
	if readErr != nil || mcpResponse.StatusCode < http.StatusOK || mcpResponse.StatusCode >= http.StatusMultipleChoices || len(body) == 0 {
		return errors.New("runtime MCP bearer readiness failed")
	}
	return nil
}

func readRuntimeCredential(path string, maximum int64) (string, error) {
	if !validRuntimeCredentialPath(path) {
		return "", errors.New("runtime credential path is invalid")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximum || info.Mode().Perm()&0o007 != 0 {
		return "", errors.New("runtime credential file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("read runtime credential file")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", errors.New("runtime credential value is invalid")
	}
	return value, nil
}

func validRuntimeCredentialPath(path string) bool {
	clean := filepath.Clean(path)
	if clean != path {
		return false
	}
	if strings.HasPrefix(path, "/var/run/secrets/") {
		return true
	}
	relative, err := filepath.Rel(os.TempDir(), path)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return false
	}
	parts := strings.SplitN(relative, string(os.PathSeparator), 2)
	return len(parts) == 2 && strings.HasPrefix(parts[0], "mattercodex-runtime-credentials-")
}

func (r *runner) publishRuntimeHandoff(ctx context.Context, status, runID, finalMessage, errorMessage, archive string) error {
	contract := r.runtimeContract
	if contract == nil {
		return errors.New("runtime session contract is missing")
	}
	outcome := "SUCCEEDED"
	if status == "blocked" {
		outcome = "BLOCKED"
	} else if status != "succeeded" {
		outcome = "FAILED"
	}
	digest := sha256.Sum256([]byte(runID + "\x00" + status + "\x00" + finalMessage + "\x00" + errorMessage + "\x00" + archive))
	resultMarkdown := finalMessage
	if resultMarkdown == "" {
		resultMarkdown = errorMessage
	}
	if resultMarkdown == "" {
		resultMarkdown = "Runtime completed without a user-visible result."
	}
	resultPayload := []byte(resultMarkdown)
	if len(resultPayload) > 160<<10 {
		return errors.New("runtime result exceeds owner handoff limit")
	}
	resultDigest := sha256.Sum256(resultPayload)
	resultArtifactID := uuid.NewSHA1(uuid.NameSpaceURL,
		[]byte("mattercodex:runtime-result:"+contract.ExecutionID+":"+runID+":"+hex.EncodeToString(resultDigest[:]))).String()
	handoff := runtimeHandoff{
		Schema: runtimecontract.HandoffSchemaV1, ExecutionID: contract.ExecutionID,
		ExecutionVersion: contract.ExecutionVersion, Fence: contract.Fence,
		GrantGeneration: contract.GrantGeneration, RuntimeRevisionSHA256: contract.RuntimeRevisionSHA256,
		EffectiveRuntimeSHA256: contract.EffectiveRuntimeSHA256,
		ImmutableInputSHA256:   contract.ImmutableInputSHA256, Outcome: outcome,
		AgentSessionID: contract.AgentSessionID, AgentSessionTurnID: contract.AgentSessionTurnID,
		AgentRunID: contract.AgentRunID, AgentBindingSHA256: contract.AgentBindingSHA256,
		TerminalReference: "agent-runner:" + runID, TerminalSHA256: hex.EncodeToString(digest[:]),
		ResultArtifactID: resultArtifactID, ResultArtifactVersion: 1,
		ResultArtifactSHA256: hex.EncodeToString(resultDigest[:]), ResultArtifactName: "result.md",
		ResultArtifactMediaType: "text/markdown", ResultArtifactPayload: resultPayload,
		ObservedAt: time.Now().UTC(),
	}
	raw, err := runtimecontract.EncodeHandoff(handoff)
	if err != nil {
		return errors.New("encode runtime turn handoff")
	}
	client, podName, namespace, err := runtimeKubernetesClient()
	if err != nil {
		return err
	}
	patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":%q,"%s":"CLOSED"}}}`, runtimeHandoffAnnotation, string(raw), runtimeArchiveGateAnnotation))
	if _, err := client.CoreV1().Pods(namespace).Patch(ctx, podName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return errors.New("publish runtime turn handoff")
	}
	return nil
}

func (r *runner) waitRuntimeSuccessor(ctx context.Context) (*runtimeSessionContract, error) {
	client, podName, namespace, err := runtimeKubernetesClient()
	if err != nil {
		return nil, err
	}
	current := r.runtimeContract.ExecutionID
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return nil, errors.New("read runtime successor gate")
		}
		configName := pod.Annotations[runtimeNextInputAnnotation]
		gate := pod.Annotations[runtimeArchiveGateAnnotation]
		if (gate == "OPEN" || gate == "SUCCESSOR_READY") && configName != "" {
			config, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, configName, metav1.GetOptions{})
			if err != nil {
				return nil, errors.New("read immutable runtime successor input")
			}
			path, credentialRoot, err := materializeRuntimeSuccessorInput(
				ctx, client, namespace, config.BinaryData["runtime.json"],
			)
			if err != nil {
				return nil, err
			}
			next, loadErr := loadRuntimeSessionContract(path)
			_ = os.Remove(path)
			if loadErr != nil {
				_ = os.RemoveAll(credentialRoot)
				return nil, errors.New("runtime successor input is incompatible")
			}
			next.credentialRoot = credentialRoot
			if next.ExecutionID == current {
				_ = os.RemoveAll(credentialRoot)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ticker.C:
					continue
				}
			}
			if gate != "SUCCESSOR_READY" || next.EffectiveRuntimeSHA256 != r.runtimeContract.EffectiveRuntimeSHA256 {
				_ = os.RemoveAll(credentialRoot)
				return nil, errors.New("runtime successor input is incompatible")
			}
			r.codexAuthFile = next.CredentialFiles.CodexAuth
			patch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"CLOSED"}}}`, runtimeArchiveGateAnnotation))
			if _, err := client.CoreV1().Pods(namespace).Patch(ctx, podName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
				return nil, errors.New("close runtime successor gate")
			}
			return next, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func runtimeKubernetesClient() (kubernetes.Interface, string, string, error) {
	podName := os.Getenv("MATTERCODEX_RUNTIME_POD_NAME")
	namespace := os.Getenv("MATTERCODEX_RUNTIME_POD_NAMESPACE")
	if podName == "" || namespace == "" {
		return nil, "", "", errors.New("runtime pod identity is missing")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, "", "", errors.New("load runtime pod Kubernetes configuration")
	}
	config.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, "", "", errors.New("create runtime pod Kubernetes client")
	}
	return client, podName, namespace, nil
}
