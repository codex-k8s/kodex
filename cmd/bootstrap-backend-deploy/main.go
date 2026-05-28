package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	ring := flag.String("ring", "first", "backend deploy ring: first, second, or all")
	skipBuild := flag.Bool("skip-build", false, "skip Kaniko image builds and only apply manifests")
	skipHealth := flag.Bool("skip-health", false, "skip HTTP readiness checks after rollout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()
	if err := run(ctx, deployOptions{
		RepoRoot:         *repoRoot,
		EnvFilePath:      *envFile,
		ServicesFilePath: *servicesFile,
		Ring:             *ring,
		SkipBuild:        *skipBuild,
		SkipHealth:       *skipHealth,
	}, os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "bootstrap-backend-deploy: %v\n", err)
		os.Exit(1)
	}
}

type deployOptions struct {
	RepoRoot         string
	EnvFilePath      string
	ServicesFilePath string
	Ring             string
	SkipBuild        bool
	SkipHealth       bool
}

type deployConfig struct {
	Namespace      string
	RolloutTimeout string
}

type serviceDeploy struct {
	Name         string
	Dir          string
	MigrationJob string
	ReadyPort    string
}

type backendRing struct {
	Name       string
	Label      string
	Services   []serviceDeploy
	ImageNames []string
}

type imageBuild struct {
	Name        string
	ImageName   string
	Dockerfile  string
	Target      string
	Destination string
}

type secretValues struct {
	Postgres  map[string]string
	Runtime   map[string]string
	RenderEnv map[string]string
	Generated []string
}

type secretSnapshot map[string]map[string]string

var firstRingServices = []serviceDeploy{
	{Name: "access-manager", Dir: "access-manager", MigrationJob: "access-manager-migrations", ReadyPort: "8080"},
	{Name: "project-catalog", Dir: "project-catalog", MigrationJob: "project-catalog-migrations", ReadyPort: "8080"},
	{Name: "package-hub", Dir: "package-hub", MigrationJob: "package-hub-migrations", ReadyPort: "8080"},
	{Name: "provider-hub", Dir: "provider-hub", MigrationJob: "provider-hub-migrations", ReadyPort: "8080"},
}

var secondRingServices = []serviceDeploy{
	{Name: "fleet-manager", Dir: "fleet-manager", MigrationJob: "fleet-manager-migrations", ReadyPort: "8080"},
	{Name: "runtime-manager", Dir: "runtime-manager", MigrationJob: "runtime-manager-migrations", ReadyPort: "8080"},
	{Name: "interaction-hub", Dir: "interaction-hub", MigrationJob: "interaction-hub-migrations", ReadyPort: "8080"},
	{Name: "governance-manager", Dir: "governance-manager", MigrationJob: "governance-manager-migrations", ReadyPort: "8080"},
	{Name: "agent-manager", Dir: "agent-manager", MigrationJob: "agent-manager-migrations", ReadyPort: "8080"},
	{Name: "integration-gateway", Dir: "integration-gateway", ReadyPort: "8080"},
	{Name: "codex-hook-ingress", Dir: "codex-hook-ingress", ReadyPort: "8080"},
}

var firstRingImageNames = []string{
	"platform-event-log-migrations",
	"access-manager",
	"access-manager-migrations",
	"project-catalog",
	"project-catalog-migrations",
	"package-hub",
	"package-hub-migrations",
	"provider-hub",
	"provider-hub-migrations",
}

var secondRingImageNames = []string{
	"platform-event-log-migrations",
	"fleet-manager",
	"fleet-manager-migrations",
	"runtime-manager",
	"runtime-manager-migrations",
	"interaction-hub",
	"interaction-hub-migrations",
	"governance-manager",
	"governance-manager-migrations",
	"agent-manager",
	"agent-manager-migrations",
	"integration-gateway",
	"codex-hook-ingress",
}

var (
	firstRing = backendRing{
		Name:       "first",
		Label:      "first backend ring",
		Services:   firstRingServices,
		ImageNames: firstRingImageNames,
	}
	secondRing = backendRing{
		Name:       "second",
		Label:      "second backend ring",
		Services:   secondRingServices,
		ImageNames: secondRingImageNames,
	}
)

