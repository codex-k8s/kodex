package contextfiles

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"golang.org/x/sys/unix"
)

type fakeSource struct {
	body  []byte
	calls int
	fail  bool
}

func (source *fakeSource) WriteSkillFile(_ context.Context, _ runtimecontract.RunnerInput, _ runtimecontract.RuntimeSkillBundle, _ runtimecontract.RuntimeSkillFile, writer io.Writer) error {
	source.calls++
	if source.fail {
		return io.ErrUnexpectedEOF
	}
	_, err := writer.Write(source.body)
	return err
}

func fixture() (runtimecontract.RunnerInput, runtimecontract.RuntimeContextSnapshot, *fakeSource, time.Time) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	input := runtimecontract.RunnerInput{OrganizationRef: "org_abcdefgh", ProjectRef: "proj_abcdefgh", AgentRef: "agt_abcdefgh",
		RuntimeRevisionRef: "rrev_abcdefgh", RuntimeRevisionDigest: strings.Repeat("a", 64), SessionRef: "ses_abcdefgh", TurnRef: "turn_abcdefgh", Attempt: 1}
	source := &fakeSource{body: []byte("---\nname: fixture\ndescription: Synthetic skill\n---\nRead references before acting.\n")}
	digest := sha256.Sum256(source.body)
	provenance := runtimecontract.RuntimeContextProvenance{ActorRef: "act_abcdefgh", SourceKind: "UI", Digest: strings.Repeat("b", 64), CreatedAt: now}
	snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef,
		Skills: []runtimecontract.RuntimeSkillBundle{{BundleRef: "sklb_abcdefgh", RevisionRef: "sklv_abcdefgh", Revision: 1,
			Digest: strings.Repeat("c", 64), Name: "fixture", Description: "Synthetic skill", BindingRef: "ctxb_abcdefgh", BindingVersion: 1, Provenance: provenance,
			ScanEngine: "fixture", ScanDigest: strings.Repeat("d", 64), ScannedAt: now,
			Files: []runtimecontract.RuntimeSkillFile{{Path: "SKILL.md", ArtifactRef: "art_abcdefgh", ArtifactRevision: 3, Digest: "sha256:" + hex.EncodeToString(digest[:]), SizeBytes: int64(len(source.body))}}}},
		Memories: []runtimecontract.RuntimeMemoryRecord{{RecordRef: "memr_abcdefgh", RevisionRef: "memv_abcdefgh", Revision: 2,
			Digest: strings.Repeat("e", 64), Title: "Fixture memory", Summary: "Remember the synthetic constraint.", BindingRef: "ctxb_ijklmnop", BindingVersion: 1, Provenance: provenance, RetentionUntil: now.Add(time.Hour)}},
	}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	return input, snapshot, source, now
}

func TestMaterializesExactSkillAndMemoryThenClearsRemovedContext(t *testing.T) {
	input, snapshot, source, now := fixture()
	root := t.TempDir()
	if err := materializeAt(t.Context(), root, input, snapshot, source, now); err != nil {
		t.Fatal(err)
	}
	if err := verifyAt(root, input, snapshot, now, false); err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 {
		t.Fatal("unexpected file fetch cardinality")
	}
	if raw, err := os.ReadFile(filepath.Join(root, "memory/memr_abcdefgh.md")); err != nil || !strings.Contains(string(raw), snapshot.Memories[0].Summary) {
		t.Fatal("typed memory was not materialized")
	}
	previous := input
	input.Attempt++
	input.TurnRef = "turn_ijklmnop"
	input.RuntimeRevisionRef = "rrev_ijklmnop"
	input.RuntimeRevisionDigest = strings.Repeat("f", 64)
	if verifyAt(root, input, snapshot, now, false) == nil {
		t.Fatal("previous attempt manifest was reused")
	}
	snapshot.Skills, snapshot.Memories = nil, nil
	snapshot.Digest, _ = snapshot.ComputeDigest()
	if err := materializeAt(t.Context(), root, input, snapshot, nil, now); err != nil {
		t.Fatal(err)
	}
	if verifyAt(root, previous, snapshot, now, false) == nil {
		t.Fatal("old attempt accepted renewed context")
	}
	for _, relative := range []string{"skills/sklb_abcdefgh/SKILL.md", "memory/memr_abcdefgh.md"} {
		if _, err := os.Lstat(filepath.Join(root, relative)); !os.IsNotExist(err) {
			t.Fatal("removed context survived renewal")
		}
	}
}

