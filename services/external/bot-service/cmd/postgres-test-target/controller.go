package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/bot-service/internal/repository/postgres/testsupport"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	postgresControllerLabel          = "mattercodex.dev/postgres-test-run"
	postgresDisposableNamespaceLabel = "mattercodex.dev/disposable-postgres-tests"
	postgresContainerPort            = 5432
	postgres15ControllerImage        = "pgvector/pgvector:0.8.5-pg15@sha256:18d16372b8406bb38a9f94cbff15d125c463d71fde2770aa8b5c64bfcc1578ee"
	postgres16ControllerImage        = "pgvector/pgvector:0.8.5-pg16@sha256:1d533553fefe4f12e5d80c7b80622ba0c382abb5758856f52983d8789179f0fb"
)

type controllerPostgresHarness struct {
	bootstrapDSN   string
	bootstrapProof string
	close          func(context.Context) error
}

func (harness *controllerPostgresHarness) Close(ctx context.Context) error {
	if harness == nil || harness.close == nil {
		return nil
	}
	closeFunction := harness.close
	harness.close = nil
	return closeFunction(ctx)
}

func startControllerPostgres(ctx context.Context, mode postgresTestMode, major string) (*controllerPostgresHarness, error) {
	if major != "15" && major != "16" {
		return nil, fmt.Errorf("controller mode требует exact PostgreSQL major")
	}
	var harness *controllerPostgresHarness
	var err error
	switch mode {
	case postgresTestModeDocker:
		harness, err = startDockerPostgres(ctx, major)
	case postgresTestModeKubernetes:
		harness, err = startKubernetesPostgres(ctx, major)
	default:
		return nil, fmt.Errorf("mode не принадлежит external controller")
	}
	if err != nil {
		return nil, err
	}
	proof, err := testsupport.PrepareControllerOwnedPostgres(ctx, harness.bootstrapDSN, major)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = harness.Close(cleanupCtx)
		cancel()
		return nil, err
	}
	harness.bootstrapProof = proof
	return harness, nil
}

func postgresControllerImage(major string) (string, error) {
	switch major {
	case "15":
		return postgres15ControllerImage, nil
	case "16":
		return postgres16ControllerImage, nil
	default:
		return "", fmt.Errorf("unsupported PostgreSQL major")
	}
}

func validatePostgresControllerImage(major string, image string) error {
	expected, err := postgresControllerImage(major)
	if err != nil || image != expected {
		return fmt.Errorf("PostgreSQL controller image не закреплён за exact major и OCI digest")
	}
	tagDigest := strings.Split(image, "@sha256:")
	if len(tagDigest) != 2 || len(tagDigest[1]) != 64 || !strings.HasSuffix(tagDigest[0], "-pg"+major) {
		return fmt.Errorf("PostgreSQL controller image имеет недопустимый OCI ref")
	}
	if _, err := hex.DecodeString(tagDigest[1]); err != nil {
		return fmt.Errorf("PostgreSQL controller image имеет недопустимый OCI digest")
	}
	return nil
}

func randomControllerIdentity() (string, error) {
	body := make([]byte, 12)
	if _, err := rand.Read(body); err != nil {
		return "", err
	}
	return hex.EncodeToString(body), nil
}

func dockerRunArguments(identity string, image string) []string {
	return []string{
		"run", "--detach",
		"--name", "mc-postgres-test-" + identity,
		"--label", postgresControllerLabel + "=" + identity,
		"--publish", "127.0.0.1::5432",
		"--env", "POSTGRES_HOST_AUTH_METHOD=trust",
		"--env", "POSTGRES_USER=mattercodex_test_owner",
		"--env", "POSTGRES_DB=postgres",
		image,
	}
}

