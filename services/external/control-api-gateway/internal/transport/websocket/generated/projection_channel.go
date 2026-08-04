
package generated

type ProjectionChannel uint

const (
  ProjectionChannelRuns ProjectionChannel = iota
  ProjectionChannelIncidents
  ProjectionChannelResources
  ProjectionChannelConfigurationChanges
)

// Value returns the value of the enum.
func (op ProjectionChannel) Value() any {
	if op >= ProjectionChannel(len(ProjectionChannelValues)) {
		return nil
	}
	return ProjectionChannelValues[op]
}

var ProjectionChannelValues = []any{"RUNS","INCIDENTS","RESOURCES","CONFIGURATION_CHANGES"}
var ValuesToProjectionChannel = map[any]ProjectionChannel{
  ProjectionChannelValues[ProjectionChannelRuns]: ProjectionChannelRuns,
  ProjectionChannelValues[ProjectionChannelIncidents]: ProjectionChannelIncidents,
  ProjectionChannelValues[ProjectionChannelResources]: ProjectionChannelResources,
  ProjectionChannelValues[ProjectionChannelConfigurationChanges]: ProjectionChannelConfigurationChanges,
}