func run(ctx context.Context, options deployOptions, output io.Writer) error {
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

	rings, err := selectBackendRings(options.Ring)
	if err != nil {
		return err
	}
	ok(output, "selected backend deploy rings: "+strings.Join(ringNames(rings), ", "))

	config, err := loadDeployConfig()
	if err != nil {
		return err
	}
	if err := requireCommands("go", "kubectl"); err != nil {
		return err
	}
	ok(output, "required commands are present")

	if err := ensureNamespace(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "namespace exists")

	renderEnv, cleanupRenderEnv, err := prepareRuntime(ctx, config, output)
	if err != nil {
		return err
	}
	defer cleanupRenderEnv()

	renderDir, cleanupRenderDir, err := renderBackendRings(repoRoot, servicesFile, renderEnv, rings)
	if err != nil {
		return err
	}
	defer cleanupRenderDir()
	ok(output, "backend ring manifests rendered in a private temp dir")

	if err := applyRegistry(ctx, config, renderDir, output); err != nil {
		return err
	}
	if !options.SkipBuild {
		if err := buildImages(ctx, config, repoRoot, stack, rings, output); err != nil {
			return err
		}
	} else {
		skip(output, "Kaniko image builds skipped by operator flag")
	}
	if err := applyPostgres(ctx, config, renderDir, output); err != nil {
		return err
	}
	if err := applyMigrations(ctx, config, renderDir, rings, output); err != nil {
		return err
	}
	if err := applyServices(ctx, config, renderDir, rings, !options.SkipHealth, output); err != nil {
		return err
	}
	ok(output, "backend ring deploy finished")
	return nil
}

func selectBackendRings(value string) ([]backendRing, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "first", "ring-1", "1":
		return []backendRing{firstRing}, nil
	case "second", "ring-2", "2":
		return []backendRing{secondRing}, nil
	case "all":
		return []backendRing{firstRing, secondRing}, nil
	default:
		return nil, fmt.Errorf("unsupported backend deploy ring %q; expected first, second, or all", value)
	}
}

func ringNames(rings []backendRing) []string {
	names := make([]string, 0, len(rings))
	for _, ring := range rings {
		names = append(names, ring.Name)
	}
	return names
}

func loadDeployConfig() (deployConfig, error) {
	registryPort := stackinventory.EnvOr("KODEX_INTERNAL_REGISTRY_PORT", "5000")
	registryHost := stackinventory.EnvOr("KODEX_INTERNAL_REGISTRY_HOST", "127.0.0.1:"+registryPort)
	if _, _, err := net.SplitHostPort(registryHost); err != nil {
		return deployConfig{}, fmt.Errorf("KODEX_INTERNAL_REGISTRY_HOST must be host:port")
	}
	if os.Getenv("KODEX_INTERNAL_REGISTRY_HOST") == "" {
		_ = os.Setenv("KODEX_INTERNAL_REGISTRY_HOST", registryHost)
	}
	config := deployConfig{
		Namespace:      stackinventory.EnvOr("KODEX_PRODUCTION_NAMESPACE", "kodex-prod"),
		RolloutTimeout: stackinventory.EnvOr("KODEX_ROLLOUT_TIMEOUT", "1800s"),
	}
	if strings.TrimSpace(config.Namespace) == "" {
		return deployConfig{}, errors.New("KODEX_PRODUCTION_NAMESPACE is required")
	}
	if _, err := time.ParseDuration(config.RolloutTimeout); err != nil {
		return deployConfig{}, fmt.Errorf("KODEX_ROLLOUT_TIMEOUT must be a duration: %w", err)
	}
	return config, nil
}

