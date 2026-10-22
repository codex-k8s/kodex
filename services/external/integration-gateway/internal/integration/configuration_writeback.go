package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"google.golang.org/protobuf/proto"
)

// configurationWriteBackError stores a closed owner-report failure class without keeping provider payloads, credentials or raw Git diagnostics.
// RU: configurationWriteBackError хранит закрытый класс ошибки owner-отчёта без provider payload, credentials и сырой Git-диагностики.
type configurationWriteBackError struct {
	failure cp.ManagedConfigurationGitWriteBackFailure
}

// Error returns the same safe diagnostic for every internally classified writeback failure.
// RU: Error возвращает одну безопасную диагностику для любого внутреннего класса ошибки записи.
func (*configurationWriteBackError) Error() string { return "configuration writeback failed" }

// writeBackFailure wraps an already selected owner failure enum without incorporating an external error string.
// RU: writeBackFailure оборачивает уже выбранный owner failure enum без включения внешней строки ошибки.
func writeBackFailure(code cp.ManagedConfigurationGitWriteBackFailure) error {
	return &configurationWriteBackError{failure: code}
}

// ConfigurationWriteBackFailure unwraps a classified writeback error, distinguishes deadline expiry and maps every remaining error to unavailable.
// RU: ConfigurationWriteBackFailure извлекает классифицированную writeback-ошибку, выделяет истечение срока и переводит остальные ошибки в unavailable.
func ConfigurationWriteBackFailure(err error) cp.ManagedConfigurationGitWriteBackFailure {
	var safe *configurationWriteBackError
	if errors.As(err, &safe) {
		return safe.failure
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_DEADLINE_EXCEEDED
	}
	return cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_UNAVAILABLE
}

// ConfigurationWriteBackCandidate pins the prepared commit, tree, replacement blob and original blob for owner begin and later provider readback.
// RU: ConfigurationWriteBackCandidate закрепляет подготовленные commit, tree, новый и исходный blob для owner begin и последующего provider readback.
type ConfigurationWriteBackCandidate struct{ CommitSHA, TreeSHA, BlobSHA, BaseBlobSHA string }

// ConfigurationWriteBackPullRequest holds the provider-validated numeric reference and exact canonical URL of a matching pull or merge request.
// RU: ConfigurationWriteBackPullRequest хранит проверенные числовую provider-ссылку и точный канонический URL совпавшего pull либо merge request.
type ConfigurationWriteBackPullRequest struct{ Ref, URL string }

// ConfigurationWriteBackExecution owns a cloned server work snapshot, loaded credential, lazy private Git workspace and prepared candidate for one application-managed execution.
// RU: ConfigurationWriteBackExecution владеет копией серверной работы, credential, отложенным приватным Git workspace и кандидатом одного исполнения под управлением приложения.
// It validates provider operations but does not issue approval or persist owner lifecycle transitions; the caller must sequence begin, effect, readback and completion.
// RU: Объект проверяет provider-операции, но не выдаёт одобрение и не сохраняет owner-переходы; вызывающий обязан упорядочить begin, effect, readback и completion.
type ConfigurationWriteBackExecution struct {
	adapter       *Adapter
	work          *cp.ManagedConfigurationGitWriteBackWork
	definition    integrationpackage.Package
	configuration map[string]string
	credential    []byte
	git           *writeBackGitWorkspace
	remote        string
	deadline      time.Time
	candidate     *ConfigurationWriteBackCandidate
}

// Close best-effort removes the private Git directory and clears loaded credential and cloned proposed-content bytes after an opened execution.
// RU: Close пытается удалить приватный Git-каталог и очищает загруженные credential-байты и копию proposed content после открытого исполнения.
// Removal errors are not returned, and neither filesystem secure erasure nor clearing previously created string copies is guaranteed.
// RU: Ошибки удаления не возвращаются; безопасное стирание файловой системы и очистка ранее созданных строковых копий не гарантируются.
func (execution *ConfigurationWriteBackExecution) Close() {
	if execution.git != nil {
		execution.git.close()
	}
	clear(execution.credential)
	clear(execution.work.ProposedContent)
}

