package sshtarget

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// managedFixtureInput defines public synthetic metadata only; the real wire fixture still creates and authenticates its own unprinted ephemeral keys.
// managedFixtureInput задаёт только public synthetic metadata; real wire fixture создаёт и проверяет собственные непечатаемые ephemeral keys.
func managedFixtureInput() (managedssh.Profile, managedssh.Request) {
	profile := managedssh.Profile{Version: managedssh.Version, Revision: 1, ValidUntil: time.Now().Add(time.Hour).Unix(), UID: 1001, GID: 1001, Groups: []uint32{1001}, ExecutableDigest: strings.Repeat("b", 64), ShellDigest: strings.Repeat("8", 64), StateDirectory: "/srv/kodex/receipts", ReceiptLimit: 128, Roots: []managedssh.RootScope{{Alias: "documents", Path: "/srv/kodex/documents"}}, Services: []managedssh.ServiceScope{}}
	digest, _ := profile.Digest()
	request := managedssh.Request{Version: managedssh.Version, ProfileDigest: digest, AttemptID: "33333333-3333-4333-8333-333333333333", Operation: managedssh.Inspect}
	return profile, request
}

// managedReply serializes a complete production protocol result before supplying it to the encrypted test peer, avoiding a parallel decoder or schema-only proof.
// managedReply сериализует полный production protocol result перед передачей encrypted test peer без parallel decoder либо schema-only proof.
func managedReply(t *testing.T, request managedssh.Request, reply managedssh.Reply) []byte {
	t.Helper()
	raw, err := managedssh.EncodeReply(request, reply)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestManagedUsesOnlyFixedHelperAndCanonicalInput verifies actual encrypted SSH preserves exact profile/request pins and admits the expected observed non-root identity.
// TestManagedUsesOnlyFixedHelperAndCanonicalInput проверяет exact profile/request pins в actual encrypted SSH и допускает ожидаемую observed non-root identity.
func TestManagedUsesOnlyFixedHelperAndCanonicalInput(t *testing.T) {
	profile, request := managedFixtureInput()
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	reply.Identity = &managedssh.Identity{UID: profile.UID, GID: profile.GID, Groups: profile.Groups, Executable: profile.ExecutableDigest}
	behavior := &fixtureBehavior{command: managedHelperCommand, stdout: managedReply(t, request, reply), input: make(chan []byte, 1)}
	client, target, key := newWireFixture(t, behavior)
	observed, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request})
	if err != nil || observed.Status != managedssh.Observed || observed.Identity.UID != profile.UID {
		t.Fatal("exact managed identity rejected")
	}
	expected, _ := managedssh.EncodeRequest(request)
	if !bytes.Equal(<-behavior.input, expected) || behavior.commands.Load() != 1 {
		t.Fatal("managed transport changed request or replayed")
	}
}

// TestManagedQualificationWithdrawalStopsBeforeHelperStart proves the qualified path can revoke a previously observed deployment after SSH authentication without dispatching even the fixed helper command.
// TestManagedQualificationWithdrawalStopsBeforeHelperStart доказывает отзыв ранее observed deployment после SSH authentication без dispatch даже fixed helper command.
func TestManagedQualificationWithdrawalStopsBeforeHelperStart(t *testing.T) {
	profile, request := managedFixtureInput()
	behavior := &fixtureBehavior{command: managedHelperCommand}
	client, target, key := newWireFixture(t, behavior)
	checked := false
	_, err := client.managed(t.Context(), target, key, profile, ManagedRequest{Wire: request}, func() error {
		checked = true
		return ErrRejected
	})
	if !errors.Is(err, ErrRejected) || errors.Is(err, ErrUnknown) || !checked || behavior.publicKey.Load() == 0 || behavior.commands.Load() != 0 {
		t.Fatal("withdrawn qualification dispatched the managed helper")
	}
}

// TestManagedRejectsUnselectedScopeBeforeAuthentication proves resource/profile expiry cannot turn a helper recipe into authority over another alias or target.
// TestManagedRejectsUnselectedScopeBeforeAuthentication доказывает, что resource/profile expiry не выдаёт helper recipe полномочий на другой alias либо target.
func TestManagedRejectsUnselectedScopeBeforeAuthentication(t *testing.T) {
	profile, request := managedFixtureInput()
	behavior := &fixtureBehavior{command: managedHelperCommand}
	client, target, key := newWireFixture(t, behavior)
	request.Operation, request.Resource, request.Path, request.Content = managedssh.FileCreate, "unselected", "file", []byte("refused")
	if _, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request}); !errors.Is(err, ErrRejected) {
		t.Fatal("foreign managed scope admitted")
	}
	request.Resource = "documents"
	profile.ValidUntil = 1
	request.ProfileDigest, _ = profile.Digest()
	if _, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request}); !errors.Is(err, ErrRejected) || behavior.publicKey.Load() != 0 || behavior.commands.Load() != 0 {
		t.Fatal("expired scope reached SSH authentication")
	}
}

