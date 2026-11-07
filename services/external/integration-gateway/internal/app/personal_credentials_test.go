package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// personalProbeFixture records actual worker ordering at the generated client boundary. Owner
// admission and provider responses are controlled inputs; this fixture does not prove JWT or HTTP.
// personalProbeFixture записывает порядок worker на границе generated client. Ответы owner и
// provider заданы явно; fixture не доказывает JWT либо HTTP.
type personalProbeFixture struct {
	cp.PersonalIntegrationCredentialServiceClient
	t                                             *testing.T
	steps                                         []string
	claims                                        *cp.ClaimPersonalIntegrationCredentialTestsResponse
	admission                                     *cp.BeginPersonalIntegrationCredentialProbeResponse
	claimErr, beginErr, operationErr, completeErr error
	receiptState                                  string
	receiptMismatch                               bool
	completion                                    *cp.CompletePersonalIntegrationCredentialTestRequest
	probeResult                                   integration.CredentialProbeResult
}

// ClaimPersonalIntegrationCredentialTests captures a single bounded poll without materializing
// credentials, fetching external data or synthesizing another claim after an error.
// ClaimPersonalIntegrationCredentialTests фиксирует один bounded poll без материализации ключа,
// внешних запросов и повторного claim после ошибки.
func (fixture *personalProbeFixture) ClaimPersonalIntegrationCredentialTests(_ context.Context, input *cp.ClaimPersonalIntegrationCredentialTestsRequest, _ ...grpc.CallOption) (*cp.ClaimPersonalIntegrationCredentialTestsResponse, error) {
	fixture.steps = append(fixture.steps, "claim")
	if input.Limit != 1 || input.WorkloadInstance != "personal-fixture-worker" {
		fixture.t.Fatal("worker claim lost its instance or single-job bound")
	}
	return fixture.claims, fixture.claimErr
}

// BeginPersonalIntegrationCredentialProbe checks the exact lease sent by the worker and can
// inject a lost admission ACK; the production loop must not retry or call the provider afterward.
// BeginPersonalIntegrationCredentialProbe проверяет точную аренду и может потерять admission ACK;
// рабочий цикл после этого не должен повторять допуск либо обращаться к provider.
func (fixture *personalProbeFixture) BeginPersonalIntegrationCredentialProbe(_ context.Context, input *cp.BeginPersonalIntegrationCredentialProbeRequest, _ ...grpc.CallOption) (*cp.BeginPersonalIntegrationCredentialProbeResponse, error) {
	fixture.steps = append(fixture.steps, "begin")
	if input.TestRef != "pct_fixture01" || input.LeaseRef != "lea_fixture01" || input.Fence != "fnc_fixture01" || input.Generation != 1 {
		fixture.t.Fatal("worker changed owner lease coordinates")
	}
	return fixture.admission, fixture.beginErr
}

// VerifyPersonalCredential records the bounded probe port and exact personal descriptor,
// returning only controlled observation metadata without a provider payload or credential.
// VerifyPersonalCredential фиксирует bounded probe port и личный descriptor, возвращая лишь
// управляемые observation metadata без provider payload либо credential.
func (fixture *personalProbeFixture) VerifyPersonalCredential(ctx context.Context, input integration.Request) (integration.CredentialProbeResult, error) {
	fixture.steps = append(fixture.steps, "probe")
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Second || ctx.Err() != nil || input.Credential == nil || input.Credential.Ref != "icr_personalfixture" || input.ConnectionRef != "int_fixture01" || input.CapabilityKey != "" || input.Operation != "" {
		fixture.t.Fatal("probe lost its bounded context or owner-selected personal descriptor")
	}
	return fixture.probeResult, fixture.operationErr
}

// CompletePersonalIntegrationCredentialTest captures a closed observation and immutable lease
// pins, returning an exact receipt or an explicit owner failure without simulating a retry.
// CompletePersonalIntegrationCredentialTest фиксирует закрытый outcome и immutable lease pins,
// возвращая точный receipt либо явную ошибку владельца без имитации повтора.
func (fixture *personalProbeFixture) CompletePersonalIntegrationCredentialTest(_ context.Context, input *cp.CompletePersonalIntegrationCredentialTestRequest, _ ...grpc.CallOption) (*cp.CompletePersonalIntegrationCredentialTestResponse, error) {
	fixture.steps = append(fixture.steps, "complete")
	fixture.completion = input
	if input.TestRef != "pct_fixture01" || input.LeaseRef != "lea_fixture01" || input.Fence != "fnc_fixture01" || input.Generation != 1 || input.GetMutation().GetIdempotencyKey() != stableKey("pct_fixture01", "personal-complete:1") {
		fixture.t.Fatal("completion changed owner lease or stable receipt key")
	}
	if strings.Contains(protojson.Format(input), "synthetic-private-provider-summary") {
		fixture.t.Fatal("provider payload escaped into owner completion")
	}
	state := fixture.receiptState
	if state == "" {
		state = "FAILED"
		if input.Result == cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED {
			state = "SUCCEEDED"
		}
	}
	ref := input.TestRef
	if fixture.receiptMismatch {
		ref = "pct_other01"
	}
	return &cp.CompletePersonalIntegrationCredentialTestResponse{TestRef: ref, State: state, Applied: state != "CANCELLED"}, fixture.completeErr
}