func prepareRuntime(ctx context.Context, config deployConfig, output io.Writer) (string, func(), error) {
	existing, err := loadExistingSecrets(ctx, config.Namespace)
	if err != nil {
		return "", func() {}, err
	}
	values, err := resolveSecretValues(existing, os.LookupEnv)
	if err != nil {
		return "", func() {}, err
	}
	if err := applyRuntimeSecrets(ctx, config.Namespace, values); err != nil {
		return "", func() {}, err
	}
	if len(values.Generated) > 0 {
		ok(output, fmt.Sprintf("generated or normalized Kubernetes secret keys: %d", len(values.Generated)))
	} else {
		ok(output, "Kubernetes secret keys already present")
	}
	renderEnv, cleanup, err := writeRenderEnv(values.RenderEnv)
	if err != nil {
		return "", func() {}, err
	}
	ok(output, "Kubernetes runtime secrets prepared without printing values")
	return renderEnv, cleanup, nil
}

func loadExistingSecrets(ctx context.Context, namespace string) (secretSnapshot, error) {
	result := secretSnapshot{}
	for _, name := range []string{"kodex-postgres", "kodex-platform-runtime"} {
		values, err := readSecret(ctx, namespace, name)
		if err != nil {
			return nil, err
		}
		result[name] = values
	}
	return result, nil
}

func readSecret(ctx context.Context, namespace, name string) (map[string]string, error) {
	return readSecretWithKubectl(ctx, namespace, name, kubectlOutput)
}

func readSecretWithKubectl(ctx context.Context, namespace, name string, run func(context.Context, ...string) ([]byte, error)) (map[string]string, error) {
	output, err := run(ctx, "-n", namespace, "get", "secret", name, "--ignore-not-found", "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("read Kubernetes secret %s: %w", name, err)
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return map[string]string{}, nil
	}
	var secret struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(output, &secret); err != nil {
		return nil, fmt.Errorf("decode Kubernetes secret %s: %w", name, err)
	}
	values := make(map[string]string, len(secret.Data))
	for key, encoded := range secret.Data {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode Kubernetes secret key %s: %w", key, err)
		}
		values[key] = string(decoded)
	}
	return values, nil
}

