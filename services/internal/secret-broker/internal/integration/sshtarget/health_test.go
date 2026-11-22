package sshtarget

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"testing"
)

// TestSSHHealthExecuteUsesOnlyFixedReads runs actual encrypted SSH with pinned keys, rejecting identity drift without ever sending an employee command.
// TestSSHHealthExecuteUsesOnlyFixedReads выполняет actual encrypted SSH с pinned keys и отвергает identity drift без employee command.
func TestSSHHealthExecuteUsesOnlyFixedReads(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		profile, probes := executeIdentityFixture()
		if wrong {
			probes[numericGIDCommand] = fixtureProbeReply{stdout: []byte("0\n")}
		}
		behavior := &fixtureBehavior{identityReads: probes}
		client, target, key := newWireFixture(t, behavior)
		pin, err := target.hostPin()
		if err != nil {
			t.Fatal(err)
		}
		profile.HostKeyDigest = hex.EncodeToString(pin.digest[:])
		profile.HostKeyAlgorithm = pin.algorithm
		signer, err := authenticationSigner(key)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(signer.PublicKey().Marshal())
		profile.AuthenticationKey = hex.EncodeToString(sum[:])
		refreshCalls := 0
		observation, err := client.probeIdentity(t.Context(), target, key, profile, "health_attempt_fixture", func() error { refreshCalls++; return nil })
		if wrong {
			if err == nil || observation.ObservedSubjectDigest != "" || behavior.commands.Load() != 0 {
				t.Fatal("changed numeric identity became authentication")
			}
			continue
		}
		if err != nil || !managedssh.Digest(observation.ObservedSubjectDigest) || observation.QualifiedUntil.Unix() != profile.ValidUntil || behavior.connections.Load() != 1 || behavior.identityCalls.Load() != 6 || behavior.commands.Load() != 0 || refreshCalls != 2 {
			t.Fatalf("fixed joined identity read failed: %v, identity=%d command=%d refresh=%d", err, behavior.identityCalls.Load(), behavior.commands.Load(), refreshCalls)
		}
	}
}

// TestSSHHealthRevokedAfterReadCannotAuthenticate checks a fresh operator read after SSH joins, preserving unavailable rather than inferring a rejected credential.
// TestSSHHealthRevokedAfterReadCannotAuthenticate проверяет fresh operator read после SSH join и сохраняет unavailable без вывода о rejected credential.
func TestSSHHealthRevokedAfterReadCannotAuthenticate(t *testing.T) {
	profile, probes := executeIdentityFixture()
	behavior := &fixtureBehavior{identityReads: probes}
	client, target, key := newWireFixture(t, behavior)
	calls := 0
	observation, err := client.probeIdentity(t.Context(), target, key, profile, "health_attempt_fixture", func() error {
		calls++
		if calls == 2 {
			return errors.New("synthetic revoked operator")
		}
		return nil
	})
	if err == nil || observation.ObservedSubjectDigest != "" || calls != 2 || behavior.commands.Load() != 0 {
		t.Fatal("late revoked operator produced authentication")
	}
}

// TestSSHHealthManagedUsesOnlyInstalledInspect exercises the real encrypted helper protocol and joins exact UID/executable evidence; a foreign reply cannot establish authentication.
// TestSSHHealthManagedUsesOnlyInstalledInspect проверяет real encrypted helper protocol и exact UID/executable evidence; foreign reply не подтверждает authentication.
func TestSSHHealthManagedUsesOnlyInstalledInspect(t *testing.T) {
	profile, request := managedFixtureInput()
	qualification, _ := executeIdentityFixture()
	qualification.Mode = managedssh.ManagedTransport
	qualification.Profile = &profile
	qualification.Groups = profile.Groups
	qualification.IdentityProbe = ""
	request.AttemptID = "6cb159c0-0633-458c-bab7-61833f2935c6"
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	reply.Identity = &managedssh.Identity{UID: profile.UID, GID: profile.GID, Groups: profile.Groups, Executable: profile.ExecutableDigest}
	behavior := &fixtureBehavior{command: managedHelperCommand, stdout: managedReply(t, request, reply)}
	client, target, key := newWireFixture(t, behavior)
	observation, err := client.probeIdentity(t.Context(), target, key, qualification, "health_attempt_fixture", func() error { return nil })
	if err != nil || !managedssh.Digest(observation.ObservedSubjectDigest) || behavior.commands.Load() != 1 || behavior.identityCalls.Load() != 0 {
		t.Fatal("managed health did not use exactly one installed Inspect", err)
	}
}
