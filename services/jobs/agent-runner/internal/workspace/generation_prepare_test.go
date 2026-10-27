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
