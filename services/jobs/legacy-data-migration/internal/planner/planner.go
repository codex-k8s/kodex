// Package planner строит единственный typed owner plan из immutable source snapshot.
package planner

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const schemaVersion = "mattercodex.legacy-data-migration-plan.v1"

const legacyProviderCapability = "chat"

type sourceRow map[string]any

// CredentialEvidence фиксирует уже материализованный immutable Kubernetes
// Secret без раскрытия его содержимого. ContentVersion соответствует exact
// UID:resourceVersion целевого Secret, поэтому runtime broker сможет доказать,
// что продолжает работу именно с принятым owner snapshot.
type CredentialEvidence struct {
	SecretRef          string
	ImmutableSecretRef string
	ContentVersion     string
	ContentSHA256      string
}

// Evidence содержит только deploy-owned, несекретное readback-доказательство
// immutable archive, provider и role image. Business graph всегда выводится
// из source snapshot; этот контракт не принимает target IDs или DML payload.
type Evidence struct {
	ArchiveStoragePrefix                string
	ArchiveStorageVersion               string
	ArchiveRetentionRef                 string
	ArchiveScanPolicyRevision           uint64
	ArchiveScanEvidenceSHA256           string
	ArchiveScannerWorkloadID            string
	ArchiveScannedAt                    time.Time
	ProviderObservedAt                  time.Time
	ProviderObservationRevision         uint64
	ProviderObservedLimit               uint64
	RoleImage                           *controlplanev1.RoleImageRecipeInput
	RoleImageGeneration                 uint64
	RoleImageSpecSHA256                 string
	ImagePolicyRevision                 uint64
	ImagePolicySHA256                   string
	RuntimeContractRevision             uint64
	RuntimeContractSHA256               string
	ImageBuildStagingReference          string
	ImageBuildManifestDigest            string
	ImageBuildProvenanceSHA256          string
	ImageArtifactPromotedReference      string
	ImageAdmissionRevision              uint64
	ImageAdmissionReceiptSHA256         string
	ImageAdmissionReceiptManifestDigest string
	ImageSignatureSHA256                string
	ImagePromotionReadbackSHA256        string
	ImageSBOMSHA256                     string
	ImageVulnerabilityEvidenceSHA256    string
	ImageSignatureIdentity              string
	ImagePromotedAt                     time.Time
	AuthorityPolicyRevision             uint64
	AuthorityPolicySHA256               string
	Credentials                         map[int64]CredentialEvidence
}

type Result struct {
	Report  model.Plan
	Request *controlplanev1.PrepareLegacyGraphMigrationRequest
}

func Build(planID, idempotencyKey, sourceRootReference, sourceRootSHA256 string,
	projection []model.SnapshotRow, sourceDigest string, counts map[string]uint64,
	sourceTableDigests map[string]string, evidence Evidence,
) (Result, error) {
	return build(planID, idempotencyKey, sourceRootReference, sourceRootSHA256,
		projection, sourceDigest, counts, sourceTableDigests, evidence, false)
}

// BuildConfiguration строит bounded plan только для активной конфигурации
// одного проекта. Исторические Session/Turn/Process остаются в immutable
// backup и намеренно не материализуются в новый runtime.
func BuildConfiguration(planID, idempotencyKey, sourceRootReference, sourceRootSHA256 string,
	projection []model.SnapshotRow, sourceDigest string, counts map[string]uint64,
	sourceTableDigests map[string]string, evidence Evidence,
) (Result, error) {
	return build(planID, idempotencyKey, sourceRootReference, sourceRootSHA256,
		projection, sourceDigest, counts, sourceTableDigests, evidence, true)
}

