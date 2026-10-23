// Package imageowner adapts the image owner's admission and promotion RPCs,
// preserving exact claim/evidence bindings without issuing owner authority locally.
// Package imageowner адаптирует admission и promotion RPC владельца образов,
// сохраняя точные привязки claim/evidence без локальной выдачи полномочий владельца.
package imageowner

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedclient "github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Config supplies the shared RPC transport/identity configuration and per-call
// deadline. Promotion selects a distinct caller/operation profile; credential
// contents are loaded and checked by the shared client, not stored in this value.
// Config задаёт транспорт/идентичность общего RPC-клиента и срок одного вызова.
// Promotion выбирает отдельные caller/operation profile; содержимое реквизитов
// загружается и проверяется общим клиентом, а не хранится в этом значении.
type Config struct {
	RPCProfile                                                                 string
	Target, TLSServerName, CAFile, ClientCertificateFile, ClientPrivateKeyFile string
	ApplicationGrantFile                                                       string
	ExpectedIssuerUID, ExpectedIssuerGID                                       uint32
	DialTimeout, RPCDeadline                                                   time.Duration
	Promotion                                                                  bool
}

// scopeName maps only organization/project wire scopes to persisted short names;
// unknown enum values become empty instead of inventing a permitted resource scope.
// scopeName переводит только organization/project enum в короткие имена снимка;
// неизвестные значения дают пустую строку, а не новую разрешённую область ресурса.
func scopeName(scope controlplanev1.RuntimeResourceScopeKind) string {
	switch scope {
	case controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION,
		controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT:
		return strings.TrimPrefix(scope.String(), "RUNTIME_RESOURCE_SCOPE_KIND_")
	default:
		return ""
	}
}

// Claim retains the private owner-issued admission snapshot: tenant/build/policy
// pins, fencing credential, exact attempt and optional risk/source receipt tuple.
// JSON persistence must preserve the entire value for replay; possessing or locally
// validating it is not equivalent to a fresh owner authorization.
// Claim сохраняет закрытый снимок допуска владельца: tenant/build/policy,
// fencing-реквизиты, точную попытку и необязательную связку risk/source receipt.
// JSON-хранилище должно сохранять всё значение для повтора; наличие снимка или его
// локальная проверка не заменяют новое разрешение владельца.
type Claim struct {
	ScopeKind                    string         `json:"scopeKind"`
	OrganizationRef              string         `json:"organizationRef"`
	ProjectRef                   string         `json:"projectRef"`
	ArtifactID                   string         `json:"artifactId"`
	Version                      uint64         `json:"version"`
	Fence                        uint64         `json:"fence"`
	AuthorityGeneration          uint64         `json:"authorityGeneration"`
	ClaimToken                   string         `json:"claimToken"`
	ExpiresAt                    time.Time      `json:"expiresAt"`
	RecipeID                     string         `json:"recipeId"`
	RecipeVersion                uint64         `json:"recipeVersion"`
	RecipeGeneration             uint64         `json:"recipeGeneration"`
	SpecSHA256                   string         `json:"specSHA256"`
	BuildID                      string         `json:"buildId"`
	BuildVersion                 uint64         `json:"buildVersion"`
	BuildAttempt                 uint32         `json:"buildAttempt"`
	StagingReference             string         `json:"stagingReference"`
	ManifestDigest               string         `json:"manifestDigest"`
	ImmutableBuildSHA256         string         `json:"immutableBuildSHA256"`
	ProvenanceSHA256             string         `json:"provenanceSHA256"`
	BaseImageDigest              string         `json:"baseImageDigest"`
	SourceSHA256                 string         `json:"sourceSHA256"`
	ContextSHA256                string         `json:"contextSHA256"`
	BuilderSHA256                string         `json:"builderSHA256"`
	FrontendSHA256               string         `json:"frontendSHA256"`
	ToolchainSHA256              string         `json:"toolchainSHA256"`
	RoleRuntimeContractRevision  uint64         `json:"roleRuntimeContractRevision"`
	RoleRuntimeContractSHA256    string         `json:"roleRuntimeContractSHA256"`
	Platforms                    []string       `json:"platforms"`
	PolicyRevision               uint64         `json:"policyRevision"`
	PolicySHA256                 string         `json:"policySHA256"`
	DeclaredTools                []DeclaredTool `json:"declaredTools"`
	AdmissionAttemptRef          string         `json:"admissionAttemptRef"`
	AdmissionAttempt             uint32         `json:"admissionAttempt"`
	RiskAcceptanceJSON           string         `json:"riskAcceptanceJSON"`
	RiskAcceptanceSHA256         string         `json:"riskAcceptanceSHA256"`
	SourceAdmissionReceiptSHA256 string         `json:"sourceAdmissionReceiptSHA256"`
	SourceEvidenceManifestDigest string         `json:"sourceEvidenceManifestDigest"`
	SourceAdmissionRevision      uint64         `json:"sourceAdmissionRevision"`
}

