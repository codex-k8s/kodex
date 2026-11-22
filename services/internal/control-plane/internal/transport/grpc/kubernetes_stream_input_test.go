package grpc

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// kubernetesInputPublicViewFixture creates synthetic immutable owner metadata without claiming an active stream, grant or private stage exists.
// kubernetesInputPublicViewFixture создаёт synthetic immutable owner metadata без заявления об actual active stream, grant либо private stage.
func kubernetesInputPublicViewFixture() entity.KubernetesStreamInputView {
	view := entity.KubernetesStreamInputView{Resource: managedkubernetes.ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true, Namespace: "example", Name: "web", UID: "30000000-0000-4000-8000-000000000001", ResourceVersion: "18"}, TargetRef: "target_review", TargetRevision: 2, ConnectionRef: "int_synthetic", InvocationRef: "inv_synthetic", StreamRef: "kst_synthetic", RunRef: "run_synthetic", NodeRef: "nod_synthetic", Operation: "kubernetes.exec", Namespace: "example", PodName: "web", Container: "app", Generation: 7, Version: 3, State: enum.KubernetesStreamActive, InputChannels: []string{"STDIN", "TERMINAL_RESIZE"}, MaximumFrameBytes: 32768, MaximumTotalBytes: 65536, MaximumStagedBytes: 1 << 30, MaximumStagedObjects: 32768, HardDeadline: time.Now().Add(time.Minute)}
	view.Pending = &entity.KubernetesStreamInputSubmission{Ref: "kif_synthetic", InvocationRef: view.InvocationRef, StreamRef: view.StreamRef, Generation: view.Generation, Sequence: 1, Channel: "STDIN", PayloadDigest: strings.Repeat("a", 64), ByteCount: 4, State: enum.KubernetesInputQueued}
	return view
}

// TestKubernetesStreamInputPublicProjectionRejectsMixedMetadata verifies finite typed public output and exact nested origin before a browser consumes it.
// TestKubernetesStreamInputPublicProjectionRejectsMixedMetadata проверяет finite typed public output и exact nested origin до потребления браузером.
func TestKubernetesStreamInputPublicProjectionRejectsMixedMetadata(t *testing.T) {
	view := kubernetesInputPublicViewFixture()
	mapped, err := kubernetesStreamInputViewToProto(view)
	if err != nil || mapped.Version != 3 || mapped.Pending.Sequence != 1 || mapped.HardDeadline == nil || mapped.MaximumStagedBytes != view.MaximumStagedBytes || mapped.MaximumStagedObjects != view.MaximumStagedObjects {
		t.Fatal("valid public metadata lost")
	}
	for _, sequence := range []uint64{4097, 1 << 20, maximumKubernetesInputPublicSequence} {
		candidate := kubernetesInputPublicViewFixture()
		candidate.Pending.Sequence = sequence
		candidate.AcknowledgedSequence = sequence - 1
		if _, err := kubernetesStreamInputViewToProto(candidate); err != nil {
			t.Fatal("representable public sequence rejected", sequence, err)
		}
	}
	for name, mutate := range map[string]func(*entity.KubernetesStreamInputView){
		"stream locator":       func(v *entity.KubernetesStreamInputView) { v.StreamRef = "foreign_stream" },
		"node locator":         func(v *entity.KubernetesStreamInputView) { v.NodeRef = "foreign_node" },
		"version":              func(v *entity.KubernetesStreamInputView) { v.Version = 0 },
		"state":                func(v *entity.KubernetesStreamInputView) { v.State = "FUTURE" },
		"operation":            func(v *entity.KubernetesStreamInputView) { v.Operation = "kubernetes.get" },
		"channels":             func(v *entity.KubernetesStreamInputView) { v.InputChannels = []string{"STDOUT"} },
		"duplicate channels":   func(v *entity.KubernetesStreamInputView) { v.InputChannels = []string{"STDIN", "STDIN"} },
		"closed channel":       func(v *entity.KubernetesStreamInputView) { v.ClosedInputChannels = []string{"FILE_CONTENT"} },
		"staging bytes absent": func(v *entity.KubernetesStreamInputView) { v.MaximumStagedBytes = 0 },
		"staging bytes excessive": func(v *entity.KubernetesStreamInputView) {
			v.MaximumStagedBytes = managedkubernetes.MaximumStagedBytes + 1
		},
		"staging objects absent": func(v *entity.KubernetesStreamInputView) { v.MaximumStagedObjects = 0 },
		"staging objects excessive": func(v *entity.KubernetesStreamInputView) {
			v.MaximumStagedObjects = managedkubernetes.MaximumStagedObjects + 1
		},
		"frame limit":           func(v *entity.KubernetesStreamInputView) { v.MaximumFrameBytes = 32769 },
		"total limit":           func(v *entity.KubernetesStreamInputView) { v.MaximumTotalBytes = 1 },
		"deadline":              func(v *entity.KubernetesStreamInputView) { v.HardDeadline = time.Time{} },
		"reason":                func(v *entity.KubernetesStreamInputView) { v.ReasonCode = "private diagnostic" },
		"pending origin":        func(v *entity.KubernetesStreamInputView) { v.Pending.InvocationRef = "inv_foreign" },
		"pending generation":    func(v *entity.KubernetesStreamInputView) { v.Pending.Generation++ },
		"pending unknown state": func(v *entity.KubernetesStreamInputView) { v.Pending.State = "FUTURE" },
		"pending sequence": func(v *entity.KubernetesStreamInputView) {
			v.Pending.Sequence = maximumKubernetesInputPublicSequence + 1
		},
		"pending empty bytes":     func(v *entity.KubernetesStreamInputView) { v.Pending.ByteCount = 0 },
		"pending unknown channel": func(v *entity.KubernetesStreamInputView) { v.Pending.Channel = "CONTROL" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := kubernetesInputPublicViewFixture()
			mutate(&changed)
			if _, err := kubernetesStreamInputViewToProto(changed); err == nil {
				t.Fatal("malformed owner metadata projected")
			}
		})
	}
	view = kubernetesInputPublicViewFixture()
	view.State = enum.KubernetesStreamExpired
	view.HardDeadline = time.Unix(1, 0)
	historical, err := kubernetesStreamInputViewToProto(view)
	if err != nil || !historical.HardDeadline.AsTime().Equal(view.HardDeadline) {
		t.Fatal("historical expiry renewed or hidden")
	}
	readback := entity.KubernetesStreamInputReadback{View: view, Submission: view.Pending}
	if _, _, err := kubernetesStreamInputReadbackToProto(readback, "inv_foreign", view.StreamRef); err == nil {
		t.Fatal("foreign lookup adopted metadata")
	}
}