func resolveSecretValues(existing secretSnapshot, lookup func(string) (string, bool)) (secretValues, error) {
	generated := []string{}
	value := func(secretName, key string, fallback func() (string, error)) (string, error) {
		if secret := existing[secretName]; secret != nil {
			if current := secret[key]; current != "" {
				return current, nil
			}
		}
		if envValue, ok := lookup(key); ok && envValue != "" {
			return envValue, nil
		}
		next, err := fallback()
		if err != nil {
			return "", err
		}
		generated = append(generated, key)
		return next, nil
	}
	dsnValue := func(key, fallback string, password string) string {
		if secret := existing["kodex-platform-runtime"]; secret != nil {
			if current := secret[key]; dsnUsesPassword(current, password) {
				return current
			}
		}
		if envValue, ok := lookup(key); ok && dsnUsesPassword(envValue, password) {
			return envValue
		}
		generated = append(generated, key)
		return fallback
	}
	literal := func(value string) func() (string, error) {
		return func() (string, error) { return value, nil }
	}
	random := func() (string, error) { return randomHex(32) }

	postgres := map[string]string{}
	var err error
	postgres["KODEX_POSTGRES_DB"], err = value("kodex-postgres", "KODEX_POSTGRES_DB", literal("kodex"))
	if err != nil {
		return secretValues{}, err
	}
	postgres["KODEX_POSTGRES_USER"], err = value("kodex-postgres", "KODEX_POSTGRES_USER", literal("kodex"))
	if err != nil {
		return secretValues{}, err
	}
	postgres["KODEX_POSTGRES_PASSWORD"], err = value("kodex-postgres", "KODEX_POSTGRES_PASSWORD", random)
	if err != nil {
		return secretValues{}, err
	}

	runtime := map[string]string{}
	for _, key := range independentRuntimeSecretKeys() {
		runtime[key], err = value("kodex-platform-runtime", key, random)
		if err != nil {
			return secretValues{}, err
		}
	}
	for _, derived := range derivedRuntimeSecretKeys() {
		source := runtime[derived.SourceKey]
		runtime[derived.Key], err = value("kodex-platform-runtime", derived.Key, literal(source))
		if err != nil {
			return secretValues{}, err
		}
	}

	dbNames := databaseNameDefaults()
	for key, database := range dbNames {
		dsnKey := strings.TrimSuffix(key, "_DATABASE_NAME") + "_DATABASE_DSN"
		if key == "KODEX_PLATFORM_EVENT_LOG_DATABASE_NAME" {
			dsnKey = "KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN"
		}
		databaseName := envOrLookup(lookup, key, database)
		dsnDefault := postgresURI(postgres["KODEX_POSTGRES_USER"], postgres["KODEX_POSTGRES_PASSWORD"], "postgres", "5432", databaseName)
		runtime[dsnKey] = dsnValue(dsnKey, dsnDefault, postgres["KODEX_POSTGRES_PASSWORD"])
	}
	for _, key := range eventLogDSNKeys() {
		runtime[key] = dsnValue(key, runtime["KODEX_PLATFORM_EVENT_LOG_DATABASE_DSN"], postgres["KODEX_POSTGRES_PASSWORD"])
	}

	renderEnv := collectCurrentRenderEnv()
	for key, value := range postgres {
		renderEnv[key] = value
	}
	for key, value := range runtime {
		renderEnv[key] = value
	}
	for key, value := range dbNames {
		renderEnv[key] = envOrLookup(lookup, key, value)
	}
	renderEnv["KODEX_PROJECT_DB_ADMIN_HOST"] = envOrLookup(lookup, "KODEX_PROJECT_DB_ADMIN_HOST", "postgres")
	renderEnv["KODEX_PROJECT_DB_ADMIN_PORT"] = envOrLookup(lookup, "KODEX_PROJECT_DB_ADMIN_PORT", "5432")
	renderEnv["KODEX_PROJECT_DB_ADMIN_USER"] = envOrLookup(lookup, "KODEX_PROJECT_DB_ADMIN_USER", postgres["KODEX_POSTGRES_USER"])
	renderEnv["KODEX_PROJECT_DB_ADMIN_PASSWORD"] = envOrLookup(lookup, "KODEX_PROJECT_DB_ADMIN_PASSWORD", postgres["KODEX_POSTGRES_PASSWORD"])
	renderEnv["KODEX_PROJECT_DB_ADMIN_SSLMODE"] = envOrLookup(lookup, "KODEX_PROJECT_DB_ADMIN_SSLMODE", "disable")
	renderEnv["KODEX_PROJECT_DB_ADMIN_DATABASE"] = envOrLookup(lookup, "KODEX_PROJECT_DB_ADMIN_DATABASE", "postgres")

	sort.Strings(generated)
	return secretValues{Postgres: postgres, Runtime: runtime, RenderEnv: renderEnv, Generated: generated}, nil
}

func independentRuntimeSecretKeys() []string {
	return []string{
		"KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN",
		"KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN",
		"KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN",
		"KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN",
		"KODEX_GITHUB_WEBHOOK_SECRET",
		"KODEX_EXTERNAL_CALLBACK_SECRET",
		"KODEX_FLEET_MANAGER_GRPC_AUTH_TOKEN",
		"KODEX_FLEET_MANAGER_SECRET_RESOLVER_VAULT_TOKEN",
		"KODEX_PACKAGE_HUB_VAULT_TOKEN",
		"KODEX_PROVIDER_HUB_VAULT_TOKEN",
		"KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_TOKEN",
		"KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN",
	}
}

type derivedSecretKey struct {
	Key       string
	SourceKey string
}

func derivedRuntimeSecretKeys() []derivedSecretKey {
	return []derivedSecretKey{
		{Key: "KODEX_FLEET_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_PACKAGE_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_PROVIDER_HUB_ACCESS_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_GOVERNANCE_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_RUNTIME_MANAGER_ACCESS_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_ACCESS_MANAGER_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_RUNTIME_MANAGER_FLEET_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_FLEET_MANAGER_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_AGENT_MANAGER_PACKAGE_HUB_GRPC_AUTH_TOKEN", SourceKey: "KODEX_PACKAGE_HUB_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_AGENT_MANAGER_PROJECT_CATALOG_GRPC_AUTH_TOKEN", SourceKey: "KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_AGENT_MANAGER_RUNTIME_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN"},
		{Key: "KODEX_AGENT_MANAGER_PROVIDER_HUB_GRPC_AUTH_TOKEN", SourceKey: "KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN"},
	}
}

