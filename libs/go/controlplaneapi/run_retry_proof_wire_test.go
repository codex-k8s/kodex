package controlplaneapi

import (
	"bytes"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestRunEdgeWorkflowRetryWire preserves every old edge field and exact legacy bytes while adding
// one presence-bearing historical read proof; no execution or grant data enters its closed shape.
// TestRunEdgeWorkflowRetryWire сохраняет все прежние поля и точные legacy bytes ребра, добавляя
// один optional historical read proof без execution или grant data в его закрытой форме.
func TestRunEdgeWorkflowRetryWire(t *testing.T) {
	legacy := &cp.RunEdge{Ref: "edg_legacy01", RunRef: "run_legacy01", SourceNodeRef: "nod_source01", TargetNodeRef: "nod_target01", Type: cp.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF, Label: "retained"}
	want := []byte{}
	for index, value := range []string{legacy.Ref, legacy.RunRef, legacy.SourceNodeRef, legacy.TargetNodeRef} {
		want = protowire.AppendTag(want, protowire.Number(index+1), protowire.BytesType)
		want = protowire.AppendString(want, value)
	}
	want = protowire.AppendTag(want, 5, protowire.VarintType)
	want = protowire.AppendVarint(want, uint64(cp.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF))
	want = protowire.AppendTag(want, 6, protowire.BytesType)
	want = protowire.AppendString(want, legacy.Label)
	encoded, err := proto.Marshal(legacy)
	if err != nil || !bytes.Equal(encoded, want) {
		t.Fatal("legacy run edge wire changed")
	}
	fields := legacy.ProtoReflect().Descriptor().Fields()
	field := fields.ByNumber(7)
	if fields.Len() != 7 || field.JSONName() != "serverWorkflowRetry" || field.Kind() != protoreflect.MessageKind || !field.HasPresence() || legacy.ProtoReflect().Has(field) {
		t.Fatal("retry proof lost optional message presence or reused old wire")
	}
	oldJSON, err := protojson.Marshal(legacy)
	if err != nil || strings.Contains(string(oldJSON), "serverWorkflowRetry") {
		t.Fatal("absent retry proof changed legacy JSON")
	}
	legacy.ServerWorkflowRetry = &cp.WorkflowRetryReadProof{WorkflowVersionRef: "wfv_fixture1", WorkflowVersionDigest: strings.Repeat("a", 64), OriginNodeRef: "nod_origin01", StepKey: "work", PreviousBusinessAttempt: 1, BusinessAttempt: 2}
	raw, err := proto.Marshal(legacy)
	var decoded cp.RunEdge
	if err != nil || proto.Unmarshal(raw, &decoded) != nil || !proto.Equal(legacy, &decoded) || !decoded.ProtoReflect().Has(field) {
		t.Fatal("typed retry proof did not survive protobuf wire")
	}
	proofFields := legacy.ServerWorkflowRetry.ProtoReflect().Descriptor().Fields()
	if proofFields.Len() != 6 {
		t.Fatal("historical read proof expanded beyond six public fields")
	}
	jsonBytes, err := protojson.Marshal(legacy)
	var fromJSON cp.RunEdge
	if err != nil || protojson.Unmarshal(jsonBytes, &fromJSON) != nil || !proto.Equal(legacy, &fromJSON) {
		t.Fatal("typed retry proof lost JSON numbers or presence")
	}
}