func startDockerPostgres(ctx context.Context, major string) (*controllerPostgresHarness, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker mode недоступен")
	}
	if err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ID}}").Run(); err != nil {
		return nil, fmt.Errorf("docker mode недоступен")
	}
	identity, err := randomControllerIdentity()
	if err != nil {
		return nil, fmt.Errorf("docker mode не создал run identity")
	}
	image, err := postgresControllerImage(major)
	if err != nil {
		return nil, err
	}
	if err := validatePostgresControllerImage(major, image); err != nil {
		return nil, err
	}
	containerOutput, err := exec.CommandContext(ctx, "docker", dockerRunArguments(identity, image)...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker mode не создал disposable container")
	}
	containerID := strings.TrimSpace(string(containerOutput))
	if containerID == "" {
		return nil, fmt.Errorf("docker mode не получил exact container identity")
	}
	harness := &controllerPostgresHarness{}
	harness.close = func(closeCtx context.Context) error {
		if err := validateDockerContainerIdentity(closeCtx, containerID, identity); err != nil {
			return err
		}
		if err := exec.CommandContext(closeCtx, "docker", "rm", "--force", containerID).Run(); err != nil {
			return fmt.Errorf("docker mode не удалил exact container")
		}
		exists, err := dockerContainerExists(closeCtx, containerID)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("docker mode не подтвердил удаление exact container")
		}
		return nil
	}
	cleanupOnError := func(startErr error) (*controllerPostgresHarness, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr := harness.Close(cleanupCtx)
		cancel()
		return nil, errors.Join(startErr, cleanupErr)
	}
	if err := validateDockerContainerIdentity(ctx, containerID, identity); err != nil {
		return cleanupOnError(err)
	}
	portOutput, err := exec.CommandContext(ctx, "docker", "port", containerID, "5432/tcp").Output()
	if err != nil {
		return cleanupOnError(fmt.Errorf("docker mode не получил loopback port"))
	}
	port, err := parseLoopbackDockerPort(string(portOutput))
	if err != nil {
		return cleanupOnError(err)
	}
	if err := waitTCP(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err != nil {
		return cleanupOnError(fmt.Errorf("docker PostgreSQL не стал готов"))
	}
	harness.bootstrapDSN = fmt.Sprintf("host=127.0.0.1 port=%d user=mattercodex_test_owner dbname=postgres connect_timeout=5", port)
	return harness, nil
}

func dockerContainerExists(ctx context.Context, containerID string) (bool, error) {
	output, err := exec.CommandContext(ctx, "docker", "ps", "--all", "--quiet", "--no-trunc", "--filter", "id="+containerID).Output()
	if err != nil {
		return false, fmt.Errorf("docker mode не подтвердил состояние exact container после cleanup")
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return false, nil
	}
	if trimmed != containerID {
		return false, fmt.Errorf("docker mode получил неоднозначную identity после cleanup")
	}
	return true, nil
}

func validateDockerContainerIdentity(ctx context.Context, containerID string, identity string) error {
	output, err := exec.CommandContext(ctx, "docker", "inspect", "--format", `{{.Id}} {{index .Config.Labels "mattercodex.dev/postgres-test-run"}}`, containerID).Output()
	if err != nil {
		return fmt.Errorf("docker mode не подтвердил exact container identity")
	}
	fields := strings.Fields(string(output))
	if len(fields) != 2 || fields[0] != containerID || fields[1] != identity {
		return fmt.Errorf("docker mode обнаружил replacement container")
	}
	return nil
}

func parseLoopbackDockerPort(raw string) (int, error) {
	lines := strings.Fields(strings.TrimSpace(raw))
	if len(lines) != 1 {
		return 0, fmt.Errorf("docker mode получил неоднозначный port mapping")
	}
	host, portRaw, err := net.SplitHostPort(lines[0])
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return 0, fmt.Errorf("docker mode получил не-loopback port mapping")
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("docker mode получил недопустимый port mapping")
	}
	return port, nil
}

