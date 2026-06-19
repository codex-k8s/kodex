package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/manifestrender"
	"github.com/codex-k8s/kodex/libs/go/stackinventory"
	"github.com/google/uuid"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "repository root containing services.yaml and deploy/base")
	envFile := flag.String("env-file", "", "bootstrap env file; values are loaded but never printed")
	servicesFile := flag.String("services-file", "", "root services.yaml path; defaults to <repo-root>/services.yaml")
	renderDir := flag.String("render-dir", "", "optional empty output directory for rendered dry-run manifests")
	requireKubernetes := flag.Bool("require-kubernetes", false, "fail if live Kubernetes foundation checks are unavailable")
	skipLiveKubernetes := flag.Bool("skip-live-kubernetes", false, "skip live kubectl checks")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := run(ctx, planOptions{
		RepoRoot:           *repoRoot,
		EnvFilePath:        *envFile,
		ServicesFilePath:   *servicesFile,
		RenderDir:          *renderDir,
		RequireKubernetes:  *requireKubernetes,
		SkipLiveKubernetes: *skipLiveKubernetes,
	}, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap-deploy-plan: %v\n", err)
		os.Exit(1)
	}
}

type planOptions struct {
	RepoRoot           string
	EnvFilePath        string
	ServicesFilePath   string
	RenderDir          string
	RequireKubernetes  bool
	SkipLiveKubernetes bool
}

type planConfig struct {
	Namespace               string
	PublicIngressConfigured bool
	IDPConfigured           bool
}

type manifestSet struct {
	Name      string
	Source    string
	Output    string
	Kustomize bool
}

type secretRef struct {
	Name string
	Key  string
}

type readinessError struct {
	Code       string
	Message    string
	NextAction string
}

var errKubectlUnavailable = errors.New("kubectl is not available")

func run(ctx context.Context, options planOptions, output io.Writer) error {
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

	config, err := loadPlanConfig()
	if err != nil {
		return err
	}
	ok(output, "backend deploy env names checked without printing values")
	if err := checkSelfDeployReadinessEnv(output); err != nil {
		return err
	}
	if err := checkSelfDeployRuntimeExecutorEnv(output); err != nil {
		return err
	}
	restoreRenderEnv := applySafeRenderEnv()
	defer restoreRenderEnv()

	if err := validateDeployInventory(repoRoot, stack, output); err != nil {
		return err
	}
	if err := renderDeployManifests(ctx, repoRoot, servicesFile, options.RenderDir, stack, !options.SkipLiveKubernetes, options.RequireKubernetes, output); err != nil {
		return err
	}
	checkExternalRefs(config, output)
	if err := checkKubernetesFoundation(ctx, config, options.RequireKubernetes, options.SkipLiveKubernetes, output); err != nil {
		return err
	}
	if err := checkSelfDeployChainAcceptanceTool(repoRoot, output); err != nil {
		return err
	}

	readiness(output, "final-self-deploy-preflight", "ready", "self_deploy_static_readiness_ready", "run onboarding-runner apply, rollout, controlled trigger and self-deploy-chain-acceptance")
	ok(output, "backend deploy dry-run plan passed without cluster changes")
	return nil
}

func loadPlanConfig() (planConfig, error) {
	config := planConfig{
		Namespace:               stackinventory.EnvOr("KODEX_PRODUCTION_NAMESPACE", "kodex-prod"),
		PublicIngressConfigured: os.Getenv("KODEX_PRODUCTION_DOMAIN") != "" || os.Getenv("KODEX_PUBLIC_BASE_URL") != "",
		IDPConfigured:           os.Getenv("KODEX_GITHUB_OAUTH_CLIENT_ID") != "" || os.Getenv("KODEX_GITHUB_OAUTH_CLIENT_SECRET") != "",
	}
	if strings.TrimSpace(config.Namespace) == "" {
		return planConfig{}, errors.New("KODEX_PRODUCTION_NAMESPACE is required")
	}
	registryHost := stackinventory.EnvOr("KODEX_INTERNAL_REGISTRY_HOST", "127.0.0.1:5000")
	if _, _, err := net.SplitHostPort(registryHost); err != nil {
		return planConfig{}, fmt.Errorf("KODEX_INTERNAL_REGISTRY_HOST must be host:port")
	}
	return config, nil
}

