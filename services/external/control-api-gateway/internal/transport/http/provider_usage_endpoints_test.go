package httptransport

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func providerUsageAccountFixture() *cp.ProviderAccount {
	now := time.Now().UTC()
	dimension := func(state cp.ProviderUsageState, reason cp.ProviderUsageReason) *cp.ProviderUsageDimension {
		return &cp.ProviderUsageDimension{State: state, Reason: reason, Remediation: cp.ProviderUsageRemediation_PROVIDER_USAGE_REMEDIATION_NONE}
	}
	ready := cp.ProviderUsageState_PROVIDER_USAGE_STATE_READY
	return &cp.ProviderAccount{Ref: "pacc_usage0001", Version: 3, Usage: &cp.ProviderAccountUsage{
		Context:        &cp.ProviderAccountUsageContext{Purpose: cp.ProviderAccountUsagePurpose_PROVIDER_ACCOUNT_USAGE_PURPOSE_CONFIGURE, AgentRef: "agt_usage00001", RuntimeProfileRef: "codex-default", ProviderDefinitionKey: "openai-codex"},
		AccountVersion: 3, AgentVersion: 2, Lifecycle: dimension(ready, cp.ProviderUsageReason_PROVIDER_USAGE_REASON_AVAILABLE), Credential: dimension(ready, cp.ProviderUsageReason_PROVIDER_USAGE_REASON_CREDENTIAL_READY),
		ProviderHealth: dimension(ready, cp.ProviderUsageReason_PROVIDER_USAGE_REASON_CREDENTIALED_CATALOG_REACHABLE), ModelCompatibility: dimension(cp.ProviderUsageState_PROVIDER_USAGE_STATE_NOT_EVALUATED, cp.ProviderUsageReason_PROVIDER_USAGE_REASON_MODEL_REQUIRED),
		Capacity: dimension(ready, cp.ProviderUsageReason_PROVIDER_USAGE_REASON_CAPACITY_AVAILABLE), ActorEligibility: dimension(ready, cp.ProviderUsageReason_PROVIDER_USAGE_REASON_AVAILABLE),
		EligibleForSelection: true, OperationalState: cp.ProviderUsageState_PROVIDER_USAGE_STATE_BLOCKED, MaximumConcurrentExecutions: 1,
		CatalogStatus: &cp.ProviderModelCatalogStatus{State: cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_READY, Source: cp.ProviderModelCatalogSource_PROVIDER_MODEL_CATALOG_SOURCE_REMOTE_API, Failure: cp.ProviderModelCatalogFailure_PROVIDER_MODEL_CATALOG_FAILURE_NONE, ObservedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Minute))},
		CatalogDigest: strings.Repeat("a", 64), CatalogRevision: "mcat_" + strings.Repeat("a", 64), ContextDigest: strings.Repeat("b", 64), ObservedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(10 * time.Second)),
		ProviderHealthScope: cp.ProviderHealthScope_PROVIDER_HEALTH_SCOPE_CREDENTIALED_CATALOG_REACHABILITY, ProviderHealthObservedAt: timestamppb.New(now), ProviderHealthExpiresAt: timestamppb.New(now.Add(time.Minute)),
	}}
}

const providerConfigureQuery = "?usagePurpose=CONFIGURE&usageAgentRef=agt_usage00001&usageRuntimeProfileRef=codex-default&usageProviderDefinitionKey=openai-codex"

func TestProviderUsageListAndSingleKeepContextAndIndependentDimensions(t *testing.T) {
	for _, list := range []bool{false, true} {
		account := providerUsageAccountFixture()
		var response proto.Message = &cp.GetProviderAccountResponse{Account: account}
		path := "/api/v1/provider-accounts/" + account.Ref
		if list {
			response = &cp.ListProviderAccountsResponse{Accounts: []*cp.ProviderAccount{account}}
			path = "/api/v1/provider-accounts"
		}
		client := &catalogRPCRecorder{response: response}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path+providerConfigureQuery, nil))
		if w.Code != 200 {
			t.Fatalf("usage response status %d", w.Code)
		}
		var forwarded *cp.ProviderAccountUsageContext
		switch request := client.request.(type) {
		case *cp.GetProviderAccountRequest:
			forwarded = request.UsageContext
		case *cp.ListProviderAccountsRequest:
			forwarded = request.UsageContext
		}
		if !proto.Equal(forwarded, account.Usage.Context) {
			t.Fatal("usage context changed")
		}
		var output map[string]any
		if json.Unmarshal(w.Body.Bytes(), &output) != nil {
			t.Fatal("invalid response")
		}
		if list {
			output = output["items"].([]any)[0].(map[string]any)
		}
		u := output["usage"].(map[string]any)
		if u["allowedToSubmit"] != false || u["eligibleForSelection"] != true || u["activeExecutions"] != float64(0) || u["providerHealthScope"] != "CREDENTIALED_CATALOG_REACHABILITY" || u["modelCompatibility"].(map[string]any)["state"] != "NOT_EVALUATED" {
			t.Fatal("independent dimensions or explicit zero lost")
		}
	}
}

