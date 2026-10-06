package codex

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

func outboxPublicationFixture(t *testing.T) (string, *os.File) {
	t.Helper()
	root := t.TempDir()
	if unix.Chmod(root, 0o2770) != nil {
		t.Fatal("prepare publication directory")
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal("open publication directory")
	}
	t.Cleanup(func() { directory.Close() })
	return root, directory
}

func TestProviderOutboxPublicationPrivateAndAtomicFiles(t *testing.T) {
	root, directory := outboxPublicationFixture(t)
	for _, name := range []string{"private.md", "replace.md"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("synthetic artifact"), 0o600); err != nil {
			t.Fatal("write publication fixture")
		}
	}
	temporary, err := os.CreateTemp(root, ".atomic-")
	if err != nil {
		t.Fatal("create atomic fixture")
	}
	if _, err := temporary.WriteString("atomic artifact"); err != nil || temporary.Close() != nil || os.Rename(temporary.Name(), filepath.Join(root, "replace.md")) != nil {
		t.Fatal("replace atomic fixture")
	}
	uid, group := uint32(os.Geteuid()), uint32(os.Getegid())
	if err := publishOutboxDirectory(t.Context(), directory, 10, uid, uid, group); err != nil {
		t.Fatal("private artifact publication failed")
	}
	for _, name := range []string{"private.md", "replace.md"} {
		var stat unix.Stat_t
		if unix.Stat(filepath.Join(root, name), &stat) != nil || stat.Mode&0o7777 != 0o640 || stat.Uid != uid || stat.Gid != group || stat.Nlink != 1 {
			t.Fatal("published artifact metadata is invalid")
		}
	}
}

func TestProviderOutboxPublicationRejectsUnsafeEntries(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "empty", "oversized", "foreign owner", "reserved provenance", "invalid name", "directory owner", "directory mode", "too many", "cancelled", "zero limit", "excess limit"} {
		t.Run(kind, func(t *testing.T) {
			root, directory := outboxPublicationFixture(t)
			path := filepath.Join(root, "artifact.md")
			uid, group := uint32(os.Geteuid()), uint32(os.Getegid())
			providerUID, runnerUID, limit := uid, uid, int64(10)
			ctx := t.Context()
			body := []byte("synthetic artifact")
			if kind == "empty" {
				body = nil
			}
			if kind == "oversized" {
				body = bytes.Repeat([]byte("x"), maximumPublishedArtifactBytes+1)
			}
			if kind == "reserved provenance" {
				path = filepath.Join(root, "workspace-write-result.json")
			}
			if kind == "invalid name" {
				path = filepath.Join(root, "invalid\nname")
			}
			if kind == "symlink" || kind == "hardlink" {
				outside := filepath.Join(t.TempDir(), "private-sentinel")
				if os.WriteFile(outside, body, 0o600) != nil {
					t.Fatal("write isolated sentinel")
				}
				var err error
				if kind == "symlink" {
					err = os.Symlink(outside, path)
				} else {
					err = os.Link(outside, path)
				}
				if err != nil {
					t.Fatal("prepare unsafe link")
				}
				t.Cleanup(func() {
					info, err := os.Stat(outside)
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Error("publication changed an outside inode")
					}
				})
			} else if kind == "fifo" {
				if unix.Mkfifo(path, 0o600) != nil {
					t.Fatal("prepare unsafe FIFO")
				}
			} else if os.WriteFile(path, body, 0o600) != nil {
				t.Fatal("write isolated artifact")
			}
			switch kind {
			case "foreign owner":
				providerUID++
			case "directory owner":
				runnerUID++
			case "directory mode":
				if unix.Chmod(root, 0o777) != nil {
					t.Fatal("prepare unsafe directory")
				}
			case "too many":
				limit = 1
				if os.WriteFile(filepath.Join(root, "second.md"), body, 0o600) != nil {
					t.Fatal("write second artifact")
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "zero limit":
				limit = 0
			case "excess limit":
				limit = runtimecontract.RuntimeWorkspaceMaximumFiles + 1
			}
			if err := publishOutboxDirectory(ctx, directory, limit, runnerUID, providerUID, group); !errors.Is(err, errProviderOutboxPublication) || strings.Contains(err.Error(), "private-sentinel") {
				t.Fatal("unsafe artifact publication did not fail closed")
			}
		})
	}
}

func TestExecuteProviderTurnDoesNotPublishAssistantOrFailedOutbox(t *testing.T) {
	for _, mode := range []string{"SYSTEM", "PROJECT", "NO_CAPABILITY", "FAILED", "EXECUTION_ERROR"} {
		t.Run(mode, func(t *testing.T) {
			input, authPath := providerTurnFixture(t, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"test-key"}`))
			input.Capabilities = []string{runtimecontract.ArtifactCapability}
			want := Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic result"}
			var executionErr error
			switch mode {
			case "SYSTEM":
				input.AssistantScope = runtimecontract.AssistantScopeSystem
			case "PROJECT":
				input.AssistantScope = runtimecontract.AssistantScopeProject
			case "NO_CAPABILITY":
				input.Capabilities = nil
			case "FAILED":
				want.Outcome, want.FailureCode = "FAILED", "provider_bad_request"
			case "EXECUTION_ERROR":
				executionErr = errors.New("synthetic execution failure")
			}
			got, err := executeProviderTurn(t.Context(), input, []byte("task"), strings.Repeat("a", 64),
				func(context.Context, model.Input, []byte, string) (Result, error) { return want, executionErr },
				func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error {
					t.Fatal("unchanged credential reached refresh relay")
					return nil
				})
			if !errors.Is(err, executionErr) || got.Outcome != want.Outcome || got.FinalMessage != want.FinalMessage || got.FailureCode != want.FailureCode {
				t.Fatal("outbox publication changed an ineligible turn")
			}
			assertRemoved(t, authPath)
		})
	}
}

func TestExecuteProviderTurnRejectsArtifactPublicationPreservingUsage(t *testing.T) {
	input, authPath := providerTurnFixture(t, []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"test-key"}`))
	input.Capabilities = []string{runtimecontract.ArtifactCapability}
	input.WorkspacePolicy = runtimecontract.RuntimeWorkspacePolicyV1()
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	got, err := executeProviderTurn(t.Context(), input, []byte("task"), strings.Repeat("a", 64),
		func(context.Context, model.Input, []byte, string) (Result, error) {
			return Result{Outcome: "SUCCEEDED", FinalMessage: "synthetic result", Usage: usage}, nil
		}, func(context.Context, model.Input, runtimecontract.RunnerProviderCredentialRefreshRequest) error {
			t.Fatal("unchanged credential reached refresh relay")
			return nil
		})
	if err != nil || got.Outcome != "FAILED" || got.FailureCode != "RUNTIME_ARTIFACT_INVALID" || got.FinalMessage != "i18n:RUNTIME_ARTIFACT_INVALID" || got.Usage != usage {
		t.Fatal("publication failure lost its closed outcome or measured usage")
	}
	assertRemoved(t, authPath)
}