func applySafeRenderEnv() func() {
	snapshot := os.Environ()
	clearKodexEnv()
	_ = os.Setenv("KODEX_PRODUCTION_NAMESPACE", "kodex-dry-run")
	_ = os.Setenv("KODEX_INTERNAL_REGISTRY_HOST", "127.0.0.1:5000")
	return func() {
		clearKodexEnv()
		for _, item := range snapshot {
			key, value, ok := strings.Cut(item, "=")
			if !ok || !strings.HasPrefix(key, "KODEX_") {
				continue
			}
			_ = os.Setenv(key, value)
		}
	}
}

func clearKodexEnv() {
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && strings.HasPrefix(key, "KODEX_") {
			_ = os.Unsetenv(key)
		}
	}
}

func validateDeployInventory(repoRoot string, stack stackinventory.Stack, output io.Writer) error {
	if len(stack.Spec.DeployableServices) == 0 {
		return errors.New("services.yaml has no deployableServices")
	}
	if err := resolveImage(stack, "postgres"); err != nil {
		return err
	}
	if err := resolveImage(stack, "platform-event-log-migrations"); err != nil {
		return err
	}
	if err := resolveImage(stack, "kaniko-executor"); err != nil {
		return err
	}
	readiness(output, "kaniko-builder-image", "ready", "kaniko_builder_image_resolved", "none")

	knownServices := make(map[string]struct{}, len(stack.Spec.DeployableServices))
	for _, service := range stack.Spec.DeployableServices {
		if strings.TrimSpace(service.Name) == "" {
			return errors.New("deployable service without name")
		}
		knownServices[service.Name] = struct{}{}
	}
	for _, dataStore := range stack.Spec.PlatformDataStores {
		if strings.TrimSpace(dataStore.Name) != "" {
			knownServices[dataStore.Name] = struct{}{}
		}
	}
	for _, service := range stack.Spec.DeployableServices {
		if err := validateDeployableService(repoRoot, stack, service, knownServices); err != nil {
			return err
		}
		migrations := "нет"
		if service.Deploy.MigrationsManifest != "" {
			migrations = "есть"
		}
		dependencies := "нет"
		if len(service.Dependencies) > 0 {
			dependencies = strings.Join(service.Dependencies, ",")
		}
		_, _ = fmt.Fprintf(output, "PLAN: service %s status=%s zone=%s migrations=%s dependencies=%s\n", service.Name, service.Status, service.Zone, migrations, dependencies)
	}
	ok(output, fmt.Sprintf("deploy inventory validated for %d backend services", len(stack.Spec.DeployableServices)))
	return nil
}

func validateDeployableService(repoRoot string, stack stackinventory.Stack, service stackinventory.DeployableServiceSpec, knownServices map[string]struct{}) error {
	for _, item := range []struct {
		name string
		path string
	}{
		{name: "service path", path: service.Path},
		{name: "Dockerfile", path: service.Dockerfile},
		{name: "kustomization", path: service.Deploy.Kustomization},
		{name: "service manifest", path: service.Deploy.ServiceManifest},
	} {
		if strings.TrimSpace(item.path) == "" {
			return fmt.Errorf("%s %q is missing %s", "deployable service", service.Name, item.name)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, item.path)); err != nil {
			return fmt.Errorf("stat %s for service %s: %w", item.name, service.Name, err)
		}
	}
	if service.Deploy.MigrationsManifest != "" {
		if _, err := os.Stat(filepath.Join(repoRoot, service.Deploy.MigrationsManifest)); err != nil {
			return fmt.Errorf("stat migrations manifest for service %s: %w", service.Name, err)
		}
		if err := resolveImage(stack, service.Name+"-migrations"); err != nil {
			return err
		}
	}
	for _, dependency := range service.Dependencies {
		if _, ok := knownServices[dependency]; !ok {
			return fmt.Errorf("service %s depends on unknown deployable service %s", service.Name, dependency)
		}
	}
	return resolveImage(stack, service.Name)
}