// TestManagedRejectsValidButForeignObservedIdentity ensures successful SSH authentication and a syntactically valid helper reply cannot replace operator-qualified UID pins.
// TestManagedRejectsValidButForeignObservedIdentity проверяет, что успешная SSH authentication и синтаксически valid helper reply не заменяют operator-qualified UID pins.
func TestManagedRejectsValidButForeignObservedIdentity(t *testing.T) {
	profile, request := managedFixtureInput()
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	reply.Identity = &managedssh.Identity{UID: profile.UID + 1, GID: profile.GID, Groups: profile.Groups, Executable: profile.ExecutableDigest}
	behavior := &fixtureBehavior{command: managedHelperCommand, stdout: managedReply(t, request, reply)}
	client, target, key := newWireFixture(t, behavior)
	if _, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request}); !errors.Is(err, ErrUnknown) || errors.Is(err, ErrRejected) {
		t.Fatal("foreign observed identity adopted")
	}
}

// TestManagedRecoveryPinsOriginalEffectWithoutReplay observes an UNKNOWN intent only after matching owner-held original digest/operation and never sends the original write frame.
// TestManagedRecoveryPinsOriginalEffectWithoutReplay читает UNKNOWN intent только после сверки original digest/operation владельца и не отправляет исходный write frame.
func TestManagedRecoveryPinsOriginalEffectWithoutReplay(t *testing.T) {
	profile, request := managedFixtureInput()
	request.Operation = managedssh.ReceiptRead
	original := strings.Repeat("c", 64)
	reply, _ := managedssh.NewReply(request, managedssh.Observed)
	reply.Receipt = &managedssh.Receipt{AttemptID: request.AttemptID, RequestDigest: original, Operation: managedssh.FileCreate, Status: managedssh.Unknown}
	behavior := &fixtureBehavior{command: managedHelperCommand, stdout: managedReply(t, request, reply), input: make(chan []byte, 2)}
	client, target, key := newWireFixture(t, behavior)
	if _, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request}); !errors.Is(err, ErrRejected) || behavior.commands.Load() != 0 {
		t.Fatal("recovery omitted original owner pins")
	}
	expectation := &RecoveryExpectation{Operation: managedssh.FileCreate, Digest: original}
	observed, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request, Recovery: expectation})
	if err != nil || observed.Receipt.Status != managedssh.Unknown {
		t.Fatal("recovery changed ambiguous outcome")
	}
	wire, err := managedssh.DecodeRequest(<-behavior.input)
	if err != nil || wire.Operation != managedssh.ReceiptRead || behavior.commands.Load() != 1 {
		t.Fatal("recovery replayed the original effect")
	}
	expectation.Digest = strings.Repeat("d", 64)
	if _, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request, Recovery: expectation}); !errors.Is(err, ErrUnknown) {
		t.Fatal("foreign original digest adopted")
	}
}

// TestManagedLostAcknowledgementRemainsUnknown proves the managed wrapper preserves the SSH Start ambiguity instead of interpreting a missing frame as a known non-effect.
// TestManagedLostAcknowledgementRemainsUnknown доказывает сохранение SSH Start ambiguity managed wrapper без трактовки отсутствующего frame как известного отсутствия effect.
func TestManagedLostAcknowledgementRemainsUnknown(t *testing.T) {
	profile, request := managedFixtureInput()
	request.Operation, request.Resource, request.Path, request.Content = managedssh.FileCreate, "documents", "file", []byte("synthetic")
	behavior := &fixtureBehavior{command: managedHelperCommand, lostACK: true}
	client, target, key := newWireFixture(t, behavior)
	if _, err := client.Managed(t.Context(), target, key, profile, ManagedRequest{Wire: request}); !errors.Is(err, ErrUnknown) || errors.Is(err, ErrRejected) || behavior.commands.Load() != 1 {
		t.Fatal("managed lost ACK lost its committed outcome class")
	}
}
