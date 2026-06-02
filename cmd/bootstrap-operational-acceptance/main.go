package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/stackinventory"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "repository root containing services.yaml")
	envFile := flag.String("env-file", "", "bootstrap env file; values are loaded but never printed")
	servicesFile := flag.String("services-file", "", "root services.yaml path; defaults to <repo-root>/services.yaml")
	skipPublicHTTP := flag.Bool("skip-public-http", false, "skip public HTTPS redirect check")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := run(ctx, acceptanceOptions{
		RepoRoot:         *repoRoot,
		EnvFilePath:      *envFile,
		ServicesFilePath: *servicesFile,
		SkipPublicHTTP:   *skipPublicHTTP,
	}, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap-operational-acceptance: %v\n", err)
		os.Exit(1)
	}
}

type acceptanceOptions struct {
	RepoRoot         string
	EnvFilePath      string
	ServicesFilePath string
	SkipPublicHTTP   bool
}

type acceptanceConfig struct {
	Namespace    string
	PublicURL    string
	RegistryPort string
}

type commandRunner func(context.Context, ...string) ([]byte, error)

var kubectlRunner commandRunner = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	return cmd.CombinedOutput()
}

func run(ctx context.Context, options acceptanceOptions, output io.Writer) error {
	repoRoot, err := absolutePath(firstNonEmpty(options.RepoRoot, "."))
	if err != nil {
		return err
	}
	envFile := strings.TrimSpace(options.EnvFilePath)
	if envFile != "" {
		envFile, err = absolutePath(envFile)
		if err != nil {
			return err
		}
		if err := stackinventory.LoadEnvFile(envFile); err != nil {
			return err
		}
		ok(output, "env file loaded; values hidden")
	}

	servicesFile := strings.TrimSpace(options.ServicesFilePath)
	if servicesFile == "" {
		servicesFile = filepath.Join(repoRoot, "services.yaml")
	}
	stack, err := stackinventory.LoadFile(servicesFile)
	if err != nil {
		return err
	}
	ok(output, "root services.yaml loaded through stackinventory")

	config, err := loadAcceptanceConfig()
	if err != nil {
		return err
	}
	ok(output, "acceptance config loaded without printing values")

	if err := resolveBuilderRefs(stack); err != nil {
		return err
	}
	ok(output, "registry and builder image refs resolved from services.yaml")

	if err := kubectlReadyz(ctx); err != nil {
		return err
	}
	ok(output, "Kubernetes API readiness check passed")

	if err := checkWorkloads(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "deployments and PostgreSQL statefulset are ready")

	if err := checkJobs(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "migration and bootstrap jobs are complete")

	if err := checkNoActiveFailedBuildJobs(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "no active or failed backend build jobs remain")

	if err := checkServices(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "service objects are present and internal services stay ClusterIP")

	if err := checkOperatorHTTPSurface(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "operator HTTP surface responds through local port-forward")

	if err := checkSecrets(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "required Kubernetes secret keys are present without printing values")

	if err := checkRegistryEndpoint(ctx, config.RegistryPort); err != nil {
		return err
	}
	ok(output, "local registry endpoint responds on loopback")

	if err := checkPublicContour(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "public web contour routes only through oauth2-proxy and certificate is ready")

	if !options.SkipPublicHTTP {
		if err := checkPublicRedirect(config.PublicURL); err != nil {
			return err
		}
		ok(output, "public web root redirects to GitHub OAuth without exposing query values")
	}

	ok(output, "operational acceptance completed")
	return nil
}

func loadAcceptanceConfig() (acceptanceConfig, error) {
	namespace := strings.TrimSpace(stackinventory.EnvOr("KODEX_PRODUCTION_NAMESPACE", ""))
	if namespace == "" {
		return acceptanceConfig{}, errors.New("KODEX_PRODUCTION_NAMESPACE is required")
	}
	publicURL := strings.TrimSpace(stackinventory.EnvOr("KODEX_PUBLIC_BASE_URL", ""))
	if publicURL == "" {
		return acceptanceConfig{}, errors.New("KODEX_PUBLIC_BASE_URL is required")
	}
	return acceptanceConfig{
		Namespace:    namespace,
		PublicURL:    publicURL,
		RegistryPort: strings.TrimSpace(stackinventory.EnvOr("KODEX_INTERNAL_REGISTRY_PORT", "5000")),
	}, nil
}

func resolveBuilderRefs(stack stackinventory.Stack) error {
	for _, name := range []string{"registry", "kaniko-executor", "crane", "busybox"} {
		if _, err := stack.Image(name); err != nil {
			return fmt.Errorf("resolve %s image: %w", name, err)
		}
	}
	return nil
}

