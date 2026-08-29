// Package workload материализует immutable RuntimeRevision в execution-scoped Kubernetes Pod.
package workload

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

const (
	managedLabel                = "runtime.kodex.dev/managed"
	modeLabel                   = "runtime.kodex.dev/mode"
	revisionAnnotation          = "runtime.kodex.dev/revision-digest"
	configAnnotation            = "runtime.kodex.dev/config-digest"
	environmentAnnotation       = "runtime.kodex.dev/environment-digest"
	warmCompatibilityAnnotation = "runtime.kodex.dev/warm-compatibility-digest"
	controllerAnnotation        = "runtime.kodex.dev/controller-pod-uid"
	podAnnotation               = "runtime.kodex.dev/pod-name"
	inputKey                    = "runtime.json"
	ticketKey                   = "token"
)

type Config struct {
	Environment, Namespace, ControllerPodUID, ControllerPodIP              string
	CallbackTLSServerName, CallbackClientCASecret, CallbackClientTLSSecret string
	StorageClass, SessionPVCSize, RunnerServiceAccount                     string
	ProviderHTTPSProxy                                                     string
	PromotedRoleImageRepository, DefaultRoleImageReference                 string
	RoleRuntimeContractSHA256                                              string
	RoleRuntimeContractRevision                                            uint64
	TurnCPUMilli, TurnMemoryBytes                                          int64
}

// ProviderSecretBinding остаётся только внутри trusted runtime-controller и
// не сериализуется в runtime.json, доступный role image.
type ProviderSecretBinding struct {
	Name, UID, ResourceVersion, ContentSHA256 string
}

type Manager struct {
	client     kubernetes.Interface
	config     Config
	pvcRequest resource.Quantity
}

func InCluster(config Config) (*Manager, error) {
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, errors.New("load Kubernetes runtime configuration")
	}
	restConfig.Timeout = 5 * time.Second
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, errors.New("create Kubernetes runtime client")
	}
	return New(client, config)
}

func New(client kubernetes.Interface, config Config) (*Manager, error) {
	pvcRequest, err := resource.ParseQuantity(config.SessionPVCSize)
	if client == nil || err != nil || pvcRequest.Sign() <= 0 || config.Namespace == "" ||
		config.ControllerPodUID == "" || net.ParseIP(config.ControllerPodIP) == nil ||
		config.CallbackTLSServerName == "" || config.CallbackClientCASecret == "" ||
		config.CallbackClientTLSSecret == "" ||
		config.ProviderHTTPSProxy == "" ||
		(config.StorageClass != "" && !validDNSSubdomain(config.StorageClass)) ||
		config.RunnerServiceAccount == "" ||
		config.PromotedRoleImageRepository == "" || config.RoleRuntimeContractRevision == 0 ||
		!validPinnedImageReference(config.DefaultRoleImageReference) ||
		len(config.RoleRuntimeContractSHA256) != sha256.Size*2 || config.TurnCPUMilli < 100 || config.TurnMemoryBytes < 128<<20 {
		return nil, errors.New("Kubernetes runtime manager configuration is invalid")
	}
	return &Manager{client: client, config: config, pvcRequest: pvcRequest}, nil
}

func (manager *Manager) Check(ctx context.Context) error {
	if _, err := manager.client.CoreV1().Pods(manager.config.Namespace).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("Kubernetes runtime namespace observation failed: %w", err)
	}
	return nil
}

// AllowsLastKnownGoodObservation допускает короткое LKG-окно только для
// временной недоступности transport/API server. Ошибка авторизации, целостности
// TLS, неизвестная классификация и повреждённый ответ закрывают readiness сразу.
func AllowsLastKnownGoodObservation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || apierrors.IsTimeout(err) ||
		apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) ||
		apierrors.IsTooManyRequests(err) || apierrors.IsInternalError(err) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError) && (networkError.Timeout() || networkError.Temporary())
}

func (manager *Manager) RunAsLeader(ctx context.Context, run func(context.Context) error) error {
	if run == nil {
		return errors.New("runtime leader callback is required")
	}
	electionContext, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{LeaseMeta: metav1.ObjectMeta{Name: "runtime-controller-leader", Namespace: manager.config.Namespace},
			Client: manager.client.CoordinationV1(), LockConfig: resourcelock.ResourceLockConfig{Identity: manager.config.ControllerPodUID}},
		LeaseDuration: 15 * time.Second, RenewDeadline: 10 * time.Second, RetryPeriod: 2 * time.Second, ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{OnStartedLeading: func(leaderContext context.Context) {
			result <- run(leaderContext)
			cancel()
		}, OnStoppedLeading: func() {}},
	})
	if err != nil {
		return errors.New("configure runtime leader election")
	}
	elector.Run(electionContext)
	select {
	case err := <-result:
		return err
	default:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("runtime leadership lost")
	}
}

func (manager *Manager) CleanupStaleTurns(ctx context.Context) error {
	selector := labels.Set{managedLabel: "true", modeLabel: "turn"}.AsSelector().String()
	pods, err := manager.client.CoreV1().Pods(manager.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256})
	if err != nil {
		return errors.New("list retained runtime turn pods")
	}
	var result error
	for index := range pods.Items {
		pod := &pods.Items[index]
		if pod.Annotations[controllerAnnotation] == manager.config.ControllerPodUID {
			continue
		}
		if err := manager.client.CoreV1().Pods(manager.config.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: int64Pointer(0)}); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete stale runtime turn pod"))
		}
	}
	return result
}

