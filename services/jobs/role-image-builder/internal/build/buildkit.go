package build

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	controlclient "github.com/codex-k8s/matter-codex/services/jobs/role-image-builder/internal/clients/controlplane"
)

var (
	ErrBuildKit   = errors.New("BuildKit execution failed")
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const (
	provenanceBindingSchema = "mattercodex.dev/image-provenance-binding/v1"
	expectedBuilderID       = "spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder"
	expectedBuildType       = "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md"
)

type Config struct {
	Binary, Address, TLSServerName, CAFile, CertificateFile, PrivateKeyFile string
	InputDockerConfig, WorkspaceRoot                                        string
	InputRegistryTLSServerName, InputRegistryCAFile                         string
	InputRegistryCertificateFile, InputRegistryPrivateKeyFile               string
	InputRepository, TrustedRoleBaseRepository, TrustedRoleBaseDigest       string
	FrontendRepository, StagingRepository                                   string
	ExpectedBuilderSHA256, ExpectedFrontendSHA256, ExpectedToolchainSHA256  string
	RoleRuntimeContractSHA256                                               string
	RoleRuntimeContractRevision                                             uint64
}

type Executor struct {
	config       Config
	materializer *Materializer
}

type Prepared struct {
	root, contextDirectory, dockerfile, installation string
	input                                            *controlplanev1.RoleImageBuildInput
}

type Phase struct {
	Stage   controlplanev1.ImageBuildStage
	Percent uint32
}

type Failure struct {
	ErrorCode, DiagnosticCode, DiagnosticSummary string
}

func (failure Failure) Error() string { return failure.ErrorCode }

func New(config Config) (*Executor, error) {
	if !filepath.IsAbs(config.Binary) ||
		!filepath.IsAbs(config.WorkspaceRoot) || !filepath.IsAbs(config.CAFile) || !filepath.IsAbs(config.CertificateFile) ||
		!filepath.IsAbs(config.PrivateKeyFile) || !filepath.IsAbs(config.InputDockerConfig) ||
		!filepath.IsAbs(config.InputRegistryCAFile) || !filepath.IsAbs(config.InputRegistryCertificateFile) ||
		!filepath.IsAbs(config.InputRegistryPrivateKeyFile) || !validDNSName(config.InputRegistryTLSServerName) ||
		!strings.HasPrefix(config.Address, "tcp://") || !validDNSName(config.TLSServerName) ||
		!validRepository(config.InputRepository) || !validRepository(config.TrustedRoleBaseRepository) ||
		!digestPattern.MatchString(config.TrustedRoleBaseDigest) || !validRepository(config.FrontendRepository) ||
		!validRepository(config.StagingRepository) || !plainSHA256(config.ExpectedBuilderSHA256) ||
		!plainSHA256(config.ExpectedFrontendSHA256) || !plainSHA256(config.ExpectedToolchainSHA256) ||
		config.RoleRuntimeContractRevision == 0 || !plainSHA256(config.RoleRuntimeContractSHA256) {
		return nil, errors.New("role image builder BuildKit configuration is invalid")
	}
	materializer, err := newMaterializer(MaterializerConfig{DockerConfig: config.InputDockerConfig,
		Repository: config.InputRepository, TLSServerName: config.InputRegistryTLSServerName,
		CAFile: config.InputRegistryCAFile, CertificateFile: config.InputRegistryCertificateFile,
		PrivateKeyFile: config.InputRegistryPrivateKeyFile})
	if err != nil {
		return nil, err
	}
	return &Executor{config: config, materializer: materializer}, nil
}

func (executor *Executor) Check(ctx context.Context) error {
	if err := executor.materializer.Check(ctx); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, executor.config.Binary,
		"--addr", executor.config.Address, "--tlscacert", executor.config.CAFile,
		"--tlscert", executor.config.CertificateFile, "--tlskey", executor.config.PrivateKeyFile,
		"--tlsservername", executor.config.TLSServerName, "debug", "workers")
	command.Stdout, command.Stderr = ioDiscard{}, ioDiscard{}
	if err := command.Run(); err != nil {
		return ErrBuildKit
	}
	root, err := os.MkdirTemp(executor.config.WorkspaceRoot, "buildkit-readiness-")
	if err != nil {
		return ErrBuildKit
	}
	defer os.RemoveAll(root)
	dockerfile := []byte(fmt.Sprintf("# syntax=%s@sha256:%s\nFROM %s@%s\nRUN [\"/bin/sh\",\"-c\",\"test -x /usr/local/bin/mattercodex-init && test -x /usr/local/bin/matter-codex-agent-runner\"]\n",
		executor.config.FrontendRepository, executor.config.ExpectedFrontendSHA256,
		executor.config.TrustedRoleBaseRepository, executor.config.TrustedRoleBaseDigest))
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), dockerfile, 0o600); err != nil {
		return ErrBuildKit
	}
	command = exec.CommandContext(ctx, executor.config.Binary,
		"--addr", executor.config.Address, "--tlscacert", executor.config.CAFile,
		"--tlscert", executor.config.CertificateFile, "--tlskey", executor.config.PrivateKeyFile,
		"--tlsservername", executor.config.TLSServerName, "build", "--frontend", "dockerfile.v0",
		"--local", "context="+root, "--local", "dockerfile="+root, "--opt", "filename=Dockerfile",
		"--opt", "platform=linux/amd64", "--output", "type=cacheonly", "--no-cache")
	command.Env = append(os.Environ(), "DOCKER_CONFIG="+filepath.Dir(executor.config.InputDockerConfig), "HOME="+root)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		return ErrBuildKit
	}
	return nil
}

