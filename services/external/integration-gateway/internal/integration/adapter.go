// Package integration содержит закрытый реестр типизированных adapter-ов.
package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
)

const maximumResponseBytes = 64 << 10

var (
	credentialRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
	dnsLabelPattern      = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
)

type Config struct {
	CredentialDirectory, ProxyURL, AllowedHosts string
	Timeout                                     time.Duration
}

type Request struct {
	DefinitionKey, ConnectionRef, CredentialRef, CapabilityKey string
	Configuration, Input                                       map[string]any
}

type githubContentResponse struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Size     int64  `json:"size"`
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

type githubContentProjection struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	SHA     string `json:"sha"`
	Size    int64  `json:"size"`
	Content string `json:"content_base64,omitempty"`
}

type kubernetesWorkloadResponse struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name       string            `json:"name"`
		Namespace  string            `json:"namespace"`
		Generation int64             `json:"generation"`
		Labels     map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		Phase              string `json:"phase"`
		ObservedGeneration int64  `json:"observedGeneration"`
		Replicas           int32  `json:"replicas"`
		ReadyReplicas      int32  `json:"readyReplicas"`
		AvailableReplicas  int32  `json:"availableReplicas"`
		Succeeded          int32  `json:"succeeded"`
		Failed             int32  `json:"failed"`
		Conditions         []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"conditions"`
	} `json:"status"`
}

type kubernetesConditionProjection struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type kubernetesWorkloadProjection struct {
	APIVersion         string                          `json:"api_version"`
	Kind               string                          `json:"kind"`
	Name               string                          `json:"name"`
	Namespace          string                          `json:"namespace"`
	Generation         int64                           `json:"generation"`
	ObservedGeneration int64                           `json:"observed_generation"`
	Phase              string                          `json:"phase,omitempty"`
	Replicas           int32                           `json:"replicas,omitempty"`
	ReadyReplicas      int32                           `json:"ready_replicas,omitempty"`
	AvailableReplicas  int32                           `json:"available_replicas,omitempty"`
	Succeeded          int32                           `json:"succeeded,omitempty"`
	Failed             int32                           `json:"failed,omitempty"`
	Conditions         []kubernetesConditionProjection `json:"conditions,omitempty"`
}

type SafeError struct{ Code string }

func (err *SafeError) Error() string { return err.Code }

type Adapter struct {
	credentialDirectory string
	proxyURL            *url.URL
	allowedHosts        map[string]struct{}
	timeout             time.Duration
}

func New(config Config) (*Adapter, error) {
	proxy, err := url.Parse(config.ProxyURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host == "" {
		return nil, errors.New("integration adapter proxy is invalid")
	}
	if !filepath.IsAbs(config.CredentialDirectory) || config.Timeout < time.Second || config.Timeout > 2*time.Minute {
		return nil, errors.New("integration adapter configuration is invalid")
	}
	hosts := map[string]struct{}{"api.github.com": {}}
	for _, item := range strings.Split(config.AllowedHosts, ",") {
		host := strings.ToLower(strings.TrimSpace(item))
		if host == "" {
			continue
		}
		if parsed := net.ParseIP(host); parsed != nil || !validHostname(host) {
			return nil, errors.New("integration host allowlist is invalid")
		}
		hosts[host] = struct{}{}
	}
	return &Adapter{credentialDirectory: filepath.Clean(config.CredentialDirectory), proxyURL: proxy, allowedHosts: hosts, timeout: config.Timeout}, nil
}

func RequestFromTest(claim *controlplanev1.IntegrationConnectionTestClaim) Request {
	configuration := map[string]any{}
	if claim.GetPublicConfiguration() != nil {
		configuration = claim.GetPublicConfiguration().AsMap()
	}
	return Request{DefinitionKey: claim.GetDefinitionKey(), ConnectionRef: claim.GetConnectionRef(), CredentialRef: claim.GetCredentialMaterializationRef(), Configuration: configuration}
}

func RequestFromInvocation(claim *controlplanev1.IntegrationInvocationClaim) Request {
	configuration, input := map[string]any{}, map[string]any{}
	if claim.GetPublicConfiguration() != nil {
		configuration = claim.GetPublicConfiguration().AsMap()
	}
	if claim.GetBoundedInput() != nil {
		input = claim.GetBoundedInput().AsMap()
	}
	return Request{DefinitionKey: claim.GetDefinitionKey(), ConnectionRef: claim.GetConnectionRef(), CredentialRef: claim.GetCredentialMaterializationRef(), CapabilityKey: claim.GetCapabilityKey(), Configuration: configuration, Input: input}
}

func Outcome(err error) (bool, string) {
	if err == nil {
		return true, ""
	}
	var safe *SafeError
	if errors.As(err, &safe) {
		return false, safe.Code
	}
	return false, "INTEGRATION_UNAVAILABLE"
}

func (adapter *Adapter) Test(ctx context.Context, request Request) (string, error) {
	switch request.DefinitionKey {
	case "github":
		_, err := adapter.call(ctx, request, http.MethodGet, "https://api.github.com/user", nil, nil)
		return "i18n:INTEGRATION_TEST_SUCCEEDED", err
	case "kubernetes":
		base, err := adapter.configuredBaseURL(request, "server_url")
		if err != nil {
			return "", err
		}
		roots, err := adapter.loadCA(request.CredentialRef)
		if err != nil {
			return "", err
		}
		_, err = adapter.call(ctx, request, http.MethodGet, base+"/version", nil, roots)
		return "i18n:INTEGRATION_TEST_SUCCEEDED", err
	case "mattermost":
		base, err := adapter.configuredBaseURL(request, "base_url")
		if err != nil {
			return "", err
		}
		_, err = adapter.call(ctx, request, http.MethodGet, base+"/api/v4/system/ping", nil, nil)
		return "i18n:INTEGRATION_TEST_SUCCEEDED", err
	default:
		return "", &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func (adapter *Adapter) Execute(ctx context.Context, request Request) (string, error) {
	switch request.CapabilityKey {
	case "github.repository.read":
		return adapter.readGitHubRepository(ctx, request)
	case "kubernetes.workload.read":
		return adapter.readKubernetesWorkload(ctx, request)
	case "mattermost.notifications", "mattermost.result_mirror":
		return adapter.sendMattermostMessage(ctx, request)
	default:
		return "", &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func (adapter *Adapter) readGitHubRepository(ctx context.Context, request Request) (string, error) {
	owner, ownerOK := boundedString(request.Configuration, "owner", 100)
	repository, repositoryOK := boundedString(request.Configuration, "repository", 100)
	pathValue, pathOK := boundedString(request.Input, "path", 1024)
	ref, refOK := boundedString(request.Input, "ref", 128)
	if !ownerOK || !repositoryOK || !pathOK || !refOK || !safeRepositoryPath(pathValue) {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	segments := strings.Split(pathValue, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	endpoint := "https://api.github.com/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/contents/" + strings.Join(segments, "/") + "?ref=" + url.QueryEscape(ref)
	body, err := adapter.call(ctx, request, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return "", err
	}
	return projectGitHubContent(body, pathValue)
}

func (adapter *Adapter) readKubernetesWorkload(ctx context.Context, request Request) (string, error) {
	base, err := adapter.configuredBaseURL(request, "server_url")
	if err != nil {
		return "", err
	}
	resource, resourceOK := boundedString(request.Input, "resource", 32)
	namespace, namespaceOK := boundedString(request.Input, "namespace", 63)
	name, nameOK := boundedString(request.Input, "name", 253)
	if !resourceOK || !namespaceOK || !nameOK || !dnsLabelPattern.MatchString(namespace) || !dnsLabelPattern.MatchString(name) || !allowedNamespace(request.Configuration, namespace) {
		return "", &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	apiPrefix := map[string]string{"pods": "/api/v1", "deployments": "/apis/apps/v1", "statefulsets": "/apis/apps/v1", "jobs": "/apis/batch/v1"}[resource]
	if apiPrefix == "" {
		return "", &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	endpoint := base + apiPrefix + "/namespaces/" + url.PathEscape(namespace) + "/" + resource + "/" + url.PathEscape(name)
	roots, err := adapter.loadCA(request.CredentialRef)
	if err != nil {
		return "", err
	}
	body, err := adapter.call(ctx, request, http.MethodGet, endpoint, nil, roots)
	if err != nil {
		return "", err
	}
	return projectKubernetesWorkload(body, namespace, name)
}

func (adapter *Adapter) sendMattermostMessage(ctx context.Context, request Request) (string, error) {
	base, err := adapter.configuredBaseURL(request, "base_url")
	if err != nil {
		return "", err
	}
	team, teamOK := boundedString(request.Configuration, "team_name", 64)
	channel, channelOK := boundedString(request.Configuration, "channel_name", 64)
	message, messageOK := boundedString(request.Input, "message", 4000)
	if !teamOK || !channelOK || !messageOK || !dnsLabelPattern.MatchString(team) || !dnsLabelPattern.MatchString(channel) {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	channelBody, err := adapter.call(ctx, request, http.MethodGet, base+"/api/v4/teams/name/"+url.PathEscape(team)+"/channels/name/"+url.PathEscape(channel), nil, nil)
	if err != nil {
		return "", err
	}
	var channelResult struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(channelBody, &channelResult) != nil || channelResult.ID == "" || len(channelResult.ID) > 64 {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	payload, _ := json.Marshal(map[string]string{"channel_id": channelResult.ID, "message": message})
	if _, err := adapter.call(ctx, request, http.MethodPost, base+"/api/v4/posts", payload, nil); err != nil {
		return "", err
	}
	return "MATTERMOST_DELIVERY_ACCEPTED", nil
}

func (adapter *Adapter) configuredBaseURL(request Request, key string) (string, error) {
	raw, ok := boundedString(request.Configuration, key, 2048)
	if !ok {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	host := strings.ToLower(parsed.Hostname())
	if _, allowed := adapter.allowedHosts[host]; !allowed {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	parsed.Path = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func (adapter *Adapter) call(ctx context.Context, request Request, method, endpoint string, body []byte, roots *x509.CertPool) ([]byte, error) {
	credential, err := adapter.readCredential(request.CredentialRef, "token")
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	defer clear(credential)
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	if _, allowed := adapter.allowedHosts[strings.ToLower(parsed.Hostname())]; !allowed {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: parsed.Hostname(), RootCAs: roots}
	transport := &http.Transport{Proxy: http.ProxyURL(adapter.proxyURL), ForceAttemptHTTP2: true, TLSClientConfig: tlsConfig, MaxIdleConns: 2, MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: adapter.timeout}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: adapter.timeout}
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+string(credential))
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "MatterCodex/integration-gateway")
	if len(body) > 0 {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, statusError(response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maximumResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil || len(responseBody) > maximumResponseBytes {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return responseBody, nil
}

func (adapter *Adapter) loadCA(ref string) (*x509.CertPool, error) {
	raw, err := adapter.readCredential(ref, "ca.pem")
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	defer clear(raw)
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	return roots, nil
}

func (adapter *Adapter) readCredential(ref, name string) ([]byte, error) {
	if !credentialRefPattern.MatchString(ref) {
		return nil, errors.New("credential reference is invalid")
	}
	root, err := filepath.EvalSymlinks(adapter.credentialDirectory)
	if err != nil {
		return nil, errors.New("credential root is unavailable")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, ref, name))
	if err != nil {
		return nil, errors.New("credential file is unavailable")
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return nil, errors.New("credential file escapes root")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 1<<20 || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("credential file is unsafe")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return nil, errors.New("read credential file")
	}
	return bytes.TrimSpace(raw), nil
}

func statusError(code int) error {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &SafeError{Code: "INTEGRATION_AUTH_REJECTED"}
	case http.StatusTooManyRequests:
		return &SafeError{Code: "INTEGRATION_RATE_LIMITED"}
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	default:
		return &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
}

func boundedString(values map[string]any, key string, maximum int) (string, bool) {
	value, ok := values[key].(string)
	value = strings.TrimSpace(value)
	return value, ok && value != "" && len(value) <= maximum
}

func safeRepositoryPath(value string) bool {
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func allowedNamespace(configuration map[string]any, namespace string) bool {
	raw, ok := configuration["allowed_namespaces"].([]any)
	if !ok || len(raw) == 0 || len(raw) > 64 {
		return false
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || !dnsLabelPattern.MatchString(value) {
			return false
		}
		values = append(values, value)
	}
	sort.Strings(values)
	index := sort.SearchStrings(values, namespace)
	return index < len(values) && values[index] == namespace
}

func validHostname(value string) bool {
	if len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) < 1 || len(label) > 63 || !dnsLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}

func projectGitHubContent(body []byte, expectedPath string) (string, error) {
	var provider githubContentResponse
	if json.Unmarshal(body, &provider) != nil || provider.Type != "file" || provider.Name == "" || provider.Path != expectedPath || provider.SHA == "" || provider.Size < 0 || provider.Size > maximumResponseBytes || provider.Encoding != "base64" || len(provider.Content) > maximumResponseBytes {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	projected, err := json.Marshal(githubContentProjection{Type: provider.Type, Name: provider.Name, Path: provider.Path, SHA: provider.SHA, Size: provider.Size, Content: provider.Content})
	if err != nil || len(projected) > maximumResponseBytes {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return string(projected), nil
}

func projectKubernetesWorkload(body []byte, namespace, name string) (string, error) {
	var provider kubernetesWorkloadResponse
	if json.Unmarshal(body, &provider) != nil || provider.APIVersion == "" || provider.Kind == "" || provider.Metadata.Name != name || provider.Metadata.Namespace != namespace || len(provider.Status.Conditions) > 32 {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	projection := kubernetesWorkloadProjection{
		APIVersion: provider.APIVersion, Kind: provider.Kind, Name: provider.Metadata.Name, Namespace: provider.Metadata.Namespace,
		Generation: provider.Metadata.Generation, ObservedGeneration: provider.Status.ObservedGeneration, Phase: provider.Status.Phase,
		Replicas: provider.Status.Replicas, ReadyReplicas: provider.Status.ReadyReplicas, AvailableReplicas: provider.Status.AvailableReplicas,
		Succeeded: provider.Status.Succeeded, Failed: provider.Status.Failed,
	}
	for _, condition := range provider.Status.Conditions {
		if len(condition.Type) > 128 || len(condition.Status) > 32 || len(condition.Reason) > 256 {
			return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		projection.Conditions = append(projection.Conditions, kubernetesConditionProjection{Type: condition.Type, Status: condition.Status, Reason: condition.Reason})
	}
	projected, err := json.Marshal(projection)
	if err != nil || len(projected) > maximumResponseBytes {
		return "", &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return string(projected), nil
}
