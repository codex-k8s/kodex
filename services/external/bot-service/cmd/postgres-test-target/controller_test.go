package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDockerRunArgumentsUseExactIdentityAndLoopbackRandomPort(t *testing.T) {
	image, err := postgresControllerImage("15")
	if err != nil {
		t.Fatal(err)
	}
	arguments := dockerRunArguments("run-identity", image)
	for _, required := range []string{
		"--name", "mc-postgres-test-run-identity",
		"--label", postgresControllerLabel + "=run-identity",
		"--publish", "127.0.0.1::5432",
	} {
		if !slices.Contains(arguments, required) {
			t.Fatalf("docker arguments не содержат %q: %#v", required, arguments)
		}
	}
	if slices.Contains(arguments, "--rm") {
		t.Fatal("Docker controller не должен терять identity до подтверждённого cleanup")
	}
}

func TestPostgresControllerImagesRequireDigestAndExactMajorMapping(t *testing.T) {
	for _, major := range []string{"15", "16"} {
		image, err := postgresControllerImage(major)
		if err != nil {
			t.Fatalf("postgresControllerImage(%s): %v", major, err)
		}
		if err := validatePostgresControllerImage(major, image); err != nil {
			t.Fatalf("validatePostgresControllerImage(%s): %v", major, err)
		}
		arguments := dockerRunArguments("run-identity", image)
		if arguments[len(arguments)-1] != image {
			t.Fatalf("Docker image для PG%s не закреплён", major)
		}
		pod := kubernetesPostgresPod("run-identity", image)
		if pod.Spec.Containers[0].Image != image {
			t.Fatalf("Kubernetes image для PG%s не закреплён", major)
		}
	}
	for _, testCase := range []struct {
		major string
		image string
	}{
		{major: "15", image: "pgvector/pgvector:0.8.5-pg15"},
		{major: "15", image: postgres16ControllerImage},
		{major: "16", image: postgres15ControllerImage},
		{major: "16", image: "pgvector/pgvector:0.8.5-pg16@sha256:not-a-digest"},
	} {
		if err := validatePostgresControllerImage(testCase.major, testCase.image); err == nil {
			t.Fatalf("небезопасный image ref принят для PG%s", testCase.major)
		}
	}
}

func TestParseLoopbackDockerPortRejectsBroadOrAmbiguousBinding(t *testing.T) {
	port, err := parseLoopbackDockerPort("127.0.0.1:49152\n")
	if err != nil || port != 49152 {
		t.Fatalf("parseLoopbackDockerPort() = %d, %v", port, err)
	}
	for _, value := range []string{"0.0.0.0:49152", "127.0.0.1:49152\n127.0.0.1:49153", "127.0.0.1:0"} {
		if _, err := parseLoopbackDockerPort(value); err == nil {
			t.Fatalf("небезопасный Docker port mapping принят: %q", value)
		}
	}
}

func TestKubernetesPostgresPodHasNoServiceAccountTokenOrPrivilegeExpansion(t *testing.T) {
	pod := kubernetesPostgresPod("run-identity", "example.invalid/postgres:test")
	if pod.GenerateName != "mc-postgres-test-" || pod.Labels[postgresControllerLabel] != "run-identity" {
		t.Fatalf("Pod identity = %#v", pod.ObjectMeta)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Fatal("disposable PostgreSQL Pod получает ServiceAccount token")
	}
	if len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].SecurityContext == nil ||
		pod.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation == nil || *pod.Spec.Containers[0].SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("Pod security context = %#v", pod.Spec.Containers)
	}
	if len(pod.Spec.Containers[0].SecurityContext.Capabilities.Drop) != 1 || pod.Spec.Containers[0].SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatal("disposable PostgreSQL Pod не удаляет Linux capabilities")
	}
}

func TestKubernetesPostgresServiceIsClusterInternalAndSelectsExactRun(t *testing.T) {
	service := kubernetesPostgresService("run-identity")
	if service.Spec.Type != "ClusterIP" || service.Spec.Selector[postgresControllerLabel] != "run-identity" || service.Labels[postgresControllerLabel] != "run-identity" {
		t.Fatalf("Service boundary = %#v", service)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != 5432 {
		t.Fatalf("Service ports = %#v", service.Spec.Ports)
	}
}

func TestKubernetesCleanupRefusesReplacementService(t *testing.T) {
	service := kubernetesPostgresService("replacement-run")
	service.Name = "mc-postgres-test-replacement"
	service.GenerateName = ""
	service.Namespace = "disposable-tests"
	service.UID = types.UID("replacement-uid")
	client := fake.NewClientset(service)
	err := waitKubernetesServiceDeleted(context.Background(), client, service.Namespace, service.Name, types.UID("original-uid"))
	if err == nil || !strings.Contains(err.Error(), "replacement Service") {
		t.Fatalf("replacement cleanup error = %v", err)
	}
	current, readErr := client.CoreV1().Services(service.Namespace).Get(context.Background(), service.Name, metav1.GetOptions{})
	if readErr != nil || current.UID != service.UID {
		t.Fatalf("replacement Service был изменён: item=%#v error=%v", current, readErr)
	}
}
