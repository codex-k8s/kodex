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
	}
}
