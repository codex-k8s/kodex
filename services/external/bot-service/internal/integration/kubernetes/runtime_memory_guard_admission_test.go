//go:build kubeadmission

package kubernetes

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type renderedAdmissionObjects struct {
	priorityClass  *schedulingv1.PriorityClass
	quota          *corev1.ResourceQuota
	serviceAccount *corev1.ServiceAccount
	role           *rbacv1.Role
	roleBinding    *rbacv1.RoleBinding
}

func TestAgentMemoryGuardRealKubernetesAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	namespace := strings.TrimSpace(os.Getenv("MATTERCODEX_ADMISSION_NAMESPACE"))
	renderDirectory := strings.TrimSpace(os.Getenv("MATTERCODEX_ADMISSION_RENDER_DIR"))
	if namespace == "" || renderDirectory == "" {
		t.Fatal("MATTERCODEX_ADMISSION_NAMESPACE и MATTERCODEX_ADMISSION_RENDER_DIR обязательны для real admission contour")
	}
	objects := loadRenderedAdmissionObjects(t, renderDirectory)
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatalf("BuildConfigFromFlags() error = %v", err)
	}
	config.Timeout = 10 * time.Second
	admin, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig(admin) error = %v", err)
	}

	if _, err := admin.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create disposable namespace: %v", err)
	}
	objects.quota.Namespace = namespace
	objects.serviceAccount.Namespace = namespace
	objects.role.Namespace = namespace
	objects.roleBinding.Namespace = namespace
	if _, err := admin.SchedulingV1().PriorityClasses().Create(ctx, objects.priorityClass, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create rendered PriorityClass: %v", err)
	}
	createdQuota, err := admin.CoreV1().ResourceQuotas(namespace).Create(ctx, objects.quota, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create rendered ResourceQuota: %v", err)
	}
	createdQuota.Status.Hard = createdQuota.Spec.Hard.DeepCopy()
	createdQuota.Status.Used = corev1.ResourceList{
		corev1.ResourceRequestsMemory: resource.MustParse("0"),
		corev1.ResourceLimitsMemory:   resource.MustParse("0"),
	}
	if _, err := admin.CoreV1().ResourceQuotas(namespace).UpdateStatus(ctx, createdQuota, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("initialize observable ResourceQuota status: %v", err)
	}
	if _, err := admin.CoreV1().ServiceAccounts(namespace).Create(ctx, objects.serviceAccount, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create rendered ServiceAccount: %v", err)
	}
	if _, err := admin.RbacV1().Roles(namespace).Create(ctx, objects.role, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create rendered Role: %v", err)
	}
	if _, err := admin.RbacV1().RoleBindings(namespace).Create(ctx, objects.roleBinding, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create rendered RoleBinding: %v", err)
	}

	untrusted := untrustedAdmissionClient(t, ctx, admin, config, namespace, objects.serviceAccount.Name)
	assertRenderedAgentRBACBoundary(t, ctx, untrusted, namespace, objects)
	waitForQuotaAdmission(t, ctx, admin, namespace, objects)

	if _, err := admin.CoreV1().Pods(namespace).Create(ctx, admissionPod("initial", namespace, objects.priorityClass.Name, objects.serviceAccount.Name, "1Gi"), metav1.CreateOptions{}); err != nil {
		t.Fatalf("create initial admitted Pod: %v", err)
	}
	waitForObservedQuota(t, ctx, admin, namespace, objects.quota.Name, "1Gi")

	start := make(chan struct{})
	errorsByPod := make([]error, 3)
	var wait sync.WaitGroup
	for index := range errorsByPod {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByPod[index] = admin.CoreV1().Pods(namespace).Create(ctx, admissionPod(fmt.Sprintf("concurrent-%d", index), namespace, objects.priorityClass.Name, objects.serviceAccount.Name, "1Gi"), metav1.CreateOptions{})
		}(index)
	}
	close(start)
	wait.Wait()
	admitted := 0
	forbidden := 0
	for _, createErr := range errorsByPod {
		switch {
		case createErr == nil:
			admitted++
		case isExceededQuotaForbidden(createErr, objects.quota.Name):
			forbidden++
		default:
			t.Fatalf("concurrent admission returned unexpected error: %v", createErr)
		}
	}
	if admitted != 2 || forbidden != 1 {
		t.Fatalf("concurrent boundary admitted=%d forbidden=%d, want 2/1", admitted, forbidden)
	}
	waitForObservedQuota(t, ctx, admin, namespace, objects.quota.Name, "3Gi")

	_, err = admin.CoreV1().Pods(namespace).Create(ctx, admissionPod("over-budget", namespace, objects.priorityClass.Name, objects.serviceAccount.Name, "1Gi"), metav1.CreateOptions{})
	if !isExceededQuotaForbidden(err, objects.quota.Name) {
		t.Fatalf("next Pod error = %v, want Forbidden/exceeded quota", err)
	}
}

