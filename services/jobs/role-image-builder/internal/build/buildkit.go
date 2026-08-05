package build

import (
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
	ErrBuildKit       = errors.New("BuildKit execution failed")
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	registryHostRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*(?::[0-9]{1,5})?$`)
)

const (
	provenanceBindingSchema = "mattercodex.dev/image-provenance-binding/v1"
	expectedBuilderID       = "spiffe://mattercodex.local/ns/mattercodex-system/sa/role-image-builder"
	expectedBuildType       = "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md"
)

type Config struct {
	Binary, Address, TLSServerName, CAFile, CertificateFile, PrivateKeyFile string
	ContextRoot, SecretRoot, WorkspaceRoot                                  string
	BaseDockerConfig, StagingDockerConfig                                   string
	FrontendRepository, StagingRepository                                   string
	ExpectedBuilderSHA256, ExpectedToolchainSHA256                          string
}

type Executor struct{ config Config }

type Prepared struct {
	root, contextDirectory, dockerfile, installation, dockerConfig string
	input                                                          *controlplanev1.RoleImageBuildInput
}

func New(config Config) (*Executor, error) {
	if !filepath.IsAbs(config.Binary) || !filepath.IsAbs(config.ContextRoot) || !filepath.IsAbs(config.SecretRoot) ||
		!filepath.IsAbs(config.WorkspaceRoot) || !filepath.IsAbs(config.CAFile) || !filepath.IsAbs(config.CertificateFile) ||
		!filepath.IsAbs(config.PrivateKeyFile) || !filepath.IsAbs(config.BaseDockerConfig) || !filepath.IsAbs(config.StagingDockerConfig) ||
		!strings.HasPrefix(config.Address, "tcp://") || !validDNSName(config.TLSServerName) ||
		!validRepository(config.FrontendRepository) || !validRepository(config.StagingRepository) ||
		!plainSHA256(config.ExpectedBuilderSHA256) || !plainSHA256(config.ExpectedToolchainSHA256) {
		return nil, errors.New("role image builder BuildKit configuration is invalid")
	}
	return &Executor{config: config}, nil
}

func (executor *Executor) Check(ctx context.Context) error {
	command := exec.CommandContext(ctx, executor.config.Binary,
		"--addr", executor.config.Address, "--tlscacert", executor.config.CAFile,
		"--tlscert", executor.config.CertificateFile, "--tlskey", executor.config.PrivateKeyFile,
		"--tlsservername", executor.config.TLSServerName, "debug", "workers")
	command.Stdout, command.Stderr = ioDiscard{}, ioDiscard{}
	if err := command.Run(); err != nil {
		return ErrBuildKit
	}
	return nil
}

func (executor *Executor) Prepare(input *controlplanev1.RoleImageBuildInput) (*Prepared, error) {
	if input == nil || !plainSHA256(input.GetContextSha256()) || !plainSHA256(input.GetSourceSha256()) ||
		!plainSHA256(input.GetSpecSha256()) || !plainSHA256(input.GetImmutableBuildSha256()) ||
		!plainSHA256(input.GetFrontendSha256()) || !digestPattern.MatchString(input.GetBaseImageDigest()) ||
		input.GetBuilderSha256() != executor.config.ExpectedBuilderSHA256 ||
		input.GetToolchainSha256() != executor.config.ExpectedToolchainSHA256 ||
		!strings.HasPrefix(input.GetContextRef(), "oci://") ||
		!strings.HasSuffix(input.GetContextRef(), "@sha256:"+input.GetContextSha256()) ||
		strings.ContainsAny(input.GetInstallationBlock(), "\x00\r") {
		return nil, ErrInvalidContext
	}
	root, err := os.MkdirTemp(executor.config.WorkspaceRoot, "image-build-")
	if err != nil {
		return nil, ErrInvalidContext
	}
	prepared := &Prepared{root: root, contextDirectory: filepath.Join(root, "context"),
		dockerfile: filepath.Join(root, "dockerfile"), installation: filepath.Join(root, "installation"),
		dockerConfig: filepath.Join(root, "docker"), input: input}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()
	if err := os.MkdirAll(prepared.contextDirectory, 0o700); err != nil {
		return nil, ErrInvalidContext
	}
	archive := filepath.Join(executor.config.ContextRoot, input.GetContextSha256()+".tar")
	if err := ExtractContext(archive, prepared.contextDirectory, input.GetContextSha256(), input.GetSourceSha256()); err != nil {
		return nil, err
	}
	if err := verifyPinnedInputs(prepared.contextDirectory, input); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(prepared.dockerfile, 0o700); err != nil {
		return nil, ErrInvalidContext
	}
	if err := os.WriteFile(filepath.Join(prepared.dockerfile, "Dockerfile"), dockerfile(input, executor.config.FrontendRepository), 0o600); err != nil {
		return nil, ErrInvalidContext
	}
	if err := os.MkdirAll(prepared.installation, 0o700); err != nil {
		return nil, ErrInvalidContext
	}
	if err := os.WriteFile(filepath.Join(prepared.installation, "install.sh"), installationScript(input), 0o600); err != nil {
		return nil, ErrInvalidContext
	}
	if err := executor.materializeDockerConfig(prepared.dockerConfig, input.GetBaseImageReference()); err != nil {
		return nil, err
	}
	cleanup = false
	return prepared, nil
}

func (prepared *Prepared) Close() error { return os.RemoveAll(prepared.root) }

func (executor *Executor) Build(ctx context.Context, prepared *Prepared, attempt uint32) (controlclient.BuildEvidence, error) {
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
		"--opt", "attest:provenance=mode=min,builder-id=" + expectedBuilderID,
		"--output", "type=image,name=" + tag + ",push=true", "--metadata-file", metadataFile}
	for index, reference := range input.GetBuildSecretRefs() {
		digest := sha256.Sum256([]byte(reference))
		path := filepath.Join(executor.config.SecretRoot, hex.EncodeToString(digest[:]))
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			return controlclient.BuildEvidence{}, ErrInvalidContext
		}
		args = append(args, "--secret", fmt.Sprintf("id=secret-%02d,src=%s", index, path))
	}
	command := exec.CommandContext(ctx, executor.config.Binary, args...)
	command.Env = append(os.Environ(), "DOCKER_CONFIG="+prepared.dockerConfig, "HOME="+prepared.root)
	command.Stdout, command.Stderr = ioDiscard{}, ioDiscard{}
	if err := command.Run(); err != nil {
		return controlclient.BuildEvidence{}, ErrBuildKit
	}
	metadata, err := os.ReadFile(metadataFile)
	if err != nil || len(metadata) == 0 || len(metadata) > 1<<20 {
		return controlclient.BuildEvidence{}, ErrBuildKit
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(metadata, &document) != nil {
		return controlclient.BuildEvidence{}, ErrBuildKit
	}
	var digest string
	if json.Unmarshal(document["containerimage.digest"], &digest) != nil || !digestPattern.MatchString(digest) {
		return controlclient.BuildEvidence{}, ErrBuildKit
	}
	provenanceSHA256, err := provenanceBindingSHA256(input, digest)
	if err != nil {
		return controlclient.BuildEvidence{}, ErrBuildKit
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
	for index, reference := range input.GetBuildSecretRefs() {
		digest := sha256.Sum256([]byte(reference))
		mounts = append(mounts, fmt.Sprintf("--mount=type=secret,id=secret-%02d,target=/run/secrets/mattercodex/%s", index, hex.EncodeToString(digest[:8])))
	}
	return []byte(fmt.Sprintf("# syntax=%s@sha256:%s\nFROM %s@%s\nRUN %s /bin/sh /run/mattercodex/install.sh\nLABEL mattercodex.dev/spec-sha256=%q\n",
		frontendRepository, input.GetFrontendSha256(), input.GetBaseImageReference(), input.GetBaseImageDigest(),
		strings.Join(mounts, " "), input.GetSpecSha256()))
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

func (executor *Executor) materializeDockerConfig(directory, baseReference string) error {
	baseHost := strings.SplitN(baseReference, "/", 2)[0]
	frontendHost := strings.SplitN(executor.config.FrontendRepository, "/", 2)[0]
	stagingHost := strings.SplitN(executor.config.StagingRepository, "/", 2)[0]
	if !registryHostRegex.MatchString(baseHost) || !registryHostRegex.MatchString(frontendHost) ||
		!registryHostRegex.MatchString(stagingHost) || baseHost != frontendHost || baseHost == stagingHost {
		return errors.New("base pull and staging push registry scopes are invalid")
	}
	baseAuth, err := singleRegistryAuth(executor.config.BaseDockerConfig, baseHost)
	if err != nil {
		return err
	}
	stagingAuth, err := singleRegistryAuth(executor.config.StagingDockerConfig, stagingHost)
	if err != nil {
		return err
	}
	document := map[string]any{"auths": map[string]json.RawMessage{baseHost: baseAuth, stagingHost: stagingAuth}}
	raw, err := json.Marshal(document)
	if err != nil {
		return errors.New("encode bounded registry credentials")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errors.New("create Docker credential directory")
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), raw, 0o600); err != nil {
		return errors.New("write bounded registry credentials")
	}
	return nil
}

func singleRegistryAuth(path, expectedHost string) (json.RawMessage, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 64<<10 {
		return nil, errors.New("read bounded registry credential")
	}
	var document struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if json.Unmarshal(raw, &document) != nil || len(document.Auths) != 1 || len(document.Auths[expectedHost]) == 0 {
		return nil, errors.New("registry credential scope is invalid")
	}
	return document.Auths[expectedHost], nil
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
