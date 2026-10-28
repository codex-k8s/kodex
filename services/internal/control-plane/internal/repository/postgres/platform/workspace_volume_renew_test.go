package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestWorkspaceVolumeRenewRejectsForeignAuthorityBeforePersistence prevents unrelated methods/workloads from resolving or reconciling a root.
// TestWorkspaceVolumeRenewRejectsForeignAuthorityBeforePersistence не позволяет чужим methods/workloads разрешать либо согласовывать root.
func TestWorkspaceVolumeRenewRejectsForeignAuthorityBeforePersistence(t *testing.T) {
	repository := &Repository{}
	input := command.WorkspaceVolumeTaskInput{TaskRef: "svt_fixture000", LeaseRef: "lea_fixture000", Fence: "fnc_fixture000", Generation: 1}
	for _, principal := range []value.Principal{
		{CallerWorkload: "runtime-controller", Permission: command.WorkspaceVolumeRenewPermission},
		{CallerWorkload: "session-archive", Permission: command.WorkspaceVolumeCommandPermission(command.ClaimWorkspaceVolumeTasks)},
		{CallerWorkload: "session-archive", Permission: "platform.session-archive.tasks.renew"},
	} {
		if _, err := repository.RenewWorkspaceVolumeTask(t.Context(), principal, input); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("foreign renewal authority reached persistence")
		}
	}
}

// TestWorkspaceVolumeRenewRejectsReportPayloadBeforePersistence prevents a renewal from recording an unvalidated physical outcome.
// TestWorkspaceVolumeRenewRejectsReportPayloadBeforePersistence не позволяет renewal записывать непроверенный physical outcome.
func TestWorkspaceVolumeRenewRejectsReportPayloadBeforePersistence(t *testing.T) {
	repository := &Repository{}
	principal := value.Principal{CallerWorkload: "session-archive", Permission: command.WorkspaceVolumeRenewPermission}
	for _, mutate := range []func(*command.WorkspaceVolumeTaskInput){
		func(v *command.WorkspaceVolumeTaskInput) { v.ObservedPVCUID = "10000000-0000-4000-8000-000000000001" },
		func(v *command.WorkspaceVolumeTaskInput) { v.JobUID = "20000000-0000-4000-8000-000000000001" },
		func(v *command.WorkspaceVolumeTaskInput) { v.PodUID = "30000000-0000-4000-8000-000000000001" },
		func(v *command.WorkspaceVolumeTaskInput) { v.SafeErrorCode = "STORAGE_UNAVAILABLE" },
	} {
		input := command.WorkspaceVolumeTaskInput{TaskRef: "svt_fixture000", LeaseRef: "lea_fixture000", Fence: "fnc_fixture000", Generation: 1}
		mutate(&input)
		if _, err := repository.RenewWorkspaceVolumeTask(t.Context(), principal, input); !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("physical report entered a renewal database path")
		}
	}
}

// TestWorkspaceVolumeFenceArgsNeverCarryRawFence keeps private proof bytes out of SQL arguments while bounding invalid generations.
// TestWorkspaceVolumeFenceArgsNeverCarryRawFence исключает raw fence bytes из SQL arguments и ограничивает invalid generations.
func TestWorkspaceVolumeFenceArgsNeverCarryRawFence(t *testing.T) {
	input := command.WorkspaceVolumeTaskInput{TaskRef: "svt_fixture000", LeaseRef: "lea_fixture000", Fence: "fnc_fixture000", Generation: 1}
	args, err := workspaceVolumeFenceArgs(scope{}, input)
	if err != nil || len(args["fence_digest"].(string)) != 64 {
		t.Fatal("bounded fence digest was not constructed")
	}
	for _, value := range args {
		if text, ok := value.(string); ok && strings.Contains(text, input.Fence) {
			t.Fatal("raw fence reached SQL arguments")
		}
	}
	input.Generation = 6
	if _, err := workspaceVolumeFenceArgs(scope{}, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("unbounded attempt generation reached SQL arguments")
	}
}
