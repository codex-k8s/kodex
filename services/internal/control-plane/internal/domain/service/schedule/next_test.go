package schedule

import (
	"testing"
	"time"
)

// TestNormalizeAndNext checks hourly, daily, weekday, weekly and custom schedules produce exact
// UTC instants, including shifting a nonexistent Berlin local minute forward.
// TestNormalizeAndNext проверяет точные UTC моменты hourly, daily, weekday, weekly и custom
// расписаний, включая сдвиг несуществующей локальной минуты Берлина вперёд.
func TestNormalizeAndNext(t *testing.T) {
	tests := []struct {
		name        string
		spec        Spec
		after, want string
	}{
		{name: "hourly", spec: Spec{Preset: "HOURLY", Timezone: "UTC"}, after: "2026-08-23T10:15:00Z", want: "2026-08-23T11:00:00Z"},
		{name: "daily", spec: Spec{Preset: "DAILY", TimeOfDay: "09:30", Timezone: "Europe/Saratov"}, after: "2026-08-23T05:00:00Z", want: "2026-08-23T05:30:00Z"},
		{name: "weekdays skip weekend", spec: Spec{Preset: "WEEKDAYS", TimeOfDay: "09:00", Timezone: "UTC"}, after: "2026-08-22T12:00:00Z", want: "2026-08-24T09:00:00Z"},
		{name: "weekly", spec: Spec{Preset: "WEEKLY", TimeOfDay: "12:00", DayOfWeek: "WEDNESDAY", Timezone: "UTC"}, after: "2026-08-23T12:00:00Z", want: "2026-08-26T12:00:00Z"},
		{name: "custom", spec: Spec{Preset: "CUSTOM", CronExpression: "*/15 9-10 * * 1-5", Timezone: "UTC"}, after: "2026-08-24T09:07:00Z", want: "2026-08-24T09:15:00Z"},
		{name: "dst gap moves forward", spec: Spec{Preset: "DAILY", TimeOfDay: "02:30", Timezone: "Europe/Berlin"}, after: "2026-03-28T23:00:00Z", want: "2026-03-29T01:00:00Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			after, _ := time.Parse(time.RFC3339, test.after)
			want, _ := time.Parse(time.RFC3339, test.want)
			value, err := Normalize(test.spec, after)
			if err != nil || !value.Next.Equal(want) {
				t.Fatalf("next = %s, want %s, err=%v", value.Next, want, err)
			}
		})
	}
}

// TestNormalizeRejectsInvalidInput rejects out-of-range clock values, missing weekly weekday,
// unsupported presets, malformed cron and unavailable timezone identifiers.
// TestNormalizeRejectsInvalidInput отклоняет значения часов вне диапазона, отсутствующий weekly
// день недели, неподдерживаемые presets, повреждённый cron и недоступные timezone identifiers.
func TestNormalizeRejectsInvalidInput(t *testing.T) {
	for _, spec := range []Spec{
		{Preset: "DAILY", TimeOfDay: "25:00", Timezone: "UTC"},
		{Preset: "WEEKLY", TimeOfDay: "09:00", DayOfWeek: "", Timezone: "UTC"},
		{Preset: "MONTHLY", TimeOfDay: "09:00", Timezone: "UTC"},
		{Preset: "CUSTOM", CronExpression: "61 * * * *", Timezone: "UTC"},
		{Preset: "CUSTOM", CronExpression: "* * *", Timezone: "UTC"},
		{Preset: "DAILY", TimeOfDay: "09:00", Timezone: "Unknown/Nowhere"},
	} {
		if _, err := Normalize(spec, time.Now()); err == nil {
			t.Fatalf("invalid specification accepted: %#v", spec)
		}
	}
}

// TestPreviewUsesNormalizedCustomCronAndRunsDSTFoldOnce checks custom Berlin fold preview selects
// the earlier repeated local minute and schedules the following day rather than the second fold
// instant.
// TestPreviewUsesNormalizedCustomCronAndRunsDSTFoldOnce проверяет выбор ранней повторяющейся
// локальной минуты custom preview Берлина и следующего дня вместо второго fold момента.
func TestPreviewUsesNormalizedCustomCronAndRunsDSTFoldOnce(t *testing.T) {
	after := mustTime(t, "2026-10-25T00:20:00Z")
	values, err := Preview(Spec{
		Preset: "CUSTOM", CronExpression: "30 2 * * *", Timezone: "Europe/Berlin",
		DSTGapPolicy: DSTGapShiftForward, DSTFoldPolicy: DSTFoldRunOnce,
		MisfirePolicy: MisfireCoalesce, OverlapPolicy: OverlapForbid,
	}, after, 2)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !values[0].Equal(mustTime(t, "2026-10-25T00:30:00Z")) ||
		!values[1].Equal(mustTime(t, "2026-10-26T01:30:00Z")) {
		t.Fatalf("fold preview = %v", values)
	}
}

