package platform

import (
	"reflect"
	"testing"
)

func TestCollectionCreateActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, role, action string
		want               []string
	}{
		{name: "owner", role: "OWNER", action: "CREATE_PROJECT", want: []string{"CREATE_PROJECT"}},
		{name: "administrator", role: "ADMINISTRATOR", action: "CREATE_CONNECTION", want: []string{"CREATE_CONNECTION"}},
		{name: "operator", role: "OPERATOR", action: "CREATE_PROJECT", want: []string{}},
		{name: "member", role: "MEMBER", action: "CREATE_CONNECTION", want: []string{}},
		{name: "auditor", role: "AUDITOR", action: "CREATE_PROJECT", want: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := collectionCreateActions(test.role, test.action); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("collectionCreateActions(%q, %q)=%v, want %v", test.role, test.action, got, test.want)
			}
		})
	}
}
