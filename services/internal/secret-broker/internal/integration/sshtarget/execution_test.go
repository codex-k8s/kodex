package sshtarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// sshExecutionWireAdmission binds synthetic owner metadata to the real wire fixture's ephemeral key and complete operator profile; it does not emulate owner PostgreSQL authority.
// sshExecutionWireAdmission связывает synthetic owner metadata с ephemeral key real wire fixture и complete operator profile; owner PostgreSQL authority не имитируется.
func sshExecutionWireAdmission(t *testing.T, target Target, profile managedssh.TargetQualification, key []byte, operation string, raw []byte) (managedssh.ExecutionAdmission, managedssh.TargetQualification) {
	t.Helper()
	metadata, err := PrepareCredential(key)
	if err != nil {
		t.Fatal("prepare isolated SSH key")
	}
	pin, err := target.hostPin()
	if err != nil {
		t.Fatal("resolve isolated SSH host pin")
	}
	profile.HostKeyAlgorithm, profile.HostKeyDigest, profile.AuthenticationKey = pin.algorithm, hex.EncodeToString(pin.digest[:]), metadata.AuthenticationSHA256
	input, err := managedssh.DecodeOperationInput(operation, raw)
	if err != nil {
		t.Fatal("decode bounded execution fixture input")
	}
	defer input.Clear()
	inputDigest, _ := input.Digest()
	profileDigest, _ := profile.Digest()
	identity := managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: "ssh_identity_fixture", TargetRef: profile.TargetRef, Revision: 1, Namespace: "kodex-ssh-identities", SecretUID: "uid_fixture", SecretResourceVersion: "rv_fixture", ContentSHA256: metadata.ContentSHA256, AuthenticationSHA256: metadata.AuthenticationSHA256}
	identity.SecretName = managedssh.IdentitySecretName(identity.IdentityRevisionRef, identity.Revision)
	return managedssh.ExecutionAdmission{AttemptRef: "ssh_execution_fixture", InvocationRef: "invocation_ssh_fixture", SnapshotDigest: strings.Repeat("c", 64), Operation: operation, CanonicalInput: bytes.Clone(raw), InputDigest: inputDigest, SubjectDigest: profileDigest, Identity: identity, Mode: profile.Mode, ExpiresAt: time.Now().Add(20 * time.Second)}, profile
}

// TestSSHExecutionExecutePreservesOneDispatch runs the actual encrypted numeric identity preflight and one exact business command; lost acknowledgements retain UNKNOWN and never replay even during explicit recovery.
// TestSSHExecutionExecutePreservesOneDispatch выполняет actual encrypted numeric identity preflight и один exact business command; lost acknowledgement сохраняет UNKNOWN без replay даже при explicit recovery.
func TestSSHExecutionExecutePreservesOneDispatch(t *testing.T) {
	for _, variant := range []string{"success", "failed-exit", "lost-ack", "withdrawn", "changed-health"} {
		t.Run(variant, func(t *testing.T) {
			profile, probes := executeIdentityFixture()
			behavior := &fixtureBehavior{command: "fixture-command", identityReads: probes, stdout: []byte("bounded result"), lostACK: variant == "lost-ack"}
			if variant == "failed-exit" {
				behavior.exit = 3
			}
			client, target, key := newWireFixture(t, behavior)
			admission, profile := sshExecutionWireAdmission(t, target, profile, key, "ssh.execute", []byte(`{"command":"fixture-command","maximum_output":1024}`))
			if variant == "changed-health" {
				admission.SubjectDigest = strings.Repeat("e", 64)
			}
			refreshes := 0
			refresh := func() error {
				refreshes++
				if variant == "withdrawn" && refreshes == 2 {
					return ErrRejected
				}
				return nil
			}
			observation, err := client.executeIdentity(t.Context(), admission, target, profile, key, refresh)
			if variant == "changed-health" {
				if err == nil || behavior.connections.Load() != 0 {
					t.Fatal("changed health source reached SSH")
				}
				return
			}
			if err != nil || observation.Validate() != nil {
				t.Fatal("qualified execution observation rejected")
			}
			want := managedssh.ExecutionSucceeded
			if variant == "failed-exit" || variant == "withdrawn" {
				want = managedssh.ExecutionFailed
			} else if variant == "lost-ack" {
				want = managedssh.ExecutionUnknown
			}
			commands := int32(1)
			if variant == "withdrawn" {
				commands = 0
			}
			if observation.Outcome != want || behavior.commands.Load() != commands || behavior.connections.Load() != 1 {
				t.Fatal("execution replayed or changed outcome")
			}
			if variant == "lost-ack" {
				admission.Recover = true
				recovered, err := client.executeIdentity(t.Context(), admission, target, profile, key, refresh)
				if err != nil || recovered.Outcome != managedssh.ExecutionUnknown || len(recovered.Result) != 0 || behavior.commands.Load() != 1 || behavior.connections.Load() != 1 {
					t.Fatal("arbitrary execution recovery resent or invented output")
				}
			}
		})
	}
}