func databaseNameDefaults() map[string]string {
	return map[string]string{
		"KODEX_ACCESS_MANAGER_DATABASE_NAME":     "kodex_access_manager",
		"KODEX_PROJECT_CATALOG_DATABASE_NAME":    "kodex_project_catalog",
		"KODEX_PACKAGE_HUB_DATABASE_NAME":        "kodex_package_hub",
		"KODEX_PROVIDER_HUB_DATABASE_NAME":       "kodex_provider_hub",
		"KODEX_GOVERNANCE_MANAGER_DATABASE_NAME": "kodex_governance_manager",
		"KODEX_INTERACTION_HUB_DATABASE_NAME":    "kodex_interaction_hub",
		"KODEX_FLEET_MANAGER_DATABASE_NAME":      "kodex_fleet_manager",
		"KODEX_RUNTIME_MANAGER_DATABASE_NAME":    "kodex_runtime_manager",
		"KODEX_AGENT_MANAGER_DATABASE_NAME":      "kodex_agent_manager",
		"KODEX_PLATFORM_EVENT_LOG_DATABASE_NAME": "kodex_platform_event_log",
	}
}

func eventLogDSNKeys() []string {
	return []string{
		"KODEX_ACCESS_MANAGER_EVENT_LOG_DATABASE_DSN",
		"KODEX_PACKAGE_HUB_EVENT_LOG_DATABASE_DSN",
		"KODEX_PROVIDER_HUB_EVENT_LOG_DATABASE_DSN",
		"KODEX_GOVERNANCE_MANAGER_EVENT_LOG_DATABASE_DSN",
		"KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_DSN",
		"KODEX_FLEET_MANAGER_EVENT_LOG_DATABASE_DSN",
		"KODEX_RUNTIME_MANAGER_EVENT_LOG_DATABASE_DSN",
		"KODEX_AGENT_MANAGER_EVENT_LOG_DATABASE_DSN",
	}
}

func dsnUsesPassword(dsn string, password string) bool {
	return dsn != "" && password != "" && strings.Contains(dsn, ":"+password+"@")
}

func collectCurrentRenderEnv() map[string]string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(key, "KODEX_") || key == "OPERATOR_USER" {
			values[key] = value
		}
	}
	return values
}

func applyRuntimeSecrets(ctx context.Context, namespace string, values secretValues) error {
	for name, data := range map[string]map[string]string{
		"kodex-postgres":         values.Postgres,
		"kodex-platform-runtime": values.Runtime,
	} {
		if err := kubectlApply(ctx, secretManifest(namespace, name, data)); err != nil {
			return fmt.Errorf("apply Kubernetes secret %s: %w", name, err)
		}
	}
	return nil
}

func secretManifest(namespace, name string, values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n")
	out.WriteString("  name: " + name + "\n")
	out.WriteString("  namespace: " + namespace + "\n")
	out.WriteString("  labels:\n    app.kubernetes.io/part-of: kodex\n")
	out.WriteString("type: Opaque\nstringData:\n")
	for _, key := range keys {
		out.WriteString("  " + key + ": " + strconv.Quote(values[key]) + "\n")
	}
	return out.String()
}

