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
	ring := flag.String("ring", "first", "deploy ring: first, second, staff, mcp, web, web-public, or all")
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

type webPublicConfig struct {
	Domain             string
	PublicBaseURL      string
	LetsEncryptEmail   string
	CertManagerVersion string
}

type serviceDeploy struct {
	Name         string
	Dir          string
	MigrationJob string
	ReadyPort    string
}

type backendRing struct {
	Name                  string
	Label                 string
	Services              []serviceDeploy
	ImageNames            []string
	UsesBackendFoundation bool
	PublicWeb             bool
}

type imageBuild struct {
	Name        string
	ImageName   string
	Dockerfile  string
	Target      string
	Destination string
}

type kubernetesJobStatus struct {
	Status struct {
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

type buildBaseImages struct {
	Golang string
	Node   string
	Nginx  string
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

var staffRingServices = []serviceDeploy{
	{Name: "staff-gateway", Dir: "staff-gateway", ReadyPort: "8080"},
}

var mcpRingServices = []serviceDeploy{
	{Name: "platform-mcp-server", Dir: "platform-mcp-server", ReadyPort: "8080"},
}

var webRingServices = []serviceDeploy{
	{Name: "web-console", Dir: "web-console", ReadyPort: "8080"},
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

var staffRingImageNames = []string{
	"staff-gateway",
}

var mcpRingImageNames = []string{
	"platform-mcp-server",
}

var webRingImageNames = []string{
	"web-console",
}

var (
	firstRing = backendRing{
		Name:                  "first",
		Label:                 "first backend ring",
		Services:              firstRingServices,
		ImageNames:            firstRingImageNames,
		UsesBackendFoundation: true,
	}
	secondRing = backendRing{
		Name:                  "second",
		Label:                 "second backend ring",
		Services:              secondRingServices,
		ImageNames:            secondRingImageNames,
		UsesBackendFoundation: true,
	}
	staffRing = backendRing{
		Name:                  "staff",
		Label:                 "staff gateway contour",
		Services:              staffRingServices,
		ImageNames:            staffRingImageNames,
		UsesBackendFoundation: true,
	}
	mcpRing = backendRing{
		Name:                  "mcp",
		Label:                 "platform MCP server contour",
		Services:              mcpRingServices,
		ImageNames:            mcpRingImageNames,
		UsesBackendFoundation: true,
	}
	webRing = backendRing{
		Name:       "web",
		Label:      "staff web-console contour",
		Services:   webRingServices,
		ImageNames: webRingImageNames,
	}
	webPublicRing = backendRing{
		Name:      "web-public",
		Label:     "public web-console contour",
		PublicWeb: true,
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
	ok(output, "selected deploy rings: "+strings.Join(ringNames(rings), ", "))

	config, err := loadDeployConfig()
	if err != nil {
		return err
	}
	var publicConfig webPublicConfig
	if hasPublicWebRing(rings) {
		publicConfig, err = loadWebPublicConfig(stack)
		if err != nil {
			return err
		}
		ok(output, "public web contour env names checked without printing values")
	}
	if err := requireCommands("go", "kubectl"); err != nil {
		return err
	}
	ok(output, "required commands are present")

	if err := ensureNamespace(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "namespace exists")

	if requiresWorkloadDeploy(rings) {
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
		if requiresBackendFoundation(rings) {
			if err := applyPostgres(ctx, config, renderDir, output); err != nil {
				return err
			}
			if err := applyMigrations(ctx, config, renderDir, rings, output); err != nil {
				return err
			}
		} else {
			skip(output, "PostgreSQL and backend migrations skipped for selected ring")
		}
		if err := applyServices(ctx, config, renderDir, rings, !options.SkipHealth, output); err != nil {
			return err
		}
	} else {
		skip(output, "backend workload deploy skipped for selected ring")
	}

	if hasPublicWebRing(rings) {
		if err := deployWebPublicContour(ctx, config, publicConfig, repoRoot, servicesFile, output); err != nil {
			return err
		}
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
	case "staff", "staff-gateway":
		return []backendRing{staffRing}, nil
	case "mcp", "platform-mcp", "platform-mcp-server":
		return []backendRing{mcpRing}, nil
	case "web", "web-console", "frontend":
		return []backendRing{webRing}, nil
	case "web-public", "public-web", "web-contour", "frontend-public":
		return []backendRing{webPublicRing}, nil
	case "all":
		return []backendRing{firstRing, secondRing}, nil
	default:
		return nil, fmt.Errorf("unsupported deploy ring %q; expected first, second, staff, mcp, web, web-public, or all", value)
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

func loadWebPublicConfig(stack stackinventory.Stack) (webPublicConfig, error) {
	const requiredDomain = "platform.kodex.works"
	const requiredBaseURL = "https://platform.kodex.works"

	domain := strings.TrimSpace(os.Getenv("KODEX_PRODUCTION_DOMAIN"))
	if domain == "" {
		return webPublicConfig{}, errors.New("KODEX_PRODUCTION_DOMAIN is required for web-public")
	}
	if domain != requiredDomain {
		return webPublicConfig{}, errors.New("KODEX_PRODUCTION_DOMAIN must match the public web-console domain")
	}
	publicBaseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("KODEX_PUBLIC_BASE_URL")), "/")
	if publicBaseURL == "" {
		return webPublicConfig{}, errors.New("KODEX_PUBLIC_BASE_URL is required for web-public")
	}
	if publicBaseURL != requiredBaseURL {
		return webPublicConfig{}, errors.New("KODEX_PUBLIC_BASE_URL must match the public web-console base URL")
	}
	letsEncryptEmail := strings.TrimSpace(os.Getenv("KODEX_LETSENCRYPT_EMAIL"))
	if letsEncryptEmail == "" {
		return webPublicConfig{}, errors.New("KODEX_LETSENCRYPT_EMAIL is required for web-public")
	}
	certManagerVersion, err := stack.Version("cert-manager")
	if err != nil {
		return webPublicConfig{}, fmt.Errorf("resolve cert-manager version: %w", err)
	}
	return webPublicConfig{
		Domain:             domain,
		PublicBaseURL:      publicBaseURL,
		LetsEncryptEmail:   letsEncryptEmail,
		CertManagerVersion: certManagerVersion,
	}, nil
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
		if strings.TrimSpace(source) == "" {
			return secretValues{}, fmt.Errorf("cannot derive %s from empty %s", derived.Key, derived.SourceKey)
		}
		current := ""
		if secret := existing["kodex-platform-runtime"]; secret != nil {
			current = strings.TrimSpace(secret[derived.Key])
		}
		runtime[derived.Key] = source
		if current != source {
			generated = append(generated, derived.Key)
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
		"KODEX_PLATFORM_MCP_SERVER_MCP_AUTH_TOKEN",
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
		{Key: "KODEX_AGENT_MANAGER_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN", SourceKey: "KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN"},
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

func applyWebPublicOAuthSecret(ctx context.Context, namespace string, output io.Writer) error {
	existing, err := readSecret(ctx, namespace, "kodex-web-oauth2-proxy")
	if err != nil {
		return err
	}
	values, generated, err := resolveWebPublicOAuthSecretValues(existing, os.LookupEnv)
	if err != nil {
		return err
	}
	if err := kubectlApply(ctx, secretManifest(namespace, "kodex-web-oauth2-proxy", values)); err != nil {
		return fmt.Errorf("apply web public OAuth secret: %w", err)
	}
	if len(generated) > 0 {
		ok(output, fmt.Sprintf("generated or normalized web public OAuth secret keys: %d", len(generated)))
	} else {
		ok(output, "web public OAuth secret keys already present")
	}
	return nil
}

func resolveWebPublicOAuthSecretValues(existing map[string]string, lookup func(string) (string, bool)) (map[string]string, []string, error) {
	generated := []string{}
	required := func(key string) (string, error) {
		if value := strings.TrimSpace(existing[key]); value != "" {
			return value, nil
		}
		if value, ok := lookup(key); ok && strings.TrimSpace(value) != "" {
			generated = append(generated, key)
			return value, nil
		}
		return "", fmt.Errorf("%s is required for web-public auth secret", key)
	}
	cookieSecret := func(key string) (string, error) {
		if value := strings.TrimSpace(existing[key]); validOAuth2CookieSecret(value) {
			return value, nil
		}
		if value, ok := lookup(key); ok && strings.TrimSpace(value) != "" {
			value = strings.TrimSpace(value)
			if !validOAuth2CookieSecret(value) {
				return "", fmt.Errorf("%s must be 16, 24, or 32 bytes for oauth2-proxy", key)
			}
			generated = append(generated, key)
			return value, nil
		}
		value, err := randomCookieSecret()
		if err != nil {
			return "", err
		}
		generated = append(generated, key)
		return value, nil
	}

	values := map[string]string{}
	var err error
	values["KODEX_GITHUB_OAUTH_CLIENT_ID"], err = required("KODEX_GITHUB_OAUTH_CLIENT_ID")
	if err != nil {
		return nil, nil, err
	}
	values["KODEX_GITHUB_OAUTH_CLIENT_SECRET"], err = required("KODEX_GITHUB_OAUTH_CLIENT_SECRET")
	if err != nil {
		return nil, nil, err
	}
	values["KODEX_OAUTH2_PROXY_COOKIE_SECRET"], err = cookieSecret("KODEX_OAUTH2_PROXY_COOKIE_SECRET")
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(generated)
	return values, generated, nil
}

func validOAuth2CookieSecret(value string) bool {
	switch len([]byte(value)) {
	case 16, 24, 32:
		return true
	default:
		return false
	}
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

func deployWebPublicContour(ctx context.Context, config deployConfig, publicConfig webPublicConfig, repoRoot, servicesFile string, output io.Writer) error {
	if err := applyCertManager(ctx, publicConfig.CertManagerVersion, output); err != nil {
		return err
	}
	if err := waitCertManager(ctx, config, output); err != nil {
		return err
	}

	renderDir, cleanupRenderDir, err := renderWebPublicContour(repoRoot, servicesFile)
	if err != nil {
		return err
	}
	defer cleanupRenderDir()
	ok(output, "public web manifests rendered in a private temp dir")

	if err := kubectlQuiet(ctx, "apply", "-k", filepath.Join(renderDir, "web-public-foundation")); err != nil {
		return fmt.Errorf("apply public web foundation: %w", err)
	}
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "rollout", "status", "deployment/kodex-public-ingress", "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait public ingress rollout: %w", err)
	}
	ok(output, "public ingress controller ready")
	if err := kubectlQuiet(ctx, "wait", "--for=condition=Ready", "clusterissuer/kodex-letsencrypt-production", "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait Let's Encrypt ClusterIssuer: %w", err)
	}
	ok(output, "Let's Encrypt ClusterIssuer ready")

	if err := applyWebPublicOAuthSecret(ctx, config.Namespace, output); err != nil {
		return err
	}

	if err := kubectlQuiet(ctx, "apply", "-k", filepath.Join(renderDir, "web-console-public")); err != nil {
		return fmt.Errorf("apply web-console public contour: %w", err)
	}
	if err := ensurePublicIngressTargetsOAuth2Proxy(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "public Ingress targets oauth2-proxy only")
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "rollout", "status", "deployment/web-console-public-oauth2-proxy", "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait oauth2-proxy rollout: %w", err)
	}
	ok(output, "oauth2-proxy ready")
	if err := checkHTTP(ctx, config.Namespace, "web-console-public-oauth2-proxy", "4180", "/ping"); err != nil {
		return fmt.Errorf("oauth2-proxy ping check: %w", err)
	}
	ok(output, "oauth2-proxy ping OK")
	if err := kubectlQuiet(ctx, "-n", config.Namespace, "wait", "--for=condition=Ready", "certificate/web-console-public-tls", "--timeout="+config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait public web TLS certificate: %w", err)
	}
	ok(output, "public web TLS certificate ready")
	if err := kubectlQuiet(ctx, "apply", "-k", filepath.Join(renderDir, "integration-gateway-public")); err != nil {
		return fmt.Errorf("apply integration-gateway public webhook contour: %w", err)
	}
	if err := ensureProviderWebhookIngressTargets(ctx, config.Namespace); err != nil {
		return err
	}
	ok(output, "provider webhook public Ingress targets integration-gateway only")
	return nil
}

func applyCertManager(ctx context.Context, version string, output io.Writer) error {
	url := "https://github.com/cert-manager/cert-manager/releases/download/" + version + "/cert-manager.yaml"
	if err := kubectlQuiet(ctx, "apply", "-f", url); err != nil {
		return fmt.Errorf("apply cert-manager static manifest: %w", err)
	}
	ok(output, "cert-manager static manifest applied")
	return nil
}

func waitCertManager(ctx context.Context, config deployConfig, output io.Writer) error {
	for _, crd := range []string{
		"certificates.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"challenges.acme.cert-manager.io",
		"orders.acme.cert-manager.io",
	} {
		if err := kubectlQuiet(ctx, "wait", "--for=condition=Established", "crd/"+crd, "--timeout="+config.RolloutTimeout); err != nil {
			return fmt.Errorf("wait cert-manager CRD %s: %w", crd, err)
		}
	}
	for _, deployment := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
		if err := kubectlQuiet(ctx, "-n", "cert-manager", "rollout", "status", "deployment/"+deployment, "--timeout="+config.RolloutTimeout); err != nil {
			return fmt.Errorf("wait cert-manager deployment %s: %w", deployment, err)
		}
	}
	ok(output, "cert-manager ready")
	return nil
}

func renderWebPublicContour(repoRoot, servicesFile string) (string, func(), error) {
	renderDir, err := os.MkdirTemp("", "kodex-web-public-render-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create public web render dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(renderDir) }
	sets := []struct {
		source string
		output string
	}{
		{source: filepath.Join(repoRoot, "deploy/base/web-public-foundation"), output: filepath.Join(renderDir, "web-public-foundation")},
		{source: filepath.Join(repoRoot, "deploy/base/web-console-public"), output: filepath.Join(renderDir, "web-console-public")},
		{source: filepath.Join(repoRoot, "deploy/base/integration-gateway-public"), output: filepath.Join(renderDir, "integration-gateway-public")},
	}
	for _, set := range sets {
		if err := manifestrender.Render(manifestrender.Options{
			SourcePath:       set.source,
			OutputPath:       set.output,
			ServicesFilePath: servicesFile,
		}); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return renderDir, cleanup, nil
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
	baseImages, err := resolveBuildBaseImages(stack)
	if err != nil {
		return err
	}
	for _, build := range builds {
		jobName := "kodex-build-" + build.Name
		if err := kubectlQuiet(ctx, "-n", config.Namespace, "delete", "job", jobName, "--ignore-not-found"); err != nil {
			return fmt.Errorf("delete previous build job %s: %w", jobName, err)
		}
		manifest := kanikoBuildJobManifest(config.Namespace, jobName, repoRoot, kanikoImage, baseImages, build)
		if err := kubectlApply(ctx, manifest); err != nil {
			return fmt.Errorf("apply build job %s: %w", jobName, err)
		}
		if err := waitJobComplete(ctx, config.Namespace, jobName, config.RolloutTimeout); err != nil {
			return fmt.Errorf("wait build job %s: %w", jobName, err)
		}
		ok(output, "Kaniko build completed for "+build.Name)
	}
	return nil
}

func resolveBuildBaseImages(stack stackinventory.Stack) (buildBaseImages, error) {
	golangImage, err := stack.ImageOr("golang-alpine", "KODEX_GOLANG_IMAGE")
	if err != nil {
		return buildBaseImages{}, fmt.Errorf("resolve Go build image: %w", err)
	}
	nodeImage, err := stack.ImageOr("node-alpine", "KODEX_NODE_IMAGE")
	if err != nil {
		return buildBaseImages{}, fmt.Errorf("resolve Node build image: %w", err)
	}
	nginxImage, err := stack.ImageOr("nginx-unprivileged", "KODEX_NGINX_IMAGE")
	if err != nil {
		return buildBaseImages{}, fmt.Errorf("resolve nginx runtime image: %w", err)
	}
	return buildBaseImages{
		Golang: golangImage,
		Node:   nodeImage,
		Nginx:  nginxImage,
	}, nil
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

func requiresBackendFoundation(rings []backendRing) bool {
	for _, ring := range rings {
		if ring.UsesBackendFoundation {
			return true
		}
	}
	return false
}

func requiresWorkloadDeploy(rings []backendRing) bool {
	return len(servicesForRings(rings)) > 0 || len(imageNamesForRings(rings)) > 0 || requiresBackendFoundation(rings)
}

func hasPublicWebRing(rings []backendRing) bool {
	for _, ring := range rings {
		if ring.PublicWeb {
			return true
		}
	}
	return false
}

func kanikoBuildJobManifest(namespace, jobName, repoRoot, kanikoImage string, baseImages buildBaseImages, build imageBuild) string {
	args := []string{
		"--context=dir:///workspace/repo",
		"--dockerfile=/workspace/repo/" + build.Dockerfile,
		"--destination=" + build.Destination,
		"--target=" + build.Target,
		"--build-arg=GOLANG_IMAGE=" + baseImages.Golang,
		"--build-arg=NODE_IMAGE=" + baseImages.Node,
		"--build-arg=NGINX_IMAGE=" + baseImages.Nginx,
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
	if err := waitJobComplete(ctx, config.Namespace, "kodex-postgres-bootstrap-databases", config.RolloutTimeout); err != nil {
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
	if err := waitJobComplete(ctx, config.Namespace, jobName, config.RolloutTimeout); err != nil {
		return fmt.Errorf("wait migration job %s: %w", jobName, err)
	}
	ok(output, "migration job completed for "+jobName)
	return nil
}

func waitJobComplete(ctx context.Context, namespace string, jobName string, timeoutValue string) error {
	timeout, err := time.ParseDuration(timeoutValue)
	if err != nil || timeout <= 0 {
		timeout = 30 * time.Minute
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		done, err := checkJobComplete(waitCtx, namespace, jobName)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("job %s did not complete before timeout", jobName)
		case <-ticker.C:
		}
	}
}

func checkJobComplete(ctx context.Context, namespace string, jobName string) (bool, error) {
	output, err := kubectlOutput(ctx, "-n", namespace, "get", "job", jobName, "-o", "json")
	if err != nil {
		return false, fmt.Errorf("read job %s status: %w", jobName, err)
	}
	var status kubernetesJobStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return false, fmt.Errorf("parse job %s status: %w", jobName, err)
	}
	return jobCompleteFromStatus(jobName, status)
}

func jobCompleteFromStatus(jobName string, status kubernetesJobStatus) (bool, error) {
	for _, condition := range status.Status.Conditions {
		if condition.Status != "True" {
			continue
		}
		switch condition.Type {
		case "Complete":
			return true, nil
		case "Failed", "FailureTarget":
			reason := firstNonEmpty(condition.Reason, "failed")
			return false, fmt.Errorf("job %s failed: %s", jobName, reason)
		}
	}
	return false, nil
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
	return checkHTTP(ctx, namespace, service, targetPort, "/health/readyz")
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
	deadline := time.Now().Add(60 * time.Second)
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
		time.Sleep(time.Second)
	}
	return errors.New("HTTP check did not become healthy")
}

func ensurePublicIngressTargetsOAuth2Proxy(ctx context.Context, namespace string) error {
	targets, err := readIngressTargets(ctx, namespace, "web-console-public")
	if err != nil {
		return err
	}
	services := ingressTargetServices(targets)
	if len(services) == 0 {
		return errors.New("public web Ingress has no service backends")
	}
	if services["web-console"] {
		return errors.New("public web Ingress must not target web-console directly")
	}
	if !services["web-console-public-oauth2-proxy"] {
		return errors.New("public web Ingress must target oauth2-proxy")
	}
	if len(services) != 1 {
		return errors.New("public web Ingress must target only oauth2-proxy")
	}
	return nil
}

func ensureProviderWebhookIngressTargets(ctx context.Context, namespace string) error {
	targets, err := readIngressTargets(ctx, namespace, "integration-gateway-public-webhook")
	if err != nil {
		return fmt.Errorf("read provider webhook public Ingress: %w", err)
	}
	services := ingressTargetServices(targets)
	if len(services) == 0 {
		return errors.New("provider webhook public Ingress has no service backends")
	}
	if services["web-console-public-oauth2-proxy"] {
		return errors.New("provider webhook public Ingress must not target oauth2-proxy")
	}
	if services["web-console"] {
		return errors.New("provider webhook public Ingress must not target web-console")
	}
	if !services["integration-gateway"] {
		return errors.New("provider webhook public Ingress must target integration-gateway")
	}
	if len(services) != 1 {
		return errors.New("provider webhook public Ingress must target only integration-gateway")
	}
	for _, target := range targets {
		if target.Service != "integration-gateway" {
			continue
		}
		if target.Path != "/v1/provider-webhooks/github" || target.PathType != "Exact" {
			return errors.New("provider webhook public Ingress must expose only the exact GitHub webhook path")
		}
	}
	return nil
}

type ingressTarget struct {
	Service  string
	Path     string
	PathType string
}

func readIngressTargets(ctx context.Context, namespace, name string) ([]ingressTarget, error) {
	output, err := kubectlOutput(ctx, "-n", namespace, "get", "ingress", name, "-o", "json")
	if err != nil {
		return nil, fmt.Errorf("read Ingress %s: %w", name, err)
	}
	var ingress struct {
		Spec struct {
			Rules []struct {
				HTTP struct {
					Paths []struct {
						Path     string `json:"path"`
						PathType string `json:"pathType"`
						Backend  struct {
							Service struct {
								Name string `json:"name"`
							} `json:"service"`
						} `json:"backend"`
					} `json:"paths"`
				} `json:"http"`
			} `json:"rules"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(output, &ingress); err != nil {
		return nil, fmt.Errorf("decode Ingress %s: %w", name, err)
	}
	targets := []ingressTarget{}
	for _, rule := range ingress.Spec.Rules {
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service.Name == "" {
				continue
			}
			targets = append(targets, ingressTarget{
				Service:  path.Backend.Service.Name,
				Path:     path.Path,
				PathType: path.PathType,
			})
		}
	}
	return targets, nil
}

func ingressTargetServices(targets []ingressTarget) map[string]bool {
	services := map[string]bool{}
	for _, target := range targets {
		services[target.Service] = true
	}
	return services
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

func randomCookieSecret() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate secret value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
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