func build(planID, idempotencyKey, sourceRootReference, sourceRootSHA256 string,
	projection []model.SnapshotRow, sourceDigest string, counts map[string]uint64,
	sourceTableDigests map[string]string, evidence Evidence, configurationOnly bool,
) (Result, error) {
	rows, _, err := decodeSource(projection, counts)
	if err != nil || !validSHA(sourceDigest) || !validSHA(sourceRootSHA256) ||
		!validSourceTableDigests(sourceTableDigests) {
		return Result{}, errors.New("source inventory evidence is invalid")
	}
	builder := ownerBuilder{rows: rows, counts: counts, tableDigests: cloneStrings(sourceTableDigests),
		evidence: evidence, operations: make([]*controlplanev1.LegacyGraphOperation, 0, 512),
		configurationOnly: configurationOnly}
	if err := builder.build(); err != nil {
		return Result{}, err
	}
	if err := builder.sortOperations(); err != nil {
		return Result{}, err
	}
	dispositions, ownerSnapshotSHA256, err := builder.dispositions()
	if err != nil {
		return Result{}, err
	}
	request := &controlplanev1.PrepareLegacyGraphMigrationRequest{
		IdempotencyKey: idempotencyKey, PlanId: planID,
		SourceRootReference: sourceRootReference, SourceRootSha256: sourceRootSHA256,
		SourceSnapshotSha256: ownerSnapshotSHA256, SourceDispositions: dispositions,
		Operations: builder.operations,
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(request)
	if err != nil || len(encoded) > 8<<20 || len(builder.operations) > 2000 {
		return Result{}, errors.New("typed owner materialization plan exceeds its closed limit")
	}
	requestSHA256 := digest(encoded)
	report := model.Plan{
		SchemaVersion: schemaVersion, PlanID: planID, SourceSHA256: sourceDigest,
		MappingSHA256: ownerSnapshotSHA256, MaterializationSHA256: requestSHA256,
		MaterializationCount: uint64(len(builder.operations)), OwnerRequestSHA256: requestSHA256,
		Counts: model.Counts{Source: cloneCounts(counts), Mapped: builder.mapped, Archive: builder.archived},
		Violations: map[string]uint64{"ambiguous_target": 0, "broken_lineage": 0,
			"duplicate_source": 0, "orphan_reference": 0, "stale_reference": 0,
			"tenant_mismatch": 0, "unknown_state": 0, "unmaterialized_active": 0,
			"unsupported_state": 0},
	}
	report.PlanSHA256 = digest([]byte(planID + "\x00" + sourceRootSHA256 + "\x00" +
		sourceDigest + "\x00" + ownerSnapshotSHA256 + "\x00" + requestSHA256))
	return Result{Report: report, Request: request}, nil
}

type ownerBuilder struct {
	rows                                               map[string][]sourceRow
	counts, mapped, archived                           map[string]uint64
	tableDigests                                       map[string]string
	evidence                                           Evidence
	operations                                         []*controlplanev1.LegacyGraphOperation
	roles, chats, sessions, turns, attempts, processes map[int64]string
	assignments                                        map[string]string
	providers, providerCredentials                     map[string]string
	credentials                                        map[int64]string
	githubCredentials                                  map[string]string
	promptArtifactsBySHA                               map[string]string
	promptArtifactRefs                                 map[int64]string
	provenanceRefs                                     []string
	projectID                                          int64
	configurationOnly                                  bool
}

func (builder *ownerBuilder) build() error {
	builder.mapped, builder.archived = make(map[string]uint64), make(map[string]uint64)
	builder.roles, builder.chats = make(map[int64]string), make(map[int64]string)
	builder.sessions, builder.turns, builder.attempts = make(map[int64]string), make(map[int64]string), make(map[int64]string)
	builder.processes, builder.assignments = make(map[int64]string), make(map[string]string)
	builder.providers, builder.providerCredentials = make(map[string]string), make(map[string]string)
	builder.credentials = make(map[int64]string)
	builder.githubCredentials = make(map[string]string)
	builder.promptArtifactsBySHA = make(map[string]string)
	builder.promptArtifactRefs = make(map[int64]string)
	if err := validateEvidence(builder.evidence); err != nil {
		return err
	}
	projects := builder.rows["matter_codex_projects"]
	if len(projects) != 1 {
		return errors.New("typed owner plan requires exactly one unambiguous source project")
	}
	project := projects[0]
	builder.projectID = number(project, "id")
	if builder.projectID == 0 || builder.validateProjectBoundary() != nil {
		return errors.New("source graph crosses or lacks the selected project boundary")
	}
	builder.addProject(project)
	builder.addTeam(project)
	for _, row := range builder.rows["matter_codex_chats"] {
		builder.addChat(row)
	}
	for _, row := range builder.rows["matter_codex_agent_roles"] {
		builder.addRolePrerequisites(row)
	}
	for _, row := range builder.rows["matter_codex_agent_session_turns"] {
		builder.addTurnArtifacts(row)
	}
	builder.addProvenanceArtifacts()
	for _, row := range builder.rows["matter_codex_credentials"] {
		if err := builder.addCredential(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_openai_accounts"] {
		if err := builder.addProvider(row); err != nil {
			return err
		}
	}
	if len(builder.providers) == 0 {
		return errors.New("active source graph has no eligible provider evidence")
	}
	for _, row := range builder.rows["matter_codex_agent_roles"] {
		if err := builder.addRoleGraph(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_repositories"] {
		if err := builder.addRepositoryWorkspace(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_chat_participants"] {
		if err := builder.addAssignment(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_automation_schedules"] {
		if err := builder.addSchedule(row); err != nil {
			return err
		}
	}
	if builder.configurationOnly {
		if len(builder.roles) == 0 || len(builder.assignments) == 0 {
			return errors.New("active configuration has no role assignments")
		}
		return nil
	}
	for _, row := range builder.rows["matter_codex_agent_sessions"] {
		if err := builder.indexSession(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_agent_session_turns"] {
		if err := builder.indexTurn(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_process_runs"] {
		builder.indexProcess(row)
	}
	for _, row := range builder.rows["matter_codex_agent_sessions"] {
		if err := builder.addRuntime(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_agent_sessions"] {
		if err := builder.addSession(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_agent_session_turns"] {
		if err := builder.addTurnAndAttempt(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_process_runs"] {
		if err := builder.addProcess(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_agent_delegations"] {
		if err := builder.addDelegation(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_agent_delegation_callback_delivery_manifests"] {
		if err := builder.addCallbackManifest(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_agent_delegation_callback_deliveries"] {
		if err := builder.addCallbackDelivery(row); err != nil {
			return err
		}
	}
	for _, row := range builder.rows["matter_codex_memory_record_versions"] {
		builder.addMemory(row)
	}
	for _, row := range builder.rows["matter_codex_memory_records"] {
		builder.addMemoryRecord(row)
	}
	if len(builder.sessions) == 0 || len(builder.turns) == 0 || len(builder.processes) == 0 {
		return errors.New("full active Session/Turn/Process graph is incomplete")
	}
	return nil
}

func (builder *ownerBuilder) validateProjectBoundary() error {
	for table, rows := range builder.rows {
		for _, row := range rows {
			if _, scoped := row["project_id"]; scoped && number(row, "project_id") != builder.projectID {
				return fmt.Errorf("source table %s has a cross-project row", table)
			}
		}
	}
	return nil
}

func (builder *ownerBuilder) source(table, prefix string, row sourceRow) *controlplanev1.LegacyOperationSource {
	id := sourceID(row)
	ref := table + "/" + id
	return &controlplanev1.LegacyOperationSource{SourceTable: sourceTable(table), SourceRef: ref,
		SourceRevision: sourceRevision(row), SourceSha256: rowDigest(row), LocalRef: prefix + "-" + id}
}

func (builder *ownerBuilder) add(operation *controlplanev1.LegacyGraphOperation, table string) {
	builder.operations = append(builder.operations, operation)
	builder.mapped[table]++
}

func (builder *ownerBuilder) sortOperations() error {
	for _, operation := range builder.operations {
		if ownerOperationRank(operation) < 0 {
			return errors.New("typed owner operation kind is unsupported")
		}
	}
	sort.SliceStable(builder.operations, func(left, right int) bool {
		return ownerOperationRank(builder.operations[left]) < ownerOperationRank(builder.operations[right])
	})
	if len(builder.operations) == 0 || ownerOperationRank(builder.operations[0]) != 0 {
		return errors.New("typed owner plan has no leading project operation")
	}
	return nil
}

func ownerOperationRank(operation *controlplanev1.LegacyGraphOperation) int {
	switch operation.GetOperation().(type) {
	case *controlplanev1.LegacyGraphOperation_Project:
		return 0
	case *controlplanev1.LegacyGraphOperation_Team, *controlplanev1.LegacyGraphOperation_Chat:
		return 1
	case *controlplanev1.LegacyGraphOperation_Artifact, *controlplanev1.LegacyGraphOperation_CredentialBinding,
		*controlplanev1.LegacyGraphOperation_RepositoryWorkspace:
		return 2
	case *controlplanev1.LegacyGraphOperation_RoleDefinition:
		return 3
	case *controlplanev1.LegacyGraphOperation_InstructionSet:
		return 4
	case *controlplanev1.LegacyGraphOperation_ProviderReference:
		return 5
	case *controlplanev1.LegacyGraphOperation_ProviderPool:
		return 6
	case *controlplanev1.LegacyGraphOperation_RoleImageRecipe:
		return 7
	case *controlplanev1.LegacyGraphOperation_ImageBuild:
		return 8
	case *controlplanev1.LegacyGraphOperation_ImageArtifact:
		return 9
	case *controlplanev1.LegacyGraphOperation_Agent:
		return 10
	case *controlplanev1.LegacyGraphOperation_AgentAssignment:
		return 11
	case *controlplanev1.LegacyGraphOperation_Schedule:
		return 12
	case *controlplanev1.LegacyGraphOperation_RuntimeRevision:
		return 13
	case *controlplanev1.LegacyGraphOperation_Session:
		return 14
	case *controlplanev1.LegacyGraphOperation_Turn:
		return 15
	case *controlplanev1.LegacyGraphOperation_TurnAttempt:
		return 16
	case *controlplanev1.LegacyGraphOperation_ProcessRun:
		return 17
	case *controlplanev1.LegacyGraphOperation_DelegationEdge:
		return 18
	case *controlplanev1.LegacyGraphOperation_CallbackManifest:
		return 19
	case *controlplanev1.LegacyGraphOperation_CallbackDelivery:
		return 20
	case *controlplanev1.LegacyGraphOperation_MemoryRecord:
		return 21
	default:
		return -1
	}
}

func (builder *ownerBuilder) addProject(row sourceRow) {
	source := builder.source("matter_codex_projects", "project", row)
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Project{Project: &controlplanev1.LegacyProjectOperation{
		Source: source, Name: nonempty(text(row, "name"), "Legacy project"), Slug: stable(nonempty(text(row, "slug"), "legacy-project")),
		Description: text(row, "description"), Locale: nonempty(text(row, "locale"), "ru"),
	}}}, "matter_codex_projects")
}

func (builder *ownerBuilder) addTeam(row sourceRow) {
	roleRefs := make([]string, 0, len(builder.rows["matter_codex_agent_roles"]))
	for _, role := range builder.rows["matter_codex_agent_roles"] {
		roleRefs = append(roleRefs, "role-definition-"+strconv.FormatInt(number(role, "id"), 10))
	}
	source := builder.source("matter_codex_projects", "team", row)
	source.LocalRef = "team-root"
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Team{Team: &controlplanev1.LegacyTeamOperation{
		Source: source, Name: "Legacy team", StableKey: "legacy-team", ExternalTeamRef: nonempty(text(row, "mattermost_team_id"), "legacy://team/root"),
		RoleDefinitionRefs: roleRefs,
	}}}, "matter_codex_projects")
}

func (builder *ownerBuilder) addRolePrerequisites(row sourceRow) {
	id := number(row, "id")
	idText := strconv.FormatInt(id, 10)
	base := "role-" + strconv.FormatInt(id, 10)
	content := nonempty(text(row, "prompt_template"), text(row, "description"))
	if content == "" {
		content = "Legacy role " + nonempty(text(row, "name"), strconv.FormatInt(id, 10))
	}
	contentSHA := digest([]byte(content))
	artifactSource := builder.source("matter_codex_agent_roles", "artifact-prompt", row)
	artifactSource.LocalRef = "artifact-prompt-" + idText
	canonicalArtifactRef, exists := builder.promptArtifactsBySHA[contentSHA]
	if !exists {
		canonicalArtifactRef = artifactSource.LocalRef
		builder.promptArtifactsBySHA[contentSHA] = canonicalArtifactRef
		builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Artifact{Artifact: builder.artifact(artifactSource,
			"Prompt "+nonempty(text(row, "name"), base), "instruction", "INPUT", contentSHA, uint64(len(content)))}}, "matter_codex_agent_roles")
	}
	builder.promptArtifactRefs[id] = canonicalArtifactRef
	configSource := builder.source("matter_codex_agent_roles", "artifact-role-config", row)
	configSource.LocalRef = "artifact-role-config-" + strconv.FormatInt(id, 10)
	builder.provenanceRefs = append(builder.provenanceRefs, configSource.LocalRef)
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Artifact{Artifact: builder.artifact(configSource,
		"Role configuration "+nonempty(text(row, "name"), base), "legacy-role-configuration", "ARCHIVE", rowDigest(row), sourceBytes(row))}}, "matter_codex_agent_roles")
}

func (builder *ownerBuilder) addTurnArtifacts(row sourceRow) {
	id := strconv.FormatInt(number(row, "id"), 10)
	input := nonempty(text(row, "message"), rowDigest(row))
	inputSource := builder.source("matter_codex_agent_session_turns", "artifact-turn-input", row)
	inputSource.LocalRef = "artifact-turn-input-" + id
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Artifact{Artifact: builder.artifact(inputSource,
		"Turn input "+id, "turn-input", "INPUT", digest([]byte(input)), uint64(len(input)))}}, "matter_codex_agent_session_turns")
	result := nonempty(text(row, "final_message"), text(row, "error_message"))
	if result == "" && number(row, "artifacts") == 0 {
		return
	}
	resultEvidence := result + "\x00" + text(row, "artifacts_sha256")
	resultSource := builder.source("matter_codex_agent_session_turns", "artifact-turn-result", row)
	resultSource.LocalRef = "artifact-turn-result-" + id
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Artifact{Artifact: builder.artifact(resultSource,
		"Turn result "+id, "turn-result", "OUTPUT", digest([]byte(resultEvidence)), uint64(maxInt64(1, int64(len(resultEvidence)))))}}, "matter_codex_agent_session_turns")
}

func (builder *ownerBuilder) addChat(row sourceRow) {
	id := number(row, "id")
	ref := "chat-" + strconv.FormatInt(id, 10)
	builder.chats[id] = ref
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Chat{Chat: &controlplanev1.LegacyChatOperation{
		Source: builder.source("matter_codex_chats", "chat", row), Name: nonempty(text(row, "name"), ref), StableKey: stable(nonempty(text(row, "slug"), ref)),
		RoomType: controlplanev1.RoomType_ROOM_TYPE_COORDINATION, ExternalChannelRef: nonempty(text(row, "mattermost_channel_id"), "legacy://chat/"+ref),
		WorkPolicy: nonempty(text(row, "work_policy"), "managed"),
	}}}, "matter_codex_chats")
}