func (manager *Manager) BuildTurnInput(execution *controlplanev1.ClaimedExecution) (runtimecontract.RunnerInput, ProviderSecretBinding, error) {
	if execution == nil || execution.GetRun() == nil || execution.GetNode() == nil || execution.GetRevision() == nil ||
		execution.GetRevision().GetRuntime() == nil || execution.GetLease() == nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, errors.New("claimed execution is incomplete")
	}
	revision := execution.GetRevision()
	input := manager.baseInput(revision, runtimecontract.RunnerModeTurn)
	input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef = execution.GetRun().GetRef(), execution.GetNode().GetRef(), revision.GetSessionRef(), revision.GetTurnRef()
	input.ProjectRef = execution.GetRun().GetProjectRef()
	input.AgentRef, input.Attempt = revision.GetAgentRef(), revision.GetAttempt()
	input.LeaseRef, input.LeaseFence, input.LeaseGeneration = execution.GetLease().GetRef(), execution.GetLease().GetFence(), execution.GetLease().GetGeneration()
	input.Task, input.BoundedInput = execution.GetTask(), map[string]any{}
	if revision.GetBoundedInput() != nil {
		input.BoundedInput = revision.GetBoundedInput().AsMap()
	}
	if context := revision.GetAssistantContext(); context != nil {
		input.AssistantContext = &runtimecontract.RunnerAssistantContext{Route: context.GetRoute(), EntityKind: context.GetEntityKind(),
			EntityRef: context.GetEntityRef(), EntityName: context.GetEntityName(), EntityVersion: context.EntityVersion}
		for _, operation := range context.GetAllowedOperations() {
			if operation != controlplanev1.AssistantPlanOperation_TYPE_UNSPECIFIED {
				input.AssistantContext.AllowedOperations = append(input.AssistantContext.AllowedOperations, strings.TrimPrefix(operation.String(), "TYPE_"))
			}
		}
	}
	manager.addCatalog(&input, revision)
	binding, err := providerSecretBinding(revision)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	return input, binding, input.Validate()
}

func (manager *Manager) BuildWarmInput(revision *controlplanev1.RuntimeRevisionSnapshot) (runtimecontract.RunnerInput, ProviderSecretBinding, error) {
	if revision == nil || revision.GetRuntime() == nil || !revision.GetSystemAssistant() {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, errors.New("warm runtime revision is invalid")
	}
	input := manager.baseInput(revision, runtimecontract.RunnerModeWarm)
	input.SessionRef, input.AgentRef = revision.GetSessionRef(), revision.GetAgentRef()
	manager.addCatalog(&input, revision)
	binding, err := providerSecretBinding(revision)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	return input, binding, input.Validate()
}

