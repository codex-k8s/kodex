// Package schedule содержит доменные правила долговечного расписания.
package schedule

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

var ErrInvalid = errors.New("invalid schedule specification")

type Spec struct {
	Preset, TimeOfDay, DayOfWeek, Timezone string
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

func Normalize(spec Spec, after time.Time) (Normalized, error) {
	spec.Preset = strings.ToUpper(strings.TrimSpace(spec.Preset))
	spec.TimeOfDay = strings.TrimSpace(spec.TimeOfDay)
	spec.DayOfWeek = strings.ToUpper(strings.TrimSpace(spec.DayOfWeek))
	spec.Timezone = strings.TrimSpace(spec.Timezone)
	if spec.Timezone == "" {
		return Normalized{}, ErrInvalid
	}
	if _, err := time.LoadLocation(spec.Timezone); err != nil {
		return Normalized{}, ErrInvalid
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

func Display(preset, cron string) (string, string, error) {
	preset = strings.ToUpper(strings.TrimSpace(preset))
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
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	timeOfDay, dayOfWeek, err := Display(preset, cron)
	if err != nil {
		return time.Time{}, err
	}
	localAfter := after.In(location)
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
