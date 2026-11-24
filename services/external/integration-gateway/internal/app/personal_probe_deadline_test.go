package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// finitePersonalProbeFixture supplies one exact claim and version-capable response to the real worker loop without accessing credentials or providers.
// finitePersonalProbeFixture передаёт real worker loop один exact claim и version-capable response без доступа к credentials либо providers.
func finitePersonalProbeFixture(t *testing.T, original, fresh time.Time) *personalProbeFixture {
	t.Helper()
	return &personalProbeFixture{t: t,
		admission: &cp.BeginPersonalIntegrationCredentialProbeResponse{Admitted: true, AuthorizedUntil: timestamppb.New(fresh)},
		claims: &cp.ClaimPersonalIntegrationCredentialTestsResponse{Claims: []*cp.IntegrationConnectionTestClaim{{
			CredentialDeliveryKind: cp.IntegrationCredentialDeliveryKind_INTEGRATION_CREDENTIAL_DELIVERY_KIND_TOKEN, InputDigest: strings.Repeat("a", 64), TestRef: "pct_fixture01", ConnectionRef: "int_fixture01", CredentialRevision: &cp.IntegrationCredentialRevision{Ref: "icr_personalfixture"},
			Lease: &cp.WorkLease{Ref: "lea_fixture01", Fence: "fnc_fixture01", Generation: 1, ExpiresAt: timestamppb.New(original)},
		}}},
	}
}

// TestPersonalProbeFiniteAdmissionRejectsLegacyAndLateACK keeps absent/invalid/expired owner credit and successful delayed responses before every adapter read and completion.
// TestPersonalProbeFiniteAdmissionRejectsLegacyAndLateACK не допускает adapter read либо completion при absent/invalid/expired owner credit и позднем successful response.
func TestPersonalProbeFiniteAdmissionRejectsLegacyAndLateACK(t *testing.T) {
	for _, scenario := range []string{"legacy-owner", "invalid-owner", "expired-owner", "late-fresh-ack", "late-original-ack"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Now()
			fixture := finitePersonalProbeFixture(t, now.Add(time.Minute), now.Add(time.Minute))
			switch scenario {
			case "legacy-owner":
				fixture.admission.AuthorizedUntil = nil
			case "invalid-owner":
				fixture.admission.AuthorizedUntil = &timestamppb.Timestamp{Seconds: 1 << 62}
			case "expired-owner":
				fixture.admission.AuthorizedUntil = timestamppb.New(now.Add(-time.Second))
			case "late-fresh-ack", "late-original-ack":
				until := now.Add(40 * time.Millisecond)
				if scenario == "late-fresh-ack" {
					fixture.admission.AuthorizedUntil = timestamppb.New(until)
				} else {
					fixture.claims.Claims[0].Lease.ExpiresAt = timestamppb.New(until)
				}
				fixture.beforeACK = func() { time.Sleep(time.Until(until) + 5*time.Millisecond) }
			}
			count, err := processPersonalCredentialTests(t.Context(), fixture, fixture,
				Config{InstanceID: "personal-fixture-worker", OperationTimeout: time.Millisecond, RequestTimeout: time.Millisecond},
				func(bool) { t.Fatal("unstarted probe produced metrics") })
			if err == nil || count != 0 || !reflect.DeepEqual(fixture.steps, []string{"claim", "begin"}) || fixture.completion != nil {
				t.Fatal("unsupported or expired admission reached an external probe", scenario, count, err, fixture.steps)
			}
		})
	}
}

// TestPersonalProbeFiniteAdmissionKeepsOriginalAndFreshMinimum verifies the actual adapter context after fresh shortening or permission restoration; neither response renews the original ceiling.
// TestPersonalProbeFiniteAdmissionKeepsOriginalAndFreshMinimum проверяет actual adapter context после fresh shortening либо восстановления permission; response не продлевает original ceiling.
func TestPersonalProbeFiniteAdmissionKeepsOriginalAndFreshMinimum(t *testing.T) {
	for _, restored := range []bool{false, true} {
		now := time.Now()
		short, long := now.Add(100*time.Millisecond), now.Add(time.Minute)
		original, fresh := long, short
		if restored {
			short = now.Add(500 * time.Millisecond)
			original, fresh = short, long
		}
		fixture := finitePersonalProbeFixture(t, original, fresh)
		if restored {
			fixture.beforeACK = func() { time.Sleep(time.Until(now.Add(300 * time.Millisecond))) }
		}
		fixture.verifyContext = func(ctx context.Context) {
			deadline, ok := ctx.Deadline()
			// The lease arrives as a protobuf wall-clock timestamp, independent of local monotonic sampling.
			// Срок аренды приходит как wall-clock timestamp, независимо от локальных монотонных замеров.
			if !ok || deadline.Round(0).After(short.Round(0)) || ctx.Err() != nil {
				t.Fatal("probe context lost the original/fresh finite minimum", restored, deadline)
			}
		}
		metrics := 0
		count, err := processPersonalCredentialTests(t.Context(), fixture, fixture,
			Config{InstanceID: "personal-fixture-worker", OperationTimeout: 300 * time.Millisecond, RequestTimeout: time.Millisecond},
			func(bool) { metrics++ })
		if err != nil || count != 1 || metrics != 1 || !reflect.DeepEqual(fixture.steps, []string{"claim", "begin", "probe", "complete"}) {
			t.Fatal("finite admitted probe lost its normal completion", restored, count, err, fixture.steps)
		}
	}
}
