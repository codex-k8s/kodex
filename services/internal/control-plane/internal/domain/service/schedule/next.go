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
	Preset, CronExpression, TimeOfDay, DayOfWeek, Timezone string
	DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy string
}

type Normalized struct {
	Spec
	CronExpression string
	Next           time.Time
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
		if _, err := standardParser.Parse(cron); err != nil {
			return Normalized{}, ErrInvalid
		}
		if _, err := parseCron(cron); err != nil {
			return Normalized{}, ErrInvalid
		}
		next, err := Next(spec.Preset, cron, spec.Timezone, after)
		return Normalized{Spec: spec, CronExpression: cron, Next: next}, err
	}
	if spec.Preset == "HOURLY" {
		spec.TimeOfDay = ""
		spec.DayOfWeek = ""
		next, err := Next(spec.Preset, "0 * * * *", spec.Timezone, after)
		return Normalized{Spec: spec, CronExpression: "0 * * * *", Next: next}, err
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
	next, err := Next(spec.Preset, cron, spec.Timezone, after)
	if err != nil {
		return Normalized{}, err
	}
	return Normalized{Spec: spec, CronExpression: cron, Next: next}, nil
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
	normalized, err := Normalize(spec, scheduledFor.Add(-time.Minute))
	if err != nil || !normalized.Next.Equal(scheduledFor) {
		return nil, time.Time{}, ErrInvalid
	}
	if now.Before(scheduledFor) {
		return nil, scheduledFor, nil
	}
	nextAfter := scheduledFor
	if spec.MisfirePolicy == MisfireCoalesce || spec.MisfirePolicy == MisfireSkip {
		nextAfter = now
	}
	next, err := NextWithPolicy(normalized.Spec, nextAfter)
	if err != nil {
		return nil, time.Time{}, err
	}
	if normalized.MisfirePolicy == MisfireSkip && now.After(scheduledFor) {
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
	preset, cron, timezone := spec.Preset, spec.CronExpression, spec.Timezone
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	timeOfDay, dayOfWeek, err := Display(preset, cron)
	if err != nil {
		return time.Time{}, err
	}
	localAfter := after.In(location)
	if strings.EqualFold(strings.TrimSpace(preset), "CUSTOM") {
		specification, parseErr := parseCron(strings.Join(strings.Fields(cron), " "))
		if parseErr != nil {
			return time.Time{}, ErrInvalid
		}
		candidate := localAfter.Truncate(time.Minute).Add(time.Minute)
		for scanned := 0; scanned < 5*366*24*60; scanned++ {
			if specification.matches(candidate) && !duplicateFold(candidate, after, location, spec.DSTFoldPolicy) {
				return candidate.UTC(), nil
			}
			candidate = candidate.Add(time.Minute)
		}
		return time.Time{}, ErrInvalid
	}
	if preset == "HOURLY" {
		candidate := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day(), localAfter.Hour(), 0, 0, 0, location)
		for !candidate.After(after) {
			candidate = candidate.Add(time.Hour)
		}
		return candidate.UTC(), nil
	}
	hour, minute, _ := parseTime(timeOfDay)
	for offset := 0; offset <= 8; offset++ {
		date := time.Date(localAfter.Year(), localAfter.Month(), localAfter.Day()+offset, 0, 0, 0, 0, location)
		if preset == "WEEKDAYS" && (date.Weekday() == time.Saturday || date.Weekday() == time.Sunday) {
			continue
		}
		if preset == "WEEKLY" && date.Weekday() != weekdays[dayOfWeek] {
			continue
		}
		candidate := occurrenceOn(date, hour, minute, location)
		if candidate.After(after) {
			return candidate.UTC(), nil
		}
	}
	return time.Time{}, ErrInvalid
}