func (manager *Manager) baseInput(revision *controlplanev1.RuntimeRevisionSnapshot, mode string) runtimecontract.RunnerInput {
	input := runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV6, Mode: mode, WorkloadInstance: manager.config.ControllerPodUID,
		RuntimeRevisionRef: revision.GetRef(), RuntimeRevisionVersion: revision.GetVersion(), RuntimeRevisionDigest: revision.GetRevisionDigest(),
		ImageReference: revision.GetImageReference(), ImageManifestDigest: revision.GetImageManifestDigest(),
		EnvironmentImage: runtimecontract.RuntimeEnvironmentImage{
			ArtifactRef: revision.GetRoleImageArtifactRef(), RecipeRef: revision.GetRoleImageRecipeRef(),
			RecipeGeneration: revision.GetRoleImageRecipeGeneration(), Reference: revision.GetImageReference(), Digest: revision.GetImageManifestDigest(),
		},
		RoleRuntimeContractRevision: revision.GetRoleRuntimeContractRevision(), RoleRuntimeContractSHA256: revision.GetRoleRuntimeContractSha256(),
		SystemAssistant: revision.GetSystemAssistant(), Instructions: revision.GetInstructions(), Provider: revision.GetRuntime().GetProvider(), Model: revision.GetRuntime().GetModel(),
		CodexSessionID:             revision.GetCodexSessionId(),
		ProviderAccountRef:         revision.GetProviderCredential().GetAccountRef(),
		ProviderCredentialRef:      revision.GetProviderCredential().GetCredentialRevisionRef(),
		ProviderCredentialRevision: revision.GetProviderCredential().GetCredentialRevision(),
		ProviderCredentialSHA256:   revision.GetProviderCredential().GetContentSha256(),
		RuntimeConfigRef:           revision.GetRuntimeConfigRef(),
		RuntimeConfigVersion:       revision.GetRuntimeConfigVersion(),
		RuntimeConfigDigest:        revision.GetRuntimeConfigDigest(),
		ProviderPolicyRef:          revision.GetProviderPolicyRef(),
		ProviderPolicyVersion:      revision.GetProviderPolicyVersion(),
		ProviderPolicyDigest:       revision.GetProviderPolicyDigest(),
		ConfigOverlayRef:           revision.GetConfigOverlayRef(),
		ConfigOverlayVersion:       revision.GetConfigOverlayVersion(),
		ConfigOverlayDigest:        revision.GetConfigOverlayDigest(),
		ConfigOverlay:              revision.GetConfigOverlay(),
		RuntimeEnvironmentRef:      revision.GetRuntimeEnvironmentRef(),
		RuntimeEnvironmentVersion:  revision.GetRuntimeEnvironmentVersion(),
		RuntimeEnvironmentDigest:   revision.GetRuntimeEnvironmentDigest(),
		EnvironmentBindingRef:      revision.GetEnvironmentBindingRef(),
		EnvironmentBindingVersion:  revision.GetEnvironmentBindingVersion(),
		EnvironmentBindingDigest:   revision.GetEnvironmentBindingDigest(),
		CodexSandbox:               "read-only", CodexApprovalPolicy: "never",
		CallbackURL: "https://" + net.JoinHostPort(manager.config.ControllerPodIP, "8444"),
		CallbackTLS: runtimecontract.RuntimeTLSBinding{ServerName: manager.config.CallbackTLSServerName,
			CAFile: "/var/run/config/kodex/runtime/callback/ca.crt", CertificateFile: "/var/run/secrets/kodex/runtime/callback-client/tls.crt", PrivateKeyFile: "/var/run/secrets/kodex/runtime/callback-client/tls.key"},
		ExecutionTicketFile: "/var/run/secrets/kodex/runtime/ticket/token",
		ProviderAuthFile:    "/run/secrets/kodex/runtime/provider/auth.json", ProviderAuthSHA256File: "/run/secrets/kodex/runtime/provider/auth.sha256",
		WorkspaceRoot: "/workspace", OutboxRoot: "/workspace/.kodex/outbox", CodexHome: "/workspace/.kodex/state/codex-home",
	}
	for _, item := range revision.GetEnvironmentValues() {
		input.EnvironmentValues = append(input.EnvironmentValues, runtimecontract.RuntimeEnvironmentValue{Name: item.GetName(), Value: item.GetValue()})
	}
	for _, item := range revision.GetSecretProjections() {
		input.SecretProjections = append(input.SecretProjections, runtimecontract.RuntimeSecretProjection{
			Name: item.GetName(), SecretName: item.GetSecretName(), SecretKey: item.GetSecretKey(),
			SecretUID: item.GetSecretUid(), SecretResourceVersion: item.GetSecretResourceVersion(),
			ContentSHA256: item.GetContentSha256(),
		})
	}
	for _, item := range revision.GetEnvironmentTools() {
		input.EnvironmentTools = append(input.EnvironmentTools, runtimecontract.RuntimeEnvironmentTool{
			Name: item.GetName(), Command: item.GetCommand(), Description: item.GetDescription(), UsageHint: item.GetUsageHint(),
		})
	}
	return input
}

func providerSecretBinding(revision *controlplanev1.RuntimeRevisionSnapshot) (ProviderSecretBinding, error) {
	binding := revision.GetProviderCredential()
	if binding == nil || !validDNSLabel(binding.GetSecretName()) || binding.GetSecretUid() == "" ||
		binding.GetSecretResourceVersion() == "" || len(binding.GetContentSha256()) != sha256.Size*2 {
		return ProviderSecretBinding{}, errors.New("provider credential binding is invalid")
	}
	return ProviderSecretBinding{Name: binding.GetSecretName(), UID: binding.GetSecretUid(),
		ResourceVersion: binding.GetSecretResourceVersion(), ContentSHA256: binding.GetContentSha256()}, nil
}

func (manager *Manager) addCatalog(input *runtimecontract.RunnerInput, revision *controlplanev1.RuntimeRevisionSnapshot) {
	for _, capability := range revision.GetCapabilities() {
		input.Capabilities = append(input.Capabilities, capability.GetKey())
		if capability.GetKey() == runtimecontract.ArtifactCapability {
			input.CodexSandbox = "workspace-write"
		}
	}
	for _, message := range revision.GetSessionContext() {
		input.SessionContext = append(input.SessionContext, runtimecontract.RunnerSessionMessage{Role: message.GetRole(), Content: message.GetContent()})
	}
	for _, target := range revision.GetDelegationTargets() {
		input.DelegationTargets = append(input.DelegationTargets, runtimecontract.RunnerDelegationTarget{Ref: target.GetRef(), Name: target.GetName(), Purpose: target.GetPurpose(), RoleDescription: target.GetRoleDescription(), WorkflowStepKey: target.GetWorkflowStepKey(), WorkflowStepName: target.GetWorkflowStepName(), Instructions: target.GetInstructions(), ExpectedResult: target.GetExpectedResult()})
	}
	for _, grant := range revision.GetIntegrationGrants() {
		if !grant.GetEnabled() {
			continue
		}
		input.IntegrationGrants = append(input.IntegrationGrants, runtimecontract.RunnerIntegrationGrant{Ref: grant.GetRef(), ConnectionRef: grant.GetConnectionRef(), DefinitionKey: grant.GetDefinitionKey(), ConnectionName: grant.GetConnectionName(), CapabilityKey: grant.GetCapabilityKey(), CapabilityName: grant.GetCapabilityName(), CapabilityDescription: grant.GetCapabilityDescription(), Risk: grant.GetRisk()})
	}
	input.AttachmentSetRef = revision.GetAttachmentSetRef()
	input.AttachmentSetManifestDigest = revision.GetAttachmentSetManifestDigest()
	input.AttachmentContext = revision.GetAttachmentContext()
	for _, runtimeArtifact := range revision.GetInputArtifacts() {
		artifact := runtimeArtifact.GetArtifact()
		if artifact == nil {
			continue
		}
		input.InputArtifacts = append(input.InputArtifacts, runtimecontract.RunnerInputArtifact{
			Ref: artifact.GetRef(), FileName: artifact.GetFileName(), MediaType: artifact.GetMediaType(),
			Digest: artifact.GetDigest(), SizeBytes: artifact.GetSizeBytes(), Revision: int64(artifact.GetRevision()),
			Version: artifact.GetVersion(), Scope: runtimeArtifact.GetScope(), Position: runtimeArtifact.GetPosition(),
			Source: runnerArtifactSource(artifact.GetSource()),
		})
	}
}

