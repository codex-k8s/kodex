package planner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
)

func TestCanonicalSourceTableAliasesRemainClosed(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"matter_codex_agent_delegation_callback_delivery_manifests": "matter_codex_agent_delegation_callback_manifests",
		"matter_codex_cluster_admin_bot_bindings":                   "matter_codex_cluster_bot_bindings",
		"matter_codex_cluster_admin_subjects":                       "matter_codex_cluster_subjects",
	}
	for input, want := range cases {
		if got := canonicalSourceTableName(input); got != want {
			t.Fatalf("canonicalSourceTableName(%q) = %q, нужно %q", input, got, want)
		}
		if sourceTable(input) == controlplanev1.LegacySourceTable_LEGACY_SOURCE_TABLE_UNSPECIFIED {
			t.Fatalf("source table %q отсутствует в закрытом owner enum", input)
		}
	}
}

func TestDispositionsMatchTypedProofsByCanonicalAlias(t *testing.T) {
	t.Parallel()
	const table = "matter_codex_cluster_admin_bot_bindings"
	counts := make(map[string]uint64, len(inventory.Tables))
	digests := make(map[string]string, len(inventory.Tables))
	for _, candidate := range inventory.Tables {
		counts[candidate] = 0
		digests[candidate] = digest([]byte("table:" + candidate))
	}
	counts[table] = 1
	source := &controlplanev1.LegacyOperationSource{
		SourceTable: sourceTable(table), SourceRef: table + "/1",
		SourceRevision: 1, SourceSha256: digest([]byte("row")), LocalRef: "binding-1",
	}
	builder := ownerBuilder{
		counts: counts, tableDigests: digests, rows: map[string][]sourceRow{},
		operations: []*controlplanev1.LegacyGraphOperation{{
			Operation: &controlplanev1.LegacyGraphOperation_Artifact{
				Artifact: &controlplanev1.LegacyArtifactOperation{Source: source},
			},
		}},
		mapped: make(map[string]uint64), archived: make(map[string]uint64),
	}
	dispositions, _, err := builder.dispositions()
	if err != nil {
		t.Fatalf("dispositions() отклонил typed proof для alias: %v", err)
	}
	for _, disposition := range dispositions {
		if disposition.GetSourceTable() == sourceTable(table) {
			if disposition.GetDisposition() != controlplanev1.LegacySourceDispositionKind_LEGACY_SOURCE_DISPOSITION_KIND_MATERIALIZE {
				t.Fatalf("alias disposition = %s", disposition.GetDisposition())
			}
			return
		}
	}
	t.Fatal("alias disposition отсутствует")
}

func TestValidateTerminalRowsFailsClosed(t *testing.T) {
	t.Parallel()
	if err := validateTerminalRows("matter_codex_work_claims", []sourceRow{{"status": "completed"}}); err != nil {
		t.Fatalf("terminal row отклонён: %v", err)
	}
	if err := validateTerminalRows("matter_codex_work_claims", []sourceRow{{"status": "running"}}); err == nil {
		t.Fatal("active work claim должен блокировать materialization plan")
	}
}

func TestProviderAccountNameMatchesOwnerDerivation(t *testing.T) {
	t.Parallel()
	account := "Primary Account"
	principal := "provider:" + stable(account)
	ownerName := principal[len("provider:"):]
	if ownerName != stable(account) {
		t.Fatalf("provider account mismatch: %q != %q", ownerName, stable(account))
	}
}

