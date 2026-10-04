package integrationpackage

import (
	"encoding/json"
	"errors"
)

var errApprovalPolicy = errors.New("integration approval policy is not allowed")

// AllowsApprovalPolicy проверяет выбранную policy, не подменяя её package default.
func (capability Capability) AllowsApprovalPolicy(selected string) bool {
	if !validApprovalPolicy(selected) {
		return false
	}
	for _, allowed := range capability.AllowedApprovalPolicies {
		if allowed == selected {
			return true
		}
	}
	return false
}

// CollaborativeWrite имеет закрытый реестр; чужой package не наследует GitHub authority.
func (definition Package) CollaborativeWrite(capability Capability) bool {
	if definition.Metadata.Key != "github" || definition.Spec.Adapter != string(AdapterGitHub) ||
		definition.Spec.AdapterOwner != string(OwnerIntegrationGateway) || definition.Spec.ExecutionRoute != string(RouteManagedMCP) ||
		capability.ResourceScope.Kind != "GITHUB_REPOSITORY" || capability.Risk != string(RiskWrite) ||
		capability.Key != capability.Operation {
		return false
	}
	switch capability.Operation {
	case "github.issue.comment.create", "github.issue.comment.update", "github.pull_request.create",
		"github.pull_request.update", "github.pull_request.review.create":
		return true
	default:
		return false
	}
}

// ValidateInvocationApprovalPolicy сохраняет ограничения payload до чтения credential и effect.
func (definition Package) ValidateInvocationApprovalPolicy(capability Capability, selected string, canonicalInput []byte) error {
	if !capability.AllowsApprovalPolicy(selected) {
		return errApprovalPolicy
	}
	if selected == string(ApprovalNone) && capability.Operation == "github.pull_request.review.create" {
		var input struct {
			Event string `json:"event"`
		}
		if !definition.CollaborativeWrite(capability) || json.Unmarshal(canonicalInput, &input) != nil || input.Event != "COMMENT" {
			return errApprovalPolicy
		}
	}
	return nil
}

func validCapabilityApprovalPolicies(definition *Package, capability Capability) bool {
	if !validApprovalPolicy(capability.ApprovalPolicy) || len(capability.AllowedApprovalPolicies) < 1 ||
		len(capability.AllowedApprovalPolicies) > 3 || !capability.AllowsApprovalPolicy(capability.ApprovalPolicy) {
		return false
	}
	seen := map[string]bool{}
	for _, policy := range capability.AllowedApprovalPolicies {
		if !validApprovalPolicy(policy) || seen[policy] {
			return false
		}
		seen[policy] = true
		if capability.Risk == string(RiskRead) && policy != string(ApprovalNone) {
			return false
		}
		if capability.Risk != string(RiskRead) && policy == string(ApprovalNone) &&
			!emailMailboxApproval(definition, capability) && !definition.CollaborativeWrite(capability) {
			return false
		}
		if policy == string(ApprovalHumanScoped) && definition.Spec.Adapter != string(AdapterOpenAPIMCP) &&
			!definition.CollaborativeWrite(capability) {
			return false
		}
	}
	if definition.Spec.Adapter == string(AdapterContext7) {
		return capability.Risk == string(RiskRead) && capability.ApprovalPolicy == string(ApprovalNone) &&
			(capability.Operation == "context7.library.resolve" || capability.Operation == "context7.docs.query")
	}
	return true
}

func narrowApprovalPolicies(candidate, baseline []string) bool {
	allowed := map[string]bool{}
	for _, policy := range baseline {
		allowed[policy] = true
	}
	seen := map[string]bool{}
	for _, policy := range candidate {
		if !allowed[policy] || seen[policy] {
			return false
		}
		seen[policy] = true
	}
	return len(candidate) > 0
}
