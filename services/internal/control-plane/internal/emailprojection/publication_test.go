package emailprojection

import (
	"context"
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type publicationResolver struct{}

func (publicationResolver) Resolve(context.Context, string) (mailpolicy.Snapshot, error) {
	return mailpolicy.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}, ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func publicationFixture(t *testing.T) (*Kubernetes, *fake.Clientset, api.Configuration, mailpolicy.MailDocument) {
	t.Helper()
	publisher, client, configuration := projectionFixture(t)
	document, err := mailpolicy.Produce(t.Context(), configuration, strings.Repeat("a", 64), publicationResolver{})
	if err != nil {
		t.Fatal(err)
	}
	policySource, bindingSource := mailpolicy.PublicationAdmissionResources()
	raw, _ := json.Marshal(policySource)
	var policy admissionv1.ValidatingAdmissionPolicy
	if json.Unmarshal(raw, &policy) != nil {
		t.Fatal("decode policy fixture")
	}
	policy.UID, policy.ResourceVersion, policy.Generation = "policy-fixture", "1", 1
	policy.Status = admissionv1.ValidatingAdmissionPolicyStatus{ObservedGeneration: 1, TypeChecking: &admissionv1.TypeChecking{}}
	raw, _ = json.Marshal(bindingSource)
	var binding admissionv1.ValidatingAdmissionPolicyBinding
	if json.Unmarshal(raw, &binding) != nil {
		t.Fatal("decode binding fixture")
	}
	binding.UID, binding.ResourceVersion = "binding-fixture", "1"
	if err := client.Tracker().Add(&policy); err != nil {
		t.Fatal(err)
	}
	if err := client.Tracker().Add(&binding); err != nil {
		t.Fatal(err)
	}
	network := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: mailPolicyNetworkName, Namespace: "kodex-system", UID: "network-fixture", ResourceVersion: "1"}}
	if err := client.Tracker().Add(network); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"egress-gateway", "email-bridge"} {
		replicas := int32(2)
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kodex-system", UID: "deployment-fixture", ResourceVersion: "1", Generation: 1},
			Spec: appsv1.DeploymentSpec{Replicas: &replicas, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: name}}}}}}
		if name == "egress-gateway" {
			deployment.Spec.Template.Spec.Containers[0].Env = []corev1.EnvVar{{Name: "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST", Value: document.GatewayPolicyDigest}, {Name: "EGRESS_GATEWAY_MAIL_POLICY_FILE", Value: "/var/run/config/kodex/egress-gateway-mail/mail-policy.json"}}
			deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "mail-policy", MountPath: "/var/run/config/kodex/egress-gateway-mail", ReadOnly: true}}
			deployment.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "mail-policy"}}
		}
		if err := client.Tracker().Add(deployment); err != nil {
			t.Fatal(err)
		}
	}
	client.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		object := action.(k8stesting.CreateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		object.UID, object.ResourceVersion = "configmap-fixture", "1"
		err := client.Tracker().Create(action.GetResource(), object, action.GetNamespace())
		return true, object, err
	})
	client.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "" {
			return false, nil, nil
		}
		object := action.(k8stesting.UpdateAction).GetObject().(*appsv1.Deployment).DeepCopy()
		object.Generation++
		err := client.Tracker().Update(action.GetResource(), object, action.GetNamespace())
		return true, object, err
	})
	return publisher, client, configuration, document
}

