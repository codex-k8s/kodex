package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryPersonalLaunchRequirements reads required personal grants before the legacy runtime
// readiness filter can hide them, retaining exact package freshness and a hard 257-row sentinel.
// queryPersonalLaunchRequirements читает обязательные personal grants до legacy runtime filter,
// сохраняя exact package freshness и жёсткий sentinel257 строк.
//
//go:embed sql/personal_launch__requirements.sql
var queryPersonalLaunchRequirements string

// queryPersonalLaunchConnections locks all addressed Connections in stable order and returns
// current configuration eligibility and actor visibility without secret descriptors.
// queryPersonalLaunchConnections блокирует Connection в устойчивом порядке и возвращает текущие
// configuration eligibility и actor visibility без secret descriptors.
//
//go:embed sql/personal_launch__connections.sql
var queryPersonalLaunchConnections string

// queryPersonalLaunchBindings locks only the resolved actor's visible bindings after Connection
// locks; missing bindings stay missing and concurrent creation must wait on the same Connection.
// queryPersonalLaunchBindings блокирует лишь видимые bindings разрешённого actor после Connection;
// отсутствующие bindings не создаются, конкурентное создание ждёт тот же Connection.
//
//go:embed sql/personal_launch__bindings.sql
var queryPersonalLaunchBindings string

// queryPersonalLaunchSessionOwner binds manual continuation to its stored owner rather than
// interpreting target launch rights as permission to use a different session's personal keys.
// queryPersonalLaunchSessionOwner связывает manual continuation с сохранённым владельцем;
// target launch rights не превращаются в право использовать чужие session personal keys.
//
//go:embed sql/personal_launch__session_owner.sql
var queryPersonalLaunchSessionOwner string

// personalLaunchContext contains only owner-resolved launch provenance and session coordinates.
// A noninteractive source cannot turn a schedule owner's USER identity into interactive consent.
// personalLaunchContext содержит лишь разрешённые владельцем provenance и session coordinates.
// Неинтерактивный source не превращает USER identity владельца расписания в interactive consent.
type personalLaunchContext struct{ source, sessionRef, targetType, targetRef, assistantScope string }

// personalLaunchTarget is derived from an authorized direct agent or a published workflow step.
// No browser-provided capability list or owner identifier enters the requirement query.
// personalLaunchTarget выводится из разрешённого direct agent либо опубликованного workflow step.
// Browser capability list и owner identifier не попадают в requirement query.
type personalLaunchTarget struct {
	AgentRef     string   `json:"agent_ref"`
	Capabilities []string `json:"capability_keys"`
	All          bool     `json:"all_capabilities"`
}

// personalLaunchConnection holds a locked current public configuration and availability facts.
// These facts do not grant execution, expose plaintext or replace per-call authority checks.
// personalLaunchConnection содержит заблокированную текущую public configuration и availability.
// Эти факты не разрешают исполнение, не раскрывают plaintext и не заменяют per-call authority.
type personalLaunchConnection struct {
	ref, name, definitionKey, definitionVersion, definitionDigest, subjectKind string
	version                                                                    int64
	policy                                                                     entity.IntegrationCredentialPolicy
	configuration                                                              []byte
	ready, visible, usable                                                     bool
}

// personalLaunchRequirement retains one grant's exact Connection version and capability. A
// duplicate requirement with any stale grant remains unavailable rather than being silently lost.
// personalLaunchRequirement сохраняет exact Connection version и capability одного grant.
// Повтор требования со stale grant остаётся недоступным, а не молча исчезает.
type personalLaunchRequirement struct {
	connectionRef, key string
	version            int64
	current            bool
}

// personalLaunchTargets excludes workflow coordinators, whose runtime contract permits delegation
// rather than integration calls, and uses only each published step's required capabilities.
// personalLaunchTargets исключает workflow coordinators, чей runtime contract разрешает delegation,
// и использует только required capabilities каждого опубликованного этапа.
func personalLaunchTargets(agentRefs []string, workflow *entity.WorkflowVersion) []personalLaunchTarget {
	if workflow == nil {
		result := make([]personalLaunchTarget, 0, len(agentRefs))
		for _, ref := range agentRefs {
			result = append(result, personalLaunchTarget{AgentRef: ref, All: true, Capabilities: []string{}})
		}
		return result
	}
	result := make([]personalLaunchTarget, 0, len(workflow.Steps))
	for _, step := range workflow.Steps {
		if len(step.RequiredCapabilityKeys) > 0 {
			result = append(result, personalLaunchTarget{AgentRef: step.AgentRef, Capabilities: append([]string(nil), step.RequiredCapabilityKeys...)})
		}
	}
	return result
}

