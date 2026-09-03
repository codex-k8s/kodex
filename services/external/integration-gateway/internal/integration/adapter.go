// Package integration содержит закрытый реестр типизированных adapter-ов.
package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/credentialfs"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/go-github/v74/github"
	"github.com/google/uuid"
)

const (
	maximumResponseBytes        = 64 << 10
	githubAPIBaseURL            = "https://api.github.com/"
	syntheticServiceHost        = "integration-synthetic.kodex-system.svc.cluster.local"
	exactCredentialSecretPrefix = "kodex-system/kodex-integration-credentials#"
	credentialReadRetryInterval = 250 * time.Millisecond
)

type Config struct {
	CredentialDirectory, ProxyURL, SyntheticBaseURL string
	Timeout                                         time.Duration
}

type CredentialRevision struct {
	Ref, SecretRef, SecretUID, SecretResourceVersion, ContentSHA256 string
	Revision                                                        int64
}

type Request struct {
	DefinitionKey, DefinitionVersion, DefinitionDigest, ConnectionRef string
	CapabilityKey, Operation, Risk, ApprovalPolicy                    string
	ResourceKind, ResourceScopeDigest, EffectKey, InputDigest         string
	Configuration, Input                                              map[string]any
	ResourceScope                                                     map[string]string
	Credential                                                        *CredentialRevision
}

type Receipt struct {
	EffectKey, InputDigest, ProviderEffectRef, ResponseDigest string
}

type Result struct {
	Summary string
	Receipt Receipt
}

type SafeError struct{ Code string }

func (err *SafeError) Error() string { return err.Code }

type Adapter struct {
	credentials        *credentialfs.Store
	definitions        map[string]integrationpackage.Package
	githubHTTPClient   *http.Client
	githubBaseURL      *url.URL
	providerHTTPClient *http.Client
	syntheticClient    *http.Client
	syntheticBaseURL   *url.URL
	timeout            time.Duration
}

func New(config Config) (*Adapter, error) {
	proxy, err := url.Parse(config.ProxyURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host != "egress-gateway.kodex-system.svc.cluster.local:8080" ||
		proxy.Path != "" || proxy.RawQuery != "" || proxy.User != nil {
		return nil, errors.New("integration adapter proxy is invalid")
	}
	syntheticBase, err := url.Parse(config.SyntheticBaseURL)
	if err != nil || syntheticBase.Scheme != "http" || syntheticBase.Hostname() != syntheticServiceHost ||
		syntheticBase.Port() != "8080" || syntheticBase.Path != "" || syntheticBase.RawQuery != "" || syntheticBase.User != nil {
		return nil, errors.New("synthetic integration endpoint is invalid")
	}
	credentials, err := credentialfs.New(config.CredentialDirectory)
	if err != nil || config.Timeout < time.Second || config.Timeout > 2*time.Minute {
		return nil, errors.New("integration adapter configuration is invalid")
	}
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		return nil, errors.New("load shipped integration definitions")
	}
	githubTransport := &http.Transport{
		Proxy: http.ProxyURL(proxy), ForceAttemptHTTP2: true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "api.github.com"},
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: config.Timeout,
	}
	providerTransport := githubTransport.Clone()
	providerTransport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	return &Adapter{
		credentials: credentials, definitions: definitions,
		githubHTTPClient: &http.Client{Transport: githubTransport, Timeout: config.Timeout},
		githubBaseURL:    mustURL(githubAPIBaseURL),
		providerHTTPClient: &http.Client{
			Transport: providerTransport,
			Timeout:   config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("provider redirect is forbidden")
			},
		},
		syntheticClient: &http.Client{
			Timeout: config.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("synthetic integration redirect is forbidden")
			},
		},
		syntheticBaseURL: syntheticBase, timeout: config.Timeout,
	}, nil
}