func runnerArtifactSource(source controlplanev1.ArtifactSource) string {
	switch source {
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_CONTROL_CENTER:
		return "CONTROL_CENTER"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_AGENT_RESULT:
		return "AGENT_RESULT"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTEGRATION_RESULT:
		return "INTEGRATION_RESULT"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_KNOWLEDGE_SOURCE:
		return "KNOWLEDGE_SOURCE"
	case controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTERACTION_ATTACHMENT:
		return "INTERACTION_ATTACHMENT"
	default:
		return ""
	}
}

func (manager *Manager) EnsureTurn(ctx context.Context, input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding) error {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil || manager.validateImage(input) != nil {
		return errors.New("runtime turn input is invalid")
	}
	if err := manager.validateProviderSecret(ctx, input, providerBinding); err != nil {
		return err
	}
	environmentSecrets, err := manager.materializeEnvironmentSecrets(ctx, input)
	if err != nil {
		return err
	}
	if err := manager.ensureSessionPVC(ctx, input.SessionRef); err != nil {
		return err
	}
	token, err := newTicket()
	if err != nil {
		return err
	}
	secretName := ticketName(input.LeaseRef)
	podName := turnPodName(input.LeaseRef)
	if err := manager.ensureTicket(ctx, secretName, podName, "turn", input, token, environmentSecrets); err != nil {
		return err
	}
	pod := manager.runtimePod(input, providerBinding, secretName, podName, "turn")
	_, err = manager.client.CoreV1().Pods(manager.config.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().Pods(manager.config.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if getErr != nil || existing.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest || existing.Spec.Containers[0].Image != input.ImageReference {
			return errors.New("existing runtime turn pod conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime turn pod")
	}
	return nil
}

func (manager *Manager) EnsureWarm(ctx context.Context, input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding) (bool, error) {
	if input.Mode != runtimecontract.RunnerModeWarm || input.Validate() != nil || manager.validateImage(input) != nil {
		return false, errors.New("warm runtime input is invalid")
	}
	if err := manager.validateProviderSecret(ctx, input, providerBinding); err != nil {
		return false, err
	}
	environmentSecrets, err := manager.materializeEnvironmentSecrets(ctx, input)
	if err != nil {
		return false, err
	}
	if err := manager.ensureSessionPVC(ctx, input.SessionRef); err != nil {
		return false, err
	}
	const podName = "system-assistant-warm"
	secretName := manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
	compatibilityDigest, _ := runtimecontract.WarmCompatibilityDigest(input)
	existing, err := manager.client.CoreV1().Pods(manager.config.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil && (existing.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest ||
		existing.Annotations[warmCompatibilityAnnotation] != compatibilityDigest ||
		existing.Annotations[controllerAnnotation] != manager.config.ControllerPodUID ||
		runtimePodTerminal(existing)) {
		boundTicket := runtimeInputSecretName(existing)
		if deleteErr := manager.client.CoreV1().Pods(manager.config.Namespace).Delete(ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: int64Pointer(0)}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return false, errors.New("replace stale warm runtime pod")
		}
		if boundTicket != "" {
			if deleteErr := manager.deleteOwnedWarmTicket(ctx, boundTicket); deleteErr != nil {
				return false, deleteErr
			}
		}
		err = apierrors.NewNotFound(corev1.Resource("pods"), podName)
	}
	if apierrors.IsNotFound(err) {
		if ticketErr := manager.removeConflictingWarmTicket(ctx, secretName, input); ticketErr != nil {
			return false, ticketErr
		}
		token, ticketErr := newTicket()
		if ticketErr != nil {
			return false, ticketErr
		}
		if ticketErr = manager.ensureTicket(ctx, secretName, podName, "warm", input, token, environmentSecrets); ticketErr != nil {
			return false, ticketErr
		}
		pod := manager.runtimePod(input, providerBinding, secretName, podName, "warm")
		existing, err = manager.client.CoreV1().Pods(manager.config.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, err = manager.client.CoreV1().Pods(manager.config.Namespace).Get(ctx, podName, metav1.GetOptions{})
		}
		if err != nil {
			return false, errors.New("create warm runtime pod")
		}
	} else if err != nil {
		return false, errors.New("read warm runtime pod")
	}
	if err := manager.cleanupStaleWarmTickets(ctx, secretName); err != nil {
		return false, err
	}
	return podReady(existing), nil
}

func (manager *Manager) deleteOwnedWarmTicket(ctx context.Context, name string) error {
	secret, err := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read stale warm runtime ticket")
	}
	if secret.Labels[managedLabel] != "true" || secret.Labels[modeLabel] != "warm" {
		return errors.New("stale warm runtime ticket ownership is invalid")
	}
	if err := manager.client.CoreV1().Secrets(manager.config.Namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete stale warm runtime ticket")
	}
	return nil
}

func (manager *Manager) cleanupStaleWarmTickets(ctx context.Context, current string) error {
	selector := labels.Set{managedLabel: "true", modeLabel: "warm"}.AsSelector().String()
	secrets, err := manager.client.CoreV1().Secrets(manager.config.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256})
	if err != nil {
		return errors.New("list stale warm runtime tickets")
	}
	var result error
	for index := range secrets.Items {
		if secrets.Items[index].Name == current {
			continue
		}
		if err := manager.client.CoreV1().Secrets(manager.config.Namespace).Delete(ctx, secrets.Items[index].Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete stale warm runtime ticket"))
		}
	}
	return result
}