func (builder *ownerBuilder) addCredential(row sourceRow) error {
	id := number(row, "id")
	ref := "credential-" + strconv.FormatInt(id, 10)
	secretName := text(row, "secret_ref")
	accountName, purpose, principalPrefix := "", "", ""
	for _, account := range builder.rows["matter_codex_openai_accounts"] {
		if number(account, "credential_id") != id {
			continue
		}
		if accountName != "" {
			return errors.New("credential is referenced by multiple accounts")
		}
		accountName = nonempty(text(account, "name"), strconv.FormatInt(number(account, "id"), 10))
		purpose, principalPrefix = "provider-account", "provider:"
	}
	for _, account := range builder.rows["matter_codex_github_accounts"] {
		if number(account, "credential_id") != id {
			continue
		}
		if accountName != "" {
			return errors.New("credential is referenced by multiple accounts")
		}
		accountName = nonempty(text(account, "name"), strconv.FormatInt(number(account, "id"), 10))
		secretName = nonempty(text(account, "secret_ref"), secretName)
		purpose, principalPrefix = "repository-account", "github:"
	}
	if accountName == "" {
		return errors.New("credential has no exact account")
	}
	status := text(row, "status")
	if secretName == "" || purpose == "provider-account" && status != "authorized" ||
		purpose == "repository-account" && status != "configured" {
		return errors.New("credential is not authoritatively eligible")
	}
	evidence, ok := builder.evidence.Credentials[id]
	if !ok || evidence.SecretRef == "" || evidence.ImmutableSecretRef == "" ||
		evidence.ContentVersion == "" || !validSHA(evidence.ContentSHA256) {
		return errors.New("credential immutable snapshot evidence is missing")
	}
	input := &controlplanev1.LegacyCredentialBindingOperation{
		Source: builder.source("matter_codex_credentials", "credential", row), Name: nonempty(text(row, "name"), ref), Purpose: purpose,
		SecretRef: evidence.SecretRef, ImmutableSecretRef: evidence.ImmutableSecretRef,
		PrincipalRef: principalPrefix + stable(accountName), Revision: sourceRevision(row),
		ContentVersion: evidence.ContentVersion, ContentSha256: evidence.ContentSHA256,
	}
	if purpose == "provider-account" {
		input.ProviderCapabilities = []string{legacyProviderCapability}
		input.ObservedLimit = builder.evidence.ProviderObservedLimit
		input.ObservationRevision = builder.evidence.ProviderObservationRevision
		input.ObservedAt = timestamppb.New(builder.evidence.ProviderObservedAt)
	} else {
		builder.githubCredentials[accountName] = ref
	}
	builder.credentials[id] = ref
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_CredentialBinding{CredentialBinding: input}}, "matter_codex_credentials")
	return nil
}

func (builder *ownerBuilder) addProvider(row sourceRow) error {
	if text(row, "status") != "authorized" {
		return errors.New("provider account is not authoritatively eligible")
	}
	id := number(row, "credential_id")
	credential := builder.credentials[id]
	if credential == "" {
		return errors.New("provider account has no exact credential binding")
	}
	name := nonempty(text(row, "name"), strconv.FormatInt(number(row, "id"), 10))
	ref := "provider-" + stable(name)
	builder.providers[name] = ref
	builder.providerCredentials[name] = credential
	sha := rowDigest(row)
	source := builder.source("matter_codex_openai_accounts", "provider", row)
	source.LocalRef = ref
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_ProviderReference{ProviderReference: &controlplanev1.LegacyProviderConnectionReferenceOperation{
		Source: source, Name: name, StableKey: stable(name), Provider: "openai",
		ServerReference: "provider://openai/" + stable(name), ReferenceVersion: sourceRevision(row), ReferenceGeneration: sourceRevision(row), ReferenceSha256: sha,
		MaskedLabel: name, MaskedStatus: "AVAILABLE", Capabilities: []string{legacyProviderCapability}, ObservedAt: timestamppb.New(builder.evidence.ProviderObservedAt),
		ReceiptId:      uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:legacy-provider-receipt\x00"+source.SourceRef)).String(),
		ReceiptVersion: sourceRevision(row), ReceiptSha256: sha, CredentialBindingRef: credential,
	}}}, "matter_codex_openai_accounts")
	return nil
}

func (builder *ownerBuilder) addRepositoryWorkspace(row sourceRow) error {
	id := number(row, "id")
	owner, name := text(row, "owner"), text(row, "name")
	if id == 0 || owner == "" || name == "" || text(row, "status") != "active" {
		return errors.New("repository workspace is not authoritatively eligible")
	}
	accountName := text(row, "github_account_name")
	credentialRef := builder.githubCredentials[accountName]
	if credentialRef == "" {
		return errors.New("repository workspace credential is missing")
	}
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_RepositoryWorkspace{RepositoryWorkspace: &controlplanev1.LegacyRepositoryWorkspaceOperation{
		Source: builder.source("matter_codex_repositories", "repository-workspace", row), Name: owner + "/" + name,
		RepositoryRef: "https://github.com/" + owner + "/" + name, WorkspaceMode: "GIT",
		DefaultBranch: nonempty(text(row, "default_branch"), "main"), CredentialBindingRef: credentialRef,
	}}}, "matter_codex_repositories")
	return nil
}

func (builder *ownerBuilder) addRoleGraph(row sourceRow) error {
	id := number(row, "id")
	idText := strconv.FormatInt(id, 10)
	roleRef := "role-" + idText
	name := nonempty(text(row, "name"), roleRef)
	stableKey := stable(name)
	provider := builder.providers[nonempty(text(row, "openai_account_name"), "primary")]
	if provider == "" {
		return errors.New("role provider reference is missing")
	}
	recipeRef := "recipe-" + idText
	instructionRef := "instruction-" + idText
	poolRef := "pool-" + idText
	roleDefinitionRef := "role-definition-" + idText
	artifactRef := builder.promptArtifactRefs[id]
	if artifactRef == "" {
		return errors.New("role prompt artifact reference is missing")
	}
	roleImage := proto.Clone(builder.evidence.RoleImage).(*controlplanev1.RoleImageRecipeInput)
	buildRef := "image-build-" + idText
	imageRef := "image-artifact-" + idText
	source := builder.source("matter_codex_agent_roles", "role-definition", row)
	source.LocalRef = roleDefinitionRef
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_RoleDefinition{RoleDefinition: &controlplanev1.LegacyRoleDefinitionOperation{
		Source: source, Name: name, StableKey: stableKey, Description: text(row, "description"), Capabilities: roleCapabilities(row), RoleImageRecipeRef: recipeRef,
	}}}, "matter_codex_agent_roles")
	content := nonempty(text(row, "prompt_template"), text(row, "description"))
	if content == "" {
		content = "Legacy role " + name
	}
	contentSHA := digest([]byte(content))
	instructionSource := builder.source("matter_codex_agent_roles", "instruction", row)
	instructionSource.LocalRef = instructionRef
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_InstructionSet{InstructionSet: &controlplanev1.LegacyInstructionSetOperation{
		Source: instructionSource, Name: "Instruction " + name, StableKey: stableKey, Locale: "ru", Content: content, ContentSha256: contentSHA,
		ValidationSha256: contentSHA, ContentArtifactRef: artifactRef,
	}}}, "matter_codex_agent_roles")
	poolSource := builder.source("matter_codex_agent_roles", "pool", row)
	poolSource.LocalRef = poolRef
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_ProviderPool{ProviderPool: &controlplanev1.LegacyProviderPoolOperation{
		Source: poolSource, Name: "Provider pool " + name, StableKey: stableKey, Policy: "weighted", PolicyRevision: 1,
		ObservationMaxAge: durationpb.New(24 * time.Hour), Bindings: []*controlplanev1.LegacyProviderPoolBinding{{ProviderReferenceRef: provider, Weight: 100}},
	}}}, "matter_codex_agent_roles")
	recipeSource := builder.source("matter_codex_agent_roles", "recipe", row)
	recipeSource.LocalRef = recipeRef
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_RoleImageRecipe{RoleImageRecipe: &controlplanev1.LegacyRoleImageRecipeOperation{
		Source: recipeSource, Name: "Role image " + name, Input: roleImage, Generation: builder.evidence.RoleImageGeneration,
		SpecSha256: builder.evidence.RoleImageSpecSHA256, PolicyRevision: builder.evidence.ImagePolicyRevision,
		PolicySha256: builder.evidence.ImagePolicySHA256, RuntimeContractRevision: builder.evidence.RuntimeContractRevision,
		RuntimeContractSha256: builder.evidence.RuntimeContractSHA256,
	}}}, "matter_codex_agent_roles")
	buildSource := builder.source("matter_codex_agent_roles", "image-build", row)
	buildSource.LocalRef = buildRef
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_ImageBuild{ImageBuild: &controlplanev1.LegacyImageBuildOperation{
		Source: buildSource, Name: "Legacy image build " + name, RecipeRef: recipeRef, Attempt: 1,
		TerminalState:          controlplanev1.LifecycleState_LIFECYCLE_STATE_SUCCEEDED,
		TerminalEvidenceSha256: builder.evidence.ImageBuildProvenanceSHA256,
		StagingReference:       builder.evidence.ImageBuildStagingReference, ManifestDigest: builder.evidence.ImageBuildManifestDigest,
		ProvenanceSha256: builder.evidence.ImageBuildProvenanceSHA256,
	}}}, "matter_codex_agent_roles")
	imageSource := builder.source("matter_codex_agent_roles", "image-artifact", row)
	imageSource.LocalRef = imageRef
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_ImageArtifact{ImageArtifact: &controlplanev1.LegacyImageArtifactOperation{
		Source: imageSource, Name: "Legacy image artifact " + name, RecipeRef: recipeRef, ImageBuildRef: buildRef,
		ManifestDigest: builder.evidence.ImageBuildManifestDigest, PromotedReference: builder.evidence.ImageArtifactPromotedReference,
		AdmissionRevision: builder.evidence.ImageAdmissionRevision, AdmissionReceiptSha256: builder.evidence.ImageAdmissionReceiptSHA256,
		AdmissionReceiptManifestDigest: builder.evidence.ImageAdmissionReceiptManifestDigest, SignatureSha256: builder.evidence.ImageSignatureSHA256,
		PromotionReadbackSha256: builder.evidence.ImagePromotionReadbackSHA256, SbomSha256: builder.evidence.ImageSBOMSHA256,
		VulnerabilityEvidenceSha256: builder.evidence.ImageVulnerabilityEvidenceSHA256, SignatureIdentity: builder.evidence.ImageSignatureIdentity,
		PromotedAt: timestamppb.New(builder.evidence.ImagePromotedAt),
	}}}, "matter_codex_agent_roles")
	agentSource := builder.source("matter_codex_agent_roles", "agent", row)
	agentSource.LocalRef = roleRef
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Agent{Agent: &controlplanev1.LegacyAgentOperation{
		Source: agentSource, Name: name, StableKey: stableKey, RoleDefinitionRef: roleDefinitionRef, InstructionSetRef: instructionRef,
		ProviderPoolRef: poolRef, RoleImageRecipeRef: recipeRef, Capabilities: roleCapabilities(row), Enabled: boolean(row, "enabled"),
		BotIdentityRef: nonempty(text(row, "bot_identity"), "legacy://bot/"+stableKey), BotUsername: stableKey,
		BotTeamRef: "legacy://team/root", BotMaskedStatus: "AVAILABLE",
		BotReceiptId:      uuid.NewSHA1(uuid.NameSpaceURL, []byte("mattercodex:legacy-agent-bot-receipt\x00"+agentSource.SourceRef)).String(),
		BotReceiptVersion: sourceRevision(row), BotReceiptSha256: rowDigest(row), BotProviderRevision: sourceRevision(row), BotProviderGeneration: sourceRevision(row),
	}}}, "matter_codex_agent_roles")
	builder.roles[id] = roleRef
	return nil
}

