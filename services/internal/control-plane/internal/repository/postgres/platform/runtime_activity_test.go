package platform

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func TestRuntimeActivityPublishedMessageClosedProjection(t *testing.T) {
	valid := entity.RunMessage{Ref: "msg_fixture01", Phase: "COMMENTARY", Revision: 1, Text: "Проверяю доступные инструменты."}
	if !validPublishedMessage(&valid) {
		t.Fatal("valid published commentary rejected")
	}
	for name, change := range map[string]func(*entity.RunMessage){
		"hidden":        func(v *entity.RunMessage) { v.Phase = "ANALYSIS" },
		"user":          func(v *entity.RunMessage) { v.Phase = "USER" },
		"unknown phase": func(v *entity.RunMessage) { v.Phase = "" },
		"revision":      func(v *entity.RunMessage) { v.Revision = 2 },
		"reference":     func(v *entity.RunMessage) { v.Ref = "../message" },
		"empty":         func(v *entity.RunMessage) { v.Text = " " },
		"budget":        func(v *entity.RunMessage) { v.Text = strings.Repeat("я", 32769) },
		"utf8":          func(v *entity.RunMessage) { v.Text = string([]byte{0xff}) },
	} {
		candidate := valid
		change(&candidate)
		if validPublishedMessage(&candidate) {
			t.Fatalf("invalid %s accepted", name)
		}
	}
}

func TestRuntimeActivityToolLifecycleClosedProjection(t *testing.T) {
	for _, state := range []string{"SUCCEEDED", "FAILED", "CANCELLED"} {
		if !validToolActivityLifecycle(state, 2, "safe result", 1) {
			t.Fatalf("terminal %s rejected", state)
		}
	}
	if !validToolActivityLifecycle("RUNNING", 1, "", 0) ||
		validToolActivityLifecycle("RUNNING", 2, "", 0) ||
		validToolActivityLifecycle("RUNNING", 1, "raw output", 0) ||
		validToolActivityLifecycle("RUNNING", 1, "", 10) ||
		validToolActivityLifecycle("UNKNOWN", 1, "", 0) ||
		validToolActivityLifecycle("SUCCEEDED", 1, "", 0) ||
		validToolActivityLifecycle("SUCCEEDED", 0, "", 0) {
		t.Fatal("unsafe lifecycle projection accepted")
	}
}
