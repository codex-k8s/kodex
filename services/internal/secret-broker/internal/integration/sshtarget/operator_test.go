package sshtarget

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"golang.org/x/crypto/ssh"
)

// operatorFixture creates an encrypted synthetic target and a real readonly canonical deployment artifact pinned to its generated public authentication and host keys.
// operatorFixture создаёт encrypted synthetic target и настоящий readonly canonical deployment artifact с pins к сгенерированным public authentication и host keys.
func operatorFixture(t *testing.T) (*os.File, string, string, Target, []byte, managedssh.TargetQualification) {
	t.Helper()
	_, target, key := newWireFixture(t, &fixtureBehavior{})
	public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(target.HostKey))
	if err != nil {
		t.Fatal(err)
	}
	signer, err := authenticationSigner(key)
	if err != nil {
		t.Fatal(err)
	}
	hostDigest, keyDigest := sha256.Sum256(public.Marshal()), sha256.Sum256(signer.PublicKey().Marshal())
	now := time.Now().Unix()
	qualification := managedssh.TargetQualification{TargetRef: "11111111-1111-4111-8111-111111111111", Address: target.Address, Port: target.Port, User: target.User, Mode: managedssh.ExecuteTransport, HostKeyAlgorithm: managedssh.ED25519HostKey, HostKeyDigest: hex.EncodeToString(hostDigest[:]), AuthenticationKey: hex.EncodeToString(keyDigest[:]), UID: 1001, GID: 1001, Groups: []uint32{1001}, QualifiedAt: now, ValidUntil: now + 3600, DeploymentDigest: strings.Repeat("a", 64), SSHDConfiguration: strings.Repeat("b", 64), IdentityInventory: strings.Repeat("c", 64), RuntimeReadback: strings.Repeat("d", 64), IdentityProbe: strings.Repeat("9", 64)}
	raw, err := managedssh.EncodeQualifications(managedssh.Qualifications{Schema: managedssh.QualificationSchema, Profiles: []managedssh.TargetQualification{qualification}}, now)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "profiles.json")
	if os.WriteFile(name, raw, 0400) != nil {
		t.Fatal("cannot write synthetic operator artifact")
	}
	file, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	digest := sha256.Sum256(raw)
	return file, hex.EncodeToString(digest[:]), qualification.TargetRef, target, key, qualification
}

// TestOperatorArtifactBindsExactOwnerTargetModeAndKeys exercises the actual mounted-file decoder and broker key parser, rejecting foreign coordinates before any command is dispatched.
// TestOperatorArtifactBindsExactOwnerTargetModeAndKeys проверяет actual mounted-file decoder и broker key parser, отклоняя foreign coordinates до отправки команды.
func TestOperatorArtifactBindsExactOwnerTargetModeAndKeys(t *testing.T) {
	file, digest, ref, target, key, expected := operatorFixture(t)
	read := func(ref string, target Target, key []byte, mode managedssh.TransportMode, now int64) error {
		if _, err := file.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		observed, err := readOperatorQualification(file, digest, ref, target, key, mode, now)
		if err == nil && observed.UID != expected.UID {
			t.Fatal("foreign operator identity adopted")
		}
		return err
	}
	if err := read(ref, target, key, managedssh.ExecuteTransport, time.Now().Unix()); err != nil {
		t.Fatal("exact readonly operator artifact rejected")
	}
	if err := read(ref, target, key, managedssh.ManagedTransport, time.Now().Unix()); !errors.Is(err, ErrRejected) {
		t.Fatal("execute identity upgraded to managed")
	}
	foreign := target
	foreign.User = "other_user"
	if err := read(ref, foreign, key, managedssh.ExecuteTransport, time.Now().Unix()); !errors.Is(err, ErrRejected) {
		t.Fatal("foreign account accepted")
	}
	if err := read("22222222-2222-4222-8222-222222222222", target, key, managedssh.ExecuteTransport, time.Now().Unix()); !errors.Is(err, ErrRejected) {
		t.Fatal("foreign owner target accepted")
	}
	_, _, foreignKey := newWireFixture(t, &fixtureBehavior{})
	if err := read(ref, target, foreignKey, managedssh.ExecuteTransport, time.Now().Unix()); !errors.Is(err, ErrRejected) {
		t.Fatal("rotated authentication key accepted without qualification")
	}
	if err := read(ref, target, key, managedssh.ExecuteTransport, expected.ValidUntil); !errors.Is(err, ErrRejected) {
		t.Fatal("expired operator artifact accepted")
	}
	digest = strings.Repeat("a", 64)
	if err := read(ref, target, key, managedssh.ExecuteTransport, time.Now().Unix()); !errors.Is(err, ErrRejected) {
		t.Fatal("mutated deployment bytes accepted")
	}
}

// TestOperatorMissingOrWritableEvidenceNeverReachesSSH proves an absent trusted deployment closes both entrypoints before authentication, and a writable artifact is not a qualified deployment.
// TestOperatorMissingOrWritableEvidenceNeverReachesSSH доказывает отказ обоих entrypoints до authentication при отсутствии trusted deployment; writable artifact не считается qualified deployment.
func TestOperatorMissingOrWritableEvidenceNeverReachesSSH(t *testing.T) {
	behavior := &fixtureBehavior{}
	client, target, key := newWireFixture(t, behavior)
	operator, err := NewOperatorQualifications("", "")
	if err != nil {
		t.Fatal(err)
	}
	_, request := managedFixtureInput()
	if _, err := client.QualifiedManaged(t.Context(), operator, "11111111-1111-4111-8111-111111111111", target, key, ManagedRequest{Wire: request}); !errors.Is(err, ErrRejected) {
		t.Fatal("missing deployment reached helper")
	}
	if _, err := client.QualifiedExecute(t.Context(), operator, "11111111-1111-4111-8111-111111111111", target, key, Command{Text: "fixture-command", MaximumOutput: 1024}); !errors.Is(err, ErrRejected) || behavior.publicKey.Load() != 0 || behavior.commands.Load() != 0 {
		t.Fatal("missing deployment reached SSH")
	}
	file, digest, ref, target, key, _ := operatorFixture(t)
	if file.Chmod(0600) != nil {
		t.Fatal("cannot prepare writable fixture")
	}
	if _, err := readOperatorQualification(file, digest, ref, target, key, managedssh.ExecuteTransport, time.Now().Unix()); !errors.Is(err, ErrRejected) {
		t.Fatal("writable deployment artifact accepted")
	}
	for _, name := range []string{"/tmp/profiles.json", OperatorQualificationFile + "/../profiles.json"} {
		if _, err := NewOperatorQualifications(name, digest); !errors.Is(err, ErrRejected) {
			t.Fatal("caller-selected operator path accepted")
		}
	}
}
