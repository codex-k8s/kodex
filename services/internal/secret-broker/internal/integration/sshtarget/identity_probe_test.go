package sshtarget

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// executeIdentityFixture provides a trusted synthetic identity snapshot only for the private wire test; operator mount and key-binding admission have separate real-file boundary tests.
// executeIdentityFixture задаёт trusted synthetic identity snapshot только для private wire test; operator mount и key-binding admission проверяются отдельными real-file boundary tests.
func executeIdentityFixture() (managedssh.TargetQualification, map[string]fixtureProbeReply) {
	now := time.Now().Unix()
	profile := managedssh.TargetQualification{TargetRef: "ssh_target_example01", Address: "192.0.2.10", Port: 22, User: "fixture", Mode: managedssh.ExecuteTransport, HostKeyAlgorithm: managedssh.ED25519HostKey, HostKeyDigest: strings.Repeat("a", 64), AuthenticationKey: strings.Repeat("b", 64), UID: 1001, GID: 1001, Groups: []uint32{1001, 1002}, QualifiedAt: now, ValidUntil: now + 3600, DeploymentDigest: strings.Repeat("c", 64), SSHDConfiguration: strings.Repeat("d", 64), IdentityInventory: strings.Repeat("e", 64), RuntimeReadback: strings.Repeat("f", 64), IdentityProbe: strings.Repeat("9", 64)}
	return profile, map[string]fixtureProbeReply{numericUIDCommand: {stdout: []byte("1001\n")}, numericRealUIDCommand: {stdout: []byte("1001\n")}, numericGIDCommand: {stdout: []byte("1001\n")}, numericRealGIDCommand: {stdout: []byte("1001\n")}, numericGroupsCommand: {stdout: []byte("1002 1001\n")}}
}

// TestExecuteFreshIdentityUsesSameAuthenticatedConnection proves all fixed numeric probes precede a single business dispatch without reconnecting, interpreting a shell prefix or copying commands into probes.
// TestExecuteFreshIdentityUsesSameAuthenticatedConnection доказывает выполнение всех fixed numeric probes до единственного business dispatch без reconnect, интерпретации shell prefix либо копирования command в probes.
func TestExecuteFreshIdentityUsesSameAuthenticatedConnection(t *testing.T) {
	profile, probes := executeIdentityFixture()
	behavior := &fixtureBehavior{identityReads: probes, stdout: []byte("safe result")}
	client, target, key := newWireFixture(t, behavior)
	result, err := client.run(t.Context(), target, key, Command{Text: "fixture-command", MaximumOutput: 1024}, &profile, nil)
	if err != nil || !result.SessionExited || string(result.Stdout) != "safe result" || behavior.identityCalls.Load() != 5 || behavior.commands.Load() != 1 || behavior.connections.Load() != 1 {
		t.Fatal("fresh identity did not precede the exact effect on one SSH connection")
	}
}

// TestExecuteFreshIdentityRejectsDriftBeforeBusinessDispatch distinguishes definite no-effect probe failures from the later ambiguous business Start, including root aliases, missing exits and bounded response overflow.
// TestExecuteFreshIdentityRejectsDriftBeforeBusinessDispatch отделяет definite no-effect probe failures от позднего ambiguous business Start, включая root aliases, missing exits и bounded response overflow.
func TestExecuteFreshIdentityRejectsDriftBeforeBusinessDispatch(t *testing.T) {
	cases := []struct {
		name    string
		command string
		reply   fixtureProbeReply
	}{
		{name: "root UID", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte("0\n")}},
		{name: "root real UID", command: numericRealUIDCommand, reply: fixtureProbeReply{stdout: []byte("0\n")}},
		{name: "changed UID", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte("1003\n")}},
		{name: "named UID", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte("uid=1001(fixture)\n")}},
		{name: "alias UID", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte("01001\n")}},
		{name: "changed GID", command: numericGIDCommand, reply: fixtureProbeReply{stdout: []byte("1003\n")}},
		{name: "root real GID", command: numericRealGIDCommand, reply: fixtureProbeReply{stdout: []byte("0\n")}},
		{name: "root group", command: numericGroupsCommand, reply: fixtureProbeReply{stdout: []byte("1001 0\n")}},
		{name: "changed groups", command: numericGroupsCommand, reply: fixtureProbeReply{stdout: []byte("1001 1003\n")}},
		{name: "duplicate groups", command: numericGroupsCommand, reply: fixtureProbeReply{stdout: []byte("1001 1001\n")}},
		{name: "stderr", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte("1001\n"), stderr: []byte("synthetic private diagnostic")}},
		{name: "nonzero exit", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte("1001\n"), exit: 1}},
		{name: "missing exit", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte("1001\n"), missing: true}},
		{name: "probe lost ACK", command: numericUIDCommand, reply: fixtureProbeReply{lostACK: true}},
		{name: "overflow", command: numericUIDCommand, reply: fixtureProbeReply{stdout: []byte(strings.Repeat("1", 1025))}},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			profile, probes := executeIdentityFixture()
			probes[item.command] = item.reply
			behavior := &fixtureBehavior{identityReads: probes}
			client, target, key := newWireFixture(t, behavior)
			_, err := client.run(t.Context(), target, key, Command{Text: "fixture-command", MaximumOutput: 1024}, &profile, nil)
			if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || err.Error() != ErrRejected.Error() || behavior.commands.Load() != 0 {
				t.Fatal("identity failure dispatched business effect or leaked diagnostics")
			}
		})
	}
}

