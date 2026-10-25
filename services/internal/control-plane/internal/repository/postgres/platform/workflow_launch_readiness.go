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

// queryWorkflowLaunchReadiness embeds one page-level workflow read/launch eligibility and runtime
// dependency projection under tenant/actor/project and role-image contract pins. It includes
// published revision, coordinator and ordered dependency JSON for context hashing; it does not
// reserve capacity or issue a launch grant.
// queryWorkflowLaunchReadiness встраивает общую для страницы workflow read/launch eligibility и
// runtime dependency projection внутри tenant/actor/project и role-image contract pins. Включает
// published revision, coordinator и упорядоченный dependency JSON для context hashing; не
// резервирует capacity и не выдаёт launch grant.
//
//go:embed sql/workflow_launch_readiness.sql
var queryWorkflowLaunchReadiness string

// projectWorkflowLaunchReadiness loads one dependency snapshot for all nonnil workflows, accepts
// only closed SQL reasons and hashes actor/project/ref plus readiness/dependencies. It batches
// provider admission for allowed coordinators, blocks invalid published graphs and combines
// provider context digests, then replaces each item readiness and LAUNCH hint in place. Missing
// rows/provider results fail closed. Earlier items can already be changed when a later error
// occurs; callers must discard partial output. This read creates no Session, Run, lease or command
// permission.
// projectWorkflowLaunchReadiness загружает один dependency snapshot всех ненулевых workflows,
// принимает только закрытые SQL reasons и хеширует actor/project/ref с readiness/dependencies.
// Пакетно читает provider admission разрешённых coordinators, блокирует неверный published graph и
// объединяет provider context digests, затем заменяет readiness и LAUNCH hint каждого item на
// месте. Отсутствующие rows/provider results закрыто отклоняются. При поздней ошибке предыдущие
// items могут быть уже изменены; вызывающий обязан отбросить частичный output. Чтение не создаёт
// Session, Run, lease или command permission.
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
		"default_image_digest": r.roleImages.DefaultImageDigest,
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
		if item.Published != nil && !validWorkflowVersion(*item.Published) {
			value.AllowedToSubmit = false
			value.OperationalState = "BLOCKED"
			value.Reason = "DEPENDENCY_UNAVAILABLE"
			value.ContextDigest = digestBytes(asJSON(struct{ Workflow, Configuration string }{value.ContextDigest, workflowVersionInvalidField(*item.Published)}))
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
