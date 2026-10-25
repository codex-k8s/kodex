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

// queryRuntimeManagedMCPExecution embeds the latest running node revision and live claimed lease
// with root/deadline checks under shared locks. It returns the owner snapshot used to authorize
// the invocation, not a caller profile.
// queryRuntimeManagedMCPExecution встраивает последнюю ревизию работающего узла и действующую
// захваченную аренду с root/deadline проверками под разделяемыми блокировками. Возвращает owner
// snapshot для допуска invocation, а не профиль клиента.
//
//go:embed sql/runtime_managed_mcp__execution.sql
var queryRuntimeManagedMCPExecution string

// queryRuntimeManagedMCPCurrentGrants embeds exact current Context7 resolve/query grant,
// connection and package version/digest/readiness matching. Both READ/NONE grants must remain
// enabled and tied to the pinned agent; a count of two is required by the caller.
// queryRuntimeManagedMCPCurrentGrants встраивает точное сопоставление текущих Context7
// resolve/query grants, подключения и версии, дайджеста и готовности пакета. Оба READ/NONE grant
// должны оставаться включёнными и связанными с закреплённым агентом; вызывающий код требует число
// два.
//
//go:embed sql/runtime_managed_mcp__current_grants.sql
var queryRuntimeManagedMCPCurrentGrants string

// requireCurrentManagedMCPHealth decodes the single pinned owner profile, verifies the selected
// connection and both exact current grants, then resolves current health and compares stable
// inputs. Missing/invalid owner data is Forbidden, changed health inputs Conflict and backend
// failures propagate. It neither rewrites RuntimeRevision nor lets a new observation or caller
// profile extend authority.
// requireCurrentManagedMCPHealth декодирует единственный закреплённый профиль владельца, сверяет
// выбранное подключение и оба точных текущих grant, затем разрешает текущее здоровье и сравнивает
// стабильные входы. Отсутствующие или неверные данные владельца дают Forbidden, изменённые входы —
// Conflict, ошибки хранилища передаются выше. Не переписывает RuntimeRevision и не позволяет
// новому наблюдению либо профилю клиента расширить права.
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

// managedMCPInputFromOwnerSnapshot bounds owner JSON, extracts public MCP dependencies and
// converts grants, then strictly decodes the profile array with no unknown fields or trailing
// value. It requires exactly one validated profile and rejects future CheckedAt. Outer snapshot
// JSON remains permissive; the result is validated metadata, not a new grant.
// managedMCPInputFromOwnerSnapshot ограничивает JSON владельца, извлекает публичные
// MCP-зависимости и преобразует grants, затем строго декодирует массив профилей без неизвестных
// полей и лишнего значения. Требует ровно один проверенный профиль и отклоняет CheckedAt из
// будущего. Внешний JSON снимка остаётся обычным; результат — проверенные метаданные, а не новый
// grant.
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

// sameManagedMCPHealthInputs compares health proofs after clearing only observation ref,
// generation and timestamp on local value copies. Configuration, credential, package and
// connection inputs remain exact; grant refs/versions and freshness are checked separately by the
// owner path.
// sameManagedMCPHealthInputs сравнивает health proofs после очистки только ссылки наблюдения,
// поколения и времени в локальных копиях значений. Конфигурация, credential, пакет и подключение
// остаются точными; ссылки и версии grants и свежесть проверяет отдельно путь владельца.
func sameManagedMCPHealthInputs(a, b runtimecontract.ManagedMCPHealthProof) bool {
	a.TestRef, b.TestRef = "", ""
	a.Generation, b.Generation = 0, 0
	a.CheckedAt, b.CheckedAt = time.Time{}, time.Time{}
	return a == b
}