func resolveImage(stack stackinventory.Stack, name string) error {
	if _, err := stack.Image(name); err != nil {
		return fmt.Errorf("resolve image %s: %w", name, err)
	}
	return nil
}

func renderDeployManifests(ctx context.Context, repoRoot, servicesFile, renderDir string, stack stackinventory.Stack, runKustomize bool, requireKustomize bool, output io.Writer) error {
	renderDir, cleanup, err := manifestrender.PrepareOutputRoot(renderDir, "kodex-backend-deploy-plan-*")
	if err != nil {
		return err
	}
	defer cleanup()

	for _, set := range buildManifestSets(repoRoot, renderDir, stack) {
		if err := manifestrender.Render(manifestrender.Options{
			SourcePath:       set.Source,
			OutputPath:       set.Output,
			ServicesFilePath: servicesFile,
		}); err != nil {
			return fmt.Errorf("render %s: %w", set.Name, err)
		}
		ok(output, set.Name+" rendered")
		if set.Kustomize && runKustomize {
			if err := kustomize(ctx, set.Output); err != nil {
				if errors.Is(err, errKubectlUnavailable) && !requireKustomize {
					skip(output, set.Name+" kustomize check deferred because kubectl is unavailable")
					continue
				}
				return fmt.Errorf("kustomize %s: %w", set.Name, err)
			}
			ok(output, set.Name+" kustomize check passed")
		}
	}
	if err := checkRenderedSelfDeploySurface(renderDir, output); err != nil {
		return err
	}
	return nil
}

func buildManifestSets(repoRoot string, renderDir string, stack stackinventory.Stack) []manifestSet {
	sets := []manifestSet{
		{
			Name:      "PostgreSQL foundation",
			Source:    filepath.Join(repoRoot, "deploy/base/postgres"),
			Output:    filepath.Join(renderDir, "postgres"),
			Kustomize: true,
		},
		{
			Name:      "platform event-log migrations",
			Source:    filepath.Join(repoRoot, "deploy/base/platform-event-log/migrations.yaml.tpl"),
			Output:    filepath.Join(renderDir, "platform-event-log", "migrations.yaml"),
			Kustomize: false,
		},
		{
			Name:      "registry foundation",
			Source:    filepath.Join(repoRoot, "deploy/base/bootstrap-foundation"),
			Output:    filepath.Join(renderDir, "bootstrap-foundation"),
			Kustomize: true,
		},
		{
			Name:      "registry and Kaniko smoke",
			Source:    filepath.Join(repoRoot, "deploy/base/bootstrap-builder-smoke"),
			Output:    filepath.Join(renderDir, "bootstrap-builder-smoke"),
			Kustomize: true,
		},
		{
			Name:      "public web foundation",
			Source:    filepath.Join(repoRoot, "deploy/base/web-public-foundation"),
			Output:    filepath.Join(renderDir, "web-public-foundation"),
			Kustomize: true,
		},
		{
			Name:      "public web console contour",
			Source:    filepath.Join(repoRoot, "deploy/base/web-console-public"),
			Output:    filepath.Join(renderDir, "web-console-public"),
			Kustomize: true,
		},
		{
			Name:      "public integration-gateway webhook contour",
			Source:    filepath.Join(repoRoot, "deploy/base/integration-gateway-public"),
			Output:    filepath.Join(renderDir, "integration-gateway-public"),
			Kustomize: true,
		},
	}
	for _, service := range stack.Spec.DeployableServices {
		sets = append(sets, manifestSet{
			Name:      "backend service " + service.Name,
			Source:    filepath.Join(repoRoot, filepath.Dir(service.Deploy.Kustomization)),
			Output:    filepath.Join(renderDir, service.Name),
			Kustomize: true,
		})
	}
	return sets
}

