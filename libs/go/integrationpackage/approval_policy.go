package integrationpackage

import (
	"encoding/json"
	"errors"
)

// errApprovalPolicy is the safe refusal for a selected policy or invocation that exceeds package authority.
// errApprovalPolicy — безопасный отказ выбранной политике или вызову, выходящим за полномочия пакета.
var errApprovalPolicy = errors.New("integration approval policy is not allowed")

// AllowsApprovalPolicy checks the exact selected policy against the capability's closed allowed set.
// AllowsApprovalPolicy проверяет точную выбранную политику по закрытому списку разрешений возможности.
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

// CollaborativeWrite recognizes only listed GitHub write operations from the exact managed adapter and repository scope.
// CollaborativeWrite признаёт только перечисленные операции записи GitHub с точным управляемым адаптером и областью репозитория.
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

// ValidateInvocationApprovalPolicy rejects unauthorized selections and allows autonomous review only for COMMENT input.
// ValidateInvocationApprovalPolicy отклоняет неразрешённый выбор и допускает автономное ревью лишь с входным событием COMMENT.
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

// validCapabilityApprovalPolicies enforces bounded, nonduplicate choices with stricter read and write rules per adapter.
// validCapabilityApprovalPolicies обеспечивает ограниченный выбор без дублей и отдельные правила чтения и записи по адаптеру.
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

// narrowApprovalPolicies accepts only a nonempty duplicate-free subset of an existing approved policy set.
// narrowApprovalPolicies принимает только непустое множество без дублей внутри ранее утверждённого набора политик.
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
