package planner

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
)

func TestBuildMapsActiveRuntimeLineage(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("a", 64)
	rows := map[string][]map[string]any{
		"matter_codex_projects":    {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":       {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles": {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_agent_sessions": {{"id": 4, "project_id": 1, "chat_id": 2, "role_id": 3,
			"session_key": "session", "status": "running", "binding_version": 2, "active_turn_id": 5, "active_run_id": "run"}},
		"matter_codex_agent_session_turns": {{"id": 5, "session_id": 4, "run_id": "run", "status": "running",
			"binding_version": 3, "artifacts": map[string]any{}}},
		"matter_codex_agent_runs":       {{"id": 8, "run_id": "run", "status": "running"}},
		"matter_codex_policy_revisions": {{"id": 7, "project_id": 1, "version": 1, "status": "active"}},
		"matter_codex_process_runs": {{"id": 6, "project_id": 1, "policy_revision_id": 7,
			"root_role_id": 3, "status": "running"}},
		"matter_codex_process_turns": {{"process_run_id": 6, "turn_id": 5}},
	}
	target := append(configurationTarget("owner-1"),
		model.TargetResource{ID: "session-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: "owner-1",
			Kind: "SESSION", State: "ACTIVE", Version: 4, Spec: map[string]any{"agentSessionId": 4, "agentSessionKey": "session",
				"agentSessionBindingVersion": 2, "agentSessionBindingSha256": digest, "agentId": "agent-id", "conversationId": "chat-id"}, Canonical: []byte(`{"kind":"SESSION"}`)},
		model.TargetResource{ID: "revision-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: "owner-1",
			Kind: "RUNTIME_REVISION", State: "ACTIVE", Version: 7, Spec: map[string]any{"sessionId": "session-id",
				"effectiveRuntimeSha256": digest, "components": []any{map[string]any{"kind": "AGENT", "resourceId": "agent-id", "version": 1, "projectionSha256": digest}}},
			Canonical: []byte(`{"kind":"RUNTIME_REVISION"}`)},
		model.TargetResource{ID: "artifact-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: "owner-1",
			Kind: "ARTIFACT", State: "ACTIVE", Version: 1, Spec: map[string]any{"sha256": digest}, Canonical: []byte(`{"kind":"ARTIFACT"}`)},
		model.TargetResource{ID: "process-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: "owner-1",
			Kind: "PROCESS_RUN", State: "RUNNING", Version: 1, Spec: map[string]any{"rootTurnId": "turn-id",
				"rootSessionId": "session-id", "rootAttempt": 1, "runtimeRevisionId": "revision-id"}, Canonical: []byte(`{"kind":"PROCESS_RUN"}`)},
		model.TargetResource{ID: "turn-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: "owner-1",
			Kind: "TURN", State: "RUNNING", Version: 5, Spec: map[string]any{"agentSessionTurnId": 5, "agentRunId": "run",
				"agentTurnBindingVersion": 3, "agentTurnBindingSha256": digest, "sessionId": "session-id", "runtimeRevisionId": "revision-id",
				"attempt": 1, "effectiveInputSha256": digest, "promptArtifactId": "artifact-id", "processRunId": "process-id"}, Canonical: []byte(`{"kind":"TURN"}`)},
		model.TargetResource{ID: "turn-id#1", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: "owner-1",
			Kind: "TURN_ATTEMPT", State: "CLAIMED", Version: 1, Spec: map[string]any{"turnId": "turn-id", "attempt": 1,
				"inputSha256": digest, "runtimeRevisionId": "revision-id", "runtimeRevisionVersion": 7}, Canonical: []byte(`{"kind":"TURN_ATTEMPT"}`)},
		model.TargetResource{ID: "execution-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: "owner-1",
			Kind: "RUNTIME_EXECUTION", State: "RUNNING", Version: 1, Spec: map[string]any{"turnId": "turn-id", "sessionId": "session-id",
				"processRunId": "process-id", "attempt": 1, "runtimeRevisionId": "revision-id", "runtimeRevisionVersion": 7,
				"runtimeRevisionSha256": digest, "immutableInputSha256": digest}, Canonical: []byte(`{"kind":"RUNTIME_EXECUTION"}`)},
	)
	plan, err := Build("issue-196-plan-0000", snapshotRows(t, rows), target)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !plan.Ready() || plan.Counts.Mapped["RUNTIME_EXECUTION"] != 1 || plan.Counts.Mapped["PROCESS_RUN"] != 1 ||
		plan.Counts.Mapped["AGENT_RUN"] != 1 {
		t.Fatalf("active lineage was not mapped: %#v", plan)
	}
}

