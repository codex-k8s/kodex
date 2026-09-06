package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/workflow_launch_readiness.sql
var queryWorkflowLaunchReadiness string

// Весь page получает один dependency snapshot; отдельного запроса на каждый
// Workflow или Agent нет. Этот read не создаёт Session, Run или provider lease.
func (r *Repository) projectWorkflowLaunchReadiness(ctx context.Context, runner pgx.Tx, s scope, items []*entity.Workflow) error {
	refs := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil {
			refs = append(refs, item.Ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	rows, err := runner.Query(ctx, queryWorkflowLaunchReadiness, pgx.StrictNamedArgs{
		"organization_id": s.organizationID, "actor_id": s.actorID, "authority_project": s.authorityProjectID,
		"workflow_refs": refs, "contract_revision": r.roleImages.RoleRuntimeContractRevision, "contract_digest": r.roleImages.RoleRuntimeContractSHA256,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	values := map[string]entity.WorkflowLaunchReadiness{}
	coordinators := map[string]string{}
	selectedAgents := []string{}
	for rows.Next() {
		var ref, coordinator string
		var value entity.WorkflowLaunchReadiness
		var dependencies json.RawMessage
		if rows.Scan(&ref, &value.WorkflowVersion, &value.RevisionRef, &coordinator, &value.Reason, &dependencies) != nil {
			return errs.ErrUnavailable
		}
		if !slices.Contains([]string{"READY", "PERMISSION_REQUIRED", "UNPUBLISHED", "DEPENDENCY_UNAVAILABLE"}, value.Reason) {
			return errs.ErrUnavailable
		}
		value.AllowedToSubmit = value.Reason == "READY"
		value.OperationalState = "UNKNOWN"
		if !value.AllowedToSubmit {
			value.OperationalState = "BLOCKED"
		}
		raw, err := json.Marshal(struct {
			Actor, Project, Ref string
			Value               entity.WorkflowLaunchReadiness
			Dependencies        json.RawMessage
		}{s.actorRef, s.authorityProjectID, ref, value, dependencies})
		if err != nil {
			return errs.ErrUnavailable
		}
		digest := sha256.Sum256(raw)
		value.ContextDigest = hex.EncodeToString(digest[:])
		values[ref] = value
		coordinators[ref] = coordinator
		if value.AllowedToSubmit && !slices.Contains(selectedAgents, coordinator) {
			selectedAgents = append(selectedAgents, coordinator)
		}
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	rows.Close()
	providers := map[string]providerAdmissionSnapshot{}
	if len(selectedAgents) > 0 {
		providers, err = r.providerAdmissionForAgents(ctx, runner, s, selectedAgents)
		if err != nil {
			return err
		}
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		value, ok := values[item.Ref]
		if !ok {
			return errs.ErrNotFound
		}
		if value.AllowedToSubmit {
			provider, found := providers[coordinators[item.Ref]]
			if !found {
				return errs.ErrUnavailable
			}
			value.AllowedToSubmit = provider.AllowedToSubmit
			value.OperationalState = provider.OperationalState
			if !value.AllowedToSubmit {
				value.Reason = "DEPENDENCY_UNAVAILABLE"
			}
			value.ContextDigest = digestBytes(asJSON(struct{ Workflow, Provider string }{value.ContextDigest, provider.ContextDigest}))
		}
		item.LaunchReadiness = &value
		item.NextActions = slices.DeleteFunc(item.NextActions, func(action string) bool { return action == "LAUNCH" })
		if value.AllowedToSubmit {
			item.NextActions = append(item.NextActions, "LAUNCH")
		}
	}
	return nil
}
