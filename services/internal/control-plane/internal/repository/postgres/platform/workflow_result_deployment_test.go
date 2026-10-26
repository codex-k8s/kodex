package platform

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// workflowDeploymentFixture creates public metadata and immutable native receipt bytes without network or secret values.
// workflowDeploymentFixture создаёт public metadata и immutable native receipt bytes без сети и значений секретов.
func workflowDeploymentFixture(t *testing.T) (entity.WorkflowResultRequirement, entity.WorkflowDeclaredResult, workflowIntegrationResult, []workflowResultDependency, sshcompose.Target) {
	t.Helper()
	key := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
	key = append(key, 0, 0, 0, 32)
	key = append(key, bytes.Repeat([]byte{1}, 32)...)
	configuration := map[string]string{"project_ref": "prj_fixture1838", "owner": "acme", "repository": "repo", "address": "8.8.8.8", "user": "kdx_fixture", "host_key": "ssh-ed25519 " + base64.StdEncoding.EncodeToString(key), "published_port": "8080", "runtime_image": "docker.io/library/python@sha256:" + strings.Repeat("a", 64), "ssh_secret_ref": "sec_fixture1838"}
	target, err := sshcompose.TargetFromConfiguration(configuration)
	if err != nil {
		t.Fatal(err)
	}
	scope := map[string]string{}
	for k, v := range configuration {
		if k != "ssh_secret_ref" {
			scope[k] = v
		}
	}
	scope["target_ref"] = target.Ref
	requirement := entity.WorkflowResultRequirement{Key: "deploy", Kind: "DEPLOYMENT", Operation: "ssh.compose.deploy", Repository: target.Repository, SubjectStepKey: "merge", TargetRef: target.Ref}
	declaration := entity.WorkflowDeclaredResult{Key: "deploy", ReceiptRef: "erc_fixture1838"}
	receipt := sshcompose.Receipt{Version: 1, TargetRef: target.Ref, ProjectRef: target.ProjectRef, DeploymentRef: "inv_fixture1838", EffectKey: "eff_fixture1838", InputDigest: strings.Repeat("c", 64), MergedSHA: strings.Repeat("d", 40), TreeSHA: strings.Repeat("e", 40), BundleSHA256: strings.Repeat("f", 64), Image: target.RuntimeImage, Generation: 1, State: "SUCCEEDED", HealthOK: true, StartedAt: 1, FinishedAt: 2, ExpiresAt: 3, Deadline: 4}
	output := map[string]any{"receipt": string(asJSON(receipt)), "target_ref": target.Ref, "deployed_sha": receipt.MergedSHA, "bundle_sha256": receipt.BundleSHA256, "image_digest": target.RuntimeImage[len(target.RuntimeImage)-71:], "health_ok": true}
	summary := string(asJSON(output))
	proof := workflowIntegrationResult{Operation: requirement.Operation, Scope: asJSON(scope), Input: []byte(`{"inspect_invocation_ref":"inv_inspect1838"}`), Summary: summary, Digest: workflowResultSHA([]byte(summary)), ProviderRef: "ssh-compose:" + target.Ref + ":" + receipt.DeploymentRef}
	inputs := []workflowResultDependency{{StepKey: "merge", SourceNodeID: "owner-merge", Receipt: entity.WorkflowAttemptResult{Attempt: 1, Accepted: true, Results: []entity.WorkflowAcceptedResult{{Kind: "INTEGRATION_RECEIPT", Repository: target.Repository, Number: 7, HeadSHA: receipt.MergedSHA, ReceiptRef: "erc_merge1838"}}}}}
	return requirement, declaration, proof, inputs, target
}

