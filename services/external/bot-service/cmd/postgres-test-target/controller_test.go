package main

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDockerRunArgumentsUseExactIdentityAndLoopbackRandomPort(t *testing.T) {
	image, err := postgresControllerImage("15")
	if err != nil {
		t.Fatal(err)
	}
	const expiresAt = int64(1900000000)
	arguments := dockerRunArguments("run-identity", image, expiresAt)
	for _, required := range []string{
		"--rm", "--init", "--stop-timeout", "30",
		"--name", "mc-postgres-test-run-identity",
		"--label", postgresControllerLabel + "=run-identity",
		"--label", postgresControllerLabel + "-expires-at=" + strconv.FormatInt(expiresAt, 10),
		"--publish", "127.0.0.1::5432",
		"--entrypoint", "/usr/bin/timeout", "12m", "/usr/local/bin/docker-entrypoint.sh", "postgres",
	} {
		if !slices.Contains(arguments, required) {
			t.Fatalf("docker arguments не содержат %q: %#v", required, arguments)
		}
	}
	if slices.Contains(arguments, "network") {
		t.Fatal("Docker controller не должен создавать persistent custom network")
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
		arguments := dockerRunArguments("run-identity", image, 1900000000)
		if !slices.Contains(arguments, image) {
			t.Fatalf("Docker image для PG%s не закреплён", major)
		}
		job := kubernetesPostgresJob("run-identity", image)
		if job.Spec.Template.Spec.Containers[0].Image != image {
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

func TestAllowControllerPostgresEndpointRegistersAndRestoresExactEndpoint(t *testing.T) {
	const variable = "MATTERCODEX_POSTGRES_TEST_EPHEMERAL_ENDPOINTS"
	t.Setenv(variable, "10.0.0.8:15432")
	restore, err := allowControllerPostgresEndpoint("host=127.0.0.1 port=25432 user=test dbname=postgres")
	if err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(variable); got != "10.0.0.8:15432,127.0.0.1:25432" {
		t.Fatalf("controller allowlist = %q", got)
	}
	restore()
	if got := os.Getenv(variable); got != "10.0.0.8:15432" {
		t.Fatalf("restored allowlist = %q", got)
	}
}

func TestKubernetesPostgresJobHasKillSafeLifetimeWithoutPrivilegeExpansion(t *testing.T) {
	job := kubernetesPostgresJob("run-identity", "example.invalid/postgres:test")
	if job.GenerateName != "mc-postgres-test-" || job.Labels[postgresControllerLabel] != "run-identity" {
		t.Fatalf("Job identity = %#v", job.ObjectMeta)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != int64(postgresControllerLifetime/time.Second) ||
		job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != postgresControllerTTLSeconds ||
		job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("Job lifetime = %#v", job.Spec)
	}
	pod := job.Spec.Template.Spec
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("disposable PostgreSQL Pod получает ServiceAccount token")
	}
	if len(pod.Containers) != 1 || pod.Containers[0].SecurityContext == nil ||
		pod.Containers[0].SecurityContext.AllowPrivilegeEscalation == nil || *pod.Containers[0].SecurityContext.AllowPrivilegeEscalation {
		t.Fatalf("Pod security context = %#v", pod.Containers)
	}
	if len(pod.Containers[0].SecurityContext.Capabilities.Drop) != 1 || pod.Containers[0].SecurityContext.Capabilities.Drop[0] != "ALL" {
		t.Fatal("disposable PostgreSQL Pod не удаляет Linux capabilities")
	}
	readiness := pod.Containers[0].ReadinessProbe
	expectedReadinessCommand := []string{"pg_isready", "--host", "127.0.0.1", "--port", "5432", "--username", "mattercodex_test_owner", "--dbname", "postgres"}
	if readiness == nil || readiness.Exec == nil || !slices.Equal(readiness.Exec.Command, expectedReadinessCommand) || readiness.PeriodSeconds != 1 || readiness.TimeoutSeconds != 1 {
		t.Fatalf("PostgreSQL readiness probe = %#v", readiness)
	}
}

func TestKubernetesPortForwardArgumentsUseCanonicalLoopbackForm(t *testing.T) {
	arguments := kubernetesPortForwardArguments("disposable-tests", "mc-postgres-test-abc", 25432)
	expected := []string{
		"--namespace", "disposable-tests",
		"port-forward",
		"--address", "127.0.0.1",
		"service/mc-postgres-test-abc",
		"25432:5432",
	}
	if !slices.Equal(arguments, expected) {
		t.Fatalf("kubectl port-forward argv = %#v, ожидалось %#v", arguments, expected)
	}
}

func TestKubernetesPostgresReadinessRetriesEarlyConnectionAndRestartsDeadTunnel(t *testing.T) {
	t.Run("ранняя неготовность сохраняет живой tunnel", func(t *testing.T) {
		process := newFakePostgresPortForward()
		starts := 0
		probes := 0
		ready, err := waitKubernetesPostgresReady(context.Background(), func() (postgresPortForward, error) {
			starts++
			return process, nil
		}, func(context.Context) error {
			probes++
			if probes == 1 {
				return errors.New("synthetic backend connection refused")
			}
			return nil
		}, func(context.Context) error { return nil })
		if err != nil || ready != process || starts != 1 || probes != 2 || process.stopCalls != 0 {
			t.Fatalf("readiness retry: ready=%T starts=%d probes=%d stops=%d error=%v", ready, starts, probes, process.stopCalls, err)
		}
	})

	t.Run("первое подключение завершает tunnel", func(t *testing.T) {
		first := newFakePostgresPortForward()
		second := newFakePostgresPortForward()
		starts := 0
		probes := 0
		ready, err := waitKubernetesPostgresReady(context.Background(), func() (postgresPortForward, error) {
			starts++
			if starts == 1 {
				return first, nil
			}
			return second, nil
		}, func(context.Context) error {
			probes++
			if probes == 1 {
				close(first.done)
				return errors.New("synthetic backend connection refused")
			}
			return nil
		}, func(context.Context) error { return nil })
		if err != nil || ready != second || starts != 2 || probes != 2 || first.stopCalls != 1 || second.stopCalls != 0 {
			t.Fatalf("tunnel restart: ready=%T starts=%d probes=%d first_stops=%d second_stops=%d error=%v", ready, starts, probes, first.stopCalls, second.stopCalls, err)
		}
	})
}

type fakePostgresPortForward struct {
	done      chan struct{}
	stopCalls int
}

func newFakePostgresPortForward() *fakePostgresPortForward {
	return &fakePostgresPortForward{done: make(chan struct{})}
}

func (process *fakePostgresPortForward) Done() <-chan struct{} {
	return process.done
}

func (process *fakePostgresPortForward) Stop() {
	process.stopCalls++
}

func TestKubernetesPostgresServiceIsClusterInternalAndSelectsExactRun(t *testing.T) {
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "postgres-job", UID: types.UID("job-uid")}}
	service := kubernetesPostgresService("run-identity", job)
	if service.Spec.Type != "ClusterIP" || service.Spec.Selector[postgresControllerLabel] != "run-identity" || service.Labels[postgresControllerLabel] != "run-identity" {
		t.Fatalf("Service boundary = %#v", service)
	}
	if len(service.Spec.Ports) != 1 || service.Spec.Ports[0].Port != 5432 {
		t.Fatalf("Service ports = %#v", service.Spec.Ports)
	}
	if len(service.OwnerReferences) != 1 || service.OwnerReferences[0].UID != job.UID || service.OwnerReferences[0].Kind != "Job" || service.OwnerReferences[0].Controller == nil || !*service.OwnerReferences[0].Controller {
		t.Fatalf("Service ownerRef = %#v", service.OwnerReferences)
	}
}

func TestKubernetesCleanupRefusesReplacementService(t *testing.T) {
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "postgres-job", UID: types.UID("job-uid")}}
	service := kubernetesPostgresService("replacement-run", job)
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

func TestKubernetesControllerRefusesUnlabelledDisposableTargetBeforeMutation(t *testing.T) {
	t.Setenv("MATTERCODEX_TEST_POSTGRES_K8S_NAMESPACE", "")
	if _, err := startKubernetesPostgres(context.Background(), "15"); err == nil || !strings.Contains(err.Error(), "disposable namespace") {
		t.Fatalf("kubernetes admission error = %v", err)
	}
}