func kustomize(ctx context.Context, path string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return errKubectlUnavailable
	}
	command := exec.CommandContext(ctx, "kubectl", "kustomize", path)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func checkExternalRefs(config planConfig, output io.Writer) {
	if config.PublicIngressConfigured {
		ok(output, "public ingress env refs are configured; values hidden")
	} else {
		skip(output, "public ingress env refs are deferred: KODEX_PRODUCTION_DOMAIN/KODEX_PUBLIC_BASE_URL")
	}
	if config.IDPConfigured {
		ok(output, "IdP/OAuth env refs are configured; values hidden")
	} else {
		skip(output, "IdP/OAuth env refs are deferred: KODEX_GITHUB_OAUTH_CLIENT_ID/KODEX_GITHUB_OAUTH_CLIENT_SECRET")
	}
}

func checkKubernetesFoundation(ctx context.Context, config planConfig, require bool, skipLive bool, output io.Writer) error {
	if skipLive {
		skip(output, "live Kubernetes foundation checks skipped")
		return nil
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		if require {
			return errors.New("kubectl is required for live backend deploy plan")
		}
		skip(output, "kubectl is not available; live Kubernetes foundation checks deferred")
		readiness(output, "runtime-kubernetes-foundation", "skipped", "runtime_kubernetes_foundation_deferred", "rerun with --require-kubernetes on the target cluster")
		return nil
	}
	deferred := false
	checks := []struct {
		Label string
		Args  []string
	}{
		{Label: "kubectl context is available", Args: []string{"config", "current-context"}},
		{Label: "Kubernetes API readiness check passed", Args: []string{"get", "--raw=/readyz"}},
		{Label: "production namespace exists", Args: []string{"get", "namespace", config.Namespace}},
		{Label: "registry deployment exists", Args: []string{"-n", config.Namespace, "get", "deployment", "kodex-registry"}},
		{Label: "registry service exists", Args: []string{"-n", config.Namespace, "get", "service", "kodex-registry"}},
		{Label: "PostgreSQL StatefulSet exists", Args: []string{"-n", config.Namespace, "get", "statefulset", "postgres"}},
		{Label: "PostgreSQL service exists", Args: []string{"-n", config.Namespace, "get", "service", "postgres"}},
		{Label: "platform runtime secret exists", Args: []string{"-n", config.Namespace, "get", "secret", "kodex-platform-runtime"}},
		{Label: "PostgreSQL secret exists", Args: []string{"-n", config.Namespace, "get", "secret", "kodex-postgres"}},
	}
	for _, check := range checks {
		if err := runKubectl(ctx, check.Args...); err != nil {
			if require {
				return fmt.Errorf("%s: %w", check.Label, err)
			}
			deferred = true
			skip(output, check.Label+" deferred")
			continue
		}
		ok(output, check.Label)
	}
	for _, ref := range requiredSelfDeployRuntimeSecretRefs() {
		if err := checkKubernetesSecretKey(ctx, config.Namespace, ref.Name, ref.Key); err != nil {
			if require {
				return err
			}
			deferred = true
			skip(output, "runtime secret ref "+safeSecretRef(ref)+" deferred")
			continue
		}
		ok(output, "runtime secret ref "+safeSecretRef(ref)+" exists")
	}
	serviceAccountsDeferred, err := checkKubernetesRuntimeServiceAccounts(ctx, config.Namespace, require, output)
	if err != nil {
		return err
	}
	deferred = deferred || serviceAccountsDeferred
	if deferred {
		readiness(output, "runtime-kubernetes-foundation", "skipped", "runtime_kubernetes_foundation_deferred", "rerun with --require-kubernetes on the target cluster")
	} else {
		readiness(output, "runtime-kubernetes-foundation", "ready", "runtime_kubernetes_foundation_ready", "none")
	}
	return nil
}