func TestRoleImageSpecSHA256MatchesControlPlaneCanonicalJSON(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 64)
	input := &controlplanev1.RoleImageRecipeInput{
		BaseImageReference: "registry.example/agent-runner", BaseImageDigest: "sha256:" + sha,
		SourceRef: "git://github.com/codex-k8s/matter-codex", SourceRevision: strings.Repeat("b", 40),
		SourceSha256: sha, ContextRef: "oci://registry.example/input@sha256:" + sha, ContextSha256: sha,
		BuilderSha256: sha, FrontendSha256: sha,
		Platforms:         []*controlplanev1.RoleImagePlatform{{Os: "linux", Architecture: "amd64"}},
		InstallationBlock: "", ToolchainSha256: sha,
	}
	got, err := RoleImageSpecSHA256(input, 1, sha, 1, sha)
	if err != nil {
		t.Fatalf("RoleImageSpecSHA256() error = %v", err)
	}
	wantJSON := `{"Input":{"baseImageReference":"registry.example/agent-runner","baseImageDigest":"sha256:` + sha +
		`","sourceRef":"git://github.com/codex-k8s/matter-codex","sourceRevision":"` + strings.Repeat("b", 40) +
		`","sourceSha256":"` + sha + `","contextRef":"oci://registry.example/input@sha256:` + sha +
		`","contextSha256":"` + sha + `","builderSha256":"` + sha + `","frontendSha256":"` + sha +
		`","platforms":[{"os":"linux","architecture":"amd64"}],"packages":[],"tools":[],"installationBlock":"","toolchainSha256":"` + sha +
		`"},"PolicyRevision":1,"PolicySHA256":"` + sha + `","RuntimeContractRevision":1,"RuntimeContractSHA256":"` + sha + `"}`
	if want := digest([]byte(wantJSON)); got != want {
		t.Fatalf("canonical role image hash = %s, нужно %s", got, want)
	}
}

func TestTypedOperationsAreSortedForOwnerCompiler(t *testing.T) {
	t.Parallel()
	builder := ownerBuilder{operations: []*controlplanev1.LegacyGraphOperation{
		{Operation: &controlplanev1.LegacyGraphOperation_ProviderReference{ProviderReference: &controlplanev1.LegacyProviderConnectionReferenceOperation{}}},
		{Operation: &controlplanev1.LegacyGraphOperation_Project{Project: &controlplanev1.LegacyProjectOperation{}}},
		{Operation: &controlplanev1.LegacyGraphOperation_Artifact{Artifact: &controlplanev1.LegacyArtifactOperation{}}},
		{Operation: &controlplanev1.LegacyGraphOperation_RoleDefinition{RoleDefinition: &controlplanev1.LegacyRoleDefinitionOperation{}}},
	}}
	if err := builder.sortOperations(); err != nil {
		t.Fatalf("sort operations: %v", err)
	}
	for index := 1; index < len(builder.operations); index++ {
		if ownerOperationRank(builder.operations[index]) < ownerOperationRank(builder.operations[index-1]) {
			t.Fatal("typed owner operations остались вне contract rank")
		}
	}
}

