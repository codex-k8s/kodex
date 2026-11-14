package modeldiscovery

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

// TestPinnedCatalogSearchAndCursor proves broad discovery, provider separation, exact pagination and cross-query rejection without fetching any remote service.
// TestPinnedCatalogSearchAndCursor доказывает широкий поиск, разделение провайдеров, точную пагинацию и отказ чужому курсору без обращения к внешнему сервису.
func TestPinnedCatalogSearchAndCursor(t *testing.T) {
	first, err := Search("", "", "", 20, nil)
	if err != nil || first.Total < 9000 || len(first.Items) != 20 || first.NextPageToken == "" || first.Source.ModelCount != 9726 {
		t.Fatalf("catalog page is invalid: %v", err)
	}
	second, err := Search("", "", first.NextPageToken, 20, nil)
	if err != nil || second.Items[0].ProviderID == first.Items[0].ProviderID && second.Items[0].ModelID == first.Items[0].ModelID {
		t.Fatal("catalog cursor repeated a page")
	}
	if _, err := Search("glm", "", first.NextPageToken, 20, nil); err == nil {
		t.Fatal("cross-query cursor accepted")
	}
	if _, err := Search("", "zai-coding-plan", first.NextPageToken, 20, nil); err == nil {
		t.Fatal("cross-provider cursor accepted")
	}
	zai, err := Search("glm-5.3", "zai-coding-plan", "", 20, nil)
	if err != nil || len(zai.Items) < 2 {
		t.Fatal("coding models missing")
	}
	for _, model := range zai.Items {
		if model.ProviderID != "zai-coding-plan" || model.Adapter != "BUNDLED" || model.Account != "UNKNOWN" || model.Allowed != "UNAVAILABLE" || model.Tested != "NOT_RUN" || model.RouteConfigured {
			t.Fatal("catalog metadata granted execution eligibility")
		}
	}
}