// TestSSHExecutionManagedRecoveryReadsExactReceipt exercises the real encrypted helper decoder for an admitted file change and exact read-only reconciliation, including a failed observation that must remain UNKNOWN.
// TestSSHExecutionManagedRecoveryReadsExactReceipt проверяет real encrypted helper decoder для admitted file change и exact read-only reconciliation, включая failed observation, сохраняющий UNKNOWN.
func TestSSHExecutionManagedRecoveryReadsExactReceipt(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, lost := range []bool{false, true} {
			helper, _ := managedFixtureInput()
			profile, _ := executeIdentityFixture()
			profile.Mode, profile.Profile, profile.Groups, profile.IdentityProbe = managedssh.ManagedTransport, &helper, helper.Groups, ""
			behavior := &fixtureBehavior{command: managedHelperCommand, lostACK: lost, input: make(chan []byte, 1)}
			client, target, key := newWireFixture(t, behavior)
			admission, profile := sshExecutionWireAdmission(t, target, profile, key, "ssh.file.create", []byte(`{"content_base64":"c2FmZQ==","path":"example.txt","resource":"documents"}`))
			admission.Recover = recovery
			input, _ := managedssh.DecodeOperationInput(admission.Operation, admission.CanonicalInput)
			request, err := sshExecutionManagedRequest(admission, input, helper)
			input.Clear()
			if err != nil {
				t.Fatal("build exact helper request")
			}
			digest, _ := managedssh.RequestDigest(request.Wire)
			status := managedssh.Applied
			originalOperation := request.Wire.Operation
			if recovery {
				status, digest, originalOperation = managedssh.Observed, request.Recovery.Digest, request.Recovery.Operation
			}
			reply, _ := managedssh.NewReply(request.Wire, status)
			reply.Receipt = &managedssh.Receipt{AttemptID: request.Wire.AttemptID, RequestDigest: digest, Operation: originalOperation, Status: managedssh.Applied}
			behavior.stdout = managedReply(t, request.Wire, reply)
			observation, err := client.executeIdentity(t.Context(), admission, target, profile, key, func() error { return nil })
			if err != nil || observation.Validate() != nil || behavior.commands.Load() != 1 || behavior.connections.Load() != 1 {
				t.Fatal("managed execution lost joined original lineage")
			}
			if lost {
				if observation.Outcome != managedssh.ExecutionUnknown || len(observation.Result) != 0 {
					t.Fatal("lost managed acknowledgement became a known outcome")
				}
				continue
			}
			if observation.Outcome != managedssh.ExecutionSucceeded {
				t.Fatal("exact managed receipt was not observed")
			}
			var sent map[string]json.RawMessage
			if json.Unmarshal(<-behavior.input, &sent) != nil {
				t.Fatal("capture helper request")
			}
			var operation managedssh.Operation
			_ = json.Unmarshal(sent["operation"], &operation)
			if recovery && operation != managedssh.ReceiptRead || !recovery && operation != managedssh.FileCreate {
				t.Fatal("reconciliation resent the original mutation")
			}
		}
	}
}

