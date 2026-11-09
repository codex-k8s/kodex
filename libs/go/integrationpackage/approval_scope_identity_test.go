package integrationpackage

import "testing"

// approvalMRFixture supplies synthetic repository/MR identity and mutable comment text under one explicit reusable-consent contract.
// approvalMRFixture задаёт синтетические repository/MR identity и изменяемый текст комментария в явном контракте переиспользуемого согласования.
func approvalMRFixture() Capability {
	return Capability{InputFields: []Field{
		{Key: "project_path", Type: "STRING", Format: "IDENTIFIER", Required: true, MaximumLength: 255},
		{Key: "merge_request_iid", Type: "INTEGER", Required: true, Minimum: 1, Maximum: 2147483647},
		{Key: "body", Type: "STRING", Format: "PLAIN", Required: true, MaximumLength: 4096},
	}, ApprovalScope: &ApprovalScopeContract{Name: "Merge request", RequiredInputPaths: []string{"/project_path", "/merge_request_iid"}}}
}

// TestApprovalScopeContractCannotDropObjectIdentity prevents choosing comment text or another subset in place of the exact repository/MR boundary.
// TestApprovalScopeContractCannotDropObjectIdentity запрещает выбирать текст комментария или другой subset вместо точной repository/MR boundary.
func TestApprovalScopeContractCannotDropObjectIdentity(t *testing.T) {
	capability := approvalMRFixture()
	if err := capability.validateApprovalScopeContract(); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{nil, {}, {"/body"}, {"/merge_request_iid"}, {"/project_path"}} {
		if err := capability.ValidateApprovalScopePaths(paths); err == nil {
			t.Fatalf("identity omitted: %v", paths)
		}
	}
	for _, paths := range [][]string{{"/project_path", "/merge_request_iid"}, {"/body", "/merge_request_iid", "/project_path"}} {
		if err := capability.ValidateApprovalScopePaths(paths); err != nil {
			t.Fatal(err)
		}
	}
	for _, contract := range []*ApprovalScopeContract{
		{Name: "", RequiredInputPaths: []string{}}, {Name: "Missing list"},
		{Name: "Unknown field", RequiredInputPaths: []string{"/unknown"}},
		{Name: "Duplicate", RequiredInputPaths: []string{"/project_path", "/project_path"}},
	} {
		capability.ApprovalScope = contract
		if capability.validateApprovalScopeContract() == nil {
			t.Fatalf("invalid contract: %+v", contract)
		}
	}
}

// TestApprovalScopeContractReusesOnlyTheSameObject allows new text for one MR while changing either repository or MR changes the consent digest.
// TestApprovalScopeContractReusesOnlyTheSameObject допускает новый текст для одного MR, но другой repository или MR меняет consent digest.
func TestApprovalScopeContractReusesOnlyTheSameObject(t *testing.T) {
	capability := approvalMRFixture()
	paths := []string{"/project_path", "/merge_request_iid"}
	inputs := []string{
		`{"project_path":"team/alpha","merge_request_iid":7,"body":"First"}`,
		`{"project_path":"team/alpha","merge_request_iid":7,"body":"Second"}`,
		`{"project_path":"team/bravo","merge_request_iid":7,"body":"First"}`,
		`{"project_path":"team/alpha","merge_request_iid":8,"body":"First"}`,
	}
	scopes := make([]ApprovalScope, len(inputs))
	for i, input := range inputs {
		value, err := capability.ResolveApprovalScope(paths, []byte(input))
		if err != nil {
			t.Fatal(err)
		}
		scopes[i] = value
	}
	if scopes[0].Digest != scopes[1].Digest {
		t.Fatal("comment text unnecessarily narrowed MR consent")
	}
	if scopes[0].Digest == scopes[2].Digest || scopes[0].Digest == scopes[3].Digest {
		t.Fatal("another resource reused MR consent")
	}
}

// TestApprovalScopeContractExplicitParentAndLegacyEmptyPaths distinguishes declared connection-resource consent from absent legacy metadata.
// TestApprovalScopeContractExplicitParentAndLegacyEmptyPaths различает явное согласование ресурса подключения и отсутствующие legacy metadata.
func TestApprovalScopeContractExplicitParentAndLegacyEmptyPaths(t *testing.T) {
	capability := Capability{InputFields: []Field{}, ApprovalScope: &ApprovalScopeContract{Name: "Repository", RequiredInputPaths: []string{}}}
	if err := capability.validateApprovalScopeContract(); err != nil {
		t.Fatal(err)
	}
	if _, err := capability.ResolveApprovalScope(nil, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	capability.ApprovalScope = nil
	if capability.ValidateApprovalScopePaths(nil) == nil {
		t.Fatal("legacy empty scope was widened")
	}
}