func duplicateFold(candidate, after time.Time, location *time.Location, policy string) bool {
	if policy != DSTFoldRunOnce {
		return false
	}
	local := candidate.In(location)
	for _, delta := range []time.Duration{-time.Hour, -2 * time.Hour} {
		previous := candidate.Add(delta)
		previousLocal := previous.In(location)
		if previous.After(after) || previousLocal.Year() != local.Year() || previousLocal.YearDay() != local.YearDay() ||
			previousLocal.Hour() != local.Hour() || previousLocal.Minute() != local.Minute() {
			continue
		}
		return true
	}
	return false
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

type cronSpecification struct {
	minute, hour, dayOfMonth, month, dayOfWeek map[int]struct{}
	anyDayOfMonth, anyDayOfWeek                bool
}

func (specification cronSpecification) matches(value time.Time) bool {
	_, minute := specification.minute[value.Minute()]
	_, hour := specification.hour[value.Hour()]
	_, month := specification.month[int(value.Month())]
	_, dayOfMonth := specification.dayOfMonth[value.Day()]
	_, dayOfWeek := specification.dayOfWeek[int(value.Weekday())]
	dayMatches := dayOfMonth && dayOfWeek
	if !specification.anyDayOfMonth && !specification.anyDayOfWeek {
		dayMatches = dayOfMonth || dayOfWeek
	}
	return minute && hour && month && dayMatches
}

func parseCron(expression string) (cronSpecification, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 || len(expression) > 128 {
		return cronSpecification{}, ErrInvalid
	}
	minute, _, err := parseCronField(fields[0], 0, 59, false)
	if err != nil {
		return cronSpecification{}, err
	}
	hour, _, err := parseCronField(fields[1], 0, 23, false)
	if err != nil {
		return cronSpecification{}, err
	}
	dayOfMonth, anyDayOfMonth, err := parseCronField(fields[2], 1, 31, false)
	if err != nil {
		return cronSpecification{}, err
	}
	month, _, err := parseCronField(fields[3], 1, 12, false)
	if err != nil {
		return cronSpecification{}, err
	}
	dayOfWeek, anyDayOfWeek, err := parseCronField(fields[4], 0, 7, true)
	if err != nil {
		return cronSpecification{}, err
	}
	return cronSpecification{minute: minute, hour: hour, dayOfMonth: dayOfMonth, month: month,
		dayOfWeek: dayOfWeek, anyDayOfMonth: anyDayOfMonth, anyDayOfWeek: anyDayOfWeek}, nil
}

func parseCronField(field string, minimum, maximum int, normalizeSunday bool) (map[int]struct{}, bool, error) {
	values := make(map[int]struct{}, maximum-minimum+1)
	any := field == "*"
	for _, segment := range strings.Split(field, ",") {
		if segment == "" {
			return nil, false, ErrInvalid
		}
		base, step := segment, 1
		parts := strings.Split(segment, "/")
		if len(parts) == 2 {
			base = parts[0]
			parsedStep, err := strconv.Atoi(parts[1])
			if err != nil || parsedStep < 1 || parsedStep > maximum-minimum+1 {
				return nil, false, ErrInvalid
			}
			step = parsedStep
		} else if len(parts) != 1 {
			return nil, false, ErrInvalid
		}
		start, end := minimum, maximum
		if base != "*" {
			bounds := strings.Split(base, "-")
			parsedStart, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, false, ErrInvalid
			}
			start, end = parsedStart, parsedStart
			if len(bounds) == 2 {
				parsedEnd, endErr := strconv.Atoi(bounds[1])
				if endErr != nil {
					return nil, false, ErrInvalid
				}
				end = parsedEnd
			} else if len(bounds) != 1 {
				return nil, false, ErrInvalid
			}
		}
		if start < minimum || end > maximum || start > end {
			return nil, false, ErrInvalid
		}
		for raw := start; raw <= end; raw += step {
			value := raw
			if normalizeSunday && value == 7 {
				value = 0
			}
			values[value] = struct{}{}
		}
	}
	if len(values) == 0 {
		return nil, false, ErrInvalid
	}
	return values, any, nil
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
	candidate := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, location)
	local := candidate.In(location)
	if local.Year() == date.Year() && local.Month() == date.Month() && local.Day() == date.Day() && local.Hour() == hour && local.Minute() == minute {
		return candidate
	}
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	requestedMinute := hour*60 + minute
	for offset := 0; offset < 26*60; offset++ {
		value := start.Add(time.Duration(offset) * time.Minute).In(location)
		if value.Year() == date.Year() && value.Month() == date.Month() && value.Day() == date.Day() && value.Hour()*60+value.Minute() >= requestedMinute {
			return value
		}
	}
	return candidate
}
