package service

import (
	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	fleetevents "github.com/codex-k8s/kodex/libs/go/platformevents/fleet"
)

const (
	platformDefaultKey = "platform-default"

	fleetEventScopeCreated    = fleetevents.EventScopeCreated
	fleetEventScopeUpdated    = fleetevents.EventScopeUpdated
	fleetEventScopeDisabled   = fleetevents.EventScopeDisabled
	fleetEventScopeEnabled    = fleetevents.EventScopeEnabled
	fleetEventServerCreated   = fleetevents.EventServerCreated
	fleetEventServerUpdated   = fleetevents.EventServerUpdated
	fleetEventServerDisabled  = fleetevents.EventServerDisabled
	fleetEventServerEnabled   = fleetevents.EventServerEnabled
	fleetEventClusterCreated  = fleetevents.EventClusterCreated
	fleetEventClusterUpdated  = fleetevents.EventClusterUpdated
	fleetEventClusterDisabled = fleetevents.EventClusterDisabled
	fleetEventClusterEnabled  = fleetevents.EventClusterEnabled

	fleetAggregateScope   = fleetevents.AggregateFleetScope
	fleetAggregateServer  = fleetevents.AggregateServer
	fleetAggregateCluster = fleetevents.AggregateKubernetesCluster

	fleetOperationCreateScope     = "domain.Service.CreateFleetScope"
	fleetOperationUpdateScope     = "domain.Service.UpdateFleetScope"
	fleetOperationDisableScope    = "domain.Service.DisableFleetScope"
	fleetOperationEnableScope     = "domain.Service.EnableFleetScope"
	fleetOperationRegisterServer  = "domain.Service.RegisterServer"
	fleetOperationUpdateServer    = "domain.Service.UpdateServer"
	fleetOperationDisableServer   = "domain.Service.DisableServer"
	fleetOperationEnableServer    = "domain.Service.EnableServer"
	fleetOperationRegisterCluster = "domain.Service.RegisterKubernetesCluster"
	fleetOperationUpdateCluster   = "domain.Service.UpdateKubernetesCluster"
	fleetOperationDisableCluster  = "domain.Service.DisableKubernetesCluster"
	fleetOperationEnableCluster   = "domain.Service.EnableKubernetesCluster"

	fleetActionScopeCreate     = accesscatalog.ActionFleetScopeCreate
	fleetActionScopeUpdate     = accesscatalog.ActionFleetScopeUpdate
	fleetActionScopeDisable    = accesscatalog.ActionFleetScopeDisable
	fleetActionScopeEnable     = accesscatalog.ActionFleetScopeEnable
	fleetActionScopeRead       = accesscatalog.ActionFleetScopeRead
	fleetActionScopeList       = accesscatalog.ActionFleetScopeList
	fleetActionServerRegister  = accesscatalog.ActionFleetServerRegister
	fleetActionServerUpdate    = accesscatalog.ActionFleetServerUpdate
	fleetActionServerDisable   = accesscatalog.ActionFleetServerDisable
	fleetActionServerEnable    = accesscatalog.ActionFleetServerEnable
	fleetActionServerRead      = accesscatalog.ActionFleetServerRead
	fleetActionServerList      = accesscatalog.ActionFleetServerList
	fleetActionClusterRegister = accesscatalog.ActionFleetClusterRegister
	fleetActionClusterUpdate   = accesscatalog.ActionFleetClusterUpdate
	fleetActionClusterDisable  = accesscatalog.ActionFleetClusterDisable
	fleetActionClusterEnable   = accesscatalog.ActionFleetClusterEnable
	fleetActionClusterRead     = accesscatalog.ActionFleetClusterRead
	fleetActionClusterList     = accesscatalog.ActionFleetClusterList
)

type resourceRef struct {
	Type      string
	ID        string
	ScopeType string
	ScopeID   string
}
