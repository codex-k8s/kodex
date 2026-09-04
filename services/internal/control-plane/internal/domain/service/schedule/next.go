// Package schedule содержит доменные правила долговечного расписания.
package schedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	robfigcron "github.com/robfig/cron/v3"
)

var ErrInvalid = errors.New("invalid schedule specification")

type Spec struct {
	Preset, CronExpression, TimeOfDay, DayOfWeek, Timezone    string
	DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy string
}

type Normalized struct {
	Spec
	Next time.Time
}

var weekdays = map[string]time.Weekday{
	"SUNDAY": time.Sunday, "MONDAY": time.Monday, "TUESDAY": time.Tuesday,
	"WEDNESDAY": time.Wednesday, "THURSDAY": time.Thursday,
	"FRIDAY": time.Friday, "SATURDAY": time.Saturday,
}

const (
	DSTGapShiftForward = "SHIFT_FORWARD"
	DSTFoldRunOnce     = "RUN_ONCE_EARLIEST"
	MisfireCoalesce    = "COALESCE"
	MisfireCatchUpOne  = "CATCH_UP_ONE"
	MisfireSkip        = "SKIP"
	OverlapForbid      = "FORBID"
	OverlapAllow       = "ALLOW"
)

var standardParser = robfigcron.NewParser(
	robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow,
)

func Normalize(spec Spec, after time.Time) (Normalized, error) {
	spec.Preset = strings.ToUpper(strings.TrimSpace(spec.Preset))
	spec.TimeOfDay = strings.TrimSpace(spec.TimeOfDay)
	spec.DayOfWeek = strings.ToUpper(strings.TrimSpace(spec.DayOfWeek))
	spec.Timezone = strings.TrimSpace(spec.Timezone)
	spec.DSTGapPolicy = defaultPolicy(spec.DSTGapPolicy, DSTGapShiftForward)
	spec.DSTFoldPolicy = defaultPolicy(spec.DSTFoldPolicy, DSTFoldRunOnce)
	spec.MisfirePolicy = defaultPolicy(spec.MisfirePolicy, MisfireCoalesce)
	spec.OverlapPolicy = defaultPolicy(spec.OverlapPolicy, OverlapForbid)
	if spec.DSTGapPolicy != DSTGapShiftForward || spec.DSTFoldPolicy != DSTFoldRunOnce ||
		!oneOf(spec.MisfirePolicy, MisfireCoalesce, MisfireCatchUpOne, MisfireSkip) ||
		!oneOf(spec.OverlapPolicy, OverlapForbid, OverlapAllow) {
		return Normalized{}, ErrInvalid
	}
	if spec.Timezone == "" {
		return Normalized{}, ErrInvalid
	}
	if _, err := time.LoadLocation(spec.Timezone); err != nil {
		return Normalized{}, ErrInvalid
	}
	if spec.Preset == "CUSTOM" {
		spec.TimeOfDay = ""
		spec.DayOfWeek = ""
		cron := strings.Join(strings.Fields(spec.CronExpression), " ")
		if _, err := parseCron(cron); err != nil {
			return Normalized{}, ErrInvalid
		}
		spec.CronExpression = cron
		next, err := NextWithPolicy(spec, after)
		return Normalized{Spec: spec, Next: next}, err
	}
	if spec.Preset == "HOURLY" {
		spec.TimeOfDay = ""
		spec.DayOfWeek = ""
		spec.CronExpression = "0 * * * *"
		next, err := NextWithPolicy(spec, after)
		return Normalized{Spec: spec, Next: next}, err
	}
	hour, minute, valid := parseTime(spec.TimeOfDay)
	if !valid {
		return Normalized{}, ErrInvalid
	}
	cron := fmt.Sprintf("%d %d * * *", minute, hour)
	switch spec.Preset {
	case "DAILY":
		spec.DayOfWeek = ""
	case "WEEKDAYS":
		spec.DayOfWeek = ""
		cron = fmt.Sprintf("%d %d * * 1-5", minute, hour)
	case "WEEKLY":
		weekday, ok := weekdays[spec.DayOfWeek]
		if !ok {
			return Normalized{}, ErrInvalid
		}
		cron = fmt.Sprintf("%d %d * * %d", minute, hour, int(weekday))
	default:
		return Normalized{}, ErrInvalid
	}
	spec.CronExpression = cron
	next, err := NextWithPolicy(spec, after)
	if err != nil {
		return Normalized{}, err
	}
	return Normalized{Spec: spec, Next: next}, nil
}

func Preview(spec Spec, after time.Time, limit int) ([]time.Time, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	normalized, err := Normalize(spec, after)
	if err != nil {
		return nil, err
	}
	values := make([]time.Time, 0, limit)
	cursor := after
	for len(values) < limit {
		next, nextErr := NextWithPolicy(normalized.Spec, cursor)
		if nextErr != nil {
			return nil, nextErr
		}
		values = append(values, next)
		cursor = next
	}
	return values, nil
}

// ResolveDue применяет ту же семантику, что create/preview, и возвращает
// occurrence=nil только для явного SKIP пропущенного запуска.
func ResolveDue(spec Spec, scheduledFor, now time.Time) (*time.Time, time.Time, error) {
	if spec.Preset != "CUSTOM" {
		var err error
		spec.TimeOfDay, spec.DayOfWeek, err = Display(spec.Preset, spec.CronExpression)
		if err != nil {
			return nil, time.Time{}, err
		}
	}
	normalized, err := Normalize(spec, scheduledFor.Add(-time.Minute))
	if err != nil || !normalized.Next.Equal(scheduledFor) {
		return nil, time.Time{}, ErrInvalid
	}
	if now.Before(scheduledFor) {
		return nil, scheduledFor, nil
	}
	nextAfter := scheduledFor
	if normalized.MisfirePolicy == MisfireCoalesce || normalized.MisfirePolicy == MisfireSkip {
		nextAfter = now
	}
	next, err := NextWithPolicy(normalized.Spec, nextAfter)
	if err != nil {
		return nil, time.Time{}, err
	}
	if normalized.MisfirePolicy == MisfireSkip && !now.Before(scheduledFor.Add(time.Minute)) {
		return nil, next, nil
	}
	occurrence := scheduledFor
	return &occurrence, next, nil
}

