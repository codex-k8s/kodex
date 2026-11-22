package managedkubernetes

import (
	"strings"
	"testing"
)

// TestChildProposalDigest binds placement, namespace witnesses and set cardinality without order authority.
// TestChildProposalDigest связывает placement, namespace witnesses и set cardinality без order authority.
func TestChildProposalDigest(t *testing.T) {
	original := ChildCandidate{ObservationState: ObservationPresent, Resource: ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "apps", Name: "worker", UID: "20000000-0000-4000-8000-000000000001", ResourceVersion: "7"}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64), PodWitness: &PodNodeWitness{NodeName: "node-one", NodeUID: "20000000-0000-4000-8000-000000000002", Container: "worker"}}
	snapshot := strings.Repeat("c", 64)
	first, err := ChildProposalDigest(snapshot, []ChildCandidate{original})
	if err != nil {
		t.Fatal("valid observed Pod rejected")
	}
	changed := original
	copyWitness := *original.PodWitness
	copyWitness.Container = "other"
	changed.PodWitness = &copyWitness
	second, err := ChildProposalDigest(snapshot, []ChildCandidate{changed})
	if err != nil || first == second {
		t.Fatal("container witness missing from digest")
	}
	changed = original
	changed.NamespaceUID = "20000000-0000-4000-8000-000000000003"
	second, err = ChildProposalDigest(snapshot, []ChildCandidate{changed})
	if err != nil || first == second {
		t.Fatal("namespace witness missing from digest")
	}
	other := original
	other.Resource.Name = "another"
	ordered, err := ChildProposalDigest(snapshot, []ChildCandidate{original, other})
	if err != nil {
		t.Fatal("valid finite set rejected")
	}
	reverse, err := ChildProposalDigest(snapshot, []ChildCandidate{other, original})
	if err != nil || ordered != reverse || original.Resource.Name != "worker" {
		t.Fatal("input ordering changed set authority")
	}
	if _, err := ChildProposalDigest(snapshot, []ChildCandidate{original, original}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	for _, mutate := range []func(*ChildCandidate){
		func(c *ChildCandidate) { c.ObservationState = ObservationCollection },
		func(c *ChildCandidate) { c.Resource.Resource = "configmaps" },
		func(c *ChildCandidate) { c.Resource.ResourceVersion = "" },
		func(c *ChildCandidate) { copy := *c.PodWitness; copy.NodeUID = "not-a-uid"; c.PodWitness = &copy },
		func(c *ChildCandidate) {
			copy := *c.PodWitness
			copy.NodeName = "https://caller.invalid"
			c.PodWitness = &copy
		},
	} {
		bad := original
		mutate(&bad)
		if _, err := ChildProposalDigest(snapshot, []ChildCandidate{bad}); err == nil {
			t.Fatal("invalid placement observation accepted")
		}
	}
}

// TestChildProposalUnboundNode separates discovery proposals from final owner-bound authority.
// Missing NodeUID is proposal-only; enrichment must leave the original proposal bytes intact.
// TestChildProposalUnboundNode разделяет discovery proposals и окончательную owner-bound authority.
// Отсутствующий NodeUID допустим только в proposal; обогащение не меняет исходные proposal bytes.
func TestChildProposalUnboundNode(t *testing.T) {
	proposal := ChildCandidate{ObservationState: ObservationPresent, Resource: ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "apps", Name: "worker", UID: "20000000-0000-4000-8000-000000000001", ResourceVersion: "7"}, ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64), PodWitness: &PodNodeWitness{NodeName: "node-one", Container: "worker"}}
	snapshot := strings.Repeat("c", 64)
	originalDigest, err := ChildProposalDigest(snapshot, []ChildCandidate{proposal})
	if err != nil || proposal.ValidateProposal() != nil || proposal.Validate() == nil {
		t.Fatal("unbound proposal was rejected or accepted as final authority")
	}
	bound := proposal
	witness := *proposal.PodWitness
	witness.NodeUID = "20000000-0000-4000-8000-000000000002"
	bound.PodWitness = &witness
	boundDigest, err := ChildProposalDigest(snapshot, []ChildCandidate{bound})
	if err != nil || bound.Validate() != nil || boundDigest == originalDigest || proposal.PodWitness.NodeUID != "" {
		t.Fatal("bound witness did not produce distinct final identity")
	}
	readbackDigest, err := ChildProposalDigest(snapshot, []ChildCandidate{proposal})
	if err != nil || readbackDigest != originalDigest {
		t.Fatal("original proposal changed during enrichment")
	}
	for _, mutate := range []func(*ChildCandidate){
		func(c *ChildCandidate) { c.PodWitness.NodeName = "" },
		func(c *ChildCandidate) { c.PodWitness.Container = "" },
		func(c *ChildCandidate) { c.PodWitness.NodeUID = "not-a-uid" },
		func(c *ChildCandidate) { c.PodWitness.NodeUID = "00000000-0000-0000-0000-000000000000" },
		func(c *ChildCandidate) { c.Resource.Resource = "configmaps" },
		func(c *ChildCandidate) { c.Resource.ResourceVersion = "" },
		func(c *ChildCandidate) { c.ObservationState = ObservationCollection },
	} {
		bad := proposal
		copyWitness := *proposal.PodWitness
		bad.PodWitness = &copyWitness
		mutate(&bad)
		if bad.ValidateProposal() == nil {
			t.Fatal("invalid unbound proposal accepted")
		}
	}
}
