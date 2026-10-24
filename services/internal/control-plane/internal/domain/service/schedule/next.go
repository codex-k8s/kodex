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

// ErrInvalid marks an invalid schedule shape, unsupported execution policy or unavailable next
// occurrence with one fixed runtime error.
// ErrInvalid обозначает неверную форму расписания, неподдерживаемую policy выполнения либо
// отсутствие следующего запуска одной фиксированной runtime-ошибкой.
var ErrInvalid = errors.New("invalid schedule specification")

// Spec describes preset/custom cron and IANA timezone together with explicit DST, misfire and
// overlap policies for durable scheduling.
// Spec описывает preset либо custom cron и IANA timezone вместе с явными DST, misfire и overlap
// policies долговечного расписания.
type Spec struct {
	Preset, CronExpression, TimeOfDay, DayOfWeek, Timezone    string
	DSTGapPolicy, DSTFoldPolicy, MisfirePolicy, OverlapPolicy string
}

// Normalized pairs the normalized persisted schedule specification with its computed next UTC
// occurrence.
// Normalized связывает нормализованную сохраняемую спецификацию расписания с вычисленным следующим
// запуском в UTC.
type Normalized struct {
	Spec
	Next time.Time
}

// weekdays maps the closed uppercase weekday names to Go weekday values for weekly preset parsing
// and display reconstruction.
// weekdays отображает закрытые имена дней недели в верхнем регистре в значения Go для parsing
// weekly preset и восстановления отображения.
var weekdays = map[string]time.Weekday{
	"SUNDAY": time.Sunday, "MONDAY": time.Monday, "TUESDAY": time.Tuesday,
	"WEDNESDAY": time.Wednesday, "THURSDAY": time.Thursday,
	"FRIDAY": time.Friday, "SATURDAY": time.Saturday,
}

const (
	// DSTGapShiftForward selects the first existing local minute after a nonexistent scheduled wall
	// time during a timezone gap.
	// DSTGapShiftForward выбирает первую существующую локальную минуту после несуществующего времени
	// расписания при timezone gap.
	DSTGapShiftForward = "SHIFT_FORWARD"
	// DSTFoldRunOnce selects the earliest instant for a repeated local wall time and prevents
	// scheduling its later fold duplicate.
	// DSTFoldRunOnce выбирает наиболее ранний момент повторяющегося локального времени и предотвращает
	// повторный запуск позднего fold дубликата.
	DSTFoldRunOnce = "RUN_ONCE_EARLIEST"
	// MisfireCoalesce retains one due occurrence and advances the next cursor past now instead of
	// replaying every missed interval.
	// MisfireCoalesce сохраняет один наступивший запуск и продвигает следующий cursor за now вместо
	// воспроизведения всех пропущенных интервалов.
	MisfireCoalesce = "COALESCE"
	// MisfireCatchUpOne retains the due occurrence and advances only one scheduled step so the owner
	// can catch up incrementally.
	// MisfireCatchUpOne сохраняет наступивший запуск и продвигает расписание лишь на один шаг для
	// последовательного восполнения владельцем.
	MisfireCatchUpOne = "CATCH_UP_ONE"
	// MisfireSkip skips an occurrence delayed by at least a minute and advances beyond now while
	// allowing normal polling inside its scheduled minute.
	// MisfireSkip пропускает запуск с задержкой не менее минуты и продвигает расписание за now с
	// допуском обычного polling внутри назначенной минуты.
	MisfireSkip = "SKIP"
	// OverlapForbid records the closed policy that forbids overlapping runs; enforcement belongs to
	// the schedule owner rather than next-time calculation.
	// OverlapForbid задаёт закрытую policy запрета параллельных запусков; исполнение принадлежит
	// владельцу расписания, а не вычислению следующего времени.
	OverlapForbid = "FORBID"
	// OverlapAllow records the closed policy permitting overlapping runs for later owner-side
	// admission without changing cron occurrence calculation.
	// OverlapAllow задаёт закрытую policy допуска параллельных запусков для дальнейшего admission
	// владельца без изменения вычисления cron запусков.
	OverlapAllow = "ALLOW"
)

// standardParser parses only the five minute/hour/day-of-month/month/day-of-week cron fields
// without seconds or descriptor extensions.
// standardParser разбирает только пять cron полей минуты, часа, дня месяца, месяца и дня недели
// без секунд либо descriptor расширений.
var standardParser = robfigcron.NewParser(
	robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow,
)

