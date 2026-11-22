package managedkubernetes

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// TestBindingRequiresExactlyOneOwnerClaim covers malformed provenance locators before owner SQL.
// TestBindingRequiresExactlyOneOwnerClaim проверяет malformed provenance locators до owner SQL.
func TestBindingRequiresExactlyOneOwnerClaim(t *testing.T) {
	base := Binding{WorkloadInstance: "synthetic-worker", InvocationRef: "inv_synthetic", LeaseRef: "lea_synthetic",
		Fence: "synthetic-fence-do-not-print", Generation: 1, InputDigest: strings.Repeat("a", 64)}
	if base.Validate() != nil {
		t.Fatal("complete invocation binding was rejected")
	}
	testBinding := base
	testBinding.InvocationRef, testBinding.ConnectionTestRef = "", "ict_synthetic"
	if testBinding.Validate() != nil {
		t.Fatal("complete health-test binding was rejected")
	}
	for _, mutate := range []func(*Binding){
		func(binding *Binding) { binding.InvocationRef = "" },
		func(binding *Binding) { binding.ConnectionTestRef = "ict_synthetic" },
		func(binding *Binding) { binding.WorkloadInstance = "" },
		func(binding *Binding) { binding.LeaseRef = "lea_synthetic\n" },
		func(binding *Binding) { binding.Fence = "" },
		func(binding *Binding) { binding.Fence = strings.Repeat("x", 257) },
		func(binding *Binding) { binding.Generation = 0 },
		func(binding *Binding) { binding.InputDigest = strings.Repeat("A", 64) },
		func(binding *Binding) { binding.InputDigest = strings.Repeat("a", 62) },
		func(binding *Binding) { binding.InvocationRef = string([]byte{0xff}) },
	} {
		changed := base
		mutate(&changed)
		if changed.Validate() == nil {
			t.Fatal("incomplete or ambiguous owner binding was accepted")
		}
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(format, base), base.Fence) {
			t.Fatal("owner fence escaped diagnostic formatting")
		}
	}
	encoded, err := json.Marshal(base)
	if err != nil || strings.Contains(string(encoded), base.Fence) {
		t.Fatal("owner fence escaped default JSON serialization")
	}
}
