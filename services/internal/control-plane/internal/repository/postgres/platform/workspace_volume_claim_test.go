package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// TestWorkspaceVolumeClaimRejectsInvalidBatchBeforePersistence denies invalid budgets before accessing a transaction or allocating an attempt.
// TestWorkspaceVolumeClaimRejectsInvalidBatchBeforePersistence отклоняет invalid budgets до обращения к transaction либо создания attempt.
func TestWorkspaceVolumeClaimRejectsInvalidBatchBeforePersistence(t *testing.T) {
	repository := &Repository{}
	for _, input := range []command.WorkspaceVolumeClaimInput{
		{WorkloadInstance: "controller", Limit: 0}, {WorkloadInstance: "controller", Limit: -1},
		{WorkloadInstance: "controller", Limit: 17}, {WorkloadInstance: "", Limit: 1},
		{WorkloadInstance: " \n ", Limit: 1}, {WorkloadInstance: strings.Repeat("x", 257), Limit: 1},
	} {
		_, err := repository.claimWorkspaceVolumeTasksTx(t.Context(), nil, scope{}, input)
		if !errors.Is(err, errs.ErrInvalid) {
			t.Fatal("invalid physical-work budget reached persistence")
		}
	}
}

// TestWorkspaceVolumeClaimRejectsBorrowedMachineAuthority denies a different workload or method before any database lookup.
// TestWorkspaceVolumeClaimRejectsBorrowedMachineAuthority отклоняет другой workload/method до любого database lookup.
func TestWorkspaceVolumeClaimRejectsBorrowedMachineAuthority(t *testing.T) {
	repository := &Repository{}
	for _, principal := range []value.Principal{
		{CallerWorkload: "runtime-controller", Permission: command.WorkspaceVolumeCommandPermission(command.ClaimWorkspaceVolumeTasks)},
		{CallerWorkload: "session-archive", Permission: "platform.session-archive.tasks.claim"},
		{CallerWorkload: "session-archive", Permission: command.WorkspaceVolumeRenewPermission},
		{CallerWorkload: "control-api-gateway", Permission: command.WorkspaceVolumeCommandPermission(command.ClaimWorkspaceVolumeTasks)},
	} {
		err := repository.authorizeCommand(t.Context(), nil, scope{}, command.Command{Kind: command.ClaimWorkspaceVolumeTasks,
			Principal: principal, Payload: command.WorkspaceVolumeClaimInput{WorkloadInstance: "fixture", Limit: 1}})
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("borrowed machine authority reached a physical-work database lookup")
		}
	}
}

// TestWorkspaceVolumeClaimRejectsProjectScopedAuthority prevents a project-bound proof from granting an organization-wide physical batch.
// TestWorkspaceVolumeClaimRejectsProjectScopedAuthority не позволяет project-bound proof выдавать organization-wide physical batch.
func TestWorkspaceVolumeClaimRejectsProjectScopedAuthority(t *testing.T) {
	repository := &Repository{}
	input := command.Command{Kind: command.ClaimWorkspaceVolumeTasks,
		Principal: value.Principal{CallerWorkload: "session-archive", Permission: command.WorkspaceVolumeCommandPermission(command.ClaimWorkspaceVolumeTasks)},
		Payload:   command.WorkspaceVolumeClaimInput{WorkloadInstance: "fixture", Limit: 1}}
	if err := repository.authorizeCommand(t.Context(), nil, scope{authorityProjectID: "project-fixture"}, input); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("project-bound authority reached an organization-wide queue")
	}
	expectedVersion := int64(1)
	input.Mutation.ExpectedVersion = &expectedVersion
	if err := repository.authorizeCommand(t.Context(), nil, scope{}, input); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("batch claim accepted an unrelated aggregate version")
	}
}
