package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

// generationPrepareFixture supplies closed synthetic owner metadata and a private unprivileged directory.
// It tests descriptor behavior without granting production UID or invoking the public trusted-root opener.
// generationPrepareFixture задаёт closed synthetic owner metadata и private unprivileged directory.
// Проверяются descriptors без production UID и без публичного trusted-root opener.
func generationPrepareFixture(t *testing.T) (int, string, runtimecontract.RunnerInput, runtimecontract.WorkspaceWriterAcknowledgement, generationFileOwner) {
	t.Helper()
	scope := runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"}
	input := runtimecontract.RunnerInput{RoleRuntimeContractRevision: runtimecontract.WorkspaceStorageRuntimeContractRevision, Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone,
		OrganizationRef: scope.OrganizationRef, ProjectRef: scope.ProjectRef, SessionRef: scope.SessionRef, AgentRef: scope.AgentRef,
		RunRef: "run_fixture000", NodeRef: "node_fixture000", TurnRef: "turn_fixture000", RuntimeRevisionRef: "rrev_fixture000", RuntimeRevisionVersion: 1,
		RuntimeRevisionDigest: strings.Repeat("a", 64), ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64),
		LeaseRef: "lease_fixture000", LeaseGeneration: 1, Attempt: 1,
		WorkspaceStorage: &runtimecontract.WorkspaceStorageBinding{Profile: runtimecontract.WorkspaceStorageProfile, Scope: scope, WorkspaceRef: "wks_fixture000", Generation: 1}}
	ack := runtimecontract.WorkspaceWriterAcknowledgement{Schema: runtimecontract.WorkspaceWriterAcknowledgementSchema, Scope: scope,
		WorkspaceRef: input.WorkspaceStorage.WorkspaceRef, Generation: 1,
		Writer: runtimecontract.WorkspaceWriterCommitment{RunRef: input.RunRef, NodeRef: input.NodeRef, TurnRef: input.TurnRef,
			RuntimeRevisionRef: input.RuntimeRevisionRef, RuntimeRevisionVersion: input.RuntimeRevisionVersion,
			RuntimeRevisionDigest: input.RuntimeRevisionDigest, ExecutionBindingDigest: input.ExecutionBindingDigest, InputDigest: input.InputDigest,
			LeaseRef: input.LeaseRef, LeaseGeneration: input.LeaseGeneration, Attempt: input.Attempt, PodUID: "pod-fixture", PVCUID: "pvc-fixture"}}
	if !ack.Matches(input) {
		t.Fatal("invalid synthetic writer ACK")
	}
	root := t.TempDir()
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd, root, input, ack, generationFileOwner{uid: uint32(os.Geteuid()), gid: uint32(os.Getegid())}
}

// TestWorkspaceProviderStateIsSeparateAndRetained prepares the exact history sibling after BIND and preserves existing private history on rejoin.
// TestWorkspaceProviderStateIsSeparateAndRetained готовит exact history sibling после BIND и сохраняет private history при rejoin.
func TestWorkspaceProviderStateIsSeparateAndRetained(t *testing.T) {
	fd, root, input, ack, owner := generationPrepareFixture(t)
	if err := prepareBoundGenerationAt(t.Context(), fd, input, ack, owner); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, runtimecontract.WorkspaceProviderStateSubPath)
	var before, after unix.Stat_t
	if unix.Lstat(name, &before) != nil || before.Mode&0o7777 != 0o2770 || before.Uid != owner.uid || before.Gid != owner.gid {
		t.Fatal("provider state did not receive its confined shared root")
	}
	file := filepath.Join(name, "history")
	if os.WriteFile(file, []byte("private-history"), 0o600) != nil || prepareBoundGenerationAt(t.Context(), fd, input, ack, owner) != nil ||
		unix.Lstat(name, &after) != nil || before.Ino != after.Ino || before.Dev != after.Dev || before.Mode != after.Mode {
		t.Fatal("provider history root was replaced during rejoin")
	}
	raw, err := os.ReadFile(file)
	if err != nil || string(raw) != "private-history" || unix.Lstat(file, &after) != nil || after.Mode&0o7777 != 0o600 {
		t.Fatal("rejoin changed private provider history")
	}
	if _, err := os.Lstat(filepath.Join(name, "workspaces")); !os.IsNotExist(err) {
		t.Fatal("private generations became descendants of provider history")
	}
}

