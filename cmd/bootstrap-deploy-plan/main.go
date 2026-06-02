package main

import (
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
		return nil
	}
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
			skip(output, check.Label+" deferred")
			continue
		}
		ok(output, check.Label)
	}
	return nil
}

func runKubectl(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "kubectl", args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
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