// OpenConfigurationWriteBack checks exact proposal/lease/revision/content/author bindings, fixed proposal-branch semantics and supported execute or read-only recovery state.
// RU: OpenConfigurationWriteBack проверяет точные proposal/lease/revision/content/author привязки, фиксированную proposal-ветку и допустимое execute либо read-only recovery состояние.
// GitHub additionally requires its Git destination; each write capability must require HUMAN_EACH_EFFECT, while the source read must remain READ/NONE.
// RU: GitHub дополнительно требует Git destination; каждая write capability обязана требовать HUMAN_EACH_EFFECT, а чтение источника — оставаться READ/NONE.
// After bounding work by lease and capability deadlines, it loads credentials and clones the snapshot without pushing, creating a PR or granting approval.
// RU: После ограничения срока работой, арендой и capability функция загружает credential и клонирует снимок без push, создания PR и выдачи одобрения.
// Approval metadata is consistency evidence from the owner claim, not independent authorization of a caller-created proposal.
// RU: Метаданные одобрения являются доказательством согласованности owner claim, а не самостоятельной авторизацией созданного вызывающим предложения.
func (adapter *Adapter) OpenConfigurationWriteBack(ctx context.Context, work *cp.ManagedConfigurationGitWriteBackWork) (*ConfigurationWriteBackExecution, error) {
	invalid := func() (*ConfigurationWriteBackExecution, error) {
		return nil, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_RESPONSE_INVALID)
	}
	if work == nil || len(work.ProtoReflect().GetUnknown()) != 0 || work.GetLease() == nil || work.GetProposal() == nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return invalid()
	}
	lease, p := work.GetLease(), work.GetProposal()
	if len(lease.ProtoReflect().GetUnknown()) != 0 || len(p.ProtoReflect().GetUnknown()) != 0 || lease.GetProposalRef() != p.GetRef() || lease.GetAttempt() < 1 || lease.GetClaimGeneration() < 1 || lease.GetClaimant() == "" || lease.GetFence() == "" || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || !lease.GetExpiresAt().AsTime().After(time.Now()) || !work.GetDeadline().AsTime().After(time.Now()) {
		return invalid()
	}
	if p.GetVersion() < 1 || p.GetConfigurationVersion() < 1 || p.GetSourceVersion() < 1 || p.GetConnectionVersion() < 1 || p.GetConfigurationRef() == "" || p.GetSourceRef() == "" || p.GetConnectionRef() == "" || p.GetApprovalDigest() == "" || p.GetApprovedAt() == nil || p.GetApprovedAt().CheckValid() != nil || p.GetCreatedAt() == nil || p.GetCreatedAt().CheckValid() != nil {
		return invalid()
	}
	if p.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE && p.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION {
		return invalid()
	}
	if p.GetContentFormat() != "JSON" && p.GetContentFormat() != "YAML" || !sourceCommitPattern.MatchString(p.GetBaseCommitSha()) || !validRepositoryPath(p.GetPath(), false) || len(p.GetPath()) > 512 || p.GetProposalBranch() != "kodex/writeback/"+p.GetRef() || !validWriteBackRef(p.GetSourceRefName()) || !validWriteBackRef(p.GetProposalBranch()) || p.GetProposalBranch() == p.GetSourceRefName() {
		return invalid()
	}
	if len(work.GetProposedContent()) == 0 || len(work.GetProposedContent()) > maximumSourceContentBytes || sourceContentDigest(work.GetProposedContent()) != p.GetProposedContentSha256() || len(p.GetBaseContentSha256()) != 64 || work.GetEffectMarker() != "kodex-configuration-writeback:"+p.GetRef() || work.GetCommitMessage() != "Update managed configuration" || work.GetCommitAuthorName() != "Kodex" || work.GetCommitAuthorEmail() != "configuration@kodex.invalid" || work.GetCommitTime() == nil || work.GetCommitTime().CheckValid() != nil || !work.GetCommitTime().AsTime().Equal(p.GetCreatedAt().AsTime()) {
		return invalid()
	}
	if work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE && work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY {
		return invalid()
	}
	if work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE && p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_CLAIMED || work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY && p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME {
		return invalid()
	}
	if work.GetEffect() != cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_BRANCH && work.GetEffect() != cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST {
		return invalid()
	}
	if work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY && (work.GetEffectStartedAt() == nil || work.GetEffectStartedAt().CheckValid() != nil || !sourceCommitPattern.MatchString(p.GetCandidateCommitSha())) {
		return invalid()
	}
	request := Request{DefinitionPackage: work.GetDefinitionPackage(), DefinitionKey: work.GetDefinitionKey(), DefinitionVersion: work.GetDefinitionVersion(), DefinitionDigest: work.GetDefinitionDigest(), ConnectionRef: p.GetConnectionRef(), Credential: credentialFromProto(work.GetCredentialRevision())}
	definition, err := adapter.validateDefinition(request)
	if err != nil || work.GetPublicConfiguration() == nil {
		return invalid()
	}
	configuration, err := normalizeStringMap(work.GetPublicConfiguration().AsMap())
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return invalid()
	}
	remote := ""
	operations := []string{}
	switch definition.Spec.Adapter {
	case "GITHUB":
		if p.GetRepositoryRef() != configuration["owner"]+"/"+configuration["repository"] || !definition.HasNetworkDestination(integrationpackage.NetworkDestination{Key: "github_git", Source: "STATIC", Hostname: "github.com", Port: 443, TLS: "REQUIRED"}) {
			return invalid()
		}
		remote = "https://github.com/" + url.PathEscape(configuration["owner"]) + "/" + url.PathEscape(configuration["repository"]) + ".git"
		operations = []string{"github.branch.create", "github.repository.content.update", "github.pull_request.create"}
	case "GITLAB":
		base, err := parseProviderBaseURL(configuration["base_url"])
		if err != nil || p.GetRepositoryRef() != configuration["project_path"] {
			return invalid()
		}
		base.Path = strings.TrimSuffix(base.Path, "/") + "/" + configuration["project_path"] + ".git"
		remote = base.String()
		operations = []string{"gitlab.branch.create", "gitlab.commit.create", "gitlab.merge_request.create"}
	default:
		return invalid()
	}
	deadline := work.GetDeadline().AsTime()
	if lease.GetExpiresAt().AsTime().Before(deadline) {
		deadline = lease.GetExpiresAt().AsTime()
	}
	for _, operation := range operations {
		capability, ok := definition.Capability(operation)
		if !ok || capability.Operation != operation || capability.Risk == "READ" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
			return invalid()
		}
		if selected := time.Now().Add(time.Duration(capability.Execution.TimeoutSeconds) * time.Second); selected.Before(deadline) {
			deadline = selected
		}
	}
	readOperation := "github.repository.content.read"
	if definition.Spec.Adapter == "GITLAB" {
		readOperation = "gitlab.repository.file.read"
	}
	readCapability, ok := definition.Capability(readOperation)
	if !ok || readCapability.Risk != "READ" || readCapability.ApprovalPolicy != "NONE" {
		return invalid()
	}
	if selected := time.Now().Add(time.Duration(readCapability.Execution.TimeoutSeconds) * time.Second); selected.Before(deadline) {
		deadline = selected
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	credential, err := adapter.readCredential(ctx, request.Credential)
	if err != nil {
		return nil, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_CREDENTIAL_REJECTED)
	}
	return &ConfigurationWriteBackExecution{adapter: adapter, work: proto.Clone(work).(*cp.ManagedConfigurationGitWriteBackWork), definition: definition, configuration: configuration, credential: credential, remote: remote, deadline: deadline}, nil
}