func runKubectl(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "kubectl", args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func checkKubernetesSecretKey(ctx context.Context, namespace string, secretName string, key string) error {
	command := exec.CommandContext(ctx, "kubectl", "-n", namespace, "get", "secret", secretName, "-o", "jsonpath={.data."+key+"}")
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("read Kubernetes secret key %s/%s:%s: %w", namespace, secretName, key, err)
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return fmt.Errorf("Kubernetes secret key %s/%s:%s is missing", namespace, secretName, key)
	}
	return nil
}

func checkKubernetesRuntimeServiceAccounts(ctx context.Context, namespace string, require bool, output io.Writer) (bool, error) {
	deferred := false
	for _, serviceAccount := range requiredRuntimeServiceAccounts() {
		if err := runKubectl(ctx, "-n", namespace, "get", "serviceaccount", serviceAccount); err != nil {
			if require {
				return false, fmt.Errorf("runtime service account %s is required for self-deploy readiness: %w", serviceAccount, err)
			}
			deferred = true
			skip(output, "runtime service account "+serviceAccount+" deferred")
			continue
		}
		ok(output, "runtime service account "+serviceAccount+" exists")
	}
	return deferred, nil
}

func requiredRuntimeServiceAccounts() []string {
	return []string{
		"runtime-manager",
		stackinventory.EnvOr("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEPLOY_SERVICE_ACCOUNT", "runtime-deployer"),
	}
}

func requiredSelfDeployRuntimeSecretKeys() []string {
	result := make([]string, 0, len(requiredSelfDeployRuntimeSecretRefs()))
	for _, ref := range requiredSelfDeployRuntimeSecretRefs() {
		if ref.Name == "kodex-platform-runtime" {
			result = append(result, ref.Key)
		}
	}
	return result
}

func requiredSelfDeployRuntimeSecretRefs() []secretRef {
	return []secretRef{
		{Name: "kodex-platform-runtime", Key: "KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN"},
		{Name: "kodex-platform-runtime", Key: "KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN"},
		{Name: "kodex-platform-runtime", Key: "KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN"},
		{Name: "kodex-platform-runtime", Key: "KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN"},
		{
			Name: stackinventory.EnvOr("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_NAME", "kodex-platform-runtime"),
			Key:  stackinventory.EnvOr("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_KEY", "KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN"),
		},
		{
			Name: stackinventory.EnvOr("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_NAME", "kodex-platform-runtime"),
			Key:  stackinventory.EnvOr("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_KEY", "KODEX_GIT_BOT_TOKEN"),
		},
	}
}

func safeSecretRef(ref secretRef) string {
	return ref.Name + ":" + ref.Key
}

func checkSelfDeployReadinessEnv(output io.Writer) error {
	if err := validateSelfDeployProjectID(os.Getenv("KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID")); err != nil {
		return err
	}
	for _, item := range requiredSelfDeployBooleanEnv() {
		enabled, err := boolEnv(item.Name, item.Fallback)
		if err != nil {
			return readinessError{
				Code:       "self_deploy_flag_invalid",
				Message:    err.Error(),
				NextAction: "set " + item.Name + " to true or false in the protected deploy env",
			}
		}
		if !enabled {
			return readinessError{
				Code:       "self_deploy_flag_disabled",
				Message:    item.Name + " must be true for self-deploy readiness",
				NextAction: "set " + item.Name + "=true before rollout",
			}
		}
	}
	ok(output, "agent-manager self-deploy readiness env checked; values hidden")
	readiness(output, "agent-manager-self-deploy-env", "ready", "agent_manager_self_deploy_env_ready", "none")
	return nil
}

