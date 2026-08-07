// Package planner строит детерминированную source-to-target карту без effects.
package planner

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/inventory"
	"github.com/codex-k8s/matter-codex/services/jobs/legacy-data-migration/internal/model"
)

const schemaVersion = "mattercodex.legacy-data-migration-plan.v1"

type sourceRow map[string]any

var knownSourceTables = func() map[string]struct{} {
	result := make(map[string]struct{}, len(inventory.Tables))
	for _, table := range inventory.Tables {
		result[table] = struct{}{}
	}
	return result
}()

func Build(planID string, snapshot []model.SnapshotRow, target []model.TargetResource) (model.Plan, error) {
	counts := make(map[string]uint64)
	source, sourceDigest, err := decodeSource(snapshot, counts)
	if err != nil {
		return model.Plan{}, err
	}
	return build(planID, source, sourceDigest, counts, target)
}

// BuildInventory принимает полный потоковый digest/counts и только безопасную
// проекцию строк, нужную для mapping. Raw archive payload остаётся в pg_dump.
func BuildInventory(planID string, projection []model.SnapshotRow, sourceDigest string,
	counts map[string]uint64, target []model.TargetResource,
) (model.Plan, error) {
	projection = append([]model.SnapshotRow(nil), projection...)
	sort.SliceStable(projection, func(left, right int) bool {
		if projection[left].Table != projection[right].Table {
			return projection[left].Table < projection[right].Table
		}
		return bytes.Compare(projection[left].Payload, projection[right].Payload) < 0
	})
	projectedCounts := make(map[string]uint64)
	source, _, err := decodeSource(projection, projectedCounts)
	if err != nil || !validSHA(sourceDigest) || len(counts) == 0 {
		return model.Plan{}, errors.New("source inventory evidence is invalid")
	}
	for table, count := range projectedCounts {
		if count > counts[table] {
			return model.Plan{}, errors.New("source inventory projection count is invalid")
		}
	}
	for table, count := range counts {
		if _, known := knownSourceTables[table]; known && projectedCounts[table] != count {
			return model.Plan{}, errors.New("source inventory projection is incomplete")
		}
	}
	clonedCounts := make(map[string]uint64, len(counts))
	for table, count := range counts {
		clonedCounts[table] = count
	}
	return build(planID, source, sourceDigest, clonedCounts, target)
}

func build(planID string, source map[string][]sourceRow, sourceDigest string,
	counts map[string]uint64, target []model.TargetResource,
) (model.Plan, error) {
	if len(planID) < 16 || len(planID) > 128 {
		return model.Plan{}, errors.New("migration plan identifier is invalid")
	}
	plan := model.Plan{
		SchemaVersion: schemaVersion,
		PlanID:        planID,
		Counts: model.Counts{
			Source: counts, Mapped: make(map[string]uint64), Archive: make(map[string]uint64),
		},
		Violations: map[string]uint64{
			"ambiguous_target": 0, "broken_lineage": 0, "duplicate_source": 0,
			"orphan_reference": 0, "stale_reference": 0, "tenant_mismatch": 0,
			"unknown_state": 0, "unmaterialized_active": 0, "unsupported_state": 0,
		},
	}
	plan.SourceSHA256 = sourceDigest
	for required := range knownSourceTables {
		if _, exists := plan.Counts.Source[required]; !exists {
			plan.Violations["unsupported_state"]++
		}
	}
	matched := make(map[string]model.TargetResource)
	mapping := make([]string, 0, len(source))
	mapSource(source, target, &plan, matched, &mapping)
	validateArchivedInventory(source, &plan)
	archiveRemaining(source, &plan, &mapping)
	detectMappingDuplicates(mapping, &plan)
	plan.MappingSHA256 = digestMapping(mapping)
	if len(plan.Materialization) > 100_000 {
		return model.Plan{}, errors.New("materialization plan exceeds the closed command limit")
	}
	materialization, materializationErr := json.Marshal(plan.Materialization)
	if materializationErr != nil {
		return model.Plan{}, errors.New("encode materialization plan")
	}
	if len(materialization) > 1024*1024 {
		return model.Plan{}, errors.New("materialization plan exceeds the closed byte limit")
	}
	plan.MaterializationSHA256 = digestBytes(materialization)
	plan.MaterializationCount = uint64(len(plan.Materialization))
	plan.TargetSHA256 = digestTargets(matched)
	planSHA, err := digestPlan(plan)
	if err != nil {
		return model.Plan{}, err
	}
	plan.PlanSHA256 = planSHA
	return plan, nil
}

func SourceDigest(snapshot []model.SnapshotRow) (string, map[string]uint64, error) {
	counts := make(map[string]uint64)
	_, digest, err := decodeSource(snapshot, counts)
	return digest, counts, err
}

func decodeSource(rows []model.SnapshotRow, counts map[string]uint64) (map[string][]sourceRow, string, error) {
	hash := sha256.New()
	result := make(map[string][]sourceRow)
	lastTable := ""
	var lastPayload []byte
	for _, row := range rows {
		if !validTableName(row.Table) || row.Table < lastTable {
			return nil, "", errors.New("source snapshot table order is invalid")
		}
		if row.Table != lastTable {
			if len(row.Payload) != 0 {
				return nil, "", errors.New("source snapshot table sentinel is missing")
			}
			lastPayload = nil
		} else if len(row.Payload) == 0 || lastPayload != nil && bytes.Compare(row.Payload, lastPayload) < 0 {
			return nil, "", errors.New("source snapshot row order is invalid")
		}
		lastTable = row.Table
		writeFramed(hash, []byte(row.Table))
		if _, exists := counts[row.Table]; !exists {
			counts[row.Table] = 0
		}
		if len(row.Payload) == 0 {
			continue
		}
		writeFramed(hash, row.Payload)
		lastPayload = append(lastPayload[:0], row.Payload...)
		var decoded sourceRow
		decoder := json.NewDecoder(bytes.NewReader(row.Payload))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return nil, "", errors.New("decode source snapshot row")
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return nil, "", errors.New("source snapshot row has trailing data")
		}
		result[row.Table] = append(result[row.Table], decoded)
		counts[row.Table]++
	}
	if len(counts) == 0 {
		return nil, "", errors.New("source snapshot inventory is empty")
	}
	return result, hex.EncodeToString(hash.Sum(nil)), nil
}

func validTableName(value string) bool {
	return inventory.Contains(value)
}

func mapSource(source map[string][]sourceRow, target []model.TargetResource, plan *model.Plan,
	matched map[string]model.TargetResource, mapping *[]string,
) {
	projects := indexTarget(target, "PROJECT", "slug")
	teams := indexTarget(target, "TEAM", "externalTeamRef")
	chats := indexTarget(target, "CHAT", "externalChannelRef")
	agents := indexTarget(target, "AGENT", "stableKey")
	sessions := indexTargetNumber(target, "SESSION", "agentSessionId")
	turns := indexTargetNumber(target, "TURN", "agentSessionTurnId")
	revisions := targetByID(target, "RUNTIME_REVISION")

	sourceProjects := byNumber(source["matter_codex_projects"], "id", plan)
	sourceChats := byNumber(source["matter_codex_chats"], "id", plan)
	sourceRoles := byNumber(source["matter_codex_agent_roles"], "id", plan)
	sourceSessions := byNumber(source["matter_codex_agent_sessions"], "id", plan)
	sourceTurns := byNumber(source["matter_codex_agent_session_turns"], "id", plan)
	sourceBotsByRole := make(map[int64]sourceRow)
	for _, bot := range source["matter_codex_mattermost_bot_identities"] {
		roleID := number(bot, "role_id")
		if roleID <= 0 {
			plan.Violations["unsupported_state"]++
			continue
		}
		if _, duplicate := sourceBotsByRole[roleID]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		sourceBotsByRole[roleID] = bot
	}

	projectMap := make(map[int64]model.TargetResource)
	teamMap := make(map[int64]model.TargetResource)
	for id, row := range sourceProjects {
		key := text(row, "slug")
		candidate, ok := uniqueTarget(projects[key], plan)
		if !ok || candidate.ProjectID != candidate.ID || candidate.OrganizationID == "" ||
			candidate.OwnerActorID == "" || candidate.State != "ACTIVE" {
			plan.Violations["ambiguous_target"]++
			continue
		}
		projectMap[id] = candidate
		match(plan, matched, "PROJECT", candidate)
		recordMapping(mapping, "PROJECT", id, candidate)
		team, found := uniqueGloballyScoped(teams[text(row, "mattermost_team_id")], candidate, plan)
		if !found || team.State != "ACTIVE" {
			delete(projectMap, id)
			continue
		}
		match(plan, matched, "TEAM", team)
		recordMapping(mapping, "TEAM", id, team)
		teamMap[id] = team
	}

	chatMap := make(map[int64]model.TargetResource)
	archivedChats := make(map[int64]bool)
	for id, row := range sourceChats {
		project, ok := projectMap[number(row, "project_id")]
		if !ok {
			plan.Violations["orphan_reference"]++
			continue
		}
		status := text(row, "status")
		if status != "active" && status != "archived" {
			plan.Violations["unknown_state"]++
			continue
		}
		candidates, hiddenMismatch := globallyScopedTargets(chats[text(row, "mattermost_channel_id")], project)
		if status == "archived" && len(candidates) == 0 && !hiddenMismatch {
			archivedChats[id] = true
			plan.Counts.Archive["CHAT"]++
			recordArchive(mapping, "CHAT", id)
			continue
		}
		if hiddenMismatch || len(candidates) != 1 {
			if hiddenMismatch {
				plan.Violations["tenant_mismatch"]++
			} else {
				plan.Violations["ambiguous_target"]++
			}
			continue
		}
		candidate := candidates[0]
		if stable := text(candidate.Spec, "stableKey"); stable != text(row, "slug") {
			plan.Violations["stale_reference"]++
			continue
		}
		if status == "active" && candidate.State != "ACTIVE" || status == "archived" && candidate.State != "ARCHIVED" {
			plan.Violations["unsupported_state"]++
			continue
		}
		chatMap[id] = candidate
		match(plan, matched, "CHAT", candidate)
		recordMapping(mapping, "CHAT", id, candidate)
	}

	roleMap := make(map[int64]model.TargetResource)
	for id, row := range sourceRoles {
		if !set("raw", "template")[text(row, "prompt_mode")] {
			plan.Violations["unknown_state"]++
			continue
		}
		project, ok := projectMap[number(row, "project_id")]
		if !ok {
			plan.Violations["orphan_reference"]++
			continue
		}
		candidate, found := uniqueScoped(agents[text(row, "name")], project, plan)
		if !found {
			continue
		}
		if candidate.State != "ACTIVE" || boolean(candidate.Spec, "enabled") != boolean(row, "enabled") {
			plan.Violations["unsupported_state"]++
			continue
		}
		if !validateAgentConfiguration(id, text(row, "prompt_sha256"), candidate, project, target, plan, matched, mapping) ||
			!validateAgentBot(candidate, teamMap[number(row, "project_id")], sourceBotsByRole[id], plan) {
			continue
		}
		roleMap[id] = candidate
		match(plan, matched, "AGENT", candidate)
		recordMapping(mapping, "AGENT", id, candidate)
	}
	mapSchedules(source, projectMap, chatMap, roleMap, target, plan, matched, mapping)

	sessionMap := make(map[int64]model.TargetResource)
	archivedSessions := make(map[int64]bool)
	for id, row := range sourceSessions {
		if !knownSession(text(row, "status")) {
			plan.Violations["unknown_state"]++
			continue
		}
		project, projectOK := projectMap[number(row, "project_id")]
		chat, chatOK := chatMap[number(row, "chat_id")]
		agent, roleOK := roleMap[number(row, "role_id")]
		if projectOK && archivedChats[number(row, "chat_id")] && roleOK && terminalSession(text(row, "status")) {
			archivedSessions[id] = true
			plan.Counts.Archive["SESSION"]++
			recordArchive(mapping, "SESSION", id)
			continue
		}
		if !projectOK || !chatOK || !roleOK {
			plan.Violations["orphan_reference"]++
			continue
		}
		if !sameBoundary(project, chat) || !sameBoundary(project, agent) {
			plan.Violations["tenant_mismatch"]++
			continue
		}
		candidates, hiddenMismatch := globallyScopedTargets(sessions[id], project)
		if hiddenMismatch || len(candidates) != 1 {
			if len(candidates) == 0 && !hiddenMismatch && terminalSession(text(row, "status")) {
				archivedSessions[id] = true
				plan.Counts.Archive["SESSION"]++
				recordArchive(mapping, "SESSION", id)
				continue
			}
			if hiddenMismatch {
				plan.Violations["tenant_mismatch"]++
			} else if len(candidates) > 1 {
				plan.Violations["ambiguous_target"]++
			} else {
				plan.Violations["unmaterialized_active"]++
			}
			continue
		}
		candidate := candidates[0]
		if text(candidate.Spec, "agentSessionKey") != text(row, "session_key") ||
			uint64(number(candidate.Spec, "agentSessionBindingVersion")) != uint64(number(row, "binding_version")) ||
			text(candidate.Spec, "agentId") != agent.ID || text(candidate.Spec, "conversationId") != chat.ID ||
			!validSHA(text(candidate.Spec, "agentSessionBindingSha256")) ||
			!sessionStateCompatible(text(row, "status"), candidate.State) {
			plan.Violations["stale_reference"]++
			continue
		}
		sessionMap[id] = candidate
		match(plan, matched, "SESSION", candidate)
		recordMapping(mapping, "SESSION", id, candidate)
	}

	turnMap := make(map[int64]model.TargetResource)
	for id, row := range sourceTurns {
		sessionID := number(row, "session_id")
		session, sessionOK := sessionMap[sessionID]
		status := text(row, "status")
		if !knownTurn(status) {
			plan.Violations["unknown_state"]++
			continue
		}
		if !sessionOK {
			if archivedSessions[sessionID] && terminalTurn(status) {
				plan.Counts.Archive["TURN"]++
				plan.Counts.Archive["ARTIFACT"] += artifactCardinality(row["artifacts"])
				recordArchive(mapping, "TURN", id)
				continue
			}
			plan.Violations["orphan_reference"]++
			continue
		}
		candidates, hiddenMismatch := globallyScopedTargets(turns[id], sessionProject(session))
		if len(candidates) == 1 && !hiddenMismatch {
			candidate := candidates[0]
			revisionID := text(candidate.Spec, "runtimeRevisionId")
			revision, revisionOK := revisions[revisionID]
			if text(candidate.Spec, "sessionId") != session.ID || !turnStateCompatible(status, candidate.State) ||
				text(candidate.Spec, "agentRunId") != text(row, "run_id") ||
				uint64(number(candidate.Spec, "agentTurnBindingVersion")) != uint64(number(row, "binding_version")) ||
				!validSHA(text(candidate.Spec, "agentTurnBindingSha256")) || !revisionOK ||
				!sameBoundary(session, revision) || text(revision.Spec, "sessionId") != session.ID ||
				!validSHA(text(revision.Spec, "effectiveRuntimeSha256")) {
				plan.Violations["broken_lineage"]++
				continue
			}
			turnMap[id] = candidate
			match(plan, matched, "TURN", candidate)
			match(plan, matched, "RUNTIME_REVISION", revision)
			recordMapping(mapping, "TURN", id, candidate)
			recordMapping(mapping, "RUNTIME_REVISION", id, revision)
			mapTurnDetails(candidate, revision, target, plan, matched, mapping, text(row, "status"))
			continue
		}
		if hiddenMismatch {
			plan.Violations["tenant_mismatch"]++
		} else if len(candidates) > 1 {
			plan.Violations["ambiguous_target"]++
		} else {
			plan.Violations["unmaterialized_active"]++
		}
	}

	mapProcesses(source, turnMap, archivedSessions, sourceSessions, sourceTurns, plan, matched, mapping, target)
	validateSessionCurrentTuple(sourceSessions, sourceTurns, turnMap, plan)
	validateBindingReceipts(source, sessionMap, turnMap, sourceTurns, archivedSessions, target, plan, mapping)
	validateAgentRuns(source, turnMap, sourceTurns, archivedSessions, plan, mapping)
}

