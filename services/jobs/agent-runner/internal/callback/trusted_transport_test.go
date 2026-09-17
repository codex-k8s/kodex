package callback

import (
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestTrustedTransportRejectsDestinationDriftBeforeNetwork(t *testing.T) {
	input := model.Input{CallbackURL: "http://10.42.0.10:8444", CallbackTLS: model.TLSBinding{Profile: runtimecontract.CallbackProfileTrustedCluster}}
	transport, err := TransportForInput(input)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	if transport.Proxy != nil || transport.TLSClientConfig != nil {
		t.Fatal("trusted transport retained proxy or TLS dependency")
	}
	for _, destination := range []string{"10.42.0.11:8444", "10.42.0.10:443", "example.com:8444", "127.0.0.1:8444"} {
		if connection, err := transport.DialContext(t.Context(), "tcp", destination); err == nil {
			connection.Close()
			t.Fatal("changed callback destination reached network")
		}
	}
	input.CallbackTLS.Profile = "unknown"
	if _, err := TransportForInput(input); err == nil {
		t.Fatal("unknown callback profile accepted")
	}
}
