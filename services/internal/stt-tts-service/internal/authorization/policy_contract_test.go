package authorization

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// Проверяется настоящий consumer относительно сгенерированной deploy policy,
// а не две независимые фикстуры с одинаковой ошибкой в имени permission.
func TestGeneratedPolicyReachesTranscriptionPrincipal(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "deploy", "k8s", "base", "internal-rpc-authority-publisher", "authority-policy.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Policy struct {
			Bindings []struct {
				OperationID     string `json:"operation_id"`
				Permission      string `json:"permission"`
				FullMethod      string `json:"full_method"`
				Caller          string `json:"caller_workload_id"`
				Target          string `json:"target_workload_id"`
				Audience        string `json:"audience"`
				ProjectRequired bool   `json:"project_required"`
				RequestProfile  struct {
					Mode string `json:"mode"`
				} `json:"request_profile"`
			} `json:"operation_bindings"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, binding := range document.Policy.Bindings {
		if binding.OperationID != transcribeOperation {
			continue
		}
		found++
		if binding.ProjectRequired || binding.FullMethod != sttv1.SpeechToTextService_Transcribe_FullMethodName {
			t.Fatal("STT policy scope or method changed")
		}
		verified := validVerifiedAuthorizationContext()
		verified.OperationId, verified.Permission = binding.OperationID, binding.Permission
		verified.FullMethod, verified.RequestBindingMode = binding.FullMethod, binding.RequestProfile.Mode
		verified.CallerWorkloadId, verified.TargetWorkloadId = binding.Caller, binding.Target
		verified.Audience, verified.Authority.Project = binding.Audience, nil
		principal, err := Principal(verifiedPrincipalContext(t, verified), binding.FullMethod)
		if err != nil || principal.Permission != value.PermissionTranscribe || principal.ProjectID != "" {
			t.Fatal("generated STT policy is rejected by the transcription consumer")
		}
	}
	if found != 1 {
		t.Fatal("STT policy binding is missing or duplicated")
	}
}