// TestWorkspaceProviderStateRejectsUnknownExistingEntry never normalizes a foreign link, file or incompatible directory before generation writes.
// TestWorkspaceProviderStateRejectsUnknownExistingEntry не исправляет unknown link/file/incompatible directory до generation writes.
func TestWorkspaceProviderStateRejectsUnknownExistingEntry(t *testing.T) {
	for _, kind := range []string{"symlink", "file", "wrong directory mode"} {
		t.Run(kind, func(t *testing.T) {
			fd, root, input, ack, owner := generationPrepareFixture(t)
			name := filepath.Join(root, runtimecontract.WorkspaceProviderStateSubPath)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(t.TempDir(), name)
			case "file":
				err = os.WriteFile(name, []byte("retained"), 0o600)
			default:
				err = os.Mkdir(name, 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			var before, after unix.Stat_t
			if unix.Lstat(name, &before) != nil || prepareBoundGenerationAt(t.Context(), fd, input, ack, owner) == nil ||
				unix.Lstat(name, &after) != nil || before.Ino != after.Ino || before.Mode != after.Mode || before.Ctim != after.Ctim {
				t.Fatal("unknown provider-state entry was admitted or repaired")
			}
			if _, err := os.Lstat(filepath.Join(root, "workspaces")); !os.IsNotExist(err) {
				t.Fatal("unknown provider state allowed generation writes")
			}
		})
	}
}

// TestWorkspaceGenerationRecoversCompleteProofBeforeAcknowledgement models the exact crash publication boundary.
// A fresh owner BIND may finish a missing ACK only when the protected mount proof and empty tree still match.
// TestWorkspaceGenerationRecoversCompleteProofBeforeAcknowledgement моделирует exact crash publication boundary.
// Fresh owner BIND завершает missing ACK лишь при прежнем protected mount proof и пустом tree.
func TestWorkspaceGenerationRecoversCompleteProofBeforeAcknowledgement(t *testing.T) {
	fd, root, input, ack, owner := generationPrepareFixture(t)
	if err := prepareBoundGenerationAt(t.Context(), fd, input, ack, owner); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "workspaces", input.WorkspaceStorage.WorkspaceRef, "g1")
	before, err := os.ReadFile(filepath.Join(directory, generationMountFile))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, generationWriterFile)); err != nil {
		t.Fatal(err)
	}
	if err := prepareBoundGenerationAt(t.Context(), fd, input, ack, owner); err != nil {
		t.Fatalf("complete proof crash could not recover: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(directory, generationMountFile))
	if err != nil || string(before) != string(after) {
		t.Fatal("recovery replaced existing mount proof")
	}
	raw, err := os.ReadFile(filepath.Join(directory, generationWriterFile))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement(raw)
	if err != nil || decoded != ack {
		t.Fatal("recovery did not publish exact owner ACK")
	}
}

// TestWorkspaceGenerationCrashNeverErasesChangedTreeOrPartialProof keeps ambiguous evidence intact and blocked.
// A cancelled context also prevents creation, even though the lower helper receives an ACK-shaped value.
// TestWorkspaceGenerationCrashNeverErasesChangedTreeOrPartialProof сохраняет ambiguous evidence в blocked state.
// Cancelled context запрещает создание даже при ACK-shaped value в lower helper.
func TestWorkspaceGenerationCrashNeverErasesChangedTreeOrPartialProof(t *testing.T) {
	for _, mode := range []string{"changed tree", "partial proof", "foreign proof"} {
		t.Run(mode, func(t *testing.T) {
			fd, root, input, ack, owner := generationPrepareFixture(t)
			if err := prepareBoundGenerationAt(t.Context(), fd, input, ack, owner); err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(root, "workspaces", input.WorkspaceStorage.WorkspaceRef, "g1")
			if err := os.Remove(filepath.Join(directory, generationWriterFile)); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, generationMountFile)
			payload := []byte("{")
			if mode == "changed tree" {
				path = filepath.Join(directory, "tree", "unpublished.go")
				payload = []byte("unpublished source")
			}
			if mode == "foreign proof" {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				proof, err := runtimecontract.DecodeWorkspaceMountProof(raw)
				if err != nil {
					t.Fatal(err)
				}
				proof.ExecutionBindingDigest = strings.Repeat("d", 64)
				payload, err = runtimecontract.EncodeWorkspaceMountProof(proof)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode != "changed tree" {
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			if mode != "changed tree" {
				if err := os.Chmod(path, 0o440); err != nil {
					t.Fatal(err)
				}
			}
			if err := prepareBoundGenerationAt(t.Context(), fd, input, ack, owner); err == nil {
				t.Fatal("ambiguous crash evidence accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(payload) {
				t.Fatal("blocked recovery erased previous data")
			}
		})
	}
	fd, root, input, ack, owner := generationPrepareFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := prepareBoundGenerationAt(ctx, fd, input, ack, owner); err == nil {
		t.Fatal("cancelled BIND preparation accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled preparation modified filesystem")
	}
}
