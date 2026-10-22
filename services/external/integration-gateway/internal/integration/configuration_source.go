package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

const (
	// maximumSourceContentBytes caps decoded configuration bytes independently of the larger provider-response envelope.
	// RU: maximumSourceContentBytes ограничивает декодированные байты конфигурации независимо от большего provider-response конверта.
	maximumSourceContentBytes = 256 << 10
	// maximumSourceResponseBytes bounds raw source metadata and encoded-content responses before parsing them.
	// RU: maximumSourceResponseBytes ограничивает сырые метаданные источника и ответы с кодированным содержимым до разбора.
	maximumSourceResponseBytes = 1 << 20
)

// sourceCommitPattern accepts only canonical lowercase forty-character Git object IDs used by these source adapters.
// RU: sourceCommitPattern принимает только канонические строчные сорокасимвольные Git object ID этих source-адаптеров.
var sourceCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// ConfigurationSourceResult returns exact provider commit/content provenance and ancestry; the owner must still validate and admit the configuration itself.
// RU: ConfigurationSourceResult возвращает точные commit/content provenance и происхождение истории; владелец ещё обязан проверить и допустить саму конфигурацию.
// Successful content bytes become caller-owned and must be cleared after owner completion or rejection.
// RU: Успешно прочитанные байты переходят вызывающему и требуют очистки после owner completion либо отклонения.
type ConfigurationSourceResult struct {
	CommitSHA, ContentSHA256 string
	Content                  []byte
	Ancestry                 cp.ManagedConfigurationSourceAncestry
}

// configurationSourceError carries a closed source-failure enum without retaining provider diagnostics or file contents.
// RU: configurationSourceError хранит закрытый source-failure enum без provider-диагностики и содержимого файлов.
type configurationSourceError struct {
	failure cp.ManagedConfigurationSourceFailure
}

// Error exposes one fixed diagnostic regardless of the internal source-failure classification.
// RU: Error возвращает одну фиксированную диагностику независимо от внутренней source-failure классификации.
func (*configurationSourceError) Error() string { return "configuration source read failed" }

// ConfigurationSourceFailure extracts a wrapped source failure and maps every unrelated error to unavailable for the owner's failure report.
// RU: ConfigurationSourceFailure извлекает обёрнутую source-ошибку и переводит остальные ошибки в unavailable для owner-отчёта.
func ConfigurationSourceFailure(err error) cp.ManagedConfigurationSourceFailure {
	var failure *configurationSourceError
	if errors.As(err, &failure) {
		return failure.failure
	}
	return cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_UNAVAILABLE
}

// sourceFailure wraps an already selected source-failure enum without incorporating untrusted error strings.
// RU: sourceFailure оборачивает уже выбранный source-failure enum без включения недоверенных строк ошибок.
func sourceFailure(failure cp.ManagedConfigurationSourceFailure) error {
	return &configurationSourceError{failure: failure}
}

// sourceResponseInvalid produces the common closed failure for malformed claims, source metadata or content evidence.
// RU: sourceResponseInvalid формирует общий закрытый отказ для неверных claims, source-метаданных либо доказательств содержимого.
func sourceResponseInvalid() error {
	return sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_RESPONSE_INVALID)
}