func readyPublicationDeployments(t *testing.T, client *fake.Clientset) {
	t.Helper()
	for _, name := range []string{"egress-gateway", "email-bridge"} {
		deployment, err := client.AppsV1().Deployments("kodex-system").Get(t.Context(), name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		replicas := *deployment.Spec.Replicas
		deployment.Status = appsv1.DeploymentStatus{ObservedGeneration: deployment.Generation, Replicas: replicas, UpdatedReplicas: replicas, ReadyReplicas: replicas, AvailableReplicas: replicas}
		if _, err := client.AppsV1().Deployments("kodex-system").UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMailboxPublicationRequiresFullApplyAndReplicaReadback(t *testing.T) {
	publisher, client, configuration, document := publicationFixture(t)
	digests := fixtureCredentialDigests(configuration)
	if _, err := publisher.PublishMailbox(t.Context(), configuration, digests, document); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.CheckMailbox(t.Context(), configuration, digests, document); err == nil {
		t.Fatal("unobserved deployment accepted")
	}
	readyPublicationDeployments(t, client)
	if _, err := publisher.CheckMailbox(t.Context(), configuration, digests, document); err != nil {
		t.Fatal(err)
	}
	deployment, _ := client.AppsV1().Deployments("kodex-system").Get(t.Context(), "email-bridge", metav1.GetOptions{})
	deployment.Status.UpdatedReplicas = 1
	if _, err := client.AppsV1().Deployments("kodex-system").UpdateStatus(t.Context(), deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.CheckMailbox(t.Context(), configuration, digests, document); err == nil {
		t.Fatal("single replica proves entire deployment")
	}
	readyPublicationDeployments(t, client)
	network, _ := client.NetworkingV1().NetworkPolicies("kodex-system").Get(t.Context(), mailPolicyNetworkName, metav1.GetOptions{})
	network.Spec.Egress[0].To[0].IPBlock.CIDR = "0.0.0.0/0"
	if _, err := client.NetworkingV1().NetworkPolicies("kodex-system").Update(t.Context(), network, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.CheckMailbox(t.Context(), configuration, digests, document); err == nil {
		t.Fatal("widened CNI policy accepted")
	}
}

func TestMailboxPublicationAdmissionFailsBeforeCreate(t *testing.T) {
	for _, mode := range []string{"missing", "warning", "generation", "binding"} {
		t.Run(mode, func(t *testing.T) {
			publisher, client, configuration, document := publicationFixture(t)
			policy, _ := client.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(t.Context(), mailpolicy.PublicationAdmissionName, metav1.GetOptions{})
			switch mode {
			case "missing":
				_ = client.AdmissionregistrationV1().ValidatingAdmissionPolicies().Delete(t.Context(), policy.Name, metav1.DeleteOptions{})
			case "warning":
				policy.Status.TypeChecking.ExpressionWarnings = []admissionv1.ExpressionWarning{{FieldRef: "spec.validations[0].expression", Warning: "fixture warning"}}
				_, _ = client.AdmissionregistrationV1().ValidatingAdmissionPolicies().UpdateStatus(t.Context(), policy, metav1.UpdateOptions{})
			case "generation":
				policy.Status.ObservedGeneration = 0
				_, _ = client.AdmissionregistrationV1().ValidatingAdmissionPolicies().UpdateStatus(t.Context(), policy, metav1.UpdateOptions{})
			case "binding":
				binding, _ := client.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Get(t.Context(), mailpolicy.PublicationAdmissionName, metav1.GetOptions{})
				binding.Spec.ValidationActions = []admissionv1.ValidationAction{admissionv1.Warn}
				_, _ = client.AdmissionregistrationV1().ValidatingAdmissionPolicyBindings().Update(t.Context(), binding, metav1.UpdateOptions{})
			}
			client.ClearActions()
			if _, err := publisher.PublishMailbox(t.Context(), configuration, fixtureCredentialDigests(configuration), document); err == nil {
				t.Fatal("unserved admission accepted")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("external effect before admission readback")
				}
			}
		})
	}
}

func TestMailboxPublicationCannotRollBackNetworkOrSecret(t *testing.T) {
	publisher, client, configuration, document := publicationFixture(t)
	if _, err := publisher.PublishMailbox(t.Context(), configuration, fixtureCredentialDigests(configuration), document); err != nil {
		t.Fatal(err)
	}
	next := configuration
	next.Revision++
	nextDocument, err := mailpolicy.Produce(t.Context(), next, document.GatewayPolicyDigest, publicationResolver{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.PublishMailbox(t.Context(), next, fixtureCredentialDigests(next), nextDocument); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.PublishMailbox(t.Context(), configuration, fixtureCredentialDigests(configuration), document); err == nil {
		t.Fatal("old publication replaced new state")
	}
	secret, _ := client.CoreV1().Secrets("kodex-system").Get(t.Context(), SecretName, metav1.GetOptions{})
	var actual api.Configuration
	if api.Decode(secret.Data[DocumentKey], &actual) != nil || actual.Revision != next.Revision {
		t.Fatal("old publication changed secret snapshot")
	}
}