// DeclaredTool carries one owner-declared tool name, version and digest copied
// from the artifact; the type alone does not inspect a binary or authorize its use.
// DeclaredTool переносит имя, версию и хеш инструмента из артефакта владельца;
// сам тип не проверяет исполняемый файл и не разрешает его использование.
type DeclaredTool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

// AdmissionEvidence collects report/inventory bytes and evidence/signature/receipt
// digests plus the requested verdict. Local validation binds the vulnerability
// projection and risk tuple; the owner RPC still decides full admission eligibility.
// AdmissionEvidence объединяет байты report/inventory, хеши доказательств, подписи
// и квитанции с запрошенным вердиктом. Локальная проверка связывает проекцию
// уязвимостей и риск; полный допуск по-прежнему определяет RPC владельца.
type AdmissionEvidence struct {
	SBOMSHA256, VulnerabilityEvidenceSHA256, SignatureIdentity, SignatureSHA256 string
	AdmissionReceiptSHA256                                                      string
	AdmissionReceiptOCIManifestDigest                                           string
	Accepted                                                                    bool
	ToolInventoryJSON, ToolInventorySHA256                                      string
	VulnerabilityReportJSON, VulnerabilityReportProjectionSHA256                string
	RiskAcceptanceSHA256                                                        string
}

// Promotion retains a private promotion claim and exact admitted image/receipt,
// then carries its replacement authorization token and the caller's publication
// readback. Mutable fields are persisted separately from immutable admission Claim.
// Promotion сохраняет закрытый claim продвижения и точные допущенные образ/квитанцию,
// затем переносит заменивший claim authorization token и readback публикации.
// Изменяемые поля хранятся отдельно от неизменяемого admission Claim.
type Promotion struct {
	ScopeKind                         string    `json:"scopeKind"`
	OrganizationRef                   string    `json:"organizationRef"`
	ProjectRef                        string    `json:"projectRef"`
	ArtifactID                        string    `json:"artifactId"`
	Version                           uint64    `json:"version"`
	Claim                             string    `json:"claim"`
	Fence                             uint64    `json:"fence"`
	ExpiresAt                         time.Time `json:"expiresAt"`
	StagingReference                  string    `json:"stagingReference"`
	ManifestDigest                    string    `json:"manifestDigest"`
	AdmissionRevision                 uint64    `json:"admissionRevision"`
	AdmissionReceiptSHA256            string    `json:"admissionReceiptSHA256"`
	AdmissionReceiptOCIManifestDigest string    `json:"admissionReceiptOCIManifestDigest"`
	PromotedReference                 string    `json:"promotedReference,omitempty"`
	ReadbackSHA256                    string    `json:"readbackSHA256,omitempty"`
	AuthorizationToken                string    `json:"authorizationToken,omitempty"`
	AuthorizationExpiresAt            time.Time `json:"authorizationExpiresAt,omitempty"`
}

// Client owns the shared admission or promotion RPC adapter and bounds each call;
// it does not own artifact state, implement a worker loop or automatically re-claim.
// Client владеет общим admission либо promotion RPC-адаптером и ограничивает вызовы;
// он не владеет состоянием артефакта, циклом worker и автоматическим новым claim.
type Client struct {
	shared      *sharedclient.Client
	rpcDeadline time.Duration
}

