package runtimecontract

import (
	"bytes"
	"strings"
	"testing"
)

// TestWorkspaceUnboundStopIsNotAWriterACK checks closed canonical stopped metadata, exact scope,
// no fence disclosure and refusal to reinterpret it as an admitted writer or native checkpoint.
// TestWorkspaceUnboundStopIsNotAWriterACK проверяет closed canonical stopped metadata, exact scope,
// отсутствие fence и отказ интерпретации как admitted writer либо native checkpoint.
func TestWorkspaceUnboundStopIsNotAWriterACK(t *testing.T) {
	joined, input, binding := workspaceQuiescenceFixture()
	input.WorkspaceStorage = &binding
	stop := WorkspaceUnboundStop{Schema: WorkspaceUnboundStopSchema, Scope: binding.Scope, WorkspaceRef: binding.WorkspaceRef, Generation: binding.Generation, NodeName: "node.synthetic", NodeUID: "synthetic-node-uid", Evidence: "FIRST_INIT_FAILED", Execution: joined.Writer}
	raw, err := EncodeWorkspaceUnboundStop(stop)
	if err != nil || !stop.Matches(input) || bytes.Contains(raw, []byte(input.LeaseFence)) {
		t.Fatal("exact bounded metadata rejected or fence disclosed")
	}
	if got, err := DecodeWorkspaceUnboundStop(raw); err != nil || got != stop {
		t.Fatal("canonical record did not round trip")
	}
	if _, err := DecodeWorkspaceWriterAcknowledgement(raw); err == nil {
		t.Fatal("unbound stop became a writer ACK")
	}
	if _, err := DecodeWorkspaceQuiescence(raw); err == nil {
		t.Fatal("unbound stop became positive writer join")
	}
	for _, bad := range [][]byte{append(raw, ' '), bytes.Replace(raw, []byte(`"schema":`), []byte(`"extra":true,"schema":`), 1), bytes.Replace(raw, []byte(`"evidence":"FIRST_INIT_FAILED"`), []byte(`"evidence":"UNKNOWN"`), 1)} {
		if _, err := DecodeWorkspaceUnboundStop(bad); err == nil {
			t.Fatal("noncanonical or unknown record accepted")
		}
	}
	for _, change := range []func(*WorkspaceUnboundStop){func(s *WorkspaceUnboundStop) { s.Generation++ }, func(s *WorkspaceUnboundStop) { s.Scope.ProjectRef = "prj_foreign000" }, func(s *WorkspaceUnboundStop) { s.Execution.RuntimeRevisionDigest = strings.Repeat("0", 64) }, func(s *WorkspaceUnboundStop) { s.Execution.JoinSHA256 = strings.Repeat("a", 64) }} {
		bad := stop
		change(&bad)
		if bad.Matches(input) {
			t.Fatal("stale scope or fabricated join accepted")
		}
	}
}
