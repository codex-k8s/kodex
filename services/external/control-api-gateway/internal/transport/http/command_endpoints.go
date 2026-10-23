package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	// scheduleActionEnable is the exact public schedule action that selects the owner enable command with Enabled=true, not an execution trigger.
	// scheduleActionEnable — точное публичное действие автоматизации, выбирающее enable владельца с Enabled=true, а не запуск выполнения.
	scheduleActionEnable = "ENABLE"
	// scheduleActionPause selects the same owner enable command with Enabled=false; occurrence cancellation policy remains owner-managed.
	// scheduleActionPause выбирает ту же enable-команду владельца с Enabled=false; политика отмены occurrence остаётся у владельца.
	scheduleActionPause = "PAUSE"
	// scheduleActionArchive selects the dedicated archive command rather than pretending archival is a disable or delete.
	// scheduleActionArchive выбирает отдельную команду архивации, не подменяя её отключением или удалением.
	scheduleActionArchive = "ARCHIVE"
	// maximumArtifactBytes caps declared and forwarded artifact data at512MiB; this transport bound neither scans content nor grants download eligibility.
	// maximumArtifactBytes ограничивает объявленные и переданные данные файла512МиБ; транспортный предел не проверяет содержимое и не выдаёт допуск скачивания.
	maximumArtifactBytes = 512 << 20
)

var (
	// errArtifactContentLengthMismatch classifies missing/invalid forwarding configuration or actual bytes disagreeing with the declared/bounded size, using the upload length problem rather than upstream diagnostics.
	// errArtifactContentLengthMismatch обозначает неверные параметры передачи либо несовпадение байтов с объявленной и допустимой длиной и выбирает upload length problem вместо внешней диагностики.
	errArtifactContentLengthMismatch = errors.New("artifact content length mismatch")
	// errArtifactBodyRead replaces arbitrary body-reader failures with a fixed public read-failure classification; already forwarded prefix bytes are not undone.
	// errArtifactBodyRead заменяет произвольный отказ reader фиксированной публичной классификацией; уже переданный префикс байтов не откатывается.
	errArtifactBodyRead = errors.New("artifact request body read failed")
)

// CompleteOnboarding creates mutation context for the dedicated owner command and writes the returned bootstrap state through its specialized projector. Completion eligibility and actor identity remain owner/boundary decisions, not client state.
// CompleteOnboarding создаёт mutation context отдельной команды владельца и выдаёт bootstrap через специальную проекцию. Допуск завершения и личность остаются решениями владельца и границы, а не клиентским состоянием.
func (server *Server) CompleteOnboarding(w http.ResponseWriter, r *http.Request, p generated.CompleteOnboardingParams) {
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	if m == nil {
		return
	}
	response, err := server.control.Command.CompleteOnboarding(r.Context(), &controlplanev1.CompleteOnboardingRequest{Mutation: m})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	server.writeBootstrapState(w, r, response.GetState())
}