// Dial selects the fixed admission/promotion caller and operation registration,
// delegates transport/credential setup to the shared client and retains its handle.
// Construction does not claim work or grant access beyond that operation profile.
// Dial выбирает фиксированные admission/promotion caller и регистрацию операций,
// передаёт настройку транспорта/реквизитов общему клиенту и сохраняет его handle.
// Создание не забирает работу и не расширяет доступ за пределы профиля операций.
func Dial(ctx context.Context, config Config) (*Client, error) {
	operations := sharedclient.ImageAdmissionOperations()
	caller := "image-admission"
	if config.Promotion {
		operations = sharedclient.ImagePromotionOperations()
		caller = "image-promotion"
	}
	client, err := sharedclient.Dial(ctx, sharedclient.Config{
		RPCProfile: config.RPCProfile, CallerWorkload: caller,
		Target: config.Target, TLSServerName: config.TLSServerName, CAFile: config.CAFile,
		ClientCertificateFile: config.ClientCertificateFile, ClientPrivateKeyFile: config.ClientPrivateKeyFile,
		ApplicationGrantFile: config.ApplicationGrantFile, ExpectedIssuerUID: config.ExpectedIssuerUID,
		ExpectedIssuerGID: config.ExpectedIssuerGID, DialTimeout: config.DialTimeout, Operations: operations,
	})
	if err != nil {
		return nil, err
	}
	return &Client{shared: client, rpcDeadline: config.RPCDeadline}, nil
}

// Check checks only the shared local authority path under a fresh RPC deadline;
// trusted-cluster identity needs no local issuer RPC. It is not image-owner health
// readback and does not validate any admission or promotion claim.
// Check проверяет только локальную authority общего клиента с новым сроком RPC;
// trusted-cluster identity не требует RPC локального issuer. Это не проверка
// готовности владельца образов и не проверка admission либо promotion claim.
func (client *Client) Check(ctx context.Context) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	return client.shared.CheckLocalAuthority(callCtx)
}

// Claim requests one owner-selected admission using the supplied receipt key,
// copies artifact/tool/platform/risk pins and validates the retained claim tuple.
// The embedded attempt must match reference, number, artifact and fence and still
// be CLAIMED; no terminal callback connection-wait option is added to this claim.
// Claim запрашивает выбранный владельцем допуск по ключу квитанции, переносит
// artifact/tool/platform/risk и проверяет связку сохранённого claim. Вложенная
// попытка должна совпадать по ссылке, номеру, артефакту и fence и быть CLAIMED;
// ожидание соединения terminal callback к новому claim не добавляется.
func (client *Client) Claim(ctx context.Context, key string) (Claim, error) {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.RoleImages.ClaimImageAdmission(callCtx,
		&controlplanev1.ClaimImageAdmissionRequest{IdempotencyKey: key})
	if err != nil {
		return Claim{}, err
	}
	artifact := response.GetImageArtifact()
	if artifact == nil || artifact.GetVersion() == 0 || response.GetClaimToken() == "" ||
		response.GetFence() == 0 || response.GetAuthorityGeneration() == 0 || response.GetClaimExpiresAt() == nil || len(artifact.GetPlatforms()) == 0 {
		return Claim{}, errors.New("image admission claim is incomplete")
	}
	platforms := make([]string, 0, len(artifact.GetPlatforms()))
	for _, platform := range artifact.GetPlatforms() {
		value := platform.GetOs() + "/" + platform.GetArchitecture()
		if platform.GetVariant() != "" {
			value += "/" + platform.GetVariant()
		}
		platforms = append(platforms, value)
	}
	declared := []DeclaredTool{}
	for _, tool := range artifact.GetDeclaredTools() {
		declared = append(declared, DeclaredTool{Name: tool.GetName(), Version: tool.GetVersion(), SHA256: tool.GetSha256()})
	}
	claim := Claim{DeclaredTools: declared, ArtifactID: artifact.GetRef(), Version: artifact.GetVersion(), Fence: response.GetFence(),
		ScopeKind: scopeName(artifact.GetScopeKind()), OrganizationRef: artifact.GetOrganizationRef(), ProjectRef: artifact.GetProjectRef(),
		AuthorityGeneration: response.GetAuthorityGeneration(),
		ClaimToken:          response.GetClaimToken(), ExpiresAt: response.GetClaimExpiresAt().AsTime(),
		RecipeID: artifact.GetRecipeRef(), RecipeVersion: artifact.GetRecipeVersion(), RecipeGeneration: artifact.GetRecipeGeneration(),
		SpecSHA256: artifact.GetSpecSha256(), BuildID: artifact.GetBuildRef(), BuildVersion: artifact.GetBuildVersion(),
		BuildAttempt: artifact.GetBuildAttempt(), StagingReference: artifact.GetStagingReference(),
		ManifestDigest: artifact.GetManifestDigest(), ImmutableBuildSHA256: artifact.GetImmutableBuildSha256(),
		ProvenanceSHA256: artifact.GetProvenanceSha256(), PolicyRevision: artifact.GetPolicyRevision(),
		PolicySHA256: artifact.GetPolicySha256(), BaseImageDigest: artifact.GetBaseImageDigest(),
		SourceSHA256: artifact.GetSourceSha256(), ContextSHA256: artifact.GetContextSha256(),
		BuilderSHA256: artifact.GetBuilderSha256(), FrontendSHA256: artifact.GetFrontendSha256(),
		ToolchainSHA256: artifact.GetToolchainSha256(), Platforms: platforms,
		RoleRuntimeContractRevision: artifact.GetRoleRuntimeContractRevision(),
		RoleRuntimeContractSHA256:   artifact.GetRoleRuntimeContractSha256(),
		AdmissionAttemptRef:         response.GetAdmissionAttemptRef(), AdmissionAttempt: response.GetAdmissionAttempt(),
		RiskAcceptanceJSON: response.GetRiskAcceptanceJson(), RiskAcceptanceSHA256: response.GetRiskAcceptanceSha256(),
		SourceAdmissionReceiptSHA256: response.GetSourceAdmissionReceiptSha256(),
		SourceEvidenceManifestDigest: response.GetSourceEvidenceManifestDigest(), SourceAdmissionRevision: response.GetSourceAdmissionRevision()}
	if err := ValidateAdmissionClaim(claim); err != nil {
		return Claim{}, err
	}
	if attempt := artifact.GetAdmissionAttempt(); attempt == nil || attempt.GetRef() != claim.AdmissionAttemptRef ||
		attempt.GetNumber() != claim.AdmissionAttempt || attempt.GetArtifactRef() != claim.ArtifactID ||
		attempt.GetFence() != claim.Fence || attempt.GetState() != controlplanev1.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_CLAIMED {
		return Claim{}, errAdmissionClaim
	}
	return claim, nil
}