func (executor *Executor) Prepare(
	ctx context.Context,
	input *controlplanev1.RoleImageBuildInput,
	beforeContextValidation func() error,
) (*Prepared, string, error) {
	if input == nil || !plainSHA256(input.GetContextSha256()) || !plainSHA256(input.GetSourceSha256()) ||
		!plainSHA256(input.GetSpecSha256()) || !plainSHA256(input.GetImmutableBuildSha256()) ||
		input.GetFrontendSha256() != executor.config.ExpectedFrontendSHA256 || !digestPattern.MatchString(input.GetBaseImageDigest()) ||
		input.GetBaseImageReference() != executor.config.TrustedRoleBaseRepository ||
		input.GetBaseImageDigest() != executor.config.TrustedRoleBaseDigest ||
		input.GetBuilderSha256() != executor.config.ExpectedBuilderSHA256 ||
		input.GetToolchainSha256() != executor.config.ExpectedToolchainSHA256 ||
		input.GetRoleRuntimeContractRevision() != executor.config.RoleRuntimeContractRevision ||
		input.GetRoleRuntimeContractSha256() != executor.config.RoleRuntimeContractSHA256 ||
		!strings.HasPrefix(input.GetContextRef(), "oci://") ||
		strings.ContainsAny(input.GetInstallationBlock(), "\x00\r") {
		return nil, "INPUT_FETCH_REJECTED", ErrInvalidContext
	}
	root, err := os.MkdirTemp(executor.config.WorkspaceRoot, "image-build-")
	if err != nil {
		return nil, "INPUT_FETCH_REJECTED", ErrInvalidContext
	}
	prepared := &Prepared{root: root, contextDirectory: filepath.Join(root, "context"),
		dockerfile: filepath.Join(root, "dockerfile"), installation: filepath.Join(root, "installation"),
		input: input}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	diagnostic, err := executor.materializer.Materialize(ctx, root, input, beforeContextValidation)
	if err != nil {
		return nil, diagnostic, err
	}
	if err := verifyPinnedInputs(prepared.contextDirectory, input); err != nil {
		return nil, "INPUT_DIGEST_MISMATCH", err
	}
	if err := os.MkdirAll(prepared.dockerfile, 0o700); err != nil {
		return nil, "ARCHIVE_REJECTED", ErrInvalidContext
	}
	if err := os.WriteFile(filepath.Join(prepared.dockerfile, "Dockerfile"), dockerfile(input, executor.config.FrontendRepository), 0o600); err != nil {
		return nil, "ARCHIVE_REJECTED", ErrInvalidContext
	}
	if err := os.MkdirAll(prepared.installation, 0o700); err != nil {
		return nil, "ARCHIVE_REJECTED", ErrInvalidContext
	}
	if err := os.WriteFile(filepath.Join(prepared.installation, "install.sh"), installationScript(input), 0o600); err != nil {
		return nil, "ARCHIVE_REJECTED", ErrInvalidContext
	}
	cleanup = false
	return prepared, "", nil
}