func (builder *ownerBuilder) addAssignment(row sourceRow) error {
	roleID, chatID := number(row, "role_id"), number(row, "chat_id")
	role, chat := builder.roles[roleID], builder.chats[chatID]
	if role == "" || chat == "" {
		return errors.New("chat participant owner boundary is incomplete")
	}
	ref := "assignment-" + strconv.FormatInt(number(row, "id"), 10)
	builder.assignments[assignmentKey(roleID, chatID)] = ref
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_AgentAssignment{AgentAssignment: &controlplanev1.LegacyAgentAssignmentOperation{
		Source: builder.source("matter_codex_chat_participants", "assignment", row), Name: ref, AgentRef: role, RoomRef: chat,
		AssignmentGeneration: sourceRevision(row),
	}}}, "matter_codex_chat_participants")
	return nil
}

func (builder *ownerBuilder) addSchedule(row sourceRow) error {
	roleID, chatID := number(row, "target_agent_role_id"), number(row, "target_chat_id")
	role, chat, assignment := builder.roles[roleID], builder.chats[chatID], builder.assignments[assignmentKey(roleID, chatID)]
	if role == "" || chat == "" || assignment == "" || text(row, "owner_mattermost_user_id") == "" {
		return errors.New("schedule target graph is incomplete")
	}
	roleText := strconv.FormatInt(roleID, 10)
	state := controlplanev1.LifecycleState_LIFECYCLE_STATE_PAUSED
	if boolean(row, "enabled") {
		state = controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE
	}
	localTime := nonempty(text(row, "local_time"), "00:00")
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Schedule{Schedule: &controlplanev1.LegacyScheduleOperation{
		Source: builder.source("matter_codex_automation_schedules", "schedule", row), Name: nonempty(text(row, "name"), "Legacy schedule"),
		StableKey:      stable(nonempty(text(row, "public_id"), "schedule-"+strconv.FormatInt(number(row, "id"), 10))),
		CronExpression: cronDaily(localTime), Timezone: nonempty(text(row, "time_zone"), "UTC"),
		OverlapPolicy: controlplanev1.ScheduleOverlapPolicy_SCHEDULE_OVERLAP_POLICY_FORBID,
		MisfirePolicy: controlplanev1.ScheduleMisfirePolicy_SCHEDULE_MISFIRE_POLICY_SKIP,
		AgentRef:      role, AssignmentRef: assignment, InstructionSetRef: "instruction-" + roleText,
		ProviderPoolRef: "pool-" + roleText, RoomRef: chat, RoleImageRecipeRef: "recipe-" + roleText,
		NextRunAt: timestamp(row, "next_run_at"), State: state, Calendar: "gregorian", MisfireGrace: durationpb.New(5 * time.Minute),
		DeliveryPolicy: "callback", MaximumAttempts: 1, InitialBackoff: durationpb.New(time.Second), MaximumBackoff: durationpb.New(time.Minute),
		DeadLetterAfter: durationpb.New(24 * time.Hour), SessionPolicy: "reuse", NotificationPolicy: "owner", MaximumExecutionDuration: durationpb.New(time.Hour),
		Coalesce: false,
	}}}, "matter_codex_automation_schedules")
	return nil
}

func (builder *ownerBuilder) indexSession(row sourceRow) error {
	id := number(row, "id")
	roleID, chatID := number(row, "role_id"), number(row, "chat_id")
	if builder.roles[roleID] == "" || builder.chats[chatID] == "" || builder.assignments[assignmentKey(roleID, chatID)] == "" {
		return errors.New("session owner graph is incomplete")
	}
	builder.sessions[id] = "session-" + strconv.FormatInt(id, 10)
	return nil
}

func (builder *ownerBuilder) indexTurn(row sourceRow) error {
	id, sessionID := number(row, "id"), number(row, "session_id")
	if builder.sessions[sessionID] == "" {
		return errors.New("turn session lineage is orphaned")
	}
	builder.turns[id] = "turn-" + strconv.FormatInt(id, 10)
	builder.attempts[id] = "attempt-" + strconv.FormatInt(id, 10)
	return nil
}

func (builder *ownerBuilder) indexProcess(row sourceRow) {
	id := number(row, "id")
	builder.processes[id] = "process-" + strconv.FormatInt(id, 10)
}

func (builder *ownerBuilder) addRuntime(row sourceRow) error {
	sessionID, roleID, chatID := number(row, "id"), number(row, "role_id"), number(row, "chat_id")
	role, chat := builder.roles[roleID], builder.chats[chatID]
	assignment := builder.assignments[assignmentKey(roleID, chatID)]
	provider := builder.providers[nonempty(roleProviderName(builder.rows["matter_codex_agent_roles"], roleID), "primary")]
	if role == "" || chat == "" || assignment == "" || provider == "" {
		return errors.New("runtime revision dependency is incomplete")
	}
	providerName := nonempty(roleProviderName(builder.rows["matter_codex_agent_roles"], roleID), "primary")
	credentialRef := builder.providerCredentials[providerName]
	if credentialRef == "" {
		return errors.New("runtime credential is missing")
	}
	roleText := strconv.FormatInt(roleID, 10)
	created := timestamp(row, "created_at")
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_RuntimeRevision{RuntimeRevision: &controlplanev1.LegacyRuntimeRevisionOperation{
		Source: builder.source("matter_codex_agent_sessions", "runtime", row), Name: "Runtime " + builder.sessions[sessionID], SessionRef: builder.sessions[sessionID],
		ChatRef: chat, AgentRef: role, AssignmentRef: assignment, RoleDefinitionRef: "role-definition-" + roleText,
		InstructionSetRef: "instruction-" + roleText, ProviderPoolRef: "pool-" + roleText, ProviderCredentialRef: credentialRef,
		RoleImageRecipeRef: "recipe-" + roleText, ImageBuildRef: "image-build-" + roleText, ImageArtifactRef: "image-artifact-" + roleText,
		PromptArtifactRef: builder.promptArtifactRefs[roleID], ImageReference: builder.evidence.ImageArtifactPromotedReference,
		ProviderAccountName: stable(providerName), CodexModel: "gpt-5",
		CodexSandbox:        nonempty(roleField(builder.rows["matter_codex_agent_roles"], roleID, "sandbox_mode"), "danger-full-access"),
		CodexApprovalPolicy: "never", AuthorityPolicyRevision: builder.evidence.AuthorityPolicyRevision,
		AuthorityPolicySha256: builder.evidence.AuthorityPolicySHA256, Components: runtimeComponents(builder.provenanceRefs), CreatedAt: created,
	}}}, "matter_codex_agent_sessions")
	return nil
}

func (builder *ownerBuilder) addSession(row sourceRow) error {
	id, roleID, chatID := number(row, "id"), number(row, "role_id"), number(row, "chat_id")
	role, chat := builder.roles[roleID], builder.chats[chatID]
	assignment := builder.assignments[assignmentKey(roleID, chatID)]
	providerName := nonempty(roleProviderName(builder.rows["matter_codex_agent_roles"], roleID), "primary")
	if role == "" || chat == "" || assignment == "" || builder.providers[providerName] == "" {
		return errors.New("session dependency is incomplete")
	}
	state, err := sessionState(text(row, "status"))
	if err != nil {
		return err
	}
	lastSequence := uint64(0)
	for _, turn := range builder.rows["matter_codex_agent_session_turns"] {
		if number(turn, "session_id") == id {
			lastSequence++
		}
	}
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Session{Session: &controlplanev1.LegacySessionOperation{
		Source: builder.source("matter_codex_agent_sessions", "session", row), Name: nonempty(text(row, "session_key"), builder.sessions[id]),
		AgentRef: role, ProviderPoolRef: "pool-" + strconv.FormatInt(roleID, 10), AssignmentRef: assignment, ChatRef: chat,
		LastTurnSequence: lastSequence, ArchiveRef: archiveRef(row), State: state,
	}}}, "matter_codex_agent_sessions")
	return nil
}

