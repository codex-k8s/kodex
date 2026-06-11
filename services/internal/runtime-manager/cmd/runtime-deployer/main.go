package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

const (
	fieldManager      = "kodex-runtime-deployer"
	maxManifestFiles  = 128
	maxManifestBytes  = 8 * 1024 * 1024
	rolloutTimeout    = 10 * time.Minute
	rolloutPoll       = 2 * time.Second
	defaultResultText = "runtime deploy completed"
)

type config struct {
	bundlePath      string
	bundleDigest    string
	targetNamespace string
	serviceKey      string
	expectedImage   string
	rolloutTargets  []string
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "runtime_deploy_failed: %s\n", safeDiagnostic(err.Error()))
		os.Exit(1)
	}
	fmt.Println(defaultResultText)
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "apply" {
		return errors.New("unsupported deploy command")
	}
	cfg, err := parseConfig(args[1:])
	if err != nil {
		return err
	}
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return errors.New("kubernetes in-cluster config unavailable")
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return errors.New("kubernetes dynamic client unavailable")
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return errors.New("kubernetes discovery client unavailable")
	}
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return errors.New("kubernetes typed client unavailable")
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient))
	objects, err := readObjects(cfg.bundlePath)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return errors.New("deploy manifest bundle is empty")
	}
	for index := range objects {
		if err := applyObject(ctx, dynamicClient, mapper, cfg.targetNamespace, &objects[index]); err != nil {
			return err
		}
	}
	return waitRollouts(ctx, kubeClient, cfg.targetNamespace, cfg.rolloutTargets)
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("apply", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	cfg := config{}
	var rolloutTargets multiValue
	flags.StringVar(&cfg.bundlePath, "bundle-path", "", "")
	flags.StringVar(&cfg.bundleDigest, "bundle-digest", "", "")
	flags.StringVar(&cfg.targetNamespace, "target-namespace", "", "")
	flags.StringVar(&cfg.serviceKey, "service-key", "", "")
	flags.StringVar(&cfg.expectedImage, "expected-image", "", "")
	flags.Var(&rolloutTargets, "rollout-target", "")
	if err := flags.Parse(args); err != nil {
		return config{}, errors.New("invalid deploy arguments")
	}
	cfg.rolloutTargets = rolloutTargets
	if !safePath(cfg.bundlePath) ||
		!safeDigest(cfg.bundleDigest) ||
		len(validation.IsDNS1123Label(cfg.targetNamespace)) > 0 ||
		!safeLabel(cfg.serviceKey) ||
		!safeImageRef(cfg.expectedImage) ||
		len(cfg.rolloutTargets) == 0 {
		return config{}, errors.New("invalid deploy input")
	}
	for _, target := range cfg.rolloutTargets {
		if _, _, _, err := parseRolloutTarget(target, cfg.targetNamespace); err != nil {
			return config{}, err
		}
	}
	return cfg, nil
}

type multiValue []string

func (m *multiValue) String() string { return strings.Join(*m, ",") }

func (m *multiValue) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("empty rollout target")
	}
	*m = append(*m, trimmed)
	return nil
}

