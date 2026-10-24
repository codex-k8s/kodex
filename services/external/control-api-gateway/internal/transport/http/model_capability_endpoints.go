package httptransport

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// modelProviderKey defines the closed-shaped lowercase provider-definition key accepted by model
// catalog requests and responses, independently of account authorization.
// modelProviderKey задаёт грамматику provider-definition key нижнего регистра для запросов и
// ответов каталога моделей независимо от авторизации аккаунта.
var modelProviderKey = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,79}$`)

// modelBlocker bounds model readiness blocker codes to uppercase ASCII tokens, preventing
// arbitrary diagnostic text from entering this public code field.
// modelBlocker ограничивает коды блокеров готовности модели ASCII токенами верхнего регистра,
// исключая произвольную диагностику из этого публичного поля.
var modelBlocker = regexp.MustCompile(`^[A-Z0-9_]{1,80}$`)

// ListModelCapabilities validates optional provider/account and expected catalog pins, queries CP
// and rejects malformed pages, duplicate model identities or scope mismatches before output.
// Eligibility comes from the owner response rather than the requested account ref.
// ListModelCapabilities проверяет необязательные provider/account и ожидаемые pins каталога,
// запрашивает CP и отклоняет неверные страницы, дубликаты моделей и несовпадение области до
// выдачи. Допуск приходит из ответа владельца, а не запрошенного ref аккаунта.
func (server *Server) ListModelCapabilities(w http.ResponseWriter, r *http.Request, p generated.ListModelCapabilitiesParams) {
	r, ok := catalogRequest(w, r, nil, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	key, account := stringValue(p.ProviderDefinitionKey), stringValue(p.ProviderAccountRef)
	revision, digest := stringValue(p.ExpectedCatalogRevision), stringValue(p.ExpectedCatalogDigest)
	if (p.ExpectedCatalogRevision != nil || p.ExpectedCatalogDigest != nil) && (!validManagedDigest(digest) || revision != "mcat_"+digest) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if p.ProviderDefinitionKey != nil && !modelProviderKey.MatchString(key) || p.ProviderAccountRef != nil &&
		(!opaqueHTTPReference.MatchString(account) || !strings.HasPrefix(account, "pacc_") || len(account) > 96) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListModelCapabilities(r.Context(), &cp.ListModelCapabilitiesRequest{
		ProviderDefinitionKey: key, ProviderAccountRef: account, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
		ExpectedCatalogRevision: revision, ExpectedCatalogDigest: digest,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || !validManagedDigest(response.GetCatalogDigest()) || response.GetCatalogRevision() != "mcat_"+response.GetCatalogDigest() ||
		revision != "" && (response.GetCatalogRevision() != revision || response.GetCatalogDigest() != digest) || response.Total < int64(len(response.Models)) || response.Total > maximumSafeJSONInteger ||
		len(response.Models) > int(page(p.PageSize, p.PageToken).PageSize) || len(response.GetPage().GetNextPageToken()) > 512 || !utf8.ValidString(response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result := generated.ModelCapabilityPage{Items: make([]generated.ModelCapability, 0, len(response.Models)), Total: response.Total, NextPageToken: response.GetPage().GetNextPageToken(), CatalogRevision: response.GetCatalogRevision(), CatalogDigest: response.GetCatalogDigest()}
	status, validStatus := modelCatalogStatusView(response.GetCatalogStatus(), account != "")
	if !validStatus {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	result.CatalogStatus = status
	seen := map[string]bool{}
	for _, model := range response.Models {
		item, valid := modelCapabilityView(model)
		identity := item.ProviderDefinitionKey + ":" + item.Id
		if !valid || seen[identity] || key != "" && item.ProviderDefinitionKey != key || account != "" && item.Available && !slices.Contains(item.EligibleProviderAccountRefs, account) {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		seen[identity] = true
		result.Items = append(result.Items, item)
	}
	writeJSON(w, http.StatusOK, result)
}

// modelCatalogStatusView requires status only for account-scoped queries and maps known
// state/source/failure values with coherent observation times. READY/EXPIRED need a remote source
// and NONE failure; FAILED forbids a source and requires a failure.
// modelCatalogStatusView требует status только для запросов аккаунта и преобразует известные
// state/source/failure с согласованным временем наблюдения. READY/EXPIRED требуют remote source и
// NONE; FAILED запрещает source и требует ошибку.
func modelCatalogStatusView(status *cp.ProviderModelCatalogStatus, accountScoped bool) (*generated.ProviderModelCatalogStatus, bool) {
	if !accountScoped {
		return nil, status == nil
	}
	if status == nil {
		return nil, false
	}
	result := &generated.ProviderModelCatalogStatus{}
	switch status.State {
	case cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_PENDING:
		result.State = "PENDING"
	case cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_READY:
		result.State = "READY"
	case cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_FAILED:
		result.State = "FAILED"
	case cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_EXPIRED:
		result.State = "EXPIRED"
	default:
		return nil, false
	}
	switch status.Source {
	case cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_UNSPECIFIED:
	case cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_API:
		value := generated.ProviderModelCatalogStatusSource("REMOTE_API")
		result.Source = &value
	case cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_CODEX:
		value := generated.ProviderModelCatalogStatusSource("REMOTE_CODEX")
		result.Source = &value
	default:
		return nil, false
	}
	switch status.Failure {
	case cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNSPECIFIED:
	case cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE:
		value := generated.ProviderModelCatalogStatusFailure("NONE")
		result.Failure = &value
	case cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNAVAILABLE:
		value := generated.ProviderModelCatalogStatusFailure("UNAVAILABLE")
		result.Failure = &value
	case cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_UNVERIFIED_SOURCE:
		value := generated.ProviderModelCatalogStatusFailure("UNVERIFIED_SOURCE")
		result.Failure = &value
	case cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_AUTHORIZATION_REJECTED:
		value := generated.ProviderModelCatalogStatusFailure("AUTHORIZATION_REJECTED")
		result.Failure = &value
	default:
		return nil, false
	}
	if status.ObservedAt != nil || status.ExpiresAt != nil {
		observed, ok := contextTimestamp(status.ObservedAt)
		expires, valid := contextTimestamp(status.ExpiresAt)
		if !ok || !valid || !expires.After(observed) {
			return nil, false
		}
		result.ObservedAt, result.ExpiresAt = &observed, &expires
	}
	if result.State != "PENDING" && (result.ObservedAt == nil || result.Failure == nil) {
		return nil, false
	}
	if result.State == "READY" || result.State == "EXPIRED" {
		if result.Source == nil || *result.Failure != "NONE" {
			return nil, false
		}
	}
	if result.State == "FAILED" && (result.Source != nil || *result.Failure == "NONE") {
		return nil, false
	}
	return result, true
}

// modelCapabilityView validates one model, distinct effort/account/blocker lists, default effort
// membership and availability/blocker consistency, then copies the public slices. It does not
// infer actual provider execution success from catalog availability.
// modelCapabilityView проверяет модель, уникальные списки effort/account/blocker, наличие default
// effort и согласованность available/blocker, затем копирует публичные срезы. Доступность каталога
// не объявляется успешным исполнением провайдера.
func modelCapabilityView(model *cp.ModelCapability) (generated.ModelCapability, bool) {
	if model == nil || !boundedModelText(model.Id, 160) || !modelProviderKey.MatchString(model.ProviderDefinitionKey) || len(model.ReasoningEfforts) > 16 || len(model.ReadinessBlockers) > 16 {
		return generated.ModelCapability{}, false
	}
	for _, values := range [][]string{model.ReasoningEfforts, model.EligibleProviderAccountRefs, model.ReadinessBlockers} {
		seen := map[string]bool{}
		for _, value := range values {
			if seen[value] {
				return generated.ModelCapability{}, false
			}
			seen[value] = true
		}
	}
	for _, effort := range model.ReasoningEfforts {
		if !boundedModelText(effort, 80) {
			return generated.ModelCapability{}, false
		}
	}
	if model.DefaultReasoningEffort != "" && !slices.Contains(model.ReasoningEfforts, model.DefaultReasoningEffort) || len(model.ReasoningEfforts) > 0 && model.DefaultReasoningEffort == "" {
		return generated.ModelCapability{}, false
	}
	for _, ref := range model.EligibleProviderAccountRefs {
		if !opaqueHTTPReference.MatchString(ref) || !strings.HasPrefix(ref, "pacc_") {
			return generated.ModelCapability{}, false
		}
	}
	for _, blocker := range model.ReadinessBlockers {
		if !modelBlocker.MatchString(blocker) {
			return generated.ModelCapability{}, false
		}
	}
	if model.Available && (len(model.EligibleProviderAccountRefs) == 0 || len(model.ReadinessBlockers) != 0) || !model.Available && len(model.ReadinessBlockers) == 0 {
		return generated.ModelCapability{}, false
	}
	return generated.ModelCapability{Id: model.Id, ProviderDefinitionKey: model.ProviderDefinitionKey,
		ReasoningEfforts: append([]string{}, model.ReasoningEfforts...), DefaultReasoningEffort: model.DefaultReasoningEffort,
		Available: model.Available, EligibleProviderAccountRefs: append([]string{}, model.EligibleProviderAccountRefs...),
		ReadinessBlockers: append([]string{}, model.ReadinessBlockers...)}, true
}

// boundedModelText requires nonempty valid UTF-8 text within a byte bound and forbids NUL, CR and
// LF. It preserves text exactly without trimming or normalization.
// boundedModelText требует непустой корректный UTF-8 текст в байтовом лимите без NUL, CR и LF.
// Текст сохраняется точно, без обрезки пробелов и нормализации.
func boundedModelText(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}
