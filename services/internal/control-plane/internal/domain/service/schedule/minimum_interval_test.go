package schedule

import (
	"testing"
	"time"
)

// TestMinimumCronIntervalMatchesSchedulerAcrossDST checks minute cron preview and due resolution
// agree in UTC, DST gap and fold, and every consecutive occurrence remains at least one minute
// apart.
// TestMinimumCronIntervalMatchesSchedulerAcrossDST проверяет согласованность preview минутного
// cron и due resolution в UTC, DST gap и fold и интервал не менее минуты между последовательными
// запусками.
func TestMinimumCronIntervalMatchesSchedulerAcrossDST(t *testing.T) {
	for _, test := range []struct {
		name, zone, after string
	}{
		{"utc", "UTC", "2026-09-08T10:00:59Z"},
		{"gap", "Europe/Berlin", "2026-03-29T00:58:59Z"},
		{"fold", "Europe/Berlin", "2026-10-25T00:58:59Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := Spec{Preset: "CUSTOM", CronExpression: "* * * * *", Timezone: test.zone}
			after := mustTime(t, test.after)
			preview, err := Preview(spec, after, 5)
			if err != nil || len(preview) != 5 {
				t.Fatalf("minute preview: count=%d err=%v", len(preview), err)
			}
			for i, scheduled := range preview {
				if i > 0 && scheduled.Sub(preview[i-1]) < time.Minute {
					t.Fatal("occurrences violate the one minute minimum interval")
				}
				occurrence, next, err := ResolveDue(spec, scheduled, scheduled.Add(time.Second))
				if err != nil || occurrence == nil || !occurrence.Equal(scheduled) {
					t.Fatalf("scheduler disagrees with minute preview: %v", err)
				}
				if i+1 < len(preview) && !next.Equal(preview[i+1]) {
					t.Fatal("scheduler next occurrence differs from preview")
				}
			}
		})
	}
}

// TestSubMinuteCronIsRejectedBeforePreview rejects seconds-field cron and interval descriptors so
// preview cannot admit schedules below the one-minute supported interval.
// TestSubMinuteCronIsRejectedBeforePreview отклоняет cron с полем секунд и interval descriptors,
// чтобы preview не допускал расписания меньше поддерживаемого интервала в одну минуту.
func TestSubMinuteCronIsRejectedBeforePreview(t *testing.T) {
	for _, expression := range []string{"*/30 * * * * *", "@every 30s", "@every 1s"} {
		if _, err := Preview(Spec{Preset: "CUSTOM", CronExpression: expression, Timezone: "UTC"}, mustTime(t, "2026-09-08T10:00:00Z"), 5); err == nil {
			t.Fatalf("sub-minute expression was accepted: %s", expression)
		}
	}
}
