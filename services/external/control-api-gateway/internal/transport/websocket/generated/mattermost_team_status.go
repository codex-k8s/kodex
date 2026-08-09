
package generated

type MattermostTeamStatus uint

const (
  MattermostTeamStatusActive MattermostTeamStatus = iota
  MattermostTeamStatusDeleted
)

// Value returns the value of the enum.
func (op MattermostTeamStatus) Value() any {
	if op >= MattermostTeamStatus(len(MattermostTeamStatusValues)) {
		return nil
	}
	return MattermostTeamStatusValues[op]
}

var MattermostTeamStatusValues = []any{"ACTIVE","DELETED"}
var ValuesToMattermostTeamStatus = map[any]MattermostTeamStatus{
  MattermostTeamStatusValues[MattermostTeamStatusActive]: MattermostTeamStatusActive,
  MattermostTeamStatusValues[MattermostTeamStatusDeleted]: MattermostTeamStatusDeleted,
}
