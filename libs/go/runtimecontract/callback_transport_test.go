package runtimecontract

import "testing"

func TestTrustedCallbackRequiresExplicitPrivateExactEndpoint(t *testing.T) {
	input := validRunnerInputFixture()
	input.CallbackTLS = RuntimeTLSBinding{Profile: CallbackProfileTrustedCluster}
	input.CallbackURL = "http://10.42.0.10:8444"
	if err := input.ValidateCallbackTransport(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://127.0.0.1:8444", "http://8.8.8.8:8444", "http://example.com:8444",
		"https://10.42.0.10:8444", "http://10.42.0.10:80", "http://user@10.42.0.10:8444",
		"http://10.42.0.10:8444/", "http://10.42.0.10:8444?", "http://10.42.0.10:8444?q=x", "%"} {
		candidate := input
		candidate.CallbackURL = endpoint
		if candidate.ValidateCallbackTransport() == nil {
			t.Fatalf("unapproved callback endpoint accepted: %s", endpoint)
		}
	}
	for _, binding := range []RuntimeTLSBinding{{}, {Profile: "insecure"}, {Profile: CallbackProfileTrustedCluster, CAFile: "/var/run/config/ca.pem"}} {
		input.CallbackTLS = binding
		if input.ValidateCallbackTransport() == nil {
			t.Fatal("implicit or mixed transport accepted")
		}
	}
}

func TestCallbackProfileChangesExecutionButNotDomainRevision(t *testing.T) {
	input := validRunnerInputFixture()
	oldExecution, oldMCP, err := RuntimeExecutionBindingDigests(input)
	if err != nil {
		t.Fatal(err)
	}
	source := RuntimeRevisionCredentialSource{SecretName: "fixture", SecretUID: "fixture", SecretResourceVersion: "1"}
	oldRevision, err := RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}
	input.CallbackTLS = RuntimeTLSBinding{Profile: CallbackProfileTrustedCluster}
	input.CallbackURL = "http://10.42.0.10:8444"
	execution, mcp, err := RuntimeExecutionBindingDigests(input)
	if err != nil || execution == oldExecution || mcp == oldMCP {
		t.Fatal("callback profile was not bound to execution")
	}
	revision, err := RuntimeRevisionDigest(input, source)
	if err != nil || revision != oldRevision {
		t.Fatal("transport changed domain revision")
	}
}