func RequestFromTest(claim *controlplanev1.IntegrationConnectionTestClaim) Request {
	configuration := map[string]any{}
	if claim.GetPublicConfiguration() != nil {
		configuration = claim.GetPublicConfiguration().AsMap()
	}
	return Request{
		DefinitionKey: claim.GetDefinitionKey(), DefinitionVersion: claim.GetDefinitionVersion(),
		DefinitionDigest: claim.GetDefinitionDigest(), ConnectionRef: claim.GetConnectionRef(),
		Configuration: configuration, Credential: credentialFromProto(claim.GetCredentialRevision()),
	}
}

func RequestFromInvocation(claim *controlplanev1.IntegrationInvocationClaim) Request {
	configuration, input := map[string]any{}, map[string]any{}
	if claim.GetPublicConfiguration() != nil {
		configuration = claim.GetPublicConfiguration().AsMap()
	}
	if claim.GetBoundedInput() != nil {
		input = claim.GetBoundedInput().AsMap()
	}
	resourceScope := map[string]string{}
	resourceKind, resourceScopeDigest := "", ""
	if scope := claim.GetResourceScope(); scope != nil {
		resourceScope = scope.GetValues()
		resourceKind = strings.TrimPrefix(scope.GetKind().String(), "INTEGRATION_RESOURCE_KIND_")
		resourceScopeDigest = scope.GetDigest()
	}
	return Request{
		DefinitionKey: claim.GetDefinitionKey(), DefinitionVersion: claim.GetDefinitionVersion(),
		DefinitionDigest: claim.GetDefinitionDigest(), ConnectionRef: claim.GetConnectionRef(),
		CapabilityKey: claim.GetCapabilityKey(), Operation: claim.GetOperation(),
		Risk:           strings.TrimPrefix(claim.GetRisk().String(), "INTEGRATION_RISK_"),
		ApprovalPolicy: strings.TrimPrefix(claim.GetApprovalPolicy().String(), "INTEGRATION_APPROVAL_POLICY_"),
		ResourceKind:   resourceKind, ResourceScope: resourceScope, ResourceScopeDigest: resourceScopeDigest,
		EffectKey: claim.GetEffectKey(), InputDigest: claim.GetInputDigest(), Configuration: configuration,
		Input: input, Credential: credentialFromProto(claim.GetCredentialRevision()),
	}
}