func (builder *ownerBuilder) addTurnAndAttempt(row sourceRow) error {
	id, sessionID := number(row, "id"), number(row, "session_id")
	sessionRef := builder.sessions[sessionID]
	if sessionRef == "" {
		return errors.New("turn session is missing")
	}
	sequence := uint64(1)
	predecessor := ""
	for _, candidate := range builder.rows["matter_codex_agent_session_turns"] {
		if number(candidate, "session_id") != sessionID || number(candidate, "id") >= id {
			continue
		}
		sequence++
		if predecessor == "" || number(candidate, "id") > parseRefID(predecessor) {
			predecessor = builder.turns[number(candidate, "id")]
		}
	}
	state, attemptState, finished, err := turnStates(text(row, "status"))
	if err != nil {
		return err
	}
	inputSHA := digest([]byte(nonempty(text(row, "message"), rowDigest(row))))
	processRef, parentRef := builder.processForTurn(id)
	turnSource := builder.source("matter_codex_agent_session_turns", "turn", row)
	turnSource.LocalRef = builder.turns[id]
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Turn{Turn: &controlplanev1.LegacyTurnOperation{
		Source: turnSource, Name: nonempty(text(row, "run_id"), builder.turns[id]), SessionRef: sessionRef, Sequence: sequence,
		SourceTurnRef:      nonempty(text(row, "mattermost_post_id"), "legacy://turn/"+strconv.FormatInt(id, 10)),
		PromptArtifactRef:  "artifact-turn-input-" + strconv.FormatInt(id, 10),
		RuntimeRevisionRef: "runtime-" + strconv.FormatInt(sessionID, 10), PredecessorTurnRef: predecessor, ParentTurnRef: parentRef,
		ProcessRunRef: processRef, ResultArtifactRef: turnResultArtifactRef(row), Attempt: 1,
		EffectiveInputSha256: inputSHA, Outcome: turnOutcome(row), State: state,
	}}}, "matter_codex_agent_session_turns")
	attemptSource := builder.source("matter_codex_agent_session_turns", "attempt", row)
	attemptSource.LocalRef = builder.attempts[id]
	started := timeValue(row, "started_at")
	if started.IsZero() {
		started = timeValue(row, "created_at")
	}
	var finishedAt *timestamppb.Timestamp
	if finished {
		value := timeValue(row, "finished_at")
		if value.IsZero() {
			value = timeValue(row, "updated_at")
		}
		finishedAt = timestamppb.New(value)
	}
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_TurnAttempt{TurnAttempt: &controlplanev1.LegacyTurnAttemptOperation{
		Source: attemptSource, TurnRef: builder.turns[id], Attempt: 1, ImmutableInputSha256: inputSHA,
		RuntimeRevisionRef: "runtime-" + strconv.FormatInt(sessionID, 10), State: attemptState, Outcome: turnOutcome(row),
		StartedAt: timestamppb.New(started), FinishedAt: finishedAt,
	}}}, "matter_codex_agent_session_turns")
	return nil
}

func (builder *ownerBuilder) processForTurn(turnID int64) (string, string) {
	for _, link := range builder.rows["matter_codex_process_turns"] {
		if number(link, "turn_id") != turnID {
			continue
		}
		parent := ""
		if parentID := number(link, "parent_turn_id"); parentID > 0 {
			parent = builder.turns[parentID]
		}
		return builder.processes[number(link, "process_run_id")], parent
	}
	return "", ""
}

func (builder *ownerBuilder) addProcess(row sourceRow) error {
	id := number(row, "id")
	processRef := builder.processes[id]
	links := make([]sourceRow, 0)
	for _, link := range builder.rows["matter_codex_process_turns"] {
		if number(link, "process_run_id") == id {
			links = append(links, link)
		}
	}
	if len(links) == 0 {
		return errors.New("process has no exact Session/Turn/Attempt tuple")
	}
	rootActorRef, rootTriggerRef := text(row, "root_initiator_user_id"), text(row, "root_trigger_post_id")
	if rootActorRef == "" || rootTriggerRef == "" {
		return errors.New("process root actor or trigger provenance is missing")
	}
	sort.Slice(links, func(i, j int) bool { return number(links[i], "turn_id") < number(links[j], "turn_id") })
	rootTurnID := number(links[0], "turn_id")
	rootSessionID := turnSession(builder.rows["matter_codex_agent_session_turns"], rootTurnID)
	parentProcessRef, launchingTurnRef, launchingAttemptRef, delegationRef, targetSessionRef, targetTurnRef, targetAttemptRef := "", "", "", "", "", "", ""
	for _, delegation := range builder.rows["matter_codex_agent_delegations"] {
		targetID := number(delegation, "target_turn_id")
		candidateProcess, _ := builder.processForTurn(targetID)
		if candidateProcess != processRef {
			continue
		}
		launchID := number(delegation, "source_turn_id")
		parentProcessRef, _ = builder.processForTurn(launchID)
		if parentProcessRef == "" {
			return errors.New("child process parent provenance is missing")
		}
		launchingTurnRef, launchingAttemptRef = builder.turns[launchID], builder.attempts[launchID]
		delegationRef = "delegation-" + strconv.FormatInt(number(delegation, "id"), 10)
		targetSessionRef, targetTurnRef, targetAttemptRef = builder.sessions[number(delegation, "target_session_id")], builder.turns[targetID], builder.attempts[targetID]
		parentRootTurn, parentRootSession := builder.rootTupleForProcess(parentProcessRef)
		if parentRootTurn == 0 || parentRootSession == 0 {
			return errors.New("child process inherited root provenance is missing")
		}
		rootTurnID, rootSessionID = parentRootTurn, parentRootSession
		break
	}
	if builder.turns[rootTurnID] == "" || builder.sessions[rootSessionID] == "" {
		return errors.New("process root lineage is orphaned")
	}
	policyRevision, policySHA := builder.legacyPolicy(row)
	if policyRevision == 0 || !validSHA(policySHA) {
		return errors.New("process legacy policy evidence is missing")
	}
	state, err := processState(text(row, "status"))
	if err != nil {
		return err
	}
	immutable := digest([]byte(rowDigest(row) + "\x00" + builder.turns[rootTurnID] + "\x00" + builder.attempts[rootTurnID]))
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_ProcessRun{ProcessRun: &controlplanev1.LegacyProcessRunOperation{
		Source: builder.source("matter_codex_process_runs", "process", row), Name: nonempty(text(row, "public_id"), processRef),
		RootSessionRef: builder.sessions[rootSessionID], RootTurnRef: builder.turns[rootTurnID], RootAttemptRef: builder.attempts[rootTurnID],
		RuntimeRevisionRef: "runtime-" + strconv.FormatInt(func() int64 {
			if targetSessionRef != "" {
				return parseRefID(targetSessionRef)
			}
			return rootSessionID
		}(), 10),
		ParentProcessRef: parentProcessRef, LaunchingTurnRef: launchingTurnRef, LaunchingAttemptRef: launchingAttemptRef,
		DelegationRef: delegationRef, TargetSessionRef: targetSessionRef, TargetTurnRef: targetTurnRef, TargetAttemptRef: targetAttemptRef,
		ImmutableInputSha256: immutable,
		LegacyPolicyRevision: policyRevision, LegacyPolicySha256: policySHA,
		PlaybookRef: nonempty(text(row, "playbook_ref"), "legacy://playbook/default"), RootTriggerRef: rootTriggerRef,
		Outcome: nonempty(text(row, "outcome"), strings.ToLower(text(row, "status"))), State: state,
	}}}, "matter_codex_process_runs")
	return nil
}

func (builder *ownerBuilder) rootTupleForProcess(processRef string) (int64, int64) {
	processID := parseRefID(processRef)
	links := make([]sourceRow, 0)
	for _, link := range builder.rows["matter_codex_process_turns"] {
		if number(link, "process_run_id") == processID {
			links = append(links, link)
		}
	}
	if len(links) == 0 {
		return 0, 0
	}
	sort.Slice(links, func(i, j int) bool { return number(links[i], "turn_id") < number(links[j], "turn_id") })
	turnID := number(links[0], "turn_id")
	for _, delegation := range builder.rows["matter_codex_agent_delegations"] {
		candidate, _ := builder.processForTurn(number(delegation, "target_turn_id"))
		if candidate != processRef {
			continue
		}
		parent, _ := builder.processForTurn(number(delegation, "source_turn_id"))
		if parent != "" {
			return builder.rootTupleForProcess(parent)
		}
	}
	return turnID, turnSession(builder.rows["matter_codex_agent_session_turns"], turnID)
}

func (builder *ownerBuilder) legacyPolicy(process sourceRow) (uint64, string) {
	policyID := number(process, "policy_revision_id")
	for _, row := range builder.rows["matter_codex_policy_revisions"] {
		if number(row, "id") == policyID {
			domain := []string{"revision:" + rowDigest(row)}
			for _, capability := range builder.rows["matter_codex_role_capabilities"] {
				if number(capability, "policy_revision_id") == policyID {
					domain = append(domain, "capability:"+rowDigest(capability))
				}
			}
			for _, relationship := range builder.rows["matter_codex_role_relationship_policies"] {
				if number(relationship, "policy_revision_id") == policyID {
					domain = append(domain, "relationship:"+rowDigest(relationship))
				}
			}
			sort.Strings(domain)
			return uint64(number(row, "version")), digest([]byte(strings.Join(domain, "\x00")))
		}
	}
	return 0, ""
}

func (builder *ownerBuilder) addDelegation(row sourceRow) error {
	sourceTurnID, targetTurnID := number(row, "source_turn_id"), number(row, "target_turn_id")
	parentProcess, _ := builder.processForTurn(sourceTurnID)
	childProcess, _ := builder.processForTurn(targetTurnID)
	parentSessionID, targetSessionID := number(row, "source_session_id"), number(row, "target_session_id")
	childRole := builder.roles[number(row, "target_role_id")]
	if parentProcess == "" || childProcess == "" || builder.sessions[parentSessionID] == "" || builder.sessions[targetSessionID] == "" ||
		childRole == "" || builder.turns[sourceTurnID] == "" || builder.attempts[sourceTurnID] == "" ||
		builder.turns[targetTurnID] == "" || builder.attempts[targetTurnID] == "" {
		return errors.New("delegation lineage is incomplete")
	}
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_DelegationEdge{DelegationEdge: &controlplanev1.LegacyDelegationEdgeOperation{
		Source: builder.source("matter_codex_agent_delegations", "delegation", row), ParentProcessRef: parentProcess,
		ParentSessionRef: builder.sessions[parentSessionID], ParentTurnRef: builder.turns[sourceTurnID], ParentAttemptRef: builder.attempts[sourceTurnID],
		ChildRoleRef: childRole, ChildSessionRef: builder.sessions[targetSessionID], ChildTurnRef: builder.turns[targetTurnID],
		ChildAttemptRef: builder.attempts[targetTurnID], GrantGeneration: sourceRevision(row), ChildProcessRef: childProcess,
	}}}, "matter_codex_agent_delegations")
	return nil
}