func validateSelfDeployProjectID(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return readinessError{
			Code:       "self_deploy_project_id_missing",
			Message:    "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID is required for self-deploy readiness",
			NextAction: "run onboarding-runner apply for self-repo and write the returned project id to the protected deploy env",
		}
	}
	projectID, err := uuid.Parse(trimmed)
	if err != nil || projectID == uuid.Nil {
		return readinessError{
			Code:       "self_deploy_project_id_invalid",
			Message:    "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID must be a valid non-nil uuid for self-deploy readiness",
			NextAction: "replace the value with the safe project id returned by onboarding-runner",
		}
	}
	return nil
}

func requiredSelfDeployBooleanEnv() []struct {
	Name     string
	Fallback string
} {
	return []struct {
		Name     string
		Fallback string
	}{
		{Name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED", Fallback: "false"},
		{Name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED", Fallback: "false"},
		{Name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED", Fallback: "false"},
		{Name: "KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_ENABLED", Fallback: "false"},
	}
}

func checkSelfDeployRuntimeExecutorEnv(output io.Writer) error {
	enabled, err := boolEnv("KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED", "false")
	if err != nil {
		return readinessError{
			Code:       "runtime_executor_flag_invalid",
			Message:    err.Error(),
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED to true or false in the protected deploy env",
		}
	}
	if !enabled {
		return readinessError{
			Code:       "runtime_executor_disabled",
			Message:    "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED must be true for self-deploy readiness",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED=true before rollout",
		}
	}
	for _, item := range requiredRuntimeExecutorStringEnv() {
		value := strings.TrimSpace(stackinventory.EnvOr(item.Name, item.Fallback))
		if value == "" {
			return readinessError{
				Code:       item.Code,
				Message:    item.Name + " is required for self-deploy readiness",
				NextAction: item.NextAction,
			}
		}
	}
	ok(output, "runtime-manager Kubernetes executor readiness env checked; values hidden")
	readiness(output, "runtime-kubernetes-executor-env", "ready", "runtime_kubernetes_executor_env_ready", "none")
	return nil
}

func requiredRuntimeExecutorStringEnv() []struct {
	Name       string
	Fallback   string
	Code       string
	NextAction string
} {
	return []struct {
		Name       string
		Fallback   string
		Code       string
		NextAction string
	}{
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE",
			Fallback:   stackinventory.EnvOr("KODEX_PRODUCTION_NAMESPACE", ""),
			Code:       "runtime_executor_namespace_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE or KODEX_PRODUCTION_NAMESPACE",
		},
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_IMAGE",
			Code:       "runtime_executor_default_image_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_IMAGE to the approved runtime worker image ref",
		},
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEPLOY_SERVICE_ACCOUNT",
			Fallback:   "runtime-deployer",
			Code:       "runtime_executor_deploy_service_account_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEPLOY_SERVICE_ACCOUNT or keep runtime-deployer",
		},
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_ADDR",
			Fallback:   "agent-manager:9090",
			Code:       "runtime_executor_agent_manager_addr_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_ADDR",
		},
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_NAME",
			Fallback:   "kodex-platform-runtime",
			Code:       "runtime_executor_agent_manager_secret_name_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_NAME",
		},
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_KEY",
			Fallback:   "KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN",
			Code:       "runtime_executor_agent_manager_secret_key_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_KEY",
		},
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_NAME",
			Fallback:   "kodex-platform-runtime",
			Code:       "runtime_executor_source_auth_secret_name_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_NAME",
		},
		{
			Name:       "KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_KEY",
			Fallback:   "KODEX_GIT_BOT_TOKEN",
			Code:       "runtime_executor_source_auth_secret_key_missing",
			NextAction: "set KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_KEY",
		},
	}
}

func boolEnv(name string, fallback string) (bool, error) {
	raw := strings.TrimSpace(stackinventory.EnvOr(name, fallback))
	switch strings.ToLower(raw) {
	case "true", "1", "yes", "y", "on":
		return true, nil
	case "false", "0", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", name)
	}
}