// runSSHExecutionFixedMountFixture reexecutes this exact test in private namespaces with the production artifact path and loopback-only networking; it never mounts or writes a host runtime directory.
// runSSHExecutionFixedMountFixture повторяет exact test в private namespaces с production artifact path и loopback-only network; host runtime directory не монтируется и не изменяется.
func runSSHExecutionFixedMountFixture(t *testing.T) bool {
	t.Helper()
	const childMarker = "KODEX_SSH_EXECUTION_FIXED_MOUNT_CHILD"
	if os.Getenv(childMarker) == "1" {
		return false
	}
	bubblewrap, err := exec.LookPath("bwrap")
	if err != nil {
		t.Fatal("isolated fixed-mount SSH fixture requires bubblewrap")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("resolve isolated SSH test executable")
	}
	logPath := filepath.Join(t.TempDir(), "fixed-mount.log")
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("create private fixed-mount SSH diagnostic")
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	arguments := []string{"--unshare-user", "--unshare-pid", "--unshare-net", "--unshare-ipc", "--unshare-uts", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv",
		"--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64",
		"--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run", "--dir", "/var/run/kodex", "--perms", "0700", "--dir", managedssh.OperatorQualificationMount,
		"--ro-bind", binary, "/run/ssh-execution.test", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "TMPDIR", "/tmp", "--setenv", childMarker, "1",
		"/run/ssh-execution.test", "-test.run=^TestSSHExecutionOperatorAndOwnerCheckPrecedeNativeStart$", "-test.timeout=25s", "-test.v"}
	command := exec.CommandContext(ctx, bubblewrap, arguments...)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Stdout, command.Stderr = log, log
	if err := command.Run(); err != nil {
		_, _ = log.Seek(0, io.SeekStart)
		diagnostic, _ := io.ReadAll(io.LimitReader(log, 16384))
		t.Fatalf("isolated fixed-mount SSH fixture failed: %v; bounded synthetic diagnostic: %q", err, diagnostic)
	}
	return true
}

// TestSSHExecutionOperatorAndOwnerCheckPrecedeNativeStart combines the real immutable artifact parser, generated key pins, encrypted numeric probes and a final owner check; owner infrastructure failure remains an error without dispatch or completion fabrication.
// TestSSHExecutionOperatorAndOwnerCheckPrecedeNativeStart объединяет real immutable artifact parser, generated key pins, encrypted numeric probes и final owner check; owner infrastructure failure остаётся ошибкой без dispatch либо придуманного completion.
func TestSSHExecutionOperatorAndOwnerCheckPrecedeNativeStart(t *testing.T) {
	if runSSHExecutionFixedMountFixture(t) {
		return
	}
	for _, mode := range []string{"success", "owner-unavailable", "owner-refused"} {
		t.Run(mode, func(t *testing.T) {
			profile, probes := executeIdentityFixture()
			behavior := &fixtureBehavior{command: "fixture-command", identityReads: probes, stdout: []byte("bounded result")}
			client, target, key := newWireFixture(t, behavior)
			admission, profile := sshExecutionWireAdmission(t, target, profile, key, "ssh.execute", []byte(`{"command":"fixture-command","maximum_output":1024}`))
			raw, err := managedssh.EncodeQualifications(managedssh.Qualifications{Schema: managedssh.QualificationSchema, Profiles: []managedssh.TargetQualification{profile}}, time.Now().Unix())
			if err != nil {
				t.Fatal(err)
			}
			path := OperatorQualificationFile
			t.Cleanup(func() { _ = os.Remove(path) })
			if err := os.WriteFile(path, raw, 0400); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			operator, err := NewOperatorQualifications(path, hex.EncodeToString(sum[:]))
			if err != nil {
				t.Fatal(err)
			}
			executor := IdentityExecutor{Client: client, Operator: operator}
			authorityFailure := errors.New("synthetic owner unavailable")
			checks := 0
			observation, err := executor.ExecuteSSHIdentity(t.Context(), admission, key, func(ctx context.Context) error {
				checks++
				if ctx.Err() != nil {
					t.Fatal("owner check lost operation context")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("owner check is unbounded")
				}
				if checks == 2 {
					if mode == "owner-unavailable" {
						return authorityFailure
					}
					if mode == "owner-refused" {
						return ErrRejected
					}
				}
				return nil
			})
			if mode == "success" {
				if err != nil || observation.Outcome != managedssh.ExecutionSucceeded || observation.Validate() != nil || checks != 2 || behavior.commands.Load() != 1 || behavior.connections.Load() != 1 {
					t.Fatal("actual composed execution failed", err)
				}
			} else {
				expected := authorityFailure
				if mode == "owner-refused" {
					expected = ErrRejected
				}
				if !errors.Is(err, expected) || checks != 2 || behavior.commands.Load() != 0 || behavior.identityCalls.Load() != 5 || behavior.connections.Load() != 1 || len(observation.Result) != 0 {
					t.Fatal("owner check failed to fence native Start", err)
				}
			}
		})
	}
}
