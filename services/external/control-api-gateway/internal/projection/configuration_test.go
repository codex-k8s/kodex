package projection

import "testing"

func TestConfigurationActionRegistryIsClosed(t *testing.T) {
	for _, action := range []string{"create", "update", "transition", "delete", "detach_access_configuration", "copy_access_configuration", "create_schedule", "manage_schedule_UPDATE", "manage_schedule_DELETE"} {
		if !IsConfigurationAction(action) {
			t.Fatalf("known configuration action rejected: %q", action)
		}
	}
	for _, action := range []string{"", "record_runtime_incident", "manage_schedule", "create_resource", "CREATE"} {
		if IsConfigurationAction(action) {
			t.Fatalf("unknown configuration action accepted: %q", action)
		}
	}
}