func writeRenderEnv(values map[string]string) (string, func(), error) {
	file, err := os.CreateTemp("", "kodex-backend-render-env-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create render env: %w", err)
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", func() {}, fmt.Errorf("secure render env: %w", err)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(file, "%s=%s\n", key, shellQuote(values[key])); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return "", func() {}, fmt.Errorf("write render env: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", func() {}, fmt.Errorf("close render env: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func renderBackendRings(repoRoot, servicesFile, renderEnv string, rings []backendRing) (string, func(), error) {
	renderDir, err := os.MkdirTemp("", "kodex-backend-ring-render-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create render dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(renderDir) }
	sets := []struct {
		source string
		output string
	}{
		{source: filepath.Join(repoRoot, "deploy/base/bootstrap-foundation"), output: filepath.Join(renderDir, "bootstrap-foundation")},
		{source: filepath.Join(repoRoot, "deploy/base/postgres"), output: filepath.Join(renderDir, "postgres")},
		{source: filepath.Join(repoRoot, "deploy/base/platform-event-log/migrations.yaml.tpl"), output: filepath.Join(renderDir, "platform-event-log", "migrations.yaml")},
	}
	for _, service := range servicesForRings(rings) {
		sets = append(sets, struct {
			source string
			output string
		}{source: filepath.Join(repoRoot, "deploy/base", service.Dir), output: filepath.Join(renderDir, service.Dir)})
	}
	for _, set := range sets {
		if err := manifestrender.Render(manifestrender.Options{
			SourcePath:       set.source,
			OutputPath:       set.output,
			EnvFilePath:      renderEnv,
			ServicesFilePath: servicesFile,
		}); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return renderDir, cleanup, nil
}

func applyRegistry(ctx context.Context, config deployConfig, renderDir string, output io.Writer) error {
	if err := kubectlQuiet(ctx, "apply", "-k", filepath.Join(renderDir, "bootstrap-foundation")); err != nil {
		return fmt.Errorf("apply registry foundation: %w", err)
	}
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "rollout", "status", "deployment/kodex-registry", "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait registry rollout: %w", err)
	}
	ok(output, "registry foundation applied and ready")
	return nil
}

func buildImages(ctx context.Context, config deployConfig, repoRoot string, stack stackinventory.Stack, rings []backendRing, output io.Writer) error {
	builds, err := ringImageBuilds(stack, rings)
	if err != nil {
		return err
	}
	kanikoImage, err := stack.ImageOr("kaniko-executor", "KODEX_KANIKO_EXECUTOR_IMAGE")
	if err != nil {
		return fmt.Errorf("resolve Kaniko image: %w", err)
	}
	golangImage, err := stack.ImageOr("golang-alpine", "KODEX_GOLANG_IMAGE")
	if err != nil {
		return fmt.Errorf("resolve Go build image: %w", err)
	}
	for _, build := range builds {
		jobName := "kodex-build-" + build.Name
		if err := kubectlQuiet(ctx, "-n", config.Namespace, "delete", "job", jobName, "--ignore-not-found"); err != nil {
			return fmt.Errorf("delete previous build job %s: %w", jobName, err)
		}
		manifest := kanikoBuildJobManifest(config.Namespace, jobName, repoRoot, kanikoImage, golangImage, build)
		if err := kubectlApply(ctx, manifest); err != nil {
			return fmt.Errorf("apply build job %s: %w", jobName, err)
		}
		if err := kubectlQuiet(ctx, "-n", config.Namespace, "wait", "--for=condition=complete", "job/"+jobName, "--timeout="+config.RolloutTimeout); err != nil {
			return fmt.Errorf("wait build job %s: %w", jobName, err)
		}
		ok(output, "Kaniko build completed for "+build.Name)
	}
	return nil
}

func ringImageBuilds(stack stackinventory.Stack, rings []backendRing) ([]imageBuild, error) {
	imageNames := imageNamesForRings(rings)
	builds := make([]imageBuild, 0, len(imageNames))
	for _, imageName := range imageNames {
		spec, ok := stack.Spec.Images[imageName]
		if !ok {
			return nil, fmt.Errorf("image %s is missing from services.yaml", imageName)
		}
		destination, err := stack.Image(imageName)
		if err != nil {
			return nil, fmt.Errorf("resolve image %s: %w", imageName, err)
		}
		if strings.TrimSpace(spec.Dockerfile) == "" || strings.TrimSpace(spec.Target) == "" {
			return nil, fmt.Errorf("image %s must define dockerfile and target", imageName)
		}
		builds = append(builds, imageBuild{
			Name:        strings.ReplaceAll(imageName, "_", "-"),
			ImageName:   imageName,
			Dockerfile:  spec.Dockerfile,
			Target:      spec.Target,
			Destination: destination,
		})
	}
	return builds, nil
}

func imageNamesForRings(rings []backendRing) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, ring := range rings {
		for _, imageName := range ring.ImageNames {
			if seen[imageName] {
				continue
			}
			seen[imageName] = true
			result = append(result, imageName)
		}
	}
	return result
}