func TestProviderUsageRejectsContextOverridesAndMalformedSnapshots(t *testing.T) {
	for _, query := range []string{"?usagePurpose=LAUNCH&usageAgentRef=agt_usage00001&usageModel=model-one", "?usagePurpose=CONFIGURE&usageAgentRef=agt_usage00001", "?usageAgentRef=agt_usage00001", providerConfigureQuery + "&usageReasoningEffort=high"} {
		client := &catalogRPCRecorder{}
		w := httptest.NewRecorder()
		catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/provider-accounts"+query, nil))
		if w.Code != 400 || client.request != nil {
			t.Fatal("invalid context reached owner")
		}
	}
	for _, list := range []bool{false, true} {
		for _, mutate := range []func(*cp.ProviderAccount){
			func(a *cp.ProviderAccount) { a.Usage = nil },
			func(a *cp.ProviderAccount) { a.Usage.ActorEligibility.State = cp.ProviderUsageState(99) },
			func(a *cp.ProviderAccount) { a.Usage.Context.RuntimeProfileRef = "other-profile" },
			func(a *cp.ProviderAccount) {
				a.Usage.CatalogStatus.State = cp.ProviderModelCatalogState_PROVIDER_MODEL_CATALOG_STATE_PENDING
			},
			func(a *cp.ProviderAccount) { a.Usage.ActiveExecutions = 1 },
			func(a *cp.ProviderAccount) { a.Version++ },
		} {
			account := providerUsageAccountFixture()
			mutate(account)
			var response proto.Message = &cp.GetProviderAccountResponse{Account: account}
			path := "/api/v1/provider-accounts/" + account.Ref
			if list {
				response = &cp.ListProviderAccountsResponse{Accounts: []*cp.ProviderAccount{account}}
				path = "/api/v1/provider-accounts"
			}
			client := &catalogRPCRecorder{response: response}
			w := httptest.NewRecorder()
			catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", path+providerConfigureQuery, nil))
			if w.Code != 502 {
				t.Fatalf("malformed usage status %d", w.Code)
			}
		}
	}
}

func TestProviderUsageLaunchPreservesQueuedAdmissionWithExhaustedCapacity(t *testing.T) {
	account := providerUsageAccountFixture()
	u := account.Usage
	u.Context = &cp.ProviderAccountUsageContext{Purpose: cp.ProviderAccountUsagePurpose_PROVIDER_ACCOUNT_USAGE_PURPOSE_LAUNCH, AgentRef: "agt_usage00001"}
	u.ModelCompatibility = proto.Clone(u.Lifecycle).(*cp.ProviderUsageDimension)
	u.ActiveExecutions = 1
	u.Capacity = &cp.ProviderUsageDimension{State: cp.ProviderUsageState_PROVIDER_USAGE_STATE_BLOCKED, Reason: cp.ProviderUsageReason_PROVIDER_USAGE_REASON_CAPACITY_EXHAUSTED, Remediation: cp.ProviderUsageRemediation_PROVIDER_USAGE_REMEDIATION_WAIT_FOR_CAPACITY}
	u.AllowedToSubmit = true
	client := &catalogRPCRecorder{response: &cp.GetProviderAccountResponse{Account: account}}
	w := httptest.NewRecorder()
	catalogTestHandler(client).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/provider-accounts/"+account.Ref+"?usagePurpose=LAUNCH&usageAgentRef=agt_usage00001", nil))
	var output map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &output) != nil {
		t.Fatalf("queued admission status %d", w.Code)
	}
	usage := output["usage"].(map[string]any)
	if usage["allowedToSubmit"] != true || usage["capacity"].(map[string]any)["reason"] != "CAPACITY_EXHAUSTED" || usage["operationalState"] != "BLOCKED" {
		t.Fatal("capacity changed owner submission admission")
	}
}