func TestBuildMaterializesClosedActiveGraph(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	rows := []model.SnapshotRow{
		testRow(t, "matter_codex_projects", map[string]any{"id": 1, "name": "Project", "slug": "project"}),
		testRow(t, "matter_codex_chats", map[string]any{"id": 1, "project_id": 1, "name": "Chat", "slug": "chat"}),
		testRow(t, "matter_codex_agent_roles", map[string]any{"id": 1, "project_id": 1, "name": "developer", "openai_account_name": "primary", "prompt_template": "Instruction", "enabled": true}),
		testRow(t, "matter_codex_credentials", map[string]any{"id": 1, "name": "openai:primary", "provider": "openai", "secret_ref": "openai-primary", "status": "authorized"}),
		testRow(t, "matter_codex_openai_accounts", map[string]any{"id": 1, "name": "primary", "credential_id": 1, "status": "authorized"}),
		testRow(t, "matter_codex_chat_participants", map[string]any{"id": 1, "chat_id": 1, "role_id": 1, "enabled": true}),
		testRow(t, "matter_codex_agent_sessions", map[string]any{"id": 1, "project_id": 1, "chat_id": 1, "role_id": 1, "session_key": "session", "status": "idle", "created_at": now.Format(time.RFC3339)}),
		testRow(t, "matter_codex_agent_session_turns", map[string]any{"id": 1, "session_id": 1, "run_id": "run", "status": "succeeded", "message": "input", "final_message": "result", "created_at": now.Format(time.RFC3339), "finished_at": now.Add(time.Minute).Format(time.RFC3339)}),
		testRow(t, "matter_codex_policy_revisions", map[string]any{"id": 1, "project_id": 1, "version": 1, "status": "active"}),
		testRow(t, "matter_codex_process_runs", map[string]any{"id": 1, "public_id": "process", "project_id": 1, "policy_revision_id": 1, "root_initiator_user_id": "actor", "root_trigger_post_id": "post", "status": "succeeded"}),
		testRow(t, "matter_codex_process_turns", map[string]any{"process_run_id": 1, "turn_id": 1, "parent_turn_id": 0}),
	}
	counts := make(map[string]uint64, len(inventory.Tables))
	tableDigests := make(map[string]string, len(inventory.Tables))
	for _, table := range inventory.Tables {
		counts[table] = 0
		tableDigests[table] = digest([]byte("table:" + table))
	}
	for _, row := range rows {
		counts[row.Table]++
	}
	evidence := testEvidence(now)
	result, err := Build("11111111-1111-4111-8111-111111111111", "11111111-1111-4111-8111-111111111111",
		"legacy-root:actor", digest([]byte("root")), rows, digest([]byte("source")), counts, tableDigests, evidence)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Request.GetSourceDispositions()) != len(inventory.Tables) || result.Report.OwnerRequestSHA256 == "" {
		t.Fatal("closed owner request evidence is incomplete")
	}
	for index := 1; index < len(result.Request.GetOperations()); index++ {
		if ownerOperationRank(result.Request.GetOperations()[index]) < ownerOperationRank(result.Request.GetOperations()[index-1]) {
			t.Fatal("owner request operation order is invalid")
		}
	}
	var runtime *controlplanev1.LegacyRuntimeRevisionOperation
	var turn *controlplanev1.LegacyTurnOperation
	var process *controlplanev1.LegacyProcessRunOperation
	for _, operation := range result.Request.GetOperations() {
		if operation.GetRuntimeRevision() != nil {
			runtime = operation.GetRuntimeRevision()
		}
		if operation.GetTurn() != nil {
			turn = operation.GetTurn()
		}
		if operation.GetProcessRun() != nil {
			process = operation.GetProcessRun()
		}
	}
	if runtime == nil || runtime.GetProviderAccountName() != "primary" || runtime.GetProviderCredentialRef() != "credential-1" ||
		turn == nil || turn.GetPromptArtifactRef() != "artifact-turn-input-1" || turn.GetResultArtifactRef() != "artifact-turn-result-1" ||
		process == nil || process.GetLegacyPolicySha256() == evidence.AuthorityPolicySHA256 {
		t.Fatal("active graph or separated policy provenance is incomplete")
	}
	if result.Report.Counts.Mapped["matter_codex_agent_roles"] != 1 || result.Report.Counts.Mapped["matter_codex_agent_session_turns"] != 1 {
		t.Fatal("mapped counts must count source rows rather than derived operations")
	}
}

func TestConfigurationProjectsSelectsOnlyActiveProjectGraph(t *testing.T) {
	t.Parallel()
	rows := []model.SnapshotRow{
		testRow(t, "matter_codex_projects", map[string]any{"id": 1, "name": "First", "slug": "first", "github_account_name": "github"}),
		testRow(t, "matter_codex_projects", map[string]any{"id": 2, "name": "Second", "slug": "second", "github_account_name": "github"}),
		testRow(t, "matter_codex_chats", map[string]any{"id": 10, "project_id": 1, "name": "Active", "slug": "active", "status": "active"}),
		testRow(t, "matter_codex_chats", map[string]any{"id": 11, "project_id": 1, "name": "Archived", "slug": "archived", "status": "archived"}),
		testRow(t, "matter_codex_chats", map[string]any{"id": 20, "project_id": 2, "name": "Active", "slug": "active", "status": "active"}),
		testRow(t, "matter_codex_agent_roles", map[string]any{"id": 100, "project_id": 1, "name": "manager", "openai_account_name": "openai", "github_account_name": "github", "enabled": true}),
		testRow(t, "matter_codex_agent_roles", map[string]any{"id": 101, "project_id": 1, "name": "disabled", "openai_account_name": "openai", "enabled": false}),
		testRow(t, "matter_codex_agent_roles", map[string]any{"id": 200, "project_id": 2, "name": "manager", "openai_account_name": "openai", "github_account_name": "github", "enabled": true}),
		testRow(t, "matter_codex_chat_participants", map[string]any{"id": 1, "chat_id": 10, "role_id": 100, "enabled": true}),
		testRow(t, "matter_codex_chat_participants", map[string]any{"id": 2, "chat_id": 20, "role_id": 200, "enabled": true}),
		testRow(t, "matter_codex_credentials", map[string]any{"id": 1, "name": "openai", "secret_ref": "openai", "status": "authorized"}),
		testRow(t, "matter_codex_credentials", map[string]any{"id": 2, "name": "github", "secret_ref": "github", "status": "configured"}),
		testRow(t, "matter_codex_openai_accounts", map[string]any{"id": 1, "name": "openai", "credential_id": 1, "status": "authorized"}),
		testRow(t, "matter_codex_github_accounts", map[string]any{"id": 2, "name": "github", "credential_id": 2, "secret_ref": "github", "status": "configured"}),
		testRow(t, "matter_codex_agent_sessions", map[string]any{"id": 1, "project_id": 1, "chat_id": 10, "role_id": 100, "status": "idle"}),
	}
	projects, err := ConfigurationProjects(rows)
	if err != nil {
		t.Fatalf("ConfigurationProjects() error = %v", err)
	}
	if len(projects) != 2 || projects[0].LegacyProjectID != 1 || projects[1].LegacyProjectID != 2 {
		t.Fatalf("unexpected project split: %#v", projects)
	}
	first, _, err := decodeSource(projects[0].Rows, projects[0].Counts)
	if err != nil {
		t.Fatalf("decode first project: %v", err)
	}
	if len(first["matter_codex_chats"]) != 1 || len(first["matter_codex_agent_roles"]) != 1 ||
		len(first["matter_codex_agent_sessions"]) != 0 || projects[0].Counts["matter_codex_agent_sessions"] != 0 {
		t.Fatal("configuration projection retained archived or runtime rows")
	}
	result, err := BuildConfiguration("11111111-1111-4111-8111-111111111111",
		"11111111-1111-4111-8111-111111111111", "legacy-root:actor", digest([]byte("root")),
		projects[0].Rows, projects[0].SourceSHA256, projects[0].Counts, projects[0].TableSHA256,
		testEvidence(time.Now().UTC().Add(-time.Minute)))
	if err != nil {
		t.Fatalf("BuildConfiguration() error = %v", err)
	}
	for _, operation := range result.Request.GetOperations() {
		if operation.GetSession() != nil || operation.GetTurn() != nil || operation.GetProcessRun() != nil {
			t.Fatal("configuration plan contains runtime history")
		}
	}
}

