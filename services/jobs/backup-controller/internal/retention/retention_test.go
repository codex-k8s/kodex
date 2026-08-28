package retention

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestSelectNeverDeletesLatestVerifiedRestorePoint(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	candidates := []Candidate{
		{BackupID: "newest", CompletedAt: now.Add(-40 * 24 * time.Hour)},
		{BackupID: "protected", CompletedAt: now.Add(-50 * 24 * time.Hour), Drilled: true},
		{BackupID: "old", CompletedAt: now.Add(-60 * 24 * time.Hour)},
	}

	selected, err := Select(candidates, now, 30*24*time.Hour, 1)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if !reflect.DeepEqual(selected, []string{"old"}) {
		t.Fatalf("Select() = %#v", selected)
	}
}

func TestSelectRefusesDeletionWithoutRestoreDrill(t *testing.T) {
	t.Parallel()
	_, err := Select([]Candidate{{BackupID: "old", CompletedAt: time.Now().Add(-365 * 24 * time.Hour)}},
		time.Now(), 24*time.Hour, 1)
	if !errors.Is(err, ErrNoVerifiedRestorePoint) {
		t.Fatalf("Select() error = %v", err)
	}
}
