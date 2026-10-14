package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"testing"
)

func TestWorkflowCatalogReadRequestWire(t *testing.T) {
	request := &cp.GetExecutionWorkflowCatalogRequest{Read: &cp.GetExecutionWorkflowCatalogRequest_PublicationRead{PublicationRead: &cp.ExecutionWorkflowPublicationRead{Pins: &cp.ExecutionWorkflowReadPins{WorkflowRef: "wfl_workflow01"}}}}
	if !workflowCatalogRequestWireValid(request) {
		t.Fatal("typed selector rejected")
	}
	request.GetPublicationRead().Pins.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	if workflowCatalogRequestWireValid(request) {
		t.Fatal("unknown nested field accepted")
	}
	request.GetPublicationRead().Pins = nil
	if workflowCatalogRequestWireValid(request) {
		t.Fatal("missing pins accepted")
	}
	if workflowCatalogRequestWireValid(nil) {
		t.Fatal("nil request accepted")
	}
}
