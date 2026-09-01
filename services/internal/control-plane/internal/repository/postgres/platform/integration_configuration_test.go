package platform

import (
	"reflect"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func TestValidateIntegrationConfigurationUsesClosedTypedSchema(t *testing.T) {
	t.Parallel()
	fields := []entity.IntegrationConfigurationField{
		{Key: "server_url", ValueType: "URL", Required: true},
		{Key: "allowed_namespaces", ValueType: "STRING_LIST", Required: true},
	}
	input := map[string]any{
		"server_url":         "https://cluster.example.test/",
		"allowed_namespaces": []any{"sales", "support", "sales"},
	}
	got, ok := validateIntegrationConfiguration(fields, input)
	if !ok {
		t.Fatal("valid typed public configuration was rejected")
	}
	want := map[string]any{"server_url": "https://cluster.example.test", "allowed_namespaces": []string{"sales", "support"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected normalized configuration: got=%#v want=%#v", got, want)
	}
	for name, invalid := range map[string]map[string]any{
		"unknown field": {"server_url": "https://cluster.example.test", "allowed_namespaces": []any{"sales"}, "token": "not-public"},
		"plaintext URL": {"server_url": "http://cluster.example.test", "allowed_namespaces": []any{"sales"}},
		"empty list":    {"server_url": "https://cluster.example.test", "allowed_namespaces": []any{}},
		"wrong type":    {"server_url": "https://cluster.example.test", "allowed_namespaces": "sales"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, valid := validateIntegrationConfiguration(fields, invalid); valid {
				t.Fatal("invalid public configuration was accepted")
			}
		})
	}
}

func TestConnectionActionsAreServerOwnedAndPermissionAware(t *testing.T) {
	t.Parallel()
	connected := entity.IntegrationConnection{Enabled: true, State: "CONNECTED", MaskedCredentialsState: "CONFIGURED", CredentialSecretKey: "token"}
	if got := connectionActions(connected, false, false); !reflect.DeepEqual(got, []string{"OPEN"}) {
		t.Fatalf("read-only actor received mutation actions: %v", got)
	}
	if got := connectionActions(connected, false, true); !reflect.DeepEqual(got, []string{"OPEN", "MANAGE_GRANTS"}) {
		t.Fatalf("project integration manager actions: %v", got)
	}
	if got := connectionActions(connected, true, true); !reflect.DeepEqual(got, []string{"OPEN", "EDIT", "CONFIGURE_CREDENTIAL", "TEST", "DISABLE", "MANAGE_GRANTS"}) {
		t.Fatalf("platform integration manager actions: %v", got)
	}
	testingState := entity.IntegrationConnection{Enabled: true, State: "TESTING", MaskedCredentialsState: "CONFIGURED", CredentialSecretKey: "token"}
	if got := connectionActions(testingState, true, true); !reflect.DeepEqual(got, []string{"OPEN", "EDIT", "DISABLE"}) {
		t.Fatalf("testing connection exposed conflicting actions: %v", got)
	}
	disabled := entity.IntegrationConnection{Enabled: false, State: "DISABLED"}
	if got := connectionActions(disabled, true, true); !reflect.DeepEqual(got, []string{"OPEN", "EDIT", "ENABLE", "DELETE"}) {
		t.Fatalf("disabled connection lifecycle actions: %v", got)
	}
}

func TestDeletedCommandReplayRequiresExactCompleteTerminalSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	expectedVersion := int64(7)
	connectionCommand := command.Command{
		Kind: command.DeleteConnection, Mutation: value.Mutation{ExpectedVersion: &expectedVersion},
		Payload: command.ConnectionInput{Ref: "int_example"},
	}
	connectionResult := command.Result{Connection: &entity.IntegrationConnection{
		Ref: "int_example", Version: 8, State: "DELETED", LifecycleState: "DELETED",
		DefinitionKey: "synthetic", DefinitionName: "Synthetic", Name: "Journal",
		DefinitionVersion: "1", DefinitionDigest: "digest", PublicConfiguration: map[string]any{},
		CreatedAt: now, UpdatedAt: now,
	}}
	if !matchesDeletedCommand(connectionCommand, connectionResult) {
		t.Fatal("complete connection terminal snapshot was not replayable")
	}
	partialConnection := connectionResult
	partialConnection.Connection = &entity.IntegrationConnection{Ref: "int_example", Version: 8, State: "DELETED", LifecycleState: "DELETED"}
	if matchesDeletedCommand(connectionCommand, partialConnection) {
		t.Fatal("partial connection terminal snapshot was replayable")
	}
	wrongVersion := int64(8)
	connectionCommand.Mutation.ExpectedVersion = &wrongVersion
	if matchesDeletedCommand(connectionCommand, connectionResult) {
		t.Fatal("connection receipt was replayable for a different expected version")
	}

	scheduleCommand := command.Command{
		Kind: command.DeleteSchedule, Mutation: value.Mutation{ExpectedVersion: &expectedVersion},
		Payload: command.ScheduleInput{Ref: "sch_example"},
	}
	scheduleResult := command.Result{Schedule: &entity.Schedule{
		Ref: "sch_example", ProjectRef: "prj_example", Name: "Daily", State: "DELETED", Version: 8,
		Target: entity.RunTarget{Type: "AGENT", Ref: "agt_example"}, Input: map[string]any{},
		CreatedAt: now, UpdatedAt: now,
	}}
	if !matchesDeletedCommand(scheduleCommand, scheduleResult) {
		t.Fatal("complete schedule terminal snapshot was not replayable")
	}

	environmentCommand := command.Command{
		Kind: command.DeleteRuntimeEnvironment, Mutation: value.Mutation{ExpectedVersion: &expectedVersion},
		Payload: command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: "renv_example"},
	}
	environmentResult := command.Result{RuntimeEnvironment: &entity.RuntimeEnvironmentSet{
		Ref: "renv_example", ProjectRef: "prj_example", Name: "Runtime", State: "DELETED", Version: 8,
		CurrentVersion: entity.RuntimeEnvironmentVersion{Ref: "renvv_example", Digest: "digest", CreatedAt: now},
		UpdatedAt:      now,
	}}
	if !matchesDeletedCommand(environmentCommand, environmentResult) {
		t.Fatal("complete runtime environment terminal snapshot was not replayable")
	}
}