func credentialFromProto(value *controlplanev1.IntegrationCredentialRevision) *CredentialRevision {
	if value == nil {
		return nil
	}
	return &CredentialRevision{
		Ref: value.GetRef(), Revision: value.GetRevision(), SecretRef: value.GetSecretRef(),
		SecretUID: value.GetSecretUid(), SecretResourceVersion: value.GetSecretResourceVersion(),
		ContentSHA256: value.GetContentSha256(),
	}
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
	definition, err := adapter.validateDefinition(request)
	if err != nil {
		return "", err
	}
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return "", &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	switch definition.Spec.Adapter {
	case "SYNTHETIC_HTTP":
		_, err = adapter.callSynthetic(ctx, http.MethodGet, "/v1/journals/"+url.PathEscape(configuration["journal"]), "", nil)
	case "GITHUB":
		client, cleanup, clientErr := adapter.githubClient(ctx, request.Credential)
		if clientErr != nil {
			return "", clientErr
		}
		defer cleanup()
		_, response, providerErr := client.Repositories.Get(ctx, configuration["owner"], configuration["repository"])
		err = githubError(response, providerErr)
	case "GITLAB":
		err = adapter.testGitLab(ctx, request, configuration)
	case "JIRA":
		err = adapter.testJira(ctx, request, configuration)
	case "CONFLUENCE":
		err = adapter.testConfluence(ctx, request, configuration)
	case "EMAIL_HTTPS":
		err = adapter.testEmail(ctx, request, configuration)
	default:
		err = &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	return "i18n:INTEGRATION_TEST_SUCCEEDED", err
}

func (adapter *Adapter) Execute(ctx context.Context, request Request) (Result, error) {
	definition, capability, canonicalInput, configuration, err := adapter.validateInvocation(request)
	if err != nil {
		return Result{}, err
	}
	var result Result
	switch definition.Spec.Adapter {
	case "SYNTHETIC_HTTP":
		result, err = adapter.executeSynthetic(ctx, request, configuration, canonicalInput)
	case "GITHUB":
		result, err = adapter.executeGitHub(ctx, request, configuration, canonicalInput)
	case "GITLAB":
		result, err = adapter.executeGitLab(ctx, request, capability, configuration, canonicalInput)
	case "JIRA":
		result, err = adapter.executeJira(ctx, request, capability, configuration, canonicalInput)
	case "CONFLUENCE":
		result, err = adapter.executeConfluence(ctx, request, capability, configuration, canonicalInput)
	case "EMAIL_HTTPS":
		result, err = adapter.executeEmail(ctx, request, capability, configuration, canonicalInput)
	default:
		err = &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	if err != nil {
		return Result{}, err
	}
	canonicalOutput, err := capability.ValidateOutput([]byte(result.Summary))
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	result.Summary = string(canonicalOutput)
	responseDigest := sha256.Sum256(canonicalOutput)
	result.Receipt.ResponseDigest = hex.EncodeToString(responseDigest[:])
	if capability.Operation != request.Operation || result.Receipt.EffectKey != request.EffectKey ||
		result.Receipt.InputDigest != request.InputDigest || result.Receipt.ProviderEffectRef == "" ||
		len(result.Receipt.ResponseDigest) != sha256.Size*2 || result.Summary == "" || len(result.Summary) > maximumResponseBytes {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return result, nil
}

func (adapter *Adapter) validateDefinition(request Request) (integrationpackage.Package, error) {
	definition, exists := adapter.definitions[request.DefinitionKey]
	if !exists || definition.Metadata.Version != request.DefinitionVersion || definition.Digest != request.DefinitionDigest {
		return integrationpackage.Package{}, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	if (definition.Spec.Credential == nil) != (request.Credential == nil) {
		return integrationpackage.Package{}, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	return definition, nil
}

func (adapter *Adapter) validateInvocation(request Request) (
	integrationpackage.Package,
	integrationpackage.Capability,
	[]byte,
	map[string]string,
	error,
) {
	definition, err := adapter.validateDefinition(request)
	if err != nil {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, err
	}
	capability, exists := definition.Capability(request.CapabilityKey)
	if !exists || capability.Operation != request.Operation || capability.Risk != request.Risk ||
		capability.ApprovalPolicy != request.ApprovalPolicy || capability.ResourceScope.Kind != request.ResourceKind ||
		request.EffectKey == "" || len(request.InputDigest) != sha256.Size*2 {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	configuration, err := normalizeStringMap(request.Configuration)
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	expectedScope, err := capability.ResourceScopeValues(configuration)
	encodedScope, encodeScopeErr := json.Marshal(expectedScope)
	scopeDigest := sha256.Sum256(encodedScope)
	if err != nil || encodeScopeErr != nil || !equalStringMap(expectedScope, request.ResourceScope) ||
		hex.EncodeToString(scopeDigest[:]) != request.ResourceScopeDigest {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	encodedInput, err := json.Marshal(request.Input)
	if err != nil {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	canonicalInput, err := capability.ValidateInput(encodedInput)
	inputDigest := sha256.Sum256(canonicalInput)
	if err != nil || hex.EncodeToString(inputDigest[:]) != request.InputDigest {
		return integrationpackage.Package{}, integrationpackage.Capability{}, nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return definition, capability, canonicalInput, configuration, nil
}

func (adapter *Adapter) executeSynthetic(ctx context.Context, request Request, configuration map[string]string, canonicalInput []byte) (Result, error) {
	journal := configuration["journal"]
	path := "/v1/journals/" + url.PathEscape(journal)
	method, effectKey, body := http.MethodGet, request.EffectKey, []byte(nil)
	if request.Operation == "synthetic.journal.write" {
		method, path, body = http.MethodPost, path+"/entries", canonicalInput
	}
	response, err := adapter.callSynthetic(ctx, method, path, effectKey, body)
	if err != nil {
		return Result{}, err
	}
	var projection struct {
		Journal   string `json:"journal"`
		EffectKey string `json:"effect_key,omitempty"`
		Sequence  int64  `json:"sequence,omitempty"`
		Value     string `json:"value,omitempty"`
		Count     int64  `json:"count"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&projection) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		projection.Journal != journal || request.Operation == "synthetic.journal.write" && projection.EffectKey != request.EffectKey {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	var summary []byte
	if request.Operation == "synthetic.journal.read" {
		summary, err = json.Marshal(struct {
			Journal string `json:"journal"`
			Count   int64  `json:"count"`
		}{Journal: projection.Journal, Count: projection.Count})
	} else {
		summary, err = json.Marshal(projection)
	}
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	providerRef := "synthetic-journal:" + journal
	if projection.Sequence > 0 {
		providerRef += ":" + strconv.FormatInt(projection.Sequence, 10)
	}
	return successfulResult(string(summary), request, providerRef), nil
}

func (adapter *Adapter) callSynthetic(ctx context.Context, method, path, effectKey string, body []byte) ([]byte, error) {
	endpoint := *adapter.syntheticBaseURL
	endpoint.Path = path
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	request.Header.Set("Accept", "application/json")
	if effectKey != "" {
		request.Header.Set("Idempotency-Key", effectKey)
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := adapter.syntheticClient.Do(request)
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, statusError(response.StatusCode)
	}
	return readBoundedResponse(response.Body)
}

func (adapter *Adapter) executeGitHub(ctx context.Context, request Request, configuration map[string]string, canonicalInput []byte) (Result, error) {
	client, cleanup, err := adapter.githubClient(ctx, request.Credential)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	owner, repository := configuration["owner"], configuration["repository"]
	switch request.Operation {
	case "github.repository.metadata.read":
		provider, response, err := client.Repositories.Get(ctx, owner, repository)
		if err := githubError(response, err); err != nil {
			return Result{}, err
		}
		projection := struct {
			FullName      string `json:"full_name"`
			DefaultBranch string `json:"default_branch"`
			Visibility    string `json:"visibility"`
			Private       bool   `json:"private"`
			Archived      bool   `json:"archived"`
		}{provider.GetFullName(), provider.GetDefaultBranch(), provider.GetVisibility(), provider.GetPrivate(), provider.GetArchived()}
		summary, marshalErr := json.Marshal(projection)
		if marshalErr != nil || provider.GetID() == 0 || provider.GetFullName() != owner+"/"+repository {
			return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return successfulResult(string(summary), request, "github-repository:"+strconv.FormatInt(provider.GetID(), 10)), nil
	case "github.issue.create":
		return adapter.createGitHubIssue(ctx, client, owner, repository, request, canonicalInput)
	case "github.issue.update":
		return adapter.updateGitHubIssue(ctx, client, owner, repository, request, canonicalInput)
	default:
		return Result{}, &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
}

func (adapter *Adapter) createGitHubIssue(ctx context.Context, client *github.Client, owner, repository string, request Request, canonicalInput []byte) (Result, error) {
	var input struct{ Title, Body string }
	if json.Unmarshal(canonicalInput, &input) != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	marker := "<!-- kodex-effect:" + request.EffectKey + " -->"
	for page := 1; page <= 10; page++ {
		issues, response, err := client.Issues.ListByRepo(ctx, owner, repository, &github.IssueListByRepoOptions{
			State: "all", ListOptions: github.ListOptions{Page: page, PerPage: 100},
		})
		if err := githubError(response, err); err != nil {
			return Result{}, err
		}
		for _, issue := range issues {
			if !issue.IsPullRequest() && strings.Contains(issue.GetBody(), marker) {
				return githubIssueResult(issue, request)
			}
		}
		if response == nil || response.NextPage == 0 {
			break
		}
	}
	body := strings.TrimSpace(input.Body)
	if body != "" {
		body += "\n\n"
	}
	body += marker
	issue, response, err := client.Issues.Create(ctx, owner, repository, &github.IssueRequest{Title: github.Ptr(input.Title), Body: github.Ptr(body)})
	if err := githubError(response, err); err != nil {
		return Result{}, err
	}
	return githubIssueResult(issue, request)
}

func (adapter *Adapter) updateGitHubIssue(ctx context.Context, client *github.Client, owner, repository string, request Request, canonicalInput []byte) (Result, error) {
	var input struct {
		IssueNumber int64  `json:"issue_number"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		State       string `json:"state"`
	}
	if json.Unmarshal(canonicalInput, &input) != nil || input.IssueNumber < 1 || input.IssueNumber > int64(^uint(0)>>1) ||
		input.Title == "" && input.Body == "" && input.State == "" || input.State != "" && input.State != "open" && input.State != "closed" {
		return Result{}, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	requestBody := &github.IssueRequest{}
	if input.Title != "" {
		requestBody.Title = github.Ptr(input.Title)
	}
	if input.Body != "" {
		requestBody.Body = github.Ptr(input.Body)
	}
	if input.State != "" {
		requestBody.State = github.Ptr(input.State)
	}
	issue, response, err := client.Issues.Edit(ctx, owner, repository, int(input.IssueNumber), requestBody)
	if err := githubError(response, err); err != nil {
		return Result{}, err
	}
	return githubIssueResult(issue, request)
}

func githubIssueResult(issue *github.Issue, request Request) (Result, error) {
	if issue == nil || issue.GetNumber() < 1 || issue.GetID() == 0 {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	projection := struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		State  string `json:"state"`
	}{issue.GetNumber(), issue.GetTitle(), issue.GetState()}
	summary, err := json.Marshal(projection)
	if err != nil {
		return Result{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return successfulResult(string(summary), request, "github-issue:"+strconv.Itoa(issue.GetNumber())), nil
}

func (adapter *Adapter) githubClient(ctx context.Context, credential *CredentialRevision) (*github.Client, func(), error) {
	value, err := adapter.readCredential(ctx, credential)
	if err != nil {
		return nil, func() {}, err
	}
	client := github.NewClient(adapter.githubHTTPClient).WithAuthToken(string(value))
	client.BaseURL = adapter.githubBaseURL
	return client, func() { clear(value) }, nil
}

func (adapter *Adapter) readCredential(ctx context.Context, credential *CredentialRevision) ([]byte, error) {
	if credential == nil || credential.Ref == "" || credential.Revision < 1 || uuid.Validate(credential.SecretUID) != nil ||
		credential.SecretResourceVersion == "" || len(credential.SecretResourceVersion) > 128 ||
		len(credential.ContentSHA256) != sha256.Size*2 || !strings.HasPrefix(credential.SecretRef, exactCredentialSecretPrefix) {
		return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
	}
	key := strings.TrimPrefix(credential.SecretRef, exactCredentialSecretPrefix)
	for {
		value, err := adapter.credentials.ReadKey(key)
		if err == nil {
			value = bytes.TrimSpace(value)
			digest := sha256.Sum256(value)
			if len(value) == 0 || hex.EncodeToString(digest[:]) != credential.ContentSHA256 {
				clear(value)
				return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
			}
			return value, nil
		}
		timer := time.NewTimer(credentialReadRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, &SafeError{Code: "INTEGRATION_CREDENTIAL_UNAVAILABLE"}
		case <-timer.C:
		}
	}
}

func successfulResult(summary string, request Request, providerRef string) Result {
	digest := sha256.Sum256([]byte(summary))
	return Result{Summary: summary, Receipt: Receipt{
		EffectKey: request.EffectKey, InputDigest: request.InputDigest, ProviderEffectRef: providerRef,
		ResponseDigest: hex.EncodeToString(digest[:]),
	}}
}

func githubError(response *github.Response, err error) error {
	if err == nil {
		return nil
	}
	if response == nil {
		return &SafeError{Code: "INTEGRATION_UNAVAILABLE"}
	}
	return statusError(response.StatusCode)
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

func readBoundedResponse(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maximumResponseBytes+1))
	if err != nil || len(body) > maximumResponseBytes {
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return body, nil
}

func normalizeStringMap(values map[string]any) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for key, raw := range values {
		value, ok := raw.(string)
		if !ok {
			return nil, errors.New("configuration value is not a string")
		}
		result[key] = value
	}
	return result, nil
}

func equalStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func mustURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(fmt.Sprintf("parse static URL: %v", err))
	}
	return parsed
}
