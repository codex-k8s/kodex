package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

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
	Schema                 string `json:"schema"`
	ExecutionID            string `json:"execution_id"`
	ExecutionVersion       uint64 `json:"execution_version"`
	Fence                  uint64 `json:"fence"`
	GrantGeneration        uint64 `json:"grant_generation"`
	RuntimeRevisionID      string `json:"runtime_revision_id"`
	RuntimeRevisionVersion uint64 `json:"runtime_revision_version"`
	RuntimeRevisionSHA256  string `json:"runtime_revision_sha256"`
	ImmutableInputSHA256   string `json:"immutable_input_sha256"`
	SessionKey             string `json:"session_key"`
	AgentProfile           string `json:"agent_profile"`
	BotServiceURL          string `json:"bot_service_url"`
	MCPURL                 string `json:"mcp_url"`
	CredentialFiles        struct {
		SessionToken string `json:"session_token"`
		MCPToken     string `json:"mcp_token"`
		CodexAuth    string `json:"codex_auth"`
	} `json:"credential_files"`
	sessionToken string
}

type runtimeSnapshot struct {
	Execution   json.RawMessage        `json:"execution"`
	Revision    json.RawMessage        `json:"runtime_revision"`
	RunnerInput runtimeSessionContract `json:"runner_input"`
}

type runtimeHandoff struct {
	Schema                string    `json:"schema"`
	ExecutionID           string    `json:"execution_id"`
	ExecutionVersion      uint64    `json:"execution_version"`
	Fence                 uint64    `json:"fence"`
	GrantGeneration       uint64    `json:"grant_generation"`
	RuntimeRevisionSHA256 string    `json:"runtime_revision_sha256"`
	ImmutableInputSHA256  string    `json:"immutable_input_sha256"`
	Outcome               string    `json:"outcome"`
	TerminalReference     string    `json:"terminal_reference"`
	TerminalSHA256        string    `json:"terminal_sha256"`
	ObservedAt            time.Time `json:"observed_at"`
}

func (r *runner) runRuntimeSession(ctx context.Context) error {
	contract, err := loadRuntimeSessionContract(os.Getenv("MATTERCODEX_RUNTIME_REVISION_FILE"))
	if err != nil {
		return err
	}
	r.runtimeContract = contract
	r.codexAuthFile = contract.CredentialFiles.CodexAuth
	return r.serveRuntimeSession(ctx)
}

func (r *runner) serveRuntimeSession(ctx context.Context) error {
	handler := http.NewServeMux()
	handler.HandleFunc("/livez", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	})
	handler.HandleFunc("/readyz", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	})
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
	go func() { runErrors <- r.runSession(runCtx) }()
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
		len(snapshot.Execution) == 0 || len(snapshot.Revision) == 0 {
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
	for name, value := range map[string]string{
		"MATTERCODEX_SESSION_KEY":     contract.SessionKey,
		"MATTERCODEX_AGENT_PROFILE":   contract.AgentProfile,
		"MATTERCODEX_BOT_SERVICE_URL": contract.BotServiceURL,
		"MATTERCODEX_SESSION_TOKEN":   sessionToken,
		"MATTERCODEX_MCP_URL":         contract.MCPURL,
		"MATTERCODEX_MCP_TOKEN":       mcpToken,
	} {
		if err := os.Setenv(name, value); err != nil {
			return nil, errors.New("apply runtime session environment")
		}
	}
	return contract, nil
}

func validateRuntimeJSONCredential(path string, maximum int64) error {
	if !strings.HasPrefix(path, "/var/run/secrets/") {
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
		!runtimeDigestPattern.MatchString(contract.ImmutableInputSHA256) ||
		contract.SessionKey == "" || len(contract.SessionKey) > 256 || contract.AgentProfile == "" ||
		botErr != nil || !validRuntimeBotURL(botURL) ||
		mcpErr != nil || !validRuntimeMCPURL(mcpURL, contract.SessionKey) ||
		contract.CredentialFiles.SessionToken == "" || contract.CredentialFiles.MCPToken == "" ||
		contract.CredentialFiles.CodexAuth == "" {
		return errors.New("runtime session contract is invalid")
	}
	return nil
}

func validRuntimeBotURL(endpoint *url.URL) bool {
	return endpoint.Scheme == "http" && endpoint.Host != "" && endpoint.Path == "" &&
		endpoint.RawQuery == "" && endpoint.Fragment == "" && endpoint.User == nil &&
		strings.HasSuffix(endpoint.Hostname(), ".svc.cluster.local")
}

func validRuntimeMCPURL(endpoint *url.URL, sessionKey string) bool {
	return endpoint.Scheme == "http" && endpoint.Host != "" && endpoint.RawQuery == "" &&
		endpoint.Fragment == "" && endpoint.User == nil &&
		strings.HasSuffix(endpoint.Hostname(), ".svc.cluster.local") &&
		endpoint.EscapedPath() == "/mcp/sessions/"+url.PathEscape(sessionKey)
}

func readRuntimeCredential(path string, maximum int64) (string, error) {
	if !strings.HasPrefix(path, "/var/run/secrets/") {
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

func (r *runner) publishRuntimeHandoff(ctx context.Context, status, runID, finalMessage, errorMessage, archive string) error {
	contract := r.runtimeContract
	if contract == nil {
		return errors.New("runtime session contract is missing")
	}
	outcome := "SUCCEEDED"
	if status != "succeeded" {
		outcome = "FAILED"
	}
	digest := sha256.Sum256([]byte(runID + "\x00" + status + "\x00" + finalMessage + "\x00" + errorMessage + "\x00" + archive))
	handoff := runtimeHandoff{
		Schema: "mattercodex.runtime-turn-handoff.v1", ExecutionID: contract.ExecutionID,
		ExecutionVersion: contract.ExecutionVersion, Fence: contract.Fence,
		GrantGeneration: contract.GrantGeneration, RuntimeRevisionSHA256: contract.RuntimeRevisionSHA256,
		ImmutableInputSHA256: contract.ImmutableInputSHA256, Outcome: outcome,
		TerminalReference: "agent-runner:" + runID, TerminalSHA256: hex.EncodeToString(digest[:]),
		ObservedAt: time.Now().UTC(),
	}
	raw, err := json.Marshal(handoff)
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
			temporary, err := os.CreateTemp("", "runtime-successor-*.json")
			if err != nil {
				return nil, errors.New("create runtime successor input")
			}
			path := temporary.Name()
			if _, err := temporary.Write(config.BinaryData["runtime.json"]); err != nil || temporary.Close() != nil {
				_ = os.Remove(path)
				return nil, errors.New("write runtime successor input")
			}
			next, loadErr := loadRuntimeSessionContract(path)
			_ = os.Remove(path)
			if loadErr != nil {
				return nil, errors.New("runtime successor input is incompatible")
			}
			if next.ExecutionID == current {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-ticker.C:
					continue
				}
			}
			if gate != "SUCCESSOR_READY" || next.RuntimeRevisionSHA256 != r.runtimeContract.RuntimeRevisionSHA256 {
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
