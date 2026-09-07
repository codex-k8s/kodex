package runtimecontract

import (
	"strings"
	"testing"
)

func TestRuntimeProfileKeyUsesSelectionContract(t *testing.T) {
	for _, key := range []string{"builtin-safe-runtime", "profile_abcdefgh", "Ab_-0123", strings.Repeat("A", 128)} {
		input := validRunnerInputFixture()
		input.RuntimeProfileRef = key
		refreshRunnerInputBindings(&input)
		raw, err := EncodeRunnerInput(input)
		if err != nil {
			t.Fatal("valid runtime selection key rejected")
		}
		decoded, err := DecodeRunnerInput(raw)
		if err != nil || decoded.RuntimeProfileRef != key {
			t.Fatal("runtime selection key changed in runner decoding")
		}
	}
	for _, key := range []string{"", "short", strings.Repeat("a", 129), "builtin safe-runtime", "runtime/key", "runtime.key", "runtime:key", "runtime\nkey", "runtime\x00key", "runtime-ключ"} {
		input := validRunnerInputFixture()
		input.RuntimeProfileRef = key
		refreshRunnerInputBindings(&input)
		if input.Validate() == nil {
			t.Fatal("invalid runtime selection key accepted")
		}
	}
	for _, mutate := range []func(*RunnerInput){
		func(v *RunnerInput) { v.AgentRef = "builtin-safe-runtime" },
		func(v *RunnerInput) { v.RoleDefinitionRef = "builtin-safe-runtime" },
		func(v *RunnerInput) { v.ProviderAccountRef = "builtin-safe-runtime" },
		func(v *RunnerInput) { v.RuntimeConfigRef = "builtin-safe-runtime" },
	} {
		input := validRunnerInputFixture()
		mutate(&input)
		refreshRunnerInputBindings(&input)
		if input.Validate() == nil {
			t.Fatal("stable-key admission widened an aggregate reference")
		}
	}
}