// TestKubernetesStreamInputPublicWatchHasNoPodOrInput proves collection discovery and Stop history remain reachable without invented Pod witnesses.
// TestKubernetesStreamInputPublicWatchHasNoPodOrInput доказывает достижимость collection discovery и Stop history без выдуманных Pod witnesses.
func TestKubernetesStreamInputPublicWatchHasNoPodOrInput(t *testing.T) {
	for _, scope := range []string{"namespace", "cluster", "all"} {
		t.Run(scope, func(t *testing.T) {
			view := kubernetesInputPublicViewFixture()
			view.Operation = "kubernetes.watch"
			view.PodName = ""
			view.Container = ""
			view.Pending = nil
			view.InputChannels = nil
			view.ClosedInputChannels = nil
			view.Resource.Name = ""
			view.Resource.UID = ""
			view.Resource.ResourceVersion = ""
			if scope == "cluster" {
				view.Namespace = "@cluster"
				view.Resource.Namespace = ""
				view.Resource.Namespaced = false
			}
			if scope == "all" {
				view.Operation = "kubernetes.watch.all-namespaces"
				view.Namespace = "@all-namespaces"
				view.Resource.Namespace = ""
				view.Resource.AllNamespaces = true
			}
			mapped, err := kubernetesStreamInputViewToProto(view)
			if err != nil || mapped.Resource == nil || mapped.Resource.Name != "" || mapped.TargetRef != view.TargetRef || mapped.ConnectionRef != view.ConnectionRef || len(mapped.InputChannels) != 0 {
				t.Fatal("collection stream hidden or assigned input")
			}
			view.InputChannels = []string{"STDIN"}
			if _, err := kubernetesStreamInputViewToProto(view); err == nil {
				t.Fatal("watch advertised raw input")
			}
		})
	}
}
