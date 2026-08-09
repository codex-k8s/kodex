
package generated

type ScheduleCalendar uint

const (
  ScheduleCalendarGregorian ScheduleCalendar = iota
  ScheduleCalendarBusiness
)

// Value returns the value of the enum.
func (op ScheduleCalendar) Value() any {
	if op >= ScheduleCalendar(len(ScheduleCalendarValues)) {
		return nil
	}
	return ScheduleCalendarValues[op]
}

var ScheduleCalendarValues = []any{"GREGORIAN","BUSINESS"}
var ValuesToScheduleCalendar = map[any]ScheduleCalendar{
  ScheduleCalendarValues[ScheduleCalendarGregorian]: ScheduleCalendarGregorian,
  ScheduleCalendarValues[ScheduleCalendarBusiness]: ScheduleCalendarBusiness,
}
