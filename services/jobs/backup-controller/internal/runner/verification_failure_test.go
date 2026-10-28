package runner

import (
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/s3backup"
)

// TestVerificationFailureKeepsPrimaryAndCleanup causes both failures to survive the same join used by Verify, preserving sentinel matching.
// RU: TestVerificationFailureKeepsPrimaryAndCleanup проверяет сохранение основного и cleanup отказов в объединении Verify, включая поиск sentinel.
func TestVerificationFailureKeepsPrimaryAndCleanup(t *testing.T) {
	t.Parallel()
	err := errors.Join(verificationFailure("payload_verify", s3backup.ErrNotFound),
		verificationFailure("lock_release", s3backup.ErrConflict))
	if !errors.Is(err, s3backup.ErrNotFound) || !errors.Is(err, s3backup.ErrConflict) {
		t.Fatal("verification failures lost repository sentinels")
	}
	if !strings.Contains(err.Error(), "backup verification payload_verify:") ||
		!strings.Contains(err.Error(), "backup verification lock_release:") {
		t.Fatal("verification failures lost stage distinction")
	}
}

// TestVerificationFailureRejectsArbitraryStages prevents caller data from becoming diagnostic labels and keeps successful cleanup nil.
// RU: TestVerificationFailureRejectsArbitraryStages исключает данные вызывающего из этапов диагностики и сохраняет nil успешной очистки.
func TestVerificationFailureRejectsArbitraryStages(t *testing.T) {
	t.Parallel()
	const privateMarker = "PRIVATE_STAGE_FIXTURE"
	err := verificationFailure(privateMarker, s3backup.ErrConflict)
	if strings.Contains(err.Error(), privateMarker) || !strings.Contains(err.Error(), "verification unknown:") {
		t.Fatal("arbitrary stage escaped into diagnostic")
	}
	for _, stage := range []string{"lock_acquire", "manifest_load", "payload_verify", "receipt_ensure", "lock_release"} {
		if verificationFailure(stage, nil) != nil {
			t.Fatal("successful operation became a failure")
		}
		if got := verificationFailure(stage, s3backup.ErrConflict); !strings.Contains(got.Error(), "verification "+stage+":") {
			t.Fatalf("registered stage %s was lost", stage)
		}
	}
}