func servicesForRings(rings []backendRing) []serviceDeploy {
	seen := map[string]bool{}
	result := []serviceDeploy{}
	for _, ring := range rings {
		for _, service := range ring.Services {
			if seen[service.Name] {
				continue
			}
			seen[service.Name] = true
			result = append(result, service)
		}
	}
	return result
}

func kanikoBuildJobManifest(namespace, jobName, repoRoot, kanikoImage, golangImage string, build imageBuild) string {
	args := []string{
		"--context=dir:///workspace/repo",
		"--dockerfile=/workspace/repo/" + build.Dockerfile,
		"--destination=" + build.Destination,
		"--target=" + build.Target,
		"--build-arg=GOLANG_IMAGE=" + golangImage,
		"--insecure",
		"--skip-tls-verify",
		"--cache=" + stackinventory.EnvOr("KODEX_KANIKO_CACHE_ENABLED", "false"),
		"--snapshot-mode=" + stackinventory.EnvOr("KODEX_KANIKO_SNAPSHOT_MODE", "redo"),
		"--verbosity=" + stackinventory.EnvOr("KODEX_KANIKO_VERBOSITY", "info"),
	}
	var out strings.Builder
	out.WriteString("apiVersion: batch/v1\nkind: Job\nmetadata:\n")
	out.WriteString("  name: " + jobName + "\n")
	out.WriteString("  namespace: " + namespace + "\n")
	out.WriteString("  labels:\n    app.kubernetes.io/part-of: kodex\n    app.kubernetes.io/component: backend-image-build\n")
	out.WriteString("spec:\n  backoffLimit: 1\n  ttlSecondsAfterFinished: 3600\n  template:\n")
	out.WriteString("    metadata:\n      labels:\n        app.kubernetes.io/part-of: kodex\n        app.kubernetes.io/component: backend-image-build\n")
	out.WriteString("    spec:\n      restartPolicy: Never\n      hostNetwork: true\n      dnsPolicy: ClusterFirstWithHostNet\n")
	out.WriteString("      containers:\n        - name: kaniko\n          image: " + strconv.Quote(kanikoImage) + "\n          imagePullPolicy: IfNotPresent\n          args:\n")
	for _, arg := range args {
		out.WriteString("            - " + strconv.Quote(arg) + "\n")
	}
	out.WriteString("          resources:\n            requests:\n")
	out.WriteString("              cpu: " + strconv.Quote(stackinventory.EnvOr("KODEX_KANIKO_CPU_REQUEST", "1")) + "\n")
	out.WriteString("              memory: " + strconv.Quote(stackinventory.EnvOr("KODEX_KANIKO_MEMORY_REQUEST", "512Mi")) + "\n")
	out.WriteString("            limits:\n")
	out.WriteString("              cpu: " + strconv.Quote(stackinventory.EnvOr("KODEX_KANIKO_CPU_LIMIT", "2")) + "\n")
	out.WriteString("              memory: " + strconv.Quote(stackinventory.EnvOr("KODEX_KANIKO_MEMORY_LIMIT", "2Gi")) + "\n")
	out.WriteString("          volumeMounts:\n            - name: repo\n              mountPath: /workspace/repo\n              readOnly: true\n")
	out.WriteString("      volumes:\n        - name: repo\n          hostPath:\n            path: " + strconv.Quote(repoRoot) + "\n            type: Directory\n")
	return out.String()
}

func applyPostgres(ctx context.Context, config deployConfig, renderDir string, output io.Writer) error {
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "delete", "job", "kodex-postgres-bootstrap-databases", "--ignore-not-found"); err != nil {
		return fmt.Errorf("delete previous PostgreSQL bootstrap job: %w", err)
	}
	if err := kubectlQuiet(ctx, "apply", "-k", filepath.Join(renderDir, "postgres")); err != nil {
		return fmt.Errorf("apply PostgreSQL foundation: %w", err)
	}
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "rollout", "status", "statefulset/postgres", "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait PostgreSQL rollout: %w", err)
	}
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "wait", "--for=condition=complete", "job/kodex-postgres-bootstrap-databases", "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait PostgreSQL database bootstrap: %w", err)
	}
	ok(output, "PostgreSQL foundation and databases are ready")
	return nil
}

