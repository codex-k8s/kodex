package service

import (
	"os"
	"strings"
	"testing"
)

func TestAccessEventConstantsAreDeclaredInAsyncAPI(t *testing.T) {
	t.Parallel()

	spec, err := os.ReadFile("../../../../../../specs/asyncapi/access-manager.v1.yaml")
	if err != nil {
		t.Fatalf("read access-manager AsyncAPI spec: %v", err)
	}
	content := string(spec)
	for _, eventType := range implementedAccessEventTypes() {
		if !strings.Contains(content, "const: "+eventType) {
			t.Fatalf("event type %q is not declared in AsyncAPI contract", eventType)
		}
	}
}

func implementedAccessEventTypes() []string {
	return []string{
		accessEventOrganizationCreated,
		accessEventGroupCreated,
		accessEventMembershipCreated,
		accessEventMembershipUpdated,
		accessEventMembershipDisabled,
		accessEventAllowlistEntryCreated,
		accessEventAllowlistEntryUpdated,
		accessEventAllowlistEntryDisabled,
		accessEventUserCreated,
		accessEventUserStatusChanged,
		accessEventUserIdentityLinked,
		accessEventExternalProviderCreated,
		accessEventExternalProviderUpdated,
		accessEventExternalProviderDisabled,
		accessEventExternalAccountCreated,
		accessEventExternalAccountStatusChanged,
		accessEventExternalAccountBindingCreated,
		accessEventExternalAccountBindingUpdated,
		accessEventExternalAccountBindingDisabled,
		accessEventAccessActionCreated,
		accessEventAccessActionUpdated,
		accessEventAccessRuleCreated,
		accessEventAccessRuleUpdated,
		accessEventAccessDecisionRecorded,
	}
}