func testRow(t *testing.T, table string, value map[string]any) model.SnapshotRow {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return model.SnapshotRow{Table: table, Payload: encoded}
}

func testEvidence(now time.Time) Evidence {
	sha := strings.Repeat("a", 64)
	return Evidence{
		ArchiveStoragePrefix: "s3://archive", ArchiveStorageVersion: "v1", ArchiveRetentionRef: "retention-v1",
		ArchiveScanPolicyRevision: 1, ArchiveScanEvidenceSHA256: sha, ArchiveScannerWorkloadID: "scanner", ArchiveScannedAt: now,
		ProviderObservedAt: now, ProviderObservationRevision: 1, ProviderObservedLimit: 1,
		RoleImage: &controlplanev1.RoleImageRecipeInput{}, RoleImageGeneration: 1, RoleImageSpecSHA256: sha,
		ImagePolicyRevision: 1, ImagePolicySHA256: sha, RuntimeContractRevision: 1, RuntimeContractSHA256: sha,
		ImageBuildStagingReference: "registry/staging", ImageBuildManifestDigest: "sha256:" + sha, ImageBuildProvenanceSHA256: sha,
		ImageArtifactPromotedReference: "registry/image@sha256:" + sha, ImageAdmissionRevision: 1,
		ImageAdmissionReceiptSHA256: sha, ImageAdmissionReceiptManifestDigest: "sha256:" + sha,
		ImageSignatureSHA256: sha, ImagePromotionReadbackSHA256: sha, ImageSBOMSHA256: sha,
		ImageVulnerabilityEvidenceSHA256: sha, ImageSignatureIdentity: "identity", ImagePromotedAt: now,
		AuthorityPolicyRevision: 1, AuthorityPolicySHA256: strings.Repeat("b", 64),
		Credentials: map[int64]CredentialEvidence{
			1: {SecretRef: "k8s-secret://mattercodex-system/legacy-credential-1",
				ImmutableSecretRef: "k8s-immutable-secret://mattercodex-system/legacy-credential-1",
				ContentVersion:     "uid-1:1", ContentSHA256: sha},
			2: {SecretRef: "k8s-secret://mattercodex-system/legacy-credential-2",
				ImmutableSecretRef: "k8s-immutable-secret://mattercodex-system/legacy-credential-2",
				ContentVersion:     "uid-2:1", ContentSHA256: sha},
		},
	}
}
