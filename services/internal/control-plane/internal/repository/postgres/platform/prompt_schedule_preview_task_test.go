package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
)

// Оракул проверяет decoded PURPOSE, а не случайное совпадение внутри JSON.
func validateSchedulePreviewTask(raw, task string, continued bool, sessionRef string) error {
	var envelope runtimecontract.PromptServiceEnvelope
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF || envelope.Revision != promptservice.ServiceTemplateRevision {
		return fmt.Errorf("invalid preview envelope")
	}
	purpose, count := "", 0
	for _, section := range envelope.Sections {
		if section.Source == "PLATFORM" && section.Slot == "PURPOSE" {
			purpose, count = section.Content, count+1
		}
	}
	if count != 1 || task == "" {
		return fmt.Errorf("exact task purpose is unavailable")
	}
	if !continued {
		// Coordinator дополняет собственную цель полным task отдельным абзацем.
		if purpose != task && !strings.HasSuffix(purpose, "\n\n"+task) {
			return fmt.Errorf("initial purpose lost exact task")
		}
		return nil
	}
	_, encodedPin, present := strings.Cut(purpose, "\n")
	var fields map[string]json.RawMessage
	if !present || json.Unmarshal([]byte(encodedPin), &fields) != nil || len(fields) != 5 {
		return fmt.Errorf("prospective task projection has invalid fields")
	}
	for _, name := range []string{"revision", "mode", "sessionRef", "taskSHA256", "taskBytes"} {
		if fields[name] == nil {
			return fmt.Errorf("prospective task projection lost %s", name)
		}
	}
	var pin continuationTaskProjectionPin
	decoder = json.NewDecoder(strings.NewReader(encodedPin))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pin) != nil || decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("prospective task projection is invalid")
	}
	digest := sha256.Sum256([]byte(task))
	want := continuationTaskProjectionPin{Revision: "continuation-task-projection-v1", Mode: "PROSPECTIVE", SessionRef: sessionRef,
		TaskSHA256: hex.EncodeToString(digest[:]), TaskBytes: len(task)}
	if sessionRef == "" || pin != want {
		return fmt.Errorf("prospective task projection does not match exact task and session")
	}
	return nil
}

func TestSchedulePreviewTaskProjection(t *testing.T) {
	task, session := "Точный task {{literal}}\nбез усечения.", "ses_preview"
	snapshot := workflowPromptFixture()
	snapshot.TargetKind, snapshot.TargetRef, snapshot.SessionRef = promptservice.TargetSessionContinuation, session, session
	snapshot.Variables["task"] = task
	if err := bindProspectiveContinuationTaskProjection(&snapshot); err != nil {
		t.Fatal(err)
	}
	materialized, err := promptservice.Materialize("Preview", promptservice.FromSnapshot(snapshot))
	if err != nil || !materialized.Complete {
		t.Fatal("prospective fixture did not materialize")
	}
	if err := validateSchedulePreviewTask(materialized.Prompt, task, true, session); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"revision", "mode", "sessionRef", "taskSHA256", "taskBytes", "runRef", "nodeRef", "turnRef", "runtimeRevisionRef", "attempt"} {
		t.Run(field, func(t *testing.T) {
			var envelope runtimecontract.PromptServiceEnvelope
			if err := json.Unmarshal([]byte(materialized.Prompt), &envelope); err != nil {
				t.Fatal(err)
			}
			for index := range envelope.Sections {
				section := &envelope.Sections[index]
				if section.Source != "PLATFORM" || section.Slot != "PURPOSE" {
					continue
				}
				notice, raw, _ := strings.Cut(section.Content, "\n")
				var pin map[string]any
				if err := json.Unmarshal([]byte(raw), &pin); err != nil {
					t.Fatal(err)
				}
				pin[field] = "wrong"
				if field == "taskBytes" || field == "attempt" {
					pin[field] = 1
				}
				encoded, err := json.Marshal(pin)
				if err != nil {
					t.Fatal(err)
				}
				section.Content = notice + "\n" + string(encoded)
			}
			raw, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if validateSchedulePreviewTask(string(raw), task, true, session) == nil {
				t.Fatal("incorrect prospective pin accepted")
			}
		})
	}
	initial := snapshot
	initial.TargetKind, initial.TargetRef, initial.SemanticValues = promptservice.TargetAgent, "agt_preview", nil
	materialized, err = promptservice.Materialize("Preview", promptservice.FromSnapshot(initial))
	if err != nil || !materialized.Complete || validateSchedulePreviewTask(materialized.Prompt, task, false, "") != nil {
		t.Fatal("initial preview lost exact decoded task")
	}
	if validateSchedulePreviewTask(materialized.Prompt, task+" changed", false, "") == nil {
		t.Fatal("initial preview accepted a different task")
	}
}