func (builder *ownerBuilder) addCallbackManifest(row sourceRow) error {
	delegationID := number(row, "delegation_id")
	processRef := ""
	for _, delegation := range builder.rows["matter_codex_agent_delegations"] {
		if number(delegation, "id") == delegationID {
			processRef, _ = builder.processForTurn(number(delegation, "callback_turn_id"))
		}
	}
	if processRef == "" {
		return errors.New("callback process provenance is incomplete")
	}
	destinations := make([]string, 0, 2)
	callbackRunID := text(row, "callback_run_id")
	for _, delivery := range builder.rows["matter_codex_agent_delegation_callback_deliveries"] {
		if number(delivery, "delegation_id") == delegationID && text(delivery, "callback_run_id") == callbackRunID {
			destinations = append(destinations, text(delivery, "destination"))
		}
	}
	sort.Strings(destinations)
	if len(destinations) == 0 || int64(len(destinations)) != number(row, "expected_count") {
		return errors.New("callback manifest destination set is incomplete")
	}
	for index := 1; index < len(destinations); index++ {
		if destinations[index] == destinations[index-1] {
			return errors.New("callback manifest destination set is duplicated")
		}
	}
	source := builder.source("matter_codex_agent_delegation_callback_delivery_manifests", "callback-manifest", row)
	source.LocalRef = "callback-manifest-" + strconv.FormatInt(delegationID, 10)
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_CallbackManifest{CallbackManifest: &controlplanev1.LegacyCallbackManifestOperation{
		Source:        source,
		DelegationRef: "delegation-" + strconv.FormatInt(delegationID, 10), CallbackProcessRef: processRef, Destinations: destinations,
		ManifestSha256: hexText(row, "plan_sha256"),
	}}}, "matter_codex_agent_delegation_callback_delivery_manifests")
	return nil
}

func (builder *ownerBuilder) addCallbackDelivery(row sourceRow) error {
	state := controlplanev1.LegacyCallbackDeliveryState_LEGACY_CALLBACK_DELIVERY_STATE_UNSPECIFIED
	switch text(row, "status") {
	case "delivered":
		state = controlplanev1.LegacyCallbackDeliveryState_LEGACY_CALLBACK_DELIVERY_STATE_DELIVERED
	case "blocked":
		state = controlplanev1.LegacyCallbackDeliveryState_LEGACY_CALLBACK_DELIVERY_STATE_FAILED
	default:
		return errors.New("nonterminal callback delivery blocks migration")
	}
	delivered := timestamp(row, "delivered_at")
	if delivered == nil {
		return errors.New("callback delivery timestamp is missing")
	}
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_CallbackDelivery{CallbackDelivery: &controlplanev1.LegacyCallbackDeliveryOperation{
		Source:              builder.source("matter_codex_agent_delegation_callback_deliveries", "callback-delivery", row),
		CallbackManifestRef: "callback-manifest-" + strconv.FormatInt(number(row, "delegation_id"), 10), Destination: text(row, "destination"),
		ReceiptSha256: hexText(row, "payload_sha256"), TerminalState: state, DeliveredAt: delivered,
	}}}, "matter_codex_agent_delegation_callback_deliveries")
	return nil
}

func (builder *ownerBuilder) addMemory(row sourceRow) {
	recordID := number(row, "record_id")
	state := controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE
	for _, record := range builder.rows["matter_codex_memory_records"] {
		if number(record, "id") == recordID && text(record, "status") != "active" {
			state = controlplanev1.LifecycleState_LIFECYCLE_STATE_ARCHIVED
		}
	}
	content := text(row, "content")
	sha := text(row, "content_hash")
	if !validSHA(sha) {
		sha = digest([]byte(content))
	}
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_MemoryRecord{MemoryRecord: &controlplanev1.LegacyMemoryRecordOperation{
		Source: builder.source("matter_codex_memory_record_versions", "memory", row), Name: nonempty(text(row, "title"), "Legacy memory"),
		MemoryKind: "coordination", Content: content, ContentSha256: sha, SourceVersion: uint64(number(row, "version")), State: state,
	}}}, "matter_codex_memory_record_versions")
}

func (builder *ownerBuilder) addMemoryRecord(row sourceRow) {
	id := number(row, "id")
	var latest sourceRow
	for _, version := range builder.rows["matter_codex_memory_record_versions"] {
		if number(version, "record_id") == id && (latest == nil || number(version, "version") > number(latest, "version")) {
			latest = version
		}
	}
	content, sha := text(latest, "content"), text(latest, "content_hash")
	if content == "" {
		content = "Legacy memory metadata " + rowDigest(row)
	}
	if !validSHA(sha) {
		sha = digest([]byte(content))
	}
	state := controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE
	if text(row, "status") != "active" {
		state = controlplanev1.LifecycleState_LIFECYCLE_STATE_ARCHIVED
	}
	builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_MemoryRecord{MemoryRecord: &controlplanev1.LegacyMemoryRecordOperation{
		Source: builder.source("matter_codex_memory_records", "memory-record", row), Name: "Legacy memory " + strconv.FormatInt(id, 10),
		MemoryKind: nonempty(text(row, "scope"), "coordination"), Content: content, ContentSha256: sha,
		SourceVersion: uint64(maxInt64(1, number(latest, "version"))), State: state,
	}}}, "matter_codex_memory_records")
}

// addProvenanceArtifacts сохраняет exact rows конфигурационных таблиц, у
// которых нет самостоятельного target aggregate: они становятся immutable
// CLEAN Artifact evidence и затем входят в RuntimeRevision provenance. Это не
// DML dispatcher: target kind и все поля закрыты compile-time Proto oneof.
func (builder *ownerBuilder) addProvenanceArtifacts() {
	for _, table := range []string{
		"matter_codex_agent_profiles", "matter_codex_agent_prompt_templates",
		"matter_codex_agent_role_runtime_variables", "matter_codex_agent_runs", "matter_codex_cluster_admin_bot_bindings",
		"matter_codex_cluster_admin_dependencies",
		"matter_codex_cluster_admin_prompt_templates", "matter_codex_cluster_admin_runtime_variable_bindings",
		"matter_codex_cluster_admin_session_bindings", "matter_codex_cluster_admin_subjects",
		"matter_codex_github_accounts", "matter_codex_interaction_capabilities",
		"matter_codex_mattermost_bot_identities", "matter_codex_policy_revisions",
		"matter_codex_process_turns", "matter_codex_project_repositories",
		"matter_codex_project_runtime_variables",
		"matter_codex_role_capabilities", "matter_codex_role_relationship_policies",
		"matter_codex_thread_contexts", "matter_codex_chat_repositories",
	} {
		for _, row := range builder.rows[table] {
			source := builder.source(table, "provenance-"+stable(strings.TrimPrefix(table, "matter_codex_")), row)
			builder.provenanceRefs = append(builder.provenanceRefs, source.LocalRef)
			sha := rowDigest(row)
			builder.add(&controlplanev1.LegacyGraphOperation{Operation: &controlplanev1.LegacyGraphOperation_Artifact{Artifact: builder.artifact(source,
				"Legacy provenance "+strings.TrimPrefix(table, "matter_codex_"), "legacy-provenance", "ARCHIVE", sha, sourceBytes(row))}}, table)
		}
	}
}

func (builder *ownerBuilder) artifact(source *controlplanev1.LegacyOperationSource, name, kind, direction, sha string, size uint64) *controlplanev1.LegacyArtifactOperation {
	if size == 0 {
		size = 1
	}
	storage := strings.TrimSuffix(builder.evidence.ArchiveStoragePrefix, "/") + "/" + source.GetSourceTable().String() + "/" + sha + "?version=" + builder.evidence.ArchiveStorageVersion
	return &controlplanev1.LegacyArtifactOperation{Source: source, Name: name, ArtifactKind: stable(kind), Direction: direction,
		StorageRef: storage, StorageVersion: builder.evidence.ArchiveStorageVersion, SizeBytes: size, MediaType: "application/json",
		Sha256: sha, RetentionPolicyRef: builder.evidence.ArchiveRetentionRef, ScanPolicyRevision: builder.evidence.ArchiveScanPolicyRevision,
		ScanEvidenceSha256: builder.evidence.ArchiveScanEvidenceSHA256, ScannerWorkloadId: builder.evidence.ArchiveScannerWorkloadID,
		ScannedAt: timestamppb.New(builder.evidence.ArchiveScannedAt)}
}

