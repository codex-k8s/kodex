
package generated

type SnapshotMessageType uint

const (
  SnapshotMessageTypeSnapshot SnapshotMessageType = iota
)

// Value returns the value of the enum.
func (op SnapshotMessageType) Value() any {
	if op >= SnapshotMessageType(len(SnapshotMessageTypeValues)) {
		return nil
	}
	return SnapshotMessageTypeValues[op]
}

var SnapshotMessageTypeValues = []any{"SNAPSHOT"}
var ValuesToSnapshotMessageType = map[any]SnapshotMessageType{
  SnapshotMessageTypeValues[SnapshotMessageTypeSnapshot]: SnapshotMessageTypeSnapshot,
}