// errAdmissionClaim is the fixed binding failure without private claim/risk details.
// errAdmissionClaim — постоянная ошибка привязки без закрытых подробностей claim/risk.
var errAdmissionClaim = errors.New("image admission claim binding is invalid")

// errAdmissionReport is the fixed evidence failure without report or credential bytes.
// errAdmissionReport — постоянная ошибка доказательств без байтов отчёта и реквизитов.
var errAdmissionReport = errors.New("image admission report binding is invalid")

// admissionAttemptRefPattern accepts only the bounded imgadm reference grammar,
// independent of whether the referenced attempt exists or belongs to the caller.
// admissionAttemptRefPattern допускает только ограниченную грамматику ссылки imgadm,
// независимо от существования попытки и её принадлежности вызывающему.
var admissionAttemptRefPattern = regexp.MustCompile(`^imgadm_[A-Za-z0-9_-]{8,120}$`)

// ValidateAdmissionClaim checks the nonempty fencing/attempt tuple and either an
// absent risk tuple or canonical risk bytes matching the retained tenant/build/
// policy/source pins. It does not check current expiry or replace owner authority:
// expired snapshots are retained for exact terminal recovery, while authorization
// for a new admission remains an owner check.
// ValidateAdmissionClaim проверяет непустую fencing/attempt связку и отсутствие
// риска либо канонические risk-байты с теми же tenant/build/policy/source полями.
// Текущее истечение не проверяется, полномочия владельца не заменяются: старые
// снимки сохраняются для точного terminal recovery; новый допуск проверяет владелец.
func ValidateAdmissionClaim(claim Claim) error {
	if !admissionAttemptRefPattern.MatchString(claim.AdmissionAttemptRef) || claim.AdmissionAttempt == 0 || claim.Version == 0 || claim.Fence == 0 ||
		claim.AuthorityGeneration == 0 || claim.ClaimToken == "" || claim.ExpiresAt.IsZero() {
		return errAdmissionClaim
	}
	if claim.RiskAcceptanceJSON == "" {
		if claim.RiskAcceptanceSHA256 != "" || claim.SourceAdmissionReceiptSHA256 != "" ||
			claim.SourceEvidenceManifestDigest != "" || claim.SourceAdmissionRevision != 0 {
			return errAdmissionClaim
		}
		return nil
	}
	raw := []byte(claim.RiskAcceptanceJSON)
	decision, err := runtimecontract.DecodeImageRiskAcceptance(raw)
	if err != nil || claim.RiskAcceptanceSHA256 != runtimecontract.ImageVulnerabilitySHA256(raw) ||
		decision.SourceAdmissionReceiptSHA256 != claim.SourceAdmissionReceiptSHA256 ||
		decision.SourceEvidenceManifestDigest != claim.SourceEvidenceManifestDigest || decision.SourceAdmissionRevision != claim.SourceAdmissionRevision ||
		decision.ScopeKind != claim.ScopeKind || decision.OrganizationRef != claim.OrganizationRef || decision.ProjectRef != claim.ProjectRef ||
		decision.ArtifactRef != claim.ArtifactID || decision.ImageDigest != claim.ManifestDigest || decision.RecipeRef != claim.RecipeID ||
		decision.RecipeVersion != claim.RecipeVersion || decision.RecipeGeneration != claim.RecipeGeneration ||
		decision.BuildRef != claim.BuildID || decision.BuildVersion != claim.BuildVersion || decision.BuildAttempt != claim.BuildAttempt ||
		decision.PolicyRevision != claim.PolicyRevision || decision.PolicySHA256 != claim.PolicySHA256 {
		return errAdmissionClaim
	}
	return nil
}