// TestWorkflowDeploymentResultPredicates rejects altered health, scope, subject and receipt bytes through the actual evaluator helper.
// TestWorkflowDeploymentResultPredicates отклоняет изменённые health/scope/subject/receipt bytes настоящим evaluator helper.
func TestWorkflowDeploymentResultPredicates(t *testing.T) {
	for _, fault := range []string{"valid", "digest", "target", "subject", "subject-kind", "operation", "provider", "health", "receipt-health", "state", "sha", "tree", "bundle", "image", "project", "generation", "duplicate", "trailing", "opaque"} {
		t.Run(fault, func(t *testing.T) {
			requirement, declaration, proof, inputs, _ := workflowDeploymentFixture(t)
			var output map[string]any
			if err := json.Unmarshal([]byte(proof.Summary), &output); err != nil {
				t.Fatal(err)
			}
			var receipt sshcompose.Receipt
			if err := json.Unmarshal([]byte(output["receipt"].(string)), &receipt); err != nil {
				t.Fatal(err)
			}
			switch fault {
			case "target":
				requirement.TargetRef = "ssh_foreign1838"
			case "subject":
				inputs[0].Receipt.Results[0].HeadSHA = strings.Repeat("0", 40)
			case "subject-kind":
				inputs[0].Receipt.Results[0].Kind = "PULL_REQUEST"
			case "operation":
				proof.Operation = "ssh.compose.status"
			case "provider":
				proof.ProviderRef = "ssh-compose:foreign:inv_fixture1838"
			case "health":
				output["health_ok"] = false
			case "receipt-health":
				receipt.HealthOK = false
			case "state":
				receipt.State = "UNKNOWN_OUTCOME"
			case "sha":
				output["deployed_sha"] = strings.Repeat("0", 40)
			case "tree":
				receipt.TreeSHA = "main"
			case "bundle":
				output["bundle_sha256"] = strings.Repeat("0", 64)
			case "image":
				output["image_digest"] = "sha256:" + strings.Repeat("0", 64)
			case "project":
				receipt.ProjectRef = "prj_foreign1838"
			case "generation":
				receipt.Generation = 0
			}
			output["receipt"] = string(asJSON(receipt))
			proof.Summary = string(asJSON(output))
			switch fault {
			case "duplicate":
				proof.Summary = strings.Replace(proof.Summary, `"health_ok":true`, `"health_ok":false,"health_ok":true`, 1)
			case "trailing":
				proof.Summary += " {}"
			case "opaque":
				proof.Summary = `{"url":"https://example.invalid","text":"deployed successfully"}`
			}
			proof.Digest = workflowResultSHA([]byte(proof.Summary))
			if fault == "digest" {
				proof.Digest = strings.Repeat("0", 64)
			}
			result, reason := acceptWorkflowDeploymentResult(requirement, declaration, proof, inputs)
			if fault == "valid" {
				if reason != "" || !result.HealthOK || result.ReceiptRef != declaration.ReceiptRef || result.HeadSHA != inputs[0].Receipt.Results[0].HeadSHA {
					t.Fatal("exact deployment proof lost")
				}
			} else if reason == "" || result.ReceiptRef != "" {
				t.Fatal("unverified deployment accepted")
			}
		})
	}
}

// TestSSHComposeGatePreviewUsesOnlyOwnerMetadata checks bounded display without exposing SSH secret descriptors or upgrading completeness.
// TestSSHComposeGatePreviewUsesOnlyOwnerMetadata проверяет bounded display без SSH secret descriptors и повышения completeness.
func TestSSHComposeGatePreviewUsesOnlyOwnerMetadata(t *testing.T) {
	_, _, proof, _, target := workflowDeploymentFixture(t)
	var values map[string]string
	if err := json.Unmarshal(proof.Scope, &values); err != nil {
		t.Fatal(err)
	}
	for _, complete := range []bool{false, true} {
		intent := &entity.IntegrationIntent{Operation: "ssh.compose.deploy", ResourceScope: values, EffectPreview: map[string]any{"fields": []any{}, "contentComplete": complete}}
		if err := appendSSHComposeGatePreview(intent, target.Ref, strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 64), target.RuntimeImage); err != nil {
			t.Fatal(err)
		}
		if intent.EffectPreview["contentComplete"] != complete || len(intent.EffectPreview["fields"].([]any)) != 4 {
			t.Fatal("preview completeness changed")
		}
		raw := string(asJSON(intent.EffectPreview))
		for _, private := range []string{"sshSecret", "originRuntimeRevisionId", "originLeaseId", "secretUid", "PRIVATE_KEY"} {
			if strings.Contains(raw, private) {
				t.Fatal("private metadata leaked")
			}
		}
		if err := appendSSHComposeGatePreview(intent, "ssh_foreign1838", strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 64), target.RuntimeImage); err == nil {
			t.Fatal("scope substitution accepted")
		}
	}
}
