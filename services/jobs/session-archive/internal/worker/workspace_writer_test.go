package worker

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestWorkspaceHelperContextClosesInvalidBudgetAndIdentity rejects unknown budgets and caller identity before any task or credential read.
// TestWorkspaceHelperContextClosesInvalidBudgetAndIdentity отклоняет unknown budgets/caller identity до task или credential read.
func TestWorkspaceHelperContextClosesInvalidBudgetAndIdentity(t *testing.T) {
	for _, budget := range []string{"invalid", "0s", "59s", "16m"} {
		t.Run(budget, func(t *testing.T) {
			t.Setenv("SESSION_ARCHIVE_WORKER_TIMEOUT", budget)
			ctx, cancel, err := workspaceHelperContext(t.Context(), os.Geteuid())
			if err == nil || ctx != nil || cancel != nil {
				t.Fatal("invalid helper budget acquired a lifecycle")
			}
		})
	}
	t.Setenv("SESSION_ARCHIVE_WORKER_TIMEOUT", "1m")
	if ctx, cancel, err := workspaceHelperContext(t.Context(), os.Geteuid()+1); err == nil || ctx != nil || cancel != nil {
		t.Fatal("wrong helper identity acquired a lifecycle")
	}
	if ctx, cancel, err := workspaceHelperContext(nil, os.Geteuid()); err == nil || ctx != nil || cancel != nil {
		t.Fatal("missing caller context acquired a lifecycle")
	}
}

// TestWorkspaceHelperContextPreservesParentCancellationAndEarlierDeadline cannot extend the caller's bounded execution grant.
// TestWorkspaceHelperContextPreservesParentCancellationAndEarlierDeadline не продлевает bounded execution grant caller.
func TestWorkspaceHelperContextPreservesParentCancellationAndEarlierDeadline(t *testing.T) {
	t.Setenv("SESSION_ARCHIVE_WORKER_TIMEOUT", "15m")
	parent, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	ctx, cancel, err := workspaceHelperContext(parent, os.Geteuid())
	if err != nil {
		t.Fatal("create bounded helper context")
	}
	defer cancel()
	before, _ := parent.Deadline()
	actual, ok := ctx.Deadline()
	if !ok || !actual.Equal(before) {
		t.Fatal("helper extended its caller deadline")
	}
	stop()
	if ctx.Err() != context.Canceled {
		t.Fatal("helper did not inherit cancellation")
	}
}
