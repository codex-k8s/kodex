package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_managed_mcp__execution.sql
var queryRuntimeManagedMCPExecution string

//go:embed sql/runtime_managed_mcp__current_grants.sql
var queryRuntimeManagedMCPCurrentGrants string

// Чтение выполняется внутри той же owner transaction, которая разрешает
// invocation. Никакой caller profile или новый receipt не меняет RuntimeRevision.
func requireCurrentManagedMCPHealth(ctx context.Context, tx pgx.Tx, organizationID, nodeID, connectionRef string) error {
	var raw []byte
	if err := tx.QueryRow(ctx, queryRuntimeManagedMCPExecution,
		pgx.StrictNamedArgs{"organization_id": organizationID, "node_id": nodeID}).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrForbidden
		}
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	input, err := managedMCPInputFromOwnerSnapshot(raw)
	if err != nil || len(input.ManagedMCPProfiles) != 1 {
		return errs.ErrForbidden
	}
	pinned := input.ManagedMCPProfiles[0]
	if pinned.Health.ConnectionRef != connectionRef {
		return errs.ErrForbidden
	}
	var resolve, query runtimecontract.RunnerIntegrationGrant
	for _, grant := range input.IntegrationGrants {
		if grant.Ref == pinned.ResolveGrantRef {
			resolve = grant
		} else if grant.Ref == pinned.QueryGrantRef {
			query = grant
		}
	}
	var matches int
	if err := tx.QueryRow(ctx, queryRuntimeManagedMCPCurrentGrants, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_ref": connectionRef, "connection_version": pinned.Health.ConnectionVersion,
		"agent_ref": input.AgentRef, "definition_version": pinned.Health.DefinitionVersion, "definition_digest": pinned.Health.DefinitionDigest,
		"resolve_ref": resolve.Ref, "resolve_version": resolve.GrantVersion, "query_ref": query.Ref, "query_version": query.GrantVersion,
	}).Scan(&matches); err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if matches != 2 {
		return errs.ErrForbidden
	}
	current, err := runtimeManagedMCPProfiles(ctx, tx, organizationID, pinned.ScopeKind, pinned.ScopeRef, input.AgentRef, input.ProjectRef, input.IntegrationGrants)
	if err != nil {
		return err
	}
	if len(current) != 1 || !sameManagedMCPHealthInputs(pinned.Health, current[0].Health) {
		return errs.ErrConflict
	}
	return nil
}

func managedMCPInputFromOwnerSnapshot(raw []byte) (runtimecontract.RunnerInput, error) {
	if len(raw) == 0 || len(raw) > runtimecontract.MaximumRunnerInputBytes {
		return runtimecontract.RunnerInput{}, errs.ErrInvalid
	}
	var snapshot struct {
		AgentRef            string          `json:"agentRef"`
		ProjectRef          string          `json:"projectRef"`
		AssistantScope      string          `json:"assistantScope"`
		AssistantProfileRef string          `json:"assistantProfileRef"`
		Grants              json.RawMessage `json:"integrationGrants"`
		Profiles            json.RawMessage `json:"managedMCPProfiles"`
	}
	// Извлекаем только публичные MCP зависимости полного owner snapshot.
	if json.Unmarshal(raw, &snapshot) != nil {
		return runtimecontract.RunnerInput{}, errs.ErrInvalid
	}
	var grants []map[string]string
	if json.Unmarshal(snapshot.Grants, &grants) != nil {
		return runtimecontract.RunnerInput{}, errs.ErrInvalid
	}
	input := runtimecontract.RunnerInput{AgentRef: snapshot.AgentRef, ProjectRef: snapshot.ProjectRef,
		AssistantScope: runtimecontract.AssistantScope(snapshot.AssistantScope), AssistantProfileRef: snapshot.AssistantProfileRef,
		IntegrationGrants: runtimeRevisionGrants(grants)}
	decoder := json.NewDecoder(bytes.NewReader(snapshot.Profiles))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input.ManagedMCPProfiles) != nil || decoder.Decode(new(any)) != io.EOF ||
		runtimecontract.ValidateManagedMCPProfiles(input) != nil || len(input.ManagedMCPProfiles) != 1 ||
		input.ManagedMCPProfiles[0].Health.CheckedAt.After(time.Now().UTC()) {
		return runtimecontract.RunnerInput{}, errs.ErrInvalid
	}
	return input, nil
}

// Только новое наблюдение допускает отличие. Config/credential/package/version
// неизменно сравниваются; grant refs/versions проверены отдельно у владельца.
func sameManagedMCPHealthInputs(a, b runtimecontract.ManagedMCPHealthProof) bool {
	a.TestRef, b.TestRef = "", ""
	a.Generation, b.Generation = 0, 0
	a.CheckedAt, b.CheckedAt = time.Time{}, time.Time{}
	return a == b
}
