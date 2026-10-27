package platform

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// incidentRowFixture supplies synthetic rows while recording cleanup ownership.
// incidentRowFixture возвращает синтетические rows и фиксирует владение очисткой.
type incidentRowFixture struct {
	pgx.Rows
	states             []string
	index, scanFailure int
	iterationFailure   bool
	explicitClose      int
	closed             bool
}

// Next advances rows and reproduces pgx automatic closing on exhaustion.
// Next продвигает rows и воспроизводит автоматическое закрытие pgx по исчерпании.
func (r *incidentRowFixture) Next() bool {
	if r.index >= len(r.states) {
		r.closed = true
		return false
	}
	return true
}

// Scan returns exact incident fields or a synthetic driver failure.
// Scan возвращает точные поля инцидента либо синтетическую ошибку драйвера.
func (r *incidentRowFixture) Scan(dest ...any) error {
	r.index++
	if r.index == r.scanFailure {
		r.closed = true
		return errors.New("scan failure")
	}
	*dest[0].(*string) = "incident"
	*dest[1].(*string) = "project"
	*dest[2].(*string) = "run"
	*dest[3].(*string) = r.states[r.index-1]
	*dest[4].(*int) = 2
	*dest[5].(*int) = 2
	*dest[6].(*time.Time) = time.Unix(1, 0).UTC()
	return nil
}

// Err exposes iteration failure only after automatic closing.
// Err возвращает ошибку итерации только после автоматического закрытия.
func (r *incidentRowFixture) Err() error {
	if !r.closed {
		panic("error checked before row completion")
	}
	if r.iterationFailure {
		return errors.New("iteration failure")
	}
	return nil
}

// Close records explicit caller cleanup.
// Close фиксирует явное закрытие вызывающей стороной.
func (r *incidentRowFixture) Close() { r.explicitClose++; r.closed = true }

// TestAppendInteractionIncidents verifies projection, ordering, prefix and fail-closed errors.
// TestAppendInteractionIncidents проверяет проекцию, порядок, исходный префикс и закрытые ошибки.
func TestAppendInteractionIncidents(t *testing.T) {
	rows := &incidentRowFixture{states: []string{"UNKNOWN_OUTCOME", "SUCCEEDED"}}
	prefix := []entity.Incident{{Ref: "existing"}}
	got, err := appendInteractionIncidents(rows, prefix)
	if err != nil || len(got) != 3 || got[0].Ref != "existing" || got[1].State != "OPEN" || got[1].Severity != "ERROR" || got[2].State != "RESOLVED" || got[2].Severity != "INFO" || rows.explicitClose != 0 {
		t.Fatal("incident projection or cleanup changed")
	}
	empty := &incidentRowFixture{}
	got, err = appendInteractionIncidents(empty, nil)
	if err != nil || got != nil || empty.explicitClose != 0 {
		t.Fatal("empty result or cleanup changed")
	}
	for _, rows := range []*incidentRowFixture{{states: []string{"UNKNOWN_OUTCOME"}, scanFailure: 1}, {states: []string{"SUCCEEDED"}, iterationFailure: true}} {
		got, err = appendInteractionIncidents(rows, nil)
		if !errors.Is(err, errs.ErrUnavailable) || got != nil || rows.explicitClose != 0 {
			t.Fatal("failure must discard partial results without explicit close")
		}
	}
}