func startKubernetesPostgres(ctx context.Context, major string) (*controllerPostgresHarness, error) {
	namespace := strings.TrimSpace(os.Getenv("MATTERCODEX_TEST_POSTGRES_K8S_NAMESPACE"))
	if namespace == "" {
		return nil, fmt.Errorf("kubernetes mode требует явно заданный disposable namespace")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kubernetes mode доступен только remote agent внутри кластера")
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("kubernetes mode не создал client")
	}
	namespaceObject, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil || namespaceObject.Labels[postgresDisposableNamespaceLabel] != "true" {
		return nil, fmt.Errorf("kubernetes namespace не имеет server-owned disposable admission")
	}
	identity, err := randomControllerIdentity()
	if err != nil {
		return nil, fmt.Errorf("kubernetes mode не создал run identity")
	}
	image, err := postgresControllerImage(major)
	if err != nil {
		return nil, err
	}
	if err := validatePostgresControllerImage(major, image); err != nil {
		return nil, err
	}
	pod, err := client.CoreV1().Pods(namespace).Create(ctx, kubernetesPostgresPod(identity, image), metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("kubernetes mode не создал disposable Pod")
	}
	harness := &controllerPostgresHarness{}
	var portForward *exec.Cmd
	var service *corev1.Service
	harness.close = func(closeCtx context.Context) error {
		if portForward != nil && portForward.Process != nil {
			_ = portForward.Process.Kill()
			_, _ = portForward.Process.Wait()
		}
		if service != nil {
			currentService, err := client.CoreV1().Services(namespace).Get(closeCtx, service.Name, metav1.GetOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("kubernetes mode не прочитал exact Service перед cleanup")
			}
			if err == nil {
				if currentService.UID != service.UID || currentService.Labels[postgresControllerLabel] != identity {
					return fmt.Errorf("kubernetes mode обнаружил replacement Service")
				}
				serviceUID := types.UID(service.UID)
				if err := client.CoreV1().Services(namespace).Delete(closeCtx, service.Name, metav1.DeleteOptions{
					Preconditions: &metav1.Preconditions{UID: &serviceUID},
				}); err != nil && !apierrors.IsNotFound(err) {
					return fmt.Errorf("kubernetes mode не удалил exact UID Service")
				}
				if err := waitKubernetesServiceDeleted(closeCtx, client, namespace, service.Name, service.UID); err != nil {
					return err
				}
			}
		}
		current, err := client.CoreV1().Pods(namespace).Get(closeCtx, pod.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("kubernetes mode не прочитал exact Pod перед cleanup")
		}
		if current.UID != pod.UID || current.Labels[postgresControllerLabel] != identity {
			return fmt.Errorf("kubernetes mode обнаружил replacement Pod")
		}
		uid := types.UID(pod.UID)
		policy := metav1.DeletePropagationBackground
		if err := client.CoreV1().Pods(namespace).Delete(closeCtx, pod.Name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &policy,
		}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("kubernetes mode не удалил exact UID Pod")
		}
		return waitKubernetesPodDeleted(closeCtx, client, namespace, pod.Name, pod.UID)
	}
	cleanupOnError := func(startErr error) (*controllerPostgresHarness, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		cleanupErr := harness.Close(cleanupCtx)
		cancel()
		return nil, errors.Join(startErr, cleanupErr)
	}
	service, err = client.CoreV1().Services(namespace).Create(ctx, kubernetesPostgresService(identity), metav1.CreateOptions{})
	if err != nil {
		return cleanupOnError(fmt.Errorf("kubernetes mode не создал disposable Service"))
	}
	if err := waitKubernetesPodReady(ctx, client, namespace, pod.Name, pod.UID, identity); err != nil {
		return cleanupOnError(err)
	}
	port, err := reserveLoopbackPort()
	if err != nil {
		return cleanupOnError(err)
	}
	var portForwardOutput bytes.Buffer
	portForward = exec.CommandContext(ctx, "kubectl", "--namespace", namespace, "port-forward", "service/"+service.Name,
		fmt.Sprintf("127.0.0.1:%d:5432", port))
	portForward.Stdout = &portForwardOutput
	portForward.Stderr = &portForwardOutput
	if err := portForward.Start(); err != nil {
		return cleanupOnError(fmt.Errorf("kubernetes mode не запустил loopback port-forward"))
	}
	if err := waitTCP(ctx, net.JoinHostPort("127.0.0.1", strconv.Itoa(port))); err != nil {
		return cleanupOnError(fmt.Errorf("kubernetes PostgreSQL port-forward не стал готов"))
	}
	harness.bootstrapDSN = fmt.Sprintf("host=127.0.0.1 port=%d user=mattercodex_test_owner dbname=postgres connect_timeout=5", port)
	return harness, nil
}

