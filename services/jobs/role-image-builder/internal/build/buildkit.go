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

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	controlclient "github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/clients/controlplane"
)

var (
	ErrBuildKit   = errors.New("BuildKit execution failed")
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const (
	provenanceBindingSchema = "kodex.dev/image-provenance-binding/v1"
	expectedBuilderID       = "spiffe://kodex.local/ns/kodex-system/sa/role-image-builder"
	expectedBuildType       = "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md"
)

type Config struct {
	Binary, Address, TLSServerName, CAFile, CertificateFile, PrivateKeyFile string
	InputDockerConfig, BuildKitPullDockerConfig, WorkspaceRoot              string
	InputRegistryTLSServerName, InputRegistryCAFile                         string
	InputRegistryCertificateFile, InputRegistryPrivateKeyFile               string
	AllowedRoleBaseImagesFile                                               string
	InputRepository, TrustedRoleBaseRepository, TrustedRoleBaseDigest       string
	FrontendRepository, StagingRepository                                   string
	ExpectedBuilderSHA256, ExpectedFrontendSHA256, ExpectedToolchainSHA256  string
	RoleRuntimeContractSHA256                                               string
	RoleRuntimeContractRevision                                             uint64
}

type Executor struct {
	config       Config
	materializer *Materializer
	allowedBases baseAllowlist
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
		!filepath.IsAbs(config.BuildKitPullDockerConfig) ||
		!filepath.IsAbs(config.AllowedRoleBaseImagesFile) ||
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
	allowedBases, err := loadBaseAllowlist(config.AllowedRoleBaseImagesFile)
	if err != nil {
		return nil, err
	}
	if !allowedBases.Allows(config.TrustedRoleBaseRepository, config.TrustedRoleBaseDigest) {
		return nil, errors.New("trusted runtime base is absent from role environment catalog")
	}
	return &Executor{config: config, materializer: materializer, allowedBases: allowedBases}, nil
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
	outputDirectory := filepath.Join(root, "output")
	if err := os.Mkdir(outputDirectory, 0o700); err != nil {
		return ErrBuildKit
	}
	dockerfile := buildKitReadinessDockerfile(
		executor.config.FrontendRepository, executor.config.ExpectedFrontendSHA256,
		executor.config.TrustedRoleBaseRepository, executor.config.TrustedRoleBaseDigest,
	)
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), dockerfile, 0o600); err != nil {
		return ErrBuildKit
	}
	command = exec.CommandContext(ctx, executor.config.Binary,
		"--addr", executor.config.Address, "--tlscacert", executor.config.CAFile,
		"--tlscert", executor.config.CertificateFile, "--tlskey", executor.config.PrivateKeyFile,
		"--tlsservername", executor.config.TLSServerName, "build", "--frontend", "dockerfile.v0",
		"--local", "context="+root, "--local", "dockerfile="+root, "--opt", "filename=Dockerfile",
		"--opt", "platform=linux/amd64", "--output", buildKitReadinessOutput(outputDirectory), "--no-cache")
	command.Env = append(os.Environ(), "DOCKER_CONFIG="+filepath.Dir(executor.config.BuildKitPullDockerConfig), "HOME="+root)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		return ErrBuildKit
	}
	marker, err := os.ReadFile(filepath.Join(outputDirectory, "kodex-readiness"))
	if err != nil || string(marker) != "ready" {
		return ErrBuildKit
	}
	return nil
}

func buildKitReadinessDockerfile(frontendRepository, frontendSHA256, baseRepository, baseDigest string) []byte {
	return []byte(fmt.Sprintf(
		"# syntax=%s@sha256:%s\nFROM %s@%s AS verify\n"+
			"RUN [\"/bin/sh\",\"-c\",\"test -x /usr/local/bin/kodex-init && "+
			"test -x /usr/local/bin/kodex-agent-runner && printf ready > /tmp/kodex-readiness\"]\n"+
			"FROM scratch\nCOPY --from=verify /tmp/kodex-readiness /kodex-readiness\n",
		frontendRepository,
		frontendSHA256,
		baseRepository,
		baseDigest,
	))
}

func buildKitReadinessOutput(directory string) string {
	return "type=local,dest=" + directory
}

