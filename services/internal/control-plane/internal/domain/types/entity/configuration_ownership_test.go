package entity

import "testing"

func TestConfigurationOwnershipAuthoritativeDrift(t *testing.T) {
	tests := []struct {
		name      string
		ownership ConfigurationOwnership
		want      string
	}{
		{name: "ui legacy", ownership: ConfigurationOwnership{ManagedBy: "UI"}, want: "NOT_APPLICABLE"},
		{name: "git legacy", ownership: ConfigurationOwnership{ManagedBy: "GIT"}, want: "UNKNOWN"},
		{name: "git drifted", ownership: ConfigurationOwnership{ManagedBy: "GIT", Drift: "DRIFTED"}, want: "DRIFTED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.ownership.AuthoritativeDrift(); got != test.want {
				t.Fatalf("authoritative drift: got %s want %s", got, test.want)
			}
		})
	}
	if err := (ConfigurationOwnership{ManagedBy: "UI", Drift: "IN_SYNC"}).Validate(); err == nil {
		t.Fatal("UI ownership accepted git-only drift")
	}
	if err := (ConfigurationOwnership{ManagedBy: "GIT", SourceRef: "git://owner/config", SourceRevision: 1, Drift: "LOCAL"}).Validate(); err == nil {
		t.Fatal("git ownership accepted unknown drift")
	}
}