func (builder *ownerBuilder) dispositions() ([]*controlplanev1.LegacySourceDisposition, string, error) {
	proofs := make(map[string]map[string]struct{})
	for _, operation := range builder.operations {
		source := operationSource(operation)
		if source == nil {
			return nil, "", errors.New("typed owner operation source is missing")
		}
		table := sourceTableName(source.SourceTable)
		if proofs[table] == nil {
			proofs[table] = make(map[string]struct{})
		}
		proofs[table][source.SourceRef] = struct{}{}
	}
	type canonicalDisposition struct {
		SourceTable         string `json:"sourceTable"`
		Disposition         string `json:"disposition"`
		RowCount            uint64 `json:"rowCount"`
		SourceSHA256        string `json:"sourceSha256"`
		TerminalStateSHA256 string `json:"terminalStateSha256,omitempty"`
	}
	dispositions := make([]*controlplanev1.LegacySourceDisposition, 0, len(inventory.Tables))
	canonical := make([]canonicalDisposition, 0, len(inventory.Tables))
	for _, table := range inventory.Tables {
		count := builder.counts[table]
		sourceSHA := builder.tableDigests[table]
		enumTable := sourceTable(table)
		kind := controlplanev1.LegacySourceDispositionKind_LEGACY_SOURCE_DISPOSITION_KIND_REJECT_NONEMPTY
		terminal := ""
		switch {
		case table == "matter_codex_cluster_admin_bindings":
			if count != 0 {
				return nil, "", errors.New("legacy cluster admin authority must be empty")
			}
		case terminalSourceTable(table):
			if count > 0 {
				if err := validateTerminalRows(table, builder.rows[table]); err != nil {
					return nil, "", err
				}
				kind = controlplanev1.LegacySourceDispositionKind_LEGACY_SOURCE_DISPOSITION_KIND_ARCHIVE_TERMINAL
				terminal = digest([]byte("terminal\x00" + table + "\x00" + sourceSHA))
				builder.archived[table] = count
			}
		case count > 0:
			kind = controlplanev1.LegacySourceDispositionKind_LEGACY_SOURCE_DISPOSITION_KIND_MATERIALIZE
			if uint64(len(proofs[canonicalSourceTableName(table)])) != count {
				return nil, "", fmt.Errorf("source table %s is not fully represented by typed operations", table)
			}
			builder.mapped[table] = count
		}
		dispositions = append(dispositions, &controlplanev1.LegacySourceDisposition{SourceTable: enumTable, Disposition: kind, RowCount: count, SourceSha256: sourceSHA, TerminalStateSha256: terminal})
		canonical = append(canonical, canonicalDisposition{SourceTable: sourceTableName(enumTable), Disposition: dispositionName(kind), RowCount: count, SourceSHA256: sourceSHA, TerminalStateSHA256: terminal})
	}
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].SourceTable < canonical[j].SourceTable })
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, "", errors.New("encode owner source dispositions")
	}
	return dispositions, digest(encoded), nil
}

func decodeSource(rows []model.SnapshotRow, expected map[string]uint64) (map[string][]sourceRow, map[string]string, error) {
	rows = append([]model.SnapshotRow(nil), rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Table != rows[j].Table {
			return rows[i].Table < rows[j].Table
		}
		return bytes.Compare(rows[i].Payload, rows[j].Payload) < 0
	})
	result := make(map[string][]sourceRow)
	digests := make(map[string]string)
	hashes := make(map[string]hashWriter)
	seenCounts := make(map[string]uint64)
	for _, table := range inventory.Tables {
		hashes[table] = hashWriter{hash: sha256.New()}
		writeFramed(hashes[table].hash, []byte(table))
	}
	for _, entry := range rows {
		if !inventory.Contains(entry.Table) {
			return nil, nil, errors.New("source inventory contains an unknown table")
		}
		if len(entry.Payload) == 0 {
			continue
		}
		var row sourceRow
		decoder := json.NewDecoder(bytes.NewReader(entry.Payload))
		decoder.UseNumber()
		if decoder.Decode(&row) != nil {
			return nil, nil, errors.New("decode source inventory row")
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return nil, nil, errors.New("source inventory row has trailing data")
		}
		result[entry.Table] = append(result[entry.Table], row)
		seenCounts[entry.Table]++
		writer := hashes[entry.Table]
		writeFramed(writer.hash, entry.Payload)
		hashes[entry.Table] = writer
	}
	for _, table := range inventory.Tables {
		if _, ok := expected[table]; !ok || seenCounts[table] != expected[table] {
			return nil, nil, errors.New("source inventory projection is incomplete")
		}
		digests[table] = hex.EncodeToString(hashes[table].hash.Sum(nil))
	}
	return result, digests, nil
}

type hashWriter struct{ hash hash.Hash }

func writeFramed(writer interface{ Write([]byte) (int, error) }, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = writer.Write(size[:])
	_, _ = writer.Write(value)
}

func operationSource(operation *controlplanev1.LegacyGraphOperation) *controlplanev1.LegacyOperationSource {
	switch value := operation.GetOperation().(type) {
	case *controlplanev1.LegacyGraphOperation_Project:
		return value.Project.Source
	case *controlplanev1.LegacyGraphOperation_Team:
		return value.Team.Source
	case *controlplanev1.LegacyGraphOperation_Chat:
		return value.Chat.Source
	case *controlplanev1.LegacyGraphOperation_Artifact:
		return value.Artifact.Source
	case *controlplanev1.LegacyGraphOperation_CredentialBinding:
		return value.CredentialBinding.Source
	case *controlplanev1.LegacyGraphOperation_RepositoryWorkspace:
		return value.RepositoryWorkspace.Source
	case *controlplanev1.LegacyGraphOperation_RoleDefinition:
		return value.RoleDefinition.Source
	case *controlplanev1.LegacyGraphOperation_InstructionSet:
		return value.InstructionSet.Source
	case *controlplanev1.LegacyGraphOperation_ProviderReference:
		return value.ProviderReference.Source
	case *controlplanev1.LegacyGraphOperation_ProviderPool:
		return value.ProviderPool.Source
	case *controlplanev1.LegacyGraphOperation_RoleImageRecipe:
		return value.RoleImageRecipe.Source
	case *controlplanev1.LegacyGraphOperation_ImageBuild:
		return value.ImageBuild.Source
	case *controlplanev1.LegacyGraphOperation_ImageArtifact:
		return value.ImageArtifact.Source
	case *controlplanev1.LegacyGraphOperation_Agent:
		return value.Agent.Source
	case *controlplanev1.LegacyGraphOperation_AgentAssignment:
		return value.AgentAssignment.Source
	case *controlplanev1.LegacyGraphOperation_Schedule:
		return value.Schedule.Source
	case *controlplanev1.LegacyGraphOperation_RuntimeRevision:
		return value.RuntimeRevision.Source
	case *controlplanev1.LegacyGraphOperation_Session:
		return value.Session.Source
	case *controlplanev1.LegacyGraphOperation_Turn:
		return value.Turn.Source
	case *controlplanev1.LegacyGraphOperation_TurnAttempt:
		return value.TurnAttempt.Source
	case *controlplanev1.LegacyGraphOperation_ProcessRun:
		return value.ProcessRun.Source
	case *controlplanev1.LegacyGraphOperation_DelegationEdge:
		return value.DelegationEdge.Source
	case *controlplanev1.LegacyGraphOperation_CallbackManifest:
		return value.CallbackManifest.Source
	case *controlplanev1.LegacyGraphOperation_CallbackDelivery:
		return value.CallbackDelivery.Source
	case *controlplanev1.LegacyGraphOperation_MemoryRecord:
		return value.MemoryRecord.Source
	default:
		return nil
	}
}

var sourceTables = []controlplanev1.LegacySourceTable{
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_DELEGATION_CALLBACK_DELIVERIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_DELEGATION_CALLBACK_MANIFESTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_DELEGATIONS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_FLOWS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_PROFILES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_PROMPT_TEMPLATES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_ROLE_RUNTIME_VARIABLES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_ROLES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_RUNS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_SESSION_TURNS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AGENT_SESSIONS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AUDIT_EVENTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AUTOMATION_AUDIT_EVENTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_AUTOMATION_SCHEDULES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CHAT_PARTICIPANTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CHAT_REPOSITORIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CHATS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_ADMIN_BINDINGS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_BOT_BINDINGS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_DELIVERY_FENCES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_DEPENDENCIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_PROMPT_TEMPLATES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_REVOCATIONS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_RUNTIME_VARIABLE_BINDINGS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_SESSION_BINDINGS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CLUSTER_SUBJECTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_CREDENTIALS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_GITHUB_ACCOUNTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_INTERACTION_CAPABILITIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_MATTERMOST_BOT_IDENTITIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_MEMORY_EMBEDDINGS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_MEMORY_RECORD_VERSIONS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_MEMORY_RECORDS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_OPENAI_ACCOUNTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_OWNER_ATTENTION_REQUESTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_POLICY_REVISIONS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_PROCESS_RUNS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_PROCESS_TURNS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_PROJECT_REPOSITORIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_PROJECT_RUNTIME_VARIABLES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_PROJECTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_REPOSITORIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_ROLE_CAPABILITIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_ROLE_RELATIONSHIP_POLICIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_RUNTIME_AGENT_BINDING_DISCOVERIES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_RUNTIME_AGENT_BINDING_OUTBOX,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_SCHEDULE_OCCURRENCES,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_SCHEDULED_RUNS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_THREAD_CONTEXTS,
	controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_WORK_CLAIMS,
}

func sourceTable(table string) controlplanev1.LegacySourceTable {
	for index, candidate := range inventory.Tables {
		if candidate == table {
			return sourceTables[index]
		}
	}
	return 0
}
func sourceTableName(value controlplanev1.LegacySourceTable) string {
	for index, candidate := range sourceTables {
		if candidate == value {
			return canonicalSourceTableName(inventory.Tables[index])
		}
	}
	return ""
}
func canonicalSourceTableName(value string) string {
	switch value {
	case "matter_codex_agent_delegation_callback_delivery_manifests":
		return "matter_codex_agent_delegation_callback_manifests"
	case "matter_codex_cluster_admin_bot_bindings":
		return "matter_codex_cluster_bot_bindings"
	case "matter_codex_cluster_admin_delivery_fences":
		return "matter_codex_cluster_delivery_fences"
	case "matter_codex_cluster_admin_dependencies":
		return "matter_codex_cluster_dependencies"
	case "matter_codex_cluster_admin_prompt_templates":
		return "matter_codex_cluster_prompt_templates"
	case "matter_codex_cluster_admin_revocations":
		return "matter_codex_cluster_revocations"
	case "matter_codex_cluster_admin_runtime_variable_bindings":
		return "matter_codex_cluster_runtime_variable_bindings"
	case "matter_codex_cluster_admin_session_bindings":
		return "matter_codex_cluster_session_bindings"
	case "matter_codex_cluster_admin_subjects":
		return "matter_codex_cluster_subjects"
	default:
		return value
	}
}

func dispositionName(value controlplanev1.LegacySourceDispositionKind) string {
	return strings.TrimPrefix(value.String(), "LEGACY_SOURCE_DISPOSITION_KIND_")
}

