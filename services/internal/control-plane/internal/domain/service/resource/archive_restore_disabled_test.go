package resource

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

func TestIssueRuntimeWorkloadTicketArchiveRestoreDisabled(t *testing.T) {
	t.Parallel()

	service := Service{
		runtimeAdmissionSigningKey: ed25519.NewKeyFromSeed([]byte("runtime-admission-signing-test!!")),
		archiveRestoreEnabled:      false,
		now:                        func() time.Time { return time.Unix(1_700_000_000, 0) },
	}
	execution := &RuntimeExecution{WorkloadTicketSHA256: strings.Repeat("0", 64)}

	if err := service.issueRuntimeWorkloadTicket(execution); err != nil {
		t.Fatalf("issue workload ticket: %v", err)
	}
	if execution.WorkloadTicket == "" {
		t.Fatal("admission workload ticket is empty")
	}
	if execution.ArchiveWorkloadTicket != "" || execution.RestoreWorkloadTicket != "" {
		t.Fatal("disabled archive/restore workload ticket was issued")
	}
}
