package websockettransport

import (
	"encoding/json"
	"testing"

	httpgenerated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	wsgenerated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/websocket/generated"
)

type strictEnumCase struct {
	name       string
	valid      []string
	newValue   func() json.Unmarshaler
	marshalRaw func(string) ([]byte, error)
}

func TestStrictBoundaryMatchesNamedGeneratedEnums(t *testing.T) {
	tests := []struct {
		name      string
		generated map[any]struct{}
		boundary  []string
	}{
		{name: "subscribe message type", generated: keys(wsgenerated.ValuesToSubscribeMessageType), boundary: []string{string(SubscribeMessageTypeSubscribe)}},
		{name: "snapshot message type", generated: keys(wsgenerated.ValuesToSnapshotMessageType), boundary: []string{string(SnapshotMessageTypeSnapshot)}},
		{name: "problem message type", generated: keys(wsgenerated.ValuesToProblemMessageType), boundary: []string{string(ProblemMessageTypeProblem)}},
		{name: "projection channel", generated: keys(wsgenerated.ValuesToProjectionChannel), boundary: []string{
			string(ProjectionChannelRuns), string(ProjectionChannelIncidents), string(ProjectionChannelResources), string(ProjectionChannelConfigurationChanges),
			string(ProjectionChannelWorkspaceTeams), string(ProjectionChannelProviders), string(ProjectionChannelIntegrations), string(ProjectionChannelApprovals),
			string(ProjectionChannelBackups), string(ProjectionChannelHealth),
		}},
		{name: "resource kind", generated: keys(wsgenerated.ValuesToResourceKind), boundary: []string{
			string(ResourceKindProject), string(ResourceKindTeam), string(ResourceKindChat), string(ResourceKindRole),
			string(ResourceKindPromptProfile), string(ResourceKindCredentialBinding), string(ResourceKindRepositoryWorkspace),
			string(ResourceKindIntegration), string(ResourceKindRuntimeRevision), string(ResourceKindSession), string(ResourceKindTurn),
			string(ResourceKindProcessRun), string(ResourceKindSchedule), string(ResourceKindOwnerGate), string(ResourceKindMemoryRecord),
			string(ResourceKindWorkClaim), string(ResourceKindArtifact), string(ResourceKindRoleImageRecipe),
			string(ResourceKindImageBuild), string(ResourceKindImageArtifact), string(ResourceKindRoleDefinition),
			string(ResourceKindAgent), string(ResourceKindAgentAssignment), string(ResourceKindInstructionSet),
			string(ResourceKindProviderConnection), string(ResourceKindProviderPool), string(ResourceKindWorkspaceBackup),
			string(ResourceKindWorkspaceRestore), string(ResourceKindWorkspaceMapping),
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if len(test.generated) != len(test.boundary) {
				t.Fatalf("generated and boundary enum sizes differ: %d != %d", len(test.generated), len(test.boundary))
			}
			for _, value := range test.boundary {
				if _, ok := test.generated[value]; !ok {
					t.Fatalf("boundary enum %q is absent from named generated contract", value)
				}
			}
		})
	}
}

func keys[T ~uint](values map[any]T) map[any]struct{} {
	result := make(map[any]struct{}, len(values))
	for value := range values {
		result[value] = struct{}{}
	}
	return result
}

