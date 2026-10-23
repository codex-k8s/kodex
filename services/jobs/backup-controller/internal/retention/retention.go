// Package retention selects old backups for exact deletion while protecting a verified restore point.
// RU: Package retention выбирает старые копии для точного удаления, сохраняя проверенную точку восстановления.
package retention

import (
	"errors"
	"sort"
	"time"
)

// ErrNoVerifiedRestorePoint prevents deletion when no backup has a successful restore drill.
// RU: ErrNoVerifiedRestorePoint запрещает удаление, если ни одна копия не прошла пробное восстановление.
var ErrNoVerifiedRestorePoint = errors.New("no verified restore point is available")

// Candidate records a completed backup and whether a restore drill verified it.
// RU: Candidate фиксирует завершённую копию и факт её проверки пробным восстановлением.
type Candidate struct {
	BackupID    string
	CompletedAt time.Time
	Drilled     bool
}

// Select returns only backups older than the cutoff and outside keep, never deleting the newest drilled point.
// RU: Select возвращает лишь копии старше порога и вне keep, не удаляя последнюю проверенную восстановлением точку.
func Select(candidates []Candidate, now time.Time, minimumAge time.Duration, keep int) ([]string, error) {
	if minimumAge <= 0 || keep < 1 {
		return nil, errors.New("retention policy is invalid")
	}
	ordered := append([]Candidate(nil), candidates...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].CompletedAt.Equal(ordered[j].CompletedAt) {
			return ordered[i].BackupID > ordered[j].BackupID
		}
		return ordered[i].CompletedAt.After(ordered[j].CompletedAt)
	})
	protected := ""
	for _, candidate := range ordered {
		if candidate.Drilled {
			protected = candidate.BackupID
			break
		}
	}
	if protected == "" {
		return nil, ErrNoVerifiedRestorePoint
	}
	cutoff := now.Add(-minimumAge)
	selected := make([]string, 0)
	for index, candidate := range ordered {
		if candidate.BackupID == protected || index < keep || !candidate.CompletedAt.Before(cutoff) {
			continue
		}
		selected = append(selected, candidate.BackupID)
	}
	return selected, nil
}
