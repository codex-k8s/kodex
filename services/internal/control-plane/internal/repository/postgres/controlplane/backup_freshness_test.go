package controlplane

import (
	"testing"
	"time"
)

func TestBackupProjectionFreshnessIsMonotonicForDerivedChanges(t *testing.T) {
	base := time.Date(2026, time.August, 6, 10, 0, 0, 120000, time.UTC)
	version, updatedAt, err := backupProjectionFreshness(base, time.Time{}, 1, 0)
	if err != nil {
		t.Fatalf("derive baseline freshness: %v", err)
	}

	rankedVersion, rankedAt, err := backupProjectionFreshness(base, time.Time{}, 2, 0)
	if err != nil || rankedVersion <= version || !rankedAt.After(updatedAt) {
		t.Fatalf("rank freshness did not advance: version=%d at=%s err=%v", rankedVersion, rankedAt, err)
	}

	expiredVersion, expiredAt, err := backupProjectionFreshness(base, base, 2, 1)
	if err != nil || expiredVersion <= rankedVersion || !expiredAt.After(rankedAt) {
		t.Fatalf("retention freshness did not advance: version=%d at=%s err=%v", expiredVersion, expiredAt, err)
	}
}
