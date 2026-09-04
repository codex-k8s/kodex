package schedule

import (
	"testing"
	"time"
)

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

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