func (executor *Executor) Prepare(
	ctx context.Context,
	input *controlplanev1.RoleImageBuildInput,
	beforeContextValidation func() error,
) (*Prepared, string, error) {
	if input == nil || !plainSHA256(input.GetContextSha256()) || !plainSHA256(input.GetSourceSha256()) ||
		!plainSHA256(input.GetSpecSha256()) || !plainSHA256(input.GetImmutableBuildSha256()) ||
		input.GetFrontendSha256() != executor.config.ExpectedFrontendSHA256 || !digestPattern.MatchString(input.GetBaseImageDigest()) ||
		!executor.allowedBases.Allows(input.GetBaseImageReference(), input.GetBaseImageDigest()) ||
		input.GetBuilderSha256() != executor.config.ExpectedBuilderSHA256 ||
		input.GetToolchainSha256() != executor.config.ExpectedToolchainSHA256 ||
		input.GetRoleRuntimeContractRevision() != executor.config.RoleRuntimeContractRevision ||
		input.GetRoleRuntimeContractSha256() != executor.config.RoleRuntimeContractSHA256 ||
		!strings.HasPrefix(input.GetContextRef(), "oci://") ||
		strings.ContainsAny(input.GetInstallationBlock(), "\x00\r") || !validOwnerDockerfile(input) {
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
	if err := os.WriteFile(filepath.Join(prepared.dockerfile, "Dockerfile"), dockerfile(
		input, executor.config.FrontendRepository, executor.config.TrustedRoleBaseRepository,
		executor.config.TrustedRoleBaseDigest,
	), 0o600); err != nil {
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
		"--local", "kodex-install=" + prepared.installation,
		"--opt", "context:kodex-install=local:kodex-install",
		"--opt", "filename=Dockerfile", "--opt", "platform=" + strings.Join(platforms, ","),
		"--opt", "label:kodex.dev/spec-sha256=" + input.GetSpecSha256(),
		"--opt", "label:kodex.dev/source-sha256=" + input.GetSourceSha256(),
		"--opt", "label:kodex.dev/context-sha256=" + input.GetContextSha256(),
		"--opt", "label:kodex.dev/base-image-digest=" + input.GetBaseImageDigest(),
		"--opt", "label:kodex.dev/builder-sha256=" + input.GetBuilderSha256(),
		"--opt", "label:kodex.dev/frontend-sha256=" + input.GetFrontendSha256(),
		"--opt", "label:kodex.dev/toolchain-sha256=" + input.GetToolchainSha256(),
		"--opt", "label:kodex.dev/immutable-build-sha256=" + input.GetImmutableBuildSha256(),
		"--opt", fmt.Sprintf("label:kodex.dev/policy-revision=%d", input.GetPolicyRevision()),
		"--opt", "label:kodex.dev/policy-sha256=" + input.GetPolicySha256(),
		"--opt", "attest:provenance=mode=min,builder-id=" + expectedBuilderID, "--progress=rawjson",
		"--output", "type=image,name=" + tag + ",push=true", "--metadata-file", metadataFile}
	command := exec.CommandContext(ctx, executor.config.Binary, args...)
	command.Env = append(os.Environ(), "DOCKER_CONFIG="+filepath.Dir(executor.config.BuildKitPullDockerConfig), "HOME="+prepared.root)
	progress, err := buildProgressPipe(command)
	if err != nil {
		return controlclient.BuildEvidence{}, Failure{"SOLVE_FAILED", "BUILD_GRAPH_REJECTED", "Build graph was rejected"}
	}
	if err := command.Start(); err != nil {
		return controlclient.BuildEvidence{}, Failure{"SOLVE_FAILED", "BUILD_GRAPH_REJECTED", "Build graph did not start"}
	}
	tracker := newBuildPhaseTracker()
	lastStage := controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL
	var progressionErr error
	scanner := bufio.NewScanner(progress)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for scanner.Scan() {
		if progressionErr != nil {
			continue
		}
		progression, err := tracker.observe(phaseFromRawJSON(scanner.Bytes()))
		if err != nil {
			progressionErr = err
			continue
		}
		for _, phase := range progression {
			lastStage = phase.Stage
			select {
			case phases <- phase:
			default:
			}
		}
	}
	waitErr := command.Wait()
	if progressionErr != nil {
		return controlclient.BuildEvidence{}, Failure{"SOLVE_FAILED", "BUILD_GRAPH_REJECTED", "Build phase progression was rejected"}
	}
	if scanner.Err() != nil || waitErr != nil {
		return controlclient.BuildEvidence{}, failureForStage(lastStage)
	}
	if !tracker.complete() {
		return controlclient.BuildEvidence{}, Failure{"SOLVE_FAILED", "BUILD_GRAPH_REJECTED", "Build phase progression was incomplete"}
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

func buildProgressPipe(command *exec.Cmd) (io.ReadCloser, error) {
	command.Stdout = io.Discard
	return command.StderrPipe()
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

func dockerfile(
	input *controlplanev1.RoleImageBuildInput,
	frontendRepository, trustedRuntimeRepository, trustedRuntimeDigest string,
) []byte {
	mounts := []string{
		"--mount=type=bind,target=/workspace/source,readonly",
		"--mount=type=bind,from=kodex-install,source=install.sh,target=/run/kodex/install.sh,readonly",
	}
	return []byte(fmt.Sprintf("# syntax=%s@sha256:%s\nFROM %s@%s AS trusted-runtime\n%s\nUSER root\nRUN %s /bin/sh /run/kodex/install.sh\nCOPY --from=trusted-runtime /usr/local/bin/kodex-init /usr/local/bin/kodex-init\nCOPY --from=trusted-runtime /usr/local/bin/kodex-agent-runner /usr/local/bin/kodex-agent-runner\nUSER 10001:10001\nENTRYPOINT [\"/usr/local/bin/kodex-init\",\"entrypoint\",\"/usr/local/bin/kodex-agent-runner\"]\nCMD [\"runtime-session\"]\nLABEL kodex.dev/spec-sha256=%q kodex.dev/runtime-contract-sha256=%q\n",
		frontendRepository, input.GetFrontendSha256(), trustedRuntimeRepository, trustedRuntimeDigest,
		strings.TrimSpace(input.GetDockerfile()), strings.Join(mounts, " "), input.GetSpecSha256(),
		input.GetRoleRuntimeContractSha256()))
}

func validOwnerDockerfile(input *controlplanev1.RoleImageBuildInput) bool {
	dockerfile := input.GetDockerfile()
	if dockerfile == "" || len(dockerfile) > 256<<10 || strings.ContainsAny(dockerfile, "\x00\r") {
		return false
	}
	expectedBase := input.GetBaseImageReference() + "@" + input.GetBaseImageDigest()
	foundFrom := false
	for _, rawLine := range strings.Split(dockerfile, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "# syntax=") || strings.HasPrefix(lower, "#syntax=") {
			return false
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], "FROM") {
			if foundFrom || len(fields) != 2 && len(fields) != 4 || fields[1] != expectedBase ||
				len(fields) == 4 && !strings.EqualFold(fields[2], "AS") || strings.HasSuffix(line, "\\") {
				return false
			}
			foundFrom = true
			continue
		}
		if !foundFrom {
			return false
		}
	}
	return foundFrom
}

func installationScript(input *controlplanev1.RoleImageBuildInput) []byte {
	var script strings.Builder
	script.WriteString("set -eu\n")
	for _, item := range input.GetPackages() {
		path := "/workspace/source/.kodex/packages/" + strings.TrimPrefix(item.GetDigest(), "sha256:")
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
		source := "/workspace/source/.kodex/tools/" + item.GetSha256()
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
			verifyRegularFileSHA256(filepath.Join(root, ".kodex", "packages", digest), digest) != nil {
			return ErrInvalidContext
		}
	}
	for _, item := range input.GetTools() {
		if !plainSHA256(item.GetSha256()) || strings.Contains(item.GetName(), "/") ||
			verifyRegularFileSHA256(filepath.Join(root, ".kodex", "tools", item.GetSha256()), item.GetSha256()) != nil {
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
		case strings.Contains(name, "/run/kodex/install.sh"):
			result = controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION
		case strings.Contains(name, "copy --from=trusted-runtime"):
			result = controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION
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
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL:                    35,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING:                      45,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION:                 60,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION: 70,
		controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH:                 80,
	}[stage]
	return Phase{Stage: stage, Percent: percent}
}

type buildPhaseTracker struct {
	next           int
	pendingSolving bool
}

var buildPhaseSequence = [...]controlplanev1.ImageBuildStage{
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH,
}

func newBuildPhaseTracker() *buildPhaseTracker { return &buildPhaseTracker{} }

func (tracker *buildPhaseTracker) observe(stage controlplanev1.ImageBuildStage) ([]Phase, error) {
	if stage == controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_UNSPECIFIED {
		return nil, nil
	}
	if stage == controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING && tracker.next == 0 {
		tracker.pendingSolving = true
		return nil, nil
	}
	for index := 0; index < tracker.next; index++ {
		if buildPhaseSequence[index] == stage {
			return nil, nil
		}
	}
	if tracker.next >= len(buildPhaseSequence) || buildPhaseSequence[tracker.next] != stage {
		return nil, errors.New("BuildKit phase progression regressed")
	}
	progression := []Phase{phaseProgress(stage)}
	tracker.next++
	if stage == controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL && tracker.pendingSolving {
		progression = append(progression, phaseProgress(controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING))
		tracker.next++
		tracker.pendingSolving = false
	}
	return progression, nil
}

func (tracker *buildPhaseTracker) complete() bool { return tracker.next == len(buildPhaseSequence) }

func failureForStage(stage controlplanev1.ImageBuildStage) Failure {
	switch stage {
	case controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL:
		return Failure{"BASE_PULL_FAILED", "BASE_RESOLUTION_REJECTED", "Trusted base pull failed"}
	case controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION:
		return Failure{"INSTALLATION_FAILED", "INSTALL_COMMAND_REJECTED", "Installation step failed"}
	case controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION:
		return Failure{"RUNTIME_FINALIZATION_FAILED", "RUNTIME_FINALIZATION_REJECTED", "Trusted runtime finalization failed"}
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