func runtimeInputSecretName(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "runtime-input" && volume.Secret != nil {
			return volume.Secret.SecretName
		}
	}
	return ""
}

func (manager *Manager) removeConflictingWarmTicket(ctx context.Context, secretName string, input runtimecontract.RunnerInput) error {
	secret, err := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read warm runtime ticket")
	}
	if secret.Annotations[revisionAnnotation] == input.RuntimeRevisionDigest &&
		secret.Annotations[controllerAnnotation] == manager.config.ControllerPodUID {
		bound, decodeErr := runtimecontract.DecodeRunnerInput(secret.Data[inputKey])
		if decodeErr == nil && bound.WorkloadInstance == input.WorkloadInstance &&
			bound.CallbackURL == input.CallbackURL && bound.CallbackTLS == input.CallbackTLS &&
			bound.ExecutionTicketFile == input.ExecutionTicketFile {
			return nil
		}
	}
	if err := manager.client.CoreV1().Secrets(manager.config.Namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("replace stale warm runtime ticket")
	}
	return nil
}

func (manager *Manager) RegisterWarmTurn(ctx context.Context, input runtimecontract.RunnerInput, token string) error {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil || token == "" {
		return errors.New("warm turn registration is invalid")
	}
	return manager.ensureTicket(ctx, ticketName(input.LeaseRef), "system-assistant-warm", "warm-turn", input, token, nil)
}

func (manager *Manager) WarmTicket(ctx context.Context, revisionRef, revisionDigest string) (string, error) {
	secret, err := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, manager.warmTicketName(revisionRef, revisionDigest), metav1.GetOptions{})
	if err != nil || len(secret.Data[ticketKey]) < 32 {
		return "", errors.New("read warm runtime ticket")
	}
	return string(secret.Data[ticketKey]), nil
}

func (manager *Manager) ResolveWarm(ctx context.Context, revisionRef, revisionDigest, token string) (runtimecontract.RunnerInput, error) {
	if revisionRef == "" || len(revisionDigest) != sha256.Size*2 || token == "" {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback authority is incomplete")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, manager.warmTicketName(revisionRef, revisionDigest), metav1.GetOptions{})
	if err != nil {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback ticket is unavailable")
	}
	if subtle.ConstantTimeCompare(secret.Data[ticketKey], []byte(token)) != 1 {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback ticket does not match")
	}
	input, err := runtimecontract.DecodeRunnerInput(secret.Data[inputKey])
	if err != nil || input.Mode != runtimecontract.RunnerModeWarm || input.RuntimeRevisionRef != revisionRef ||
		input.RuntimeRevisionDigest != revisionDigest {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback binding is invalid")
	}
	return input, nil
}

func (manager *Manager) ResolveTurn(ctx context.Context, leaseRef, token string) (runtimecontract.RunnerInput, error) {
	if leaseRef == "" || token == "" {
		return runtimecontract.RunnerInput{}, errors.New("runtime callback authority is invalid")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, ticketName(leaseRef), metav1.GetOptions{})
	if err != nil || subtle.ConstantTimeCompare(secret.Data[ticketKey], []byte(token)) != 1 {
		return runtimecontract.RunnerInput{}, errors.New("runtime callback authority is invalid")
	}
	input, err := runtimecontract.DecodeRunnerInput(secret.Data[inputKey])
	if err != nil || input.Mode != runtimecontract.RunnerModeTurn || input.LeaseRef != leaseRef {
		return runtimecontract.RunnerInput{}, errors.New("runtime callback binding is invalid")
	}
	return input, nil
}

func (manager *Manager) DeleteTurn(ctx context.Context, leaseRef string) error {
	secretName := ticketName(leaseRef)
	secret, _ := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, secretName, metav1.GetOptions{})
	podName := ""
	if secret != nil {
		podName = secret.Annotations[podAnnotation]
	}
	var result error
	if podName != "" && podName != "system-assistant-warm" {
		if err := manager.client.CoreV1().Pods(manager.config.Namespace).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete completed runtime pod"))
		}
	}
	if err := manager.client.CoreV1().Secrets(manager.config.Namespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		result = errors.Join(result, errors.New("delete completed runtime ticket"))
	}
	return result
}