// TestPersonalCredentialWorkerFencesAndAccounting exercises the actual worker loop: no probe on
// refused/uncertain admission, one bounded READ after admission, closed outcomes and accounting
// before a failed completion. It makes no claim about provider permission coverage.
// TestPersonalCredentialWorkerFencesAndAccounting проверяет реальный цикл: отказ/неизвестный
// admission исключает probe, допуск разрешает один bounded READ, outcomes закрыты, учёт происходит
// до неудачного completion. Полнота внешних прав этим тестом не подтверждается.
func TestPersonalCredentialWorkerFencesAndAccounting(t *testing.T) {
	for _, test := range []struct {
		name      string
		mutate    func(*personalProbeFixture)
		steps     []string
		processed int
		wantErr   bool
		outcome   cp.PersonalIntegrationCredentialTestResult
	}{
		{"success", nil, []string{"claim", "begin", "probe", "metric-success", "complete"}, 1, false, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED},
		{"confirmed and denied reads", func(f *personalProbeFixture) {
			f.probeResult = integration.CredentialProbeResult{ConfirmedCapabilityKeys: []string{"fixture.health", "fixture.list"}, DeniedCapabilityKeys: []string{"fixture.denied"}}
		}, []string{"claim", "begin", "probe", "metric-success", "complete"}, 1, false, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED},
		{"auth rejected", func(f *personalProbeFixture) {
			f.operationErr = &integration.SafeError{Code: "INTEGRATION_AUTH_REJECTED"}
		}, []string{"claim", "begin", "probe", "metric-failure", "complete"}, 1, false, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTH_REJECTED},
		{"timeout", func(f *personalProbeFixture) { f.operationErr = context.DeadlineExceeded }, []string{"claim", "begin", "probe", "metric-failure", "complete"}, 1, false, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_UNAVAILABLE},
		{"owner completion lost", func(f *personalProbeFixture) { f.completeErr = errors.New("owner response lost") }, []string{"claim", "begin", "probe", "metric-success", "complete"}, 1, true, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED},
		{"owner discarded observation", func(f *personalProbeFixture) { f.receiptState = "CANCELLED" }, []string{"claim", "begin", "probe", "metric-success", "complete"}, 1, false, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED},
		{"receipt mismatch", func(f *personalProbeFixture) { f.receiptMismatch = true }, []string{"claim", "begin", "probe", "metric-success", "complete"}, 1, true, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_AUTHENTICATED},
		{"false success receipt", func(f *personalProbeFixture) { f.operationErr = context.DeadlineExceeded; f.receiptState = "SUCCEEDED" }, []string{"claim", "begin", "probe", "metric-failure", "complete"}, 1, true, cp.PersonalIntegrationCredentialTestResult_PERSONAL_INTEGRATION_CREDENTIAL_TEST_RESULT_UNAVAILABLE},
		{"admission denied", func(f *personalProbeFixture) { f.admission.Admitted = false }, []string{"claim", "begin"}, 1, false, 0},
		{"admission ACK lost", func(f *personalProbeFixture) { f.beginErr = errors.New("admission response lost") }, []string{"claim", "begin"}, 0, true, 0},
		{"nil admission", func(f *personalProbeFixture) { f.admission = nil }, []string{"claim", "begin"}, 0, true, 0},
		{"claim failed", func(f *personalProbeFixture) { f.claimErr = errors.New("claim failed") }, []string{"claim"}, 0, true, 0},
		{"nil poll", func(f *personalProbeFixture) { f.claims = nil }, []string{"claim"}, 0, true, 0},
		{"empty poll", func(f *personalProbeFixture) { f.claims.Claims = nil }, []string{"claim"}, 0, false, 0},
		{"oversized batch", func(f *personalProbeFixture) { f.claims.Claims = append(f.claims.Claims, f.claims.Claims[0]) }, []string{"claim"}, 0, true, 0},
		{"nil claim", func(f *personalProbeFixture) { f.claims.Claims[0] = nil }, []string{"claim"}, 0, true, 0},
		{"expired lease", func(f *personalProbeFixture) {
			f.claims.Claims[0].Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
		}, []string{"claim"}, 0, true, 0},
		{"insufficient lease", func(f *personalProbeFixture) {
			f.claims.Claims[0].Lease.ExpiresAt = timestamppb.New(time.Now().Add(time.Second))
		}, []string{"claim"}, 0, true, 0},
		{"missing credential", func(f *personalProbeFixture) { f.claims.Claims[0].CredentialRevision = nil }, []string{"claim"}, 0, true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &personalProbeFixture{t: t, admission: &cp.BeginPersonalIntegrationCredentialProbeResponse{Admitted: true}, claims: &cp.ClaimPersonalIntegrationCredentialTestsResponse{Claims: []*cp.IntegrationConnectionTestClaim{{TestRef: "pct_fixture01", ConnectionRef: "int_fixture01", CredentialRevision: &cp.IntegrationCredentialRevision{Ref: "icr_personalfixture"}, Lease: &cp.WorkLease{Ref: "lea_fixture01", Fence: "fnc_fixture01", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second))}}}}}
			if test.mutate != nil {
				test.mutate(fixture)
			}
			processed, err := processPersonalCredentialTests(t.Context(), fixture, fixture, Config{InstanceID: "personal-fixture-worker", OperationTimeout: time.Second, RequestTimeout: time.Second}, func(success bool) {
				step := "metric-failure"
				if success {
					step = "metric-success"
				}
				fixture.steps = append(fixture.steps, step)
			})
			if processed != test.processed || (err != nil) != test.wantErr || !reflect.DeepEqual(fixture.steps, test.steps) {
				t.Fatalf("worker result differs: processed=%d error=%v steps=%v", processed, err, fixture.steps)
			}
			if test.outcome != 0 && (fixture.completion == nil || fixture.completion.Result != test.outcome) {
				t.Fatal("worker reported an incorrect closed health outcome")
			}
			if fixture.completion != nil && fixture.operationErr == nil && (!reflect.DeepEqual(fixture.completion.ConfirmedCapabilityKeys, fixture.probeResult.ConfirmedCapabilityKeys) || !reflect.DeepEqual(fixture.completion.DeniedCapabilityKeys, fixture.probeResult.DeniedCapabilityKeys)) {
				t.Fatal("worker lost safe probe observations at completion")
			}
		})
	}
}

