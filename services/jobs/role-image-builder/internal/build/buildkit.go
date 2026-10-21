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
	// ErrBuildKit reports a failed readiness operation without exposing subprocess output.
	// ErrBuildKit сообщает о сбое проверки готовности, не раскрывая вывод дочернего процесса.
	ErrBuildKit = errors.New("BuildKit execution failed")
	// digestPattern accepts only the sha256 algorithm prefix and 64 lowercase hexadecimal digits.
	// digestPattern допускает только префикс алгоритма sha256 и 64 шестнадцатеричных символа нижнего регистра.
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const (
	// provenanceBindingSchema versions the local JSON binding hashed into BuildEvidence, not a signed attestation.
	// provenanceBindingSchema задаёт версию локальной JSON-привязки, хеш которой входит в BuildEvidence, а не подписанного свидетельства.
	provenanceBindingSchema = "kodex.dev/image-provenance-binding/v2"
	// expectedBuilderID identifies the builder in the requested attestation and local evidence binding.
	// expectedBuilderID обозначает сборщик в запрашиваемом свидетельстве и локальной привязке доказательств.
	expectedBuilderID = "spiffe://kodex.local/ns/kodex-system/sa/role-image-builder"
	// expectedBuildType identifies BuildKit's SLSA build definition in the local provenance binding.
	// expectedBuildType обозначает определение сборки SLSA от BuildKit в локальной привязке происхождения.
	expectedBuildType = "https://github.com/moby/buildkit/blob/master/docs/attestations/slsa-definitions.md"
	// provenanceAttestation requests minimal SLSA v1 provenance with the fixed builder identity; it does not verify the result.
	// provenanceAttestation запрашивает минимальное свидетельство SLSA v1 с фиксированной идентичностью сборщика; результат здесь не проверяется.
	provenanceAttestation = "mode=min,version=v1,builder-id=" + expectedBuilderID
)

// Config supplies buildctl, registry credentials, workspace paths and exact image/toolchain pins.
// Construction validates its shape and reads local credentials/catalog; remote readiness is a separate operation.
// Config задаёт buildctl, реквизиты реестра, пути рабочего каталога и точные привязки образов и инструментов.
// Конструктор проверяет форму и читает локальные реквизиты и каталог; удалённая готовность проверяется отдельно.
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

// Executor prepares pinned inputs and invokes BuildKit using one configured transport and base catalog.
// It neither grants ownership nor promotes an image; those decisions remain with the control plane.
// Executor подготавливает закреплённые входы и вызывает BuildKit с настроенным транспортом и каталогом баз.
// Он не выдаёт владение и не публикует допущенный образ: эти решения остаются за управляющим сервисом.
type Executor struct {
	config       Config
	materializer *Materializer
	allowedBases baseAllowlist
}

// Prepared holds one temporary build tree and the original input pointer until Close removes the tree.
// Callers must keep the input unchanged and finish users of these paths before closing it.
// Prepared хранит временное дерево сборки и исходный указатель на вход до удаления дерева методом Close.
// Вызывающая сторона обязана не менять вход и завершить использование путей до закрытия.
type Prepared struct {
	root, contextDirectory, dockerfile, installation string
	input                                            *controlplanev1.RoleImageBuildInput
}

// Phase pairs a recognized build stage with its fixed progress estimate, not measured completion.
// Phase связывает распознанный этап сборки с фиксированной оценкой прогресса, а не измеренной готовностью.
type Phase struct {
	Stage   controlplanev1.ImageBuildStage
	Percent uint32
}

// Failure carries closed error/diagnostic codes and a safe summary instead of raw build output.
// Failure переносит закрытые коды ошибки и диагностики с безопасным описанием вместо сырого вывода сборки.
type Failure struct {
	ErrorCode, DiagnosticCode, DiagnosticSummary string
}

// Error exposes only the stable error code through the error interface, leaving diagnostic fields separate.
// Error выдаёт через интерфейс error только устойчивый код ошибки, оставляя поля диагностики отдельными.
func (failure Failure) Error() string { return failure.ErrorCode }