func (manager *Manager) TurnPodState(ctx context.Context, input runtimecontract.RunnerInput, warmExecution bool) (string, error) {
	podName := turnPodName(input.LeaseRef)
	if warmExecution {
		podName = "system-assistant-warm"
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.Namespace).Get(ctx, podName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "MISSING", nil
	}
	if err != nil {
		return "", errors.New("read runtime execution pod")
	}
	if warmExecution {
		compatibility, compatibilityErr := runtimecontract.WarmCompatibilityDigest(input)
		if compatibilityErr != nil || pod.Annotations[warmCompatibilityAnnotation] != compatibility {
			return "CONFLICT", nil
		}
	} else if pod.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest {
		return "CONFLICT", nil
	}
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		return "SUCCEEDED", nil
	case corev1.PodFailed:
		return "FAILED", nil
	case corev1.PodRunning:
		if !warmExecution && runtimePodTerminal(pod, "role-runtime", "provider-runtime") {
			return "FAILED", nil
		}
		if podReady(pod) {
			return "READY", nil
		}
		return "STARTING", nil
	case corev1.PodPending:
		return "STARTING", nil
	default:
		return "UNKNOWN", nil
	}
}

func (manager *Manager) ensureTicket(ctx context.Context, name, podName, mode string, input runtimecontract.RunnerInput, token string, environmentSecrets map[string][]byte) error {
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		return err
	}
	immutable := true
	data := map[string][]byte{inputKey: raw, ticketKey: []byte(token)}
	for key, value := range environmentSecrets {
		data[key] = append([]byte(nil), value...)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: manager.config.Namespace,
		Labels: map[string]string{managedLabel: "true", modeLabel: mode}, Annotations: map[string]string{
			revisionAnnotation: input.RuntimeRevisionDigest, configAnnotation: input.RuntimeConfigDigest,
			environmentAnnotation: input.RuntimeEnvironmentDigest, controllerAnnotation: manager.config.ControllerPodUID, podAnnotation: podName,
		}},
		Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: data}
	_, err = manager.client.CoreV1().Secrets(manager.config.Namespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil || existing.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest ||
			mode != "warm-turn" && !environmentProjectionMatches(input, existing.Data) ||
			subtle.ConstantTimeCompare(existing.Data[ticketKey], []byte(token)) != 1 && mode == "warm-turn" {
			return errors.New("existing runtime ticket conflicts with immutable execution")
		}
		return nil
	}
	if err != nil {
		return errors.New("create immutable runtime ticket")
	}
	return nil
}

func (manager *Manager) ensureSessionPVC(ctx context.Context, sessionRef string) error {
	name, err := runtimecontract.SessionPVCName(sessionRef)
	if err != nil {
		return err
	}
	_, err = manager.client.CoreV1().PersistentVolumeClaims(manager.config.Namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read runtime session volume")
	}
	var storageClassName *string
	if manager.config.StorageClass != "" {
		storageClassName = &manager.config.StorageClass
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: manager.config.Namespace,
		Labels: map[string]string{managedLabel: "true", "runtime.kodex.dev/session-hash": shortHash(sessionRef)}},
		Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: storageClassName,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest}}}}
	_, err = manager.client.CoreV1().PersistentVolumeClaims(manager.config.Namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return errors.New("create runtime session volume")
	}
	return nil
}