// ReadConfigurationSource validates the claimed source, bounds its deadline by capability and lease, then reads only GitHub or GitLab through the pinned credential.
// RU: ReadConfigurationSource проверяет назначенный источник, ограничивает срок capability и арендой, затем читает только GitHub либо GitLab с закреплённым credential.
// It clears credential bytes and rejected content, returning accepted bytes only with a canonical commit, size check and freshly computed SHA-256.
// RU: Функция очищает credential-байты и отклонённое содержимое, возвращая байты только с каноническим commit, проверенным размером и новым SHA-256.
// Provider evidence is not configuration admission, and a deadline or cancellation overrides a partially obtained result.
// RU: Доказательства провайдера не являются допуском конфигурации; истечение срока либо отмена отменяют частично полученный результат.
func (adapter *Adapter) ReadConfigurationSource(ctx context.Context, work *cp.ManagedConfigurationSourceWork) (ConfigurationSourceResult, error) {
	request, definition, configuration, err := adapter.validateConfigurationSource(work)
	if err != nil {
		return ConfigurationSourceResult{}, err
	}
	deadline := work.GetDeadline().AsTime()
	capability, _ := definition.Capability(request.CapabilityKey)
	capabilityDeadline := time.Now().Add(time.Duration(capability.Execution.TimeoutSeconds) * time.Second)
	if capabilityDeadline.Before(deadline) {
		deadline = capabilityDeadline
	}
	if work.GetLease().GetExpiresAt().AsTime().Before(deadline) {
		deadline = work.GetLease().GetExpiresAt().AsTime()
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if ctx.Err() != nil {
		return ConfigurationSourceResult{}, ctx.Err()
	}
	credential, err := adapter.readCredential(ctx, request.Credential)
	if err != nil {
		return ConfigurationSourceResult{}, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_CREDENTIAL_REJECTED)
	}
	defer clear(credential)
	reader := configurationSourceReader{adapter: adapter, credential: credential, maximumContent: int(work.GetMaximumContentBytes())}
	var result ConfigurationSourceResult
	switch definition.Spec.Adapter {
	case "GITHUB":
		result, err = reader.readGitHub(ctx, configuration, work)
	case "GITLAB":
		result, err = reader.readGitLab(ctx, configuration, work)
	default:
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	if err != nil || ctx.Err() != nil {
		clear(result.Content)
		if ctx.Err() != nil {
			return ConfigurationSourceResult{}, ctx.Err()
		}
		return ConfigurationSourceResult{}, err
	}
	if !sourceCommitPattern.MatchString(result.CommitSHA) || len(result.Content) < 1 || len(result.Content) > reader.maximumContent {
		clear(result.Content)
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	digest := sha256.Sum256(result.Content)
	result.ContentSHA256 = hex.EncodeToString(digest[:])
	return result, nil
}

// validateConfigurationSource checks known-field owner work, lease/deadline pins, bounded path/ref/content settings and supported configuration kind/format.
// RU: validateConfigurationSource проверяет owner-работу без неизвестных полей, lease/deadline, ограниченные path/ref/content и поддерживаемые kind/format.
// The parsed package and public configuration must identify the exact GitHub/GitLab repository and an ungated READ capability whose input schema accepts the selected file.
// RU: Разобранный пакет и публичная конфигурация обязаны обозначать точный GitHub/GitLab репозиторий и READ capability без gate, схема которой принимает выбранный файл.
// These are consistency checks of trusted owner work, not independent authorization of caller-created lease or connection identifiers.
// RU: Это проверки согласованности доверенной owner-работы, а не самостоятельная авторизация созданных вызывающим lease либо connection ID.
func (adapter *Adapter) validateConfigurationSource(work *cp.ManagedConfigurationSourceWork) (Request, integrationpackage.Package, map[string]string, error) {
	invalid := func() (Request, integrationpackage.Package, map[string]string, error) {
		return Request{}, integrationpackage.Package{}, nil, sourceResponseInvalid()
	}
	if work == nil || len(work.ProtoReflect().GetUnknown()) != 0 || work.GetLease() == nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil || work.GetLease().GetExpiresAt() == nil || work.GetLease().GetExpiresAt().CheckValid() != nil {
		return invalid()
	}
	lease := work.GetLease()
	if len(lease.ProtoReflect().GetUnknown()) != 0 || work.GetCredentialRevision() == nil || len(work.GetCredentialRevision().ProtoReflect().GetUnknown()) != 0 {
		return invalid()
	}
	if lease.GetWorkRef() == "" || lease.GetSourceGeneration() < 1 || lease.GetAttempt() < 1 || lease.GetClaimGeneration() < 1 || lease.GetClaimant() == "" || lease.GetFence() == "" || !lease.GetExpiresAt().AsTime().After(time.Now()) || !work.GetDeadline().AsTime().After(time.Now()) || work.GetConnectionVersion() < 1 || work.GetConnectionRef() == "" || work.GetSourceRef() == "" || work.GetConfigurationRef() == "" {
		return invalid()
	}
	if work.GetMaximumContentBytes() < 1 || work.GetMaximumContentBytes() > maximumSourceContentBytes || len(work.GetPath()) > 1024 || !validRepositoryPath(work.GetPath(), false) || len(strings.Split(work.GetPath(), "/")) > 32 || work.GetRefName() == "" || len(work.GetRefName()) > 256 || strings.ContainsAny(work.GetRefName(), "\x00\r\n\\") || work.GetPreviousCommitSha() != "" && !sourceCommitPattern.MatchString(work.GetPreviousCommitSha()) {
		return invalid()
	}
	if work.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE && work.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION || work.GetContentFormat() != "YAML" && work.GetContentFormat() != "JSON" {
		return invalid()
	}
	request := Request{DefinitionPackage: work.GetDefinitionPackage(), DefinitionKey: work.GetDefinitionKey(), DefinitionVersion: work.GetDefinitionVersion(), DefinitionDigest: work.GetDefinitionDigest(), ConnectionRef: work.GetConnectionRef(), Credential: credentialFromProto(work.GetCredentialRevision())}
	definition, err := adapter.validateDefinition(request)
	if err != nil || work.GetPublicConfiguration() == nil {
		return invalid()
	}
	configuration, err := normalizeStringMap(work.GetPublicConfiguration().AsMap())
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return invalid()
	}
	operation := ""
	switch definition.Spec.Adapter {
	case "GITHUB":
		if work.GetRepositoryRef() != configuration["owner"]+"/"+configuration["repository"] {
			return invalid()
		}
		operation = "github.repository.content.read"
	case "GITLAB":
		if work.GetRepositoryRef() != configuration["project_path"] {
			return invalid()
		}
		operation = "gitlab.repository.file.read"
	default:
		return invalid()
	}
	capability, ok := definition.Capability(operation)
	if !ok || capability.Risk != "READ" || capability.ApprovalPolicy != "NONE" {
		return invalid()
	}
	request.CapabilityKey = capability.Key
	input := map[string]string{"ref": work.GetRefName(), "path": work.GetPath()}
	if definition.Spec.Adapter == "GITLAB" {
		input = map[string]string{"ref": work.GetRefName(), "file_path": work.GetPath()}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return invalid()
	}
	if _, err := capability.ValidateInput(raw); err != nil {
		return invalid()
	}
	return request, definition, configuration, nil
}

// configurationSourceReader carries one operation's credential bytes and decoded-content limit while reusing adapter-owned HTTP transports.
// RU: configurationSourceReader хранит credential-байты одной операции и предел декодированного содержимого, используя HTTP transport адаптера.
type configurationSourceReader struct {
	adapter        *Adapter
	credential     []byte
	maximumContent int
}

// get performs one GET under the selected base endpoint, preserving escaped path/query and fixed auth/accept headers without an application-level retry.
// RU: get выполняет один GET внутри выбранного base endpoint, сохраняя escaped path/query и фиксированные auth/accept заголовки без прикладного повтора.
// It maps closed status failures, closes obtained bodies and rejects empty, oversized or unreadable responses; new work retries belong to the owner.
// RU: Функция классифицирует статусы, закрывает полученное body и отклоняет пустой, слишком большой либо нечитаемый ответ; новые work-повторы назначает владелец.
func (reader configurationSourceReader) get(ctx context.Context, client *http.Client, base *url.URL, path string, query url.Values, accept string) ([]byte, error) {
	endpoint := *base
	escaped := strings.TrimSuffix(endpoint.EscapedPath(), "/") + path
	decoded, err := url.PathUnescape(escaped)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, sourceResponseInvalid()
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery = decoded, escaped, query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, sourceResponseInvalid()
	}
	request.Header.Set("Authorization", "Bearer "+string(reader.credential))
	request.Header.Set("Accept", accept)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := client.Do(request)
	if err != nil {
		return nil, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_UNAVAILABLE)
	}
	if response == nil || response.Body == nil {
		return nil, sourceResponseInvalid()
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_CREDENTIAL_REJECTED)
	case http.StatusForbidden:
		return nil, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_ACCESS_DENIED)
	case http.StatusNotFound:
		return nil, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_NOT_FOUND)
	default:
		return nil, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_UNAVAILABLE)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumSourceResponseBytes+1))
	if err != nil || len(raw) < 1 || len(raw) > maximumSourceResponseBytes {
		clear(raw)
		return nil, sourceResponseInvalid()
	}
	return raw, nil
}