// Normalize normalizes preset and policy names, validates timezone and closed execution policies,
// derives preset cron and computes a next occurrence with the same path as custom cron.
// Normalize нормализует имена preset и policy, проверяет timezone и закрытые policies выполнения,
// выводит cron preset и вычисляет следующий запуск тем же путём, что custom cron.
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

// Preview returns one to one hundred successive UTC occurrences from one normalized specification,
// propagating invalid input or an unavailable next occurrence.
// Preview возвращает от одного до ста последовательных UTC запусков по одной нормализованной
// спецификации с передачей ошибки ввода либо отсутствия следующего запуска.
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

// ResolveDue validates the persisted scheduled instant against normalized semantics and applies
// misfire policy; nil occurrence means not yet due or an explicit delayed SKIP, while next retains
// the cursor.
// ResolveDue проверяет сохраняемый момент запуска по нормализованной семантике и применяет misfire
// policy; nil occurrence означает ещё не наступивший срок либо явный delayed SKIP, а next
// сохраняет cursor.
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

// Display validates custom cron or reconstructs preset clock/weekday from its exact persisted
// five-field expression, rejecting mismatched preset semantics.
// Display проверяет custom cron либо восстанавливает часы и день недели preset из точного
// сохраняемого выражения из пяти полей с отказом несовпадающей семантике preset.
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

// Next computes the next occurrence with the fixed default gap, fold, coalesce and overlap
// policies through NextWithPolicy.
// Next вычисляет следующий запуск с фиксированными default policies gap, fold, coalesce и overlap
// через NextWithPolicy.
func Next(preset, cron, timezone string, after time.Time) (time.Time, error) {
	return NextWithPolicy(Spec{Preset: preset, CronExpression: cron, Timezone: timezone,
		DSTGapPolicy: DSTGapShiftForward, DSTFoldPolicy: DSTFoldRunOnce,
		MisfirePolicy: MisfireCoalesce, OverlapPolicy: OverlapForbid}, after)
}

// NextWithPolicy enumerates nominal cron dates on a DST-free calendar within five years and maps
// them into the requested zone, returning the first allowed instant strictly after the cursor.
// NextWithPolicy перебирает nominal cron даты на календаре без DST в пределах пяти лет и
// отображает их в запрошенную зону, возвращая первый допустимый момент строго после cursor.
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

// defaultPolicy trims and uppercases a policy and substitutes the supplied default only when the
// normalized value is empty.
// defaultPolicy обрезает и переводит policy в верхний регистр и подставляет переданное значение по
// умолчанию лишь при пустом нормализованном значении.
func defaultPolicy(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return fallback
	}
	return value
}

// oneOf checks exact membership in the supplied closed policy values without case conversion or
// treating unknown values as defaults.
// oneOf проверяет точное вхождение в переданные закрытые значения policy без смены регистра либо
// трактовки неизвестного значения как default.
func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

// parseCron bounds cron text to one hundred twenty-eight bytes and exactly five fields before
// using the shared standard parser, mapping any parser failure to ErrInvalid.
// parseCron ограничивает cron текст ста двадцатью восемью байтами и ровно пятью полями до общего
// standard parser с отображением любого отказа parser в ErrInvalid.
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

// parseTime accepts exactly two-character hour and minute components separated by one colon and
// validates their numeric clock ranges.
// parseTime принимает ровно двухсимвольные компоненты часа и минуты с одним двоеточием и проверяет
// числовые диапазоны часов.
func parseTime(value string) (int, int, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, false
	}
	hour, hourErr := strconv.Atoi(parts[0])
	minute, minuteErr := strconv.Atoi(parts[1])
	return hour, minute, hourErr == nil && minuteErr == nil && hour >= 0 && hour <= 23 && minute >= 0 && minute <= 59
}

// occurrenceOn resolves a nominal local minute using nearby zone offsets, choosing the earliest
// fold instant; for a gap it searches a bounded four-day span for the first existing wall time at
// or after nominal.
// occurrenceOn разрешает nominal локальную минуту по соседним zone offsets с выбором раннего fold
// момента; при gap ищет в ограниченном четырёхдневном диапазоне первое существующее локальное
// время не раньше nominal.
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