func mapSchedules(source map[string][]sourceRow, projects, chats, agents map[int64]model.TargetResource,
	target []model.TargetResource, plan *model.Plan, matched map[string]model.TargetResource, mapping *[]string,
) {
	currentSchedules := targetByID(target, "SCHEDULE")
	for _, row := range source["matter_codex_automation_schedules"] {
		id := number(row, "id")
		if id <= 0 {
			plan.Violations["unsupported_state"]++
			continue
		}
		if !boolean(row, "enabled") {
			plan.Counts.Archive["SCHEDULE"]++
			recordArchive(mapping, "SCHEDULE", id)
			continue
		}
		project, projectOK := projects[number(row, "project_id")]
		chat, chatOK := chats[number(row, "target_chat_id")]
		agent, agentOK := agents[number(row, "target_agent_role_id")]
		if !projectOK || !chatOK || !agentOK {
			plan.Violations["orphan_reference"]++
			continue
		}
		if !sameBoundary(project, chat) || !sameBoundary(project, agent) {
			plan.Violations["tenant_mismatch"]++
			continue
		}
		command, preview, ok := scheduleMaterialization(row, project, chat, agent, target, plan)
		if !ok {
			continue
		}
		if existing, exists := currentSchedules[preview.ID]; exists {
			if existing.OrganizationID != preview.OrganizationID || existing.ProjectID != preview.ProjectID ||
				existing.ParentID != preview.ParentID ||
				existing.OwnerActorID != preview.OwnerActorID || existing.Kind != preview.Kind ||
				existing.Name != preview.Name ||
				existing.State != preview.State || existing.Version != preview.Version ||
				!equalJSON(existing.Spec, preview.Spec) {
				plan.Violations["stale_reference"]++
				continue
			}
			preview = existing
		}
		plan.Materialization = append(plan.Materialization, command)
		match(plan, matched, "SCHEDULE", preview)
		recordMapping(mapping, "SCHEDULE", id, preview)
	}
}