func (prepared *Prepared) Close() error { return os.RemoveAll(prepared.root) }

func (executor *Executor) Build(
	ctx context.Context,
	prepared *Prepared,
	attempt uint32,
	phases chan<- Phase,
) (controlclient.BuildEvidence, error) {
	metadataFile := filepath.Join(prepared.root, "metadata.json")
	input := prepared.input
	tag := fmt.Sprintf("%s:spec-%s-attempt-%d", executor.config.StagingRepository, input.GetSpecSha256()[:20], attempt)
	platforms := make([]string, 0, len(input.GetPlatforms()))
	for _, platform := range input.GetPlatforms() {
		value := platform.GetOs() + "/" + platform.GetArchitecture()
		if platform.GetVariant() != "" {
			value += "/" + platform.GetVariant()
		}
		platforms = append(platforms, value)
	}
	args := []string{"--addr", executor.config.Address, "--tlscacert", executor.config.CAFile,
		"--tlscert", executor.config.CertificateFile, "--tlskey", executor.config.PrivateKeyFile,
		"--tlsservername", executor.config.TLSServerName, "build", "--frontend", "dockerfile.v0",
		"--local", "context=" + prepared.contextDirectory, "--local", "dockerfile=" + prepared.dockerfile,
		"--local", "mattercodex-install=" + prepared.installation,
		"--opt", "context:mattercodex-install=local:mattercodex-install",
		"--opt", "filename=Dockerfile", "--opt", "platform=" + strings.Join(platforms, ","),
		"--opt", "label:mattercodex.dev/spec-sha256=" + input.GetSpecSha256(),
		"--opt", "label:mattercodex.dev/source-sha256=" + input.GetSourceSha256(),
		"--opt", "label:mattercodex.dev/context-sha256=" + input.GetContextSha256(),
		"--opt", "label:mattercodex.dev/base-image-digest=" + input.GetBaseImageDigest(),
		"--opt", "label:mattercodex.dev/builder-sha256=" + input.GetBuilderSha256(),
		"--opt", "label:mattercodex.dev/frontend-sha256=" + input.GetFrontendSha256(),
		"--opt", "label:mattercodex.dev/toolchain-sha256=" + input.GetToolchainSha256(),
		"--opt", "label:mattercodex.dev/immutable-build-sha256=" + input.GetImmutableBuildSha256(),
		"--opt", fmt.Sprintf("label:mattercodex.dev/policy-revision=%d", input.GetPolicyRevision()),
		"--opt", "label:mattercodex.dev/policy-sha256=" + input.GetPolicySha256(),
		"--opt", "attest:provenance=mode=min,builder-id=" + expectedBuilderID, "--progress=rawjson",
		"--output", "type=image,name=" + tag + ",push=true", "--metadata-file", metadataFile}
	command := exec.CommandContext(ctx, executor.config.Binary, args...)
	command.Env = append(os.Environ(), "DOCKER_CONFIG="+filepath.Dir(executor.config.InputDockerConfig), "HOME="+prepared.root)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return controlclient.BuildEvidence{}, Failure{"SOLVE_FAILED", "BUILD_GRAPH_REJECTED", "Build graph was rejected"}
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return controlclient.BuildEvidence{}, Failure{"SOLVE_FAILED", "BUILD_GRAPH_REJECTED", "Build graph did not start"}
	}
	lastStage := controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		stage := phaseFromRawJSON(scanner.Bytes())
		if stage == controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED || stage == lastStage {
			continue
		}
		lastStage = stage
		select {
		case phases <- phaseProgress(stage):
		default:
		}
	}
	waitErr := command.Wait()
	if scanner.Err() != nil || waitErr != nil {
		return controlclient.BuildEvidence{}, failureForStage(lastStage)
	}
	metadata, err := os.ReadFile(metadataFile)
	if err != nil || len(metadata) == 0 || len(metadata) > 1<<20 {
		return controlclient.BuildEvidence{}, Failure{"PROVENANCE_INVALID", "PROVENANCE_REJECTED", "Build metadata was rejected"}
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(metadata, &document) != nil {
		return controlclient.BuildEvidence{}, Failure{"PROVENANCE_INVALID", "PROVENANCE_REJECTED", "Build metadata was rejected"}
	}
	var digest string
	if json.Unmarshal(document["containerimage.digest"], &digest) != nil || !digestPattern.MatchString(digest) {
		return controlclient.BuildEvidence{}, Failure{"STAGING_PUSH_FAILED", "STAGING_EXPORT_REJECTED", "Staging digest was rejected"}
	}
	provenanceSHA256, err := provenanceBindingSHA256(input, digest)
	if err != nil {
		return controlclient.BuildEvidence{}, Failure{"PROVENANCE_INVALID", "PROVENANCE_REJECTED", "Provenance binding was rejected"}
	}
	return controlclient.BuildEvidence{StagingReference: executor.config.StagingRepository + "@" + digest,
		ManifestDigest: digest, ProvenanceSHA256: provenanceSHA256,
		ImmutableBuildSHA256: input.GetImmutableBuildSha256()}, nil
}