// TestExecuteBusinessLostACKAfterFreshIdentityStillUnknown keeps the actual effect's ambiguity intact after successful preflight; the identity reads cannot be used as an acknowledgement of the command.
// TestExecuteBusinessLostACKAfterFreshIdentityStillUnknown сохраняет ambiguity actual effect после successful preflight; identity reads не являются command acknowledgement.
func TestExecuteBusinessLostACKAfterFreshIdentityStillUnknown(t *testing.T) {
	profile, probes := executeIdentityFixture()
	behavior := &fixtureBehavior{identityReads: probes, lostACK: true}
	client, target, key := newWireFixture(t, behavior)
	_, err := client.run(t.Context(), target, key, Command{Text: "fixture-command", MaximumOutput: 1024}, &profile, nil)
	if !errors.Is(err, ErrUnknown) || errors.Is(err, ErrRejected) || behavior.identityCalls.Load() != 5 || behavior.commands.Load() != 1 || behavior.connections.Load() != 1 {
		t.Fatal("fresh identity downgraded an ambiguous business dispatch")
	}
}

// TestExecuteCancelDuringIdentityClosesAndJoinsWithoutBusinessEffect proves the outer operation's cancellation still bounds the fixed preflight sessions and never falls through to execution.
// TestExecuteCancelDuringIdentityClosesAndJoinsWithoutBusinessEffect доказывает bounded fixed preflight sessions при outer cancellation без перехода к execution.
func TestExecuteCancelDuringIdentityClosesAndJoinsWithoutBusinessEffect(t *testing.T) {
	profile, probes := executeIdentityFixture()
	hold := make(chan struct{})
	defer close(hold)
	probes[numericUIDCommand] = fixtureProbeReply{hold: hold}
	behavior := &fixtureBehavior{identityReads: probes}
	client, target, key := newWireFixture(t, behavior)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := client.run(ctx, target, key, Command{Text: "fixture-command", MaximumOutput: 1024}, &profile, nil)
	if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || behavior.commands.Load() != 0 {
		t.Fatal("cancelled identity probe dispatched business command")
	}
}

// TestExecuteInputSnapshotSurvivesSlowIdentityProbe proves the submitted immutable input is captured before any network preflight, so caller-buffer reuse cannot change the later effect bytes.
// TestExecuteInputSnapshotSurvivesSlowIdentityProbe доказывает фиксацию submitted immutable input до network preflight: reuse caller buffer не меняет bytes последующего effect.
func TestExecuteInputSnapshotSurvivesSlowIdentityProbe(t *testing.T) {
	profile, probes := executeIdentityFixture()
	started, release := make(chan struct{}, 1), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	probes[numericUIDCommand] = fixtureProbeReply{stdout: []byte("1001\n"), hold: release, started: started}
	behavior := &fixtureBehavior{identityReads: probes, input: make(chan []byte, 1)}
	client, target, key := newWireFixture(t, behavior)
	input := []byte("immutable")
	finished := make(chan error, 1)
	ctx := t.Context()
	go func() {
		_, err := client.run(ctx, target, key, Command{Text: "fixture-command", Input: input, MaximumOutput: 1024}, &profile, nil)
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("identity probe did not start")
	}
	copy(input, []byte("changed!!"))
	unblock()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal("snapshot fixture execution rejected")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot fixture did not join")
	}
	if string(<-behavior.input) != "immutable" {
		t.Fatal("mutable caller input changed business effect after identity preflight")
	}
}

// TestExecuteFinalQualificationWithdrawalRejectsBeforeStart proves post-authentication evidence withdrawal is still a definite no-effect outcome after all identity reads and channel allocation.
// TestExecuteFinalQualificationWithdrawalRejectsBeforeStart доказывает definite no-effect при withdrawal evidence после authentication, всех identity reads и channel allocation.
func TestExecuteFinalQualificationWithdrawalRejectsBeforeStart(t *testing.T) {
	profile, probes := executeIdentityFixture()
	behavior := &fixtureBehavior{identityReads: probes}
	client, target, key := newWireFixture(t, behavior)
	checked := false
	_, err := client.run(t.Context(), target, key, Command{Text: "fixture-command", MaximumOutput: 1024}, &profile, func() error {
		checked = true
		return ErrRejected
	})
	if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || !checked || behavior.identityCalls.Load() != 5 || behavior.commands.Load() != 0 {
		t.Fatal("withdrawn qualification reached business Start")
	}
}