// requirePersonalLaunchCredentials keeps one typed owner refusal for run, assistant-turn and
// planned-launch entry points; it never turns a positive observation into call authority.
// requirePersonalLaunchCredentials сохраняет единый типизированный owner refusal для run,
// assistant turn и planned launch; положительное наблюдение не становится call authority.
func (repository *Repository) requirePersonalLaunchCredentials(ctx context.Context, tx pgx.Tx, current scope, projectID string, targets []personalLaunchTarget, launch personalLaunchContext) error {
	issues, err := repository.personalLaunchCredentialIssues(ctx, tx, current, projectID, targets, launch)
	if err != nil {
		return err
	}
	if len(issues) > 0 {
		return &platformservice.IntegrationCredentialPreflightError{Issues: issues}
	}
	return nil
}

// personalLaunchCredentialIssues resolves every bounded personal requirement in the launch
// transaction. Connection then self-binding locks serialize with configure/revoke; no positive
// result is cached or turned into a call grant. Shared-policy launch behavior remains unchanged.
// personalLaunchCredentialIssues разрешает все bounded personal requirements в launch transaction.
// Connection→self-binding locks сериализуются с configure/revoke; положительный результат не
// кешируется и не становится call grant. Shared-policy launch behavior не меняется.
func (repository *Repository) personalLaunchCredentialIssues(ctx context.Context, tx pgx.Tx, current scope, projectID string, targets []personalLaunchTarget, launch personalLaunchContext) ([]entity.IntegrationCredentialPreflightIssue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if len(targets) == 0 {
		return []entity.IntegrationCredentialPreflightIssue{}, nil
	}
	if projectID == "" && launch.assistantScope != "SYSTEM" ||
		launch.assistantScope != "" && launch.assistantScope != "SYSTEM" && launch.assistantScope != "PROJECT" {
		return nil, errs.ErrInvalid
	}
	encoded, err := json.Marshal(targets)
	if err != nil || len(targets) > 256 || len(encoded) > 128<<10 {
		return nil, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryPersonalLaunchRequirements, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "targets": encoded, "system_assistant": launch.assistantScope == "SYSTEM"})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	requirements := make([]personalLaunchRequirement, 0)
	refs := make([]string, 0)
	seen := make(map[[2]string]int)
	for rows.Next() {
		var item personalLaunchRequirement
		if rows.Scan(&item.connectionRef, &item.key, &item.version, &item.current) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		if len(requirements) >= 256 {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		key := [2]string{item.connectionRef, item.key}
		if position, ok := seen[key]; ok {
			if requirements[position].version != item.version {
				rows.Close()
				return nil, errs.ErrUnavailable
			}
			requirements[position].current = requirements[position].current && item.current
			continue
		}
		seen[key] = len(requirements)
		requirements = append(requirements, item)
		if len(refs) == 0 || refs[len(refs)-1] != item.connectionRef {
			refs = append(refs, item.connectionRef)
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	if len(requirements) == 0 {
		return []entity.IntegrationCredentialPreflightIssue{}, nil
	}
	if launch.sessionRef != "" {
		var owner string
		err := tx.QueryRow(ctx, queryPersonalLaunchSessionOwner, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "session_ref": launch.sessionRef, "target_type": launch.targetType, "target_ref": launch.targetRef}).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrForbidden
		}
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		if owner != current.actorID {
			return nil, errs.ErrForbidden
		}
	}
	arguments := pgx.StrictNamedArgs{"organization_id": current.organizationID, "subject_id": current.actorID, "connection_refs": refs}
	rows, err = tx.Query(ctx, queryPersonalLaunchConnections, arguments)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	connections := make(map[string]personalLaunchConnection, len(refs))
	for rows.Next() {
		var item personalLaunchConnection
		if rows.Scan(&item.ref, &item.name, &item.version, &item.policy, &item.definitionKey, &item.definitionVersion, &item.definitionDigest, &item.configuration, &item.ready, &item.visible, &item.usable, &item.subjectKind) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		connections[item.ref] = item
	}
	rows.Close()
	if rows.Err() != nil || len(connections) != len(refs) {
		return nil, errs.ErrUnavailable
	}
	for _, item := range requirements {
		if value := connections[item.connectionRef]; value.version != item.version || value.policy != entity.IntegrationCredentialInitiatorRequired {
			return nil, errs.ErrConflict
		}
	}
	rows, err = tx.Query(ctx, queryPersonalLaunchBindings, arguments)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	bindings := make(map[string]*entity.PersonalIntegrationCredential, len(refs))
	for rows.Next() {
		var row personalCredentialRow
		if rows.Scan(row.targets()...) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		binding, decodeErr := row.decode()
		if decodeErr != nil || binding.OrganizationID != current.organizationID || binding.SubjectID != current.actorID || binding.CredentialPolicy != entity.IntegrationCredentialInitiatorRequired {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		bindings[binding.ConnectionRef] = &binding
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	issues := make([]entity.IntegrationCredentialPreflightIssue, 0)
	valid := make([]entity.IntegrationCredentialRequirement, 0, len(requirements))
	configDigests := make(map[string]string, len(refs))
	definitionDigests := make(map[string]string, len(refs))
	credentialRequired := make(map[string]bool, len(refs))
	knownCapabilities := make(map[[2]string]bool, len(requirements))
	definitionKeys := make(map[string]string, len(refs))
	resourceProfiles := make(map[[2]string]string, len(requirements))
	for _, ref := range refs {
		connection := connections[ref]
		if !connection.ready || !connection.visible || !connection.usable {
			continue
		}
		definition, definitionErr := repository.integrationPackage(ctx, tx, current.organizationID, ref, connection.definitionKey, connection.definitionVersion, connection.definitionDigest)
		if definitionErr != nil {
			if errors.Is(definitionErr, errs.ErrUnavailable) {
				return nil, definitionErr
			}
			continue
		}
		var configuration map[string]any
		if json.Unmarshal(connection.configuration, &configuration) != nil {
			return nil, errs.ErrUnavailable
		}
		strings, ok := integrationStringConfiguration(configuration)
		if !ok || definition.ValidateConfiguration(strings) != nil {
			continue
		}
		raw, encodeErr := json.Marshal(configuration)
		if encodeErr != nil {
			return nil, errs.ErrUnavailable
		}
		sum := sha256.Sum256(raw)
		configDigests[ref] = hex.EncodeToString(sum[:])
		definitionDigests[ref] = definition.Digest
		definitionKeys[ref] = definition.Metadata.Key
		credentialRequired[ref] = definition.RequiresConnectionCredential()
		for _, item := range requirements {
			if item.connectionRef == ref {
				capability, ok := definition.Capability(item.key)
				knownCapabilities[[2]string{ref, item.key}] = ok && capability.Operation != "mattermost.inbound" && capability.Operation != "mattermost.gate_decisions"
				if ok {
					resourceProfiles[[2]string{ref, item.key}] = integrationpackage.CredentialResourcePermissionProfile(definition, capability)
				}
			}
		}
	}
	kind := "USER"
	if launch.source == "SCHEDULE" || launch.source == "INTEGRATION" {
		kind = "SERVICE"
	}
	for _, item := range requirements {
		connection := connections[item.connectionRef]
		if !item.current || !connection.ready || !connection.visible || !connection.usable || configDigests[item.connectionRef] == "" || !knownCapabilities[[2]string{item.connectionRef, item.key}] || !credentialRequired[item.connectionRef] {
			issues = append(issues, entity.IntegrationCredentialPreflightIssue{ConnectionRef: item.connectionRef, CapabilityKey: item.key, Problem: entity.IntegrationCredentialConnectionUnavailable})
			continue
		}
		if connection.subjectKind != "USER" {
			kind = "SERVICE"
		}
		valid = append(valid, entity.IntegrationCredentialRequirement{ConnectionRef: item.connectionRef, CapabilityKey: item.key, Policy: entity.IntegrationCredentialInitiatorRequired, CredentialRequired: true, DefinitionDigest: definitionDigests[item.connectionRef], ConfigurationDigest: configDigests[item.connectionRef],
			DefinitionKey: definitionKeys[item.connectionRef], ResourcePermissionProfile: resourceProfiles[[2]string{item.connectionRef, item.key}]})
	}
	credentialIssues, err := platformservice.PreflightIntegrationCredentials(valid, entity.IntegrationCredentialInitiator{OrganizationID: current.organizationID, SubjectID: current.actorID, SubjectKind: kind}, nil, bindings, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	issues = append(issues, credentialIssues...)
	for index := range issues {
		if connection := connections[issues[index].ConnectionRef]; connection.visible {
			issues[index].ConnectionName = connection.name
		}
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].ConnectionRef != issues[j].ConnectionRef {
			return issues[i].ConnectionRef < issues[j].ConnectionRef
		}
		return issues[i].CapabilityKey < issues[j].CapabilityKey
	})
	return issues, nil
}