func kubectlReadyz(ctx context.Context) error {
	if _, err := kubectl(ctx, "get", "--raw=/readyz"); err != nil {
		return fmt.Errorf("check Kubernetes API readyz: %w", err)
	}
	return nil
}

func checkWorkloads(ctx context.Context, namespace string) error {
	for _, name := range requiredDeployments() {
		if err := checkDeployment(ctx, namespace, name); err != nil {
			return err
		}
	}
	if err := checkStatefulSet(ctx, namespace, "postgres"); err != nil {
		return err
	}
	return nil
}

func requiredDeployments() []string {
	return []string{
		"access-manager",
		"project-catalog",
		"package-hub",
		"provider-hub",
		"fleet-manager",
		"runtime-manager",
		"interaction-hub",
		"governance-manager",
		"agent-manager",
		"integration-gateway",
		"codex-hook-ingress",
		"staff-gateway",
		"platform-mcp-server",
		"web-console",
		"web-console-public-oauth2-proxy",
		"kodex-public-ingress",
		"kodex-registry",
	}
}

func checkDeployment(ctx context.Context, namespace, name string) error {
	var deployment deploymentStatus
	if err := getKubernetesJSON(ctx, &deployment, "-n", namespace, "get", "deployment", name, "-o", "json"); err != nil {
		return fmt.Errorf("read deployment %s: %w", name, err)
	}
	if !deploymentReady(deployment) {
		return fmt.Errorf("deployment %s is not ready", name)
	}
	return nil
}

func checkStatefulSet(ctx context.Context, namespace, name string) error {
	var statefulSet statefulSetStatus
	if err := getKubernetesJSON(ctx, &statefulSet, "-n", namespace, "get", "statefulset", name, "-o", "json"); err != nil {
		return fmt.Errorf("read statefulset %s: %w", name, err)
	}
	if !statefulSetReady(statefulSet) {
		return fmt.Errorf("statefulset %s is not ready", name)
	}
	return nil
}

func checkJobs(ctx context.Context, namespace string) error {
	for _, name := range requiredJobs() {
		if err := checkJobComplete(ctx, namespace, name); err != nil {
			return err
		}
	}
	return nil
}

func requiredJobs() []string {
	return []string{
		"kodex-postgres-bootstrap-databases",
		"platform-event-log-migrations",
		"access-manager-migrations",
		"project-catalog-migrations",
		"package-hub-migrations",
		"provider-hub-migrations",
		"fleet-manager-migrations",
		"runtime-manager-migrations",
		"interaction-hub-migrations",
		"governance-manager-migrations",
		"agent-manager-migrations",
	}
}

func checkJobComplete(ctx context.Context, namespace, name string) error {
	var job jobStatus
	if err := getKubernetesJSON(ctx, &job, "-n", namespace, "get", "job", name, "-o", "json"); err != nil {
		return fmt.Errorf("read job %s: %w", name, err)
	}
	if !jobComplete(job) {
		return fmt.Errorf("job %s is not complete", name)
	}
	return nil
}

func checkNoActiveFailedBuildJobs(ctx context.Context, namespace string) error {
	var jobs jobList
	if err := getKubernetesJSON(ctx, &jobs, "-n", namespace, "get", "jobs", "-l", "app.kubernetes.io/component=backend-image-build", "-o", "json"); err != nil {
		return fmt.Errorf("read backend build jobs: %w", err)
	}
	for _, job := range jobs.Items {
		if job.Status.Active > 0 {
			return fmt.Errorf("backend build job %s is still active", job.Metadata.Name)
		}
		if job.Status.Failed > 0 && job.Status.Succeeded == 0 {
			return fmt.Errorf("backend build job %s failed", job.Metadata.Name)
		}
	}
	return nil
}

func checkServices(ctx context.Context, namespace string) error {
	for _, name := range requiredServices() {
		service, err := readService(ctx, namespace, name)
		if err != nil {
			return err
		}
		if service.Spec.Type != "ClusterIP" {
			return fmt.Errorf("service %s must stay ClusterIP", name)
		}
	}
	return nil
}

func requiredServices() []string {
	return []string{
		"access-manager",
		"project-catalog",
		"package-hub",
		"provider-hub",
		"fleet-manager",
		"runtime-manager",
		"interaction-hub",
		"governance-manager",
		"agent-manager",
		"integration-gateway",
		"codex-hook-ingress",
		"staff-gateway",
		"platform-mcp-server",
		"web-console",
		"web-console-public-oauth2-proxy",
		"kodex-registry",
		"postgres",
	}
}

func readService(ctx context.Context, namespace, name string) (serviceObject, error) {
	var service serviceObject
	if err := getKubernetesJSON(ctx, &service, "-n", namespace, "get", "service", name, "-o", "json"); err != nil {
		return serviceObject{}, fmt.Errorf("read service %s: %w", name, err)
	}
	return service, nil
}