// TestPreviewShiftsCustomCronAcrossDSTGap checks custom cron shifts the missing local minute to
// the first valid post-gap time and resumes the original wall clock on the next day.
// TestPreviewShiftsCustomCronAcrossDSTGap проверяет сдвиг отсутствующей локальной минуты custom
// cron на первое существующее время после gap и возврат исходных часов на следующий день.
func TestPreviewShiftsCustomCronAcrossDSTGap(t *testing.T) {
	values, err := Preview(Spec{
		Preset: "CUSTOM", CronExpression: "30 2 * * *", Timezone: "Europe/Berlin",
		DSTGapPolicy: DSTGapShiftForward, DSTFoldPolicy: DSTFoldRunOnce,
	}, mustTime(t, "2026-03-28T23:00:00Z"), 2)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !values[0].Equal(mustTime(t, "2026-03-29T01:00:00Z")) ||
		!values[1].Equal(mustTime(t, "2026-03-30T00:30:00Z")) {
		t.Fatalf("gap preview = %v", values)
	}
}

// TestResolveDueMisfirePolicies checks coalesce returns one old occurrence with a future cursor,
// catch-up advances one interval and skip returns no delayed occurrence.
// TestResolveDueMisfirePolicies проверяет один старый запуск и будущий cursor для coalesce,
// продвижение на один интервал для catch-up и отсутствие delayed запуска для skip.
func TestResolveDueMisfirePolicies(t *testing.T) {
	scheduled := mustTime(t, "2026-09-04T09:00:00Z")
	now := mustTime(t, "2026-09-04T12:30:00Z")
	base := Spec{Preset: "CUSTOM", CronExpression: "0 * * * *", Timezone: "UTC"}

	for _, test := range []struct {
		name, policy, occurrence, next string
	}{
		{name: "coalesce", policy: MisfireCoalesce, occurrence: "2026-09-04T09:00:00Z", next: "2026-09-04T13:00:00Z"},
		{name: "catch up one", policy: MisfireCatchUpOne, occurrence: "2026-09-04T09:00:00Z", next: "2026-09-04T10:00:00Z"},
		{name: "skip", policy: MisfireSkip, next: "2026-09-04T13:00:00Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			spec := base
			spec.MisfirePolicy = test.policy
			occurrence, next, err := ResolveDue(spec, scheduled, now)
			if err != nil || !next.Equal(mustTime(t, test.next)) {
				t.Fatalf("resolve due: occurrence=%v next=%s err=%v", occurrence, next, err)
			}
			if test.occurrence == "" && occurrence != nil {
				t.Fatalf("skip returned occurrence %s", occurrence)
			}
			if test.occurrence != "" && (occurrence == nil || !occurrence.Equal(mustTime(t, test.occurrence))) {
				t.Fatalf("occurrence = %v", occurrence)
			}
		})
	}
}

// TestNormalizeRejectsUnknownExecutionPolicies checks unsupported gap, fold, misfire and overlap
// policy values fail rather than silently falling back to defaults.
// TestNormalizeRejectsUnknownExecutionPolicies проверяет отказ неподдерживаемым gap, fold, misfire
// и overlap policy вместо молчаливого перехода к default.
func TestNormalizeRejectsUnknownExecutionPolicies(t *testing.T) {
	base := Spec{Preset: "CUSTOM", CronExpression: "0 * * * *", Timezone: "UTC"}
	for _, mutate := range []func(*Spec){
		func(spec *Spec) { spec.DSTGapPolicy = "SKIP" },
		func(spec *Spec) { spec.DSTFoldPolicy = "RUN_TWICE" },
		func(spec *Spec) { spec.MisfirePolicy = "ALL" },
		func(spec *Spec) { spec.OverlapPolicy = "MAYBE" },
	} {
		spec := base
		mutate(&spec)
		if _, err := Normalize(spec, time.Now()); err == nil {
			t.Fatalf("unknown policy accepted: %#v", spec)
		}
	}
}

// mustTime parses a test RFC3339 instant and marks parse failures at the caller through the
// testing helper boundary.
// mustTime разбирает тестовый RFC3339 момент и указывает ошибку parsing у caller через границу
// testing helper.
func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