func loadRenderedAdmissionObjects(t *testing.T, renderDirectory string) renderedAdmissionObjects {
	t.Helper()
	result := renderedAdmissionObjects{}
	for _, name := range []string{"15-runtime-limits.yaml", "20-rbac.yaml"} {
		file, err := os.Open(filepath.Join(renderDirectory, name))
		if err != nil {
			t.Fatalf("open rendered %s: %v", name, err)
		}
		decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
		for {
			var raw map[string]any
			err := decoder.Decode(&raw)
			if err == io.EOF {
				break
			}
			if err != nil {
				_ = file.Close()
				t.Fatalf("decode rendered %s: %v", name, err)
			}
			if len(raw) == 0 {
				continue
			}
			kind, _ := raw["kind"].(string)
			metadata, _ := raw["metadata"].(map[string]any)
			objectName, _ := metadata["name"].(string)
			switch kind + "/" + objectName {
			case "PriorityClass/" + os.Getenv("MATTERCODEX_AGENT_WORKLOAD_PRIORITY_CLASS"):
				result.priorityClass = &schedulingv1.PriorityClass{}
				convertRenderedObject(t, raw, result.priorityClass)
			case "ResourceQuota/matter-codex-agent-memory-quota":
				result.quota = &corev1.ResourceQuota{}
				convertRenderedObject(t, raw, result.quota)
			case "ServiceAccount/" + os.Getenv("MATTERCODEX_AGENT_RUNNER_SERVICE_ACCOUNT"):
				result.serviceAccount = &corev1.ServiceAccount{}
				convertRenderedObject(t, raw, result.serviceAccount)
			case "Role/matter-codex-agent-runner-readonly":
				result.role = &rbacv1.Role{}
				convertRenderedObject(t, raw, result.role)
			case "RoleBinding/matter-codex-agent-runner-readonly":
				result.roleBinding = &rbacv1.RoleBinding{}
				convertRenderedObject(t, raw, result.roleBinding)
			case "ClusterRoleBinding/matter-codex-agent-runner-cluster-admin":
				t.Fatal("rendered RBAC still contains legacy cluster-admin binding")
			}
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close rendered %s: %v", name, err)
		}
	}
	if result.priorityClass == nil || result.quota == nil || result.serviceAccount == nil || result.role == nil || result.roleBinding == nil {
		t.Fatalf("rendered admission objects are incomplete: priority=%t quota=%t serviceAccount=%t role=%t binding=%t", result.priorityClass != nil, result.quota != nil, result.serviceAccount != nil, result.role != nil, result.roleBinding != nil)
	}
	return result
}

func convertRenderedObject(t *testing.T, raw map[string]any, destination any) {
	t.Helper()
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, destination); err != nil {
		t.Fatalf("convert rendered object: %v", err)
	}
}

func untrustedAdmissionClient(t *testing.T, ctx context.Context, admin kubernetes.Interface, config *rest.Config, namespace string, serviceAccount string) kubernetes.Interface {
	t.Helper()
	expirationSeconds := int64(600)
	token, err := admin.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, serviceAccount, &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{Audiences: []string{"https://kubernetes.default.svc"}, ExpirationSeconds: &expirationSeconds},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("create bounded ServiceAccount token: %v", err)
	}
	untrustedConfig := rest.CopyConfig(config)
	untrustedConfig.BearerToken = token.Status.Token
	untrustedConfig.BearerTokenFile = ""
	untrustedConfig.Username = ""
	untrustedConfig.Password = ""
	untrustedConfig.CertFile = ""
	untrustedConfig.KeyFile = ""
	untrustedConfig.CertData = nil
	untrustedConfig.KeyData = nil
	untrustedConfig.AuthProvider = nil
	untrustedConfig.ExecProvider = nil
	client, err := kubernetes.NewForConfig(untrustedConfig)
	if err != nil {
		t.Fatalf("kubernetes.NewForConfig(untrusted) error = %v", err)
	}
	return client
}