// ValidateAdmissionEvidence binds a canonical vulnerability projection to the claim,
// report/SBOM digests and exact risk envelope. Accepted blocking findings require
// that matching risk decision; signature/inventory/technical eligibility remain
// separate owner checks rather than privileges granted by accepting risk locally.
// ValidateAdmissionEvidence связывает каноническую проекцию уязвимостей с claim,
// хешами report/SBOM и точным конвертом риска. Принятие blocking findings требует
// совпадающего решения о риске; подпись, inventory и техническая допустимость
// остаются отдельными проверками владельца, а не локально выдаваемыми правами.
func ValidateAdmissionEvidence(claim Claim, evidence AdmissionEvidence) error {
	if ValidateAdmissionClaim(claim) != nil || evidence.RiskAcceptanceSHA256 != claim.RiskAcceptanceSHA256 {
		return errAdmissionReport
	}
	raw := []byte(evidence.VulnerabilityReportJSON)
	report, err := runtimecontract.DecodeImageVulnerabilityReport(raw)
	if err != nil || evidence.VulnerabilityReportProjectionSHA256 != runtimecontract.ImageVulnerabilitySHA256(raw) ||
		report.ScopeKind != claim.ScopeKind || report.OrganizationRef != claim.OrganizationRef || report.ProjectRef != claim.ProjectRef ||
		report.ArtifactRef != claim.ArtifactID || report.ImageDigest != claim.ManifestDigest || report.RecipeRef != claim.RecipeID ||
		report.RecipeVersion != claim.RecipeVersion || report.RecipeGeneration != claim.RecipeGeneration ||
		report.BuildRef != claim.BuildID || report.BuildVersion != claim.BuildVersion || report.BuildAttempt != claim.BuildAttempt ||
		report.PolicyRevision != claim.PolicyRevision || report.PolicySHA256 != claim.PolicySHA256 ||
		report.SBOMSHA256 != evidence.SBOMSHA256 || report.ReportSHA256 != evidence.VulnerabilityEvidenceSHA256 {
		return errAdmissionReport
	}
	if claim.RiskAcceptanceJSON != "" {
		decision, err := runtimecontract.DecodeImageRiskAcceptance([]byte(claim.RiskAcceptanceJSON))
		if err != nil || !runtimecontract.ImageRiskAcceptanceMatchesReport(decision, report) {
			return errAdmissionReport
		}
	} else if evidence.Accepted && report.BlockingMatchCount != 0 {
		return errAdmissionReport
	}
	return nil
}

