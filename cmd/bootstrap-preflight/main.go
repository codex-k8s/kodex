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
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/manifestrender"
	"github.com/codex-k8s/kodex/libs/go/stackinventory"
)

func main() {
	repoRoot := flag.String("repo-root", ".", "repository root containing services.yaml and deploy/base")
	envFile := flag.String("env-file", "", "bootstrap env file; values are loaded but never printed")
	servicesFile := flag.String("services-file", "", "root services.yaml path; defaults to <repo-root>/services.yaml")
	renderDir := flag.String("render-dir", "", "optional output directory for rendered dry-run manifests")
	requireKubernetes := flag.Bool("require-kubernetes", false, "fail if kubectl, Kubernetes API, namespace or registry resources are unavailable")
	skipLiveKubernetes := flag.Bool("skip-live-kubernetes", false, "skip live kubectl checks and kustomize validation")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, preflightOptions{
		RepoRoot:           *repoRoot,
		EnvFilePath:        *envFile,
		ServicesFilePath:   *servicesFile,
		RenderDir:          *renderDir,
		RequireKubernetes:  *requireKubernetes,
		SkipLiveKubernetes: *skipLiveKubernetes,
	}, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap-preflight: %v\n", err)
		os.Exit(1)
	}
}

type preflightOptions struct {
	RepoRoot           string
	EnvFilePath        string
	ServicesFilePath   string
	RenderDir          string
	RequireKubernetes  bool
	SkipLiveKubernetes bool
}

type preflightConfig struct {
	Namespace            string
	OperatorUserProvided bool
}

func run(ctx context.Context, options preflightOptions, output io.Writer) error {
	repoRoot, err := absPath(defaultString(options.RepoRoot, "."))
	if err != nil {
		return err
	}
	envFile := strings.TrimSpace(options.EnvFilePath)
	if envFile != "" {
		envFile, err = absPath(envFile)
		if err != nil {
			return err
		}
		if err := stackinventory.LoadEnvFile(envFile); err != nil {
			return err
		}
		logOK(output, "env file loaded; values hidden")
	}

	servicesFile := options.ServicesFilePath
	if strings.TrimSpace(servicesFile) == "" {
		servicesFile = filepath.Join(repoRoot, "services.yaml")
	}
	stack, err := stackinventory.LoadFile(servicesFile)
	if err != nil {
		return err
	}
	logOK(output, "root services.yaml loaded through stackinventory")

	config, err := loadPreflightConfig()
	if err != nil {
		return err
	}
	logOK(output, "required env names checked without printing values")
	if !config.OperatorUserProvided {
		return errors.New("OPERATOR_USER is required")
	}

	if err := validateStackImages(stack); err != nil {
		return err
	}
	logOK(output, "registry, Kaniko and smoke image refs resolved through stackinventory")

	if err := renderBootstrapManifests(ctx, repoRoot, servicesFile, envFile, options.RenderDir, options.RequireKubernetes, options.SkipLiveKubernetes, output); err != nil {
		return err
	}

	if err := checkKubernetes(ctx, config, options.RequireKubernetes, options.SkipLiveKubernetes, output); err != nil {
		return err
	}

	logOK(output, "bootstrap deploy preflight passed")
	return nil
}

func loadPreflightConfig() (preflightConfig, error) {
	registryPort, err := envInt("KODEX_INTERNAL_REGISTRY_PORT", 5000)
	if err != nil {
		return preflightConfig{}, err
	}
	config := preflightConfig{
		Namespace:            envOr("KODEX_PRODUCTION_NAMESPACE", "kodex-prod"),
		OperatorUserProvided: strings.TrimSpace(os.Getenv("OPERATOR_USER")) != "",
	}
	registryHost := envOr("KODEX_INTERNAL_REGISTRY_HOST", "127.0.0.1:"+strconv.Itoa(registryPort))
	if strings.TrimSpace(config.Namespace) == "" {
		return preflightConfig{}, errors.New("KODEX_PRODUCTION_NAMESPACE is required")
	}
	if strings.TrimSpace(registryHost) == "" {
		return preflightConfig{}, errors.New("KODEX_INTERNAL_REGISTRY_HOST is required")
	}
	if _, _, err := net.SplitHostPort(registryHost); err != nil {
		return preflightConfig{}, fmt.Errorf("KODEX_INTERNAL_REGISTRY_HOST must be host:port")
	}
	if _, err = envDuration("KODEX_ROLLOUT_TIMEOUT", 300*time.Second); err != nil {
		return preflightConfig{}, err
	}
	if _, err = envDuration("KODEX_KANIKO_TIMEOUT", 1800*time.Second); err != nil {
		return preflightConfig{}, err
	}
	if _, err = envBool("KODEX_BOOTSTRAP_SKIP_DNS_CHECK", false); err != nil {
		return preflightConfig{}, err
	}
	if _, err = envBool("KODEX_FIREWALL_ENABLED", true); err != nil {
		return preflightConfig{}, err
	}
	if _, err = envBool("KODEX_INGRESS_HOST_NETWORK", true); err != nil {
		return preflightConfig{}, err
	}
	return config, nil
}