func provenanceBindingSHA256(input *controlplanev1.RoleImageBuildInput, manifestDigest string) (string, error) {
	document, err := json.Marshal(map[string]any{
		"buildType":            expectedBuildType,
		"builderId":            expectedBuilderID,
		"immutableBuildSHA256": input.GetImmutableBuildSha256(),
		"manifestDigest":       manifestDigest,
		"policyRevision":       input.GetPolicyRevision(),
		"policySHA256":         input.GetPolicySha256(),
		"schema":               provenanceBindingSchema,
		"specSHA256":           input.GetSpecSha256(),
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:]), nil
}

func dockerfile(input *controlplanev1.RoleImageBuildInput, frontendRepository string) []byte {
	mounts := []string{
		"--mount=type=bind,target=/workspace/source,readonly",
		"--mount=type=bind,from=mattercodex-install,source=install.sh,target=/run/mattercodex/install.sh,readonly",
	}
	return []byte(fmt.Sprintf("# syntax=%s@sha256:%s\nFROM %s@%s AS trusted-runtime\nFROM %s@%s\nRUN %s /bin/sh /run/mattercodex/install.sh\nCOPY --from=trusted-runtime /usr/local/bin/mattercodex-init /usr/local/bin/mattercodex-init\nCOPY --from=trusted-runtime /usr/local/bin/matter-codex-agent-runner /usr/local/bin/matter-codex-agent-runner\nUSER 10001:10001\nENTRYPOINT [\"/usr/local/bin/mattercodex-init\",\"entrypoint\",\"/usr/local/bin/matter-codex-agent-runner\"]\nCMD [\"runtime-session\"]\nLABEL mattercodex.dev/spec-sha256=%q mattercodex.dev/runtime-contract-sha256=%q\n",
		frontendRepository, input.GetFrontendSha256(), input.GetBaseImageReference(), input.GetBaseImageDigest(),
		input.GetBaseImageReference(), input.GetBaseImageDigest(), strings.Join(mounts, " "), input.GetSpecSha256(),
		input.GetRoleRuntimeContractSha256()))
}

func installationScript(input *controlplanev1.RoleImageBuildInput) []byte {
	var script strings.Builder
	script.WriteString("set -eu\n")
	for _, item := range input.GetPackages() {
		path := "/workspace/source/.mattercodex/packages/" + strings.TrimPrefix(item.GetDigest(), "sha256:")
		switch item.GetManager() {
		case "apk":
			fmt.Fprintf(&script, "apk add --no-network --allow-untrusted %s\n", shellQuote(path))
		case "apt":
			fmt.Fprintf(&script, "dpkg --install %s\n", shellQuote(path))
		case "dnf":
			fmt.Fprintf(&script, "dnf --disablerepo='*' install -y %s\n", shellQuote(path))
		case "pip":
			fmt.Fprintf(&script, "python3 -m pip install --no-index --no-deps %s\n", shellQuote(path))
		case "npm":
			fmt.Fprintf(&script, "npm install --global --offline %s\n", shellQuote(path))
		}
	}
	for _, item := range input.GetTools() {
		source := "/workspace/source/.mattercodex/tools/" + item.GetSha256()
		target := "/usr/local/bin/" + item.GetName()
		fmt.Fprintf(&script, "install -m 0555 %s %s\n", shellQuote(source), shellQuote(target))
	}
	script.WriteString(input.GetInstallationBlock())
	script.WriteByte('\n')
	return []byte(script.String())
}