// Record rejects mismatched report/risk evidence before RPC, submits the exact
// claim attempt and all evidence fields, then requires version+1 and the matching
// terminal attempt state/fence/receipt. RPC errors are returned without an automatic
// retry or conversion of an uncertain outcome into a successful admission.
// Record отклоняет несовпадающие report/risk до RPC, передаёт точную попытку claim
// и все доказательства, затем требует version+1 и совпадающие terminal state,
// fence и квитанцию. Ошибка RPC возвращается без автоматического повтора либо
// превращения неопределённого исхода в успешный допуск.
func (client *Client) Record(ctx context.Context, key string, claim Claim, evidence AdmissionEvidence) error {
	if err := ValidateAdmissionEvidence(claim, evidence); err != nil {
		return err
	}
	verdict := controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_REJECTED
	if evidence.Accepted {
		verdict = controlplanev1.ImageAdmissionVerdict_IMAGE_ADMISSION_VERDICT_ACCEPTED
	}
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.RoleImages.RecordImageAdmission(callCtx, &controlplanev1.RecordImageAdmissionRequest{
		IdempotencyKey: key, ImageArtifactRef: claim.ArtifactID, ExpectedVersion: claim.Version,
		ExpectedFence: claim.Fence, ClaimToken: claim.ClaimToken, ManifestDigest: claim.ManifestDigest,
		ImmutableBuildSha256: claim.ImmutableBuildSHA256, ProvenanceSha256: claim.ProvenanceSHA256,
		SbomSha256: evidence.SBOMSHA256, VulnerabilityEvidenceSha256: evidence.VulnerabilityEvidenceSHA256,
		PolicyRevision: claim.PolicyRevision, PolicySha256: claim.PolicySHA256, Verdict: verdict,
		SignatureIdentity: evidence.SignatureIdentity, SignatureSha256: evidence.SignatureSHA256,
		AdmissionReceiptSha256:            evidence.AdmissionReceiptSHA256,
		AdmissionReceiptOciManifestDigest: evidence.AdmissionReceiptOCIManifestDigest,
		ToolInventoryJson:                 evidence.ToolInventoryJSON, ToolInventorySha256: evidence.ToolInventorySHA256,
		ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt,
		VulnerabilityReportJson: evidence.VulnerabilityReportJSON, VulnerabilityReportProjectionSha256: evidence.VulnerabilityReportProjectionSHA256,
		RiskAcceptanceSha256: evidence.RiskAcceptanceSHA256,
	})
	if err != nil {
		return err
	}
	artifact := response.GetImageArtifact()
	if artifact == nil || artifact.GetRef() != claim.ArtifactID || artifact.GetVersion() != claim.Version+1 {
		return errors.New("recorded image admission response is incomplete")
	}
	attempt := artifact.GetAdmissionAttempt()
	expectedState := controlplanev1.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_REJECTED
	if evidence.Accepted {
		expectedState = controlplanev1.ImageAdmissionAttemptState_IMAGE_ADMISSION_ATTEMPT_STATE_ACCEPTED
	}
	if attempt == nil || attempt.GetRef() != claim.AdmissionAttemptRef || attempt.GetNumber() != claim.AdmissionAttempt ||
		attempt.GetArtifactRef() != claim.ArtifactID || attempt.GetState() != expectedState || attempt.GetFence() != claim.Fence ||
		attempt.GetAdmissionReceiptSha256() != evidence.AdmissionReceiptSHA256 || attempt.GetEvidenceManifestDigest() != evidence.AdmissionReceiptOCIManifestDigest {
		return errors.New("recorded image admission response is incomplete")
	}
	return nil
}

// Fail submits one exact terminal callback with WaitForReady under its bounded
// context. Only PermissionDenied triggers the separate Expire command with a
// derived receipt key and fresh deadline; other RPC errors remain errors, and
// a successful callback must return the expected failed artifact tuple.
// Fail передаёт один точный terminal callback с WaitForReady в ограниченном
// контексте. Только PermissionDenied вызывает отдельный Expire с производным
// ключом и новым сроком; прочие RPC-ошибки остаются ошибками, а успешный ответ
// должен содержать ожидаемую связку завершившегося ошибкой артефакта.
func (client *Client) Fail(ctx context.Context, key string, claim Claim, code string) error {
	if err := ValidateAdmissionClaim(claim); err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	// Только terminal callback ждёт соединение в прежнем bounded context;
	// ответ владельца не повторяется и не превращается в новую claim.
	response, err := client.shared.RoleImages.FailImageAdmission(callCtx, &controlplanev1.FailImageAdmissionRequest{
		IdempotencyKey: key, ImageArtifactRef: claim.ArtifactID, ExpectedVersion: claim.Version,
		ExpectedFence: claim.Fence, ClaimToken: claim.ClaimToken, ExpectedAuthorityGeneration: claim.AuthorityGeneration,
		ManifestDigest: claim.ManifestDigest, ImmutableBuildSha256: claim.ImmutableBuildSHA256, ProvenanceSha256: claim.ProvenanceSHA256,
		PolicyRevision: claim.PolicyRevision, PolicySha256: claim.PolicySHA256, BuildRef: claim.BuildID,
		ExpectedBuildAttempt: claim.BuildAttempt, RecipeGeneration: claim.RecipeGeneration, SpecSha256: claim.SpecSHA256, ErrorCode: code,
		ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt,
	}, grpc.WaitForReady(true))
	if status.Code(err) == codes.PermissionDenied {
		return client.Expire(ctx, key+"-expiry", claim)
	}
	if err != nil {
		return err
	}
	return validateFailure(response.GetAdmissionFailure(), claim, code)
}