func (manager *Manager) runtimePod(input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding, ticketSecret, podName, mode string) *corev1.Pod {
	roleArgs := []string{"runtime-session"}
	if mode == "warm" {
		roleArgs = []string{"runtime-warm"}
	}
	sessionVolumeName, _ := runtimecontract.SessionPVCName(input.SessionRef)
	volumes := []corev1.Volume{
		{Name: "session", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: sessionVolumeName}}},
		{Name: "runtime-input", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: ticketSecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "callback-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: manager.config.CallbackClientCASecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "callback-client", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: manager.config.CallbackClientTLSSecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "provider-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: providerBinding.Name, DefaultMode: int32Pointer(0o400)}}},
		{Name: "provider-socket", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("512Mi"))}}},
		{Name: "provider-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("512Mi"))}}},
	}
	baseMounts := []corev1.VolumeMount{{Name: "session", MountPath: "/workspace"}, {Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime/runtime.json", SubPath: inputKey, ReadOnly: true}, {Name: "runtime-input", MountPath: "/var/run/secrets/kodex/runtime/ticket/token", SubPath: ticketKey, ReadOnly: true}, {Name: "callback-ca", MountPath: "/var/run/config/kodex/runtime/callback", ReadOnly: true}, {Name: "callback-client", MountPath: "/var/run/secrets/kodex/runtime/callback-client", ReadOnly: true}, {Name: "provider-socket", MountPath: "/run/kodex/provider"}, {Name: "tmp", MountPath: "/tmp"}}
	requests := corev1.ResourceList{corev1.ResourceCPU: *resource.NewMilliQuantity(manager.config.TurnCPUMilli, resource.DecimalSI), corev1.ResourceMemory: *resource.NewQuantity(manager.config.TurnMemoryBytes, resource.BinarySI)}
	role := corev1.Container{Name: "role-runtime", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: roleArgs,
		Env: []corev1.EnvVar{
			{Name: "KODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/kodex/runtime/runtime.json"},
			{Name: "OTEL_SDK_DISABLED", Value: "true"},
			{Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment},
		},
		Ports: []corev1.ContainerPort{{Name: "runtime-health", ContainerPort: 9090}}, SecurityContext: restrictedSecurityContext(10001), VolumeMounts: baseMounts,
		Resources:    corev1.ResourceRequirements{Requests: requests, Limits: requests},
		StartupProbe: httpProbe("/readyz", "runtime-health", 2, 60), ReadinessProbe: httpProbe("/readyz", "runtime-health", 5, 3), LivenessProbe: httpProbe("/healthz", "runtime-health", 10, 3)}
	provider := corev1.Container{Name: "provider-runtime", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-provider"},
		Env: []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}, {Name: "CODEX_HOME", Value: input.CodexHome},
			{Name: "HTTPS_PROXY", Value: manager.config.ProviderHTTPSProxy}, {Name: "HTTP_PROXY", Value: manager.config.ProviderHTTPSProxy},
			{Name: "NO_PROXY", Value: "127.0.0.1,localhost"}, {Name: "OTEL_SDK_DISABLED", Value: "true"}, {Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment}}, SecurityContext: providerSandboxSecurityContext(10002),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "session", MountPath: "/workspace"},
			{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime/runtime.json", SubPath: inputKey, ReadOnly: true},
			{Name: "provider-auth", MountPath: "/run/secrets/kodex/runtime/provider/auth.json", SubPath: "auth.json", ReadOnly: true},
			{Name: "provider-auth", MountPath: "/run/secrets/kodex/runtime/provider/auth.sha256", SubPath: "auth.sha256", ReadOnly: true},
			{Name: "provider-socket", MountPath: "/run/kodex/provider"},
			{Name: "provider-tmp", MountPath: "/tmp"},
		},
		Resources: smallResources(), ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/bin/test", "-S", "/run/kodex/provider/provider.sock"}}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30}}
	for _, item := range input.EnvironmentValues {
		provider.Env = append(provider.Env, corev1.EnvVar{Name: item.Name, Value: item.Value})
	}
	for _, item := range input.SecretProjections {
		provider.Env = append(provider.Env, corev1.EnvVar{Name: item.Name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: ticketSecret}, Key: environmentProjectionKey(item.Name), Optional: boolPointer(false),
		}}})
	}
	annotations := map[string]string{revisionAnnotation: input.RuntimeRevisionDigest, configAnnotation: input.RuntimeConfigDigest,
		environmentAnnotation: input.RuntimeEnvironmentDigest, controllerAnnotation: manager.config.ControllerPodUID}
	if mode == "warm" {
		compatibility, _ := runtimecontract.WarmCompatibilityDigest(input)
		annotations[warmCompatibilityAnnotation] = compatibility
	}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: manager.config.Namespace,
		Labels:      map[string]string{managedLabel: "true", modeLabel: mode, "app.kubernetes.io/name": "agent-runner", "app.kubernetes.io/component": "role-runtime", "kodex.dev/environment": manager.config.Environment},
		Annotations: annotations},
		Spec: corev1.PodSpec{ServiceAccountName: manager.config.RunnerServiceAccount, AutomountServiceAccountToken: boolPointer(false), EnableServiceLinks: boolPointer(false), RestartPolicy: corev1.RestartPolicyNever, TerminationGracePeriodSeconds: int64Pointer(30),
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), FSGroup: int64Pointer(29000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			InitContainers:  []corev1.Container{{Name: "workspace-init", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-init-workspace"}, SecurityContext: restrictedSecurityContext(10001), VolumeMounts: baseMounts, Resources: smallResources()}},
			Containers:      []corev1.Container{role, provider}, Volumes: volumes}}
}

func (manager *Manager) validateImage(input runtimecontract.RunnerInput) error {
	promoted := input.ImageReference == manager.config.PromotedRoleImageRepository+"@"+input.ImageManifestDigest
	defaultPinned := input.ImageReference == manager.config.DefaultRoleImageReference &&
		strings.HasSuffix(manager.config.DefaultRoleImageReference, "@"+input.ImageManifestDigest)
	if (!promoted && !defaultPinned) ||
		input.RoleRuntimeContractRevision != manager.config.RoleRuntimeContractRevision ||
		input.RoleRuntimeContractSHA256 != manager.config.RoleRuntimeContractSHA256 {
		return errors.New("runtime role image is outside promoted policy")
	}
	return nil
}