func TestProjectionClosedEnumsFailBeforeWebSocketMarshal(t *testing.T) {
	ownership := func() httpgenerated.ConfigurationOwnershipProjection {
		return httpgenerated.ConfigurationOwnershipProjection{ManagedBy: httpgenerated.Ui}
	}
	validResource := func() httpgenerated.Resource {
		return httpgenerated.Resource{
			Kind:  httpgenerated.ResourceKindPROJECT,
			State: httpgenerated.LifecycleStateACTIVE,
			Spec: httpgenerated.ResourceSpecProjection{Project: &httpgenerated.ProjectProjection{
				Locale: httpgenerated.Ru, Ownership: ownership(),
			}},
		}
	}

	resourceCases := []struct {
		name   string
		mutate func(*httpgenerated.Resource)
	}{
		{name: "resource kind", mutate: func(value *httpgenerated.Resource) { value.Kind = "UNKNOWN" }},
		{name: "lifecycle state", mutate: func(value *httpgenerated.Resource) { value.State = "" }},
		{name: "project locale", mutate: func(value *httpgenerated.Resource) { value.Spec.Project.Locale = "UNKNOWN" }},
		{name: "project managed by", mutate: func(value *httpgenerated.Resource) { value.Spec.Project.Ownership.ManagedBy = "" }},
		{name: "chat room type", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindCHAT
			value.Spec = httpgenerated.ResourceSpecProjection{Chat: &httpgenerated.ChatProjection{RoomType: "UNKNOWN", Ownership: ownership()}}
		}},
		{name: "chat managed by", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindCHAT
			value.Spec = httpgenerated.ResourceSpecProjection{Chat: &httpgenerated.ChatProjection{RoomType: httpgenerated.USER, Ownership: ownership()}}
			value.Spec.Chat.Ownership.ManagedBy = "UNKNOWN"
		}},
		{name: "provider pool policy", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindROLE
			value.Spec = httpgenerated.ResourceSpecProjection{Role: &httpgenerated.RoleProjection{Ownership: ownership(), ProviderAccountPool: httpgenerated.ProviderPoolProjection{Policy: "UNKNOWN"}}}
		}},
		{name: "role managed by", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindROLE
			value.Spec = httpgenerated.ResourceSpecProjection{Role: &httpgenerated.RoleProjection{Ownership: ownership(), ProviderAccountPool: httpgenerated.ProviderPoolProjection{Policy: httpgenerated.ProviderPoolPolicyLeastUsed}}}
			value.Spec.Role.Ownership.ManagedBy = "UNKNOWN"
		}},
		{name: "schedule target kind", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindSCHEDULE
			value.Spec = httpgenerated.ResourceSpecProjection{Schedule: &httpgenerated.ScheduleProjection{TargetKind: "UNKNOWN", Ownership: ownership()}}
		}},
		{name: "schedule managed by", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindSCHEDULE
			value.Spec = httpgenerated.ResourceSpecProjection{Schedule: &httpgenerated.ScheduleProjection{TargetKind: httpgenerated.ResourceKindPROJECT, Ownership: ownership()}}
			value.Spec.Schedule.Ownership.ManagedBy = ""
		}},
		{name: "owner gate decision", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindOWNERGATE
			value.Spec = httpgenerated.ResourceSpecProjection{OwnerGate: &httpgenerated.OwnerGateProjection{Decision: "UNKNOWN"}}
		}},
		{name: "artifact scan status", mutate: func(value *httpgenerated.Resource) {
			value.Kind = httpgenerated.ResourceKindARTIFACT
			value.Spec = httpgenerated.ResourceSpecProjection{Artifact: &httpgenerated.ArtifactProjection{ScanStatus: "UNKNOWN"}}
		}},
	}
	for _, test := range resourceCases {
		t.Run(test.name, func(t *testing.T) {
			resource := validResource()
			test.mutate(&resource)
			if _, err := json.Marshal(SnapshotItems{Resources: []httpgenerated.Resource{resource}}); err == nil {
				t.Fatal("invalid closed projection enum was marshalled")
			}
		})
	}

	otherCases := []struct {
		name  string
		items SnapshotItems
	}{
		{name: "incident kind", items: SnapshotItems{Incidents: []httpgenerated.IncidentView{{Kind: "UNKNOWN"}}}},
		{name: "incident state", items: SnapshotItems{Incidents: []httpgenerated.IncidentView{{Kind: httpgenerated.HEARTBEATMISSED, State: "UNKNOWN"}}}},
		{name: "configuration action", items: SnapshotItems{ConfigurationChanges: []httpgenerated.ConfigurationChange{{Action: "UNKNOWN", Outcome: httpgenerated.Succeeded, ResourceKind: httpgenerated.ResourceKindPROJECT}}}},
		{name: "configuration outcome", items: SnapshotItems{ConfigurationChanges: []httpgenerated.ConfigurationChange{{Action: httpgenerated.ConfigurationChangeActionCreate, Outcome: "", ResourceKind: httpgenerated.ResourceKindPROJECT}}}},
		{name: "configuration resource kind", items: SnapshotItems{ConfigurationChanges: []httpgenerated.ConfigurationChange{{Action: httpgenerated.ConfigurationChangeActionCreate, Outcome: httpgenerated.Succeeded, ResourceKind: "UNKNOWN"}}}},
		{name: "team status", items: SnapshotItems{Teams: []httpgenerated.MattermostTeam{{Status: "UNKNOWN"}}}},
		{name: "provider connection state", items: SnapshotItems{ProviderConnections: []httpgenerated.ProviderConnection{{State: "UNKNOWN"}}}},
		{name: "approval status", items: SnapshotItems{Approvals: []httpgenerated.IntegrationApproval{{Status: "UNKNOWN"}}}},
		{name: "health source", items: SnapshotItems{Health: []httpgenerated.HealthObservation{{Source: "UNKNOWN", Status: httpgenerated.HealthObservationStatusOK}}}},
		{name: "health status", items: SnapshotItems{Health: []httpgenerated.HealthObservation{{Source: httpgenerated.CONTROLPLANE, Status: "OUT_OF_RANGE"}}}},
	}
	for _, test := range otherCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := json.Marshal(test.items); err == nil {
				t.Fatal("invalid closed projection enum was marshalled")
			}
		})
	}
}