func readObjects(root string) ([]unstructured.Unstructured, error) {
	objects := []unstructured.Unstructured{}
	totalBytes := int64(0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return errors.New("deploy manifest bundle cannot be read")
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		if len(objects) >= maxManifestFiles {
			return errors.New("deploy manifest bundle has too many files")
		}
		info, err := entry.Info()
		if err != nil {
			return errors.New("deploy manifest file cannot be read")
		}
		totalBytes += info.Size()
		if totalBytes > maxManifestBytes {
			return errors.New("deploy manifest bundle is too large")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return errors.New("deploy manifest file cannot be read")
		}
		decoded, err := decodeManifestObjects(raw)
		if err != nil {
			return err
		}
		objects = append(objects, decoded...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return objects, nil
}

func decodeManifestObjects(raw []byte) ([]unstructured.Unstructured, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	result := []unstructured.Unstructured{}
	for {
		var document map[string]any
		if err := decoder.Decode(&document); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, errors.New("deploy manifest yaml is invalid")
		}
		if len(document) == 0 {
			continue
		}
		object := unstructured.Unstructured{Object: document}
		if object.GetKind() == "" || object.GetName() == "" {
			return nil, errors.New("deploy manifest object is incomplete")
		}
		result = append(result, object)
	}
	return result, nil
}

type restMapping interface {
	RESTMapping(gk schema.GroupKind, versions ...string) (*apiMeta.RESTMapping, error)
}

func applyObject(ctx context.Context, client dynamic.Interface, mapper restMapping, targetNamespace string, object *unstructured.Unstructured) error {
	gvk := object.GroupVersionKind()
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return errors.New("deploy manifest mapping unavailable")
	}
	namespace := object.GetNamespace()
	if mapping.Scope.Name() == apiMeta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = targetNamespace
			object.SetNamespace(namespace)
		}
		if namespace != targetNamespace {
			return errors.New("deploy manifest namespace mismatch")
		}
	}
	options := metav1.ApplyOptions{FieldManager: fieldManager, Force: true}
	resource := client.Resource(mapping.Resource)
	var applyErr error
	if namespace != "" {
		_, applyErr = resource.Namespace(namespace).Apply(ctx, object.GetName(), object, options)
	} else {
		_, applyErr = resource.Apply(ctx, object.GetName(), object, options)
	}
	if applyErr != nil {
		if apierrors.IsForbidden(applyErr) {
			return errors.New("deploy apply access denied")
		}
		return errors.New("deploy apply failed")
	}
	return nil
}

func waitRollouts(ctx context.Context, client kubernetes.Interface, defaultNamespace string, targets []string) error {
	waitCtx, cancel := context.WithTimeout(ctx, rolloutTimeout)
	defer cancel()
	for _, target := range targets {
		kind, namespace, name, err := parseRolloutTarget(target, defaultNamespace)
		if err != nil {
			return err
		}
		if kind != "deployment" {
			return errors.New("unsupported rollout target")
		}
		if err := waitDeployment(waitCtx, client, namespace, name); err != nil {
			return err
		}
	}
	return nil
}

func waitDeployment(ctx context.Context, client kubernetes.Interface, namespace string, name string) error {
	ticker := time.NewTicker(rolloutPoll)
	defer ticker.Stop()
	for {
		deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsForbidden(err) {
				return errors.New("deploy rollout access denied")
			}
			return errors.New("deploy rollout status unavailable")
		}
		if deploymentReady(deployment) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("deploy rollout timed out")
		case <-ticker.C:
		}
	}
}

func deploymentReady(deployment *appsv1.Deployment) bool {
	if deployment == nil || deployment.Generation != deployment.Status.ObservedGeneration {
		return false
	}
	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}
	if deployment.Status.UpdatedReplicas < replicas {
		return false
	}
	if deployment.Status.AvailableReplicas < replicas {
		return false
	}
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func parseRolloutTarget(value string, defaultNamespace string) (string, string, string, error) {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 3 {
		return "", "", "", errors.New("invalid rollout target")
	}
	kind := strings.ToLower(strings.TrimSpace(parts[0]))
	namespace := strings.TrimSpace(parts[1])
	name := strings.TrimSpace(parts[2])
	if namespace == "" {
		namespace = defaultNamespace
	}
	if !safeLabel(kind) || len(validation.IsDNS1123Label(namespace)) > 0 || len(validation.IsDNS1123Subdomain(name)) > 0 {
		return "", "", "", errors.New("invalid rollout target")
	}
	return kind, namespace, name, nil
}

func safePath(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && strings.HasPrefix(trimmed, "/") && !strings.ContainsAny(trimmed, "\x00\r\n\t")
}

func safeDigest(value string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if !strings.HasPrefix(trimmed, "sha256:") || len(trimmed) != len("sha256:")+64 {
		return false
	}
	for _, char := range strings.TrimPrefix(trimmed, "sha256:") {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}

func safeLabel(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > 128 {
		return false
	}
	for _, char := range trimmed {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' || char == '/' {
			continue
		}
		return false
	}
	return true
}

func safeImageRef(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= 512 && !strings.ContainsAny(trimmed, " \t\r\n")
}

func safeDiagnostic(value string) string {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	for _, marker := range []string{"token", "secret", "authorization", "bearer", "kubeconfig", "provider payload", "webhook body", "raw payload", "-----begin"} {
		if strings.Contains(lower, marker) {
			return "runtime deploy failed"
		}
	}
	if len(trimmed) > 512 {
		return trimmed[:512]
	}
	return trimmed
}
