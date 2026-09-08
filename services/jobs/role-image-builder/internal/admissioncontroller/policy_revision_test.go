package admissioncontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func versionedTestPolicy(t *testing.T) *corev1.ConfigMap {
	t.Helper()
	p := completeTestPolicy()
	payload := map[string]string{}
	for key, value := range p.Data {
		if key != "policySHA256" && key != "orchestrationRevision" {
			payload[key] = value
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(append(encoded, '\n'))
	p.Data["policySHA256"] = hex.EncodeToString(digest[:])
	p.Name = policyName + "-" + p.Data["policySHA256"][:32]
	return p
}

func TestVersionedPolicyRequiresExactConfiguredNameAndCompleteDigest(t *testing.T) {
	p := versionedTestPolicy(t)
	if _, err := validatePolicy(p, p.Name); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.PolicyConfigMap = p.Name
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*corev1.ConfigMap){
		func(p *corev1.ConfigMap) { p.Data["trustedRoleBaseDigest"] = "sha256:" + strings.Repeat("9", 64) },
		func(p *corev1.ConfigMap) { p.Data["policyRevision"] = "8" },
		func(p *corev1.ConfigMap) { p.Data["hidden"] = "unexpected" },
		func(p *corev1.ConfigMap) { p.Data["policySHA256"] = strings.Repeat("0", 64) },
		func(p *corev1.ConfigMap) { p.Name = policyName },
		func(p *corev1.ConfigMap) { *p.Immutable = false },
		func(p *corev1.ConfigMap) { p.Namespace = "foreign" },
		func(p *corev1.ConfigMap) { p.Labels["kodex.dev/owner-intent"] = "false" },
		func(p *corev1.ConfigMap) { p.Data["orchestrationRevision"] = strings.Repeat("0", 40) },
	} {
		bad := p.DeepCopy()
		mutate(bad)
		if _, err := validatePolicy(bad, p.Name); err == nil {
			t.Fatal("changed policy was accepted")
		}
	}
	for _, name := range []string{"foreign", policyName + "-latest", policyName + "-" + strings.Repeat("a", 31)} {
		cfg.PolicyConfigMap = name
		if cfg.Validate() == nil {
			t.Fatal("unregistered policy name was accepted")
		}
	}
	if _, err := validatePolicy(testPolicy(), policyName); err != nil {
		t.Fatal("legacy reader compatibility was lost")
	}
}

func TestPauseNewRunsDrainsExistingChainWithoutCreatingAnotherCycle(t *testing.T) {
	policy := versionedTestPolicy(t)
	client := fake.NewClientset(policy)
	cfg := testConfig()
	cfg.PolicyConfigMap = policy.Name
	controller, err := New(client, testRenderer{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)
	controller.now = func() time.Time { return now }
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, _ := client.CoreV1().PersistentVolumeClaims(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if len(workspaces.Items) != 1 {
		t.Fatal("initial admission chain is absent")
	}
	id := workspaces.Items[0].Labels[idLabel]
	controller.config.PauseNewRuns = true
	if err := controller.Check(ctx); err != nil {
		t.Fatal("paused controller must remain ready", err)
	}
	for _, phase := range phaseOrder {
		markJobSucceeded(t, client, id, phase)
		now = now.Add(time.Minute)
		if err := controller.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
		if phase != "admit" {
			assertJob(t, client, id, phaseOrder[indexOf(phaseOrder, phase)+1])
		}
	}
	markJobSucceeded(t, client, id, "promote")
	now = now.Add(time.Minute)
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, _ = client.CoreV1().PersistentVolumeClaims(cfg.Namespace).List(ctx, metav1.ListOptions{})
	jobs, _ := client.BatchV1().Jobs(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if len(workspaces.Items) != 0 || len(jobs.Items) != 1 || jobs.Items[0].Labels[phaseLabel] != "promote" || !jobTerminal(&jobs.Items[0]) {
		t.Fatal("pause created a new cycle or failed to drain the old one")
	}
	controller.config.PauseNewRuns = false
	if err := controller.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	workspaces, _ = client.CoreV1().PersistentVolumeClaims(cfg.Namespace).List(ctx, metav1.ListOptions{})
	if len(workspaces.Items) != 1 {
		t.Fatal("resume did not create a new admission chain")
	}
}
