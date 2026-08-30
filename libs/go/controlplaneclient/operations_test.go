package controlplaneclient

import "testing"

func TestAttachmentSetCreateOperationsKeepProjectAndOrganizationScopesSeparate(t *testing.T) {
	operations := ControlAPIGatewayOperations()
	projectRequired := ControlAPIGatewayProjectRequiredOperations()
	projectOperation := "platform.command.attachment-sets.create-draft"
	organizationOperation := "platform.command.organization-attachment-sets.create"

	projectMethod := operations[projectOperation]
	organizationMethod := operations[organizationOperation]
	if projectMethod == "" || organizationMethod == "" || projectMethod == organizationMethod {
		t.Fatalf("attachment set create operations are not independently registered: project=%q organization=%q", projectMethod, organizationMethod)
	}
	if _, ok := projectRequired[projectOperation]; !ok {
		t.Fatal("project attachment set create operation must require project authority")
	}
	if _, ok := projectRequired[organizationOperation]; ok {
		t.Fatal("organization attachment set create operation must not require project authority")
	}
}