func validPinnedImageReference(reference string) bool {
	separator := strings.LastIndex(reference, "@sha256:")
	if separator <= 0 || separator+len("@sha256:")+64 != len(reference) {
		return false
	}
	for _, character := range reference[separator+len("@sha256:"):] {
		if (character < 'a' || character > 'f') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return !strings.ContainsAny(reference[:separator], "@${}")
}

func (manager *Manager) validateProviderSecret(ctx context.Context, input runtimecontract.RunnerInput, binding ProviderSecretBinding) error {
	if !validDNSLabel(binding.Name) || binding.UID == "" || binding.ResourceVersion == "" ||
		binding.ContentSHA256 != input.ProviderCredentialSHA256 {
		return errors.New("provider credential binding is invalid")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || string(secret.UID) != binding.UID ||
		secret.ResourceVersion != binding.ResourceVersion {
		return errors.New("provider credential revision is unavailable")
	}
	authentication := secret.Data["auth.json"]
	digestFile := strings.TrimSpace(string(secret.Data["auth.sha256"]))
	digest := sha256.Sum256(authentication)
	actual := hex.EncodeToString(digest[:])
	if len(authentication) == 0 || len(authentication) > 1<<20 || digestFile != actual || actual != binding.ContentSHA256 {
		return errors.New("provider credential revision digest is invalid")
	}
	return nil
}

func (manager *Manager) materializeEnvironmentSecrets(ctx context.Context, input runtimecontract.RunnerInput) (map[string][]byte, error) {
	result := make(map[string][]byte, len(input.SecretProjections))
	totalBytes := 0
	for _, item := range input.EnvironmentValues {
		totalBytes += len(item.Name) + len(item.Value)
	}
	for _, item := range input.SecretProjections {
		if !validDNSSubdomain(item.SecretName) {
			return nil, errors.New("runtime Secret projection is invalid")
		}
		secret, err := manager.client.CoreV1().Secrets(manager.config.Namespace).Get(ctx, item.SecretName, metav1.GetOptions{})
		if err != nil || secret.Immutable == nil || !*secret.Immutable || string(secret.UID) != item.SecretUID ||
			secret.ResourceVersion != item.SecretResourceVersion {
			return nil, errors.New("runtime Secret projection is unavailable")
		}
		value, present := secret.Data[item.SecretKey]
		digest := sha256.Sum256(value)
		if !present || len(value) > 8<<10 || !utf8.Valid(value) || bytesContainNUL(value) ||
			hex.EncodeToString(digest[:]) != item.ContentSHA256 {
			return nil, errors.New("runtime Secret projection digest is invalid")
		}
		totalBytes += len(item.Name) + len(value)
		result[environmentProjectionKey(item.Name)] = append([]byte(nil), value...)
	}
	if totalBytes > runtimecontract.MaximumRuntimeEnvironmentBytes {
		return nil, errors.New("runtime Secret projection byte limit exceeded")
	}
	return result, nil
}

func environmentProjectionMatches(input runtimecontract.RunnerInput, data map[string][]byte) bool {
	for _, item := range input.SecretProjections {
		value, present := data[environmentProjectionKey(item.Name)]
		digest := sha256.Sum256(value)
		if !present || hex.EncodeToString(digest[:]) != item.ContentSHA256 {
			return false
		}
	}
	return true
}

func environmentProjectionKey(name string) string {
	return "environment-" + shortHash(name)
}

func bytesContainNUL(value []byte) bool {
	return bytes.IndexByte(value, 0) >= 0
}

func podReady(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func runtimePodTerminal(pod *corev1.Pod, requiredContainers ...string) bool {
	if pod == nil || pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
		return true
	}
	required := make(map[string]struct{}, len(requiredContainers))
	for _, name := range requiredContainers {
		required[name] = struct{}{}
	}
	for _, status := range pod.Status.ContainerStatuses {
		_, requiredContainer := required[status.Name]
		if status.State.Terminated != nil && (len(required) == 0 || requiredContainer) {
			return true
		}
	}
	return false
}

func newTicket() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errors.New("generate execution ticket")
	}
	return hex.EncodeToString(raw), nil
}

func shortHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}
func (manager *Manager) warmTicketName(revisionRef, revisionDigest string) string {
	return ticketName("warm-" + revisionRef + "|" + revisionDigest + "|" + manager.config.ControllerPodUID + "|" + manager.config.ControllerPodIP)
}
func ticketName(value string) string                             { return "runtime-ticket-" + shortHash(value) }
func turnPodName(value string) string                            { return "runtime-turn-" + shortHash(value) }
func int64Pointer(value int64) *int64                            { return &value }
func int32Pointer(value int32) *int32                            { return &value }
func boolPointer(value bool) *bool                               { return &value }
func quantityPointer(value resource.Quantity) *resource.Quantity { return &value }

func validDNSLabel(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, character := range value {
		valid := character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-'
		if !valid || character == '-' && (index == 0 || index == len(value)-1) {
			return false
		}
	}
	return true
}

func validDNSSubdomain(value string) bool {
	if value == "" || len(value) > 253 {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if !validDNSLabel(label) {
			return false
		}
	}
	return true
}

func restrictedSecurityContext(uid int64) *corev1.SecurityContext {
	return &corev1.SecurityContext{RunAsNonRoot: boolPointer(true), RunAsUser: int64Pointer(uid), RunAsGroup: int64Pointer(uid), AllowPrivilegeEscalation: boolPointer(false), ReadOnlyRootFilesystem: boolPointer(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
}

func providerSandboxSecurityContext(uid int64) *corev1.SecurityContext {
	securityContext := restrictedSecurityContext(uid)
	// Codex строит внутреннюю файловую и сетевую границу через bubblewrap.
	// Default seccomp/AppArmor профили Kubernetes блокируют создание его user namespace.
	securityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
	securityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
	return securityContext
}

func smallResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}
}

func httpProbe(path, port string, period, failures int32) *corev1.Probe {
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromString(port)}}, PeriodSeconds: period, TimeoutSeconds: 2, FailureThreshold: failures}
}

func (manager *Manager) DebugSummary() string {
	return fmt.Sprintf("namespace=%s controller=%s", manager.config.Namespace, shortHash(manager.config.ControllerPodUID))
}
