package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestWorkspaceFailureTerminationHasOnlyClosedFields exercises real descriptor writes for every
// stage. A diagnostic is never a positive join and nested private error text is not copied.
// TestWorkspaceFailureTerminationHasOnlyClosedFields пишет через настоящий descriptor для всех
// этапов. Диагностика не становится positive join; вложенный private error не копируется.
func TestWorkspaceFailureTerminationHasOnlyClosedFields(t *testing.T) {
	for _, stage := range []string{"EXECUTOR_UNAVAILABLE", "EXECUTOR_JOIN", "WRITER_ACK", "PROVIDER_JOIN", "RECORD_ENCODE", "RECORD_WRITE", "PRIVATE_SENTINEL"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "termination")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			cause := rejectedWorkspaceQuiescence(stage, fmt.Errorf("PRIVATE_SENTINEL: %w", context.Canceled))
			if !errors.Is(cause, context.Canceled) || strings.Contains(cause.Error(), "PRIVATE_SENTINEL") {
				t.Fatal("closed error lost cancellation or exposed nested text")
			}
			if err := writeWorkspaceQuiescenceFailure(path, fmt.Errorf("PRIVATE_SENTINEL: %w", cause)); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			var record map[string]string
			if err != nil || json.Unmarshal(raw, &record) != nil || len(record) != 2 || record["schema"] != "kodex.workspace-quiescence-failure.v1" || record["stage"] != closedWorkspaceQuiescenceStage(stage) || bytes.Contains(raw, []byte("PRIVATE_SENTINEL")) {
				t.Fatal("termination diagnostic contains unsafe or missing fields")
			}
			if _, err := runtimecontract.DecodeWorkspaceQuiescence(raw); err == nil {
				t.Fatal("diagnostic accepted as positive workspace join")
			}
		})
	}
}

// TestWorkspaceFailureTerminationPreservesExistingRecord proves valid positive bytes and unknown
// preexisting bytes are never truncated; only an empty kubelet-owned file is eligible.
// TestWorkspaceFailureTerminationPreservesExistingRecord доказывает сохранность valid positive
// и неизвестных прежних bytes; записать можно лишь пустой kubelet-owned файл.
func TestWorkspaceFailureTerminationPreservesExistingRecord(t *testing.T) {
	record := runtimecontract.WorkspaceQuiescence{
		Schema: runtimecontract.WorkspaceQuiescenceSchema, Generation: 1, Joined: true, WorkspaceRef: "wks_fixture000",
		Scope: runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"},
		Writer: runtimecontract.WorkspaceWriterCommitment{
			RunRef: "run_fixture000", NodeRef: "nod_fixture000", TurnRef: "trn_fixture000", RuntimeRevisionRef: "rrv_fixture000",
			RuntimeRevisionVersion: 1, RuntimeRevisionDigest: strings.Repeat("a", 64), ExecutionBindingDigest: strings.Repeat("b", 64), InputDigest: strings.Repeat("c", 64),
			LeaseRef: "lea_fixture000", LeaseGeneration: 1, Attempt: 1,
			PodUID: "11111111-1111-4111-8111-111111111111", PVCUID: "22222222-2222-4222-8222-222222222222",
		},
	}
	positive, err := runtimecontract.EncodeWorkspaceQuiescence(record)
	if err != nil {
		t.Fatal("invalid synthetic positive record", err)
	}
	for _, existing := range [][]byte{positive, []byte("unknown partial record")} {
		path := filepath.Join(t.TempDir(), "termination")
		if err := os.WriteFile(path, existing, 0600); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(writeWorkspaceQuiescenceFailure(path, errors.New("PRIVATE_SENTINEL")), os.ErrExist) {
			t.Fatal("nonempty termination file was not refused")
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, existing) {
			t.Fatal("previous termination record changed")
		}
	}
}

// TestWorkspaceFailureTerminationRefusesForeignDescriptors covers missing paths, links and
// nonregular files with real local syscalls; it never opens a production path.
// TestWorkspaceFailureTerminationRefusesForeignDescriptors проверяет отсутствующий путь,
// links и nonregular files настоящими local syscalls без открытия production path.
func TestWorkspaceFailureTerminationRefusesForeignDescriptors(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "source")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for name, create := range map[string]func(string) error{
		"symlink":   func(path string) error { return os.Symlink(file, path) },
		"hardlink":  func(path string) error { return os.Link(file, path) },
		"fifo":      func(path string) error { return syscall.Mkfifo(path, 0600) },
		"directory": func(path string) error { return os.Mkdir(path, 0700) },
	} {
		path := filepath.Join(directory, name)
		if err := create(path); err != nil {
			t.Fatal(err)
		}
		if writeWorkspaceQuiescenceFailure(path, errors.New("PRIVATE_SENTINEL")) == nil {
			t.Fatal("foreign descriptor accepted", name)
		}
	}
	missing := filepath.Join(directory, "missing")
	if writeWorkspaceQuiescenceFailure(missing, nil) == nil {
		t.Fatal("missing file created")
	}
	if _, err := os.Lstat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("diagnostic created a path")
	}
	if raw, err := os.ReadFile(file); err != nil || len(raw) != 0 {
		t.Fatal("foreign target changed")
	}
}

// TestWorkspaceFailureUnknownCauseHasNoPrivateText covers an untyped error, not a known stage.
// TestWorkspaceFailureUnknownCauseHasNoPrivateText проверяет untyped error вне известных этапов.
func TestWorkspaceFailureUnknownCauseHasNoPrivateText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "termination")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeWorkspaceQuiescenceFailure(path, errors.New("PRIVATE_SENTINEL")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte(`"stage":"UNKNOWN"`)) || bytes.Contains(raw, []byte("PRIVATE_SENTINEL")) {
		t.Fatal("unknown cause was not closed")
	}
	if os.Geteuid() != 10001 && !errors.Is(recordWorkspaceQuiescenceFailure(nil), os.ErrPermission) {
		t.Fatal("production diagnostic writer admitted a foreign UID")
	}
}