func TestClosedWebSocketEnumsFailClosed(t *testing.T) {
	tests := []strictEnumCase{
		{
			name:  "subscribe message type",
			valid: []string{string(SubscribeMessageTypeSubscribe)},
			newValue: func() json.Unmarshaler {
				return new(SubscribeMessageType)
			},
			marshalRaw: func(value string) ([]byte, error) { return json.Marshal(SubscribeMessageType(value)) },
		},
		{
			name:  "snapshot message type",
			valid: []string{string(SnapshotMessageTypeSnapshot)},
			newValue: func() json.Unmarshaler {
				return new(SnapshotMessageType)
			},
			marshalRaw: func(value string) ([]byte, error) { return json.Marshal(SnapshotMessageType(value)) },
		},
		{
			name:  "problem message type",
			valid: []string{string(ProblemMessageTypeProblem)},
			newValue: func() json.Unmarshaler {
				return new(ProblemMessageType)
			},
			marshalRaw: func(value string) ([]byte, error) { return json.Marshal(ProblemMessageType(value)) },
		},
		{
			name: "projection channel",
			valid: []string{
				string(ProjectionChannelRuns),
				string(ProjectionChannelIncidents),
				string(ProjectionChannelResources),
				string(ProjectionChannelConfigurationChanges),
				string(ProjectionChannelWorkspaceTeams),
				string(ProjectionChannelProviders),
				string(ProjectionChannelIntegrations),
				string(ProjectionChannelApprovals),
				string(ProjectionChannelBackups),
				string(ProjectionChannelHealth),
			},
			newValue: func() json.Unmarshaler {
				return new(ProjectionChannel)
			},
			marshalRaw: func(value string) ([]byte, error) { return json.Marshal(ProjectionChannel(value)) },
		},
		{
			name: "resource kind",
			valid: []string{
				string(ResourceKindProject), string(ResourceKindTeam), string(ResourceKindChat),
				string(ResourceKindRole), string(ResourceKindPromptProfile), string(ResourceKindCredentialBinding),
				string(ResourceKindRepositoryWorkspace), string(ResourceKindIntegration), string(ResourceKindRuntimeRevision),
				string(ResourceKindSession), string(ResourceKindTurn), string(ResourceKindProcessRun),
				string(ResourceKindSchedule), string(ResourceKindOwnerGate), string(ResourceKindMemoryRecord),
				string(ResourceKindWorkClaim), string(ResourceKindArtifact), string(ResourceKindRoleImageRecipe),
				string(ResourceKindImageBuild), string(ResourceKindImageArtifact), string(ResourceKindRoleDefinition),
				string(ResourceKindAgent), string(ResourceKindAgentAssignment), string(ResourceKindInstructionSet),
				string(ResourceKindProviderConnection), string(ResourceKindProviderPool), string(ResourceKindWorkspaceBackup),
				string(ResourceKindWorkspaceRestore), string(ResourceKindWorkspaceMapping),
			},
			newValue: func() json.Unmarshaler {
				return new(ResourceKind)
			},
			marshalRaw: func(value string) ([]byte, error) { return json.Marshal(ResourceKind(value)) },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, raw := range []string{`"UNKNOWN"`, `""`, `null`} {
				target := test.newValue()
				if err := json.Unmarshal([]byte(raw), target); err == nil {
					t.Fatalf("invalid value %s was accepted", raw)
				}
			}
			if _, err := test.marshalRaw("OUT_OF_RANGE"); err == nil {
				t.Fatal("out-of-range value was marshalled")
			}
			for _, value := range test.valid {
				raw, err := test.marshalRaw(value)
				if err != nil {
					t.Fatalf("marshal %q: %v", value, err)
				}
				target := test.newValue()
				if err := json.Unmarshal(raw, target); err != nil {
					t.Fatalf("unmarshal %q: %v", value, err)
				}
				roundTrip, err := json.Marshal(target)
				if err != nil || string(roundTrip) != string(raw) {
					t.Fatalf("round-trip %q: %s, %v", value, roundTrip, err)
				}
			}
		})
	}
}
