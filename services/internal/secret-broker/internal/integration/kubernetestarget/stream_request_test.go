package kubernetestarget

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// streamTargetFixture identifies a synthetic Pod/container on the pin's exact node incarnation.
// streamTargetFixture задаёт synthetic Pod/container на exact node incarnation из pin.
func streamTargetFixture(pin PrivilegedTransportPin) PodStreamTarget {
	return PodStreamTarget{Namespace: "selected", Name: "selected-pod",
		UID: "7fb0912f-a690-453f-be9a-3520d2e99a85", NodeName: pin.NodeName, NodeUID: pin.NodeUID, Container: "selected-container"}
}

// TestStreamRequestBindsPodUIDAndBothPermissionBoundaries proves exact native kubelet routes,
// safe argv encoding and protocol-specific RBAC checks without executing a command or opening a port.
// TestStreamRequestBindsPodUIDAndBothPermissionBoundaries проверяет exact native kubelet routes,
// safe argv encoding и protocol-specific RBAC checks без command execution либо открытия port.
func TestStreamRequestBindsPodUIDAndBothPermissionBoundaries(t *testing.T) {
	credential, calls := transportFixture(t, false)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	target := streamTargetFixture(pin)
	for _, test := range []struct {
		operation, path string
		protocol        PrivilegedProtocol
		options         PodStreamOptions
		method, verb    string
	}{
		{"kubernetes.exec", "exec", PrivilegedWebSocketV5,
			PodStreamOptions{Command: []string{"printf", "literal $(never-host-shell) & ?", "кириллица"}, Stdout: true}, http.MethodGet, "get"},
		{"kubernetes.exec", "exec", PrivilegedSPDYV4,
			PodStreamOptions{Command: []string{"sh"}, Stdin: true, Stdout: true, TTY: true}, http.MethodPost, "create"},
		{"kubernetes.attach", "attach", PrivilegedWebSocketV5, PodStreamOptions{Stdout: true}, http.MethodGet, "get"},
		{"kubernetes.port-forward", "portForward", PrivilegedPortForwardWebSocketV4, PodStreamOptions{Ports: []uint16{8080}}, http.MethodGet, "get"},
		{"kubernetes.port-forward", "portForward", PrivilegedPortForwardSPDYV1, PodStreamOptions{Ports: []uint16{8080}}, http.MethodPost, "create"},
	} {
		t.Run(test.operation+"/"+string(test.protocol), func(t *testing.T) {
			pin.Protocol = test.protocol
			request, checks, err := credential.StreamRequest(context.Background(), pin, now, test.operation, target, test.options)
			if err != nil || request.Method != test.method || request.URL.Host == "" || request.Header.Get("Authorization") != "" || len(checks) != 3 {
				t.Fatal("typed stream request is unavailable or disclosed private authentication")
			}
			expectedPath := "/" + test.path + "/selected/selected-pod/" + target.UID
			if test.path != "portForward" {
				expectedPath += "/selected-container"
			}
			if request.URL.Path != expectedPath || checks[0].Verb != "get" || checks[0].Resource != "pods" ||
				checks[1].Resource != "pods" || checks[1].Namespace != target.Namespace || checks[1].Name != target.Name || checks[1].Verb != test.verb ||
				checks[2].Resource != "nodes" || checks[2].Subresource != "proxy" || checks[2].Name != target.NodeName || checks[2].Verb != test.verb {
				t.Fatal("stream route omitted PodUID or widened either permission boundary")
			}
			if test.path == "portForward" {
				if request.URL.Query().Get("port") != "8080" || len(request.URL.Query()) != 1 || checks[1].Subresource != "portforward" {
					t.Fatal("native port-forward request has incorrect protocol parameters")
				}
			} else if !reflect.DeepEqual(request.URL.Query()["command"], test.options.Command) {
				t.Fatal("remote argv changed during URL encoding")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("constructing a request performed an unapproved external effect")
	}
}

// TestStreamRequestRejectsChangedPinsAndUnsafeShapes rejects URI injection, node/Pod substitution,
// unknown commands, channel ambiguity and command-size bypasses before any remote action.
// TestStreamRequestRejectsChangedPinsAndUnsafeShapes отклоняет URI injection, node/Pod substitution,
// unknown commands, channel ambiguity и command-size bypass до remote action.
func TestStreamRequestRejectsChangedPinsAndUnsafeShapes(t *testing.T) {
	credential, calls := transportFixture(t, false)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	for _, mutate := range []func(*PodStreamTarget){
		func(target *PodStreamTarget) { target.UID = "" },
		func(target *PodStreamTarget) { target.UID += "/exec" },
		func(target *PodStreamTarget) { target.Namespace = "" },
		func(target *PodStreamTarget) { target.Name = "../foreign" },
		func(target *PodStreamTarget) { target.NodeName = "foreign" },
		func(target *PodStreamTarget) { target.NodeUID = "7fb0912f-a690-453f-be9a-3520d2e99a85" },
		func(target *PodStreamTarget) { target.Container = "" },
		func(target *PodStreamTarget) { target.Container = "selected?command=foreign" },
	} {
		target := streamTargetFixture(pin)
		mutate(&target)
		if _, _, err := credential.StreamRequest(context.Background(), pin, now, "kubernetes.exec", target, PodStreamOptions{Command: []string{"true"}, Stdout: true}); err == nil {
			t.Fatal("foreign or malformed stream target was accepted")
		}
	}
	for _, options := range []PodStreamOptions{
		{Stdout: true},
		{Command: []string{""}, Stdout: true},
		{Command: []string{"true"}},
		{Command: []string{"true"}, TTY: true, Stderr: true},
		{Command: []string{"true"}, Stdout: true, Ports: []uint16{8080}},
		{Command: []string{"true", "embedded\x00nul"}, Stdout: true},
		{Command: []string{strings.Repeat("a", 4097)}, Stdout: true},
		{Command: []string{"true", strings.Repeat("a", 4096), strings.Repeat("b", 4096), strings.Repeat("c", 4096), strings.Repeat("d", 4096)}, Stdout: true},
	} {
		if _, _, err := credential.StreamRequest(context.Background(), pin, now, "kubernetes.exec", streamTargetFixture(pin), options); err == nil {
			t.Fatal("ambiguous or unbounded stream options were accepted")
		}
	}
	for _, operation := range []string{"kubernetes.raw", "kubernetes.cp.upload", "exec", "kubernetes.port-forward"} {
		if _, _, err := credential.StreamRequest(context.Background(), pin, now, operation, streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}); err == nil {
			t.Fatal("unregistered or mismatched stream operation was accepted")
		}
	}
	if _, _, err := credential.StreamRequest(context.Background(), pin, now, "kubernetes.port-forward", streamTargetFixture(pin), PodStreamOptions{Ports: []uint16{8080}}); err == nil {
		t.Fatal("port-forward inherited the remote-command protocol")
	}
	pin.Protocol = PrivilegedPortForwardWebSocketV4
	if _, _, err := credential.StreamRequest(context.Background(), pin, now, "kubernetes.exec", streamTargetFixture(pin), PodStreamOptions{Command: []string{"true"}, Stdout: true}); err == nil {
		t.Fatal("exec inherited the port-forward protocol")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid stream construction reached the target")
	}
}
