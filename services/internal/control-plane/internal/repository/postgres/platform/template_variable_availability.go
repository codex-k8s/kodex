package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/template_variables_snapshot.sql
var queryTemplateVariablesSnapshot string

const (
	variableAvailable       = "AVAILABLE"
	variableProjectRequired = "PROJECT_CONTEXT_REQUIRED"
	variableAgentRequired   = "AGENT_CONTEXT_REQUIRED"
	variableRuntimeRequired = "RUNTIME_CONTEXT_REQUIRED"
	variableNotMaterialized = "NOT_MATERIALIZED"
)

func (repository *Repository) templateAvailability(ctx context.Context, principal value.Principal, current scope, filter query.Filter) (map[string]bool, bool, error) {
	available := map[string]bool{"organization.ref": true, "organization.name": true, "user.ref": true, "user.name": true}
	if filter.ProjectRef != "" {
		available["project.ref"], available["project.name"] = true, true
	}
	if filter.TemplateContext == nil {
		return available, false, nil
	}
	context := filter.TemplateContext
	for _, ref := range []string{context.AgentRef, context.RuntimeRevisionRef} {
		if len(ref) > 96 || strings.ContainsAny(ref, "\x00\r\n") {
			return nil, false, errs.ErrInvalid
		}
	}
	if context.RuntimeRevisionRef != "" {
		var raw []byte
		err := repository.pool.QueryRow(ctx, queryTemplateVariablesSnapshot, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID,
			"authority_project": current.authorityProjectID, "project_ref": filter.ProjectRef,
			"agent_ref": context.AgentRef, "revision_ref": context.RuntimeRevisionRef, "evaluated_at": time.Now().UTC(),
		}).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, errs.ErrNotFound
		}
		if err != nil {
			return nil, false, errs.ErrUnavailable
		}
		var snapshot entity.PromptMaterializationSnapshot
		if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &snapshot) != nil || snapshot.RunRef == "" || snapshot.SessionRef == "" {
			return nil, false, errs.ErrUnavailable
		}
		return materializedVariableAvailability(snapshot), true, nil
	}
	if context.AgentRef != "" {
		agent, err := repository.GetAgent(ctx, principal, context.AgentRef)
		if err != nil {
			return nil, false, err
		}
		if filter.ProjectRef != "" && agent.ProjectRef != filter.ProjectRef {
			return nil, false, errs.ErrNotFound
		}
		available["agent.ref"], available["agent.name"] = true, true
		available["project.ref"], available["project.name"] = agent.ProjectRef != "", agent.ProjectRef != ""
		view, err := repository.GetAgentRuntimeConfiguration(ctx, principal, context.AgentRef)
		if err != nil {
			return nil, false, err
		}
		if view.EnvironmentBinding.EnvironmentRef != "" && view.Environment.CurrentVersion.Ref != "" {
			for _, name := range []string{"environment.ref", "runtime.environment.ref", "runtime.environment.tools"} {
				available[name] = true
			}
			available["runtime.environment.image.reference"] = view.Environment.CurrentVersion.Image.Reference != ""
			available["runtime.environment.image.digest"] = view.Environment.CurrentVersion.Image.Digest != ""
			available["tools.summary"] = len(view.Environment.CurrentVersion.Tools) != 0
		}
	}
	return available, false, nil
}

func materializedVariableAvailability(snapshot entity.PromptMaterializationSnapshot) map[string]bool {
	available := make(map[string]bool)
	for name, value := range snapshot.Variables {
		available[name] = value != ""
	}
	for name, value := range map[string]string{"project.ref": snapshot.ProjectRef, "run.ref": snapshot.RunRef, "session.ref": snapshot.SessionRef, "target.ref": snapshot.TargetRef} {
		available[name] = value != ""
	}
	for _, variable := range templateVariableCatalog() {
		var current any = snapshot.StructuredVariables
		for _, part := range strings.Split(variable.Name, ".") {
			object, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = object[part]
		}
		if current != nil {
			value, isString := current.(string)
			available[variable.Name] = !isString || value != ""
		}
	}
	return available
}

func variableAvailabilityReason(variable entity.TemplateVariable, available map[string]bool, materialized bool) string {
	if available[variable.Name] {
		return variableAvailable
	}
	if materialized {
		return variableNotMaterialized
	}
	switch variable.Source {
	case "PROJECT":
		if !available["project.ref"] {
			return variableProjectRequired
		}
	case "AGENT":
		return variableAgentRequired
	}
	return variableRuntimeRequired
}