func verifyPinnedInputs(root string, input *controlplanev1.RoleImageBuildInput) error {
	for _, item := range input.GetPackages() {
		digest := strings.TrimPrefix(item.GetDigest(), "sha256:")
		if !plainSHA256(digest) || !supportedPackageManager(item.GetManager()) ||
			verifyRegularFileSHA256(filepath.Join(root, ".mattercodex", "packages", digest), digest) != nil {
			return ErrInvalidContext
		}
	}
	for _, item := range input.GetTools() {
		if !plainSHA256(item.GetSha256()) || strings.Contains(item.GetName(), "/") ||
			verifyRegularFileSHA256(filepath.Join(root, ".mattercodex", "tools", item.GetSha256()), item.GetSha256()) != nil {
			return ErrInvalidContext
		}
	}
	return nil
}

func verifyRegularFileSHA256(path, expected string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximumFileBytes {
		return ErrInvalidContext
	}
	file, err := os.Open(path)
	if err != nil {
		return ErrInvalidContext
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, io.LimitReader(file, maximumFileBytes+1)); err != nil ||
		hex.EncodeToString(digest.Sum(nil)) != expected {
		return ErrInvalidContext
	}
	return nil
}

func supportedPackageManager(value string) bool {
	switch value {
	case "apk", "apt", "dnf", "pip", "npm":
		return true
	default:
		return false
	}
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

func phaseFromRawJSON(raw []byte) controlplanev1.ImageBuildStage {
	var event struct {
		Name     string `json:"name"`
		Vertexes []struct {
			Name string `json:"name"`
		} `json:"vertexes"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED
	}
	names := []string{event.Name}
	for _, vertex := range event.Vertexes {
		names = append(names, vertex.Name)
	}
	result := controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED
	for _, rawName := range names {
		name := strings.ToLower(rawName)
		switch {
		case strings.Contains(name, "exporting to image") || strings.Contains(name, "pushing layers"):
			return controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH
		case strings.Contains(name, "/run/mattercodex/install.sh"):
			result = controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION
		case strings.Contains(name, "load metadata for") && result == controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED:
			result = controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL
		case name != "" && result == controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED:
			result = controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING
		}
	}
	return result
}

func phaseProgress(stage controlplanev1.ImageBuildStage) Phase {
	percent := map[controlplanev1.ImageBuildStage]uint32{
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL:    35,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING:      45,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION: 60,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH: 80,
	}[stage]
	return Phase{Stage: stage, Percent: percent}
}

func failureForStage(stage controlplanev1.ImageBuildStage) Failure {
	switch stage {
	case controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL:
		return Failure{"BASE_PULL_FAILED", "BASE_RESOLUTION_REJECTED", "Trusted base pull failed"}
	case controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION:
		return Failure{"INSTALLATION_FAILED", "INSTALL_COMMAND_REJECTED", "Installation step failed"}
	case controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH:
		return Failure{"STAGING_PUSH_FAILED", "STAGING_EXPORT_REJECTED", "Staging export failed"}
	default:
		return Failure{"SOLVE_FAILED", "BUILD_GRAPH_REJECTED", "Build solve failed"}
	}
}

func validDNSName(value string) bool {
	return value != "" && !strings.ContainsAny(value, "*/:@ ") && strings.Contains(value, ".")
}

func validRepository(value string) bool {
	return len(value) >= 3 && len(value) <= 512 && strings.Contains(value, "/") &&
		!strings.ContainsAny(value, "@?# \r\n\t") && !strings.Contains(value, "://")
}

func plainSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

type ioDiscard struct{}

func (ioDiscard) Write(value []byte) (int, error) { return len(value), nil }