// CreateProject decodes name/purpose/language, builds create idempotency context and invokes the dedicated owner create, returning the public project with HTTP201. It supplies no client-selected owner field.
// CreateProject разбирает name/purpose/language, собирает контекст идемпотентного create и вызывает отдельное создание владельца, возвращая проект с HTTP201. Клиентское поле владельца не передаётся.
func (server *Server) CreateProject(w http.ResponseWriter, r *http.Request, p generated.CreateProjectParams) {
	body, ok := decodeJSON[generated.ProjectInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.CreateProject(r.Context(), &controlplanev1.CreateProjectRequest{Mutation: m, Name: body.Name, Purpose: body.Purpose, Language: string(body.Language)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "project", "")
}

// UpdateProject binds route-project context, decodes editable fields and mutation/version pins, then invokes the owner update and public-projects the result without changing ownership locally.
// UpdateProject привязывает проект маршрута, разбирает редактируемые поля и mutation/version pins, затем вызывает update владельца и проецирует результат без локальной смены владения.
func (server *Server) UpdateProject(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.UpdateProjectParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ProjectInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UpdateProject(r.Context(), &controlplanev1.UpdateProjectRequest{Mutation: m, ProjectRef: ref, Name: body.Name, Purpose: body.Purpose, Language: string(body.Language)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "project", "")
}

// TrashProject binds project context and sends exact project plus mutation pins to the dedicated trash transition; cleanup and restoration deadlines belong to owner state.
// TrashProject привязывает проект и передаёт его с mutation pins отдельному переходу в корзину; очистка и срок восстановления принадлежат состоянию владельца.
func (server *Server) TrashProject(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.TrashProjectParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.TrashProject(r.Context(), &controlplanev1.TrashProjectRequest{Mutation: m, ProjectRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "project", "")
}

// RestoreProject delegates exact project restoration with mutation/version pins under the existing request context, then projects public state; no active-project context is fabricated for a trashed resource.
// RestoreProject поручает восстановление точного проекта с mutation/version pins в существующем контексте, затем проецирует состояние; активный проектный контекст для корзины не выдумывается.
func (server *Server) RestoreProject(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.RestoreProjectParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RestoreProject(r.Context(), &controlplanev1.RestoreProjectRequest{Mutation: m, ProjectRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "project", "")
}

// PurgeProject requests the dedicated owner purge transition with exact project/version/idempotency and returns HTTP202. Acceptance does not mean the asynchronous irreversible cleanup has finished.
// PurgeProject запрашивает отдельный purge владельца с точными проектом, версией и идемпотентностью и выдаёт HTTP202. Принятие не означает завершённую асинхронную безвозвратную очистку.
func (server *Server) PurgeProject(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.PurgeProjectParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PurgeProject(r.Context(), &controlplanev1.PurgeProjectRequest{Mutation: m, ProjectRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "project", "")
}

// AddPlatformMembership decodes the selected user and public role, forwards create mutation context to the owner and returns HTTP201; caller privilege is not derived from that requested role.
// AddPlatformMembership разбирает выбранного пользователя и публичную роль, передаёт create mutation владельцу и выдаёт HTTP201; полномочия вызывающего из запрошенной роли не выводятся.
func (server *Server) AddPlatformMembership(w http.ResponseWriter, r *http.Request, p generated.AddPlatformMembershipParams) {
	body, ok := decodeJSON[generated.PlatformMembershipCreateInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.AddPlatformMembership(r.Context(), &controlplanev1.AddPlatformMembershipRequest{Mutation: m, UserRef: body.UserRef, Role: platformRole(string(body.PlatformRole))})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "membership", "")
}

// ChangePlatformMembership sends exact membership, requested role/active flag and mutation pins to owner policy, returning the public result rather than applying IdP membership directly.
// ChangePlatformMembership передаёт точное членство, роль, active и mutation pins политике владельца, возвращая публичный результат вместо прямого изменения членства IdP.
func (server *Server) ChangePlatformMembership(w http.ResponseWriter, r *http.Request, membershipRef generated.MembershipRef, p generated.ChangePlatformMembershipParams) {
	body, ok := decodeJSON[generated.PlatformMembershipChangeInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangePlatformMembership(r.Context(), &controlplanev1.ChangePlatformMembershipRequest{Mutation: m, MembershipRef: membershipRef, Role: platformRole(string(body.PlatformRole)), Active: body.Active})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}

// RemovePlatformMembership invokes the owner removal with exact membership and mutation/version context; access revocation and session effects remain owned by the domain transaction.
// RemovePlatformMembership вызывает удаление владельца с точным членством и mutation/version context; отзыв доступа и эффекты сессий принадлежат доменной транзакции.
func (server *Server) RemovePlatformMembership(w http.ResponseWriter, r *http.Request, membershipRef generated.MembershipRef, p generated.RemovePlatformMembershipParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RemovePlatformMembership(r.Context(), &controlplanev1.RemovePlatformMembershipRequest{Mutation: m, MembershipRef: membershipRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}

// AddProjectMembership binds route-project context and maps requested permission names before the owner create. The mapper may omit unknown names; only the owner decides whether the actor may assign the resulting permissions.
// AddProjectMembership привязывает проект маршрута и преобразует имена разрешений до create владельца. Mapper может пропускать неизвестные имена; только владелец решает, может ли actor назначить итоговые права.
func (server *Server) AddProjectMembership(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.AddProjectMembershipParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ProjectMembershipCreateInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.AddProjectMembership(r.Context(), &controlplanev1.AddProjectMembershipRequest{Mutation: m, ProjectRef: ref, UserRef: body.UserRef, Permissions: permissions(body.Permissions)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "membership", "")
}

// ChangeProjectMembership binds project context and forwards exact project/membership, permissions, active flag and mutation pins to the dedicated owner command without treating request permissions as authority.
// ChangeProjectMembership привязывает проект и передаёт точные project/membership, permissions, active и mutation pins отдельной команде владельца без принятия запрошенных прав за полномочия.
func (server *Server) ChangeProjectMembership(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, membershipRef generated.MembershipRef, p generated.ChangeProjectMembershipParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ProjectMembershipChangeInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangeProjectMembership(r.Context(), &controlplanev1.ChangeProjectMembershipRequest{Mutation: m, ProjectRef: projectRef, MembershipRef: membershipRef, Permissions: permissions(body.Permissions), Active: body.Active})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}

// RemoveProjectMembership binds the route project and delegates exact membership removal with mutation pins; it does not directly revoke credentials or update another service's storage.
// RemoveProjectMembership привязывает проект маршрута и поручает удаление точного членства с mutation pins; credentials напрямую не отзывает и хранилище другого сервиса не меняет.
func (server *Server) RemoveProjectMembership(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, membershipRef generated.MembershipRef, p generated.RemoveProjectMembershipParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RemoveProjectMembership(r.Context(), &controlplanev1.RemoveProjectMembershipRequest{Mutation: m, ProjectRef: projectRef, MembershipRef: membershipRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}

// CreateAgent binds project context and forwards editable metadata, optional role/runtime references and initial instructions to owner creation, returning HTTP201 without starting an agent.
// CreateAgent привязывает проект и передаёт metadata, необязательные role/runtime и начальные инструкции созданию владельца, выдавая HTTP201 без запуска сотрудника.
func (server *Server) CreateAgent(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateAgentParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.AgentInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.CreateAgent(r.Context(), &controlplanev1.CreateAgentRequest{Mutation: m, ProjectRef: projectRef, Name: body.Name, Purpose: body.Purpose, RoleDescription: body.RoleDescription, RoleDefinitionRef: stringValue(body.RoleDefinitionRef), RuntimeRef: stringValue(body.RuntimeRef), InitialInstructions: stringValue(body.InitialInstructions)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "agent", "")
}

// UpdateAgent decodes editable metadata and role/runtime selectors with mutation pins for the dedicated owner update; it does not republish initial instructions or grant runtime capabilities itself.
// UpdateAgent разбирает metadata и role/runtime selectors с mutation pins отдельного update владельца; начальные инструкции заново не публикует и runtime capabilities сам не выдаёт.
func (server *Server) UpdateAgent(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.UpdateAgentParams) {
	body, ok := decodeJSON[generated.AgentInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UpdateAgent(r.Context(), &controlplanev1.UpdateAgentRequest{Mutation: m, AgentRef: ref, Name: body.Name, Purpose: body.Purpose, RoleDescription: body.RoleDescription, RoleDefinitionRef: stringValue(body.RoleDefinitionRef), RuntimeRef: stringValue(body.RuntimeRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

// CommandAgent routes only the explicit enable/disable/archive/capability/integration actions to their specialized owner RPCs under one mutation context. Unknown actions fail before RPC; request action/refs do not bypass owner permission policy.
// CommandAgent направляет только явные enable/disable/archive/capability/integration действия в специальные RPC владельца с одним mutation context. Неизвестные действия отклоняются до RPC; action и ссылки политику прав не обходят.
func (server *Server) CommandAgent(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.CommandAgentParams) {
	body, ok := decodeJSON[generated.AgentCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch body.Action {
	case generated.AgentCommandActionENABLE, generated.AgentCommandActionDISABLE:
		response, err = server.control.Command.SetAgentEnabled(r.Context(), &controlplanev1.SetAgentEnabledRequest{Mutation: m, AgentRef: ref, Enabled: body.Action == generated.AgentCommandActionENABLE})
	case generated.AgentCommandActionARCHIVE:
		response, err = server.control.Command.ArchiveAgent(r.Context(), &controlplanev1.ArchiveAgentRequest{Mutation: m, AgentRef: ref})
	case generated.AgentCommandActionGRANTCAPABILITY, generated.AgentCommandActionREVOKECAPABILITY:
		response, err = server.control.Command.ChangeAgentCapability(r.Context(), &controlplanev1.ChangeAgentCapabilityRequest{Mutation: m, AgentRef: ref, CapabilityKey: stringValue(body.CapabilityKey), Enabled: body.Action == generated.AgentCommandActionGRANTCAPABILITY})
	case generated.AgentCommandActionGRANTINTEGRATION, generated.AgentCommandActionREVOKEINTEGRATION:
		response, err = server.control.Command.ChangeAgentIntegrationGrant(r.Context(), &controlplanev1.ChangeAgentIntegrationGrantRequest{Mutation: m, AgentRef: ref, GrantRef: stringValue(body.GrantRef), Enabled: body.Action == generated.AgentCommandActionGRANTINTEGRATION})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

// CreateInstructionDraft decodes content and mutation/version pins, invokes the owner draft command and returns HTTP201 without publishing or applying the text to a running turn.
// CreateInstructionDraft разбирает content и mutation/version pins, вызывает черновик владельца и выдаёт HTTP201 без публикации и применения текста к действующему ходу.
func (server *Server) CreateInstructionDraft(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.CreateInstructionDraftParams) {
	body, ok := decodeJSON[generated.CreateInstructionDraftJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.CreateInstructionDraft(r.Context(), &controlplanev1.CreateInstructionDraftRequest{Mutation: m, AgentRef: ref, Content: body.Content})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "agent", "")
}

// CommandAgentInstructions keeps validate/publish/rollback as distinct owner paths: validate/rollback reject plan-selection fields, while publish delegates to impact-aware handling. Unknown actions fail closed; mutation pins do not replace owner authorization.
// CommandAgentInstructions сохраняет validate/publish/rollback отдельными путями: validate и rollback запрещают plan-selection поля, publish передаётся impact-aware обработке. Неизвестное действие отклоняется; mutation pins авторизацию не заменяют.
func (server *Server) CommandAgentInstructions(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.CommandAgentInstructionsParams) {
	body, ok := decodeJSON[generated.InstructionCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch body.Action {
	case generated.InstructionCommandActionVALIDATE:
		if body.PlanRef != nil || body.SelectedItemRefs != nil {
			writeLocalProblem(w, 400, "INVALID_REQUEST", false)
			return
		}
		response, err = server.control.Command.ValidateInstructionDraft(r.Context(), &controlplanev1.ValidateInstructionDraftRequest{Mutation: m, AgentRef: ref})
	case generated.InstructionCommandActionPUBLISH:
		server.publishInstructionsWithImpact(w, r, ref, m, body)
		return
	case generated.InstructionCommandActionROLLBACK:
		if body.PlanRef != nil || body.SelectedItemRefs != nil {
			writeLocalProblem(w, 400, "INVALID_REQUEST", false)
			return
		}
		response, err = server.control.Command.RollbackInstructions(r.Context(), &controlplanev1.RollbackInstructionsRequest{Mutation: m, AgentRef: ref, PublishedInstructionRef: stringValue(body.PublishedInstructionRef)})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

// workflowDraft maps optional workflow input/steps into a new protobuf draft, copying input options but retaining RequiredCapabilityKeys slice references. Numeric casts and many semantic constraints are unchecked here and belong to validation; it does not publish a version.
// workflowDraft преобразует необязательные вход и шаги в новый protobuf-черновик, копируя input options, но сохраняя ссылки slice RequiredCapabilityKeys. Числовые casts и многие семантические ограничения здесь не проверяются; версия не публикуется.
func workflowDraft(body generated.WorkflowInput) *controlplanev1.WorkflowVersion {
	draft := &controlplanev1.WorkflowVersion{Name: body.Name, Purpose: body.Purpose, CoordinatorAgentRef: body.CoordinatorAgentRef}
	if body.InputFields != nil {
		for _, field := range *body.InputFields {
			draft.InputFields = append(draft.InputFields, &controlplanev1.WorkflowInputField{Key: stringValue(field.Key), Label: field.Label, Description: field.Description, ValueType: string(field.ValueType), Required: field.Required, Options: append([]string(nil), field.Options...)})
		}
	}
	if body.MaxConcurrency != nil {
		draft.MaxConcurrency = int32(*body.MaxConcurrency)
	}
	if body.TimeoutSeconds != nil {
		draft.TimeoutSeconds = int32(*body.TimeoutSeconds)
	}
	if body.CompletionCriteria != nil {
		draft.CompletionCriteria = *body.CompletionCriteria
	}
	if body.Steps != nil {
		for _, step := range *body.Steps {
			current := &controlplanev1.WorkflowStep{Position: int32(step.Position), Name: step.Name, Purpose: step.Purpose, AgentRef: step.AgentRef, Parallel: step.Parallel, ParallelGroup: int32(step.ParallelGroup), TimeoutSeconds: int32(step.TimeoutSeconds), ExpectedResult: step.ExpectedResult, HumanGate: step.HumanGate, RequiredCapabilityKeys: step.RequiredCapabilityKeys}
			for _, decision := range step.GateDecisions {
				current.GateDecisions = append(current.GateDecisions, gateDecision(string(decision)))
			}
			draft.Steps = append(draft.Steps, current)
		}
	}
	return draft
}

// CreateWorkflow binds project context and creates owner workflow metadata, attaching a mapped draft only when a nonempty step list is present. An input-fields-only body does not create Draft here; publication is a separate command.
// CreateWorkflow привязывает проект и создаёт metadata процесса владельца, прикладывая draft только при непустых шагах. Одни input fields Draft здесь не создают; публикация выполняется отдельно.
func (server *Server) CreateWorkflow(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateWorkflowParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.WorkflowInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	request := &controlplanev1.CreateWorkflowRequest{Mutation: m, ProjectRef: projectRef, Name: body.Name, Purpose: body.Purpose, CoordinatorAgentRef: body.CoordinatorAgentRef}
	if body.Steps != nil && len(*body.Steps) > 0 {
		request.Draft = workflowDraft(body)
	}
	response, err := server.control.Command.CreateWorkflow(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "workflow", "")
}

// UpdateWorkflowDraft sends the full mapped draft and mutation/version context to the dedicated owner update, returning public workflow state without activation or execution.
// UpdateWorkflowDraft передаёт полный преобразованный draft и mutation/version context отдельному update владельца, возвращая состояние без активации и выполнения.
func (server *Server) UpdateWorkflowDraft(w http.ResponseWriter, r *http.Request, ref generated.WorkflowRef, p generated.UpdateWorkflowDraftParams) {
	body, ok := decodeJSON[generated.WorkflowInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UpdateWorkflowDraft(r.Context(), &controlplanev1.UpdateWorkflowDraftRequest{Mutation: m, WorkflowRef: ref, Draft: workflowDraft(body)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "workflow", "")
}

// CommandWorkflow permits only validate, publish and archive, preserving distinct typed owner RPCs and mutation pins. Unknown actions return INVALID_REQUEST rather than a generic transition.
// CommandWorkflow допускает только validate, publish и archive с отдельными типизированными RPC и mutation pins. Неизвестное действие даёт INVALID_REQUEST, а не универсальный переход.
func (server *Server) CommandWorkflow(w http.ResponseWriter, r *http.Request, ref generated.WorkflowRef, p generated.CommandWorkflowParams) {
	body, ok := decodeJSON[generated.WorkflowCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch body.Action {
	case generated.WorkflowCommandActionVALIDATE:
		response, err = server.control.Command.ValidateWorkflowDraft(r.Context(), &controlplanev1.ValidateWorkflowDraftRequest{Mutation: m, WorkflowRef: ref})
	case generated.WorkflowCommandActionPUBLISH:
		response, err = server.control.Command.PublishWorkflowDraft(r.Context(), &controlplanev1.PublishWorkflowDraftRequest{Mutation: m, WorkflowRef: ref})
	case generated.WorkflowCommandActionARCHIVE:
		response, err = server.control.Command.ArchiveWorkflow(r.Context(), &controlplanev1.ArchiveWorkflowRequest{Mutation: m, WorkflowRef: ref})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "workflow", "")
}

// CreateRun decodes launch input, maps target/input and sends project/task/session/attachment selectors to the dedicated owner LaunchRun with fixed CONTROL_CENTER source. Struct conversion errors are ignored as before; ownership, queue/admission and graph creation remain owner-managed.
// CreateRun разбирает вход запуска, преобразует target/input и передаёт project/task/session/attachment отдельному LaunchRun с фиксированным CONTROL_CENTER. Ошибка Struct conversion по-прежнему игнорируется; владение, очередь, допуск и граф принадлежат владельцу.
func (server *Server) CreateRun(w http.ResponseWriter, r *http.Request, p generated.CreateRunParams) {
	body, ok := decodeJSON[generated.RunInput](w, r)
	if !ok {
		return
	}
	input, _ := structpb.NewStruct(valueOrEmpty(body.Input))
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.LaunchRun(r.Context(), &controlplanev1.LaunchRunRequest{Mutation: m, ProjectRef: body.ProjectRef, Target: targetProto(string(body.TargetType), body.TargetRef), Title: stringValue(body.Title), Task: body.Task, Input: input, AttachmentSetRef: stringValue(body.AttachmentSetRef), SessionRef: stringValue(body.SessionRef), Source: controlplanev1.RunSource_RUN_SOURCE_CONTROL_CENTER})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "", "")
}

// valueOrEmpty returns a fresh empty map for an absent optional map, otherwise the original map without copying or validating its contents.
// valueOrEmpty возвращает новую пустую map при отсутствии необязательного значения, иначе исходную map без копирования и проверки содержимого.
func valueOrEmpty(value *map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return *value
}

// sliceOrEmpty preserves absence as nil and otherwise returns the original slice, without allocating, deduplicating or validating elements.
// sliceOrEmpty сохраняет отсутствие как nil, иначе возвращая исходный slice без выделения памяти, удаления дубликатов и проверки элементов.
func sliceOrEmpty(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

// CommandRun dispatches only CANCEL or RETRY to their dedicated owner commands with exact run, reason and mutation pins. It does not kill pods, reuse old attempt grants or implement terminal graph cleanup locally.
// CommandRun направляет только CANCEL и RETRY отдельным командам владельца с точными run, reason и mutation pins. Поды не убивает, старые grants попыток не использует и terminal cleanup графа локально не реализует.
func (server *Server) CommandRun(w http.ResponseWriter, r *http.Request, ref generated.RunRef, p generated.CommandRunParams) {
	body, ok := decodeJSON[generated.RunCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	if body.Action == generated.RunCommandActionCANCEL {
		response, err = server.control.Command.CancelRun(r.Context(), &controlplanev1.CancelRunRequest{Mutation: m, RunRef: ref, Reason: stringValue(body.Reason)})
	} else if body.Action == generated.RunCommandActionRETRY {
		response, err = server.control.Command.RetryRun(r.Context(), &controlplanev1.RetryRunRequest{Mutation: m, RunRef: ref, Reason: stringValue(body.Reason)})
	} else {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

// AddSessionTurn delegates a user turn with exact session/run/node/task/attachment selectors and create idempotency context to the owner, returning HTTP201 without directly resuming a provider session.
// AddSessionTurn поручает пользовательский ход с точными session/run/node/task/attachment и идемпотентным create владельцу, выдавая HTTP201 без прямого возобновления сессии провайдера.
func (server *Server) AddSessionTurn(w http.ResponseWriter, r *http.Request, sessionRef generated.SessionRef, p generated.AddSessionTurnParams) {
	body, ok := decodeJSON[generated.TurnInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.AddSessionTurn(r.Context(), &controlplanev1.AddSessionTurnRequest{Mutation: m, SessionRef: sessionRef, RunRef: body.RunRef, NodeRef: stringValue(body.NodeRef), Task: body.Task, AttachmentSetRef: stringValue(body.AttachmentSetRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "", "")
}

// ResolveOwnerGate forwards the explicit decision, comment, attachment and mutation/version pins to the owner, then requires an exact nonnil gate reply. The handler never manufactures a decision from chat text or executes the external effect itself.
// ResolveOwnerGate передаёт явное решение, комментарий, вложение и mutation/version pins владельцу, затем требует точный ненулевой gate-ответ. Решение из текста чата не создаётся, внешний эффект сам handler не выполняет.
func (server *Server) ResolveOwnerGate(w http.ResponseWriter, r *http.Request, ref generated.GateRef, p generated.ResolveOwnerGateParams) {
	body, ok := decodeJSON[generated.GateResolution](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ResolveOwnerGate(r.Context(), &controlplanev1.ResolveOwnerGateRequest{Mutation: m, GateRef: ref, Decision: gateDecision(string(body.Decision)), Comment: stringValue(body.Comment), AttachmentSetRef: stringValue(body.AttachmentSetRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetGate() == nil || response.Gate.Ref != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

// scheduleInput maps the run target and two JSON maps to protobuf, preserving the existing ignored Struct-conversion errors. It does not validate cron, target eligibility or materialization semantics.
// scheduleInput преобразует цель запуска и две JSON-map в protobuf с прежним игнорированием ошибок Struct conversion. Cron, допуск цели и семантику материализации не проверяет.
func scheduleInput(body generated.ScheduleInput) (*controlplanev1.RunTarget, *structpb.Struct, *structpb.Struct) {
	input, _ := structpb.NewStruct(body.Input)
	promptInputs, _ := structpb.NewStruct(body.PromptInputs)
	return targetProto(string(body.TargetType), body.TargetRef), input, promptInputs
}

// CreateSchedule binds route-project context, decodes schedule fields and forwards target/input/prompt maps plus timing, session, notification and DST/misfire/overlap policy to owner creation. HTTP201 reports metadata creation, not a scheduled execution.
// CreateSchedule привязывает проект маршрута, разбирает поля и передаёт target/input/prompt, время, session, notification и DST/misfire/overlap политику созданию владельца. HTTP201 сообщает создание metadata, а не выполнение по расписанию.
func (server *Server) CreateSchedule(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateScheduleParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ScheduleInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	target, input, promptInputs := scheduleInput(body)
	response, err := server.control.Command.CreateSchedule(r.Context(), &controlplanev1.CreateScheduleRequest{Mutation: m, ProjectRef: projectRef, Name: body.Name, Target: target, Preset: string(body.Preset), CronExpression: stringValue(body.CronExpression), TimeOfDay: stringValue(body.TimeOfDay), DayOfWeek: stringValue(body.DayOfWeek), Timezone: body.Timezone, Input: input, SessionPolicy: string(body.SessionPolicy), NotificationPolicy: string(body.NotificationPolicy), DstGapPolicy: string(body.DstGapPolicy), DstFoldPolicy: string(body.DstFoldPolicy), MisfirePolicy: string(body.MisfirePolicy), OverlapPolicy: string(body.OverlapPolicy), AutomationText: body.AutomationText, PromptInputs: promptInputs})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "schedule", "")
}

// UpdateSchedule maps the full selected schedule with mutation/version pins to the owner update, preserving explicit timing/materialization policy fields without locally acquiring or rescheduling occurrences.
// UpdateSchedule передаёт полное выбранное расписание с mutation/version pins update владельца, сохраняя явные поля времени и материализации без локальных claims и перепланирования occurrence.
func (server *Server) UpdateSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.UpdateScheduleParams) {
	body, ok := decodeJSON[generated.ScheduleInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	target, input, promptInputs := scheduleInput(body)
	response, err := server.control.Command.UpdateSchedule(r.Context(), &controlplanev1.UpdateScheduleRequest{Mutation: m, ScheduleRef: ref, Name: body.Name, Target: target, Preset: string(body.Preset), CronExpression: stringValue(body.CronExpression), TimeOfDay: stringValue(body.TimeOfDay), DayOfWeek: stringValue(body.DayOfWeek), Timezone: body.Timezone, Input: input, SessionPolicy: string(body.SessionPolicy), NotificationPolicy: string(body.NotificationPolicy), DstGapPolicy: string(body.DstGapPolicy), DstFoldPolicy: string(body.DstFoldPolicy), MisfirePolicy: string(body.MisfirePolicy), OverlapPolicy: string(body.OverlapPolicy), AutomationText: body.AutomationText, PromptInputs: promptInputs})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "schedule", "")
}

// CommandSchedule handles enable/pause through the dedicated enabled flag and archive through a separate owner RPC, rejecting unknown actions. Active occurrence and cancellation semantics stay in owner state rather than this switch.
// CommandSchedule обрабатывает enable/pause отдельным enabled-флагом, archive — другим RPC владельца, отклоняя неизвестные действия. Семантика действующих occurrence и отмены остаётся у владельца, а не в switch.
func (server *Server) CommandSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.CommandScheduleParams) {
	body, ok := decodeJSON[generated.ScheduleCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch string(body.Action) {
	case scheduleActionEnable, scheduleActionPause:
		response, err = server.control.Command.SetScheduleEnabled(r.Context(), &controlplanev1.SetScheduleEnabledRequest{Mutation: m, ScheduleRef: ref, Enabled: string(body.Action) == scheduleActionEnable})
	case scheduleActionArchive:
		response, err = server.control.Command.ArchiveSchedule(r.Context(), &controlplanev1.ArchiveScheduleRequest{Mutation: m, ScheduleRef: ref})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "schedule", "")
}

// CreateIntegrationConnection copies optional public configuration into a new map, converts it with the existing ignored Struct error and creates the owner connection with idempotency context. No credential or remote-tool invocation is performed here.
// CreateIntegrationConnection копирует необязательную публичную конфигурацию в новую map, преобразует её с прежним игнорированием Struct-ошибки и создаёт подключение владельца с идемпотентностью. Credentials и удалённые инструменты здесь не используются.
func (server *Server) CreateIntegrationConnection(w http.ResponseWriter, r *http.Request, p generated.CreateIntegrationConnectionParams) {
	body, ok := decodeJSON[generated.IntegrationConnectionInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	config := map[string]interface{}{}
	if body.PublicConfiguration != nil {
		for key, value := range *body.PublicConfiguration {
			config[key] = value
		}
	}
	public, _ := structpb.NewStruct(config)
	response, err := server.control.Command.CreateIntegrationConnection(r.Context(), &controlplanev1.CreateIntegrationConnectionRequest{
		Mutation: m, DefinitionKey: body.DefinitionKey, Name: body.Name,
		PublicConfiguration: public,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "connection", "")
}

// ConfigureIntegrationConnectionCredential converts the optional decoded value to mutable bytes, clears the decoded field and defers clearing those bytes around the owner command with mutation pins. This best effort does not guarantee erasure of immutable string/decoder copies.
// ConfigureIntegrationConnectionCredential преобразует необязательное decoded value в изменяемые байты, очищает поле и откладывает очистку байтов вокруг команды владельца с mutation pins. Это не гарантирует стирание неизменяемых string/decoder copies.
func (server *Server) ConfigureIntegrationConnectionCredential(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.ConfigureIntegrationConnectionCredentialParams) {
	body, ok := decodeJSON[generated.IntegrationCredentialInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	credential := []byte(stringValue(body.Value))
	body.Value = nil
	defer clear(credential)
	response, err := server.control.Command.ConfigureIntegrationConnectionCredential(r.Context(), &controlplanev1.ConfigureIntegrationConnectionCredentialRequest{
		Mutation: m, ConnectionRef: ref, CredentialValue: credential,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}

// CommandIntegrationConnection supports only test and enable/disable, using dedicated owner RPCs under mutation/version context. Unknown actions fail before RPC, and a test response is not a grant to use remote tools.
// CommandIntegrationConnection допускает только test и enable/disable с отдельными RPC владельца и mutation/version context. Неизвестное действие отклоняется до RPC; ответ проверки не выдаёт grant удалённых инструментов.
func (server *Server) CommandIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.CommandIntegrationConnectionParams) {
	body, ok := decodeJSON[generated.IntegrationConnectionCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	if body.Action == generated.IntegrationConnectionCommandActionTEST {
		response, err = server.control.Command.TestIntegrationConnection(r.Context(), &controlplanev1.TestIntegrationConnectionRequest{Mutation: m, ConnectionRef: ref})
	} else if body.Action == generated.IntegrationConnectionCommandActionENABLE || body.Action == generated.IntegrationConnectionCommandActionDISABLE {
		response, err = server.control.Command.SetIntegrationConnectionEnabled(r.Context(), &controlplanev1.SetIntegrationConnectionEnabledRequest{Mutation: m, ConnectionRef: ref, Enabled: body.Action == generated.IntegrationConnectionCommandActionENABLE})
	} else {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}

// ChangeIntegrationGrant copies approval scope paths and maps requested policy/targets into the dedicated owner grant command with mutation pins. Enum lookup alone is not validation, and caller-supplied policy cannot itself authorize execution.
// ChangeIntegrationGrant копирует approval scope paths и преобразует запрошенные policy и targets в отдельную grant-команду владельца с mutation pins. Enum lookup не заменяет проверку, входная policy сама выполнение не разрешает.
func (server *Server) ChangeIntegrationGrant(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.ChangeIntegrationGrantParams) {
	body, ok := decodeJSON[generated.IntegrationGrantInput](w, r)
	if !ok {
		return
	}
	var approvalScopePaths []string
	if body.ApprovalScopePaths != nil {
		approvalScopePaths = append(approvalScopePaths, (*body.ApprovalScopePaths)...)
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangeIntegrationGrant(r.Context(), &controlplanev1.ChangeIntegrationGrantRequest{Mutation: m, ConnectionRef: ref, CapabilityKey: body.CapabilityKey, AgentRef: stringValue(body.AgentRef), WorkflowRef: stringValue(body.WorkflowRef), Enabled: body.Enabled, ApprovalScopePaths: approvalScopePaths,
		ApprovalPolicy: controlplanev1.IntegrationApprovalPolicy(controlplanev1.IntegrationApprovalPolicy_value["INTEGRATION_APPROVAL_POLICY_"+string(body.ApprovalPolicy)])})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}

// ChangeArtifactBinding sends exact artifact/agent/enabled selectors and mutation pins to the owner binding command without granting raw-file access or removing quarantine locally.
// ChangeArtifactBinding передаёт точные artifact/agent/enabled и mutation pins команде привязки владельца без выдачи доступа к байтам и локального снятия карантина.
func (server *Server) ChangeArtifactBinding(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.ChangeArtifactBindingParams) {
	body, ok := decodeJSON[generated.ArtifactBindingInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangeArtifactBinding(r.Context(), &controlplanev1.ChangeArtifactBindingRequest{Mutation: m, ArtifactRef: ref, AgentRef: body.AgentRef, Enabled: body.Enabled})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "artifact", "")
}

// DeleteArtifact forwards exact artifact, impact digest and mutation/version pins to owner deletion, preserving its dependency/impact policy rather than deleting storage directly.
// DeleteArtifact передаёт точный файл, impact digest и mutation/version pins удалению владельца, сохраняя политику зависимостей и влияния вместо прямого удаления хранилища.
func (server *Server) DeleteArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.DeleteArtifactParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteArtifact(r.Context(), &controlplanev1.DeleteArtifactRequest{Mutation: m, ArtifactRef: ref, ImpactDigest: p.XImpactDigest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "artifact", "")
}

// RestoreArtifact delegates exact artifact restoration with mutation/version pins and public-projects the returned metadata without reclassifying scan safety.
// RestoreArtifact поручает восстановление точного файла с mutation/version pins и проецирует metadata без переклассификации безопасности проверки.
func (server *Server) RestoreArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.RestoreArtifactParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RestoreArtifact(r.Context(), &controlplanev1.RestoreArtifactRequest{Mutation: m, ArtifactRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "artifact", "")
}

// PurgeArtifact invokes the dedicated irreversible owner purge with impact and mutation pins, then projects the full returned response; this handler does not remove storage or invent completion state itself.
// PurgeArtifact вызывает отдельный безвозвратный purge владельца с impact и mutation pins и проецирует полный ответ; сам handler хранилище не удаляет и состояние завершения не выдумывает.
func (server *Server) PurgeArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.PurgeArtifactParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PurgeArtifact(r.Context(), &controlplanev1.PurgeArtifactRequest{Mutation: m, ArtifactRef: ref, ImpactDigest: p.XImpactDigest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

// GetArtifactImpact queries owner impact for the selected action and explicitly maps counts, digest, blockers, permission hint and active-run summaries into the public shape. The read neither authorizes purge nor validates every response field itself.
// GetArtifactImpact запрашивает влияние выбранного действия у владельца и явно переносит счётчики, digest, blockers, подсказку допуска и активные запуски в публичную форму. Чтение purge не разрешает и все поля ответа само не проверяет.
func (server *Server) GetArtifactImpact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.GetArtifactImpactParams) {
	action := controlplanev1.ArtifactImpactAction(controlplanev1.ArtifactImpactAction_value["ARTIFACT_IMPACT_ACTION_"+string(p.Action)])
	response, err := server.control.Query.GetArtifactImpact(r.Context(), &controlplanev1.GetArtifactImpactRequest{ArtifactRef: ref, Action: action})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	impact := response.GetImpact()
	activeRuns := make([]generated.ArtifactImpactRun, 0, len(impact.GetActiveRuns()))
	for _, run := range impact.GetActiveRuns() {
		item := generated.ArtifactImpactRun{
			RunRef: run.GetRunRef(), Title: run.GetTitle(),
			State: generated.ArtifactImpactRunState(strings.TrimPrefix(run.GetState().String(), "RUN_STATE_")),
		}
		if run.GetProjectRef() != "" {
			projectRef := generated.OpaqueRef(run.GetProjectRef())
			item.ProjectRef = &projectRef
		}
		activeRuns = append(activeRuns, item)
	}
	writeJSON(w, http.StatusOK, generated.ArtifactImpact{
		ArtifactRef: impact.GetArtifactRef(), ArtifactVersion: impact.GetArtifactVersion(),
		Action:       generated.ArtifactImpactAction(strings.TrimPrefix(impact.GetAction().String(), "ARTIFACT_IMPACT_ACTION_")),
		ImpactDigest: impact.GetImpactDigest(), BindingCount: impact.GetBindingCount(),
		AttachmentCount: impact.GetAttachmentCount(), ActiveRuntimeCount: impact.GetActiveRuntimeCount(),
		ActiveRuns: activeRuns, ActiveRunsTruncated: impact.GetActiveRunsTruncated(),
		Blockers: impact.GetBlockers(), Permitted: impact.GetPermitted(),
	})
}

// UploadArtifact derives route-project context and forwards project/run/name/idempotency selectors to the shared upload path; actual content and owner admission are handled there.
// UploadArtifact выводит контекст проекта маршрута и передаёт project/run/name/idempotency общему пути загрузки; содержимое и допуск владельца обрабатываются там.
func (server *Server) UploadArtifact(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.UploadArtifactParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	server.uploadArtifact(w, r, artifactUploadRequest{
		projectRef:     projectRef,
		runRef:         stringValue(p.RunRef),
		fileName:       p.XFileName,
		idempotencyKey: p.IdempotencyKey,
	})
}

// UploadOrganizationArtifact uses the shared upload path with no project/run selectors, leaving organization identity to verified owner context rather than request fields.
// UploadOrganizationArtifact использует общий путь загрузки без project/run selectors, оставляя организационную identity проверенному контексту владельца, а не входным полям.
func (server *Server) UploadOrganizationArtifact(w http.ResponseWriter, r *http.Request, p generated.UploadOrganizationArtifactParams) {
	server.uploadArtifact(w, r, artifactUploadRequest{
		fileName:       p.XFileName,
		idempotencyKey: p.IdempotencyKey,
	})
}

// artifactUploadRequest carries only endpoint-selected scope/name/idempotency metadata for the shared upload path; an empty project chooses the organization RPC, not a wildcard access grant.
// artifactUploadRequest хранит только выбранные endpoint область, имя и идемпотентность общего пути; пустой проект выбирает организационный RPC, а не wildcard grant.
type artifactUploadRequest struct {
	projectRef     string
	runRef         string
	fileName       string
	idempotencyKey string
}

// artifactUploadClient is the narrow typed metadata/chunk/commit send-and-final-reply port shared by project and organization uploads. It does not provide cancellation, admission or durable receipt guarantees itself.
// artifactUploadClient — узкий типизированный порт metadata/chunk/commit и финального ответа проектной и организационной загрузки. Сам отмену, допуск и durable receipt не гарантирует.
type artifactUploadClient interface {
	// Send submits one typed upload part and reports transport failure; a successful call alone is not owner admission or publication.
	// Send отправляет одну типизированную часть загрузки и сообщает транспортную ошибку; успех сам по себе не означает допуск и публикацию владельца.
	Send(*controlplanev1.UploadArtifactRequest) error
	// CloseAndRecv ends the outbound upload stream and obtains its typed owner reply or error; the consumer still validates/project-shapes that reply.
	// CloseAndRecv завершает исходящий поток загрузки и получает типизированный ответ владельца либо ошибку; потребитель ещё проверяет и проецирует ответ.
	CloseAndRecv() (*controlplanev1.UploadArtifactResponse, error)
}

// uploadArtifact enforces explicit length0..512MiB and mutation context, selects the exact project/organization stream, then sends metadata, counted/hash-checked chunks and commit before HTTP201 projection. Failures preserve earlier transport effects and return without an added abort/CloseSend path here; scan/quarantine/admission remain owner-managed.
// uploadArtifact требует явную длину0..512МиБ и mutation context, выбирает точный проектный либо организационный поток, затем отправляет metadata, подсчитанные и хешированные chunks и commit до HTTP201. Ошибки сохраняют прежние транспортные эффекты и возвращаются без дополнительного abort/CloseSend здесь; проверка, карантин и допуск остаются у владельца.
func (server *Server) uploadArtifact(w http.ResponseWriter, r *http.Request, upload artifactUploadRequest) {
	if r.ContentLength < 0 {
		writeLocalProblem(w, http.StatusLengthRequired, "CONTENT_LENGTH_REQUIRED", false)
		return
	}
	if r.ContentLength > maximumArtifactBytes {
		writeLocalProblem(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", false)
		return
	}
	m, ok := requireMutation(w, upload.idempotencyKey, "")
	if !ok {
		return
	}
	var stream artifactUploadClient
	var err error
	if upload.projectRef == "" {
		var organizationStream controlplanev1.PlatformCommandService_UploadOrganizationArtifactClient
		organizationStream, err = server.control.Command.UploadOrganizationArtifact(r.Context())
		if err == nil {
			stream = organizationArtifactUploadClient{stream: organizationStream}
		}
	} else {
		stream, err = server.control.Command.UploadArtifact(r.Context())
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if err = stream.Send(&controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Metadata{Metadata: &controlplanev1.UploadArtifactMetadata{Mutation: m, ProjectRef: upload.projectRef, RunRef: upload.runRef, FileName: upload.fileName, MediaType: r.Header.Get("Content-Type"), SizeBytes: r.ContentLength}}}); err != nil {
		writeRPCProblem(w, err)
		return
	}
	received, sha256Digest, err := forwardArtifactBody(r.Body, r.ContentLength, func(chunk []byte) error {
		return stream.Send(&controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Chunk{Chunk: chunk}})
	})
	if writeArtifactUploadError(w, err) {
		return
	}
	if err := stream.Send(&controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Commit{Commit: &controlplanev1.UploadArtifactCommit{
		SizeBytes: received,
		Sha256:    sha256Digest,
	}}}); err != nil {
		writeRPCProblem(w, err)
		return
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "artifact", "")
}

// organizationArtifactUploadClient adapts only the differing generated organization stream message types to the common upload port, retaining the original stream context and ownership.
// organizationArtifactUploadClient приспосабливает только различающиеся generated-типы организационного потока к общему порту, сохраняя исходные контекст и владение потоком.
type organizationArtifactUploadClient struct {
	stream controlplanev1.PlatformCommandService_UploadOrganizationArtifactClient
}

// Send converts known metadata/chunk/commit oneofs while preserving their payloads and delegates to the organization stream. An unknown part produces an empty converted message rather than a local rejection; owner validation remains necessary.
// Send преобразует известные metadata/chunk/commit oneof с сохранением payload и передаёт организационному потоку. Неизвестная часть даёт пустое сообщение вместо локального отказа; проверка владельца остаётся обязательной.
func (client organizationArtifactUploadClient) Send(request *controlplanev1.UploadArtifactRequest) error {
	converted := &controlplanev1.UploadOrganizationArtifactRequest{}
	switch part := request.GetPart().(type) {
	case *controlplanev1.UploadArtifactRequest_Metadata:
		converted.Part = &controlplanev1.UploadOrganizationArtifactRequest_Metadata{Metadata: part.Metadata}
	case *controlplanev1.UploadArtifactRequest_Chunk:
		converted.Part = &controlplanev1.UploadOrganizationArtifactRequest_Chunk{Chunk: part.Chunk}
	case *controlplanev1.UploadArtifactRequest_Commit:
		converted.Part = &controlplanev1.UploadOrganizationArtifactRequest_Commit{Commit: part.Commit}
	}
	return client.stream.Send(converted)
}

// CloseAndRecv propagates the organization's terminal error, otherwise wraps only its artifact field in the common typed reply without copying or inventing admission metadata.
// CloseAndRecv передаёт терминальную ошибку организации, иначе оборачивает только artifact в общий типизированный ответ без копирования и выдумывания metadata допуска.
func (client organizationArtifactUploadClient) CloseAndRecv() (*controlplanev1.UploadArtifactResponse, error) {
	response, err := client.stream.CloseAndRecv()
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UploadArtifactResponse{Artifact: response.GetArtifact()}, nil
}

// writeArtifactUploadError preserves upload failure precedence: length mismatch, body-read sentinel, then the existing closed RPC problem mapper. It returns false and performs no write for nil; true means an error response was attempted, not delivered or rolled back. Avatar and artifact streams retain their own commit/close lifecycle.
// writeArtifactUploadError сохраняет порядок отказов загрузки: несовпадение длины, sentinel чтения тела, затем прежний закрытый RPC problem mapper. При nil возвращает false без записи; true означает попытку error-ответа, а не доставку и откат. Commit и закрытие остаются у потоков аватара и файла.
func writeArtifactUploadError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, errArtifactContentLengthMismatch) {
		writeLocalProblem(w, http.StatusBadRequest, "CONTENT_LENGTH_MISMATCH", false)
		return true
	}
	if errors.Is(err, errArtifactBodyRead) {
		writeLocalProblem(w, http.StatusBadRequest, "REQUEST_BODY_READ_FAILED", false)
		return true
	}
	if err != nil {
		writeRPCProblem(w, err)
		return true
	}
	return false
}

// forwardArtifactBody validates reader/callback/declared bounds and forwards independent64KiB-or-smaller copies while counting and hashing exact bytes. It rejects overflow before sending that chunk, returns prior received count on failures, and yields a digest only at exact EOF/size agreement. Reader errors become a fixed sentinel, send errors stay intact; no context, deadline or zero-progress-read bound is added here.
// forwardArtifactBody проверяет reader, callback и пределы длины и передаёт независимые копии до64КиБ с подсчётом и хешированием точных байтов. Превышение отклоняется до отправки chunk, при ошибке возвращается прежний received, digest — только при точном EOF и размере. Reader-ошибка заменяется sentinel, send-ошибка сохраняется; context, deadline и предел пустых чтений здесь не добавляются.
func forwardArtifactBody(reader io.Reader, declaredSize int64, send func([]byte) error) (int64, string, error) {
	if reader == nil || send == nil || declaredSize < 0 || declaredSize > maximumArtifactBytes {
		return 0, "", errArtifactContentLengthMismatch
	}
	buffer := make([]byte, 64<<10)
	bounded := io.LimitReader(reader, maximumArtifactBytes+1)
	digest := sha256.New()
	var received int64
	for {
		count, readErr := bounded.Read(buffer)
		if count > 0 {
			received += int64(count)
			if received > declaredSize || received > maximumArtifactBytes {
				return received, "", errArtifactContentLengthMismatch
			}
			_, _ = digest.Write(buffer[:count])
			if err := send(append([]byte(nil), buffer[:count]...)); err != nil {
				return received, "", err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return received, "", errArtifactBodyRead
		}
	}
	if received != declaredSize {
		return received, "", errArtifactContentLengthMismatch
	}
	return received, hex.EncodeToString(digest.Sum(nil)), nil
}

// DownloadArtifact maps the public preview/download purpose to the dedicated owner stream and delegates bounded HTTP output; selecting PREVIEW does not bypass owner eligibility or content policy.
// DownloadArtifact преобразует публичную цель preview/download в отдельный поток владельца и передаёт ограниченную HTTP-выдачу; PREVIEW допуск и content policy владельца не обходит.
func (server *Server) DownloadArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.DownloadArtifactParams) {
	purpose := controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_UNSPECIFIED
	switch p.Purpose {
	case generated.DownloadArtifactParamsPurposeDOWNLOAD:
		purpose = controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD
	case generated.DownloadArtifactParamsPurposePREVIEW:
		purpose = controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_PREVIEW
	}
	stream, err := server.control.Command.DownloadArtifact(r.Context(), &controlplanev1.DownloadArtifactRequest{ArtifactRef: ref, Purpose: purpose})
	writeArtifactDownloadStream(w, stream, err)
}

// DownloadArtifactRevision pins artifact and revision in the owner stream request and delegates the same bounded download writer without silently selecting the latest revision.
// DownloadArtifactRevision закрепляет файл и ревизию в запросе потока владельца и передаёт тому же ограниченному writer без скрытого выбора последней ревизии.
func (server *Server) DownloadArtifactRevision(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, revisionRef string, p generated.DownloadArtifactRevisionParams) {
	purpose := controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_UNSPECIFIED
	switch p.Purpose {
	case generated.DownloadArtifactRevisionParamsPurposeDOWNLOAD:
		purpose = controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD
	case generated.DownloadArtifactRevisionParamsPurposePREVIEW:
		purpose = controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_PREVIEW
	}
	stream, err := server.control.Command.DownloadArtifactRevision(r.Context(), &controlplanev1.DownloadArtifactRevisionRequest{ArtifactRef: ref, RevisionRef: revisionRef, Purpose: purpose})
	writeArtifactDownloadStream(w, stream, err)
}

// artifactDownloadFrame exposes only typed content bytes and metadata shared by current/revision download messages; framing, authorization and total-length checks belong to the owner stream and writer.
// artifactDownloadFrame выдаёт только типизированные байты и metadata текущего и ревизионного скачивания; framing, авторизация и проверка общей длины принадлежат потоку владельца и writer.
type artifactDownloadFrame interface {
	// GetData exposes the current frame's bytes; the first metadata frame must contain none, and later bytes remain bounded by the declared total.
	// GetData выдаёт байты текущего кадра; первый metadata-кадр обязан быть пустым, следующие байты ограничены объявленной общей длиной.
	GetData() []byte
	// GetFileName supplies the first frame's display name, which the HTTP writer sanitizes before header use; it is not a filesystem path.
	// GetFileName задаёт отображаемое имя первого кадра, очищаемое writer до заголовка; файловым путём оно не является.
	GetFileName() string
	// GetMediaType supplies first-frame media metadata; the writer parses it and substitutes octet-stream for an invalid type without executing content.
	// GetMediaType задаёт media metadata первого кадра; writer разбирает её и заменяет неверный тип на octet-stream без исполнения содержимого.
	GetMediaType() string
	// GetSizeBytes exposes the declared first-frame total, bounded0..512MiB; subsequent chunk frames must leave this metadata at zero.
	// GetSizeBytes выдаёт объявленную общую длину первого кадра в пределах0..512МиБ; следующие chunk-кадры обязаны оставлять её нулевой.
	GetSizeBytes() int64
}

// writeArtifactDownloadStream requires a data-free bounded metadata frame before committing sandboxed no-store attachment headers, then accepts only metadata-free chunks within the declared total. After HTTP200, receive/write/size failures simply stop output and cannot replace it with a problem; no explicit stream cancel/join or checksum verification is added here.
// writeArtifactDownloadStream требует пустой по данным ограниченный metadata-кадр до sandboxed no-store attachment headers, затем допускает только chunks без metadata в пределах общей длины. После HTTP200 ошибки получения, записи и размера лишь останавливают выдачу и не заменяют её problem; явные cancel/join потока и проверка checksum здесь не добавляются.
func writeArtifactDownloadStream[T artifactDownloadFrame](w http.ResponseWriter, stream interface {
	Recv() (T, error)
}, err error) {
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	metadata, err := stream.Recv()
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if len(metadata.GetData()) != 0 || metadata.GetSizeBytes() < 0 || metadata.GetSizeBytes() > maximumArtifactBytes {
		writeLocalProblem(w, http.StatusBadGateway, "UPSTREAM_ARTIFACT_METADATA_INVALID", false)
		return
	}
	fileName := safeAttachmentFileName(metadata.GetFileName())
	mediaType := metadata.GetMediaType()
	if _, _, parseErr := mime.ParseMediaType(mediaType); parseErr != nil {
		mediaType = "application/octet-stream"
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": fileName})
	if disposition == "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.GetSizeBytes(), 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	var written int64
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return
		}
		if chunk.GetFileName() != "" || chunk.GetMediaType() != "" || chunk.GetSizeBytes() != 0 {
			return
		}
		written += int64(len(chunk.GetData()))
		if written > metadata.GetSizeBytes() || written > maximumArtifactBytes {
			return
		}
		if len(chunk.GetData()) > 0 {
			if _, err := w.Write(chunk.GetData()); err != nil {
				return
			}
		}
	}
	if written != metadata.GetSizeBytes() {
		return
	}
}

// safeAttachmentFileName removes ASCII controls, DEL and both slash separators, trims outer whitespace and substitutes artifact for an empty name. It preserves dots and other Unicode, so it sanitizes header display names rather than confining filesystem paths.
// safeAttachmentFileName удаляет ASCII controls, DEL и оба slash-разделителя, убирает внешние пробелы и заменяет пустое имя на artifact. Точки и прочий Unicode сохраняются: это очистка имени заголовка, а не ограничение файлового пути.
func safeAttachmentFileName(value string) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f || character == '/' || character == '\\' {
			return -1
		}
		return character
	}, value)
	value = strings.TrimSpace(value)
	if value == "" {
		return "artifact"
	}
	return value
}