// validWriteBackRef applies the existing bounded branch/ref syntax restrictions before passing names to fixed Git operations; it is not a general Git ref parser.
// RU: validWriteBackRef применяет существующие ограничения длины и синтаксиса ветки/ref до передачи фиксированным Git-операциям, не являясь общим Git ref parser.
func validWriteBackRef(ref string) bool {
	return ref != "" && len(ref) <= 256 && !strings.HasPrefix(ref, "-") && !strings.HasPrefix(ref, "/") && !strings.HasSuffix(ref, "/") && !strings.HasSuffix(ref, ".") && !strings.HasSuffix(ref, ".lock") && !strings.Contains(ref, "..") && !strings.Contains(ref, "//") && !strings.Contains(ref, "@{") && !strings.ContainsAny(ref, " ~^:?*[\\\x00\r\n\t")
}

// prepareGit lazily creates the execution's isolated bare repository and credential-file environment, reusing an existing workspace without remote mutation.
// RU: prepareGit отложенно создаёт изолированный bare-репозиторий и credential-file окружение исполнения, используя имеющийся workspace без внешнего изменения.
func (execution *ConfigurationWriteBackExecution) prepareGit(ctx context.Context) error {
	if execution.git != nil {
		return nil
	}
	w := execution.work
	workspace, err := newWriteBackGitWorkspace(ctx, execution.remote, execution.adapter.proxyURL, execution.credential, w.GetCommitAuthorName(), w.GetCommitAuthorEmail(), w.GetCommitTime().AsTime())
	if err != nil {
		return err
	}
	execution.git = workspace
	return nil
}