// TestCronParserAndPersistedSemantics checks steps, named ranges and day matching survive
// persisted due resolution, rejects unsupported/impossible cron and reconstructs preset display
// fields from storage.
// TestCronParserAndPersistedSemantics проверяет сохранение steps, именованных ranges и day
// matching при persisted due resolution, отклоняет неподдерживаемый либо невозможный cron и
// восстанавливает display поля preset из хранения.
func TestCronParserAndPersistedSemantics(t *testing.T) {
	for _, test := range []struct{ expression, after, want string }{
		{"5/15 * * * *", "2026-09-04T09:06:00Z", "2026-09-04T09:20:00Z"},
		{"0 9 * JAN-DEC MON-FRI", "2026-09-04T09:01:00Z", "2026-09-07T09:00:00Z"},
		{"0 9 */2 * MON", "2026-09-04T09:01:00Z", "2026-09-05T09:00:00Z"},
		{"0 9 1 * MON", "2026-09-04T09:01:00Z", "2026-09-07T09:00:00Z"},
	} {
		t.Run(test.expression, func(t *testing.T) {
			normalized, err := Normalize(Spec{Preset: "CUSTOM", CronExpression: test.expression, Timezone: "UTC"}, mustTime(t, test.after))
			if err != nil || !normalized.Next.Equal(mustTime(t, test.want)) {
				t.Fatalf("next=%v err=%v", normalized.Next, err)
			}
			occurrence, _, err := ResolveDue(normalized.Spec, normalized.Next, normalized.Next.Add(500*time.Millisecond))
			if err != nil || occurrence == nil || !occurrence.Equal(normalized.Next) {
				t.Fatalf("persisted claim disagrees with create: %v", err)
			}
		})
	}
	for _, expression := range []string{"@hourly", "TZ=UTC 0 0 * * *", "0 0 31 2 *", "0 0 0 * * *", "*/0 * * * *", "60 * * * *"} {
		if _, err := Normalize(Spec{Preset: "CUSTOM", CronExpression: expression, Timezone: "UTC"}, time.Now()); err == nil {
			t.Fatalf("invalid cron accepted: %s", expression)
		}
	}
	for _, preset := range []Spec{
		{Preset: "HOURLY", Timezone: "UTC"},
		{Preset: "DAILY", TimeOfDay: "12:30", Timezone: "UTC"},
		{Preset: "WEEKDAYS", TimeOfDay: "12:30", Timezone: "UTC"},
		{Preset: "WEEKLY", TimeOfDay: "12:30", DayOfWeek: "MONDAY", Timezone: "UTC"},
	} {
		normalized, err := Normalize(preset, mustTime(t, "2026-09-04T09:01:00Z"))
		if err != nil {
			t.Fatal(err)
		}
		normalized.TimeOfDay, normalized.DayOfWeek = "", ""
		if _, _, err := ResolveDue(normalized.Spec, normalized.Next, normalized.Next); err != nil {
			t.Fatalf("persisted %s: %v", preset.Preset, err)
		}
	}
}

// TestDSTPresetCustomParityAndHalfHourFold checks preset and equivalent custom cron agree across
// Berlin gaps/folds and the non-hour offset fold of Lord Howe.
// TestDSTPresetCustomParityAndHalfHourFold проверяет совпадение preset и эквивалентного custom
// cron при gaps и folds Берлина и fold Lord Howe со смещением не в час.
func TestDSTPresetCustomParityAndHalfHourFold(t *testing.T) {
	for _, test := range []struct{ zone, after, cron, preset, clock, want string }{
		{"Europe/Berlin", "2026-10-25T00:01:00Z", "0 * * * *", "HOURLY", "", "2026-10-25T02:00:00Z"},
		{"Europe/Berlin", "2026-03-29T00:59:00Z", "30 2 * * *", "DAILY", "02:30", "2026-03-29T01:00:00Z"},
		{"Australia/Lord_Howe", "2026-04-04T14:40:00Z", "45 1 * * *", "DAILY", "01:45", "2026-04-04T14:45:00Z"},
		{"Australia/Lord_Howe", "2026-04-04T14:46:00Z", "45 1 * * *", "DAILY", "01:45", "2026-04-05T15:15:00Z"},
	} {
		for _, spec := range []Spec{
			{Preset: "CUSTOM", CronExpression: test.cron, Timezone: test.zone},
			{Preset: test.preset, TimeOfDay: test.clock, Timezone: test.zone},
		} {
			value, err := Normalize(spec, mustTime(t, test.after))
			if err != nil || !value.Next.Equal(mustTime(t, test.want)) {
				t.Fatalf("%s %s next=%s err=%v", spec.Preset, test.zone, value.Next, err)
			}
		}
	}
}

// TestSkipAllowsCurrentCronMinuteAndNormalizesDefault checks SKIP permits ordinary polling jitter
// inside the due minute and omitted misfire policy advances with default coalescing.
// TestSkipAllowsCurrentCronMinuteAndNormalizesDefault проверяет допуск обычной задержки polling
// внутри due минуты при SKIP и продвижение default coalesce при отсутствующей misfire policy.
func TestSkipAllowsCurrentCronMinuteAndNormalizesDefault(t *testing.T) {
	due := mustTime(t, "2026-09-04T09:00:00Z")
	spec := Spec{Preset: "CUSTOM", CronExpression: "0 * * * *", Timezone: "UTC", MisfirePolicy: MisfireSkip}
	occurrence, _, err := ResolveDue(spec, due, due.Add(25*time.Second))
	if err != nil || occurrence == nil {
		t.Fatalf("normal poll jitter skipped a due run: %v", err)
	}
	spec.MisfirePolicy = ""
	_, next, err := ResolveDue(spec, due, due.Add(4*time.Hour))
	if err != nil || !next.Equal(due.Add(5*time.Hour)) {
		t.Fatalf("default coalesce ignored: %v %v", next, err)
	}
}
