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

func TestAssistantActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, role string
		ready      bool
		want       []string
	}{
		{name: "ready owner", role: "OWNER", ready: true, want: []string{"OPEN", "CREATE_CONVERSATION", "ADD_TURN", "EDIT"}},
		{name: "recovering owner", role: "OWNER", want: []string{"OPEN", "EDIT", "RECOVER"}},
		{name: "ready member", role: "MEMBER", ready: true, want: []string{"OPEN", "CREATE_CONVERSATION", "ADD_TURN"}},
		{name: "recovering member", role: "MEMBER", want: []string{"OPEN"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := assistantActions(test.role, test.ready); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("assistantActions(%q, %t)=%v, want %v", test.role, test.ready, got, test.want)
			}
		})
	}
}