func TestBuildBlocksHiddenOwnerCollisionEvenWithExactCandidate(t *testing.T) {
	t.Parallel()
	target := configurationTarget("owner-1")
	conflicting := target[3]
	conflicting.ID = "foreign-agent-id"
	conflicting.OwnerActorID = "owner-2"
	target = append(target, conflicting)
	plan, err := Build("issue-196-plan-0008", closedHistorySnapshot(t, "closed", "succeeded"), target)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["tenant_mismatch"] == 0 {
		t.Fatalf("hidden owner collision did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksHiddenTenantCollisionEvenWithExactCandidate(t *testing.T) {
	t.Parallel()
	target := configurationTarget("owner-1")
	conflicting := target[2]
	conflicting.ID = "foreign-chat-id"
	conflicting.OrganizationID = "foreign-organization-id"
	conflicting.ProjectID = "foreign-project-id"
	conflicting.OwnerActorID = "foreign-owner-id"
	target = append(target, conflicting)
	plan, err := Build("issue-196-plan-0014", closedHistorySnapshot(t, "closed", "succeeded"), target)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["tenant_mismatch"] == 0 {
		t.Fatalf("hidden tenant collision did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksAgentConfigurationWithoutProtectedVersionEvidence(t *testing.T) {
	t.Parallel()
	target := configurationTarget("owner-1")
	filtered := target[:0]
	for _, resource := range target {
		if resource.Historical && resource.Kind == "ROLE_DEFINITION" {
			continue
		}
		filtered = append(filtered, resource)
	}
	plan, err := Build("issue-196-plan-0015", closedHistorySnapshot(t, "closed", "succeeded"), filtered)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["broken_lineage"] == 0 {
		t.Fatalf("missing protected configuration evidence did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildRecognizesConfiguredThreadAsNonterminal(t *testing.T) {
	t.Parallel()
	rows := map[string][]map[string]any{
		"matter_codex_projects":    {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":       {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles": {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_thread_contexts": {{"id": 4, "project_id": 1, "chat_id": 2,
			"status": "configured"}},
	}
	plan, err := Build("issue-196-plan-0009", snapshotRows(t, rows), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Violations["unknown_state"] != 0 || plan.Violations["unmaterialized_active"] == 0 {
		t.Fatalf("configured thread lifecycle was classified incorrectly: %#v", plan.Violations)
	}
}

func TestBuildBlocksUnfinishedStandaloneAgentRun(t *testing.T) {
	t.Parallel()
	rows := map[string][]map[string]any{
		"matter_codex_projects":    {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":       {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles": {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_agent_runs":  {{"id": 4, "run_id": "standalone", "status": "running"}},
	}
	plan, err := Build("issue-196-plan-0010", snapshotRows(t, rows), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["unmaterialized_active"] == 0 {
		t.Fatalf("unfinished standalone run did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksUnfinishedDelegation(t *testing.T) {
	t.Parallel()
	rows := map[string][]map[string]any{
		"matter_codex_projects":    {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":       {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles": {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_agent_sessions": {{"id": 4, "project_id": 1, "chat_id": 2, "role_id": 3,
			"session_key": "session", "status": "closed"}},
		"matter_codex_agent_session_turns": {{"id": 5, "session_id": 4, "run_id": "run", "status": "succeeded",
			"artifacts": map[string]any{}}},
		"matter_codex_agent_delegations": {{"id": 6, "project_id": 1, "source_session_id": 4,
			"source_turn_id": 5, "target_chat_id": 2, "target_role_id": 3, "status": "creating"}},
	}
	plan, err := Build("issue-196-plan-0011", snapshotRows(t, rows), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["unmaterialized_active"] == 0 {
		t.Fatalf("unfinished delegation did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildArchivesClosedHistoryDeterministically(t *testing.T) {
	t.Parallel()
	snapshot := closedHistorySnapshot(t, "closed", "succeeded")
	target := configurationTarget("owner-1")
	first, err := Build("issue-196-plan-0001", snapshot, target)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	second, err := Build("issue-196-plan-0001", snapshot, target)
	if err != nil {
		t.Fatalf("Build() second error = %v", err)
	}
	if !first.Ready() || first.PlanSHA256 != second.PlanSHA256 || first.MappingSHA256 != second.MappingSHA256 ||
		first.Counts.Archive["SESSION"] != 1 || first.Counts.Archive["TURN"] != 1 {
		t.Fatalf("unexpected repeatable archive plan: %#v", first)
	}
}

func TestBuildBlocksUnmaterializedActiveSession(t *testing.T) {
	t.Parallel()
	plan, err := Build("issue-196-plan-0002", closedHistorySnapshot(t, "running", "running"), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["unmaterialized_active"] == 0 {
		t.Fatalf("active source without target did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksHiddenOwnerMismatch(t *testing.T) {
	t.Parallel()
	target := configurationTarget("owner-1")
	target[1].OwnerActorID = "owner-2"
	plan, err := Build("issue-196-plan-0003", closedHistorySnapshot(t, "closed", "succeeded"), target)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["tenant_mismatch"] == 0 {
		t.Fatalf("hidden owner mismatch did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksUnknownLegacyState(t *testing.T) {
	t.Parallel()
	plan, err := Build("issue-196-plan-0004", closedHistorySnapshot(t, "mystery", "succeeded"), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["unknown_state"] == 0 {
		t.Fatalf("unknown state did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksUnknownArchivedState(t *testing.T) {
	t.Parallel()
	rows := map[string][]map[string]any{
		"matter_codex_projects":     {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":        {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles":  {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_repositories": {{"id": 4, "status": "mystery"}},
	}
	plan, err := Build("issue-196-plan-0012", snapshotRows(t, rows), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["unknown_state"] == 0 {
		t.Fatalf("unknown archived state did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksBrokenMemoryLineage(t *testing.T) {
	t.Parallel()
	rows := map[string][]map[string]any{
		"matter_codex_projects":    {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":       {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles": {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_memory_records": {{"id": 4, "project_id": 1, "scope": "project", "created_by_role_id": 3,
			"status": "active"}},
		"matter_codex_memory_record_versions": {{"id": 5, "record_id": 99, "version": 1,
			"content_hash": strings.Repeat("a", 64)}},
	}
	plan, err := Build("issue-196-plan-0013", snapshotRows(t, rows), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["orphan_reference"] == 0 || plan.Violations["broken_lineage"] == 0 {
		t.Fatalf("broken memory lineage did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksUnknownSourceTable(t *testing.T) {
	t.Parallel()
	rows := map[string][]map[string]any{
		"matter_codex_projects":        {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":           {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles":     {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_future_resource": nil,
	}
	plan, err := Build("issue-196-plan-0005", snapshotRows(t, rows), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["unsupported_state"] == 0 {
		t.Fatalf("unknown source table did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildBlocksEnabledArchivedSchedule(t *testing.T) {
	t.Parallel()
	rows := map[string][]map[string]any{
		"matter_codex_projects":    {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":       {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles": {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_automation_schedules": {{"id": 4, "project_id": 1, "target_agent_role_id": 3,
			"target_chat_id": 2, "enabled": true}},
	}
	plan, err := Build("issue-196-plan-0006", snapshotRows(t, rows), configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Ready() || plan.Violations["unmaterialized_active"] == 0 {
		t.Fatalf("enabled archived schedule did not fail closed: %#v", plan.Violations)
	}
}

func TestBuildInventoryKeepsFullArchiveCounts(t *testing.T) {
	t.Parallel()
	projection := closedHistorySnapshot(t, "closed", "succeeded")
	projection = append(projection,
		model.SnapshotRow{Table: "matter_codex_audit_events", Payload: []byte(`{"id":1}`)},
		model.SnapshotRow{Table: "matter_codex_audit_events", Payload: []byte(`{"id":2}`)},
	)
	sort.Slice(projection, func(left, right int) bool {
		if projection[left].Table == projection[right].Table {
			return string(projection[left].Payload) < string(projection[right].Payload)
		}
		return projection[left].Table < projection[right].Table
	})
	counts := make(map[string]uint64)
	for _, row := range projection {
		if _, exists := counts[row.Table]; !exists {
			counts[row.Table] = 0
		}
		if len(row.Payload) != 0 {
			counts[row.Table]++
		}
	}
	plan, err := BuildInventory("issue-196-plan-0007", projection, strings.Repeat("b", 64), counts,
		configurationTarget("owner-1"))
	if err != nil {
		t.Fatalf("BuildInventory() error = %v", err)
	}
	if !plan.Ready() || plan.Counts.Archive["matter_codex_audit_events"] != 2 {
		t.Fatalf("full archive counts were lost: %#v", plan)
	}
	counts["matter_codex_audit_events"] = 3
	if _, err := BuildInventory("issue-196-plan-0007", projection, strings.Repeat("b", 64), counts,
		configurationTarget("owner-1")); err == nil {
		t.Fatal("BuildInventory() accepted an incomplete safe projection")
	}
}

func closedHistorySnapshot(t *testing.T, sessionState, turnState string) []model.SnapshotRow {
	t.Helper()
	rows := map[string][]map[string]any{
		"matter_codex_projects":    {{"id": 1, "slug": "project", "mattermost_team_id": "team-ref"}},
		"matter_codex_chats":       {{"id": 2, "project_id": 1, "mattermost_channel_id": "channel-ref", "slug": "chat", "status": "active"}},
		"matter_codex_agent_roles": {{"id": 3, "project_id": 1, "name": "worker", "enabled": true}},
		"matter_codex_agent_sessions": {{"id": 4, "project_id": 1, "chat_id": 2, "role_id": 3,
			"session_key": "session", "status": sessionState}},
		"matter_codex_agent_session_turns": {{"id": 5, "session_id": 4, "run_id": "run", "status": turnState,
			"artifacts": map[string]any{}}},
		"matter_codex_agent_runs": {{"id": 6, "run_id": "run", "status": turnState}},
	}
	return snapshotRows(t, rows)
}

func snapshotRows(t *testing.T, rows map[string][]map[string]any) []model.SnapshotRow {
	t.Helper()
	for _, role := range rows["matter_codex_agent_roles"] {
		if _, exists := role["prompt_mode"]; !exists {
			role["prompt_mode"] = "template"
		}
	}
	for required := range knownSourceTables {
		if _, exists := rows[required]; !exists {
			rows[required] = nil
		}
	}
	tables := make([]string, 0, len(rows))
	for table := range rows {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	result := make([]model.SnapshotRow, 0, len(rows)*2)
	for _, table := range tables {
		result = append(result, model.SnapshotRow{Table: table})
		for _, row := range rows[table] {
			encoded, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			result = append(result, model.SnapshotRow{Table: table, Payload: encoded})
		}
	}
	return result
}

func configurationTarget(owner string) []model.TargetResource {
	digest := strings.Repeat("a", 64)
	resources := []model.TargetResource{
		{ID: "project-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "PROJECT", State: "ACTIVE", Version: 1, Spec: map[string]any{"slug": "project"}, Canonical: []byte(`{"kind":"PROJECT"}`)},
		{ID: "team-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "TEAM", State: "ACTIVE", Version: 1, Spec: map[string]any{"externalTeamRef": "team-ref"}, Canonical: []byte(`{"kind":"TEAM"}`)},
		{ID: "chat-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "CHAT", State: "ACTIVE", Version: 1, Spec: map[string]any{"externalChannelRef": "channel-ref", "stableKey": "chat"}, Canonical: []byte(`{"kind":"CHAT"}`)},
		{ID: "agent-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "AGENT", State: "ACTIVE", Version: 1, Spec: map[string]any{
				"stableKey": "worker", "enabled": true,
				"roleDefinitionId": "role-definition-id", "roleDefinitionVersion": 1, "roleDefinitionSha256": digest,
				"instructionSetId": "instruction-set-id", "instructionSetVersion": 1, "instructionSetSha256": digest,
				"providerPoolId": "provider-pool-id", "providerPoolVersion": 1, "providerPoolSha256": digest,
				"runtimeProfileRef":     "control-plane://runtime-profile/role-image-recipe-id",
				"runtimeProfileVersion": 1, "runtimeProfileSha256": digest,
			}, Canonical: []byte(`{"kind":"AGENT"}`)},
		{ID: "role-definition-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "ROLE_DEFINITION", State: "ACTIVE", Version: 1, Spec: map[string]any{"stableKey": "worker"}, Canonical: []byte(`{"kind":"ROLE_DEFINITION"}`)},
		{ID: "instruction-set-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "INSTRUCTION_SET", State: "ACTIVE", Version: 1, Spec: map[string]any{"stableKey": "worker",
				"versionState": "PUBLISHED", "currentVersion": 1, "publishedVersion": 1, "contentSha256": digest,
				"contentArtifactId": "instruction-artifact-id", "contentArtifactVersion": 1},
			Canonical: []byte(`{"kind":"INSTRUCTION_SET"}`)},
		{ID: "provider-pool-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "PROVIDER_POOL", State: "ACTIVE", Version: 1, Spec: map[string]any{"stableKey": "worker",
				"eligibilitySnapshotSha256": digest, "bindings": []any{
					map[string]any{"providerConnectionReferenceId": "provider-reference-id", "providerConnectionStableKey": "provider-reference",
						"referenceVersion": 1, "referenceSha256": digest, "eligible": true, "maskedStatus": "AVAILABLE"},
				}}, Canonical: []byte(`{"kind":"PROVIDER_POOL"}`)},
		{ID: "provider-reference-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "PROVIDER_CONNECTION_REFERENCE", State: "ACTIVE", Version: 1,
			Spec:      map[string]any{"stableKey": "provider-reference", "eligible": true, "maskedStatus": "AVAILABLE"},
			Canonical: []byte(`{"kind":"PROVIDER_CONNECTION_REFERENCE"}`)},
		{ID: "role-image-recipe-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "ROLE_IMAGE_RECIPE", State: "ACTIVE", Version: 1, Spec: map[string]any{"stableKey": "runtime"},
			Canonical: []byte(`{"kind":"ROLE_IMAGE_RECIPE"}`)},
		{ID: "agent-assignment-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "AGENT_ASSIGNMENT", State: "ACTIVE", Version: 1, Spec: map[string]any{
				"agentId": "agent-id", "agentVersion": 1, "agentSha256": digest,
				"workspaceId": "project-id", "workspaceVersion": 1, "workspaceSha256": digest,
				"rootActorId": owner, "assignmentGeneration": 1,
			}, Canonical: []byte(`{"kind":"AGENT_ASSIGNMENT"}`)},
		{ID: "instruction-artifact-id", OrganizationID: "organization-id", ProjectID: "project-id", OwnerActorID: owner,
			Kind: "ARTIFACT", State: "ACTIVE", Version: 1, Spec: map[string]any{"sha256": digest},
			Canonical: []byte(`{"kind":"ARTIFACT"}`)},
	}
	for _, index := range []int{3, 4, 5, 6, 7, 8} {
		historical := resources[index]
		historical.Historical = true
		historical.ProjectionSHA256 = digest
		resources = append(resources, historical)
	}
	return resources
}