func checkOperatorHTTPSurface(ctx context.Context, namespace string) error {
	checks := []struct {
		Service string
		Port    string
		Path    string
	}{
		{Service: "staff-gateway", Port: "8080", Path: "/openapi/staff-gateway.v1.yaml"},
		{Service: "web-console", Port: "8080", Path: "/"},
		{Service: "web-console-public-oauth2-proxy", Port: "4180", Path: "/ping"},
	}
	for _, check := range checks {
		if err := checkHTTP(ctx, namespace, check.Service, check.Port, check.Path); err != nil {
			return fmt.Errorf("check %s%s: %w", check.Service, check.Path, err)
		}
	}
	return nil
}

func checkHTTP(ctx context.Context, namespace, service, targetPort, path string) error {
	localPort, err := freeLocalPort()
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "kubectl", "-n", namespace, "port-forward", "svc/"+service, localPort+":"+targetPort)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return err
	}
	defer func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	client := http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+localPort+path, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("HTTP endpoint did not become healthy")
}

func freeLocalPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = listener.Close() }()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), nil
}

func checkSecrets(ctx context.Context, namespace string) error {
	required := map[string][]string{
		"kodex-postgres": {
			"KODEX_POSTGRES_DB",
			"KODEX_POSTGRES_PASSWORD",
			"KODEX_POSTGRES_USER",
		},
		"kodex-web-oauth2-proxy": {
			"KODEX_GITHUB_OAUTH_CLIENT_ID",
			"KODEX_GITHUB_OAUTH_CLIENT_SECRET",
			"KODEX_OAUTH2_PROXY_COOKIE_SECRET",
		},
		"kodex-platform-runtime": requiredRuntimeSecretKeys(),
	}
	for name, keys := range required {
		if err := checkSecretKeys(ctx, namespace, name, keys); err != nil {
			return err
		}
	}
	return nil
}

func requiredRuntimeSecretKeys() []string {
	return []string{
		"KODEX_ACCESS_MANAGER_DATABASE_DSN",
		"KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_PROJECT_CATALOG_DATABASE_DSN",
		"KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN",
		"KODEX_PACKAGE_HUB_DATABASE_DSN",
		"KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN",
		"KODEX_PROVIDER_HUB_DATABASE_DSN",
		"KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN",
		"KODEX_FLEET_MANAGER_DATABASE_DSN",
		"KODEX_FLEET_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_RUNTIME_MANAGER_DATABASE_DSN",
		"KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_INTERACTION_HUB_DATABASE_DSN",
		"KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN",
		"KODEX_GOVERNANCE_MANAGER_DATABASE_DSN",
		"KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_DATABASE_DSN",
		"KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN",
		"KODEX_EXTERNAL_CALLBACK_SECRET",
		"KODEX_PLATFORM_MCP_SERVER_MCP_AUTH_TOKEN",
	}
}

func checkSecretKeys(ctx context.Context, namespace, name string, required []string) error {
	var secret secretObject
	if err := getKubernetesJSON(ctx, &secret, "-n", namespace, "get", "secret", name, "-o", "json"); err != nil {
		return fmt.Errorf("read secret %s: %w", name, err)
	}
	missing := missingSecretKeys(secret, required)
	if len(missing) > 0 {
		return fmt.Errorf("secret %s is missing required keys: %s", name, strings.Join(missing, ", "))
	}
	return nil
}