func Display(preset, cron string) (string, string, error) {
	preset = strings.ToUpper(strings.TrimSpace(preset))
	if preset == "CUSTOM" {
		_, err := parseCron(strings.Join(strings.Fields(cron), " "))
		return "", "", err
	}
	fields := strings.Fields(cron)
	if len(fields) != 5 || fields[2] != "*" || fields[3] != "*" {
		return "", "", ErrInvalid
	}
	if preset == "HOURLY" {
		if cron != "0 * * * *" {
			return "", "", ErrInvalid
		}
		return "", "", nil
	}
	minute, minuteErr := strconv.Atoi(fields[0])
	hour, hourErr := strconv.Atoi(fields[1])
	if minuteErr != nil || hourErr != nil || minute < 0 || minute > 59 || hour < 0 || hour > 23 {
		return "", "", ErrInvalid
	}
	timeOfDay := fmt.Sprintf("%02d:%02d", hour, minute)
	switch preset {
	case "DAILY":
		if fields[4] != "*" {
			return "", "", ErrInvalid
		}
		return timeOfDay, "", nil
	case "WEEKDAYS":
		if fields[4] != "1-5" {
			return "", "", ErrInvalid
		}
		return timeOfDay, "", nil
	case "WEEKLY":
		day, err := strconv.Atoi(fields[4])
		if err != nil || day < 0 || day > 6 {
			return "", "", ErrInvalid
		}
		for name, weekday := range weekdays {
			if int(weekday) == day {
				return timeOfDay, name, nil
			}
		}
	}
	return "", "", ErrInvalid
}

func Next(preset, cron, timezone string, after time.Time) (time.Time, error) {
	return NextWithPolicy(Spec{Preset: preset, CronExpression: cron, Timezone: timezone,
		DSTGapPolicy: DSTGapShiftForward, DSTFoldPolicy: DSTFoldRunOnce,
		MisfirePolicy: MisfireCoalesce, OverlapPolicy: OverlapForbid}, after)
}

func NextWithPolicy(spec Spec, after time.Time) (time.Time, error) {
	if defaultPolicy(spec.DSTGapPolicy, DSTGapShiftForward) != DSTGapShiftForward ||
		defaultPolicy(spec.DSTFoldPolicy, DSTFoldRunOnce) != DSTFoldRunOnce {
		return time.Time{}, ErrInvalid
	}
	location, err := time.LoadLocation(spec.Timezone)
	if err != nil || spec.Timezone == "" {
		return time.Time{}, ErrInvalid
	}
	if _, _, err := Display(spec.Preset, spec.CronExpression); err != nil {
		return time.Time{}, err
	}
	parsed, err := parseCron(spec.CronExpression)
	if err != nil {
		return time.Time{}, err
	}
	// Cron вычисляется на номинальном календаре без DST; отображение в IANA
	// выполняется отдельно, одинаково для preset и CUSTOM.
	local := after.In(location)
	cursor := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC).Add(-time.Minute)
	limit := cursor.AddDate(5, 0, 0)
	for nominal := parsed.Next(cursor); !nominal.IsZero() && nominal.Before(limit); nominal = parsed.Next(nominal) {
		candidate := occurrenceOn(nominal, nominal.Hour(), nominal.Minute(), location)
		if candidate.After(after) {
			return candidate.UTC(), nil
		}
	}
	return time.Time{}, ErrInvalid
}

func defaultPolicy(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseCron(expression string) (robfigcron.Schedule, error) {
	if len(expression) > 128 || len(strings.Fields(expression)) != 5 {
		return nil, ErrInvalid
	}
	parsed, err := standardParser.Parse(expression)
	if err != nil {
		return nil, ErrInvalid
	}
	return parsed, nil
}

func parseTime(value string) (int, int, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	return hour, minute, hourErr == nil && minuteErr == nil && hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

func occurrenceOn(date time.Time, hour, minute int, location *time.Location) time.Time {
	nominal := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, time.UTC)
	guess := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, location)
	var earliest time.Time
	// Смещение перехода не обязано равняться часу (например, Lord Howe).
	for _, delta := range []time.Duration{-48 * time.Hour, 0, 48 * time.Hour} {
		_, offset := guess.Add(delta).Zone()
		candidate := nominal.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(location)
		if local.Year() == date.Year() && local.Month() == date.Month() && local.Day() == date.Day() &&
			local.Hour() == hour && local.Minute() == minute && (earliest.IsZero() || candidate.Before(earliest)) {
			earliest = candidate
		}
	}
	if !earliest.IsZero() {
		return earliest
	}
	// Gap сдвигается к первому существующему локальному времени после nominal.
	// Обход ограничен четырьмя сутками, включая пропуск календарного дня.
	for candidate := guess.Add(-48 * time.Hour); !candidate.After(guess.Add(48 * time.Hour)); candidate = candidate.Add(time.Minute) {
		local := candidate.In(location)
		wall := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, time.UTC)
		if !wall.Before(nominal) {
			return candidate
		}
	}
	return guess
}