func assertRenderedAgentRBACBoundary(t *testing.T, ctx context.Context, client kubernetes.Interface, namespace string, objects renderedAdmissionObjects) {
	t.Helper()
	if _, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{}); err != nil {
		t.Fatalf("read-only agent cannot list Pods: %v", err)
	}
	_, podCreateErr := client.CoreV1().Pods(namespace).Create(ctx, admissionPod("rbac-pod", namespace, "", objects.serviceAccount.Name, "1Gi"), metav1.CreateOptions{})
	assertForbidden(t, podCreateErr)
	_, jobErr := client.BatchV1().Jobs(namespace).Create(ctx, &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "rbac-job", Namespace: namespace},
		Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{{Name: "runner", Image: "registry.invalid/never"}},
		}}},
	}, metav1.CreateOptions{})
	assertForbidden(t, jobErr)
	quota := objects.quota.DeepCopy()
	quota.Annotations = map[string]string{"attempt": "weaken"}
	_, quotaUpdateErr := client.CoreV1().ResourceQuotas(namespace).Update(ctx, quota, metav1.UpdateOptions{})
	assertForbidden(t, quotaUpdateErr)
	assertForbidden(t, client.CoreV1().ResourceQuotas(namespace).Delete(ctx, objects.quota.Name, metav1.DeleteOptions{}))
	priority := objects.priorityClass.DeepCopy()
	priority.Value++
	_, priorityUpdateErr := client.SchedulingV1().PriorityClasses().Update(ctx, priority, metav1.UpdateOptions{})
	assertForbidden(t, priorityUpdateErr)
	assertForbidden(t, client.SchedulingV1().PriorityClasses().Delete(ctx, objects.priorityClass.Name, metav1.DeleteOptions{}))
}

func assertForbidden(t *testing.T, err error) {
	t.Helper()
	if !apierrors.IsForbidden(err) {
		t.Fatalf("Kubernetes operation error = %v, want Forbidden", err)
	}
}

func waitForQuotaAdmission(t *testing.T, ctx context.Context, admin kubernetes.Interface, namespace string, objects renderedAdmissionObjects) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for attempt := 0; time.Now().Before(deadline); attempt++ {
		probeName := fmt.Sprintf("quota-probe-%d", attempt)
		_, err := admin.CoreV1().Pods(namespace).Create(ctx, admissionPod(probeName, namespace, objects.priorityClass.Name, objects.serviceAccount.Name, "4Gi"), metav1.CreateOptions{})
		if isExceededQuotaForbidden(err, objects.quota.Name) {
			waitForObservedQuota(t, ctx, admin, namespace, objects.quota.Name, "0")
			return
		}
		if err != nil && !apierrors.IsNotFound(err) {
			t.Fatalf("quota admission readiness probe error = %v", err)
		}
		if err == nil {
			if deleteErr := admin.CoreV1().Pods(namespace).Delete(ctx, probeName, metav1.DeleteOptions{}); deleteErr != nil {
				t.Fatalf("delete disposable quota probe: %v", deleteErr)
			}
		}
		resetObservedQuota(t, ctx, admin, namespace, objects.quota.Name)
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("ResourceQuota admission did not observe the rendered quota before deadline")
}

func resetObservedQuota(t *testing.T, ctx context.Context, admin kubernetes.Interface, namespace string, name string) {
	t.Helper()
	quota, err := admin.CoreV1().ResourceQuotas(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ResourceQuota for reset: %v", err)
	}
	quota.Status.Hard = quota.Spec.Hard.DeepCopy()
	quota.Status.Used = corev1.ResourceList{
		corev1.ResourceRequestsMemory: resource.MustParse("0"),
		corev1.ResourceLimitsMemory:   resource.MustParse("0"),
	}
	if _, err := admin.CoreV1().ResourceQuotas(namespace).UpdateStatus(ctx, quota, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("reset ResourceQuota status: %v", err)
	}
}

func waitForObservedQuota(t *testing.T, ctx context.Context, admin kubernetes.Interface, namespace string, name string, expected string) {
	t.Helper()
	want := resource.MustParse(expected)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		quota, err := admin.CoreV1().ResourceQuotas(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get observed ResourceQuota: %v", err)
		}
		requests := quota.Status.Used[corev1.ResourceRequestsMemory]
		limits := quota.Status.Used[corev1.ResourceLimitsMemory]
		if requests.Cmp(want) == 0 && limits.Cmp(want) == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("ResourceQuota status.used did not reach requests.memory=limits.memory=%s", expected)
}

func admissionPod(name string, namespace string, priorityClass string, serviceAccount string, memory string) *corev1.Pod {
	memoryQuantity := resource.MustParse(memory)
	automount := false
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.PodSpec{
			PriorityClassName:            priorityClass,
			ServiceAccountName:           serviceAccount,
			AutomountServiceAccountToken: &automount,
			RestartPolicy:                corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:  "runner",
				Image: "registry.invalid/never",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceMemory: memoryQuantity},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: memoryQuantity},
				},
			}},
		},
	}
}

func isExceededQuotaForbidden(err error, quotaName string) bool {
	return apierrors.IsForbidden(err) && strings.Contains(strings.ToLower(err.Error()), "exceeded quota") && strings.Contains(err.Error(), quotaName)
}