func scheduleMaterialization(row sourceRow, project, chat, agent model.TargetResource,
	target []model.TargetResource, plan *model.Plan,
) (model.MaterializationCommand, model.TargetResource, bool) {
	if text(row, "preset") != "daily" || text(row, "local_time") == "" || text(row, "time_zone") == "" ||
		!validExternalRef(text(row, "playbook_key")) || !validExternalRef(text(row, "prompt_version")) ||
		!validExternalRef(text(row, "callback_contract_version")) || !validSHA(text(row, "prompt_sha256")) ||
		!validSHA(text(row, "command_hash")) || !validLegacyScheduleID(text(row, "public_id")) {
		plan.Violations["unsupported_state"]++
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	if _, err := time.LoadLocation(text(row, "time_zone")); err != nil {
		plan.Violations["unsupported_state"]++
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	nextRunAt, err := time.Parse(time.RFC3339Nano, text(row, "next_run_at"))
	if err != nil || nextRunAt.IsZero() {
		plan.Violations["unsupported_state"]++
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, text(row, "updated_at"))
	if err != nil || updatedAt.UnixMicro() <= 0 {
		plan.Violations["unsupported_state"]++
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	sourceRevision := uint64(updatedAt.UTC().UnixMicro())
	agentSHA, ok := protectedProjectionSHA(target, agent, plan)
	if !ok {
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	instruction, instructionOK := exactCurrentReference(target, agent, "INSTRUCTION_SET",
		text(agent.Spec, "instructionSetId"), number(agent.Spec, "instructionSetVersion"),
		text(agent.Spec, "instructionSetSha256"), plan)
	pool, poolOK := exactCurrentReference(target, agent, "PROVIDER_POOL",
		text(agent.Spec, "providerPoolId"), number(agent.Spec, "providerPoolVersion"),
		text(agent.Spec, "providerPoolSha256"), plan)
	if !instructionOK || !poolOK || text(instruction.Spec, "versionState") != "PUBLISHED" {
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	if text(instruction.Spec, "contentSha256") != text(row, "prompt_sha256") {
		plan.Violations["stale_reference"]++
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	var assignment model.TargetResource
	for _, candidate := range target {
		if candidate.Historical || candidate.Kind != "AGENT_ASSIGNMENT" || candidate.State != "ACTIVE" ||
			text(candidate.Spec, "agentId") != agent.ID || text(candidate.Spec, "roomId") != chat.ID ||
			!sameBoundary(project, candidate) {
			continue
		}
		if assignment.ID != "" {
			plan.Violations["ambiguous_target"]++
			return model.MaterializationCommand{}, model.TargetResource{}, false
		}
		assignment = candidate
	}
	assignmentSHA, assignmentOK := protectedProjectionSHA(target, assignment, plan)
	if assignment.ID == "" || !assignmentOK {
		plan.Violations["unmaterialized_active"]++
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	prompt, promptOK := currentResource(target, "ARTIFACT", text(instruction.Spec, "contentArtifactId"), instruction, plan)
	if !promptOK || prompt.Version != uint64(number(instruction.Spec, "contentArtifactVersion")) ||
		text(prompt.Spec, "sha256") != text(row, "prompt_sha256") || text(prompt.Spec, "direction") != "INPUT" ||
		text(prompt.Spec, "mediaType") != "text/markdown" || !eligibleArtifact(prompt) {
		if promptOK {
			plan.Violations["unsupported_state"]++
		}
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	parts := strings.Split(text(row, "local_time"), ":")
	if len(parts) != 2 {
		plan.Violations["unsupported_state"]++
		return model.MaterializationCommand{}, model.TargetResource{}, false
	}
	sourceRef := "control-plane://legacy-schedule/" + text(row, "public_id")
	targetID := deterministicLegacyUUID("mattercodex:legacy-schedule:" + text(row, "public_id"))
	spec := map[string]any{
		"targetResourceId": agent.ID, "targetKind": "AGENT", "targetVersion": agent.Version,
		"effectiveInputSha256": text(row, "command_hash"), "cron": parts[1] + " " + parts[0] + " * * *",
		"timezone": text(row, "time_zone"), "calendar": "GREGORIAN", "overlapPolicy": "FORBID",
		"misfirePolicy": "RUN_ONCE", "misfireGrace": int64(0),
		"nextRunAt":      nextRunAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano),
		"deliveryPolicy": "EXACTLY_ONCE_EFFECT", "maximumAttempts": 3, "initialBackoff": int64(5 * time.Second),
		"maximumBackoff": int64(time.Minute), "deadLetterAfter": int64(24 * time.Hour), "sessionPolicy": "NEW",
		"roomId": chat.ID, "notificationPolicy": "ON_ACTION_OR_FAILURE", "maximumExecutionDuration": int64(90 * time.Minute),
		"coalesce": true, "targetType": "PLAYBOOK", "playbookRef": text(row, "playbook_key"), "playbookVersion": 1,
		"promptArtifactId": prompt.ID, "ownership": map[string]any{"managedBy": "UI", "sourceRef": sourceRef,
			"sourceRevision": sourceRevision, "sourceSha256": text(row, "command_hash")},
		"agentId": agent.ID, "agentVersion": agent.Version, "agentSha256": agentSHA,
		"instructionSetId": instruction.ID, "instructionSetVersion": instruction.Version,
		"instructionSetSha256":    text(agent.Spec, "instructionSetSha256"),
		"runtimeSelectionRef":     text(agent.Spec, "runtimeProfileRef"),
		"runtimeSelectionVersion": number(agent.Spec, "runtimeProfileVersion"),
		"runtimeSelectionSha256":  text(agent.Spec, "runtimeProfileSha256"),
		"providerPoolId":          pool.ID, "providerPoolVersion": pool.Version,
		"providerPoolSha256": text(agent.Spec, "providerPoolSha256"),
		"agentAssignmentId":  assignment.ID, "agentAssignmentVersion": assignment.Version,
		"agentAssignmentSha256": assignmentSHA,
	}
	command := model.MaterializationCommand{Operation: "UPSERT_SCHEDULE",
		SourceTable: "matter_codex_automation_schedules", SourceID: number(row, "id"),
		SourcePublicID: text(row, "public_id"), SourceRevision: sourceRevision,
		SourceDigest: text(row, "command_hash"),
		TargetID:     targetID, ProjectSlug: text(project.Spec, "slug"), AgentStableKey: text(agent.Spec, "stableKey"),
		ChatStableKey: text(chat.Spec, "stableKey"), PromptSHA256: text(row, "prompt_sha256"),
		LocalTime: text(row, "local_time"), Timezone: text(row, "time_zone"),
		NextRunAt:   nextRunAt.UTC().Truncate(time.Microsecond),
		PlaybookKey: text(row, "playbook_key"), PromptVersion: text(row, "prompt_version"),
		CallbackContractVersion: text(row, "callback_contract_version")}
	preview := model.TargetResource{ID: targetID, OrganizationID: project.OrganizationID, ProjectID: project.ProjectID,
		ParentID:     project.ID,
		OwnerActorID: project.OwnerActorID, Kind: "SCHEDULE",
		Name: "Legacy schedule " + text(row, "public_id"), State: "ACTIVE", Version: 1, Spec: spec}
	return command, preview, true
}

func deterministicLegacyUUID(value string) string {
	digest := md5.Sum([]byte(value)) // Совпадает с PostgreSQL md5(text)::uuid; не security hash.
	encoded := hex.EncodeToString(digest[:])
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}

func validLegacyScheduleID(value string) bool {
	if len(value) != len("schedule-")+32 || !strings.HasPrefix(value, "schedule-") {
		return false
	}
	for _, symbol := range strings.TrimPrefix(value, "schedule-") {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}

func equalJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func validateAgentConfiguration(sourceRoleID int64, sourcePromptSHA string, agent, project model.TargetResource,
	target []model.TargetResource, plan *model.Plan, matched map[string]model.TargetResource, mapping *[]string,
) bool {
	agentSHA, valid := protectedProjectionSHA(target, agent, plan)
	references := []struct {
		kind, idKey, versionKey, shaKey string
	}{
		{"ROLE_DEFINITION", "roleDefinitionId", "roleDefinitionVersion", "roleDefinitionSha256"},
		{"INSTRUCTION_SET", "instructionSetId", "instructionSetVersion", "instructionSetSha256"},
		{"PROVIDER_POOL", "providerPoolId", "providerPoolVersion", "providerPoolSha256"},
	}
	resolved := make(map[string]model.TargetResource, len(references))
	for _, reference := range references {
		resource, ok := exactCurrentReference(target, agent, reference.kind, text(agent.Spec, reference.idKey),
			number(agent.Spec, reference.versionKey), text(agent.Spec, reference.shaKey), plan)
		if !ok {
			valid = false
			continue
		}
		resolved[reference.kind] = resource
		match(plan, matched, reference.kind, resource)
		recordMapping(mapping, "AGENT_CONFIGURATION", sourceRoleID, resource)
	}
	runtimePrefix := "control-plane://runtime-profile/"
	runtimeReference := text(agent.Spec, "runtimeProfileRef")
	if !strings.HasPrefix(runtimeReference, runtimePrefix) || len(runtimeReference) == len(runtimePrefix) {
		plan.Violations["broken_lineage"]++
		valid = false
	} else if resource, ok := exactCurrentReference(target, agent, "ROLE_IMAGE_RECIPE",
		strings.TrimPrefix(runtimeReference, runtimePrefix), number(agent.Spec, "runtimeProfileVersion"),
		text(agent.Spec, "runtimeProfileSha256"), plan); ok {
		match(plan, matched, "ROLE_IMAGE_RECIPE", resource)
		recordMapping(mapping, "AGENT_CONFIGURATION", sourceRoleID, resource)
	} else {
		valid = false
	}

	providerPool, poolOK := resolved["PROVIDER_POOL"]
	bindings, bindingsOK := providerPool.Spec["bindings"].([]any)
	if instruction, ok := resolved["INSTRUCTION_SET"]; ok {
		contentVersion := number(instruction.Spec, "currentVersion")
		artifactID := text(instruction.Spec, "contentArtifactId")
		if sourcePromptSHA != "" && text(instruction.Spec, "contentSha256") != sourcePromptSHA {
			plan.Violations["stale_reference"]++
			valid = false
		}
		if text(instruction.Spec, "versionState") != "PUBLISHED" || contentVersion <= 0 ||
			number(instruction.Spec, "publishedVersion") != contentVersion ||
			!validSHA(text(instruction.Spec, "contentSha256")) || artifactID == "" ||
			number(instruction.Spec, "contentArtifactVersion") <= 0 {
			plan.Violations["unsupported_state"]++
			valid = false
		} else if artifact, artifactOK := currentResource(target, "ARTIFACT", artifactID, instruction, plan); !artifactOK {
			valid = false
		} else if artifact.Version != uint64(number(instruction.Spec, "contentArtifactVersion")) ||
			text(artifact.Spec, "sha256") != text(instruction.Spec, "contentSha256") ||
			text(artifact.Spec, "direction") != "INPUT" || text(artifact.Spec, "mediaType") != "text/markdown" {
			plan.Violations["stale_reference"]++
			valid = false
		} else if !eligibleArtifact(artifact) {
			plan.Violations["unsupported_state"]++
			valid = false
		} else {
			match(plan, matched, "ARTIFACT", artifact)
			recordMapping(mapping, "AGENT_CONFIGURATION", sourceRoleID, artifact)
		}
	}
	if poolOK && (!bindingsOK || len(bindings) == 0 ||
		!validSHA(text(providerPool.Spec, "eligibilitySnapshotSha256"))) {
		plan.Violations["broken_lineage"]++
		valid = false
	}
	for _, rawBinding := range bindings {
		binding, ok := rawBinding.(map[string]any)
		if !ok || !boolean(binding, "eligible") ||
			!set("AVAILABLE", "DEGRADED")[text(binding, "maskedStatus")] {
			plan.Violations["broken_lineage"]++
			valid = false
			continue
		}
		reference, ok := exactCurrentReference(target, agent, "PROVIDER_CONNECTION_REFERENCE",
			text(binding, "providerConnectionReferenceId"), number(binding, "referenceVersion"),
			text(binding, "referenceSha256"), plan)
		if !ok || text(reference.Spec, "stableKey") != text(binding, "providerConnectionStableKey") ||
			!boolean(reference.Spec, "eligible") ||
			!set("AVAILABLE", "DEGRADED")[text(reference.Spec, "maskedStatus")] {
			if ok {
				plan.Violations["stale_reference"]++
			}
			valid = false
			continue
		}
		match(plan, matched, "PROVIDER_CONNECTION_REFERENCE", reference)
		recordMapping(mapping, "AGENT_CONFIGURATION", sourceRoleID, reference)
	}

	assignments := make([]model.TargetResource, 0, 1)
	hiddenBoundary := false
	for _, resource := range target {
		if resource.Historical || resource.Kind != "AGENT_ASSIGNMENT" || text(resource.Spec, "agentId") != agent.ID {
			continue
		}
		if sameBoundary(resource, project) {
			assignments = append(assignments, resource)
		} else {
			hiddenBoundary = true
		}
	}
	if hiddenBoundary {
		plan.Violations["tenant_mismatch"]++
		valid = false
	}
	if boolean(agent.Spec, "enabled") && len(assignments) != 1 || len(assignments) > 1 {
		plan.Violations["ambiguous_target"]++
		valid = false
	}
	if len(assignments) == 1 {
		assignment := assignments[0]
		if assignment.State != "ACTIVE" || assignment.Version == 0 ||
			uint64(number(assignment.Spec, "agentVersion")) != agent.Version ||
			text(assignment.Spec, "agentSha256") != agentSHA ||
			text(assignment.Spec, "workspaceId") != project.ID ||
			uint64(number(assignment.Spec, "workspaceVersion")) != project.Version ||
			!validSHA(text(assignment.Spec, "workspaceSha256")) ||
			text(assignment.Spec, "rootActorId") != project.OwnerActorID ||
			number(assignment.Spec, "assignmentGeneration") <= 0 {
			plan.Violations["stale_reference"]++
			valid = false
		} else if roomID := text(assignment.Spec, "roomId"); roomID != "" &&
			!currentResourceInBoundary(target, "CHAT", roomID, project) {
			plan.Violations["tenant_mismatch"]++
			valid = false
		} else {
			match(plan, matched, "AGENT_ASSIGNMENT", assignment)
			recordMapping(mapping, "AGENT_ASSIGNMENT", sourceRoleID, assignment)
		}
	}
	return valid
}

func validateAgentBot(agent, team model.TargetResource, sourceBot sourceRow, plan *model.Plan) bool {
	if sourceBot == nil || text(sourceBot, "status") != "configured" {
		return true
	}
	if team.ID == "" || text(sourceBot, "mattermost_user_id") == "" || text(sourceBot, "username") == "" ||
		text(agent.Spec, "botIdentityRef") != text(sourceBot, "mattermost_user_id") ||
		text(agent.Spec, "botUsername") != text(sourceBot, "username") ||
		text(agent.Spec, "botProviderTeamRef") != text(team.Spec, "externalTeamRef") ||
		text(agent.Spec, "botMaskedStatus") != "AVAILABLE" ||
		number(agent.Spec, "botProviderRevision") <= 0 || number(agent.Spec, "botProviderGeneration") <= 0 ||
		text(agent.Spec, "botReceiptId") == "" || number(agent.Spec, "botReceiptVersion") <= 0 ||
		!validSHA(text(agent.Spec, "botReceiptSha256")) {
		plan.Violations["stale_reference"]++
		return false
	}
	return true
}

func exactCurrentReference(target []model.TargetResource, boundary model.TargetResource, kind, id string,
	version int64, expectedSHA string, plan *model.Plan,
) (model.TargetResource, bool) {
	if id == "" || version <= 0 || !validSHA(expectedSHA) {
		plan.Violations["broken_lineage"]++
		return model.TargetResource{}, false
	}
	candidates := make([]model.TargetResource, 0, 1)
	hiddenBoundary := false
	for _, resource := range target {
		if resource.Historical || resource.Kind != kind || resource.ID != id {
			continue
		}
		if sameBoundary(resource, boundary) {
			candidates = append(candidates, resource)
		} else {
			hiddenBoundary = true
		}
	}
	if hiddenBoundary {
		plan.Violations["tenant_mismatch"]++
		return model.TargetResource{}, false
	}
	if len(candidates) != 1 {
		plan.Violations["ambiguous_target"]++
		return model.TargetResource{}, false
	}
	resource := candidates[0]
	projectionSHA, projectionOK := "", false
	if resource.Kind == "ROLE_IMAGE_RECIPE" {
		projectionSHA, projectionOK = resource.ProjectionSHA256, validSHA(resource.ProjectionSHA256)
		if !projectionOK {
			plan.Violations["broken_lineage"]++
		}
	} else {
		projectionSHA, projectionOK = protectedProjectionSHA(target, resource, plan)
	}
	if resource.State != "ACTIVE" || resource.Version != uint64(version) || !projectionOK || projectionSHA != expectedSHA {
		plan.Violations["stale_reference"]++
		return model.TargetResource{}, false
	}
	return resource, true
}

func protectedProjectionSHA(target []model.TargetResource, resource model.TargetResource, plan *model.Plan) (string, bool) {
	values := make(map[string]struct{}, 1)
	hiddenBoundary := false
	for _, historical := range target {
		if !historical.Historical || historical.Kind != resource.Kind || historical.ID != resource.ID ||
			historical.Version != resource.Version {
			continue
		}
		if sameBoundary(historical, resource) {
			if validSHA(historical.ProjectionSHA256) {
				values[historical.ProjectionSHA256] = struct{}{}
			} else {
				plan.Violations["broken_lineage"]++
			}
		} else {
			hiddenBoundary = true
		}
	}
	if hiddenBoundary {
		plan.Violations["tenant_mismatch"]++
		return "", false
	}
	if len(values) != 1 {
		plan.Violations["broken_lineage"]++
		return "", false
	}
	for value := range values {
		return value, true
	}
	return "", false
}

func currentResourceInBoundary(target []model.TargetResource, kind, id string, boundary model.TargetResource) bool {
	_, ok := currentResource(target, kind, id, boundary, nil)
	return ok
}

func currentResource(target []model.TargetResource, kind, id string, boundary model.TargetResource,
	plan *model.Plan,
) (model.TargetResource, bool) {
	candidates := make([]model.TargetResource, 0, 1)
	hiddenBoundary := false
	for _, resource := range target {
		if resource.Historical || resource.Kind != kind || resource.ID != id || resource.State != "ACTIVE" {
			continue
		}
		if sameBoundary(resource, boundary) {
			candidates = append(candidates, resource)
		} else {
			hiddenBoundary = true
		}
	}
	if plan != nil {
		if hiddenBoundary {
			plan.Violations["tenant_mismatch"]++
		} else if len(candidates) != 1 {
			plan.Violations["ambiguous_target"]++
		}
	}
	return firstTarget(candidates), !hiddenBoundary && len(candidates) == 1
}

func firstTarget(values []model.TargetResource) model.TargetResource {
	if len(values) == 0 {
		return model.TargetResource{}
	}
	return values[0]
}

func validateAgentRuns(source map[string][]sourceRow, targetTurns map[int64]model.TargetResource,
	sourceTurns map[int64]sourceRow, archivedSessions map[int64]bool, plan *model.Plan, mapping *[]string,
) {
	turnsByRun := make(map[string]sourceRow, len(sourceTurns))
	for _, turn := range sourceTurns {
		runID := text(turn, "run_id")
		if runID == "" {
			plan.Violations["unsupported_state"]++
			continue
		}
		if _, duplicate := turnsByRun[runID]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		turnsByRun[runID] = turn
	}
	runsByID := make(map[string]sourceRow, len(source["matter_codex_agent_runs"]))
	for _, run := range source["matter_codex_agent_runs"] {
		runID := text(run, "run_id")
		status := text(run, "status")
		if runID == "" || number(run, "id") <= 0 {
			plan.Violations["unsupported_state"]++
			continue
		}
		if _, duplicate := runsByID[runID]; duplicate {
			plan.Violations["duplicate_source"]++
			continue
		}
		runsByID[runID] = run
		active, known := agentRunActive(status)
		if !known {
			plan.Violations["unknown_state"]++
			continue
		}
		sourceTurn, linked := turnsByRun[runID]
		if !linked {
			if active {
				plan.Violations["unmaterialized_active"]++
			} else {
				plan.Counts.Archive["AGENT_RUN"]++
				recordArchive(mapping, "AGENT_RUN", number(run, "id"))
			}
			continue
		}
		turnID := number(sourceTurn, "id")
		if active == terminalTurn(text(sourceTurn, "status")) {
			plan.Violations["stale_reference"]++
			continue
		}
		if targetTurn, mapped := targetTurns[turnID]; mapped {
			plan.Counts.Mapped["AGENT_RUN"]++
			recordMapping(mapping, "AGENT_RUN", number(run, "id"), targetTurn)
		} else if archivedSessions[number(sourceTurn, "session_id")] && terminalTurn(text(sourceTurn, "status")) {
			plan.Counts.Archive["AGENT_RUN"]++
			recordArchive(mapping, "AGENT_RUN", number(run, "id"))
		}
	}
	for _, turn := range sourceTurns {
		if runsByID[text(turn, "run_id")] == nil {
			plan.Violations["orphan_reference"]++
		}
	}
}

func agentRunActive(status string) (bool, bool) {
	if set("started", "pending", "queued", "running", "blocked", "capacity_retry")[status] {
		return true, true
	}
	if set("succeeded", "failed", "canceled", "cancelled", "completed", "completed_no_changes", "pr_created",
		"approved", "changes_requested", "review_comment", "review_submitted", "cleaned")[status] {
		return false, true
	}
	return false, false
}

func validateSessionCurrentTuple(sourceSessions, sourceTurns map[int64]sourceRow,
	targetTurns map[int64]model.TargetResource, plan *model.Plan,
) {
	for sessionID, session := range sourceSessions {
		activeTurnID := number(session, "active_turn_id")
		activeRunID := text(session, "active_run_id")
		if text(session, "status") == "running" {
			activeTurn, sourceExists := sourceTurns[activeTurnID]
			_, targetExists := targetTurns[activeTurnID]
			if activeTurnID <= 0 || !sourceExists || number(activeTurn, "session_id") != sessionID ||
				activeRunID == "" || activeRunID != text(activeTurn, "run_id") || !targetExists {
				plan.Violations["broken_lineage"]++
			}
		} else if activeTurnID != 0 || activeRunID != "" {
			plan.Violations["stale_reference"]++
		}
	}
}

func mapProcesses(source map[string][]sourceRow, turns map[int64]model.TargetResource,
	archivedSessions map[int64]bool, sourceSessions, sourceTurns map[int64]sourceRow, plan *model.Plan,
	matched map[string]model.TargetResource, mapping *[]string, target []model.TargetResource,
) {
	processes := byNumber(source["matter_codex_process_runs"], "id", plan)
	links := source["matter_codex_process_turns"]
	policies := byNumber(source["matter_codex_policy_revisions"], "id", plan)
	delegationsByTargetTurn := make(map[int64]sourceRow)
	for _, delegation := range source["matter_codex_agent_delegations"] {
		targetTurnID := number(delegation, "target_turn_id")
		if targetTurnID == 0 {
			continue
		}
		if _, duplicate := delegationsByTargetTurn[targetTurnID]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		delegationsByTargetTurn[targetTurnID] = delegation
	}
	targetProcesses := targetByID(target, "PROCESS_RUN")
	targetSessions := targetByID(target, "SESSION")
	targetRevisions := targetByID(target, "RUNTIME_REVISION")
	targetArtifacts := targetByID(target, "ARTIFACT")
	processTurns := make(map[int64][]int64)
	turnProcesses := make(map[int64]int64)
	linksByTurn := make(map[int64]sourceRow)
	for _, link := range links {
		processID, turnID := number(link, "process_run_id"), number(link, "turn_id")
		if _, ok := processes[processID]; !ok {
			plan.Violations["orphan_reference"]++
			continue
		}
		if _, ok := sourceTurns[turnID]; !ok {
			plan.Violations["orphan_reference"]++
			continue
		}
		if previous, duplicate := turnProcesses[turnID]; duplicate && previous != processID {
			plan.Violations["duplicate_source"]++
			continue
		}
		turnProcesses[turnID] = processID
		linksByTurn[turnID] = link
		processTurns[processID] = append(processTurns[processID], turnID)
	}
	for _, link := range links {
		parentID := number(link, "parent_turn_id")
		if parentID != 0 && turnProcesses[parentID] != number(link, "process_run_id") {
			plan.Violations["broken_lineage"]++
		}
	}
	for id, process := range processes {
		if len(processTurns[id]) == 0 {
			plan.Violations["broken_lineage"]++
			continue
		}
		var targetID string
		var boundary model.TargetResource
		targetTurns := make(map[string]model.TargetResource)
		rootSourceTurnID := int64(0)
		allMapped := true
		allArchived := true
		for _, turnID := range processTurns[id] {
			sourceTurn, sourceTurnOK := sourceTurns[turnID]
			sourceSession, sourceSessionOK := sourceSessions[number(sourceTurn, "session_id")]
			if !sourceTurnOK || !sourceSessionOK || number(sourceSession, "project_id") != number(process, "project_id") {
				plan.Violations["tenant_mismatch"]++
				allMapped, allArchived = false, false
				continue
			}
			if turn, ok := turns[turnID]; ok {
				link := linksByTurn[turnID]
				parentSourceTurnID := number(link, "parent_turn_id")
				expectedPredecessor := ""
				if parentSourceTurnID == 0 {
					if rootSourceTurnID != 0 {
						plan.Violations["broken_lineage"]++
					}
					rootSourceTurnID = turnID
				} else if parent, exists := turns[parentSourceTurnID]; exists {
					expectedPredecessor = parent.ID
				} else {
					plan.Violations["broken_lineage"]++
				}
				if text(turn.Spec, "predecessorTurnId") != expectedPredecessor {
					plan.Violations["broken_lineage"]++
				}
				targetTurns[turn.ID] = turn
				plan.Counts.Mapped["PROCESS_TURN"]++
				recordMapping(mapping, "PROCESS_TURN", turnID, turn)
				candidate := text(turn.Spec, "processRunId")
				if targetID != "" && candidate != targetID {
					plan.Violations["broken_lineage"]++
				}
				targetID = candidate
				if boundary.ID == "" {
					boundary = turn
				} else if !sameBoundary(boundary, turn) {
					plan.Violations["tenant_mismatch"]++
				}
				allArchived = false
				continue
			}
			allMapped = false
			if !archivedSessions[number(sourceTurn, "session_id")] || !terminalTurn(text(sourceTurn, "status")) {
				allArchived = false
			} else {
				plan.Counts.Archive["PROCESS_TURN"]++
				recordArchive(mapping, "PROCESS_TURN", turnID)
			}
		}
		if allMapped && targetID != "" {
			candidate, ok := targetProcesses[targetID]
			rootTurn, rootExists := targetTurns[text(candidate.Spec, "rootTurnId")]
			rootSession, rootSessionExists := targetSessions[text(candidate.Spec, "rootSessionId")]
			runtimeRevision, runtimeRevisionExists := targetRevisions[text(candidate.Spec, "runtimeRevisionId")]
			policy, policyExists := policies[number(process, "policy_revision_id")]
			expectedRoot, expectedRootExists := turns[rootSourceTurnID]
			if !ok || !processStateCompatible(text(process, "status"), candidate.State) ||
				!sameBoundary(boundary, candidate) || !rootExists || !rootSessionExists || !runtimeRevisionExists ||
				!policyExists || number(policy, "project_id") != number(process, "project_id") ||
				text(policy, "status") != "active" || number(policy, "version") <= 0 ||
				number(candidate.Spec, "policyRevision") != number(policy, "version") ||
				!expectedRootExists || candidate.Spec["rootTurnId"] != expectedRoot.ID ||
				text(candidate.Spec, "rootInitiatorActorId") != candidate.OwnerActorID ||
				text(process, "root_initiator_user_id") == "" || text(process, "root_trigger_post_id") == "" ||
				text(candidate.Spec, "rootTriggerRef") != "mattermost-post:"+text(process, "root_trigger_post_id") ||
				text(candidate.Spec, "rootSessionId") != text(rootTurn.Spec, "sessionId") ||
				rootSession.ID != text(rootTurn.Spec, "sessionId") || !sameBoundary(candidate, rootSession) ||
				number(candidate.Spec, "rootSessionVersion") != int64(rootSession.Version) ||
				number(candidate.Spec, "rootTurnVersion") != int64(rootTurn.Version) ||
				number(candidate.Spec, "rootAttempt") != number(rootTurn.Spec, "attempt") ||
				text(candidate.Spec, "runtimeRevisionId") != text(rootTurn.Spec, "runtimeRevisionId") ||
				!sameBoundary(candidate, runtimeRevision) ||
				number(runtimeRevision.Spec, "authorityPolicyRevision") != number(policy, "version") ||
				!validSHA(text(runtimeRevision.Spec, "authorityPolicySha256")) ||
				text(candidate.Spec, "immutableInputSha256") != text(rootTurn.Spec, "effectiveInputSha256") ||
				!validSHA(text(candidate.Spec, "immutableInputSha256")) {
				plan.Violations["broken_lineage"]++
				continue
			}
			if delegation, launched := delegationsByTargetTurn[rootSourceTurnID]; launched {
				launchSourceTurnID := number(delegation, "source_turn_id")
				launchTurn, launchMapped := turns[launchSourceTurnID]
				launchProcessID := turnProcesses[launchSourceTurnID]
				launchProcessTargetID := ""
				if launchMapped {
					launchProcessTargetID = text(launchTurn.Spec, "processRunId")
				}
				if !launchMapped || launchProcessID == 0 || launchProcessID == id ||
					text(candidate.Spec, "parentProcessRunId") != launchProcessTargetID ||
					text(candidate.Spec, "launchingProcessRunId") != launchProcessTargetID ||
					text(candidate.Spec, "launchingTurnId") != launchTurn.ID ||
					number(candidate.Spec, "launchingAttempt") != number(launchTurn.Spec, "attempt") ||
					text(candidate.Spec, "delegationId") == "" ||
					text(candidate.Spec, "targetSessionId") != text(expectedRoot.Spec, "sessionId") ||
					number(candidate.Spec, "targetSessionVersion") != int64(rootSession.Version) ||
					text(candidate.Spec, "targetTurnId") != expectedRoot.ID ||
					number(candidate.Spec, "targetTurnVersion") != int64(expectedRoot.Version) ||
					number(candidate.Spec, "targetAttempt") != number(expectedRoot.Spec, "attempt") {
					plan.Violations["broken_lineage"]++
					continue
				}
			} else if text(candidate.Spec, "parentProcessRunId") != "" ||
				text(candidate.Spec, "launchingProcessRunId") != "" || text(candidate.Spec, "launchingTurnId") != "" ||
				text(candidate.Spec, "delegationId") != "" {
				plan.Violations["broken_lineage"]++
				continue
			}
			if resultArtifactID := text(candidate.Spec, "resultArtifactId"); resultArtifactID != "" {
				artifact, artifactExists := targetArtifacts[resultArtifactID]
				if !artifactExists || !sameBoundary(candidate, artifact) || !eligibleArtifact(artifact) ||
					!revisionPinsArtifact(runtimeRevision, artifact) {
					plan.Violations["broken_lineage"]++
					continue
				}
				match(plan, matched, "ARTIFACT", artifact)
				recordMapping(mapping, "PROCESS_RESULT_ARTIFACT", id, artifact)
			}
			match(plan, matched, "PROCESS_RUN", candidate)
			recordMapping(mapping, "PROCESS_RUN", id, candidate)
			continue
		}
		if allArchived && terminalProcess(text(process, "status")) {
			plan.Counts.Archive["PROCESS_RUN"]++
			recordArchive(mapping, "PROCESS_RUN", id)
			continue
		}
		if !knownProcess(text(process, "status")) {
			plan.Violations["unknown_state"]++
		} else {
			plan.Violations["broken_lineage"]++
		}
	}
}

func mapTurnDetails(turn, revision model.TargetResource, target []model.TargetResource, plan *model.Plan,
	matched map[string]model.TargetResource, mapping *[]string, sourceStatus string,
) {
	validateRevision(revision, target, plan, matched, mapping, number(turn.Spec, "agentSessionTurnId"))
	artifacts := targetByID(target, "ARTIFACT")
	artifactIDs := []string{text(turn.Spec, "promptArtifactId"), text(turn.Spec, "resultArtifactId")}
	if values, ok := turn.Spec["inputArtifacts"].([]any); ok {
		for _, value := range values {
			if reference, ok := value.(map[string]any); ok {
				artifactIDs = append(artifactIDs, text(reference, "artifactId"))
			}
		}
	}
	seenArtifact := make(map[string]struct{})
	for index, artifactID := range artifactIDs {
		if artifactID == "" && index != 0 {
			continue
		}
		artifact, ok := artifacts[artifactID]
		if !ok || !sameBoundary(turn, artifact) || !eligibleArtifact(artifact) ||
			!revisionPinsArtifact(revision, artifact) {
			plan.Violations["broken_lineage"]++
			continue
		}
		if _, exists := seenArtifact[artifactID]; !exists {
			seenArtifact[artifactID] = struct{}{}
			match(plan, matched, "ARTIFACT", artifact)
			recordMapping(mapping, "ARTIFACT", number(turn.Spec, "agentSessionTurnId"), artifact)
		}
	}
	resultArtifactID := text(turn.Spec, "resultArtifactId")
	if resultArtifactID != "" {
		artifact := artifacts[resultArtifactID]
		if artifact.Version != uint64(number(turn.Spec, "resultArtifactVersion")) ||
			text(artifact.Spec, "sha256") != text(turn.Spec, "resultArtifactSha256") ||
			!validSHA(text(turn.Spec, "resultArtifactSha256")) {
			plan.Violations["stale_reference"]++
		}
	}
	if values, ok := turn.Spec["inputArtifacts"].([]any); ok {
		for _, value := range values {
			reference, ok := value.(map[string]any)
			artifact := artifacts[text(reference, "artifactId")]
			if !ok || artifact.Version != uint64(number(reference, "version")) ||
				text(artifact.Spec, "sha256") != text(reference, "sha256") ||
				text(artifact.Spec, "mediaType") != text(reference, "mediaType") ||
				number(artifact.Spec, "sizeBytes") != number(reference, "sizeBytes") ||
				!validSHA(text(reference, "sha256")) {
				plan.Violations["stale_reference"]++
			}
		}
	}

	currentAttempt := number(turn.Spec, "attempt")
	currentAttempts := make([]model.TargetResource, 0, 1)
	revisions := targetByID(target, "RUNTIME_REVISION")
	for _, attempt := range target {
		if attempt.Kind != "TURN_ATTEMPT" || text(attempt.Spec, "turnId") != turn.ID {
			continue
		}
		if !sameBoundary(turn, attempt) || !knownAttemptState(attempt.State) {
			plan.Violations["tenant_mismatch"]++
			continue
		}
		attemptRevisionID := text(attempt.Spec, "runtimeRevisionId")
		attemptRevision, revisionExists := revisions[attemptRevisionID]
		isCurrent := number(attempt.Spec, "attempt") == currentAttempt
		if !revisionExists || !sameBoundary(turn, attemptRevision) ||
			uint64(number(attempt.Spec, "runtimeRevisionVersion")) != attemptRevision.Version ||
			!validSHA(text(attempt.Spec, "inputSha256")) ||
			(isCurrent && (attemptRevisionID != revision.ID ||
				text(attempt.Spec, "inputSha256") != text(turn.Spec, "effectiveInputSha256"))) {
			plan.Violations["broken_lineage"]++
			continue
		}
		match(plan, matched, "TURN_ATTEMPT", attempt)
		match(plan, matched, "RUNTIME_REVISION", attemptRevision)
		recordMapping(mapping, "TURN_ATTEMPT", number(turn.Spec, "agentSessionTurnId"), attempt)
		if isCurrent {
			currentAttempts = append(currentAttempts, attempt)
		}
	}
	if len(currentAttempts) != 1 {
		plan.Violations["broken_lineage"]++
		return
	}

	executions := make([]model.TargetResource, 0, 1)
	processes := targetByID(target, "PROCESS_RUN")
	for _, execution := range target {
		if execution.Kind != "RUNTIME_EXECUTION" || text(execution.Spec, "turnId") != turn.ID ||
			number(execution.Spec, "attempt") != currentAttempt {
			continue
		}
		process := processes[text(execution.Spec, "processRunId")]
		if !sameBoundary(turn, execution) || !sameBoundary(turn, process) ||
			text(execution.Spec, "processRunId") != text(turn.Spec, "processRunId") ||
			text(execution.Spec, "sessionId") != text(turn.Spec, "sessionId") ||
			text(execution.Spec, "runtimeRevisionId") != revision.ID ||
			uint64(number(execution.Spec, "runtimeRevisionVersion")) != revision.Version ||
			!validSHA(text(execution.Spec, "runtimeRevisionSha256")) ||
			text(execution.Spec, "immutableInputSha256") != text(currentAttempts[0].Spec, "inputSha256") ||
			!validSHA(text(execution.Spec, "immutableInputSha256")) ||
			!executionStateCompatible(sourceStatus, execution.State) {
			plan.Violations["broken_lineage"]++
			continue
		}
		executions = append(executions, execution)
	}
	active := !terminalTurn(sourceStatus)
	if len(executions) > 1 || active && len(executions) != 1 {
		plan.Violations["broken_lineage"]++
		return
	}
	for _, execution := range executions {
		match(plan, matched, "RUNTIME_EXECUTION", execution)
		recordMapping(mapping, "RUNTIME_EXECUTION", number(turn.Spec, "agentSessionTurnId"), execution)
	}
}

func revisionPinsArtifact(revision, artifact model.TargetResource) bool {
	components, ok := revision.Spec["components"].([]any)
	if !ok || artifact.ProjectionSHA256 == "" {
		return false
	}
	for _, value := range components {
		component, valid := value.(map[string]any)
		if valid && text(component, "kind") == "ARTIFACT" &&
			text(component, "resourceId") == artifact.ID &&
			uint64(number(component, "version")) == artifact.Version &&
			text(component, "projectionSha256") == artifact.ProjectionSHA256 {
			return true
		}
	}
	return false
}

func validateRevision(revision model.TargetResource, target []model.TargetResource, plan *model.Plan,
	matched map[string]model.TargetResource, mapping *[]string, sourceTurnID int64,
) {
	resources := make(map[string]model.TargetResource, len(target))
	for _, resource := range target {
		resources[resource.Kind+"\x00"+resource.ID+"\x00"+strconv.FormatUint(resource.Version, 10)] = resource
	}
	components, ok := revision.Spec["components"].([]any)
	if !ok || len(components) == 0 {
		plan.Violations["broken_lineage"]++
		return
	}
	seen := make(map[string]struct{}, len(components))
	for _, value := range components {
		component, componentOK := value.(map[string]any)
		seenKey := text(component, "kind") + "\x00" + text(component, "resourceId")
		key := seenKey + "\x00" +
			strconv.FormatInt(number(component, "version"), 10)
		resource, exists := resources[key]
		if !componentOK || !exists || !sameBoundary(revision, resource) ||
			resource.Version != uint64(number(component, "version")) ||
			!validSHA(text(component, "projectionSha256")) ||
			(resource.ProjectionSHA256 != "" && resource.ProjectionSHA256 != text(component, "projectionSha256")) {
			plan.Violations["broken_lineage"]++
			continue
		}
		if resource.Kind == "ARTIFACT" && !eligibleArtifact(resource) {
			plan.Violations["unsupported_state"]++
			continue
		}
		if resource.Kind == "ARTIFACT" && (resource.ProjectionSHA256 == "" ||
			resource.ProjectionSHA256 != text(component, "projectionSha256")) {
			plan.Violations["stale_reference"]++
			continue
		}
		if _, duplicate := seen[seenKey]; duplicate {
			plan.Violations["broken_lineage"]++
			continue
		}
		seen[seenKey] = struct{}{}
		match(plan, matched, resource.Kind, resource)
		recordMapping(mapping, "RUNTIME_REVISION_COMPONENT", sourceTurnID, resource)
	}
}

func validateBindingReceipts(source map[string][]sourceRow, sessions, turns map[int64]model.TargetResource,
	sourceTurns map[int64]sourceRow, archivedSessions map[int64]bool, target []model.TargetResource, plan *model.Plan,
	mapping *[]string,
) {
	revisions := targetByID(target, "RUNTIME_REVISION")
	for _, row := range source["matter_codex_runtime_agent_binding_outbox"] {
		state := text(row, "state")
		if state != "PENDING" && state != "LEASED" && state != "DELIVERED" {
			plan.Violations["unknown_state"]++
			continue
		}
		if state != "DELIVERED" {
			plan.Violations["unmaterialized_active"]++
			continue
		}
		sourceTurn, sourceExists := sourceTurns[number(row, "agent_session_turn_id")]
		archived := sourceExists && archivedSessions[number(sourceTurn, "session_id")] &&
			terminalTurn(text(sourceTurn, "status"))
		session, sessionOK := sessions[number(row, "agent_session_id")]
		turn, turnOK := turns[number(row, "agent_session_turn_id")]
		if archived && !sessionOK && !turnOK {
			plan.Counts.Archive["RUNTIME_AGENT_BINDING_RECEIPT"]++
			recordArchive(mapping, "RUNTIME_AGENT_BINDING_RECEIPT", number(row, "id"))
			continue
		}
		if !sessionOK || !turnOK {
			plan.Violations["orphan_reference"]++
			continue
		}
		if state != "DELIVERED" || text(session.Spec, "agentSessionKey") != text(row, "agent_session_key") ||
			session.ID != text(row, "control_session_id") || session.Version != uint64(number(row, "control_session_version")) ||
			uint64(number(session.Spec, "agentSessionBindingVersion")) != uint64(number(row, "agent_session_version")) ||
			text(session.Spec, "agentSessionBindingSha256") != text(row, "agent_session_binding_sha256") ||
			turn.ID != text(row, "control_turn_id") || turn.Version != uint64(number(row, "control_turn_version")) ||
			text(turn.Spec, "agentRunId") != text(row, "agent_run_id") ||
			uint64(number(turn.Spec, "agentTurnBindingVersion")) != uint64(number(row, "agent_session_turn_version")) ||
			text(turn.Spec, "agentTurnBindingSha256") != text(row, "agent_turn_binding_sha256") ||
			text(turn.Spec, "runtimeRevisionId") != text(row, "runtime_revision_id") ||
			number(turn.Spec, "attempt") != number(row, "attempt") ||
			text(turn.Spec, "effectiveInputSha256") != text(row, "input_sha256") ||
			revisions[text(row, "runtime_revision_id")].Version != uint64(number(row, "runtime_revision_version")) ||
			!validSHA(text(row, "runtime_revision_sha256")) || !bindingExecutionMatches(target, turn, row) {
			plan.Violations["stale_reference"]++
		} else {
			plan.Counts.Mapped["RUNTIME_AGENT_BINDING_RECEIPT"]++
			recordMapping(mapping, "RUNTIME_AGENT_BINDING_RECEIPT", number(row, "id"), turn)
		}
	}
	for _, row := range source["matter_codex_runtime_agent_binding_discoveries"] {
		state := text(row, "state")
		turnID := number(row, "agent_session_turn_id")
		_, mapped := turns[turnID]
		sourceTurn, sourceExists := sourceTurns[turnID]
		archived := sourceExists && archivedSessions[number(sourceTurn, "session_id")] &&
			terminalTurn(text(sourceTurn, "status"))
		if state != "PENDING" && state != "LEASED" && state != "DELIVERED" {
			plan.Violations["unknown_state"]++
		} else if state != "DELIVERED" {
			plan.Violations["unmaterialized_active"]++
		} else if !archived && !mapped {
			plan.Violations["unmaterialized_active"]++
		} else if archived {
			plan.Counts.Archive["RUNTIME_AGENT_BINDING_DISCOVERY"]++
			recordArchive(mapping, "RUNTIME_AGENT_BINDING_DISCOVERY", number(row, "id"))
		} else {
			plan.Counts.Mapped["RUNTIME_AGENT_BINDING_DISCOVERY"]++
			recordMapping(mapping, "RUNTIME_AGENT_BINDING_DISCOVERY", number(row, "id"), turns[turnID])
		}
	}
}

func bindingExecutionMatches(target []model.TargetResource, turn model.TargetResource, row sourceRow) bool {
	matches := 0
	for _, execution := range target {
		if execution.Kind == "RUNTIME_EXECUTION" && text(execution.Spec, "turnId") == turn.ID &&
			number(execution.Spec, "attempt") == number(row, "attempt") &&
			text(execution.Spec, "runtimeRevisionId") == text(row, "runtime_revision_id") &&
			uint64(number(execution.Spec, "runtimeRevisionVersion")) == uint64(number(row, "runtime_revision_version")) &&
			text(execution.Spec, "runtimeRevisionSha256") == text(row, "runtime_revision_sha256") &&
			text(execution.Spec, "immutableInputSha256") == text(row, "input_sha256") {
			matches++
		}
	}
	return matches == 1
}

func validateArchivedInventory(source map[string][]sourceRow, plan *model.Plan) {
	for table := range plan.Counts.Source {
		if _, known := knownSourceTables[table]; !known {
			plan.Violations["unsupported_state"]++
		}
	}
	validateArchiveIdentifiers(source, plan)
	validateArchivedLifecycle(source["matter_codex_thread_contexts"], "status",
		set("pending", "configured", "closed"), set("closed"), plan)
	validateArchivedLifecycle(source["matter_codex_work_claims"], "status",
		set("active", "queued", "running", "capacity_retry", "blocked", "succeeded", "failed", "canceled", "cancelled"),
		set("succeeded", "failed", "canceled", "cancelled"), plan)
	validateArchivedLifecycle(source["matter_codex_owner_attention_requests"], "status",
		set("open", "resolved"), set("resolved"), plan)
	validateArchivedLifecycle(source["matter_codex_agent_delegation_callback_deliveries"], "status",
		set("pending", "in_flight", "blocked", "delivered"), set("delivered"), plan)
	validateArchivedLifecycle(source["matter_codex_schedule_occurrences"], "status",
		set("queued", "running", "waiting_owner", "succeeded", "failed"), set("succeeded", "failed"), plan)
	validateArchivedLifecycle(source["matter_codex_scheduled_runs"], "status",
		set("queued", "running", "waiting_owner", "succeeded", "failed"), set("succeeded", "failed"), plan)
	validateArchivedLifecycle(source["matter_codex_interaction_capabilities"], "status",
		set("pending", "unused", "consumed", "revoked"), set("consumed", "revoked"), plan)
	validateKnownArchivedState(source["matter_codex_repositories"], "status", set("active"), plan)
	validateKnownArchivedState(source["matter_codex_credentials"], "status",
		set("unknown", "not_authorized", "auth_pending", "awaiting_user", "authorized", "auth_failed", "configured", "disabled", "error"), plan)
	validateKnownArchivedState(source["matter_codex_openai_accounts"], "status",
		set("not_authorized", "auth_pending", "awaiting_user", "authorized", "auth_failed", "disabled"), plan)
	validateKnownArchivedState(source["matter_codex_github_accounts"], "status",
		set("unknown", "configured", "disabled", "error"), plan)
	validateKnownArchivedState(source["matter_codex_agent_flows"], "status", set("created"), plan)
	validateKnownArchivedState(source["matter_codex_mattermost_bot_identities"], "status",
		set("pending", "configured", "error"), plan)
	validateKnownArchivedState(source["matter_codex_policy_revisions"], "status",
		set("draft", "active", "archived"), plan)
	validateKnownArchivedState(source["matter_codex_memory_records"], "status", set("active", "archived"), plan)
	validateDelegationLifecycle(source, plan)
	validateSourceBoundaries(source, plan)
	validateMemoryLineage(source, plan)
	validateClusterAdminArchive(source, plan)
}

type deliverySet struct {
	count        int
	destinations map[string]struct{}
}

func validateDelegationLifecycle(source map[string][]sourceRow, plan *model.Plan) {
	delegations := rowsByID(source["matter_codex_agent_delegations"])
	sessions := rowsByID(source["matter_codex_agent_sessions"])
	turns := rowsByID(source["matter_codex_agent_session_turns"])
	manifests := make(map[string]int)
	deliveries := make(map[string]deliverySet)
	for _, manifest := range source["matter_codex_agent_delegation_callback_delivery_manifests"] {
		delegationID, callbackRunID := number(manifest, "delegation_id"), text(manifest, "callback_run_id")
		key := delegationCallbackKey(delegationID, callbackRunID)
		if delegationID <= 0 || callbackRunID == "" || number(manifest, "expected_count") != 2 {
			plan.Violations["unsupported_state"]++
			continue
		}
		manifests[key]++
		delegation, exists := delegations[delegationID]
		if !exists {
			plan.Violations["orphan_reference"]++
		} else if text(delegation, "callback_run_id") != callbackRunID {
			plan.Violations["stale_reference"]++
		}
	}
	for _, delivery := range source["matter_codex_agent_delegation_callback_deliveries"] {
		delegationID, callbackRunID := number(delivery, "delegation_id"), text(delivery, "callback_run_id")
		key := delegationCallbackKey(delegationID, callbackRunID)
		group := deliveries[key]
		if group.destinations == nil {
			group.destinations = make(map[string]struct{}, 2)
		}
		group.count++
		destination := text(delivery, "destination")
		if destination != "source_callback" && destination != "child_return" {
			plan.Violations["unsupported_state"]++
		} else if _, duplicate := group.destinations[destination]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		group.destinations[destination] = struct{}{}
		deliveries[key] = group
		delegation, exists := delegations[delegationID]
		if !exists {
			plan.Violations["orphan_reference"]++
		} else if text(delegation, "callback_run_id") != callbackRunID {
			plan.Violations["stale_reference"]++
		}
	}
	known := set("creating", "thread_created", "queued", "callback_queued", "failed")
	for id, delegation := range delegations {
		status := text(delegation, "status")
		if !known[status] {
			plan.Violations["unknown_state"]++
			continue
		}
		callbackRunID := text(delegation, "callback_run_id")
		callbackTurnID := number(delegation, "callback_turn_id")
		key := delegationCallbackKey(id, callbackRunID)
		if callbackRunID == "" && callbackTurnID == 0 {
			if manifests[key] != 0 || deliveries[key].count != 0 {
				plan.Violations["broken_lineage"]++
			}
			if status != "failed" {
				plan.Violations["unmaterialized_active"]++
			}
			continue
		}
		if callbackRunID == "" || callbackTurnID <= 0 || status != "callback_queued" {
			plan.Violations["broken_lineage"]++
			continue
		}
		callbackTurn, turnExists := turns[callbackTurnID]
		sourceSession, sessionExists := sessions[number(delegation, "source_session_id")]
		group := deliveries[key]
		if !turnExists || !sessionExists || number(callbackTurn, "session_id") != number(sourceSession, "id") ||
			text(callbackTurn, "run_id") != callbackRunID || manifests[key] != 1 || group.count != 2 ||
			len(group.destinations) != 2 {
			plan.Violations["broken_lineage"]++
		} else if !terminalTurn(text(callbackTurn, "status")) {
			plan.Violations["unmaterialized_active"]++
		}
	}
}

func delegationCallbackKey(delegationID int64, callbackRunID string) string {
	return strconv.FormatInt(delegationID, 10) + "\x00" + callbackRunID
}

func validateArchiveIdentifiers(source map[string][]sourceRow, plan *model.Plan) {
	owned := set(
		"matter_codex_projects", "matter_codex_chats", "matter_codex_agent_roles",
		"matter_codex_agent_sessions", "matter_codex_agent_session_turns", "matter_codex_process_runs",
	)
	for table, rows := range source {
		if owned[table] {
			continue
		}
		seen := make(map[int64]struct{}, len(rows))
		for _, row := range rows {
			if _, exists := row["id"]; !exists {
				continue
			}
			id := number(row, "id")
			if id <= 0 {
				plan.Violations["unsupported_state"]++
				continue
			}
			if _, duplicate := seen[id]; duplicate {
				plan.Violations["duplicate_source"]++
			}
			seen[id] = struct{}{}
		}
	}
}

func validateArchivedLifecycle(rows []sourceRow, stateKey string, known, terminal map[string]bool, plan *model.Plan) {
	for _, row := range rows {
		state := text(row, stateKey)
		if !known[state] {
			plan.Violations["unknown_state"]++
		} else if !terminal[state] {
			plan.Violations["unmaterialized_active"]++
		}
	}
}

func validateKnownArchivedState(rows []sourceRow, stateKey string, known map[string]bool, plan *model.Plan) {
	for _, row := range rows {
		if !known[text(row, stateKey)] {
			plan.Violations["unknown_state"]++
		}
	}
}

func validateSourceBoundaries(source map[string][]sourceRow, plan *model.Plan) {
	projects := rowsByID(source["matter_codex_projects"])
	chats := rowsByID(source["matter_codex_chats"])
	roles := rowsByID(source["matter_codex_agent_roles"])
	sessions := rowsByID(source["matter_codex_agent_sessions"])
	turns := rowsByID(source["matter_codex_agent_session_turns"])
	processes := rowsByID(source["matter_codex_process_runs"])
	policies := rowsByID(source["matter_codex_policy_revisions"])
	repositories := rowsByID(source["matter_codex_repositories"])
	credentials := rowsByID(source["matter_codex_credentials"])
	openAIAccounts := rowsByText(source["matter_codex_openai_accounts"], "name", plan)
	githubAccounts := rowsByText(source["matter_codex_github_accounts"], "name", plan)
	variables := rowsByID(source["matter_codex_project_runtime_variables"])
	schedules := rowsByID(source["matter_codex_automation_schedules"])
	occurrences := rowsByID(source["matter_codex_schedule_occurrences"])
	botsByProjectUsername := make(map[string]sourceRow)
	for _, bot := range source["matter_codex_mattermost_bot_identities"] {
		key := strconv.FormatInt(number(bot, "project_id"), 10) + "\x00" + text(bot, "username")
		if number(bot, "project_id") <= 0 || text(bot, "username") == "" {
			plan.Violations["unsupported_state"]++
		} else if _, duplicate := botsByProjectUsername[key]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		botsByProjectUsername[key] = bot
	}

	for _, row := range source["matter_codex_mattermost_bot_identities"] {
		validateProjectRole(number(row, "project_id"), number(row, "role_id"), projects, roles, plan)
	}
	for _, row := range source["matter_codex_chat_participants"] {
		validateChatRole(number(row, "chat_id"), number(row, "role_id"), chats, roles, plan)
	}
	for _, row := range source["matter_codex_project_repositories"] {
		validateReference(projects, number(row, "project_id"), plan)
		validateReference(repositories, number(row, "repository_id"), plan)
	}
	for _, row := range source["matter_codex_chat_repositories"] {
		validateReference(chats, number(row, "chat_id"), plan)
		validateReference(repositories, number(row, "repository_id"), plan)
	}
	for _, row := range source["matter_codex_openai_accounts"] {
		credentialID := number(row, "credential_id")
		if credentialID != 0 {
			validateReference(credentials, credentialID, plan)
		} else if text(row, "status") == "authorized" {
			plan.Violations["orphan_reference"]++
		}
	}
	for _, row := range source["matter_codex_github_accounts"] {
		credentialID := number(row, "credential_id")
		if credentialID != 0 {
			validateReference(credentials, credentialID, plan)
		} else if text(row, "status") == "configured" {
			plan.Violations["orphan_reference"]++
		}
	}
	for _, row := range source["matter_codex_projects"] {
		validateNamedReference(githubAccounts, text(row, "github_account_name"), plan)
	}
	for _, row := range source["matter_codex_repositories"] {
		validateNamedReference(githubAccounts, text(row, "github_account_name"), plan)
	}
	for _, row := range source["matter_codex_agent_profiles"] {
		validateNamedReference(openAIAccounts, text(row, "openai_account_name"), plan)
		validateNamedReference(githubAccounts, text(row, "github_account_name"), plan)
	}
	for _, row := range source["matter_codex_agent_roles"] {
		validateNamedReference(openAIAccounts, text(row, "openai_account_name"), plan)
		validateNamedReference(githubAccounts, text(row, "github_account_name"), plan)
		if botName := text(row, "bot_identity"); botName != "" {
			bot := botsByProjectUsername[strconv.FormatInt(number(row, "project_id"), 10)+"\x00"+botName]
			if bot == nil {
				plan.Violations["orphan_reference"]++
			} else if number(bot, "role_id") != number(row, "id") {
				plan.Violations["tenant_mismatch"]++
			}
		}
	}
	for _, row := range source["matter_codex_agent_sessions"] {
		validateNamedReference(openAIAccounts, text(row, "openai_account_name"), plan)
	}
	for _, row := range source["matter_codex_project_runtime_variables"] {
		validateReference(projects, number(row, "project_id"), plan)
	}
	for _, row := range source["matter_codex_agent_role_runtime_variables"] {
		role, roleOK := roles[number(row, "role_id")]
		variable, variableOK := variables[number(row, "variable_id")]
		if !roleOK || !variableOK {
			plan.Violations["orphan_reference"]++
		} else if number(role, "project_id") != number(variable, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_policy_revisions"] {
		validateReference(projects, number(row, "project_id"), plan)
	}
	for _, row := range source["matter_codex_role_capabilities"] {
		validatePolicyRole(number(row, "policy_revision_id"), number(row, "role_id"), policies, roles, plan)
	}
	for _, row := range source["matter_codex_role_relationship_policies"] {
		policy, policyOK := policies[number(row, "policy_revision_id")]
		sourceRole, sourceOK := roles[number(row, "source_role_id")]
		targetRole, targetOK := roles[number(row, "target_role_id")]
		if !policyOK || !sourceOK || !targetOK {
			plan.Violations["orphan_reference"]++
		} else if number(policy, "project_id") != number(sourceRole, "project_id") ||
			number(policy, "project_id") != number(targetRole, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_process_runs"] {
		validateProjectRole(number(row, "project_id"), number(row, "root_role_id"), projects, roles, plan)
		policy, ok := policies[number(row, "policy_revision_id")]
		if !ok {
			plan.Violations["orphan_reference"]++
		} else if number(policy, "project_id") != number(row, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_work_claims"] {
		validateProcessTurnRole(row, processes, turns, sessions, roles, plan)
	}
	for _, row := range source["matter_codex_owner_attention_requests"] {
		validateProcessTurnRole(row, processes, turns, sessions, roles, plan)
	}
	for _, row := range source["matter_codex_agent_delegations"] {
		validateDelegationBoundary(row, projects, chats, roles, sessions, turns, plan)
	}
	for _, row := range source["matter_codex_thread_contexts"] {
		chat, chatOK := chats[number(row, "chat_id")]
		if _, projectOK := projects[number(row, "project_id")]; !projectOK || !chatOK {
			plan.Violations["orphan_reference"]++
		} else if number(chat, "project_id") != number(row, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_memory_records"] {
		validateMemoryBoundary(row, projects, roles, sessions, turns, plan)
	}
	for _, row := range source["matter_codex_automation_schedules"] {
		projectID := number(row, "project_id")
		validateProjectRole(projectID, number(row, "target_agent_role_id"), projects, roles, plan)
		chat, ok := chats[number(row, "target_chat_id")]
		if !ok {
			plan.Violations["orphan_reference"]++
		} else if number(chat, "project_id") != projectID {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_schedule_occurrences"] {
		schedule, scheduleOK := schedules[number(row, "schedule_id")]
		if !scheduleOK || projects[number(row, "project_id")] == nil {
			plan.Violations["orphan_reference"]++
		} else if number(schedule, "project_id") != number(row, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_scheduled_runs"] {
		validateScheduledRunBoundary(row, projects, roles, chats, schedules, occurrences, sessions, turns, plan)
	}
}

func validateMemoryLineage(source map[string][]sourceRow, plan *model.Plan) {
	records := rowsByID(source["matter_codex_memory_records"])
	versions := rowsByID(source["matter_codex_memory_record_versions"])
	versionsByRecord := make(map[int64]int, len(records))
	seenVersion := make(map[string]struct{}, len(versions))
	seenHash := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		recordID := number(version, "record_id")
		sequence := number(version, "version")
		contentHash := text(version, "content_hash")
		if records[recordID] == nil {
			plan.Violations["orphan_reference"]++
		}
		if sequence <= 0 || !validSHA(contentHash) {
			plan.Violations["broken_lineage"]++
		}
		versionKey := strconv.FormatInt(recordID, 10) + "\x00" + strconv.FormatInt(sequence, 10)
		hashKey := strconv.FormatInt(recordID, 10) + "\x00" + contentHash
		if _, duplicate := seenVersion[versionKey]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		if _, duplicate := seenHash[hashKey]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		seenVersion[versionKey] = struct{}{}
		seenHash[hashKey] = struct{}{}
		versionsByRecord[recordID]++
		if supersedesID := number(version, "supersedes_version_id"); supersedesID != 0 {
			superseded, exists := versions[supersedesID]
			if !exists {
				plan.Violations["orphan_reference"]++
			} else if number(superseded, "record_id") != recordID || number(superseded, "version") >= sequence {
				plan.Violations["broken_lineage"]++
			}
		}
	}
	for id, record := range records {
		scope := text(record, "scope")
		roleID := number(record, "role_id")
		if scope != "project" && scope != "role" {
			plan.Violations["unknown_state"]++
		} else if scope == "project" && roleID != 0 || scope == "role" && roleID == 0 {
			plan.Violations["broken_lineage"]++
		}
		if versionsByRecord[id] == 0 {
			plan.Violations["broken_lineage"]++
		}
	}
	for _, embedding := range source["matter_codex_memory_embeddings"] {
		if versions[number(embedding, "version_id")] == nil {
			plan.Violations["orphan_reference"]++
		}
		if text(embedding, "model_revision") == "" || number(embedding, "dimensions") <= 0 {
			plan.Violations["broken_lineage"]++
		}
	}
}

func validateClusterAdminArchive(source map[string][]sourceRow, plan *model.Plan) {
	projects := rowsByID(source["matter_codex_projects"])
	chats := rowsByID(source["matter_codex_chats"])
	roles := rowsByID(source["matter_codex_agent_roles"])
	variables := rowsByID(source["matter_codex_project_runtime_variables"])
	repositories := rowsByID(source["matter_codex_repositories"])
	projectRepositories := rowsByID(source["matter_codex_project_repositories"])
	chatRepositories := rowsByID(source["matter_codex_chat_repositories"])
	schedules := rowsByID(source["matter_codex_automation_schedules"])
	scheduledRuns := rowsByID(source["matter_codex_scheduled_runs"])
	profiles := rowsByText(source["matter_codex_agent_profiles"], "name", plan)
	openAIAccounts := rowsByText(source["matter_codex_openai_accounts"], "name", plan)
	githubAccounts := rowsByText(source["matter_codex_github_accounts"], "name", plan)
	sessionsByKey := rowsByText(source["matter_codex_agent_sessions"], "session_key", plan)
	promptTemplates := make(map[string]sourceRow)
	for _, row := range source["matter_codex_agent_prompt_templates"] {
		key := text(row, "profile_name") + "\x00" + text(row, "template_key")
		if key == "\x00" {
			plan.Violations["unsupported_state"]++
		} else if _, duplicate := promptTemplates[key]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		promptTemplates[key] = row
	}
	revocations := make(map[string]struct{})
	knownRevocationTypes := set("agent_profile", "agent_role", "bot_binding", "runtime_variable_binding", "dependency",
		"profile_dependency", "chat_binding", "session_binding")
	for _, row := range source["matter_codex_cluster_admin_revocations"] {
		resourceType, resourceKey := text(row, "resource_type"), text(row, "resource_key")
		if !knownRevocationTypes[resourceType] {
			plan.Violations["unknown_state"]++
		}
		if resourceKey == "" {
			plan.Violations["unsupported_state"]++
		}
		key := resourceType + "\x00" + resourceKey
		if _, duplicate := revocations[key]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		revocations[key] = struct{}{}
	}
	isRevoked := func(resourceType, resourceKey string) bool {
		_, exists := revocations[resourceType+"\x00"+resourceKey]
		return exists
	}
	for _, row := range source["matter_codex_agent_prompt_templates"] {
		profileName := text(row, "profile_name")
		if profiles[profileName] == nil && !isRevoked("agent_profile", profileName) {
			plan.Violations["orphan_reference"]++
		}
	}
	for _, row := range source["matter_codex_cluster_admin_subjects"] {
		subjectType, subjectKey := text(row, "subject_type"), text(row, "subject_key")
		switch subjectType {
		case "agent_profile":
			if number(row, "project_id") != 0 || subjectKey != text(row, "profile_name") {
				plan.Violations["broken_lineage"]++
			} else if profiles[subjectKey] == nil && !isRevoked("agent_profile", subjectKey) {
				plan.Violations["orphan_reference"]++
			}
		case "agent_role":
			roleID, err := strconv.ParseInt(subjectKey, 10, 64)
			role := roles[roleID]
			if err != nil || roleID <= 0 {
				plan.Violations["unsupported_state"]++
			} else if role == nil && !isRevoked("agent_role", subjectKey) {
				plan.Violations["orphan_reference"]++
			} else if role != nil && number(role, "project_id") != number(row, "project_id") {
				plan.Violations["tenant_mismatch"]++
			}
		default:
			plan.Violations["unknown_state"]++
		}
	}
	for _, row := range source["matter_codex_cluster_admin_bindings"] {
		validateFrozenRoleProject(number(row, "role_id"), number(row, "project_id"), roles, projects,
			isRevoked("chat_binding", compoundID(row, "role_id", "chat_id")), plan)
		chat, exists := chats[number(row, "chat_id")]
		if !exists && !isRevoked("chat_binding", compoundID(row, "role_id", "chat_id")) {
			plan.Violations["orphan_reference"]++
		} else if exists && number(chat, "project_id") != number(row, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_cluster_admin_session_bindings"] {
		revoked := isRevoked("session_binding", strconv.FormatInt(number(row, "role_id"), 10)+":"+text(row, "session_key"))
		validateFrozenRoleProject(number(row, "role_id"), number(row, "project_id"), roles, projects, revoked, plan)
		session := sessionsByKey[text(row, "session_key")]
		chat := chats[number(row, "chat_id")]
		if (session == nil || chat == nil) && !revoked {
			plan.Violations["orphan_reference"]++
		} else if session != nil && (number(session, "role_id") != number(row, "role_id") ||
			number(session, "project_id") != number(row, "project_id") || number(session, "chat_id") != number(row, "chat_id")) ||
			chat != nil && number(chat, "project_id") != number(row, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_cluster_admin_bot_bindings"] {
		roleID := number(row, "role_id")
		validateFrozenRoleProject(roleID, number(row, "project_id"), roles, projects,
			isRevoked("bot_binding", strconv.FormatInt(roleID, 10)), plan)
	}
	for _, row := range source["matter_codex_cluster_admin_runtime_variable_bindings"] {
		roleID, variableID := number(row, "role_id"), number(row, "variable_id")
		revoked := isRevoked("runtime_variable_binding", strconv.FormatInt(roleID, 10)+":"+strconv.FormatInt(variableID, 10))
		role, roleOK := roles[roleID]
		variable, variableOK := variables[variableID]
		if (!roleOK || !variableOK) && !revoked {
			plan.Violations["orphan_reference"]++
		} else if roleOK && variableOK && number(role, "project_id") != number(variable, "project_id") {
			plan.Violations["tenant_mismatch"]++
		}
	}
	for _, row := range source["matter_codex_cluster_admin_prompt_templates"] {
		key := text(row, "profile_name") + "\x00" + text(row, "template_key")
		if promptTemplates[key] == nil && !isRevoked("agent_profile", text(row, "profile_name")) {
			plan.Violations["orphan_reference"]++
		}
	}
	for _, row := range source["matter_codex_cluster_admin_dependencies"] {
		validateClusterAdminDependency(row, source, roles, chats, repositories, projectRepositories,
			chatRepositories, openAIAccounts, githubAccounts, isRevoked, plan)
	}
	for _, row := range source["matter_codex_cluster_admin_delivery_fences"] {
		sessionKey := text(row, "session_key")
		if sessionKey == "" {
			plan.Violations["unsupported_state"]++
		} else if sessionsByKey[sessionKey] == nil && !hasSessionRevocation(revocations, sessionKey) {
			plan.Violations["orphan_reference"]++
		}
	}
	for _, row := range source["matter_codex_automation_audit_events"] {
		projectID := number(row, "project_id")
		if projects[projectID] == nil {
			plan.Violations["orphan_reference"]++
		}
		if scheduleID := number(row, "schedule_id"); scheduleID != 0 {
			schedule := schedules[scheduleID]
			if schedule == nil {
				plan.Violations["orphan_reference"]++
			} else if number(schedule, "project_id") != projectID {
				plan.Violations["tenant_mismatch"]++
			}
		}
		if runID := number(row, "scheduled_run_id"); runID != 0 {
			run := scheduledRuns[runID]
			if run == nil {
				plan.Violations["orphan_reference"]++
			} else if number(run, "project_id") != projectID {
				plan.Violations["tenant_mismatch"]++
			}
		}
	}
}

func rowsByText(rows []sourceRow, key string, plan *model.Plan) map[string]sourceRow {
	result := make(map[string]sourceRow, len(rows))
	for _, row := range rows {
		identifier := text(row, key)
		if identifier == "" {
			plan.Violations["unsupported_state"]++
			continue
		}
		if _, duplicate := result[identifier]; duplicate {
			plan.Violations["duplicate_source"]++
		}
		result[identifier] = row
	}
	return result
}

func validateFrozenRoleProject(roleID, projectID int64, roles, projects map[int64]sourceRow, revoked bool, plan *model.Plan) {
	role, roleExists := roles[roleID]
	_, projectExists := projects[projectID]
	if !roleExists || !projectExists {
		if !revoked {
			plan.Violations["orphan_reference"]++
		}
	} else if number(role, "project_id") != projectID {
		plan.Violations["tenant_mismatch"]++
	}
}

func validateClusterAdminDependency(row sourceRow, source map[string][]sourceRow, roles, chats, repositories,
	projectRepositories, chatRepositories map[int64]sourceRow, openAIAccounts, githubAccounts map[string]sourceRow,
	isRevoked func(string, string) bool, plan *model.Plan,
) {
	roleID := number(row, "role_id")
	resourceType, resourceKey := text(row, "resource_type"), text(row, "resource_key")
	revocationKey := strconv.FormatInt(roleID, 10) + ":" + resourceType + ":" + resourceKey
	revoked := isRevoked("dependency", revocationKey)
	role, roleExists := roles[roleID]
	if !roleExists && !revoked && !isRevoked("agent_role", strconv.FormatInt(roleID, 10)) {
		plan.Violations["orphan_reference"]++
	}
	switch resourceType {
	case "openai_account":
		if openAIAccounts[resourceKey] == nil && !revoked {
			plan.Violations["orphan_reference"]++
		}
	case "github_account":
		if githubAccounts[resourceKey] == nil && !revoked {
			plan.Violations["orphan_reference"]++
		}
	case "repository", "project_repository", "chat_repository":
		identifier, err := strconv.ParseInt(resourceKey, 10, 64)
		if err != nil || identifier <= 0 {
			plan.Violations["unsupported_state"]++
			return
		}
		referenceState := validateClusterAdminRepositoryReference(resourceType, identifier, roleID, role, roleExists,
			source, chats, repositories, projectRepositories, chatRepositories)
		if referenceState == 0 && !revoked {
			plan.Violations["orphan_reference"]++
		} else if referenceState < 0 {
			plan.Violations["tenant_mismatch"]++
		}
	default:
		plan.Violations["unknown_state"]++
	}
}

func validateClusterAdminRepositoryReference(resourceType string, identifier, roleID int64, role sourceRow, roleExists bool,
	source map[string][]sourceRow, chats, repositories, projectRepositories, chatRepositories map[int64]sourceRow,
) int {
	if !roleExists {
		return 0
	}
	projectID := number(role, "project_id")
	switch resourceType {
	case "project_repository":
		binding := projectRepositories[identifier]
		if binding == nil {
			return 0
		}
		if number(binding, "project_id") != projectID {
			return -1
		}
		return 1
	case "chat_repository":
		binding := chatRepositories[identifier]
		chat := chats[number(binding, "chat_id")]
		if binding == nil || chat == nil {
			return 0
		}
		if number(chat, "project_id") != projectID ||
			!hasEnabledParticipant(source["matter_codex_chat_participants"], roleID, number(binding, "chat_id")) {
			return -1
		}
		return 1
	case "repository":
		if repositories[identifier] == nil {
			return 0
		}
		for _, binding := range projectRepositories {
			if number(binding, "repository_id") == identifier && number(binding, "project_id") == projectID {
				return 1
			}
		}
		for _, binding := range chatRepositories {
			chatID := number(binding, "chat_id")
			chat := chats[chatID]
			if number(binding, "repository_id") == identifier && chat != nil && number(chat, "project_id") == projectID &&
				hasEnabledParticipant(source["matter_codex_chat_participants"], roleID, chatID) {
				return 1
			}
		}
		return -1
	}
	return 0
}

func hasEnabledParticipant(rows []sourceRow, roleID, chatID int64) bool {
	for _, row := range rows {
		if number(row, "role_id") == roleID && number(row, "chat_id") == chatID && boolean(row, "enabled") {
			return true
		}
	}
	return false
}

func compoundID(row sourceRow, left, right string) string {
	return strconv.FormatInt(number(row, left), 10) + ":" + strconv.FormatInt(number(row, right), 10)
}

func hasSessionRevocation(revocations map[string]struct{}, sessionKey string) bool {
	prefix := "session_binding\x00"
	for key := range revocations {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ":"+sessionKey) {
			return true
		}
	}
	return false
}

func rowsByID(rows []sourceRow) map[int64]sourceRow {
	result := make(map[int64]sourceRow, len(rows))
	for _, row := range rows {
		if id := number(row, "id"); id > 0 {
			result[id] = row
		}
	}
	return result
}

func validateReference(index map[int64]sourceRow, id int64, plan *model.Plan) {
	if id <= 0 || index[id] == nil {
		plan.Violations["orphan_reference"]++
	}
}

func validateNamedReference(index map[string]sourceRow, key string, plan *model.Plan) {
	if key != "" && index[key] == nil {
		plan.Violations["orphan_reference"]++
	}
}

func validateProjectRole(projectID, roleID int64, projects, roles map[int64]sourceRow, plan *model.Plan) {
	role, roleOK := roles[roleID]
	if projects[projectID] == nil || !roleOK {
		plan.Violations["orphan_reference"]++
	} else if number(role, "project_id") != projectID {
		plan.Violations["tenant_mismatch"]++
	}
}

func validateChatRole(chatID, roleID int64, chats, roles map[int64]sourceRow, plan *model.Plan) {
	chat, chatOK := chats[chatID]
	role, roleOK := roles[roleID]
	if !chatOK || !roleOK {
		plan.Violations["orphan_reference"]++
	} else if number(chat, "project_id") != number(role, "project_id") {
		plan.Violations["tenant_mismatch"]++
	}
}

func validatePolicyRole(policyID, roleID int64, policies, roles map[int64]sourceRow, plan *model.Plan) {
	policy, policyOK := policies[policyID]
	role, roleOK := roles[roleID]
	if !policyOK || !roleOK {
		plan.Violations["orphan_reference"]++
	} else if number(policy, "project_id") != number(role, "project_id") {
		plan.Violations["tenant_mismatch"]++
	}
}

func validateProcessTurnRole(row sourceRow, processes, turns, sessions, roles map[int64]sourceRow, plan *model.Plan) {
	process, processOK := processes[number(row, "process_run_id")]
	turn, turnOK := turns[number(row, "turn_id")]
	roleID := number(row, "role_id")
	role, roleOK := roles[roleID]
	if roleID == 0 {
		roleOK = true
	}
	if !processOK || !turnOK || !roleOK {
		plan.Violations["orphan_reference"]++
		return
	}
	session, sessionOK := sessions[number(turn, "session_id")]
	if !sessionOK {
		plan.Violations["orphan_reference"]++
	} else if number(session, "project_id") != number(process, "project_id") ||
		(roleID != 0 && number(role, "project_id") != number(process, "project_id")) {
		plan.Violations["tenant_mismatch"]++
	}
}

func validateDelegationBoundary(row sourceRow, projects, chats, roles, sessions, turns map[int64]sourceRow, plan *model.Plan) {
	projectID := number(row, "project_id")
	sourceSession, sourceSessionOK := sessions[number(row, "source_session_id")]
	sourceTurn, sourceTurnOK := turns[number(row, "source_turn_id")]
	targetChat, targetChatOK := chats[number(row, "target_chat_id")]
	targetRole, targetRoleOK := roles[number(row, "target_role_id")]
	if projects[projectID] == nil || !sourceSessionOK || !sourceTurnOK || !targetChatOK || !targetRoleOK {
		plan.Violations["orphan_reference"]++
		return
	}
	if number(sourceTurn, "session_id") != number(row, "source_session_id") ||
		number(sourceSession, "project_id") != projectID || number(targetChat, "project_id") != projectID ||
		number(targetRole, "project_id") != projectID {
		plan.Violations["tenant_mismatch"]++
	}
	sessionID, turnID := number(row, "target_session_id"), number(row, "target_turn_id")
	if sessionID == 0 && turnID == 0 {
		if text(row, "target_run_id") != "" {
			plan.Violations["stale_reference"]++
		}
		return
	}
	session, sessionOK := sessions[sessionID]
	turn, turnOK := turns[turnID]
	if !sessionOK || !turnOK {
		plan.Violations["orphan_reference"]++
	} else if number(session, "project_id") != projectID || number(session, "chat_id") != number(row, "target_chat_id") ||
		number(session, "role_id") != number(row, "target_role_id") || number(turn, "session_id") != sessionID {
		plan.Violations["tenant_mismatch"]++
	} else if text(row, "target_run_id") == "" || text(row, "target_run_id") != text(turn, "run_id") {
		plan.Violations["stale_reference"]++
	}
}

func validateMemoryBoundary(row sourceRow, projects, roles, sessions, turns map[int64]sourceRow, plan *model.Plan) {
	projectID := number(row, "project_id")
	if projects[projectID] == nil {
		plan.Violations["orphan_reference"]++
		return
	}
	for _, roleKey := range []string{"role_id", "created_by_role_id"} {
		roleID := number(row, roleKey)
		if roleID == 0 && roleKey == "role_id" {
			continue
		}
		role, ok := roles[roleID]
		if !ok {
			plan.Violations["orphan_reference"]++
		} else if number(role, "project_id") != projectID {
			plan.Violations["tenant_mismatch"]++
		}
	}
	if turnID := number(row, "source_turn_id"); turnID != 0 {
		turn, turnOK := turns[turnID]
		session, sessionOK := sessions[number(turn, "session_id")]
		if !turnOK || !sessionOK {
			plan.Violations["orphan_reference"]++
		} else if number(session, "project_id") != projectID {
			plan.Violations["tenant_mismatch"]++
		}
	}
}

func validateScheduledRunBoundary(row sourceRow, projects, roles, chats, schedules, occurrences, sessions, turns map[int64]sourceRow,
	plan *model.Plan,
) {
	projectID := number(row, "project_id")
	schedule, scheduleOK := schedules[number(row, "schedule_id")]
	occurrence, occurrenceOK := occurrences[number(row, "occurrence_id")]
	role, roleOK := roles[number(row, "target_agent_role_id")]
	chat, chatOK := chats[number(row, "target_chat_id")]
	if projects[projectID] == nil || !scheduleOK || !occurrenceOK || !roleOK || !chatOK {
		plan.Violations["orphan_reference"]++
		return
	}
	if number(schedule, "project_id") != projectID || number(occurrence, "project_id") != projectID ||
		number(occurrence, "schedule_id") != number(row, "schedule_id") || number(role, "project_id") != projectID ||
		number(chat, "project_id") != projectID {
		plan.Violations["tenant_mismatch"]++
	}
	sessionID, turnID := number(row, "runtime_session_id"), number(row, "runtime_turn_id")
	if sessionID == 0 && turnID == 0 {
		return
	}
	session, sessionOK := sessions[sessionID]
	turn, turnOK := turns[turnID]
	if !sessionOK || !turnOK {
		plan.Violations["orphan_reference"]++
	} else if number(session, "project_id") != projectID || number(turn, "session_id") != sessionID {
		plan.Violations["tenant_mismatch"]++
	} else if text(row, "runtime_run_id") == "" || text(row, "runtime_run_id") != text(turn, "run_id") {
		plan.Violations["stale_reference"]++
	}
}

func set(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func archiveRemaining(_ map[string][]sourceRow, plan *model.Plan, mapping *[]string) {
	owned := map[string]struct{}{
		"matter_codex_projects": {}, "matter_codex_chats": {}, "matter_codex_agent_roles": {},
		"matter_codex_agent_sessions": {}, "matter_codex_agent_session_turns": {},
		"matter_codex_agent_runs":   {},
		"matter_codex_process_runs": {}, "matter_codex_process_turns": {},
		"matter_codex_automation_schedules":              {},
		"matter_codex_runtime_agent_binding_outbox":      {},
		"matter_codex_runtime_agent_binding_discoveries": {},
	}
	for table, count := range plan.Counts.Source {
		if _, consumed := owned[table]; consumed || count == 0 {
			continue
		}
		plan.Counts.Archive[table] += count
		*mapping = append(*mapping, "ARCHIVE_TABLE\x00"+table+"\x00"+strconv.FormatUint(count, 10))
	}
}

func indexTarget(resources []model.TargetResource, kind, key string) map[string][]model.TargetResource {
	result := make(map[string][]model.TargetResource)
	for _, resource := range resources {
		if resource.Kind == kind && !resource.Historical {
			result[text(resource.Spec, key)] = append(result[text(resource.Spec, key)], resource)
		}
	}
	return result
}

func indexTargetNumber(resources []model.TargetResource, kind, key string) map[int64][]model.TargetResource {
	result := make(map[int64][]model.TargetResource)
	for _, resource := range resources {
		if resource.Kind == kind && !resource.Historical {
			result[number(resource.Spec, key)] = append(result[number(resource.Spec, key)], resource)
		}
	}
	return result
}

func targetByID(resources []model.TargetResource, kind string) map[string]model.TargetResource {
	result := make(map[string]model.TargetResource)
	for _, resource := range resources {
		if resource.Kind == kind && !resource.Historical {
			result[resource.ID] = resource
		}
	}
	return result
}

func byNumber(rows []sourceRow, key string, plan *model.Plan) map[int64]sourceRow {
	result := make(map[int64]sourceRow, len(rows))
	for _, row := range rows {
		id := number(row, key)
		if id <= 0 {
			plan.Violations["unsupported_state"]++
			continue
		}
		if _, exists := result[id]; exists {
			plan.Violations["duplicate_source"]++
		}
		result[id] = row
	}
	return result
}

func uniqueTarget(candidates []model.TargetResource, plan *model.Plan) (model.TargetResource, bool) {
	if len(candidates) != 1 {
		plan.Violations["ambiguous_target"]++
		return model.TargetResource{}, false
	}
	return candidates[0], true
}

func uniqueScoped(candidates []model.TargetResource, project model.TargetResource, plan *model.Plan) (model.TargetResource, bool) {
	filtered, hiddenOwnerMismatch := scopedTargets(candidates, project)
	if hiddenOwnerMismatch || len(filtered) != 1 {
		if hiddenOwnerMismatch {
			plan.Violations["tenant_mismatch"]++
		} else {
			plan.Violations["ambiguous_target"]++
		}
		return model.TargetResource{}, false
	}
	return filtered[0], true
}

func uniqueGloballyScoped(candidates []model.TargetResource, project model.TargetResource, plan *model.Plan) (model.TargetResource, bool) {
	filtered, hiddenBoundaryMismatch := globallyScopedTargets(candidates, project)
	if hiddenBoundaryMismatch || len(filtered) != 1 {
		if hiddenBoundaryMismatch {
			plan.Violations["tenant_mismatch"]++
		} else {
			plan.Violations["ambiguous_target"]++
		}
		return model.TargetResource{}, false
	}
	return filtered[0], true
}

func scopedTargets(candidates []model.TargetResource, project model.TargetResource) ([]model.TargetResource, bool) {
	filtered := make([]model.TargetResource, 0, 1)
	hiddenOwnerMismatch := false
	for _, candidate := range candidates {
		if candidate.OrganizationID == project.OrganizationID && candidate.ProjectID == project.ProjectID {
			if candidate.OwnerActorID == project.OwnerActorID {
				filtered = append(filtered, candidate)
			} else {
				hiddenOwnerMismatch = true
			}
		}
	}
	return filtered, hiddenOwnerMismatch
}

func globallyScopedTargets(candidates []model.TargetResource, project model.TargetResource) ([]model.TargetResource, bool) {
	filtered := make([]model.TargetResource, 0, 1)
	hiddenBoundaryMismatch := false
	for _, candidate := range candidates {
		if sameBoundary(candidate, project) {
			filtered = append(filtered, candidate)
		} else {
			hiddenBoundaryMismatch = true
		}
	}
	return filtered, hiddenBoundaryMismatch
}

func sessionProject(session model.TargetResource) model.TargetResource {
	return model.TargetResource{OrganizationID: session.OrganizationID, ProjectID: session.ProjectID,
		OwnerActorID: session.OwnerActorID}
}

func match(plan *model.Plan, matched map[string]model.TargetResource, kind string, resource model.TargetResource) {
	key := resource.OrganizationID + "/" + resource.ProjectID + "/" + resource.OwnerActorID + "/" +
		resource.Kind + "/" + resource.ID + "/" + strconv.FormatUint(resource.Version, 10)
	if _, exists := matched[key]; !exists {
		matched[key] = resource
		plan.Counts.Mapped[kind]++
	}
}

func sameBoundary(left, right model.TargetResource) bool {
	return left.OrganizationID != "" && left.OrganizationID == right.OrganizationID &&
		left.ProjectID != "" && left.ProjectID == right.ProjectID &&
		left.OwnerActorID != "" && left.OwnerActorID == right.OwnerActorID
}

func recordMapping(mapping *[]string, sourceKind string, sourceID int64, target model.TargetResource) {
	*mapping = append(*mapping, sourceKind+"\x00"+strconv.FormatInt(sourceID, 10)+"\x00"+
		target.OrganizationID+"\x00"+target.ProjectID+"\x00"+target.OwnerActorID+"\x00"+
		target.Kind+"\x00"+target.ID+"\x00"+strconv.FormatUint(target.Version, 10))
}

func recordArchive(mapping *[]string, sourceKind string, sourceID int64) {
	*mapping = append(*mapping, sourceKind+"\x00"+strconv.FormatInt(sourceID, 10)+"\x00ARCHIVE")
}

func digestMapping(mapping []string) string {
	sort.Strings(mapping)
	hash := sha256.New()
	for _, entry := range mapping {
		writeFramed(hash, []byte(entry))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func detectMappingDuplicates(mapping []string, plan *model.Plan) {
	oneToOne := map[string]bool{
		"PROJECT": true, "TEAM": true, "CHAT": true, "AGENT": true,
		"SESSION": true, "TURN": true, "PROCESS_RUN": true,
	}
	claimed := make(map[string]string)
	for _, entry := range mapping {
		parts := strings.Split(entry, "\x00")
		if len(parts) != 8 || !oneToOne[parts[0]] {
			continue
		}
		targetKey := strings.Join(parts[2:], "\x00")
		if previous, exists := claimed[targetKey]; exists && previous != parts[0]+"\x00"+parts[1] {
			plan.Violations["duplicate_source"]++
		} else {
			claimed[targetKey] = parts[0] + "\x00" + parts[1]
		}
	}
}

func digestTargets(resources map[string]model.TargetResource) string {
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		writeFramed(hash, []byte(key))
		resource := resources[key]
		canonical, err := json.Marshal(struct {
			ID, OrganizationID, ProjectID, ParentID, OwnerActorID, Kind, Name, State string
			Version                                                                  uint64
			Spec                                                                     map[string]any
		}{resource.ID, resource.OrganizationID, resource.ProjectID, resource.ParentID, resource.OwnerActorID,
			resource.Kind, resource.Name, resource.State, resource.Version, resource.Spec})
		if err != nil {
			return ""
		}
		writeFramed(hash, canonical)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func digestPlan(plan model.Plan) (string, error) {
	plan.PlanSHA256, plan.BackupSHA256, plan.ManifestSHA256, plan.CutoverState = "", "", "", ""
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", errors.New("encode migration plan")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func writeFramed(hash interface{ Write([]byte) (int, error) }, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write(value)
}

func text(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func number(row map[string]any, key string) int64 {
	value, ok := row[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(fmt.Sprint(value), 10, 64)
		return parsed
	}
}

func boolean(row map[string]any, key string) bool {
	value, ok := row[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(typed)
		return parsed
	default:
		return false
	}
}

func validSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func terminalTurn(status string) bool {
	return status == "succeeded" || status == "failed" || status == "canceled" || status == "cancelled"
}

func knownSession(status string) bool {
	return status == "idle" || status == "running" || status == "error" || status == "blocked" ||
		status == "closed" || status == "expired"
}

func terminalSession(status string) bool { return status == "closed" || status == "expired" }

func sessionStateCompatible(source, target string) bool {
	switch source {
	case "idle", "running", "error", "blocked":
		return target == "ACTIVE"
	case "closed":
		return target == "ARCHIVED" || target == "SUCCEEDED" || target == "FAILED" || target == "CANCELLED"
	case "expired":
		return target == "EXPIRED" || target == "ARCHIVED"
	default:
		return false
	}
}

func turnStateCompatible(source, target string) bool {
	switch source {
	case "queued":
		return target == "QUEUED"
	case "running", "capacity_retry":
		return target == "CLAIMED" || target == "RUNNING"
	case "blocked":
		return target == "BLOCKED" || target == "WAITING_OWNER" || target == "WAITING_EXTERNAL"
	case "succeeded":
		return target == "SUCCEEDED"
	case "failed":
		return target == "FAILED"
	case "canceled", "cancelled":
		return target == "CANCELLED"
	default:
		return false
	}
}

func processStateCompatible(source, target string) bool {
	switch source {
	case "running":
		return target == "RUNNING" || target == "WAITING_OWNER" || target == "WAITING_EXTERNAL" || target == "BLOCKED"
	case "waiting_owner":
		return target == "WAITING_OWNER"
	case "completed":
		return target == "SUCCEEDED"
	case "failed":
		return target == "FAILED"
	default:
		return false
	}
}

func knownAttemptState(state string) bool {
	switch state {
	case "QUEUED", "CLAIMED", "WAITING_OWNER", "WAITING_EXTERNAL", "BLOCKED", "SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
}

func knownExecutionState(state string) bool {
	switch state {
	case "PENDING", "ADMITTED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED", "RETRIED", "SUSPENDED":
		return true
	default:
		return false
	}
}

func executionStateCompatible(source, target string) bool {
	if !knownExecutionState(target) {
		return false
	}
	switch source {
	case "queued", "capacity_retry":
		return target == "PENDING" || target == "ADMITTED" || target == "RETRIED"
	case "running":
		return target == "ADMITTED" || target == "RUNNING"
	case "blocked":
		return target == "SUSPENDED"
	case "succeeded":
		return target == "SUCCEEDED"
	case "failed":
		return target == "FAILED" || target == "RETRIED"
	case "canceled", "cancelled":
		return target == "CANCELLED"
	default:
		return false
	}
}

func knownTurn(status string) bool {
	return terminalTurn(status) || status == "queued" || status == "running" || status == "capacity_retry" || status == "blocked"
}

func terminalProcess(status string) bool { return status == "completed" || status == "failed" }
func knownProcess(status string) bool {
	return terminalProcess(status) || status == "running" || status == "waiting_owner"
}

func artifactCardinality(value any) uint64 {
	switch typed := value.(type) {
	case map[string]any:
		return uint64(len(typed))
	case []any:
		return uint64(len(typed))
	case json.Number:
		parsed, _ := strconv.ParseUint(string(typed), 10, 64)
		return parsed
	case float64:
		if typed >= 0 {
			return uint64(typed)
		}
		return 0
	default:
		return 0
	}
}

func eligibleArtifact(artifact model.TargetResource) bool {
	if artifact.Kind != "ARTIFACT" || artifact.State != "ACTIVE" || artifact.Version == 0 ||
		!validStableKey(text(artifact.Spec, "kind")) ||
		!set("INPUT", "OUTPUT", "ARCHIVE")[text(artifact.Spec, "direction")] ||
		text(artifact.Spec, "scanStatus") != "CLEAN" || number(artifact.Spec, "scanPolicyRevision") <= 0 ||
		!validSHA(text(artifact.Spec, "scanEvidenceSha256")) || !validSHA(text(artifact.Spec, "sha256")) ||
		number(artifact.Spec, "sizeBytes") <= 0 || number(artifact.Spec, "sizeBytes") > 10<<30 ||
		len(text(artifact.Spec, "mediaType")) < 3 || len(text(artifact.Spec, "mediaType")) > 255 ||
		!validExternalRef(text(artifact.Spec, "retentionPolicyRef")) ||
		!validStableKey(text(artifact.Spec, "scannerWorkloadId")) || text(artifact.Spec, "scannedAt") == "" {
		return false
	}
	if scannedAt, err := time.Parse(time.RFC3339Nano, text(artifact.Spec, "scannedAt")); err != nil || scannedAt.IsZero() {
		return false
	}
	reference, err := url.Parse(text(artifact.Spec, "storageRef"))
	if err != nil || !validExternalRef(text(artifact.Spec, "storageRef")) ||
		reference.Scheme != "s3" || reference.Host == "" || reference.Path == "" || reference.Path == "/" ||
		reference.User != nil || reference.Fragment != "" {
		return false
	}
	query := reference.Query()
	return len(query) == 1 && len(query["versionId"]) == 1 && strings.TrimSpace(query.Get("versionId")) != ""
}

func validExternalRef(value string) bool {
	if len(value) < 1 || len(value) > 512 || value != strings.TrimSpace(value) {
		return false
	}
	for _, symbol := range value {
		if symbol < 0x20 || symbol == 0x7f {
			return false
		}
	}
	return true
}

func validStableKey(value string) bool {
	if len(value) < 1 || len(value) > 96 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	separator := false
	for _, symbol := range value[1:] {
		switch {
		case symbol >= 'a' && symbol <= 'z', symbol >= '0' && symbol <= '9':
			separator = false
		case symbol == '-' || symbol == '_':
			if separator {
				return false
			}
			separator = true
		default:
			return false
		}
	}
	return !separator
}