func waitKubernetesServiceDeleted(ctx context.Context, client kubernetes.Interface, namespace string, name string, uid types.UID) error {
	return waitForKubernetesDeletion(ctx, func() (types.UID, error) {
		service, err := client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return service.UID, nil
	}, uid, "Service")
}

func waitKubernetesPodDeleted(ctx context.Context, client kubernetes.Interface, namespace string, name string, uid types.UID) error {
	return waitForKubernetesDeletion(ctx, func() (types.UID, error) {
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return pod.UID, nil
	}, uid, "Pod")
}

func waitForKubernetesDeletion(ctx context.Context, readUID func() (types.UID, error), expectedUID types.UID, resourceKind string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		uid, err := readUID()
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("kubernetes mode не подтвердил удаление exact %s", resourceKind)
		}
		if uid != expectedUID {
			return fmt.Errorf("kubernetes mode обнаружил replacement %s после cleanup", resourceKind)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("kubernetes mode не подтвердил удаление exact %s", resourceKind)
		case <-ticker.C:
		}
	}
}

func kubernetesPostgresService(identity string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "mc-postgres-test-", Labels: map[string]string{postgresControllerLabel: identity}},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{postgresControllerLabel: identity},
			Ports:    []corev1.ServicePort{{Name: "postgres", Port: postgresContainerPort, TargetPort: intstr.FromInt32(postgresContainerPort), Protocol: corev1.ProtocolTCP}},
		},
	}
}

func kubernetesPostgresPod(identity string, image string) *corev1.Pod {
	falseValue := false
	trueValue := true
	runAsNonRoot := true
	userID := int64(999)
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "mc-postgres-test-", Labels: map[string]string{postgresControllerLabel: identity}},
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: &falseValue,
			RestartPolicy:                corev1.RestartPolicyNever,
			SecurityContext:              &corev1.PodSecurityContext{RunAsNonRoot: &runAsNonRoot, RunAsUser: &userID, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Containers: []corev1.Container{{
				Name: "postgres", Image: image, ImagePullPolicy: corev1.PullIfNotPresent,
				Env:   []corev1.EnvVar{{Name: "POSTGRES_HOST_AUTH_METHOD", Value: "trust"}, {Name: "POSTGRES_USER", Value: "mattercodex_test_owner"}, {Name: "POSTGRES_DB", Value: "postgres"}},
				Ports: []corev1.ContainerPort{{Name: "postgres", ContainerPort: postgresContainerPort, Protocol: corev1.ProtocolTCP}},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("2Gi")},
				},
				SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &falseValue, ReadOnlyRootFilesystem: &falseValue, RunAsNonRoot: &trueValue, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
			}},
		},
	}
}

func waitKubernetesPodReady(ctx context.Context, client kubernetes.Interface, namespace string, name string, uid types.UID, identity string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			if pod.UID != uid || pod.Labels[postgresControllerLabel] != identity {
				return fmt.Errorf("kubernetes mode обнаружил replacement Pod")
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return nil
				}
			}
			if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
				return fmt.Errorf("kubernetes PostgreSQL Pod завершился до ready")
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("kubernetes PostgreSQL Pod не стал ready")
		case <-ticker.C:
		}
	}
}

func reserveLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("loopback port не зарезервирован")
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("loopback port не освобождён")
	}
	return port, nil
}

func waitTCP(ctx context.Context, address string) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		connection, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