// New rejects malformed configuration, constructs the credentialed input reader and loads exact allowed bases.
// It requires the trusted runtime tuple in that catalog but does not connect to BuildKit or build an image.
// New отклоняет некорректную конфигурацию, создаёт авторизованный читатель входов и загружает точные разрешённые базы.
// Он требует наличия пары доверенного runtime в каталоге, но не подключается к BuildKit и не собирает образ.
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

// Check probes the input registry and BuildKit workers, then builds a tiny pinned image into a local marker.
// It runs external operations under ctx, discards subprocess output and removes its temporary tree on return.
// Check проверяет реестр входов и исполнителей BuildKit, затем собирает небольшой закреплённый образ в локальный маркер.
// Внешние операции используют ctx, вывод процессов отбрасывается, а временное дерево удаляется при возврате.
func (executor *Executor) Check(ctx context.Context) error {
	if err := executor.materializer.Check(ctx); err != nil {
		return err
	}
	command := executor.buildctlCommand(ctx, "debug", "workers")
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
	command = executor.buildctlCommand(ctx, "build", "--frontend", "dockerfile.v0",
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

// buildctlCommand creates, but does not start, a context-bound command with the same explicit mTLS prefix.
// Each call owns fresh argument storage; environment, streams and process waiting remain caller responsibilities.
// buildctlCommand создаёт, но не запускает связанную с контекстом команду с одним явным префиксом mTLS.
// Каждый вызов владеет новым массивом аргументов; окружение, потоки и ожидание процесса задаёт вызывающая сторона.
func (executor *Executor) buildctlCommand(ctx context.Context, arguments ...string) *exec.Cmd {
	args := []string{"--addr", executor.config.Address, "--tlscacert", executor.config.CAFile,
		"--tlscert", executor.config.CertificateFile, "--tlskey", executor.config.PrivateKeyFile,
		"--tlsservername", executor.config.TLSServerName}
	return exec.CommandContext(ctx, executor.config.Binary, append(args, arguments...)...)
}

// buildKitReadinessDockerfile renders a pinned frontend/runtime check that exports only a readiness marker.
// The returned bytes do not themselves execute the check or prove remote availability.
// buildKitReadinessDockerfile формирует проверку закреплённых frontend и runtime с экспортом только маркера готовности.
// Возвращённые байты сами не запускают проверку и не доказывают доступность удалённого сервиса.
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

// buildKitReadinessOutput selects BuildKit's local exporter for the caller-created output directory.
// buildKitReadinessOutput выбирает локальный экспорт BuildKit в созданный вызывающей стороной каталог.
func buildKitReadinessOutput(directory string) string {
	return "type=local,dest=" + directory
}

// Prepare checks owner shape and configured pins, materializes and verifies inputs, then writes build files.
// The validation hook is delegated to the materializer; any failure removes the temporary tree, while success
// transfers cleanup to Prepared.Close. Structural owner checks do not establish caller authorization.
// Prepare проверяет форму владельца и настроенные привязки, получает и проверяет входы, затем пишет файлы сборки.
// Обработчик проверки передаётся материализатору; при ошибке временное дерево удаляется, а при успехе
// очистка переходит к Prepared.Close. Проверка формы владельца не устанавливает полномочия вызывающего.
func (executor *Executor) Prepare(
	ctx context.Context,
	input *controlplanev1.RoleImageBuildInput,
	beforeContextValidation func() error,
) (*Prepared, string, error) {
	reason := ""
	switch {
	case !validBuildOwner(input):
		reason = inputReasonOwnerScope
	case !plainSHA256(input.GetContextSha256()) || !plainSHA256(input.GetSourceSha256()) ||
		!plainSHA256(input.GetSpecSha256()) || !plainSHA256(input.GetImmutableBuildSha256()):
		reason = inputReasonSHASchema
	case input.GetFrontendSha256() != executor.config.ExpectedFrontendSHA256:
		reason = inputReasonFrontendPin
	case !digestPattern.MatchString(input.GetBaseImageDigest()):
		reason = inputReasonSHASchema
	case !executor.allowedBases.Allows(input.GetBaseImageReference(), input.GetBaseImageDigest()):
		reason = inputReasonBaseAllowlist
	case input.GetBuilderSha256() != executor.config.ExpectedBuilderSHA256:
		reason = inputReasonBuilderPin
	case input.GetToolchainSha256() != executor.config.ExpectedToolchainSHA256:
		reason = inputReasonToolchainPin
	case input.GetRoleRuntimeContractRevision() != executor.config.RoleRuntimeContractRevision ||
		input.GetRoleRuntimeContractSha256() != executor.config.RoleRuntimeContractSHA256:
		reason = inputReasonRuntimeContract
	case !strings.HasPrefix(input.GetContextRef(), "oci://"):
		reason = inputReasonContextRef
	case strings.ContainsAny(input.GetInstallationBlock(), "\x00\r") || !validOwnerDockerfile(input):
		reason = inputReasonOwnerDockerfile
	}
	if reason != "" {
		return nil, inputFetchRejectedDiagnostic, rejectInput(reason, ErrInvalidContext)
	}
	root, err := os.MkdirTemp(executor.config.WorkspaceRoot, "image-build-")
	if err != nil {
		return nil, inputFetchRejectedDiagnostic, rejectInput(inputReasonWorkspaceCreate, ErrInvalidContext)
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
		return nil, inputDigestMismatchDiagnostic, err
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

// Close recursively removes this prepared tree; it does not stop or join processes still using its paths.
// Close рекурсивно удаляет подготовленное дерево, но не останавливает и не ожидает процессы, использующие его пути.
func (prepared *Prepared) Close() error { return os.RemoveAll(prepared.root) }

// Build runs buildctl against prepared inputs, exports to staging and tracks the required phase sequence.
// Phase notifications are nonblocking and may be dropped; the process is waited for even after a progression error.
// Success returns the metadata digest and local provenance-binding hash, not independent attestation verification
// or image admission. The caller owns prepared-input lifetime, channel closure and the operation's time budget.
// Metadata is read in full before the one-MiB acceptance check; that check is not a bounded-memory read.
// Build запускает buildctl с подготовленными входами, экспортирует в промежуточный реестр и отслеживает обязательную последовательность этапов.
// Уведомления об этапах неблокирующие и могут теряться; ожидание процесса выполняется и при ошибке последовательности.
// Успех возвращает дайджест метаданных и хеш локальной привязки происхождения, а не независимую проверку свидетельства
// или допуск образа. Срок жизни входа, закрытие канала и бюджет времени операции задаёт вызывающая сторона.
// Метаданные читаются целиком до проверки лимита в один МиБ; эта проверка не ограничивает память чтения.
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
	args := []string{"build", "--frontend", "dockerfile.v0",
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
		"--opt", "attest:provenance=" + provenanceAttestation, "--progress=rawjson",
		"--output", "type=image,name=" + tag + ",push=true", "--metadata-file", metadataFile}
	command := executor.buildctlCommand(ctx, args...)
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

// buildProgressPipe discards stdout and allocates the stderr pipe consumed as raw JSON before Wait.
// It neither starts nor waits for the command; callers retain process and pipe lifecycle ownership.
// buildProgressPipe отбрасывает stdout и создаёт канал stderr для чтения сырого JSON до Wait.
// Он не запускает и не ожидает команду; жизненным циклом процесса и канала владеет вызывающая сторона.
func buildProgressPipe(command *exec.Cmd) (io.ReadCloser, error) {
	command.Stdout = io.Discard
	return command.StderrPipe()
}

// provenanceBindingSHA256 hashes a JSON binding of owner, input, policy, builder and supplied manifest digest.
// Only owner shape is checked here; this local hash neither validates all pins nor signs/verifies an attestation.
// provenanceBindingSHA256 хеширует JSON-привязку владельца, входа, политики, сборщика и переданного дайджеста манифеста.
// Здесь проверяется только форма владельца; локальный хеш не проверяет все привязки и не подписывает или проверяет свидетельство.
func provenanceBindingSHA256(input *controlplanev1.RoleImageBuildInput, manifestDigest string) (string, error) {
	if !validBuildOwner(input) {
		return "", ErrInvalidContext
	}
	document, err := json.Marshal(map[string]any{
		"scopeKind":       strings.TrimPrefix(input.GetScopeKind().String(), "RUNTIME_RESOURCE_SCOPE_KIND_"),
		"organizationRef": input.GetOrganizationRef(), "projectRef": input.GetProjectRef(),
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

// validBuildOwner requires a valid organization reference and either no project or a valid project reference.
// It validates scope structure only, without resolving ownership or permissions in the authoritative service.
// validBuildOwner требует корректную ссылку организации и отсутствие проекта либо корректную ссылку проекта.
// Он проверяет только структуру области, не разрешая владение или права в авторитетном сервисе.
func validBuildOwner(input *controlplanev1.RoleImageBuildInput) bool {
	if input == nil || !ownerReference(input.GetOrganizationRef(), "org_") {
		return false
	}
	switch input.GetScopeKind() {
	case controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION:
		return input.GetProjectRef() == ""
	case controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT:
		return ownerReference(input.GetProjectRef(), "prj_")
	default:
		return false
	}
}

// ownerReference checks the required prefix and an 8..88-byte ASCII alphanumeric, underscore or hyphen suffix.
// It performs no lookup and therefore does not prove that the referenced resource exists or belongs to a caller.
// ownerReference проверяет обязательный префикс и ASCII-суффикс длиной 8..88 байт из букв, цифр, подчёркиваний и дефисов.
// Поиск ресурса не выполняется, поэтому существование ссылки и принадлежность вызывающему не доказываются.
func ownerReference(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) < len(prefix)+8 || len(value) > len(prefix)+88 {
		return false
	}
	for _, character := range strings.TrimPrefix(value, prefix) {
		if character != '-' && character != '_' && (character < '0' || character > '9') && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return true
}

// dockerfile wraps previously validated owner source with pinned runtime restoration and a no-network inventory stage.
// The inventory reads the finalized rootfs through a read-only mount; only its manifest is copied into the final image.
// Rendering does not execute the instructions or grant admission, and must follow validOwnerDockerfile validation.
// dockerfile оборачивает ранее проверенный исходник владельца восстановлением закреплённого runtime и этапом инвентаризации без сети.
// Инвентаризация читает итоговую файловую систему через монтирование только для чтения; в финальный образ копируется лишь её манифест.
// Формирование текста не исполняет инструкции и не выдаёт допуск; до него требуется проверка validOwnerDockerfile.
func dockerfile(
	input *controlplanev1.RoleImageBuildInput,
	frontendRepository, trustedRuntimeRepository, trustedRuntimeDigest string,
) []byte {
	mounts := []string{
		"--mount=type=bind,target=/workspace/source,readonly",
		"--mount=type=bind,from=kodex-install,source=install.sh,target=/run/kodex/install.sh,readonly",
	}
	return []byte(fmt.Sprintf("# syntax=%s@sha256:%s\nFROM %s@%s AS trusted-runtime\n%s\nUSER root\nRUN %s /bin/sh /run/kodex/install.sh\nCOPY --from=trusted-runtime /usr/local/bin/kodex-init /usr/local/bin/kodex-init\nCOPY --from=trusted-runtime /usr/local/bin/kodex-agent-runner /usr/local/bin/kodex-agent-runner\nUSER 10001:10001\nENTRYPOINT [\"/usr/local/bin/kodex-init\",\"entrypoint\",\"/usr/local/bin/kodex-agent-runner\"]\nCMD [\"runtime-session\"]\nLABEL kodex.dev/spec-sha256=%q kodex.dev/runtime-contract-sha256=%q\nFROM kodex-final-rootfs AS kodex-tool-probe\nUSER root\nRUN --network=none --mount=type=bind,from=kodex-final-rootfs,source=/,target=/image,readonly [\"/usr/local/bin/kodex-agent-runner\",\"image-tool-inventory\",%q,%q,%q]\nFROM kodex-final-rootfs\nCOPY --from=kodex-tool-probe /tmp/kodex-tool-inventory.json /usr/share/kodex/tool-inventory.json\n",
		frontendRepository, input.GetFrontendSha256(), trustedRuntimeRepository, trustedRuntimeDigest,
		finalRootStage(input.GetDockerfile()), strings.Join(mounts, " "), input.GetSpecSha256(),
		input.GetRoleRuntimeContractSha256(), input.GetSpecSha256(), input.GetImmutableBuildSha256(), input.GetRoleRuntimeContractSha256()))
}

// finalRootStage retains the first FROM alias or assigns one, then appends the canonical finalized-rootfs stage.
// It assumes the caller already enforced the single-stage owner Dockerfile constraints and is not a general parser.
// finalRootStage сохраняет псевдоним первого FROM либо назначает его, затем добавляет канонический этап итоговой файловой системы.
// Предполагается предварительная проверка ограничений одноэтапного Dockerfile владельца; это не универсальный парсер.
func finalRootStage(source string) string {
	lines := strings.Split(strings.TrimSpace(source), "\n")
	alias := "kodex-user-rootfs"
	for index, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.EqualFold(fields[0], "FROM") {
			if len(fields) == 4 {
				alias = fields[3]
			} else {
				lines[index] = "FROM " + fields[1] + " AS " + alias
			}
			break
		}
	}
	return strings.Join(lines, "\n") + "\nFROM " + alias + " AS kodex-final-rootfs"
}

// validOwnerDockerfile bounds source to 256 KiB, rejects syntax overrides and requires one exact pinned FROM.
// Instructions before FROM and reserved aliases are rejected; later instructions are not generally sandboxed here.
// validOwnerDockerfile ограничивает исходник 256 КиБ, отклоняет переопределение синтаксиса и требует один точно закреплённый FROM.
// Инструкции до FROM и зарезервированные псевдонимы отклоняются; общей изоляции последующих инструкций здесь нет.
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
			if len(fields) == 4 && (strings.HasPrefix(strings.ToLower(fields[3]), "kodex-") || strings.EqualFold(fields[3], "trusted-runtime")) {
				return false
			}
			continue
		}
		if !foundFrom {
			return false
		}
	}
	return foundFrom
}

// installationScript renders manager-specific local package installs, tool copies and the owner's installation block.
// Paths are shell-quoted and the script uses set -eu; offline manager flags do not restrict arbitrary owner commands.
// installationScript формирует локальную установку пакетов по типу менеджера, копирование инструментов и установочный блок владельца.
// Пути экранируются для shell, скрипт использует set -eu; автономные флаги менеджеров не ограничивают произвольные команды владельца.
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

// verifyPinnedInputs checks supported package managers and the hashes of materialized package/tool files.
// Tool names containing a slash are rejected; full input authorization and name validation belong to upstream boundaries.
// verifyPinnedInputs проверяет поддерживаемые менеджеры пакетов и хеши полученных файлов пакетов и инструментов.
// Имена инструментов со слешем отклоняются; полная авторизация входа и проверка имён принадлежат вышестоящим границам.
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

// verifyRegularFileSHA256 rejects a non-regular/oversized Lstat result, then hashes at most maximumFileBytes+1 bytes.
// It compares the lowercase digest to expected; the separate Lstat/Open calls do not pin filesystem identity against races.
// verifyRegularFileSHA256 отклоняет не обычный или слишком большой файл по Lstat, затем хеширует не более maximumFileBytes+1 байт.
// Дайджест нижнего регистра сравнивается с expected; раздельные Lstat и Open не закрепляют идентичность файла против гонок.
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

// supportedPackageManager recognizes exactly the five managers for which installationScript emits local install commands.
// supportedPackageManager распознаёт ровно пять менеджеров, для которых installationScript формирует локальные команды установки.
func supportedPackageManager(value string) bool {
	switch value {
	case "apk", "apt", "dnf", "pip", "npm":
		return true
	default:
		return false
	}
}

// shellQuote encodes one POSIX shell word using single quotes and quote breaks; it does not validate its meaning.
// shellQuote кодирует одно слово POSIX shell одинарными кавычками с разрывами для кавычек внутри; смысл значения не проверяется.
func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// phaseFromRawJSON classifies event/vertex names into progress stages, prioritizing recognized image export names.
// Malformed or nameless events return unspecified; textual heuristics alone do not prove stage completion.
// phaseFromRawJSON классифицирует имена событий и вершин по этапам, отдавая приоритет распознанным именам экспорта образа.
// Некорректные события и события без имён дают неопределённый этап; текстовые эвристики сами не доказывают завершение этапа.
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

// phaseProgress assigns fixed percentages to the five recognized stages and zero to all other values.
// The original stage is preserved; these values are presentation estimates rather than measured work ratios.
// phaseProgress назначает фиксированные проценты пяти распознанным этапам и ноль всем остальным значениям.
// Исходный этап сохраняется; значения служат оценкой для интерфейса, а не измеренной долей работы.
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

// buildPhaseTracker holds a per-build sequence cursor and a solving event deferred until base pull is observed.
// It is mutated by one progress reader and is not a concurrent state store.
// buildPhaseTracker хранит курсор последовательности одной сборки и событие решения, отложенное до получения базового образа.
// Его меняет один читатель прогресса; это не конкурентное хранилище состояния.
type buildPhaseTracker struct {
	next           int
	pendingSolving bool
}

// buildPhaseSequence orders the five mandatory stages required before build metadata can be accepted.
// buildPhaseSequence упорядочивает пять обязательных этапов, необходимых до принятия метаданных сборки.
var buildPhaseSequence = [...]controlplanev1.ImageBuildStage{
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_BASE_PULL,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_SOLVING,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_INSTALLATION,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_TRUSTED_RUNTIME_FINALIZATION,
	controlplanev1.ImageBuildStage_IMAGE_BUILD_STAGE_STAGING_PUSH,
}

// newBuildPhaseTracker creates an empty cursor with no deferred solving event for a new build.
// newBuildPhaseTracker создаёт для новой сборки пустой курсор без отложенного события решения.
func newBuildPhaseTracker() *buildPhaseTracker { return &buildPhaseTracker{} }

// observe ignores unspecified/repeated stages, defers an early solving event and emits only the next expected phases.
// Any other out-of-order stage returns an error without advancing the cursor; this tracks observations, not remote state.
// observe игнорирует неопределённые и повторные этапы, откладывает раннее решение и выдаёт только следующие ожидаемые этапы.
// Любой иной нарушающий порядок этап даёт ошибку без продвижения курсора; отслеживаются наблюдения, а не удалённое состояние.
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

// complete reports whether every required phase was observed; successful process exit and metadata are checked separately.
// complete сообщает, наблюдались ли все обязательные этапы; успешный выход процесса и метаданные проверяются отдельно.
func (tracker *buildPhaseTracker) complete() bool { return tracker.next == len(buildPhaseSequence) }

// failureForStage maps the last stage to a closed failure tuple, using solve failure for unknown/unmapped stages.
// failureForStage сопоставляет последнему этапу закрытый набор полей ошибки; неизвестные и несопоставленные этапы дают сбой решения.
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

// validDNSName applies a minimal nonempty/dot/forbidden-character check for configured TLS server names.
// This is not full DNS grammar validation, resolution or destination authorization.
// validDNSName минимально проверяет наличие значения и точки и отсутствие запрещённых символов в настроенном TLS-имени сервера.
// Это не полная проверка грамматики DNS, разрешение имени или авторизация назначения.
func validDNSName(value string) bool {
	return value != "" && !strings.ContainsAny(value, "*/:@ ") && strings.Contains(value, ".")
}

// validRepository bounds a slash-containing repository string and rejects schemes and selected separators.
// It does not fully parse OCI references or authorize a registry destination.
// validRepository ограничивает длину строки реестра со слешем и отклоняет схемы и выбранные разделители.
// Полного разбора OCI-ссылок и авторизации назначения реестра он не выполняет.
func validRepository(value string) bool {
	return len(value) >= 3 && len(value) <= 512 && strings.Contains(value, "/") &&
		!strings.ContainsAny(value, "@?# \r\n\t") && !strings.Contains(value, "://")
}

// plainSHA256 accepts exactly 64 lowercase hexadecimal characters without an algorithm prefix.
// plainSHA256 допускает ровно 64 шестнадцатеричных символа нижнего регистра без префикса алгоритма.
func plainSHA256(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

// ioDiscard consumes readiness subprocess bytes without retaining or forwarding their contents.
// ioDiscard поглощает байты процесса проверки готовности, не сохраняя и не перенаправляя содержимое.
type ioDiscard struct{}

// Write acknowledges the entire buffer as consumed and never retains it or reports a write error.
// Write подтверждает поглощение всего буфера, не сохраняет его и не возвращает ошибку записи.
func (ioDiscard) Write(value []byte) (int, error) { return len(value), nil }