// PrepareCandidate permits execute mode only, requires the exact current base and absent proposal branch, then prepares one-file Git objects locally.
// RU: PrepareCandidate допускает только execute mode, требует точную текущую базу и отсутствие proposal-ветки, затем локально готовит Git-объекты одного файла.
// It validates equivalent typed provider inputs and remembers the exact candidate for PushCandidate; preparation does not itself perform push or owner begin.
// RU: Функция проверяет соответствующие типизированные provider inputs и запоминает точного кандидата для PushCandidate; подготовка не выполняет push либо owner begin.
func (execution *ConfigurationWriteBackExecution) PrepareCandidate(ctx context.Context) (ConfigurationWriteBackCandidate, error) {
	ctx, cancel := context.WithDeadline(ctx, execution.deadline)
	defer cancel()
	if execution.work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE {
		return ConfigurationWriteBackCandidate{}, errWriteBackGit
	}
	if err := execution.prepareGit(ctx); err != nil {
		return ConfigurationWriteBackCandidate{}, err
	}
	p := execution.work.GetProposal()
	source, proposal, err := execution.git.refs(ctx, p.GetSourceRefName(), p.GetProposalBranch())
	if err != nil {
		return ConfigurationWriteBackCandidate{}, err
	}
	if source != p.GetBaseCommitSha() {
		return ConfigurationWriteBackCandidate{}, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_SOURCE_CHANGED)
	}
	if proposal != "" {
		return ConfigurationWriteBackCandidate{}, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_BRANCH_CONFLICT)
	}
	candidate, err := execution.git.candidate(ctx, p.GetBaseCommitSha(), p.GetPath(), p.GetBaseContentSha256(), execution.work.GetCommitMessage(), execution.work.GetProposedContent())
	if err != nil {
		return ConfigurationWriteBackCandidate{}, err
	}
	result := ConfigurationWriteBackCandidate{CommitSHA: candidate.Commit, TreeSHA: candidate.Tree, BlobSHA: candidate.Blob, BaseBlobSHA: candidate.BaseBlob}
	if execution.definition.Spec.Adapter == "GITHUB" {
		if err := execution.validateInput("github.branch.create", map[string]any{"branch": p.GetProposalBranch(), "sha": p.GetBaseCommitSha()}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
		if err := execution.validateInput("github.repository.content.update", map[string]any{"branch": p.GetProposalBranch(), "path": p.GetPath(), "message": execution.work.GetCommitMessage(), "content_base64": base64.StdEncoding.EncodeToString(execution.work.GetProposedContent()), "sha": result.BaseBlobSHA}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
	} else {
		if err := execution.validateInput("gitlab.branch.create", map[string]any{"branch": p.GetProposalBranch(), "ref": p.GetBaseCommitSha()}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
		if err := execution.validateInput("gitlab.commit.create", map[string]any{"branch": p.GetProposalBranch(), "action": "update", "file_path": p.GetPath(), "content": string(execution.work.GetProposedContent()), "commit_message": execution.work.GetCommitMessage()}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
	}
	execution.candidate = &result
	return result, nil
}

// PushCandidate requires execute mode and the exact remembered candidate, rechecks source/base and absent proposal, then pushes only that proposal ref.
// RU: PushCandidate требует execute mode и точного запомненного кандидата, заново сверяет source/base и отсутствие proposal, затем отправляет только proposal ref.
// The caller must already hold a validated owner begin receipt; the Git lease protects proposal absence, not an atomic source-ref precondition.
// RU: Вызывающий уже обязан иметь проверенную owner begin квитанцию; Git lease защищает отсутствие proposal, а не атомарное условие исходной ветки.
func (execution *ConfigurationWriteBackExecution) PushCandidate(ctx context.Context, candidate ConfigurationWriteBackCandidate) error {
	ctx, cancel := context.WithDeadline(ctx, execution.deadline)
	defer cancel()
	if execution.git == nil || execution.candidate == nil || *execution.candidate != candidate || execution.work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE {
		return errWriteBackGit
	}
	p := execution.work.GetProposal()
	source, proposal, err := execution.git.refs(ctx, p.GetSourceRefName(), p.GetProposalBranch())
	if err != nil {
		return err
	}
	if source != p.GetBaseCommitSha() || proposal != "" {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_BRANCH_CONFLICT)
	}
	return execution.git.push(ctx, p.GetProposalBranch(), candidate.CommitSHA)
}

// validateInput serializes the server-derived provider payload and checks the selected capability schema, classifying rejected content without sending it.
// RU: validateInput сериализует выведенный сервером provider payload и проверяет схему выбранной capability, классифицируя отклонённое содержимое без отправки.
func (execution *ConfigurationWriteBackExecution) validateInput(operation string, input map[string]any) error {
	capability, ok := execution.definition.Capability(operation)
	if !ok {
		return errWriteBackGit
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return errWriteBackGit
	}
	_, err = capability.ValidateInput(raw)
	if err != nil {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_CONTENT_INVALID)
	}
	return nil
}
