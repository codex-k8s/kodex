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
