package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"sort"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// scanAssistantIntegrationGrants consumes the already owner-scoped six-column query, closes rows
// before checking iteration errors and discards partial grants on any scan/read failure.
// scanAssistantIntegrationGrants читает уже ограниченный владельцем запрос из шести колонок,
// закрывает rows до проверки ошибки итерации и отбрасывает частичные grants при ошибке чтения.
func scanAssistantIntegrationGrants(rows pgx.Rows) (map[string]entity.SystemAssistantIntegrationGrantCandidate, error) {
	grants := map[string]entity.SystemAssistantIntegrationGrantCandidate{}
	for rows.Next() {
		var key string
		var grant entity.SystemAssistantIntegrationGrantCandidate
		if rows.Scan(&grant.CurrentGrantRef, &grant.CurrentGrantVersion, &key, &grant.CurrentApprovalPolicy, &grant.CurrentApprovalScopePaths, &grant.CurrentGrantEnabled) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		grants[key] = grant
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return grants, nil
}

// assistantIntegrationCandidateAfter decodes the existing single-document cursor and binds it
// to the freshly calculated owner context; the cursor itself does not authorize a resource.
// assistantIntegrationCandidateAfter декодирует прежний однодокументный cursor и связывает его
// со свежим контекстом владельца; сам cursor не даёт полномочий на ресурс.
func assistantIntegrationCandidateAfter(token, contextDigest string) (string, error) {
	after := ""
	if token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(token)
		var cursor integrationCandidateCursor
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err != nil || decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.Scope != contextDigest || !validCapabilityKey(cursor.After) {
			return "", errs.ErrInvalid
		}
		after = cursor.After
	}
	return after, nil
}

// integrationCapabilityProjection materializes the advertised schema and digest with copied
// approval options; callers retain eligibility, ownership and execution-grant checks.
// integrationCapabilityProjection материализует предлагаемую схему и digest с копией вариантов
// подтверждения; проверки eligibility, ownership и execution grant остаются у callers.
func integrationCapabilityProjection(capability integrationpackage.Capability) (entity.IntegrationCapability, error) {
	schema, err := capability.InputSchema()
	if err != nil {
		return entity.IntegrationCapability{}, errs.ErrUnavailable
	}
	schemaDigest, err := capability.InputSchemaDigest()
	if err != nil {
		return entity.IntegrationCapability{}, errs.ErrUnavailable
	}
	return entity.IntegrationCapability{Key: capability.Key, Name: capability.Name, Description: capability.Description, Operation: capability.Operation, Risk: capability.Risk, ApprovalPolicy: capability.ApprovalPolicy, AllowedApprovalPolicies: append([]string{}, capability.AllowedApprovalPolicies...), ResourceKind: capability.ResourceScope.Kind, InputFields: integrationConfigurationFields(capability.InputFields), InputSchema: string(schema), InputSchemaSHA256: schemaDigest}, nil
}

// assistantIntegrationCandidatePage sorts eligible items, keeps the pre-cursor total and emits
// the existing exclusive-key page/token; validated callers own page-size and context selection.
// assistantIntegrationCandidatePage сортирует допустимые элементы, сохраняет total до cursor
// и выдаёт прежнюю страницу по исключающему ключу; размер и контекст проверяет caller.
func assistantIntegrationCandidatePage(items []entity.SystemAssistantIntegrationGrantCandidate, size int32, contextDigest, after string) (selected []entity.SystemAssistantIntegrationGrantCandidate, total int64, next string) {
	selected = []entity.SystemAssistantIntegrationGrantCandidate{}
	sort.Slice(items, func(i, j int) bool { return items[i].Capability.Key < items[j].Capability.Key })
	total = int64(len(items))
	limit := size
	if limit == 0 {
		limit = 50
	}
	for _, item := range items {
		if item.Capability.Key <= after {
			continue
		}
		if len(selected) == int(limit) {
			cursor, _ := json.Marshal(integrationCandidateCursor{Scope: contextDigest, After: selected[len(selected)-1].Capability.Key})
			next = base64.RawURLEncoding.EncodeToString(cursor)
			break
		}
		selected = append(selected, item)
	}
	return selected, total, next
}
