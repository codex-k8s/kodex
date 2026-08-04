package generated

import (
	"encoding/json"
)

type AnonymousSchema_193 uint

const (
	AnonymousSchema_193Create AnonymousSchema_193 = iota
	AnonymousSchema_193Update
	AnonymousSchema_193Transition
	AnonymousSchema_193Delete
	AnonymousSchema_193DetachAccessConfiguration
	AnonymousSchema_193CopyAccessConfiguration
	AnonymousSchema_193CreateSchedule
	AnonymousSchema_193ManageScheduleUpdate
	AnonymousSchema_193ManageScheduleActivate
	AnonymousSchema_193ManageSchedulePause
	AnonymousSchema_193ManageScheduleArchive
	AnonymousSchema_193ManageScheduleDelete
)

// Value returns the value of the enum.
func (op AnonymousSchema_193) Value() any {
	if op >= AnonymousSchema_193(len(AnonymousSchema_193Values)) {
		return nil
	}
	return AnonymousSchema_193Values[op]
}

var AnonymousSchema_193Values = []any{"create", "update", "transition", "delete", "detach_access_configuration", "copy_access_configuration", "create_schedule", "manage_schedule_UPDATE", "manage_schedule_ACTIVATE", "manage_schedule_PAUSE", "manage_schedule_ARCHIVE", "manage_schedule_DELETE"}
var ValuesToAnonymousSchema_193 = map[any]AnonymousSchema_193{
	AnonymousSchema_193Values[AnonymousSchema_193Create]:                    AnonymousSchema_193Create,
	AnonymousSchema_193Values[AnonymousSchema_193Update]:                    AnonymousSchema_193Update,
	AnonymousSchema_193Values[AnonymousSchema_193Transition]:                AnonymousSchema_193Transition,
	AnonymousSchema_193Values[AnonymousSchema_193Delete]:                    AnonymousSchema_193Delete,
	AnonymousSchema_193Values[AnonymousSchema_193DetachAccessConfiguration]: AnonymousSchema_193DetachAccessConfiguration,
	AnonymousSchema_193Values[AnonymousSchema_193CopyAccessConfiguration]:   AnonymousSchema_193CopyAccessConfiguration,
	AnonymousSchema_193Values[AnonymousSchema_193CreateSchedule]:            AnonymousSchema_193CreateSchedule,
	AnonymousSchema_193Values[AnonymousSchema_193ManageScheduleUpdate]:      AnonymousSchema_193ManageScheduleUpdate,
	AnonymousSchema_193Values[AnonymousSchema_193ManageScheduleActivate]:    AnonymousSchema_193ManageScheduleActivate,
	AnonymousSchema_193Values[AnonymousSchema_193ManageSchedulePause]:       AnonymousSchema_193ManageSchedulePause,
	AnonymousSchema_193Values[AnonymousSchema_193ManageScheduleArchive]:     AnonymousSchema_193ManageScheduleArchive,
	AnonymousSchema_193Values[AnonymousSchema_193ManageScheduleDelete]:      AnonymousSchema_193ManageScheduleDelete,
}

func (op *AnonymousSchema_193) UnmarshalJSON(raw []byte) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*op = ValuesToAnonymousSchema_193[v]
	return nil
}

func (op AnonymousSchema_193) MarshalJSON() ([]byte, error) {
	return json.Marshal(op.Value())
}
