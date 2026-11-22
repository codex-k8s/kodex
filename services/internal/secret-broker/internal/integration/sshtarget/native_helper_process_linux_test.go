package sshtarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

// TestSSHNativeHelperProcessLifecycle connects the existing encrypted SSH peer to the actual helper filesystem/receipt runtime in independent processes; it does not attest an installed host or substitute for PostgreSQL owner admission.
// TestSSHNativeHelperProcessLifecycle соединяет existing encrypted SSH peer с actual helper filesystem/receipt runtime в отдельных processes; installed host и PostgreSQL owner admission не подменяются.
func TestSSHNativeHelperProcessLifecycle(t *testing.T) {
	binary := os.Getenv("KODEX_SSH_HELPER_PROCESS_BINARY")
	if binary == "" && os.Getenv("KODEX_SSH_HELPER_PROCESS_REQUIRED") != "1" {
		t.Skip("explicit helper process fixture is not configured")
	}
	file, err := os.Open(binary)
	if err != nil {
		t.Fatal("helper process fixture is required")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0500 || info.Size() < 1 || info.Size() > 64<<20 {
		t.Fatal("helper process fixture identity is invalid")
	}
	rawBinary, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal("read helper process fixture")
	}
	sum := sha256.Sum256(rawBinary)
	clear(rawBinary)
	if hex.EncodeToString(sum[:]) != os.Getenv("KODEX_SSH_HELPER_PROCESS_SHA256") {
		t.Fatal("helper process fixture digest changed")
	}
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "known", true: "lost-response"}[lost], func(t *testing.T) {
			base, err := os.MkdirTemp(t.TempDir(), "kodex-ssh-transport-")
			if err != nil || os.Chmod(base, 0700) != nil {
				t.Fatal("create private helper transport fixture")
			}
			for _, name := range []string{"documents", "receipts"} {
				if os.Mkdir(filepath.Join(base, name), 0700) != nil {
					t.Fatal("create private helper namespaces")
				}
			}
			helper, _ := managedFixtureInput()
			helper.UID, helper.GID, helper.Groups = uint32(os.Getuid()), uint32(os.Getgid()), []uint32{}
			helper.ExecutableDigest = hex.EncodeToString(sum[:])
			helper.StateDirectory, helper.Roots = filepath.Join(base, "receipts"), []managedssh.RootScope{{Alias: "documents", Path: filepath.Join(base, "documents")}}
			encoded, err := managedssh.EncodeProfile(helper)
			if err != nil || os.WriteFile(filepath.Join(base, "profile.json"), encoded, 0400) != nil {
				t.Fatal("write bounded private helper profile")
			}
			var mutex sync.Mutex
			counts := make(map[managedssh.Operation]int)
			failures := make(chan bool, 16)
			behavior := &fixtureBehavior{command: managedHelperCommand}
			behavior.execute = func(input []byte) fixtureExecutionResult {
				request, err := managedssh.DecodeRequest(input)
				if err != nil {
					failures <- true
					return fixtureExecutionResult{exit: 1}
				}
				mutex.Lock()
				counts[request.Operation]++
				mutex.Unlock()
				ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
				defer cancel()
				child := exec.CommandContext(ctx, binary)
				child.Env = []string{"KODEX_MANAGED_SSH_TEST_ROOT=" + base, "TMPDIR=" + base}
				child.Stdin = bytes.NewReader(input)
				capture := &outputCapture{maximum: managedssh.MaximumReply, close: func() error { cancel(); return nil }}
				child.Stdout, child.Stderr = captureWriter{capture: capture}, captureWriter{capture: capture, stderr: true}
				err = child.Run()
				output := capture.result()
				if err != nil || output.Truncated || len(output.Stderr) != 0 {
					clear(output.Stdout)
					clear(output.Stderr)
					failures <- true
					return fixtureExecutionResult{exit: 1}
				}
				return fixtureExecutionResult{stdout: output.Stdout, dropResponse: lost && request.Operation == managedssh.FileCreate}
			}
			client, target, key := newWireFixture(t, behavior)
			profile, _ := executeIdentityFixture()
			profile.Mode, profile.Profile, profile.UID, profile.GID, profile.Groups, profile.IdentityProbe = managedssh.ManagedTransport, &helper, helper.UID, helper.GID, helper.Groups, ""
			original := []byte(`{"content_base64":"Zmlyc3Q=","path":"report.txt","resource":"documents"}`)
			admission, profile := sshExecutionWireAdmission(t, target, profile, key, "ssh.file.create", original)
			admission.InvocationRef = "inv_native_helper_create"
			observed, err := client.executeIdentity(t.Context(), admission, target, profile, key, func() error { return nil })
			want := managedssh.ExecutionSucceeded
			if lost {
				want = managedssh.ExecutionUnknown
			}
			if err != nil || observed.Outcome != want || observed.Validate() != nil {
				t.Fatal("real helper create lost its classified outcome")
			}
			clear(observed.Result)
			content, err := os.ReadFile(filepath.Join(base, "documents", "report.txt"))
			if err != nil || string(content) != "first" {
				t.Fatal("real helper did not persist the exact file")
			}
			if lost {
				recovery, current := sshExecutionWireAdmission(t, target, profile, key, "ssh.receipt.read", []byte(`{"receipt_invocation_ref":"inv_native_helper_create"}`))
				recovery.InvocationRef = "inv_native_helper_recovery"
				recovery.ReceiptSource = &managedssh.ManagedReceiptSource{InvocationRef: admission.InvocationRef, Operation: admission.Operation, CanonicalInput: bytes.Clone(original)}
				observed, err = client.executeIdentity(t.Context(), recovery, target, current, key, func() error { return nil })
				if err != nil || observed.Outcome != managedssh.ExecutionSucceeded || observed.Validate() != nil {
					t.Fatal("new read-only invocation failed to recover the real durable receipt")
				}
				clear(recovery.ReceiptSource.CanonicalInput)
				clear(observed.Result)
			}
			version := sha256.Sum256([]byte("first"))
			read, current := sshExecutionWireAdmission(t, target, profile, key, "ssh.file.read", []byte(`{"expected":"`+hex.EncodeToString(version[:])+`","limit":1024,"path":"report.txt","resource":"documents"}`))
			read.InvocationRef = "inv_native_helper_read"
			observed, err = client.executeIdentity(t.Context(), read, target, current, key, func() error { return nil })
			if err != nil || observed.Outcome != managedssh.ExecutionSucceeded || !bytes.Contains(observed.Result, []byte("Zmlyc3Q=")) {
				t.Fatal("real helper read lost its exact version-bound content")
			}
			clear(observed.Result)
			revoked, current := sshExecutionWireAdmission(t, target, profile, key, "ssh.file.create", []byte(`{"content_base64":"c2Vjb25k","path":"revoked.txt","resource":"documents"}`))
			revoked.InvocationRef = "inv_native_helper_revoked"
			checks := 0
			observed, err = client.executeIdentity(t.Context(), revoked, target, current, key, func() error {
				checks++
				if checks > 1 {
					return errors.New("synthetic owner admission was revoked")
				}
				return nil
			})
			if err != nil || observed.Outcome != managedssh.ExecutionFailed || checks != 2 || len(observed.Result) != 0 {
				t.Fatal("revoked pre-Start admission lost its definite no-effect outcome")
			}
			if _, err := os.Lstat(filepath.Join(base, "documents", "revoked.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("revoked pre-Start admission reached the helper filesystem")
			}
			select {
			case <-failures:
				t.Fatal("a real helper process did not join successfully")
			default:
			}
			mutex.Lock()
			defer mutex.Unlock()
			wantReads := 0
			if lost {
				wantReads = 1
			}
			if counts[managedssh.FileCreate] != 1 || counts[managedssh.FileRead] != 1 || counts[managedssh.ReceiptRead] != wantReads || len(counts) != 2+wantReads {
				t.Fatal("reconciliation repeated a write or added an unrequested operation")
			}
		})
	}
}