// TestDiscoveryTypedFiltersAndGrouping binds all selectors to cursors and verifies family, context, media, tools, reasoning and declared cache filtering without promoting UNKNOWN to false.
// TestDiscoveryTypedFiltersAndGrouping связывает все selectors с cursors и проверяет family, context, media, tools, reasoning и declared cache, не превращая UNKNOWN в false.
func TestDiscoveryTypedFiltersAndGrouping(t *testing.T) {
	first, err := SearchFiltered("", "", 1, query.ModelDiscoveryFilter{}, nil)
	if err != nil || first.NextPageToken == "" {
		t.Fatal("base filtered page unavailable")
	}
	for _, filter := range []query.ModelDiscoveryFilter{{ProviderID: "zai-coding-plan"}, {Family: "glm"}, {MinimumContextTokens: 1000000}, {InputModality: "image"}, {OutputModality: "text"}, {Tools: "SUPPORTED"}, {Reasoning: "SUPPORTED"}, {Cache: "UNKNOWN"}, {GroupBy: "FAMILY"}} {
		if _, err := SearchFiltered("", first.NextPageToken, 1, filter, nil); err == nil {
			t.Fatal("changed filter reused another cursor")
		}
	}
	filter := query.ModelDiscoveryFilter{ProviderID: "zai-coding-plan", Family: "glm", MinimumContextTokens: 1000000, InputModality: "text", OutputModality: "text", Tools: "SUPPORTED", Reasoning: "SUPPORTED", Cache: "IMPLICIT", GroupBy: "FAMILY"}
	page, err := SearchFiltered("glm-5.3", "", 20, filter, nil)
	if err != nil || page.Total < 1 || page.GroupBy != "FAMILY" {
		t.Fatalf("typed filter unavailable: %v", err)
	}
	for _, model := range page.Items {
		if model.ProviderID != filter.ProviderID || model.Family != "glm" || !model.ContextKnown || model.ContextTokens < 1000000 || model.Tools != "SUPPORTED" || model.Reasoning != "SUPPORTED" || model.CacheImplicit != "SUPPORTED" {
			t.Fatal("filter widened result")
		}
	}
	unknown := entity.ModelDiscoveryRecord{Tools: "UNKNOWN", Reasoning: "UNKNOWN", Cache: "UNKNOWN"}
	if matchesFilters(unknown, query.ModelDiscoveryFilter{Tools: "UNSUPPORTED"}) || matchesFilters(unknown, query.ModelDiscoveryFilter{Reasoning: "SUPPORTED"}) || matchesFilters(unknown, query.ModelDiscoveryFilter{MinimumContextTokens: 1}) || !matchesFilters(unknown, query.ModelDiscoveryFilter{Cache: "UNKNOWN"}) {
		t.Fatal("unknown metadata became false or supported")
	}
	grouped, err := SearchFiltered("", "", 100, query.ModelDiscoveryFilter{GroupBy: "FAMILY"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < len(grouped.Items); index++ {
		if grouped.Items[index].Family < grouped.Items[index-1].Family {
			t.Fatal("family grouping is not globally ordered")
		}
	}
}

// TestExactRouteEvidenceKeepsAuthorityAndExecutionUnknown checks matching endpoint, protocol and model against independently configured state, including wrong-endpoint and mutation negatives.
// TestExactRouteEvidenceKeepsAuthorityAndExecutionUnknown сверяет endpoint, protocol и model с независимо настроенным состоянием, включая неверный endpoint и изменение возвращённых данных.
func TestExactRouteEvidenceKeepsAuthorityAndExecutionUnknown(t *testing.T) {
	config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "zai-coding-plan", BaseURL: "https://api.z.ai/api/coding/paas/v4", API: runtimecontract.ModelAPIChatCompletions, RouteKind: "DIRECT", AddressCIDRs: []string{"192.0.2.1/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "glm-5.3", ContextWindowTokens: 1000000, MaximumOutputTokens: 32768}}}
	definitions := []entity.ProviderDefinition{{Key: config.DefinitionKey(), Available: true, ModelProvider: &config, Models: []entity.ModelCapability{{ID: "glm-5.3", Available: true}}}}
	page, err := Search("glm-5.3", "zai-coding-plan", "", 20, definitions)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, model := range page.Items {
		if model.ModelID == "glm-5.3" {
			found = true
			if !model.RouteConfigured || model.Account != "AVAILABLE" || model.Allowed != "UNKNOWN" || model.Tested != "NOT_RUN" {
				t.Fatal("independent evidence collapsed")
			}
			model.APIs[0].BaseURL = "https://foreign.example.invalid/v1"
		}
	}
	if !found {
		t.Fatal("exact coding model missing")
	}
	definitions[0].Available = false
	page, err = Search("glm-5.3", "zai-coding-plan", "", 20, definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range page.Items {
		if model.ModelID == "glm-5.3" && (!model.RouteConfigured || model.Account != "UNAVAILABLE" || model.Allowed != "UNAVAILABLE") {
			t.Fatal("disabled descriptor lost configuration or retained account eligibility")
		}
	}
	definitions[0].Available = true
	for _, models := range [][]entity.ModelCapability{nil, {{ID: "glm-5.3", Available: false, ReadinessBlockers: []string{"PROVIDER_ACCOUNT_REVOKED"}}}} {
		definitions[0].Models = models
		expectedAccount := "UNKNOWN"
		if len(models) > 0 {
			expectedAccount = "UNAVAILABLE"
		}
		page, err = Search("glm-5.3", "zai-coding-plan", "", 20, definitions)
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range page.Items {
			if model.ModelID == "glm-5.3" && (!model.RouteConfigured || model.Account != expectedAccount || model.Allowed != "UNKNOWN" || model.Tested != "NOT_RUN") {
				t.Fatal("missing or revoked account erased configured descriptor or invented eligibility")
			}
		}
	}
	config.BaseURL = "https://api.z.ai/api/paas/v4"
	page, err = Search("glm-5.3", "zai-coding-plan", "", 20, definitions)
	if err != nil {
		t.Fatal(err)
	}
	for _, model := range page.Items {
		if model.RouteConfigured {
			t.Fatal("ordinary API alias matched Coding Plan")
		}
	}
	page, err = Search("glm-5.3", "zai-coding-plan", "", 20, nil)
	if err != nil || page.Items[0].APIs[0].BaseURL == "https://foreign.example.invalid/v1" {
		t.Fatal("returned catalog aliases immutable source")
	}
}

// TestRetiredDiscoveryCannotClaimAccountAvailability prevents a retained upstream tombstone from acquiring eligibility merely because an old owner descriptor still exists.
// TestRetiredDiscoveryCannotClaimAccountAvailability запрещает retained upstream tombstone получать допуск только потому, что старый owner descriptor ещё существует.
func TestRetiredDiscoveryCannotClaimAccountAvailability(t *testing.T) {
	config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "synthetic", BaseURL: "https://model.example.invalid/v1", AddressCIDRs: []string{"192.0.2.1/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "retired", ContextWindowTokens: 8192, MaximumOutputTokens: 1024}}}
	record := entity.ModelDiscoveryRecord{Known: "REMOVED", ModelID: "retired", APIs: []entity.ModelDiscoveryAPI{{Protocol: "chat-completions", BaseURL: config.BaseURL, SDK: config.AdapterPackage()}}}
	annotate(&record, []entity.ProviderDefinition{{Available: true, ModelProvider: &config, Models: []entity.ModelCapability{{ID: "retired", Available: true}}}})
	if record.Known != "REMOVED" || record.Allowed != "UNAVAILABLE" || record.Account != "UNKNOWN" || record.Tested != "NOT_RUN" {
		t.Fatal("retired discovery acquired execution readiness")
	}
	record.Known = "KNOWN"
	record.APIs[0].Path = "/unsupported-alternate"
	if matchesRoute(record, config, record.ModelID) {
		t.Fatal("upstream custom path matched the standard runtime API")
	}
}

// TestCatalogBoundsRejectBeforePaging exercises invalid query lengths, page sizes and cursor syntax without interpreting input as authority.
// TestCatalogBoundsRejectBeforePaging проверяет неверные длины запроса, размеры страницы и синтаксис курсора без интерпретации ввода как полномочия.
func TestCatalogBoundsRejectBeforePaging(t *testing.T) {
	for _, size := range []int{-1, 0, 101} {
		if _, err := Search("", "", "", size, nil); err == nil {
			t.Fatal("invalid page size accepted")
		}
	}
	if _, err := Search(strings.Repeat("x", 201), "", "", 20, nil); err == nil {
		t.Fatal("oversize query accepted")
	}
	if _, err := Search("", "", "invalid", 20, nil); err == nil {
		t.Fatal("malformed cursor accepted")
	}
}