func checkRegistryEndpoint(ctx context.Context, port string) error {
	if port == "" {
		port = "5000"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/v2/", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("check local registry endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("local registry endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func checkPublicContour(ctx context.Context, namespace string) error {
	var ingress ingressObject
	if err := getKubernetesJSON(ctx, &ingress, "-n", namespace, "get", "ingress", "web-console-public", "-o", "json"); err != nil {
		return fmt.Errorf("read public web ingress: %w", err)
	}
	if err := validatePublicIngressTargets(ingress); err != nil {
		return err
	}
	if err := checkConditionedResource(ctx, "certificate web-console-public-tls", "-n", namespace, "get", "certificate", "web-console-public-tls", "-o", "json"); err != nil {
		return err
	}
	if err := checkConditionedResource(ctx, "clusterissuer kodex-letsencrypt-production", "get", "clusterissuer", "kodex-letsencrypt-production", "-o", "json"); err != nil {
		return err
	}
	return nil
}

func checkConditionedResource(ctx context.Context, label string, args ...string) error {
	var resource conditionedResource
	if err := getKubernetesJSON(ctx, &resource, args...); err != nil {
		return fmt.Errorf("read %s: %w", label, err)
	}
	if !conditionReady(resource.Status.Conditions) {
		return fmt.Errorf("%s is not Ready", label)
	}
	return nil
}

func checkPublicRedirect(rawURL string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("check public web root: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusTemporaryRedirect {
		return fmt.Errorf("public web root returned HTTP %d instead of OAuth redirect", resp.StatusCode)
	}
	if err := validateGitHubOAuthLocation(resp.Header.Get("Location")); err != nil {
		return err
	}
	return nil
}

func validateGitHubOAuthLocation(location string) error {
	parsed, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("parse OAuth redirect location: %w", err)
	}
	if parsed.Host != "github.com" || parsed.Path != "/login/oauth/authorize" {
		return errors.New("public web root does not redirect to GitHub OAuth authorize endpoint")
	}
	return nil
}

func validatePublicIngressTargets(ingress ingressObject) error {
	targets := map[string]bool{}
	for _, rule := range ingress.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service.Name != "" {
				targets[path.Backend.Service.Name] = true
			}
		}
	}
	if len(targets) == 0 {
		return errors.New("public web ingress has no service targets")
	}
	if targets["web-console"] {
		return errors.New("public web ingress must not target web-console directly")
	}
	if !targets["web-console-public-oauth2-proxy"] {
		return errors.New("public web ingress must target oauth2-proxy")
	}
	if len(targets) != 1 {
		return errors.New("public web ingress must target only oauth2-proxy")
	}
	return nil
}

func deploymentReady(deployment deploymentStatus) bool {
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	return desired > 0 &&
		deployment.Status.ReadyReplicas >= desired &&
		deployment.Status.AvailableReplicas >= desired &&
		conditionReady(deployment.Status.Conditions)
}

func statefulSetReady(statefulSet statefulSetStatus) bool {
	desired := int32(1)
	if statefulSet.Spec.Replicas != nil {
		desired = *statefulSet.Spec.Replicas
	}
	return desired > 0 && statefulSet.Status.ReadyReplicas >= desired
}

func jobComplete(job jobStatus) bool {
	if job.Status.Failed > 0 && job.Status.Succeeded == 0 {
		return false
	}
	return conditionTrue(job.Status.Conditions, "Complete")
}

func conditionReady(conditions []condition) bool {
	return conditionTrue(conditions, "Ready") || conditionTrue(conditions, "Available")
}

func conditionTrue(conditions []condition, conditionType string) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType && condition.Status == "True" {
			return true
		}
	}
	return false
}

func missingSecretKeys(secret secretObject, required []string) []string {
	missing := []string{}
	for _, key := range required {
		if strings.TrimSpace(secret.Data[key]) == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func getKubernetesJSON(ctx context.Context, target any, args ...string) error {
	output, err := kubectl(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(output, target); err != nil {
		return fmt.Errorf("decode kubectl JSON: %w", err)
	}
	return nil
}

func kubectl(ctx context.Context, args ...string) ([]byte, error) {
	output, err := kubectlRunner(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("kubectl %s: %w", safeKubectlArgs(args), err)
	}
	return output, nil
}

func safeKubectlArgs(args []string) string {
	return strings.Join(args, " ")
}

func ok(output io.Writer, message string) {
	_, _ = fmt.Fprintln(output, "OK: "+message)
}

func absolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %s: %w", path, err)
	}
	return absolute, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type condition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

type deploymentStatus struct {
	Spec struct {
		Replicas *int32 `json:"replicas"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas     int32       `json:"readyReplicas"`
		AvailableReplicas int32       `json:"availableReplicas"`
		Conditions        []condition `json:"conditions"`
	} `json:"status"`
}

type statefulSetStatus struct {
	Spec struct {
		Replicas *int32 `json:"replicas"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int32 `json:"readyReplicas"`
	} `json:"status"`
}

type jobStatus struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Active     int32       `json:"active"`
		Succeeded  int32       `json:"succeeded"`
		Failed     int32       `json:"failed"`
		Conditions []condition `json:"conditions"`
	} `json:"status"`
}

type jobList struct {
	Items []jobStatus `json:"items"`
}

type serviceObject struct {
	Spec struct {
		Type string `json:"type"`
	} `json:"spec"`
}

type secretObject struct {
	Data map[string]string `json:"data"`
}

type conditionedResource struct {
	Status struct {
		Conditions []condition `json:"conditions"`
	} `json:"status"`
}

type ingressObject struct {
	Spec struct {
		Rules []struct {
			HTTP struct {
				Paths []struct {
					Backend struct {
						Service struct {
							Name string `json:"name"`
						} `json:"service"`
					} `json:"backend"`
				} `json:"paths"`
			} `json:"http"`
		} `json:"rules"`
	} `json:"spec"`
}
