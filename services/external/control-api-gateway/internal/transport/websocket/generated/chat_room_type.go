
package generated

type ChatRoomType uint

const (
  ChatRoomTypeUser ChatRoomType = iota
  ChatRoomTypeCoordination
  ChatRoomTypeWorkControl
  ChatRoomTypeRuns
)

// Value returns the value of the enum.
func (op ChatRoomType) Value() any {
	if op >= ChatRoomType(len(ChatRoomTypeValues)) {
		return nil
	}
	return ChatRoomTypeValues[op]
}

var ChatRoomTypeValues = []any{"USER","COORDINATION","WORK_CONTROL","RUNS"}
var ValuesToChatRoomType = map[any]ChatRoomType{
  ChatRoomTypeValues[ChatRoomTypeUser]: ChatRoomTypeUser,
  ChatRoomTypeValues[ChatRoomTypeCoordination]: ChatRoomTypeCoordination,
  ChatRoomTypeValues[ChatRoomTypeWorkControl]: ChatRoomTypeWorkControl,
  ChatRoomTypeValues[ChatRoomTypeRuns]: ChatRoomTypeRuns,
}
