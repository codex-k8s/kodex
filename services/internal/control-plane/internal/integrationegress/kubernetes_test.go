package integrationegress

import (
	"strings"
	"testing"

	shared "github.com/codex-k8s/kodex/libs/go/integrationegresspolicy"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func documentFixture() shared.Document {
	return shared.Document{Schema: shared.Schema, Generation: 1, SourceDigest: shared.SourceDigest(nil),
		GatewayPolicyDigest: strings.Repeat("a", 64), Destinations: []shared.Destination{}}
}

func deploymentFixture(document shared.Document) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: deploymentName, Namespace: namespace,
		Labels: map[string]string{"app.kubernetes.io/name": deploymentName, "app.kubernetes.io/component": "platform-egress"}},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: deploymentName,
				Env: []corev1.EnvVar{
					{Name: "EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST", Value: document.GatewayPolicyDigest},
					{Name: "EGRESS_GATEWAY_INTEGRATION_POLICY_FILE", Value: "/var/run/config/kodex/egress-gateway-integration/integration-policy.json"},
					{Name: "EGRESS_GATEWAY_INTEGRATION_CONNECT_LISTEN", Value: ":8083"},
				}, Ports: []corev1.ContainerPort{{Name: "openapi-connect", ContainerPort: 8083, Protocol: corev1.ProtocolTCP}},
				VolumeMounts: []corev1.VolumeMount{{Name: "integration-policy", MountPath: "/var/run/config/kodex/egress-gateway-integration", ReadOnly: true}},
			}},
			Volumes: []corev1.Volume{{Name: "integration-policy", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{}}}},
		}}}}
}

func TestDeploymentSwitchIsExactAndFenced(t *testing.T) {
	document := documentFixture()
	deployment := deploymentFixture(document)
	if err := setDeployment(deployment, document, "egress-gateway-integration-"+document.Digest()[:24]); err != nil {
		t.Fatal(err)
	}
	if deployment.Spec.Template.Annotations[generationAnnotation] != "1" ||
		deployment.Spec.Template.Labels[generationLabel] != "1" ||
		deployment.Spec.Template.Spec.Containers[0].Env[3].Value != document.Digest() ||
		deployment.Spec.Template.Spec.Volumes[0].ConfigMap.Name != "egress-gateway-integration-"+document.Digest()[:24] {
		t.Fatal("deployment did not mount exact immutable policy")
	}
	if err := setDeployment(deployment, document, "egress-gateway-integration-"+document.Digest()[:24]); err != nil {
		t.Fatal("identical replay must be idempotent", err)
	}
	stale := document
	stale.Generation = 2
	stale.SourceDigest = strings.Repeat("b", 64)
	if err := setDeployment(deployment, stale, "other"); err == nil {
		t.Fatal("forged source digest accepted")
	}
	deployment.Spec.Template.Annotations[generationAnnotation] = "3"
	if err := setDeployment(deployment, document, "other"); err == nil {
		t.Fatal("stale generation replaced deployment")
	}
	foreign := deploymentFixture(document)
	foreign.Labels["app.kubernetes.io/name"] = "other"
	if err := setDeployment(foreign, document, "other"); err == nil {
		t.Fatal("foreign deployment accepted")
	}
	missing := deploymentFixture(document)
	missing.Spec.Template.Spec.Containers[0].Ports = nil
	if err := setDeployment(missing, document, "other"); err == nil {
		t.Fatal("missing listener port accepted")
	}
}

func openAPIServiceFixture() *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: openAPIServiceName, Namespace: namespace,
		Labels: map[string]string{"app.kubernetes.io/name": deploymentName, "app.kubernetes.io/component": "platform-egress"}},
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app.kubernetes.io/name": deploymentName, "app.kubernetes.io/component": "platform-egress", generationLabel: "1"},
			Ports: []corev1.ServicePort{{Name: "openapi-connect", Port: 8083, Protocol: corev1.ProtocolTCP,
				TargetPort: intstr.FromString("openapi-connect")}}}}
}

func TestOpenAPIServiceSwitchIsExactAndFenced(t *testing.T) {
	document := documentFixture()
	document.Generation = 2
	service := openAPIServiceFixture()
	if err := setOpenAPIService(service, document); err != nil {
		t.Fatal(err)
	}
	if service.Spec.Selector[generationLabel] != "2" ||
		service.Annotations[generationAnnotation] != "2" ||
		service.Annotations[sourceAnnotation] != document.SourceDigest {
		t.Fatal("OpenAPI Service did not select the exact published generation")
	}
	if err := setOpenAPIService(service, document); err != nil {
		t.Fatal("identical replay must be idempotent", err)
	}
	stale := document
	stale.Generation = 1
	if err := setOpenAPIService(service, stale); err == nil {
		t.Fatal("stale generation replaced OpenAPI Service")
	}
	forged := service.DeepCopy()
	forged.Spec.Selector[generationLabel] = "1"
	if err := setOpenAPIService(forged, document); err == nil {
		t.Fatal("selector diverged from the fenced generation")
	}
	foreign := openAPIServiceFixture()
	foreign.Spec.Selector["app.kubernetes.io/component"] = "other"
	if err := setOpenAPIService(foreign, document); err == nil {
		t.Fatal("foreign Service selector accepted")
	}
	broad := openAPIServiceFixture()
	delete(broad.Spec.Selector, generationLabel)
	if err := setOpenAPIService(broad, document); err == nil {
		t.Fatal("unfenced broad Service selector accepted")
	}
	external := openAPIServiceFixture()
	external.Spec.ExternalIPs = []string{"203.0.113.10"}
	if err := setOpenAPIService(external, document); err == nil {
		t.Fatal("Service with external IP accepted")
	}
}

func TestNetworkBootstrapRecoveryRequiresExactEmptyPolicy(t *testing.T) {
	document := documentFixture()
	document.Generation = 2
	policy := &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
			"app.kubernetes.io/name": deploymentName, "app.kubernetes.io/component": "platform-egress",
		}},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
	}}
	if err := checkNetworkFence(policy, document); err != nil {
		t.Fatalf("exact empty bootstrap policy cannot recover: %v", err)
	}
	policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{{}}
	if err := checkNetworkFence(policy, document); err == nil {
		t.Fatal("unfenced open egress policy accepted")
	}
	policy.Spec.Egress = nil
	policy.Annotations = map[string]string{generationAnnotation: "3", sourceAnnotation: document.SourceDigest}
	if err := checkNetworkFence(policy, document); err == nil {
		t.Fatal("newer generation was overwritten")
	}
}

func TestProjectionLabelsAcceptOnlyRepoOwnedLocalProvenance(t *testing.T) {
	expected := map[string]string{"app.kubernetes.io/name": deploymentName, "app.kubernetes.io/component": "platform-egress"}
	if !validProjectionLabels(expected, expected) {
		t.Fatal("controller-created labels rejected")
	}
	local := map[string]string{"app.kubernetes.io/name": deploymentName, "app.kubernetes.io/component": "platform-egress",
		"app.kubernetes.io/part-of": "kodex", "kodex.dev/environment": "staging", "kodex.dev/local-profile": "hot-reload",
		"kodex.dev/profile": "web-only", "kodex.dev/security-profile": "trusted-cluster"}
	if !validProjectionLabels(local, expected) {
		t.Fatal("local bootstrap provenance rejected")
	}
	local["unexpected"] = "true"
	if validProjectionLabels(local, expected) {
		t.Fatal("foreign provenance label accepted")
	}
}
