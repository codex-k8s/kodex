package resource

import (
	"slices"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const (
	maximumLegacyOperations = 2000
	maximumLegacyPlanBytes  = 8 << 20
	maximumLegacyReferences = 20000
	maximumSafeInteger      = uint64(9007199254740991)
)

func legacyOperationSource(operation entity.LegacyGraphOperation) (entity.LegacyOperationSource, string, error) {
	var source entity.LegacyOperationSource
	kind := ""
	count := 0
	selectSource := func(candidate entity.LegacyOperationSource, candidateKind string) {
		source, kind, count = candidate, candidateKind, count+1
	}
	if operation.Project != nil {
		selectSource(operation.Project.Source, "PROJECT")
	}
	if operation.Team != nil {
		selectSource(operation.Team.Source, "TEAM")
	}
	if operation.Chat != nil {
		selectSource(operation.Chat.Source, "CHAT")
	}
	if operation.Artifact != nil {
		selectSource(operation.Artifact.Source, "ARTIFACT")
	}
	if operation.CredentialBinding != nil {
		selectSource(operation.CredentialBinding.Source, "CREDENTIAL_BINDING")
	}
	if operation.RepositoryWorkspace != nil {
		selectSource(operation.RepositoryWorkspace.Source, "REPOSITORY_WORKSPACE")
	}
	if operation.RoleDefinition != nil {
		selectSource(operation.RoleDefinition.Source, "ROLE_DEFINITION")
	}
	if operation.InstructionSet != nil {
		selectSource(operation.InstructionSet.Source, "INSTRUCTION_SET")
	}
	if operation.ProviderReference != nil {
		selectSource(operation.ProviderReference.Source, "PROVIDER_CONNECTION_REFERENCE")
	}
	if operation.ProviderPool != nil {
		selectSource(operation.ProviderPool.Source, "PROVIDER_POOL")
	}
	if operation.RoleImageRecipe != nil {
		selectSource(operation.RoleImageRecipe.Source, "ROLE_IMAGE_RECIPE")
	}
	if operation.ImageBuild != nil {
		selectSource(operation.ImageBuild.Source, "IMAGE_BUILD")
	}
	if operation.ImageArtifact != nil {
		selectSource(operation.ImageArtifact.Source, "IMAGE_ARTIFACT")
	}
	if operation.Agent != nil {
		selectSource(operation.Agent.Source, "AGENT")
	}
	if operation.AgentAssignment != nil {
		selectSource(operation.AgentAssignment.Source, "AGENT_ASSIGNMENT")
	}
	if operation.Schedule != nil {
		selectSource(operation.Schedule.Source, "SCHEDULE")
	}
	if operation.RuntimeRevision != nil {
		selectSource(operation.RuntimeRevision.Source, "RUNTIME_REVISION")
	}
	if operation.Session != nil {
		selectSource(operation.Session.Source, "SESSION")
	}
	if operation.Turn != nil {
		selectSource(operation.Turn.Source, "TURN")
	}
	if operation.TurnAttempt != nil {
		selectSource(operation.TurnAttempt.Source, "TURN_ATTEMPT")
	}
	if operation.ProcessRun != nil {
		selectSource(operation.ProcessRun.Source, "PROCESS_RUN")
	}
	if operation.DelegationEdge != nil {
		selectSource(operation.DelegationEdge.Source, "DELEGATION_EDGE")
	}
	if operation.CallbackManifest != nil {
		selectSource(operation.CallbackManifest.Source, "CALLBACK_MANIFEST")
	}
	if operation.CallbackDelivery != nil {
		selectSource(operation.CallbackDelivery.Source, "CALLBACK_DELIVERY")
	}
	if operation.MemoryRecord != nil {
		selectSource(operation.MemoryRecord.Source, "MEMORY_RECORD")
	}
	if count != 1 {
		return entity.LegacyOperationSource{}, "", errs.ErrInvalidInput
	}
	return source, kind, nil
}

func legacyOperationRank(operation entity.LegacyGraphOperation) int {
	_, kind, err := legacyOperationSource(operation)
	if err != nil {
		return -1
	}
	switch kind {
	case "PROJECT":
		return 0
	case "TEAM", "CHAT":
		return 1
	case "ARTIFACT", "CREDENTIAL_BINDING", "REPOSITORY_WORKSPACE":
		return 2
	case "ROLE_DEFINITION":
		return 3
	case "INSTRUCTION_SET":
		return 4
	case "PROVIDER_CONNECTION_REFERENCE":
		return 5
	case "PROVIDER_POOL":
		return 6
	case "ROLE_IMAGE_RECIPE":
		return 7
	case "IMAGE_BUILD":
		return 8
	case "IMAGE_ARTIFACT":
		return 9
	case "AGENT":
		return 10
	case "AGENT_ASSIGNMENT":
		return 11
	case "SCHEDULE":
		return 12
	case "RUNTIME_REVISION":
		return 13
	case "SESSION":
		return 14
	case "TURN":
		return 15
	case "TURN_ATTEMPT":
		return 16
	case "PROCESS_RUN":
		return 17
	case "DELEGATION_EDGE":
		return 18
	case "CALLBACK_MANIFEST":
		return 19
	case "CALLBACK_DELIVERY":
		return 20
	case "MEMORY_RECORD":
		return 21
	default:
		return -1
	}
}

func validLegacyState(state string) bool {
	switch state {
	case "ACTIVE", "PAUSED", "ARCHIVED", "DELETION_PENDING", "DELETED",
		"QUEUED", "CLAIMED", "RUNNING", "WAITING_OWNER", "WAITING_EXTERNAL",
		"SUCCEEDED", "FAILED", "CANCELLED", "EXPIRED", "BLOCKED":
		return true
	default:
		return false
	}
}

func validateLegacyGraphPlan(plan entity.LegacyGraphPlan) error {
	if value.ValidateID(plan.PlanID) != nil || len(plan.SourceRootReference) < 1 ||
		len(plan.SourceRootReference) > 512 || strings.TrimSpace(plan.SourceRootReference) != plan.SourceRootReference ||
		!validSHA256Text(plan.SourceRootSHA256) || !validSHA256Text(plan.SourceSnapshotSHA256) ||
		len(plan.Dispositions) != len(entity.LegacySourceTables) || len(plan.Operations) < 1 ||
		len(plan.Operations) > maximumLegacyOperations {
		return errs.ErrInvalidInput
	}
	if err := validateLegacyOperationOrder(plan.Operations); err != nil {
		return err
	}
	expectedTables := make(map[string]struct{}, len(entity.LegacySourceTables))
	for _, table := range entity.LegacySourceTables {
		expectedTables[table] = struct{}{}
	}
	materializedTables := make(map[string]uint64, len(plan.Dispositions))
	for _, disposition := range plan.Dispositions {
		if _, ok := expectedTables[disposition.SourceTable]; !ok ||
			!validSHA256Text(disposition.SourceSHA256) || disposition.RowCount > maximumSafeInteger {
			return errs.ErrInvalidInput
		}
		delete(expectedTables, disposition.SourceTable)
		switch disposition.Disposition {
		case entity.LegacyDispositionMaterialize:
			if disposition.RowCount == 0 || disposition.TerminalStateSHA256 != "" {
				return errs.ErrInvalidInput
			}
			materializedTables[disposition.SourceTable] = disposition.RowCount
		case entity.LegacyDispositionArchiveTerminal:
			if !validSHA256Text(disposition.TerminalStateSHA256) {
				return errs.ErrInvalidInput
			}
		case entity.LegacyDispositionRejectNonempty:
			if disposition.RowCount != 0 || disposition.TerminalStateSHA256 != "" {
				return errs.ErrInvalidInput
			}
		default:
			return errs.ErrInvalidInput
		}
	}
	if len(expectedTables) != 0 {
		return errs.ErrInvalidInput
	}
	sourceSnapshotSHA256, err := legacySourceSnapshotSHA256(plan.Dispositions)
	if err != nil || sourceSnapshotSHA256 != plan.SourceSnapshotSHA256 {
		return errs.ErrFailedPrecondition
	}
	localKinds := make(map[string]string, len(plan.Operations))
	seenTargetIDs := make(map[string]struct{}, len(plan.Operations))
	materializedSourceCounts := make(map[string]uint64, len(materializedTables))
	sourceProofs := make(map[string]entity.LegacyOperationSource, len(plan.Operations))
	kindCounts := make(map[string]int)
	referenceCount := 0
	for _, operation := range plan.Operations {
		source, kind, err := legacyOperationSource(operation)
		_, materialized := materializedTables[source.SourceTable]
		if err != nil || !materialized ||
			value.ValidateStableKey(source.LocalRef) != nil || len(source.SourceRef) < 1 ||
			len(source.SourceRef) > 512 || source.SourceRevision == 0 ||
			!validSHA256Text(source.SourceSHA256) || value.ValidateID(operation.TargetID) != nil {
			return errs.ErrInvalidInput
		}
		if _, duplicate := localKinds[source.LocalRef]; duplicate {
			return errs.ErrInvalidInput
		}
		if _, duplicate := seenTargetIDs[operation.TargetID]; duplicate {
			return errs.ErrInvalidInput
		}
		localKinds[source.LocalRef], seenTargetIDs[operation.TargetID] = kind, struct{}{}
		proofKey := source.SourceTable + "\x00" + source.SourceRef
		if existing, ok := sourceProofs[proofKey]; ok {
			if existing.SourceRevision != source.SourceRevision || existing.SourceSHA256 != source.SourceSHA256 {
				return errs.ErrFailedPrecondition
			}
		} else {
			materializedSourceCounts[source.SourceTable]++
		}
		sourceProofs[proofKey] = source
		kindCounts[kind]++
		referenceCount += len(legacyOperationReferences(operation))
		if referenceCount > maximumLegacyReferences {
			return errs.ErrInvalidInput
		}
	}
	if kindCounts["PROJECT"] != 1 {
		return errs.ErrInvalidInput
	}
	if err := validateLegacyMaterializedSourceCounts(materializedTables, materializedSourceCounts); err != nil {
		return err
	}
	for _, required := range []string{
		"ARTIFACT", "CREDENTIAL_BINDING", "ROLE_DEFINITION", "INSTRUCTION_SET",
		"PROVIDER_CONNECTION_REFERENCE", "PROVIDER_POOL", "ROLE_IMAGE_RECIPE",
		"IMAGE_BUILD", "IMAGE_ARTIFACT", "AGENT", "AGENT_ASSIGNMENT",
	} {
		if kindCounts[required] == 0 {
			return errs.ErrFailedPrecondition
		}
	}
	if materializedTables["matter_codex_repositories"] > 0 && kindCounts["REPOSITORY_WORKSPACE"] == 0 {
		return errs.ErrFailedPrecondition
	}
	runtimeKinds := []string{"RUNTIME_REVISION", "SESSION", "TURN", "TURN_ATTEMPT", "PROCESS_RUN"}
	runtimePresent := false
	for _, kind := range runtimeKinds {
		runtimePresent = runtimePresent || kindCounts[kind] > 0
	}
	for _, table := range []string{
		"matter_codex_agent_sessions", "matter_codex_agent_session_turns", "matter_codex_process_runs",
	} {
		runtimePresent = runtimePresent || materializedTables[table] > 0
	}
	if runtimePresent {
		for _, required := range runtimeKinds {
			if kindCounts[required] == 0 {
				return errs.ErrFailedPrecondition
			}
		}
	}
	if err := validateLegacyReferences(plan.Operations, localKinds); err != nil {
		return err
	}
	if err := validateLegacyLifecycleAndLineage(plan.Operations); err != nil {
		return err
	}
	return nil
}

func legacySourceSnapshotSHA256(dispositions []entity.LegacySourceDisposition) (string, error) {
	type snapshotDisposition struct {
		SourceTable    string `json:"sourceTable"`
		Disposition    string `json:"disposition"`
		RowCount       uint64 `json:"rowCount"`
		SourceSHA256   string `json:"sourceSha256"`
		TerminalSHA256 string `json:"terminalStateSha256,omitempty"`
	}
	canonical := make([]snapshotDisposition, 0, len(dispositions))
	for _, disposition := range dispositions {
		canonical = append(canonical, snapshotDisposition{
			SourceTable: disposition.SourceTable, Disposition: disposition.Disposition,
			RowCount: disposition.RowCount, SourceSHA256: disposition.SourceSHA256,
			TerminalSHA256: disposition.TerminalStateSHA256,
		})
	}
	slices.SortFunc(canonical, func(left, right snapshotDisposition) int {
		return strings.Compare(left.SourceTable, right.SourceTable)
	})
	return canonicalHash(canonical)
}

func validateLegacyMaterializedSourceCounts(expected, actual map[string]uint64) error {
	for sourceTable, rowCount := range expected {
		if rowCount == 0 || actual[sourceTable] != rowCount {
			return errs.ErrFailedPrecondition
		}
	}
	return nil
}

func validateLegacyOperationOrder(operations []entity.LegacyGraphOperation) error {
	previousRank := -1
	for index, operation := range operations {
		_, kind, err := legacyOperationSource(operation)
		rank := legacyOperationRank(operation)
		if err != nil || rank < previousRank || index == 0 && kind != "PROJECT" {
			return errs.ErrInvalidInput
		}
		previousRank = rank
	}
	return nil
}

func legacyOperationReferences(operation entity.LegacyGraphOperation) []string {
	result := make([]string, 0, 16)
	add := func(values ...string) {
		for _, value := range values {
			if value != "" {
				result = append(result, value)
			}
		}
	}
	switch {
	case operation.Team != nil:
		add(operation.Team.RoleDefinitionRefs...)
	case operation.Chat != nil:
		add(operation.Chat.DefaultAgentRef)
	case operation.RepositoryWorkspace != nil:
		add(operation.RepositoryWorkspace.CredentialBindingRef, operation.RepositoryWorkspace.SnapshotArtifactRef)
	case operation.RoleDefinition != nil:
		add(operation.RoleDefinition.RoleImageRecipeRef)
		add(operation.RoleDefinition.AllowedRoleRefs...)
	case operation.InstructionSet != nil:
		add(operation.InstructionSet.ContentArtifactRef)
	case operation.ProviderReference != nil:
		add(operation.ProviderReference.CredentialBindingRef)
	case operation.ProviderPool != nil:
		for _, binding := range operation.ProviderPool.Bindings {
			add(binding.ProviderReferenceRef)
		}
	case operation.ImageBuild != nil:
		add(operation.ImageBuild.RecipeRef)
	case operation.ImageArtifact != nil:
		add(operation.ImageArtifact.RecipeRef, operation.ImageArtifact.ImageBuildRef)
	case operation.Agent != nil:
		add(operation.Agent.RoleDefinitionRef, operation.Agent.InstructionSetRef,
			operation.Agent.ProviderPoolRef, operation.Agent.RoleImageRecipeRef)
	case operation.AgentAssignment != nil:
		add(operation.AgentAssignment.AgentRef, operation.AgentAssignment.RoomRef)
	case operation.Schedule != nil:
		add(operation.Schedule.AgentRef, operation.Schedule.AssignmentRef,
			operation.Schedule.InstructionSetRef, operation.Schedule.ProviderPoolRef,
			operation.Schedule.RoomRef, operation.Schedule.RoleImageRecipeRef)
	case operation.RuntimeRevision != nil:
		add(operation.RuntimeRevision.SessionRef, operation.RuntimeRevision.ChatRef,
			operation.RuntimeRevision.AgentRef, operation.RuntimeRevision.AssignmentRef,
			operation.RuntimeRevision.RoleDefinitionRef, operation.RuntimeRevision.InstructionSetRef,
			operation.RuntimeRevision.ProviderPoolRef, operation.RuntimeRevision.ProviderCredentialRef,
			operation.RuntimeRevision.RoleImageRecipeRef, operation.RuntimeRevision.ImageBuildRef,
			operation.RuntimeRevision.ImageArtifactRef, operation.RuntimeRevision.PromptArtifactRef)
		for _, component := range operation.RuntimeRevision.Components {
			add(component.LocalRef)
		}
	case operation.Session != nil:
		add(operation.Session.AgentRef, operation.Session.ProviderPoolRef,
			operation.Session.AssignmentRef, operation.Session.ChatRef)
	case operation.Turn != nil:
		add(operation.Turn.SessionRef, operation.Turn.PromptArtifactRef,
			operation.Turn.RuntimeRevisionRef, operation.Turn.PredecessorTurnRef,
			operation.Turn.ParentTurnRef, operation.Turn.ProcessRunRef, operation.Turn.ResultArtifactRef)
	case operation.TurnAttempt != nil:
		add(operation.TurnAttempt.TurnRef, operation.TurnAttempt.RuntimeRevisionRef)
	case operation.ProcessRun != nil:
		add(operation.ProcessRun.RootSessionRef, operation.ProcessRun.RootTurnRef,
			operation.ProcessRun.RootAttemptRef, operation.ProcessRun.RuntimeRevisionRef,
			operation.ProcessRun.ParentProcessRef, operation.ProcessRun.LaunchingTurnRef,
			operation.ProcessRun.LaunchingAttemptRef, operation.ProcessRun.DelegationRef,
			operation.ProcessRun.TargetSessionRef, operation.ProcessRun.TargetTurnRef,
			operation.ProcessRun.TargetAttemptRef)
	case operation.DelegationEdge != nil:
		add(operation.DelegationEdge.ParentProcessRef, operation.DelegationEdge.ParentSessionRef,
			operation.DelegationEdge.ParentTurnRef, operation.DelegationEdge.ParentAttemptRef,
			operation.DelegationEdge.ChildRoleRef, operation.DelegationEdge.ChildSessionRef,
			operation.DelegationEdge.ChildTurnRef, operation.DelegationEdge.ChildAttemptRef,
			operation.DelegationEdge.ChildProcessRef)
	case operation.CallbackManifest != nil:
		add(operation.CallbackManifest.DelegationRef, operation.CallbackManifest.CallbackProcessRef)
	case operation.CallbackDelivery != nil:
		add(operation.CallbackDelivery.CallbackManifestRef)
	}
	return result
}

func validateLegacyLifecycleAndLineage(operations []entity.LegacyGraphOperation) error {
	byReference := make(map[string]entity.LegacyGraphOperation, len(operations))
	for _, operation := range operations {
		source, _, err := legacyOperationSource(operation)
		if err != nil {
			return err
		}
		byReference[source.LocalRef] = operation
	}
	manifestDeliveries := make(map[string]map[string]struct{})
	for _, operation := range operations {
		switch {
		case operation.Turn != nil:
			input := operation.Turn
			runtime := byReference[input.RuntimeRevisionRef].RuntimeRevision
			if input.Sequence == 0 || input.Attempt == 0 || !validSHA256Text(input.EffectiveInputSHA256) ||
				runtime == nil || runtime.SessionRef != input.SessionRef {
				return errs.ErrFailedPrecondition
			}
			if input.PredecessorTurnRef != "" {
				predecessor := byReference[input.PredecessorTurnRef].Turn
				if predecessor == nil || predecessor.SessionRef != input.SessionRef || predecessor.Sequence >= input.Sequence {
					return errs.ErrFailedPrecondition
				}
			}
		case operation.TurnAttempt != nil:
			input := operation.TurnAttempt
			turn := byReference[input.TurnRef].Turn
			state := enum.State(input.State)
			finished := enum.TurnAttemptStateFinished(input.State)
			if input.Attempt == 0 || input.Attempt > 100 || !validSHA256Text(input.ImmutableInputSHA256) ||
				turn == nil || turn.RuntimeRevisionRef != input.RuntimeRevisionRef ||
				!enum.TurnAttemptStateValid(input.State) || state == enum.StateRunning ||
				state == enum.StateClaimed || !legacyTimeWithinBounds(input.StartedAt) ||
				finished != !input.FinishedAt.IsZero() ||
				(!input.FinishedAt.IsZero() && (!legacyTimeWithinBounds(input.FinishedAt) || input.FinishedAt.Before(input.StartedAt))) ||
				(state == enum.StateQueued && input.Outcome != "") || len(input.Outcome) > 256 {
				return errs.ErrFailedPrecondition
			}
		case operation.ProcessRun != nil:
			input := operation.ProcessRun
			rootTurn := byReference[input.RootTurnRef].Turn
			rootAttempt := byReference[input.RootAttemptRef].TurnAttempt
			state := input.State
			if rootTurn == nil || rootAttempt == nil || rootTurn.SessionRef != input.RootSessionRef ||
				rootAttempt.TurnRef != input.RootTurnRef ||
				!validSHA256Text(input.ImmutableInputSHA256) || input.LegacyPolicyRevision == 0 ||
				!validSHA256Text(input.LegacyPolicySHA256) || !validLegacyState(string(state)) ||
				state == enum.StateRunning || state == enum.StateClaimed || state == enum.StateQueued {
				return errs.ErrFailedPrecondition
			}
			hasParent := input.ParentProcessRef != ""
			for _, reference := range []string{input.LaunchingTurnRef, input.LaunchingAttemptRef,
				input.DelegationRef, input.TargetSessionRef, input.TargetTurnRef, input.TargetAttemptRef} {
				if (reference != "") != hasParent {
					return errs.ErrFailedPrecondition
				}
			}
			if hasParent {
				parentProcess := byReference[input.ParentProcessRef].ProcessRun
				launchingTurn := byReference[input.LaunchingTurnRef].Turn
				launchingAttempt := byReference[input.LaunchingAttemptRef].TurnAttempt
				targetSession := byReference[input.TargetSessionRef].Session
				targetTurn := byReference[input.TargetTurnRef].Turn
				targetAttempt := byReference[input.TargetAttemptRef].TurnAttempt
				delegation := byReference[input.DelegationRef].DelegationEdge
				if parentProcess == nil || launchingTurn == nil || launchingAttempt == nil || targetSession == nil ||
					targetTurn == nil || targetAttempt == nil || delegation == nil ||
					parentProcess.RootSessionRef != input.RootSessionRef ||
					parentProcess.RootTurnRef != input.RootTurnRef ||
					parentProcess.RootAttemptRef != input.RootAttemptRef ||
					parentProcess.LegacyPolicyRevision != input.LegacyPolicyRevision ||
					parentProcess.LegacyPolicySHA256 != input.LegacyPolicySHA256 ||
					parentProcess.PlaybookRef != input.PlaybookRef ||
					parentProcess.RootTriggerRef != input.RootTriggerRef ||
					launchingAttempt.TurnRef != input.LaunchingTurnRef ||
					targetTurn.SessionRef != input.TargetSessionRef || targetAttempt.TurnRef != input.TargetTurnRef ||
					targetAttempt.RuntimeRevisionRef != input.RuntimeRevisionRef ||
					targetSession.AgentRef != delegation.ChildRoleRef ||
					delegation.ParentProcessRef != input.ParentProcessRef ||
					delegation.ParentTurnRef != input.LaunchingTurnRef ||
					delegation.ParentAttemptRef != input.LaunchingAttemptRef ||
					delegation.ChildSessionRef != input.TargetSessionRef ||
					delegation.ChildTurnRef != input.TargetTurnRef ||
					delegation.ChildAttemptRef != input.TargetAttemptRef ||
					delegation.ChildProcessRef != legacyLocalRef(operation) {
					return errs.ErrFailedPrecondition
				}
			} else if rootAttempt.RuntimeRevisionRef != input.RuntimeRevisionRef {
				return errs.ErrFailedPrecondition
			}
		case operation.DelegationEdge != nil:
			input := operation.DelegationEdge
			parentTurn := byReference[input.ParentTurnRef].Turn
			parentAttempt := byReference[input.ParentAttemptRef].TurnAttempt
			childTurn := byReference[input.ChildTurnRef].Turn
			childAttempt := byReference[input.ChildAttemptRef].TurnAttempt
			childProcess := byReference[input.ChildProcessRef].ProcessRun
			if input.GrantGeneration == 0 || parentTurn == nil || parentAttempt == nil || childTurn == nil ||
				childAttempt == nil || childProcess == nil || parentTurn.SessionRef != input.ParentSessionRef ||
				parentAttempt.TurnRef != input.ParentTurnRef || childTurn.SessionRef != input.ChildSessionRef ||
				childAttempt.TurnRef != input.ChildTurnRef || childProcess.DelegationRef != legacyLocalRef(operation) {
				return errs.ErrFailedPrecondition
			}
		case operation.CallbackManifest != nil:
			input := operation.CallbackManifest
			if !validSHA256Text(input.ManifestSHA256) {
				return errs.ErrFailedPrecondition
			}
			manifestDeliveries[legacyLocalRef(operation)] = make(map[string]struct{}, len(input.Destinations))
		case operation.CallbackDelivery != nil:
			input := operation.CallbackDelivery
			manifest := byReference[input.CallbackManifestRef].CallbackManifest
			if manifest == nil || !slices.Contains(manifest.Destinations, input.Destination) ||
				!validSHA256Text(input.ReceiptSHA256) || !legacyTimeWithinBounds(input.DeliveredAt) ||
				(input.TerminalState != "DELIVERED" && input.TerminalState != "FAILED" &&
					input.TerminalState != "CANCELLED") {
				return errs.ErrFailedPrecondition
			}
			seen := manifestDeliveries[input.CallbackManifestRef]
			if _, duplicate := seen[input.Destination]; duplicate {
				return errs.ErrInvalidInput
			}
			seen[input.Destination] = struct{}{}
		}
	}
	for manifestRef, deliveries := range manifestDeliveries {
		if len(deliveries) != len(byReference[manifestRef].CallbackManifest.Destinations) {
			return errs.ErrFailedPrecondition
		}
	}
	return nil
}

func legacyLocalRef(operation entity.LegacyGraphOperation) string {
	source, _, _ := legacyOperationSource(operation)
	return source.LocalRef
}

func validateLegacyReferences(operations []entity.LegacyGraphOperation, kinds map[string]string) error {
	require := func(reference string, expected ...string) error {
		if reference == "" {
			return errs.ErrFailedPrecondition
		}
		kind, ok := kinds[reference]
		if !ok || !slices.Contains(expected, kind) {
			return errs.ErrFailedPrecondition
		}
		return nil
	}
	optional := func(reference string, expected ...string) error {
		if reference == "" {
			return nil
		}
		return require(reference, expected...)
	}
	for _, operation := range operations {
		switch {
		case operation.Team != nil:
			if len(operation.Team.RoleDefinitionRefs) == 0 || len(operation.Team.RoleDefinitionRefs) > 64 {
				return errs.ErrInvalidInput
			}
			for _, reference := range operation.Team.RoleDefinitionRefs {
				if err := require(reference, "ROLE_DEFINITION"); err != nil {
					return err
				}
			}
		case operation.Chat != nil:
			if err := optional(operation.Chat.DefaultAgentRef, "AGENT"); err != nil {
				return err
			}
		case operation.RepositoryWorkspace != nil:
			if err := optional(operation.RepositoryWorkspace.CredentialBindingRef, "CREDENTIAL_BINDING"); err != nil {
				return err
			}
			if err := optional(operation.RepositoryWorkspace.SnapshotArtifactRef, "ARTIFACT"); err != nil {
				return err
			}
		case operation.RoleDefinition != nil:
			if err := require(operation.RoleDefinition.RoleImageRecipeRef, "ROLE_IMAGE_RECIPE"); err != nil {
				return err
			}
			for _, reference := range operation.RoleDefinition.AllowedRoleRefs {
				if err := require(reference, "ROLE_DEFINITION"); err != nil {
					return err
				}
			}
		case operation.InstructionSet != nil:
			if err := require(operation.InstructionSet.ContentArtifactRef, "ARTIFACT"); err != nil {
				return err
			}
		case operation.ProviderReference != nil:
			if err := require(operation.ProviderReference.CredentialBindingRef, "CREDENTIAL_BINDING"); err != nil {
				return err
			}
		case operation.ProviderPool != nil:
			if len(operation.ProviderPool.Bindings) == 0 || len(operation.ProviderPool.Bindings) > 32 {
				return errs.ErrInvalidInput
			}
			for _, binding := range operation.ProviderPool.Bindings {
				if err := require(binding.ProviderReferenceRef, "PROVIDER_CONNECTION_REFERENCE"); err != nil {
					return err
				}
			}
		case operation.ImageBuild != nil:
			if err := require(operation.ImageBuild.RecipeRef, "ROLE_IMAGE_RECIPE"); err != nil {
				return err
			}
		case operation.ImageArtifact != nil:
			if err := require(operation.ImageArtifact.RecipeRef, "ROLE_IMAGE_RECIPE"); err != nil {
				return err
			}
			if err := require(operation.ImageArtifact.ImageBuildRef, "IMAGE_BUILD"); err != nil {
				return err
			}
		case operation.Agent != nil:
			for reference, expected := range map[string]string{
				operation.Agent.RoleDefinitionRef:  "ROLE_DEFINITION",
				operation.Agent.InstructionSetRef:  "INSTRUCTION_SET",
				operation.Agent.ProviderPoolRef:    "PROVIDER_POOL",
				operation.Agent.RoleImageRecipeRef: "ROLE_IMAGE_RECIPE",
			} {
				if err := require(reference, expected); err != nil {
					return err
				}
			}
		case operation.AgentAssignment != nil:
			if err := require(operation.AgentAssignment.AgentRef, "AGENT"); err != nil {
				return err
			}
			if err := optional(operation.AgentAssignment.RoomRef, "CHAT"); err != nil {
				return err
			}
		case operation.Schedule != nil:
			for reference, expected := range map[string]string{
				operation.Schedule.AgentRef: "AGENT", operation.Schedule.AssignmentRef: "AGENT_ASSIGNMENT",
				operation.Schedule.InstructionSetRef: "INSTRUCTION_SET", operation.Schedule.ProviderPoolRef: "PROVIDER_POOL",
				operation.Schedule.RoleImageRecipeRef: "ROLE_IMAGE_RECIPE",
			} {
				if err := require(reference, expected); err != nil {
					return err
				}
			}
			if err := optional(operation.Schedule.RoomRef, "CHAT"); err != nil {
				return err
			}
		case operation.RuntimeRevision != nil:
			for reference, expected := range map[string]string{
				operation.RuntimeRevision.SessionRef: "SESSION", operation.RuntimeRevision.AgentRef: "AGENT",
				operation.RuntimeRevision.AssignmentRef:         "AGENT_ASSIGNMENT",
				operation.RuntimeRevision.RoleDefinitionRef:     "ROLE_DEFINITION",
				operation.RuntimeRevision.InstructionSetRef:     "INSTRUCTION_SET",
				operation.RuntimeRevision.ProviderPoolRef:       "PROVIDER_POOL",
				operation.RuntimeRevision.ProviderCredentialRef: "CREDENTIAL_BINDING",
				operation.RuntimeRevision.RoleImageRecipeRef:    "ROLE_IMAGE_RECIPE",
				operation.RuntimeRevision.ImageBuildRef:         "IMAGE_BUILD",
				operation.RuntimeRevision.ImageArtifactRef:      "IMAGE_ARTIFACT",
				operation.RuntimeRevision.PromptArtifactRef:     "ARTIFACT",
			} {
				if err := require(reference, expected); err != nil {
					return err
				}
			}
			if err := optional(operation.RuntimeRevision.ChatRef, "CHAT"); err != nil {
				return err
			}
			for _, component := range operation.RuntimeRevision.Components {
				if _, ok := kinds[component.LocalRef]; !ok || component.ProjectionSHA256 != "" {
					return errs.ErrFailedPrecondition
				}
			}
		case operation.Session != nil:
			for reference, expected := range map[string]string{
				operation.Session.AgentRef: "AGENT", operation.Session.ProviderPoolRef: "PROVIDER_POOL",
				operation.Session.AssignmentRef: "AGENT_ASSIGNMENT",
			} {
				if err := require(reference, expected); err != nil {
					return err
				}
			}
			if err := optional(operation.Session.ChatRef, "CHAT"); err != nil {
				return err
			}
		case operation.Turn != nil:
			for reference, expected := range map[string]string{
				operation.Turn.SessionRef: "SESSION", operation.Turn.PromptArtifactRef: "ARTIFACT",
				operation.Turn.RuntimeRevisionRef: "RUNTIME_REVISION",
			} {
				if err := require(reference, expected); err != nil {
					return err
				}
			}
			for _, reference := range []string{operation.Turn.PredecessorTurnRef, operation.Turn.ParentTurnRef} {
				if err := optional(reference, "TURN"); err != nil {
					return err
				}
			}
			if err := optional(operation.Turn.ProcessRunRef, "PROCESS_RUN"); err != nil {
				return err
			}
			if err := optional(operation.Turn.ResultArtifactRef, "ARTIFACT"); err != nil {
				return err
			}
		case operation.TurnAttempt != nil:
			if err := require(operation.TurnAttempt.TurnRef, "TURN"); err != nil {
				return err
			}
			if err := require(operation.TurnAttempt.RuntimeRevisionRef, "RUNTIME_REVISION"); err != nil {
				return err
			}
		case operation.ProcessRun != nil:
			for reference, expected := range map[string]string{
				operation.ProcessRun.RootSessionRef: "SESSION", operation.ProcessRun.RootTurnRef: "TURN",
				operation.ProcessRun.RootAttemptRef:     "TURN_ATTEMPT",
				operation.ProcessRun.RuntimeRevisionRef: "RUNTIME_REVISION",
			} {
				if err := require(reference, expected); err != nil {
					return err
				}
			}
			if err := optional(operation.ProcessRun.ParentProcessRef, "PROCESS_RUN"); err != nil {
				return err
			}
			if err := optional(operation.ProcessRun.LaunchingTurnRef, "TURN"); err != nil {
				return err
			}
			if err := optional(operation.ProcessRun.LaunchingAttemptRef, "TURN_ATTEMPT"); err != nil {
				return err
			}
			for reference, expected := range map[string]string{
				operation.ProcessRun.DelegationRef:    "DELEGATION_EDGE",
				operation.ProcessRun.TargetSessionRef: "SESSION",
				operation.ProcessRun.TargetTurnRef:    "TURN",
				operation.ProcessRun.TargetAttemptRef: "TURN_ATTEMPT",
			} {
				if err := optional(reference, expected); err != nil {
					return err
				}
			}
		case operation.DelegationEdge != nil:
			for reference, expected := range map[string]string{
				operation.DelegationEdge.ParentProcessRef: "PROCESS_RUN",
				operation.DelegationEdge.ParentSessionRef: "SESSION",
				operation.DelegationEdge.ParentTurnRef:    "TURN",
				operation.DelegationEdge.ParentAttemptRef: "TURN_ATTEMPT",
				operation.DelegationEdge.ChildRoleRef:     "AGENT",
				operation.DelegationEdge.ChildSessionRef:  "SESSION",
				operation.DelegationEdge.ChildTurnRef:     "TURN",
				operation.DelegationEdge.ChildAttemptRef:  "TURN_ATTEMPT",
				operation.DelegationEdge.ChildProcessRef:  "PROCESS_RUN",
			} {
				if err := require(reference, expected); err != nil {
					return err
				}
			}
		case operation.CallbackManifest != nil:
			if err := require(operation.CallbackManifest.DelegationRef, "DELEGATION_EDGE"); err != nil {
				return err
			}
			if err := require(operation.CallbackManifest.CallbackProcessRef, "PROCESS_RUN"); err != nil {
				return err
			}
			if len(operation.CallbackManifest.Destinations) < 1 || len(operation.CallbackManifest.Destinations) > 8 ||
				!slices.IsSorted(operation.CallbackManifest.Destinations) {
				return errs.ErrInvalidInput
			}
			for index, destination := range operation.CallbackManifest.Destinations {
				if len(destination) < 1 || len(destination) > 128 ||
					(index > 0 && destination == operation.CallbackManifest.Destinations[index-1]) {
					return errs.ErrInvalidInput
				}
			}
		case operation.CallbackDelivery != nil:
			if err := require(operation.CallbackDelivery.CallbackManifestRef, "CALLBACK_MANIFEST"); err != nil {
				return err
			}
		}
	}
	return nil
}

func legacyTimeWithinBounds(timestamp time.Time) bool {
	return !timestamp.IsZero() && timestamp.Year() >= 2000 && timestamp.Year() <= 2200
}