func TestWarmContextReusesOnlyIdenticalPins(t *testing.T) {
	input, snapshot, source, now := fixture()
	input.SystemAssistant = true
	input.Mode = runtimecontract.RunnerModeWarm
	root := t.TempDir()
	if err := materializeAt(t.Context(), root, input, snapshot, source, now); err != nil {
		t.Fatal(err)
	}
	input.Mode = runtimecontract.RunnerModeTurn
	input.Attempt++
	input.TurnRef = "turn_ijklmnop"
	input.RuntimeRevisionRef = "rrev_ijklmnop"
	input.RuntimeRevisionDigest = strings.Repeat("f", 64)
	if err := verifyAt(root, input, snapshot, now, false); err != nil {
		t.Fatal("identical warm context was not reusable")
	}
	snapshot.Memories = nil
	snapshot.Digest, _ = snapshot.ComputeDigest()
	if verifyAt(root, input, snapshot, now, false) == nil {
		t.Fatal("removed memory retained old warm tree")
	}
}

func TestContextValidationPrecedesFileFetch(t *testing.T) {
	for _, kind := range []string{"digest", "traversal", "retention", "foreign", "revoked"} {
		t.Run(kind, func(t *testing.T) {
			input, snapshot, source, now := fixture()
			switch kind {
			case "digest":
				snapshot.Digest = strings.Repeat("f", 64)
			case "traversal":
				snapshot.Skills[0].Files[0].Path = "../escape"
			case "retention":
				snapshot.Memories[0].RetentionUntil = now
			case "foreign":
				snapshot.ProjectRef = "proj_foreign1"
			case "revoked":
				snapshot.Skills[0].BindingVersion = 0
			}
			if kind != "digest" {
				snapshot.Digest, _ = snapshot.ComputeDigest()
			}
			if materializeAt(t.Context(), t.TempDir(), input, snapshot, source, now) == nil || source.calls != 0 {
				t.Fatal("invalid context reached file callback")
			}
		})
	}
}

func TestContextRejectsCorruptUnsafeAndAdditionalFiles(t *testing.T) {
	for _, kind := range []string{"content", "mode", "symlink", "hardlink", "directory symlink", "extra file", "fifo", "expiry", "writable mount"} {
		t.Run(kind, func(t *testing.T) {
			input, snapshot, source, now := fixture()
			root := t.TempDir()
			if err := materializeAt(t.Context(), root, input, snapshot, source, now); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, "skills/sklb_abcdefgh/SKILL.md")
			foreign := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(foreign, source.body, 0o440); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "content":
				if err := os.Chmod(file, 0o640); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(strings.Repeat("x", len(source.body))), 0o440); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(file, 0o440); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(file, 0o640); err != nil {
					t.Fatal(err)
				}
			case "symlink", "hardlink", "fifo":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				var err error
				if kind == "symlink" {
					err = os.Symlink(foreign, file)
				} else if kind == "hardlink" {
					err = os.Link(foreign, file)
				} else {
					err = unix.Mkfifo(file, 0o440)
				}
				if err != nil {
					t.Fatal(err)
				}
			case "directory symlink":
				if err := os.RemoveAll(filepath.Dir(file)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(foreign), filepath.Dir(file)); err != nil {
					t.Fatal(err)
				}
			case "extra file":
				if err := os.WriteFile(filepath.Join(root, "unadvertised"), source.body, 0o440); err != nil {
					t.Fatal(err)
				}
			case "expiry":
				now = now.Add(time.Hour)
			}
			if verifyAt(root, input, snapshot, now, kind == "writable mount") == nil {
				t.Fatal("unsafe context passed verification")
			}
		})
	}
}

func TestPartialAndOverBudgetFetchNeverPublishManifest(t *testing.T) {
	for _, kind := range []string{"short", "long", "wrong digest", "network", "manifest mismatch"} {
		t.Run(kind, func(t *testing.T) {
			input, snapshot, source, now := fixture()
			switch kind {
			case "short":
				source.body = source.body[:len(source.body)-1]
			case "long":
				source.body = append(source.body, 'x')
			case "wrong digest":
				source.body[0] = 'x'
			case "network":
				source.fail = true
			case "manifest mismatch":
				snapshot.Skills[0].Name = "different"
				snapshot.Digest, _ = snapshot.ComputeDigest()
			}
			root := t.TempDir()
			if materializeAt(t.Context(), root, input, snapshot, source, now) == nil {
				t.Fatal("invalid body accepted")
			}
			if _, err := os.Stat(filepath.Join(root, "manifest.json")); !os.IsNotExist(err) {
				t.Fatal("partial materialization published manifest")
			}
		})
	}
}

func TestMaterializerDoesNotFollowOldSymlinks(t *testing.T) {
	input, snapshot, _, now := fixture()
	snapshot.Skills, snapshot.Memories = nil, nil
	snapshot.Digest, _ = snapshot.ComputeDigest()
	root, foreign := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "sentinel"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	if err := materializeAt(t.Context(), root, input, snapshot, nil, now); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(foreign, "sentinel")); err != nil || string(raw) != "preserve" {
		t.Fatal("cleanup followed old symlink")
	}
}