func applyMigrations(ctx context.Context, config deployConfig, renderDir string, rings []backendRing, output io.Writer) error {
	if err := runMigration(ctx, config, filepath.Join(renderDir, "platform-event-log", "migrations.yaml"), "platform-event-log-migrations", output); err != nil {
		return err
	}
	for _, service := range servicesForRings(rings) {
		if service.MigrationJob == "" {
			continue
		}
		if err := runMigration(ctx, config, filepath.Join(renderDir, service.Dir, "migrations.yaml"), service.MigrationJob, output); err != nil {
			return err
		}
	}
	return nil
}

func runMigration(ctx context.Context, config deployConfig, manifestPath, jobName string, output io.Writer) error {
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "delete", "job", jobName, "--ignore-not-found"); err != nil {
		return fmt.Errorf("delete previous migration job %s: %w", jobName, err)
	}
	if err := kubectlQuiet(ctx, "apply", "-f", manifestPath); err != nil {
		return fmt.Errorf("apply migration job %s: %w", jobName, err)
	}
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "wait", "--for=condition=complete", "job/"+jobName, "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait migration job %s: %w", jobName, err)
	}
	ok(output, "migration job completed for "+jobName)
	return nil
}

func applyServices(ctx context.Context, config deployConfig, renderDir string, rings []backendRing, checkHealth bool, output io.Writer) error {
	for _, service := range servicesForRings(rings) {
		if err := kubectlQuiet(ctx, "apply", "-k", filepath.Join(renderDir, service.Dir)); err != nil {
			return fmt.Errorf("apply service %s: %w", service.Name, err)
		}
		if err := kubectlQuiet(ctx, "-n", config.Namespace, "rollout", "restart", "deployment/"+service.Name); err != nil {
			return fmt.Errorf("restart service %s: %w", service.Name, err)
		}
		if err := kubectlQuiet(ctx, "-n", config.Namespace, "rollout", "status", "deployment/"+service.Name, "--timeout="+config.RolloutTimeout); err != nil {
			return fmt.Errorf("wait service %s rollout: %w", service.Name, err)
		}
		ok(output, "deployment ready for "+service.Name)
		if checkHealth {
			if err := checkReadyz(ctx, config.Namespace, service.Name, service.ReadyPort); err != nil {
				return fmt.Errorf("readyz check %s: %w", service.Name, err)
			}
			ok(output, "readyz OK for "+service.Name)
		}
	}
	return nil
}

func checkReadyz(ctx context.Context, namespace, service, targetPort string) error {
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
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+localPort+"/health/readyz", nil)
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
		time.Sleep(time.Second)
	}
	return errors.New("readyz did not become healthy")
}

func freeLocalPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = listener.Close() }()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port), nil
}

func ensureNamespace(ctx context.Context, namespace string) error {
	output, err := kubectlOutput(ctx, "create", "namespace", namespace, "--dry-run=client", "-o", "yaml")
	if err != nil {
		return err
	}
	return kubectlApply(ctx, string(output))
}

func kubectlApply(ctx context.Context, manifest string) error {
	command := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	command.Stdin = strings.NewReader(manifest)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func kubectlOutput(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "kubectl", args...)
	command.Stderr = io.Discard
	return command.Output()
}

func kubectlQuiet(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "kubectl", args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

func requireCommands(names ...string) error {
	for _, name := range names {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func randomHex(bytesCount int) (string, error) {
	data := make([]byte, bytesCount)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate secret value: %w", err)
	}
	return fmt.Sprintf("%x", data), nil
}

func postgresURI(user, password, host, port, database string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, database)
}

func envOrLookup(lookup func(string) (string, bool), key, fallback string) string {
	if value, ok := lookup(key); ok && value != "" {
		return value
	}
	return fallback
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
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