func checkRenderedSelfDeploySurface(renderDir string, output io.Writer) error {
	if err := checkRenderedManifestFragments(filepath.Join(renderDir, "agent-manager", "agent-manager.yaml"), "agent-manager", requiredAgentManagerSelfDeployRenderedManifestFragments()); err != nil {
		return err
	}
	ok(output, "agent-manager self-deploy rendered manifest surface checked")
	if err := checkRenderedManifestFragments(filepath.Join(renderDir, "runtime-manager", "runtime-manager.yaml"), "runtime-manager", requiredRuntimeManagerSelfDeployRenderedManifestFragments()); err != nil {
		return err
	}
	ok(output, "runtime-manager self-deploy rendered manifest surface checked")
	readiness(output, "self-deploy-rendered-manifests", "ready", "self_deploy_rendered_surface_ready", "none")
	return nil
}

func checkRenderedManifestFragments(manifestPath string, service string, fragments []string) error {
	contentBytes, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return readinessError{
			Code:       "rendered_self_deploy_manifest_missing",
			Message:    "rendered " + service + " manifest is required for self-deploy readiness",
			NextAction: "include " + service + " in services.yaml deploy inventory before final rollout",
		}
	}
	if err != nil {
		return fmt.Errorf("read rendered %s manifest: %w", service, err)
	}
	content := string(contentBytes)
	for _, fragment := range fragments {
		if !strings.Contains(content, fragment) {
			return readinessError{
				Code:       "rendered_self_deploy_surface_missing",
				Message:    "rendered " + service + " manifest is missing self-deploy readiness surface " + fragment,
				NextAction: "add the missing env/SecretRef to deploy/base/" + service,
			}
		}
	}
	return nil
}

func requiredAgentManagerSelfDeployRenderedManifestFragments() []string {
	return []string{
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_CONSUMER_ENABLED",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_GOVERNANCE_GATE_ENABLED",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_BUILD_DISPATCH_ENABLED",
		"KODEX_AGENT_MANAGER_SELF_DEPLOY_GATE_DECISION_CONSUMER_ENABLED",
		"KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN",
	}
}

func requiredRuntimeManagerSelfDeployRenderedManifestFragments() []string {
	return []string{
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_ENABLED",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_WORKER_ID",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_NAMESPACE",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEFAULT_IMAGE",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_DEPLOY_SERVICE_ACCOUNT",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_NAME",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_AGENT_MANAGER_GRPC_AUTH_SECRET_KEY",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_NAME",
		"KODEX_RUNTIME_MANAGER_KUBERNETES_EXECUTOR_SOURCE_AUTH_SECRET_KEY",
	}
}

func checkSelfDeployChainAcceptanceTool(repoRoot string, output io.Writer) error {
	path := filepath.Join(repoRoot, "cmd", "self-deploy-chain-acceptance", "main.go")
	if _, err := os.Stat(path); err != nil {
		return readinessError{
			Code:       "self_deploy_chain_acceptance_missing",
			Message:    "cmd/self-deploy-chain-acceptance is required for final self-deploy readiness",
			NextAction: "restore the read-only self-deploy chain acceptance command before final rollout",
		}
	}
	ok(output, "self-deploy chain acceptance command is available")
	readiness(output, "self-deploy-chain-acceptance", "ready", "self_deploy_chain_acceptance_available", "run it after rollout and controlled trigger")
	return nil
}

func (err readinessError) Error() string {
	if err.NextAction == "" {
		return err.Code + ": " + err.Message
	}
	return err.Code + ": " + err.Message + "; next_action=" + err.NextAction
}

func firstNonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func absolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path for %s: %w", path, err)
	}
	return abs, nil
}

func ok(output io.Writer, message string) {
	_, _ = fmt.Fprintf(output, "OK: %s\n", message)
}

func skip(output io.Writer, message string) {
	_, _ = fmt.Fprintf(output, "SKIP: %s\n", message)
}

func readiness(output io.Writer, component string, status string, code string, nextAction string) {
	_, _ = fmt.Fprintf(output, "READINESS: component=%s status=%s code=%s next_action=%s\n", component, status, code, nextAction)
}
