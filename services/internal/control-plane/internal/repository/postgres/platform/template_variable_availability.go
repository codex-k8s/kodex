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

// queryTemplateVariablesSnapshot embeds safe prompt-snapshot lookup for an exact tenant/revision
// with optional project/agent filters and authority-project restriction. Canonical run.view
// eligibility is checked in SQL before returning snapshot JSON; the value does not authorize a
// future run or external effect.
// queryTemplateVariablesSnapshot встраивает safe prompt-snapshot lookup для exact tenant/revision
// с optional project/agent filters и authority-project restriction. Canonical run.view eligibility
// проверяется SQL до возврата snapshot JSON; значение не разрешает будущий run или external
// effect.
//
//go:embed sql/template_variables_snapshot.sql
var queryTemplateVariablesSnapshot string

const (
	// variableAvailable labels a variable whose current projection contains an available value. The
	// status describes display/materialization context and is not an execution permission.
	// variableAvailable обозначает variable с доступным значением current projection. Status описывает
	// display/materialization context и не является execution permission.
	variableAvailable = "AVAILABLE"
	// variableProjectRequired labels a project-sourced variable missing a project ref before runtime
	// materialization. It does not select or authorize a project for the caller.
	// variableProjectRequired обозначает project-sourced variable без project ref до runtime
	// materialization. Не выбирает и не авторизует project для caller.
	variableProjectRequired = "PROJECT_CONTEXT_REQUIRED"
	// variableAgentRequired labels an unavailable agent-sourced variable in an unmaterialized context.
	// It requests context rather than granting access to an agent.
	// variableAgentRequired обозначает unavailable agent-sourced variable в unmaterialized context.
	// Запрашивает context, не выдавая access к agent.
	variableAgentRequired = "AGENT_CONTEXT_REQUIRED"
	// variableRuntimeRequired is the fallback unavailable-variable reason when project/agent-specific
	// handling does not apply before materialization. It does not prove that starting a runtime is
	// allowed or sufficient.
	// variableRuntimeRequired является fallback unavailable-variable reason до materialization, когда
	// project/agent-specific handling неприменим. Не доказывает, что runtime разрешён или достаточен.
	variableRuntimeRequired = "RUNTIME_CONTEXT_REQUIRED"
	// variableNotMaterialized labels any variable still absent from a stored materialized snapshot,
	// taking precedence over pre-runtime source-context reasons. It does not trigger retries or mutate
	// the snapshot.
	// variableNotMaterialized обозначает любую variable, отсутствующую в stored materialized snapshot,
	// с приоритетом над pre-runtime source-context reasons. Не запускает retry и не меняет snapshot.
	variableNotMaterialized = "NOT_MATERIALIZED"
)

// templateAvailability starts with organization/user and optional project availability; validates
// optional context refs, then reads an eligible stored revision or authorized current
// agent/runtime configuration. Stored malformed/missing run/session data is Unavailable;
// hidden/mismatched context is NotFound. It returns a materialized flag only for the
// stored-revision path; availability is not a readiness/authority grant and these separate reads
// are not one snapshot transaction.
// templateAvailability начинает с organization/user и optional project availability; проверяет
// optional context refs, затем читает eligible stored revision либо authorized current
// agent/runtime configuration. Stored malformed/missing run/session data дают Unavailable;
// hidden/mismatched context — NotFound. Materialized flag true возвращает только stored-revision
// path; availability не является readiness/authority grant, отдельные reads не образуют одну
// snapshot transaction.
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

// materializedVariableAvailability projects nonempty flat values and explicit
// project/run/session/target refs, then traverses catalog paths through map-shaped structured
// data. A present non-string value, including false, zero or an empty collection, counts as
// available; it validates no content, lifecycle or access.
// materializedVariableAvailability проецирует непустые flat values и explicit
// project/run/session/target refs, затем проходит catalog paths через map-shaped structured data.
// Присутствующее non-string value, включая false, zero либо empty collection, считается available;
// content, lifecycle и access не проверяются.
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

// variableAvailabilityReason returns AVAILABLE first, then NOT_MATERIALIZED for missing stored
// values; otherwise selects project-required, agent-required or runtime-required from
// source/current project context. This is a display classifier and does not change a principal,
// snapshot or execution state.
// variableAvailabilityReason сначала возвращает AVAILABLE, затем NOT_MATERIALIZED для missing
// stored values; иначе выбирает project-required, agent-required либо runtime-required по
// source/current project context. Это display classifier, не меняющий principal, snapshot или
// execution state.
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
