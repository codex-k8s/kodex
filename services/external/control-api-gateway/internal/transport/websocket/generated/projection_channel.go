
package generated

type ProjectionChannel uint

const (
  ProjectionChannelRuns ProjectionChannel = iota
  ProjectionChannelIncidents
  ProjectionChannelResources
  ProjectionChannelConfigurationChanges
  ProjectionChannelWorkspaceTeams
  ProjectionChannelProviders
  ProjectionChannelIntegrations
  ProjectionChannelApprovals
  ProjectionChannelBackups
  ProjectionChannelHealth
)

// Value returns the value of the enum.
func (op ProjectionChannel) Value() any {
	if op >= ProjectionChannel(len(ProjectionChannelValues)) {
		return nil
	}
	return ProjectionChannelValues[op]
}

var ProjectionChannelValues = []any{"RUNS","INCIDENTS","RESOURCES","CONFIGURATION_CHANGES","WORKSPACE_TEAMS","PROVIDERS","INTEGRATIONS","APPROVALS","BACKUPS","HEALTH"}
var ValuesToProjectionChannel = map[any]ProjectionChannel{
  ProjectionChannelValues[ProjectionChannelRuns]: ProjectionChannelRuns,
  ProjectionChannelValues[ProjectionChannelIncidents]: ProjectionChannelIncidents,
  ProjectionChannelValues[ProjectionChannelResources]: ProjectionChannelResources,
  ProjectionChannelValues[ProjectionChannelConfigurationChanges]: ProjectionChannelConfigurationChanges,
  ProjectionChannelValues[ProjectionChannelWorkspaceTeams]: ProjectionChannelWorkspaceTeams,
  ProjectionChannelValues[ProjectionChannelProviders]: ProjectionChannelProviders,
  ProjectionChannelValues[ProjectionChannelIntegrations]: ProjectionChannelIntegrations,
  ProjectionChannelValues[ProjectionChannelApprovals]: ProjectionChannelApprovals,
  ProjectionChannelValues[ProjectionChannelBackups]: ProjectionChannelBackups,
  ProjectionChannelValues[ProjectionChannelHealth]: ProjectionChannelHealth,
}