// Expire asks the owner to expire this exact retained attempt without a claim token,
// preserving fencing/build/policy pins and WaitForReady within a fresh deadline.
// Success requires the matching FAILED receipt with ADMISSION_LEASE_EXPIRED.
// Expire просит владельца завершить точную сохранённую попытку без claim token,
// сохраняя fencing/build/policy и WaitForReady с новым сроком. Успех требует
// совпадающей FAILED-квитанции с ADMISSION_LEASE_EXPIRED.
func (client *Client) Expire(ctx context.Context, key string, claim Claim) error {
	if err := ValidateAdmissionClaim(claim); err != nil {
		return err
	}
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.RoleImages.ExpireImageAdmissionClaim(callCtx, &controlplanev1.ExpireImageAdmissionClaimRequest{IdempotencyKey: key, ImageArtifactRef: claim.ArtifactID, ExpectedVersion: claim.Version, ExpectedFence: claim.Fence, ExpectedAuthorityGeneration: claim.AuthorityGeneration, ManifestDigest: claim.ManifestDigest, ImmutableBuildSha256: claim.ImmutableBuildSHA256, ProvenanceSha256: claim.ProvenanceSHA256, PolicyRevision: claim.PolicyRevision, PolicySha256: claim.PolicySHA256, BuildRef: claim.BuildID, ExpectedBuildAttempt: claim.BuildAttempt, RecipeGeneration: claim.RecipeGeneration, SpecSha256: claim.SpecSHA256, ExpectedAdmissionAttemptRef: claim.AdmissionAttemptRef, ExpectedAdmissionAttempt: claim.AdmissionAttempt}, grpc.WaitForReady(true))
	if err != nil {
		return err
	}
	return validateFailure(response.GetAdmissionFailure(), claim, "ADMISSION_LEASE_EXPIRED")
}

// validateFailure requires version+1 and the same artifact, recipe generation,
// build attempt and tenant scope with FAILED and the requested error code;
// missing or mismatched owner receipts cannot be treated as terminal success.
// validateFailure требует version+1 и те же артефакт, поколение рецепта, попытку
// сборки и область tenant при FAILED и запрошенном коде; отсутствующая либо чужая
// квитанция владельца не считается успешным terminal завершением.
func validateFailure(failure *controlplanev1.RoleImageAdmissionFailure, claim Claim, code string) error {
	if failure == nil || failure.GetImageArtifactRef() != claim.ArtifactID || failure.GetVersion() != claim.Version+1 ||
		failure.GetRecipeRef() != claim.RecipeID || failure.GetRecipeGeneration() != claim.RecipeGeneration ||
		failure.GetBuildRef() != claim.BuildID || failure.GetBuildAttempt() != claim.BuildAttempt ||
		scopeName(failure.GetScopeKind()) != claim.ScopeKind || failure.GetOrganizationRef() != claim.OrganizationRef ||
		failure.GetProjectRef() != claim.ProjectRef || failure.GetState() != "FAILED" || failure.GetErrorCode() != code {
		return errors.New("failed image admission response is incomplete")
	}
	return nil
}