func validateEvidence(value Evidence) error {
	for _, sha := range []string{value.ArchiveScanEvidenceSHA256, value.RoleImageSpecSHA256, value.ImagePolicySHA256, value.RuntimeContractSHA256,
		value.ImageBuildProvenanceSHA256, value.ImageAdmissionReceiptSHA256, value.ImageSignatureSHA256, value.ImagePromotionReadbackSHA256,
		value.ImageSBOMSHA256, value.ImageVulnerabilityEvidenceSHA256, value.AuthorityPolicySHA256} {
		if !validSHA(sha) {
			return errors.New("owner materialization evidence digest is invalid")
		}
	}
	if value.RoleImage == nil || value.ArchiveStoragePrefix == "" || value.ArchiveStorageVersion == "" || value.ArchiveRetentionRef == "" ||
		value.ArchiveScanPolicyRevision == 0 || value.ArchiveScannerWorkloadID == "" || value.ArchiveScannedAt.IsZero() || value.ProviderObservedAt.IsZero() ||
		value.ProviderObservationRevision == 0 || value.ProviderObservedLimit == 0 || value.RoleImageGeneration == 0 || value.ImagePolicyRevision == 0 || value.RuntimeContractRevision == 0 ||
		value.ImageBuildStagingReference == "" || value.ImageBuildManifestDigest == "" || value.ImageArtifactPromotedReference == "" || value.ImageAdmissionRevision == 0 ||
		value.ImageAdmissionReceiptManifestDigest == "" || value.ImageSignatureIdentity == "" || value.ImagePromotedAt.IsZero() || value.AuthorityPolicyRevision == 0 {
		return errors.New("owner materialization evidence is incomplete")
	}
	return nil
}

func terminalSourceTable(table string) bool {
	switch table {
	case "matter_codex_agent_flows", "matter_codex_audit_events", "matter_codex_automation_audit_events", "matter_codex_cluster_admin_delivery_fences", "matter_codex_cluster_admin_revocations", "matter_codex_memory_embeddings", "matter_codex_owner_attention_requests", "matter_codex_runtime_agent_binding_discoveries", "matter_codex_runtime_agent_binding_outbox", "matter_codex_schedule_occurrences", "matter_codex_scheduled_runs", "matter_codex_work_claims":
		return true
	default:
		return false
	}
}

func validateTerminalRows(table string, rows []sourceRow) error {
	if table == "matter_codex_audit_events" || table == "matter_codex_automation_audit_events" || table == "matter_codex_memory_embeddings" || table == "matter_codex_cluster_admin_revocations" || table == "matter_codex_cluster_admin_delivery_fences" {
		return nil
	}
	for _, row := range rows {
		state := strings.ToLower(nonempty(text(row, "status"), text(row, "state")))
		switch state {
		case "completed", "succeeded", "failed", "cancelled", "canceled", "expired", "blocked", "delivered", "revoked", "closed", "archived":
		default:
			return fmt.Errorf("nonterminal row in %s blocks migration", table)
		}
	}
	return nil
}

func text(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}
func number(row map[string]any, key string) int64 {
	value := text(row, key)
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}
func boolean(row map[string]any, key string) bool {
	value := strings.ToLower(text(row, key))
	return value == "true" || value == "t" || value == "1"
}
func nonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func sourceID(row sourceRow) string {
	if id := number(row, "id"); id > 0 {
		return strconv.FormatInt(id, 10)
	}
	for _, key := range []string{"public_id", "session_key", "run_id", "name", "delegation_id"} {
		if value := text(row, key); value != "" {
			return stable(value)
		}
	}
	return rowDigest(row)[:24]
}
func sourceRevision(row sourceRow) uint64 {
	for _, key := range []string{"binding_version", "version", "revision", "generation", "updated_revision"} {
		if value := number(row, key); value > 0 {
			return uint64(value)
		}
	}
	return 1
}
func rowDigest(row sourceRow) string {
	if value := text(row, "_source_sha256"); validSHA(value) {
		return value
	}
	return digest(mustJSON(row))
}
func sourceBytes(row sourceRow) uint64 {
	if value := number(row, "_source_bytes"); value > 0 {
		return uint64(value)
	}
	return uint64(len(mustJSON(row)))
}
func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
func digest(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func validSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return false
		}
	}
	return true
}
func stable(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var result strings.Builder
	lastDash := false
	for _, char := range value {
		ok := char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
		if ok {
			result.WriteRune(char)
			lastDash = false
		} else if !lastDash && result.Len() > 0 {
			result.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(result.String(), "-")
}
func cloneCounts(value map[string]uint64) map[string]uint64 {
	result := make(map[string]uint64, len(value))
	for key, count := range value {
		result[key] = count
	}
	return result
}
func cloneStrings(value map[string]string) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}
func validSourceTableDigests(value map[string]string) bool {
	if len(value) != len(inventory.Tables) {
		return false
	}
	for _, table := range inventory.Tables {
		if !validSHA(value[table]) {
			return false
		}
	}
	return true
}
func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
func runtimeComponents(refs []string) []*controlplanev1.LegacyRuntimeComponent {
	result := make([]*controlplanev1.LegacyRuntimeComponent, 0, len(refs))
	for _, ref := range refs {
		result = append(result, &controlplanev1.LegacyRuntimeComponent{LocalRef: ref})
	}
	return result
}
func assignmentKey(roleID, chatID int64) string {
	return strconv.FormatInt(roleID, 10) + "/" + strconv.FormatInt(chatID, 10)
}
func roleProviderName(rows []sourceRow, roleID int64) string {
	for _, row := range rows {
		if number(row, "id") == roleID {
			return text(row, "openai_account_name")
		}
	}
	return ""
}
func roleField(rows []sourceRow, roleID int64, key string) string {
	for _, row := range rows {
		if number(row, "id") == roleID {
			return text(row, key)
		}
	}
	return ""
}
func roleCapabilities(row sourceRow) []string {
	result := []string{"turn.execute"}
	if value := text(row, "kubernetes_access"); value != "" {
		result = append(result, "kubernetes."+stable(value))
	}
	sort.Strings(result)
	return result
}
func turnSession(rows []sourceRow, turnID int64) int64 {
	for _, row := range rows {
		if number(row, "id") == turnID {
			return number(row, "session_id")
		}
	}
	return 0
}
func parseRefID(value string) int64 {
	index := strings.LastIndexByte(value, '-')
	if index < 0 {
		return 0
	}
	parsed, _ := strconv.ParseInt(value[index+1:], 10, 64)
	return parsed
}
func cronDaily(local string) string {
	parts := strings.Split(local, ":")
	if len(parts) != 2 {
		return "0 0 * * *"
	}
	return parts[1] + " " + parts[0] + " * * *"
}
func archiveRef(row sourceRow) string {
	if sha256 := text(row, "session_archive_sha256"); validSHA(sha256) {
		return "legacy-archive://session/" + sourceID(row) + "?sha256=" + sha256
	}
	return ""
}
func turnResultArtifactRef(row sourceRow) string {
	if nonempty(text(row, "final_message"), text(row, "error_message")) != "" || number(row, "artifacts") > 0 {
		return "artifact-turn-result-" + strconv.FormatInt(number(row, "id"), 10)
	}
	return ""
}
func turnOutcome(row sourceRow) string {
	status := strings.ToLower(text(row, "status"))
	if status == "queued" || status == "running" || status == "capacity_retry" {
		return ""
	}
	return nonempty(text(row, "final_message"), text(row, "error_message"), status)
}

func timeValue(row sourceRow, key string) time.Time {
	value := text(row, key)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999-07:00", "2006-01-02 15:04:05-07:00"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC().Truncate(time.Microsecond)
		}
	}
	return time.Time{}
}
func timestamp(row sourceRow, key string) *timestamppb.Timestamp {
	value := timeValue(row, key)
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}
func hexText(row sourceRow, key string) string {
	value := text(row, key)
	value = strings.TrimPrefix(value, "\\x")
	if validSHA(value) {
		return value
	}
	return rowDigest(row)
}

func sessionState(value string) (controlplanev1.LifecycleState, error) {
	switch strings.ToLower(value) {
	case "idle", "active":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_ACTIVE, nil
	case "paused":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_PAUSED, nil
	case "completed", "closed", "archived":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_ARCHIVED, nil
	case "failed":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_FAILED, nil
	default:
		return 0, errors.New("unknown session lifecycle state")
	}
}
func turnStates(value string) (controlplanev1.LifecycleState, controlplanev1.LifecycleState, bool, error) {
	switch strings.ToLower(value) {
	case "queued", "capacity_retry":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_QUEUED, controlplanev1.LifecycleState_LIFECYCLE_STATE_QUEUED, false, nil
	case "completed", "succeeded":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_SUCCEEDED, controlplanev1.LifecycleState_LIFECYCLE_STATE_SUCCEEDED, true, nil
	case "failed":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_FAILED, controlplanev1.LifecycleState_LIFECYCLE_STATE_FAILED, true, nil
	case "cancelled", "canceled":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_CANCELLED, controlplanev1.LifecycleState_LIFECYCLE_STATE_CANCELLED, true, nil
	case "blocked", "waiting_owner":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_BLOCKED, controlplanev1.LifecycleState_LIFECYCLE_STATE_BLOCKED, true, nil
	case "running", "claimed":
		return 0, 0, false, errors.New("live turn lease blocks migration")
	default:
		return 0, 0, false, errors.New("unknown turn lifecycle state")
	}
}
func processState(value string) (controlplanev1.LifecycleState, error) {
	switch strings.ToLower(value) {
	case "completed", "succeeded":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_SUCCEEDED, nil
	case "failed":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_FAILED, nil
	case "cancelled", "canceled":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_CANCELLED, nil
	case "waiting_owner":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_WAITING_OWNER, nil
	case "blocked":
		return controlplanev1.LifecycleState_LIFECYCLE_STATE_BLOCKED, nil
	case "running", "queued", "claimed":
		return 0, errors.New("live process authority blocks migration")
	default:
		return 0, errors.New("unknown process lifecycle state")
	}
}
