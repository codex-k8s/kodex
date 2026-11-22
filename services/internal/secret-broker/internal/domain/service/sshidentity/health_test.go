package sshidentity

import (
	"context"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"strings"
	"testing"
	"time"
)

// healthPortsFixture records only the finite purpose boundary, retaining owned bytes so each test can verify they were cleared after synchronous use.
// healthPortsFixture фиксирует лишь finite purpose boundary и сохраняет owned bytes для проверки очистки после synchronous use.
type healthPortsFixture struct {
	admission                                    managedssh.HealthAdmission
	beginErr, completeErr, errorRead, errorProbe error
	reads, probes, completes                     int
	material                                     []byte
	mismatch                                     bool
	cancel                                       context.CancelFunc
	completionAlive                              bool
	observation                                  managedssh.HealthObservation
}

// BeginSSHIdentityHealthProbe returns one original synthetic admission without granting a target, user or credential supplied by a caller.
// BeginSSHIdentityHealthProbe возвращает original synthetic admission без caller-selected target, user либо credential.
func (f *healthPortsFixture) BeginSSHIdentityHealthProbe(context.Context, managedssh.Binding) (managedssh.HealthAdmission, error) {
	return f.admission, f.beginErr
}

// CompleteSSHIdentityHealthProbe captures independent cleanup liveness and exact joined observation, including deliberately lost acknowledgement.
// CompleteSSHIdentityHealthProbe фиксирует independent cleanup liveness и exact joined observation, включая lost acknowledgement.
func (f *healthPortsFixture) CompleteSSHIdentityHealthProbe(ctx context.Context, b managedssh.Binding, _ managedssh.HealthAdmission, o managedssh.HealthObservation) (managedssh.HealthReceipt, error) {
	f.completes++
	f.completionAlive = ctx.Err() == nil
	f.observation = o
	state := "FAILED"
	if o.ObservedSubjectDigest != "" {
		state = "SUCCEEDED"
	}
	return managedssh.HealthReceipt{TestRef: b.ConnectionTestRef, State: state, Applied: true}, f.completeErr
}

// ReadSSHIdentity returns owned fixture bytes only for the admitted locator and never simulates generic materialization.
// ReadSSHIdentity возвращает owned fixture bytes лишь для admitted locator без generic materialization.
func (f *healthPortsFixture) ReadSSHIdentity(context.Context, value.SSHIdentityLocator) ([]byte, error) {
	f.reads++
	return f.material, f.errorRead
}

// PrepareSSHIdentity supplies deterministic actual-parser metadata in this domain-only fixture; native parser/wire qualification remains separate.
// PrepareSSHIdentity предоставляет deterministic actual-parser metadata в domain-only fixture; native parser/wire qualification проверяется отдельно.
func (f *healthPortsFixture) PrepareSSHIdentity([]byte) (value.SSHIdentityKeyMetadata, error) {
	key := value.SSHIdentityKeyMetadata{ContentSHA256: f.admission.Identity.ContentSHA256, AuthenticationSHA256: f.admission.Identity.AuthenticationSHA256}
	if f.mismatch {
		key.AuthenticationSHA256 = strings.Repeat("f", 64)
	}
	return key, nil
}

// ProbeSSHIdentity returns only after its synchronous read, optionally cancelling the incoming context to exercise independent owner completion.
// ProbeSSHIdentity возвращается лишь после synchronous read и может отменить incoming context для проверки independent owner completion.
func (f *healthPortsFixture) ProbeSSHIdentity(context.Context, managedssh.HealthAdmission, []byte) (managedssh.HealthObservation, error) {
	f.probes++
	if f.cancel != nil {
		f.cancel()
	}
	return managedssh.HealthObservation{ObservedSubjectDigest: strings.Repeat("c", 64), QualifiedUntil: time.Now().Add(time.Hour).Truncate(time.Second)}, f.errorProbe
}

// newHealthPortsFixture creates synthetic metadata only and never claims actual key parsing, storage, SSH reachability or owner persistence.
// newHealthPortsFixture создаёт лишь synthetic metadata без утверждений actual key parsing, storage, SSH reachability либо owner persistence.
func newHealthPortsFixture() *healthPortsFixture {
	identity := managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: "identity_fixture", TargetRef: "target_fixture", Revision: 1, Namespace: "kodex-ssh-identities", SecretUID: "uid_fixture", SecretResourceVersion: "rv_fixture", ContentSHA256: strings.Repeat("a", 64), AuthenticationSHA256: strings.Repeat("b", 64)}
	identity.SecretName = managedssh.IdentitySecretName(identity.IdentityRevisionRef, 1)
	return &healthPortsFixture{admission: managedssh.HealthAdmission{AttemptRef: "probe_fixture", SnapshotDigest: strings.Repeat("d", 64), Identity: identity, Mode: managedssh.ExecuteTransport, ExpiresAt: time.Now().Add(20 * time.Second)}, material: []byte("synthetic owned test bytes")}
}

// TestSSHHealthServiceRetainsJoinedCompletion verifies cancellation, lost completion ACK, wrong material and uncertain reads never resend or fabricate authentication, and owned bytes are cleared.
// TestSSHHealthServiceRetainsJoinedCompletion проверяет cancel, lost completion ACK, wrong material и uncertain reads без повторной отправки либо fabricated authentication; owned bytes очищаются.
func TestSSHHealthServiceRetainsJoinedCompletion(t *testing.T) {
	for _, scenario := range []string{"success", "cancelled", "complete-lost", "wrong-material", "read-unavailable", "probe-unavailable", "begin-lost"} {
		t.Run(scenario, func(t *testing.T) {
			f := newHealthPortsFixture()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch scenario {
			case "cancelled":
				f.cancel = cancel
			case "complete-lost":
				f.completeErr = errors.New("synthetic lost owner ACK")
			case "wrong-material":
				f.mismatch = true
			case "read-unavailable":
				f.errorRead = errors.New("synthetic read unavailable")
			case "probe-unavailable":
				f.errorProbe = errors.New("synthetic SSH failure")
			case "begin-lost":
				f.beginErr = errors.New("synthetic lost Begin ACK")
			}
			service, err := NewHealthService(f, f, f, f)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := service.Probe(ctx, managedssh.Binding{WorkloadInstance: "gateway_fixture", ConnectionTestRef: "test_fixture", LeaseRef: "lease_fixture", Fence: "fence_fixture", Generation: 1, InputDigest: strings.Repeat("e", 64)})
			if scenario == "begin-lost" {
				if err == nil || f.reads != 0 || f.probes != 0 || f.completes != 0 {
					t.Fatal("lost Begin reached material or completion")
				}
				return
			}
			if f.completes != 1 || !f.completionAlive {
				t.Fatal("completion replayed or used cancelled context")
			}
			for _, b := range f.material {
				if b != 0 {
					t.Fatal("private buffer retained")
				}
			}
			if scenario == "complete-lost" {
				if err == nil || receipt.Applied || f.probes != 1 {
					t.Fatal("lost Complete became success or replay")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			authenticated := scenario == "success"
			if (receipt.State == "SUCCEEDED") != authenticated {
				t.Fatal("unjoined or mismatched probe became authentication")
			}
			if (scenario == "wrong-material" || scenario == "read-unavailable") && f.probes != 0 {
				t.Fatal("invalid material reached SSH")
			}
		})
	}
}