func validateStackImages(stack stackinventory.Stack) error {
	for _, image := range []struct {
		name    string
		envName string
	}{
		{name: "registry", envName: "KODEX_REGISTRY_IMAGE"},
		{name: "kaniko-executor", envName: "KODEX_KANIKO_EXECUTOR_IMAGE"},
		{name: "crane", envName: "KODEX_IMAGE_MIRROR_TOOL_IMAGE"},
		{name: "busybox", envName: "KODEX_REGISTRY_SMOKE_SOURCE_IMAGE"},
	} {
		if _, err := stack.ImageOr(image.name, image.envName); err != nil {
			return fmt.Errorf("resolve stack image %s: %w", image.name, err)
		}
	}
	return nil
}

func renderBootstrapManifests(ctx context.Context, repoRoot, servicesFile, envFile, renderDir string, requireKustomize bool, skipKustomize bool, output io.Writer) error {
	tempDir := ""
	if strings.TrimSpace(renderDir) == "" {
		var err error
		tempDir, err = os.MkdirTemp("", "kodex-bootstrap-preflight-*")
		if err != nil {
			return fmt.Errorf("create preflight render dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tempDir) }()
		renderDir = tempDir
	} else if err := os.RemoveAll(renderDir); err != nil {
		return fmt.Errorf("clean render dir: %w", err)
	}

	renderSets := []struct {
		name   string
		source string
		output string
	}{
		{
			name:   "bootstrap foundation",
			source: filepath.Join(repoRoot, "deploy/base/bootstrap-foundation"),
			output: filepath.Join(renderDir, "bootstrap-foundation"),
		},
		{
			name:   "bootstrap builder smoke",
			source: filepath.Join(repoRoot, "deploy/base/bootstrap-builder-smoke"),
			output: filepath.Join(renderDir, "bootstrap-builder-smoke"),
		},
	}
	for _, renderSet := range renderSets {
		if err := manifestrender.Render(manifestrender.Options{
			SourcePath:       renderSet.source,
			OutputPath:       renderSet.output,
			EnvFilePath:      envFile,
			ServicesFilePath: servicesFile,
		}); err != nil {
			return fmt.Errorf("render %s: %w", renderSet.name, err)
		}
		logOK(output, renderSet.name+" rendered")
		if !skipKustomize {
			if _, err := exec.LookPath("kubectl"); err != nil {
				if requireKustomize {
					return errors.New("kubectl is required for kustomize render validation")
				}
				logSkip(output, renderSet.name+" kustomize check deferred because kubectl is unavailable")
				continue
			}
			if err := runQuiet(ctx, "kubectl", "kustomize", renderSet.output); err != nil {
				return fmt.Errorf("kustomize %s: %w", renderSet.name, err)
			}
			logOK(output, renderSet.name+" kustomize check passed")
		}
	}
	return nil
}

func checkKubernetes(ctx context.Context, config preflightConfig, require bool, skip bool, output io.Writer) error {
	if skip {
		logSkip(output, "live Kubernetes checks skipped")
		return nil
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		if require {
			return errors.New("kubectl is required for live Kubernetes preflight")
		}
		logSkip(output, "kubectl is not available; live Kubernetes checks deferred")
		return nil
	}
	if err := runQuiet(ctx, "kubectl", "config", "current-context"); err != nil {
		return optionalLiveError(require, output, "kubectl context is not available")
	}
	logOK(output, "kubectl context is available")
	if err := runQuiet(ctx, "kubectl", "get", "--raw=/readyz"); err != nil {
		return optionalLiveError(require, output, "Kubernetes API readiness check failed")
	}
	logOK(output, "Kubernetes API readiness check passed")
	if err := runQuiet(ctx, "kubectl", "get", "namespace", config.Namespace); err != nil {
		return optionalLiveError(require, output, "production namespace is not present yet")
	}
	logOK(output, "production namespace exists")
	if err := runQuiet(ctx, "kubectl", "-n", config.Namespace, "get", "deployment", "kodex-registry"); err != nil {
		return optionalLiveError(require, output, "registry deployment is not present yet")
	}
	if err := runQuiet(ctx, "kubectl", "-n", config.Namespace, "get", "service", "kodex-registry"); err != nil {
		return optionalLiveError(require, output, "registry service is not present yet")
	}
	logOK(output, "registry Kubernetes resources are present")
	return nil
}

func optionalLiveError(require bool, output io.Writer, message string) error {
	if require {
		return errors.New(message)
	}
	logSkip(output, message)
	return nil
}

func runQuiet(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func envOr(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return parsed, nil
}

func envBool(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", name)
	}
	return parsed, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a Go duration", name)
	}
	return parsed, nil
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func absPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path for %s: %w", path, err)
	}
	return abs, nil
}

func logOK(output io.Writer, message string) {
	_, _ = fmt.Fprintf(output, "OK: %s\n", message)
}

func logSkip(output io.Writer, message string) {
	_, _ = fmt.Fprintf(output, "SKIP: %s\n", message)
}