// TestPersonalProbeMetadataCompletion checks the real worker's typed completion boundary and
// suppresses metadata after an authentication failure. It performs no live API or secret read.
// TestPersonalProbeMetadataCompletion проверяет typed completion boundary настоящего worker и
// удаляет metadata после authentication failure. Live API либо secret read не выполняются.
func TestPersonalProbeMetadataCompletion(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		expiry := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
		fixture := &personalProbeFixture{t: t, admission: &cp.BeginPersonalIntegrationCredentialProbeResponse{Admitted: true}, claims: &cp.ClaimPersonalIntegrationCredentialTestsResponse{Claims: []*cp.IntegrationConnectionTestClaim{{TestRef: "pct_fixture01", ConnectionRef: "int_fixture01", CredentialRevision: &cp.IntegrationCredentialRevision{Ref: "icr_personalfixture"}, Lease: &cp.WorkLease{Ref: "lea_fixture01", Fence: "fnc_fixture01", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(30 * time.Second))}}}}}
		fixture.probeResult = integration.CredentialProbeResult{ConfirmedCapabilityKeys: []string{"gitlab.project.metadata.read"}, DeniedCapabilityKeys: []string{"gitlab.issue.create"}, ObservedPermissionProfiles: []string{integrationpackage.CredentialObservationGitLabProjectWriteScopeV1}, ExpiresAt: &expiry}
		if rejected {
			fixture.operationErr = &integration.SafeError{Code: "INTEGRATION_AUTH_REJECTED", AuthenticationHTTPStatus: 401}
		}
		processed, err := processPersonalCredentialTests(t.Context(), fixture, fixture, Config{InstanceID: "personal-fixture-worker", OperationTimeout: time.Second, RequestTimeout: time.Second}, func(bool) {})
		if err != nil || processed != 1 || fixture.completion == nil {
			t.Fatal("metadata completion failed", err)
		}
		completion := fixture.completion
		if rejected {
			if len(completion.ObservedPermissionProfiles) != 0 || completion.ExpiresAt != nil || len(completion.DeniedCapabilityKeys) != 0 {
				t.Fatal("rejected batch retained metadata")
			}
			continue
		}
		if len(completion.ObservedPermissionProfiles) != 1 || completion.ObservedPermissionProfiles[0] != cp.PersonalIntegrationCredentialObservationProfile_PERSONAL_INTEGRATION_CREDENTIAL_OBSERVATION_PROFILE_GITLAB_PROJECT_WRITE_SCOPE_V1 || completion.ExpiresAt == nil || !completion.ExpiresAt.AsTime().Equal(expiry) || !reflect.DeepEqual(completion.DeniedCapabilityKeys, fixture.probeResult.DeniedCapabilityKeys) {
			t.Fatal("worker dropped typed profile/expiry/denial")
		}
	}
}