// ClaimPromotion requests owner-selected promotion work and requires the claim,
// version/fence/authority-generation/expiry plus staging image and admission receipt
// fields. The returned snapshot retains promotion fields but not the checked authority
// generation, and does not itself copy image bytes or authorize registry mutation.
// ClaimPromotion запрашивает выбранное владельцем продвижение и требует claim,
// version/fence/authority-generation/expiry, staging-образ и квитанцию допуска.
// Возвращённый снимок хранит promotion, но не проверенное authority generation;
// он сам не копирует образ и не разрешает изменение реестра.
func (client *Client) ClaimPromotion(ctx context.Context, key string) (Promotion, error) {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.RoleImages.ClaimImagePromotion(callCtx,
		&controlplanev1.ClaimImagePromotionRequest{IdempotencyKey: key})
	if err != nil {
		return Promotion{}, err
	}
	artifact := response.GetImageArtifact()
	if artifact == nil || artifact.GetRef() == "" || artifact.GetVersion() == 0 || response.GetPromotionClaim() == "" ||
		response.GetFence() == 0 || response.GetAuthorityGeneration() == 0 || response.GetClaimExpiresAt() == nil ||
		artifact.GetStagingReference() == "" || artifact.GetManifestDigest() == "" || artifact.GetAdmissionRevision() == 0 ||
		artifact.GetAdmissionReceiptSha256() == "" || artifact.GetAdmissionReceiptOciManifestDigest() == "" {
		return Promotion{}, errors.New("image promotion claim is incomplete")
	}
	return Promotion{ArtifactID: artifact.GetRef(), Version: artifact.GetVersion(),
		ScopeKind: scopeName(artifact.GetScopeKind()), OrganizationRef: artifact.GetOrganizationRef(), ProjectRef: artifact.GetProjectRef(),
		Claim: response.GetPromotionClaim(), Fence: response.GetFence(),
		ExpiresAt: response.GetClaimExpiresAt().AsTime(), StagingReference: artifact.GetStagingReference(),
		ManifestDigest: artifact.GetManifestDigest(), AdmissionRevision: artifact.GetAdmissionRevision(),
		AdmissionReceiptSHA256:            artifact.GetAdmissionReceiptSha256(),
		AdmissionReceiptOCIManifestDigest: artifact.GetAdmissionReceiptOciManifestDigest()}, nil
}

// Complete submits the authorization token, expected version and publication
// reference/digest/readback to the owner within one deadline. It currently checks
// only that a response artifact exists; it does not perform registry readback itself.
// Complete передаёт владельцу authorization token, ожидаемую версию и reference,
// digest/readback публикации в пределах одного срока. Локально проверяется только
// наличие артефакта в ответе; самостоятельного чтения реестра здесь нет.
func (client *Client) Complete(ctx context.Context, key string, promotion Promotion) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.RoleImages.CompleteImagePromotion(callCtx, &controlplanev1.CompleteImagePromotionRequest{
		IdempotencyKey: key, ImageArtifactRef: promotion.ArtifactID, ExpectedVersion: promotion.Version,
		AuthorizationToken: promotion.AuthorizationToken, PromotedReference: promotion.PromotedReference,
		ManifestDigest: promotion.ManifestDigest, PromotionReadbackSha256: promotion.ReadbackSHA256,
	})
	if err != nil {
		return err
	}
	if response.GetImageArtifact() == nil {
		return errors.New("completed image promotion response is incomplete")
	}
	return nil
}

// AuthorizePromotion exchanges the current claim for an owner token and expiry,
// requiring a strictly newer artifact version. Only after those checks does it
// replace the pointed version/token/expiry and clear Claim; it does not validate
// artifact identity or token lifetime locally beyond the present response fields.
// AuthorizePromotion меняет текущий claim на токен владельца и expiry, требуя
// строго более новую версию артефакта. После проверок обновляются version/token/
// expiry по указателю и очищается Claim; идентичность артефакта и актуальность
// токена локально не проверяются сверх наличия указанных полей ответа.
func (client *Client) AuthorizePromotion(ctx context.Context, key string, promotion *Promotion) error {
	callCtx, cancel := context.WithTimeout(ctx, client.rpcDeadline)
	defer cancel()
	response, err := client.shared.RoleImages.AuthorizeImagePromotion(callCtx,
		&controlplanev1.AuthorizeImagePromotionRequest{IdempotencyKey: key,
			ImageArtifactRef: promotion.ArtifactID, ExpectedVersion: promotion.Version,
			PromotionClaim: promotion.Claim, ManifestDigest: promotion.ManifestDigest})
	if err != nil {
		return err
	}
	artifact := response.GetImageArtifact()
	if artifact == nil || artifact.GetVersion() <= promotion.Version || response.GetAuthorizationToken() == "" ||
		response.GetAuthorizationExpiresAt() == nil {
		return errors.New("image promotion authorization response is incomplete")
	}
	promotion.Version = artifact.GetVersion()
	promotion.AuthorizationToken = response.GetAuthorizationToken()
	promotion.AuthorizationExpiresAt = response.GetAuthorizationExpiresAt().AsTime()
	promotion.Claim = ""
	return nil
}

// Close releases the shared client's owned connections and returns its close error;
// callers must stop their own in-flight work before closing the adapter.
// Close освобождает соединения общего клиента и возвращает его ошибку закрытия;
// вызывающий код должен предварительно остановить собственную активную работу.
func (client *Client) Close() error { return client.shared.Close() }
