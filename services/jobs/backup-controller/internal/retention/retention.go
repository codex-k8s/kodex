// Package retention выбирает backup, допустимые к точному удалению.
package retention

import (
	"errors"
	"sort"
	"time"
)

var ErrNoVerifiedRestorePoint = errors.New("no verified restore point is available")

type Candidate struct {
	BackupID    string
	CompletedAt time.Time
	Drilled     bool
}

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
