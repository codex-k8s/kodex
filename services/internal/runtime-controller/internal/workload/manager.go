// Package workload материализует immutable RuntimeRevision в execution-scoped Kubernetes Pod.
package workload

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
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
	managedLabel                            = "runtime.kodex.dev/managed"
	modeLabel                               = "runtime.kodex.dev/mode"
	revisionAnnotation                      = "runtime.kodex.dev/revision-digest"
	configAnnotation                        = "runtime.kodex.dev/config-digest"
	environmentAnnotation                   = "runtime.kodex.dev/environment-digest"
	warmCompatibilityAnnotation             = "runtime.kodex.dev/warm-compatibility-digest"
	controllerAnnotation                    = "runtime.kodex.dev/controller-pod-uid"
	podAnnotation                           = "runtime.kodex.dev/pod-name"
	leaseAnnotation                         = "runtime.kodex.dev/lease-ref"
	resourcesAnnotation                     = "runtime.kodex.dev/resources-digest"
	volumesAnnotation                       = "runtime.kodex.dev/volumes-digest"
	networkAnnotation                       = "runtime.kodex.dev/network-digest"
	rbacProfileAnnotation                   = "runtime.kodex.dev/rbac-profile-digest"
	effectiveRBACAnnotation                 = "runtime.kodex.dev/effective-rbac-digest"
	workspacePolicyAnnotation               = "runtime.kodex.dev/workspace-policy-digest"
	executionBindingAnnotation              = "runtime.kodex.dev/execution-binding-digest"
	mcpBindingAnnotation                    = "runtime.kodex.dev/mcp-binding-digest"
	organizationHashAnnotation              = "runtime.kodex.dev/organization-hash"
	projectHashAnnotation                   = "runtime.kodex.dev/project-hash"
	sessionHashAnnotation                   = "runtime.kodex.dev/session-hash"
	turnHashAnnotation                      = "runtime.kodex.dev/turn-hash"
	attemptAnnotation                       = "runtime.kodex.dev/attempt"
	providerSecretNameAnnotation            = "runtime.kodex.dev/provider-secret-name"
	providerSecretUIDAnnotation             = "runtime.kodex.dev/provider-secret-uid"
	providerSecretResourceVersionAnnotation = "runtime.kodex.dev/provider-secret-resource-version"
	providerCredentialRefAnnotation         = "runtime.kodex.dev/provider-credential-ref"
	providerCredentialDigestAnnotation      = "runtime.kodex.dev/provider-credential-digest"
	providerAccountRefAnnotation            = "runtime.kodex.dev/provider-account-ref"
	previousCredentialRefAnnotation         = "runtime.kodex.dev/previous-provider-credential-ref"
	previousCredentialDigestAnnotation      = "runtime.kodex.dev/previous-provider-credential-digest"
	executionHashLabel                      = "runtime.kodex.dev/execution-hash"
	inputKey                                = "runtime.json"
	ticketKey                               = "token"
	workspacePolicyKey                      = "workspace-policy.json"
	inputManifestKey                        = "inputs.json"
	resultManifestKey                       = "results.json"
	skillManifestKey                        = "skills.json"
	memoryManifestKey                       = "memories.json"
	mcpManifestKey                          = "mcp.json"
	callbackManifestKey                     = "callback.json"
	maximumKubernetesProjectionBytes        = 900 << 10
)

// ErrProviderCredentialRefreshRejected отделяет stale/invalid lineage от
// временной недоступности Kubernetes API.
var ErrProviderCredentialRefreshRejected = errors.New("provider credential refresh is rejected")

type Config struct {
	Environment, ControlNamespace, RuntimeNamespace, ControllerPodUID, ControllerPodIP string
	CallbackTLSServerName, CallbackClientCASecret, CallbackClientTLSSecret             string
	StorageClass, SessionPVCSize, RunnerServiceAccount                                 string
	ProviderHTTPSProxy                                                                 string
	ProviderAppArmorProfile                                                            string
	KubernetesAPIServiceIP                                                             string
	PromotedRoleImageRepository, DefaultRoleImageReference                             string
	RoleRuntimeContractSHA256                                                          string
	RoleRuntimeContractRevision                                                        uint64
}

// ProviderSecretBinding остаётся только внутри trusted runtime-controller и
// не сериализуется в runtime.json, доступный role image.
type ProviderSecretBinding struct {
	Name, UID, ResourceVersion, ContentSHA256 string
}

type managedChatGPTAuthentication struct {
	AuthMode string `json:"auth_mode"`
	Tokens   struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		AccountID    string `json:"account_id"`
	} `json:"tokens"`
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
	if client == nil || err != nil || pvcRequest.Sign() <= 0 || config.ControlNamespace == "" ||
		config.RuntimeNamespace == "" || config.ControlNamespace == config.RuntimeNamespace ||
		config.ControllerPodUID == "" || net.ParseIP(config.ControllerPodIP) == nil ||
		config.CallbackTLSServerName == "" || config.CallbackClientCASecret == "" ||
		config.CallbackClientTLSSecret == "" ||
		config.ProviderHTTPSProxy == "" ||
		(config.ProviderAppArmorProfile != "" && config.ProviderAppArmorProfile != "kodex-provider-runtime") ||
		net.ParseIP(config.KubernetesAPIServiceIP) == nil ||
		(config.StorageClass != "" && !validDNSSubdomain(config.StorageClass)) ||
		config.RunnerServiceAccount == "" ||
		config.PromotedRoleImageRepository == "" || config.RoleRuntimeContractRevision == 0 ||
		!validPinnedImageReference(config.DefaultRoleImageReference) ||
		len(config.RoleRuntimeContractSHA256) != sha256.Size*2 {
		return nil, errors.New("Kubernetes runtime manager configuration is invalid")
	}
	return &Manager{client: client, config: config, pvcRequest: pvcRequest}, nil
}

func (manager *Manager) Check(ctx context.Context) error {
	if _, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
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
		Lock: &resourcelock.LeaseLock{LeaseMeta: metav1.ObjectMeta{Name: "runtime-controller-leader", Namespace: manager.config.ControlNamespace},
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
	var result error
	activeHashes := make(map[string]struct{})
	continueToken := ""
	for {
		pods, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return errors.New("list retained runtime turn pods")
		}
		for index := range pods.Items {
			pod := &pods.Items[index]
			if pod.Annotations[controllerAnnotation] == manager.config.ControllerPodUID {
				if executionHash := pod.Labels[executionHashLabel]; len(executionHash) == 16 {
					activeHashes[executionHash] = struct{}{}
				}
				continue
			}
			if err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, pod.Name, metav1.DeleteOptions{GracePeriodSeconds: int64Pointer(0)}); err != nil && !apierrors.IsNotFound(err) {
				result = errors.Join(result, errors.New("delete stale runtime turn pod"))
			}
			if err := manager.deleteExecutionPolicyByHash(ctx, pod.Labels[executionHashLabel]); err != nil {
				result = errors.Join(result, err)
			}
		}
		if pods.Continue == "" {
			break
		}
		continueToken = pods.Continue
	}
	orphaned, orphanErr := manager.orphanedExecutionPolicyHashes(ctx, selector, activeHashes)
	if orphanErr != nil {
		result = errors.Join(result, orphanErr)
	}
	for executionHash := range orphaned {
		if err := manager.deleteExecutionPolicyByHash(ctx, executionHash); err != nil {
			result = errors.Join(result, err)
		}
	}
	continueToken = ""
	for {
		tickets, ticketErr := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if ticketErr != nil {
			result = errors.Join(result, errors.New("list retained runtime turn tickets"))
			break
		}
		for index := range tickets.Items {
			ticket := &tickets.Items[index]
			executionHash := strings.TrimPrefix(ticket.Annotations[podAnnotation], "runtime-turn-")
			_, active := activeHashes[executionHash]
			if ticket.Annotations[controllerAnnotation] == manager.config.ControllerPodUID && active {
				continue
			}
			if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, ticket.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				result = errors.Join(result, errors.New("delete stale runtime turn ticket"))
			}
		}
		if tickets.Continue == "" {
			break
		}
		continueToken = tickets.Continue
	}
	continueToken = ""
	for {
		projections, projectionErr := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256, Continue: continueToken})
		if projectionErr != nil {
			result = errors.Join(result, errors.New("list retained runtime VFS projections"))
			break
		}
		for index := range projections.Items {
			projection := &projections.Items[index]
			executionHash := strings.TrimPrefix(projection.Annotations[podAnnotation], "runtime-turn-")
			_, active := activeHashes[executionHash]
			if projection.Annotations[controllerAnnotation] == manager.config.ControllerPodUID && active {
				continue
			}
			if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, projection.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				result = errors.Join(result, errors.New("delete stale runtime VFS projection"))
			}
		}
		if projections.Continue == "" {
			break
		}
		continueToken = projections.Continue
	}
	return result
}

func (manager *Manager) orphanedExecutionPolicyHashes(ctx context.Context, selector string, active map[string]struct{}) (map[string]struct{}, error) {
	orphaned := make(map[string]struct{})
	inspect := func(metadata metav1.ObjectMeta) {
		executionHash := metadata.Labels[executionHashLabel]
		_, isActive := active[executionHash]
		if len(executionHash) == 16 && (metadata.Annotations[controllerAnnotation] != manager.config.ControllerPodUID || !isActive) {
			orphaned[executionHash] = struct{}{}
		}
	}
	continueToken := ""
	for {
		accounts, err := manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution ServiceAccounts")
		}
		for index := range accounts.Items {
			inspect(accounts.Items[index].ObjectMeta)
		}
		if accounts.Continue == "" {
			break
		}
		continueToken = accounts.Continue
	}
	continueToken = ""
	for {
		roles, err := manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution Roles")
		}
		for index := range roles.Items {
			inspect(roles.Items[index].ObjectMeta)
		}
		if roles.Continue == "" {
			break
		}
		continueToken = roles.Continue
	}
	continueToken = ""
	for {
		bindings, err := manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution RoleBindings")
		}
		for index := range bindings.Items {
			inspect(bindings.Items[index].ObjectMeta)
		}
		if bindings.Continue == "" {
			break
		}
		continueToken = bindings.Continue
	}
	continueToken = ""
	for {
		policies, err := manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector, Limit: 256, Continue: continueToken,
		})
		if err != nil {
			return nil, errors.New("list runtime execution NetworkPolicies")
		}
		for index := range policies.Items {
			inspect(policies.Items[index].ObjectMeta)
		}
		if policies.Continue == "" {
			break
		}
		continueToken = policies.Continue
	}
	return orphaned, nil
}

func (manager *Manager) BuildTurnInput(execution *controlplanev1.ClaimedExecution) (runtimecontract.RunnerInput, ProviderSecretBinding, error) {
	if execution == nil || execution.GetRun() == nil || execution.GetNode() == nil || execution.GetRevision() == nil ||
		execution.GetRevision().GetRuntime() == nil || execution.GetLease() == nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, errors.New("claimed execution is incomplete")
	}
	revision := execution.GetRevision()
	input, err := manager.baseInput(revision, runtimecontract.RunnerModeTurn)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef = execution.GetRun().GetRef(), execution.GetNode().GetRef(), revision.GetSessionRef(), revision.GetTurnRef()
	input.ProjectRef = execution.GetRun().GetProjectRef()
	input.AgentRef, input.Attempt = revision.GetAgentRef(), revision.GetAttempt()
	input.InputDigest = revision.GetInputDigest()
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
	input.ExecutionBindingDigest, input.MCPBindingDigest, err = runtimecontract.RuntimeExecutionBindingDigests(input)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	binding, err := providerSecretBinding(revision)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	if err := validateRuntimeRevisionDigest(input, binding); err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	return input, binding, validateRunnerInput(input)
}

func (manager *Manager) BuildWarmInput(revision *controlplanev1.RuntimeRevisionSnapshot) (runtimecontract.RunnerInput, ProviderSecretBinding, error) {
	if revision == nil || revision.GetRuntime() == nil || !revision.GetSystemAssistant() {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, errors.New("warm runtime revision is invalid")
	}
	input, err := manager.baseInput(revision, runtimecontract.RunnerModeWarm)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	input.SessionRef, input.AgentRef = revision.GetSessionRef(), revision.GetAgentRef()
	manager.addCatalog(&input, revision)
	binding, err := providerSecretBinding(revision)
	if err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	if err := validateRuntimeRevisionDigest(input, binding); err != nil {
		return runtimecontract.RunnerInput{}, ProviderSecretBinding{}, err
	}
	return input, binding, validateRunnerInput(input)
}

func validateRuntimeRevisionDigest(input runtimecontract.RunnerInput, binding ProviderSecretBinding) error {
	digest, err := runtimecontract.RuntimeRevisionDigest(input, runtimecontract.RuntimeRevisionCredentialSource{
		SecretName: binding.Name, SecretUID: binding.UID, SecretResourceVersion: binding.ResourceVersion,
	})
	if err != nil || subtle.ConstantTimeCompare([]byte(digest), []byte(input.RuntimeRevisionDigest)) != 1 {
		return errors.New("runtime revision digest mismatch")
	}
	return nil
}

func validateRunnerInput(input runtimecontract.RunnerInput) error {
	if err := input.Validate(); err != nil {
		return err
	}
	for _, set := range input.AttachmentSets {
		if set.TurnRef == "" || set.Provenance == "CURRENT_TURN" && set.TurnRef != input.TurnRef ||
			set.Provenance == "SESSION_HISTORY" && set.TurnRef == input.TurnRef {
			return errors.New("runtime attachment turn lineage is invalid")
		}
		artifacts := make([]runtimecontract.RunnerInputArtifact, 0)
		for _, artifact := range input.InputArtifacts {
			if artifact.AttachmentSetRef != set.Ref {
				continue
			}
			canonicalArtifact := artifact
			canonicalArtifact.Scope = runtimecontract.AttachmentScopeInput
			canonicalArtifact.AttachmentSetRef = ""
			canonicalArtifact.AttachmentPurpose = ""
			canonicalArtifact.Provenance = ""
			artifacts = append(artifacts, canonicalArtifact)
		}
		manifest, err := runtimecontract.BuildAttachmentManifest(set.Ref, set.Purpose, artifacts)
		if err != nil || manifest.Digest != set.ManifestDigest {
			return errors.New("runtime attachment manifest digest is invalid")
		}
	}
	if _, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts); err != nil {
		return errors.New("runtime workspace attachment manifest is invalid")
	}
	return nil
}

func (manager *Manager) baseInput(revision *controlplanev1.RuntimeRevisionSnapshot, mode string) (runtimecontract.RunnerInput, error) {
	environmentPolicy, err := runtimeEnvironmentPolicyFromProto(revision.GetEnvironmentPolicy())
	if err != nil {
		return runtimecontract.RunnerInput{}, err
	}
	effectiveAccess, err := runtimeKubernetesAccessFromProto(revision.GetEffectiveKubernetesAccess())
	if err != nil {
		return runtimecontract.RunnerInput{}, err
	}
	input := runtimecontract.RunnerInput{
		Schema: runtimecontract.RunnerInputSchemaV6, Mode: mode, WorkloadInstance: manager.config.ControllerPodUID,
		OrganizationRef:    revision.GetOrganizationRef(),
		RuntimeRevisionRef: revision.GetRef(), RuntimeRevisionVersion: revision.GetVersion(), RuntimeRevisionDigest: revision.GetRevisionDigest(),
		ImageReference: revision.GetImageReference(), ImageManifestDigest: revision.GetImageManifestDigest(),
		EnvironmentImage: runtimecontract.RuntimeEnvironmentImage{
			ArtifactRef: revision.GetRoleImageArtifactRef(), RecipeRef: revision.GetRoleImageRecipeRef(),
			RecipeGeneration: revision.GetRoleImageRecipeGeneration(), Reference: revision.GetImageReference(), Digest: revision.GetImageManifestDigest(),
		},
		RoleRuntimeContractRevision: revision.GetRoleRuntimeContractRevision(), RoleRuntimeContractSHA256: revision.GetRoleRuntimeContractSha256(),
		RoleDefinitionRef: revision.GetRoleDefinitionRef(), RuntimeProfileRef: revision.GetRuntime().GetRef(), RuntimeProfileRevision: revision.GetRuntime().GetRevision(),
		InstructionRef: revision.GetInstructionRef(), InstructionDigest: revision.GetInstructionDigest(),
		PromptTemplateRef: revision.GetPromptTemplateRef(), PromptTemplateDigest: revision.GetPromptTemplateDigest(), PromptMaterializationDigest: revision.GetPromptMaterializationDigest(),
		SystemSTTConfigurationRef: revision.GetSystemSttConfigurationRef(), SystemSTTConfigurationRevisionRef: revision.GetSystemSttConfigurationRevisionRef(), SystemSTTConfigurationVersion: revision.GetSystemSttConfigurationVersion(), SystemSTTConfigurationDigest: revision.GetSystemSttConfigurationDigest(),
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
		EnvironmentPolicy:          environmentPolicy,
		EffectiveKubernetesAccess:  effectiveAccess,
		CodexSandbox:               "read-only", CodexApprovalPolicy: "never",
		CallbackURL: "https://" + net.JoinHostPort(manager.config.ControllerPodIP, "8444"),
		CallbackTLS: runtimecontract.RuntimeTLSBinding{ServerName: manager.config.CallbackTLSServerName,
			CAFile: "/var/run/config/kodex/runtime/callback/ca.crt", CertificateFile: "/var/run/secrets/kodex/runtime/callback-client/tls.crt", PrivateKeyFile: "/var/run/secrets/kodex/runtime/callback-client/tls.key"},
		ExecutionTicketFile: "/var/run/secrets/kodex/runtime/ticket/token",
		ProviderAuthFile:    "/run/secrets/kodex/runtime/provider/auth.json", ProviderAuthSHA256File: "/run/secrets/kodex/runtime/provider/auth.sha256",
		WorkspaceRoot: "/workspace", OutboxRoot: "/workspace/.kodex/outbox", CodexHome: "/workspace/.kodex/state/codex-home",
	}
	workspacePolicy, err := runtimeWorkspacePolicyFromProto(revision.GetWorkspacePolicy())
	if err != nil {
		return runtimecontract.RunnerInput{}, err
	}
	input.WorkspacePolicy = workspacePolicy
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
	return input, nil
}

func runtimeWorkspacePolicyFromProto(value *controlplanev1.RuntimeWorkspacePolicy) (runtimecontract.RuntimeWorkspacePolicy, error) {
	if value == nil {
		return runtimecontract.RuntimeWorkspacePolicy{}, errors.New("runtime workspace policy is missing")
	}
	policy := runtimecontract.RuntimeWorkspacePolicy{Revision: value.GetRevision(), Root: value.GetRoot(), MaximumWritableBytes: value.GetMaximumWritableBytes(), MaximumFileCount: value.GetMaximumFileCount(), Digest: value.GetDigest()}
	for _, rule := range value.GetRules() {
		policy.Rules = append(policy.Rules, runtimecontract.RuntimeWorkspacePathRule{Path: rule.GetPath(), Access: strings.TrimPrefix(rule.GetAccess().String(), "RUNTIME_WORKSPACE_ACCESS_")})
	}
	for _, reason := range value.GetDenialReasons() {
		policy.DenialReasons = append(policy.DenialReasons, strings.TrimPrefix(reason.String(), "RUNTIME_WORKSPACE_DENIAL_REASON_"))
	}
	if err := policy.Validate(); err != nil {
		return runtimecontract.RuntimeWorkspacePolicy{}, err
	}
	return policy, nil
}

func runtimeEnvironmentPolicyFromProto(value *controlplanev1.RuntimeEnvironmentPolicy) (runtimecontract.RuntimeEnvironmentPolicy, error) {
	if value == nil || value.GetResources() == nil || value.GetNetwork() == nil || value.GetKubernetesAccess() == nil {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errors.New("runtime environment policy is incomplete")
	}
	resources := value.GetResources()
	policy := runtimecontract.RuntimeEnvironmentPolicy{
		Resources: runtimecontract.RuntimeResourcePolicy{
			CPURequestMilli: resources.GetCpuRequestMilli(), CPULimitMilli: resources.GetCpuLimitMilli(),
			MemoryRequestMiB: resources.GetMemoryRequestMib(), MemoryLimitMiB: resources.GetMemoryLimitMib(),
			EphemeralStorageRequestMiB: resources.GetEphemeralStorageRequestMib(),
			EphemeralStorageLimitMiB:   resources.GetEphemeralStorageLimitMib(),
		},
		Network: runtimecontract.RuntimeNetworkPolicy{DenyByDefault: value.GetNetwork().GetDenyByDefault()},
		KubernetesAccess: runtimecontract.RuntimeKubernetesAccessProfile{
			Kind:      runtimeKubernetesAccessKind(value.GetKubernetesAccess().GetKind()),
			Namespace: value.GetKubernetesAccess().GetNamespace(),
		},
		ResourcesDigest: value.GetResourcesDigest(), VolumesDigest: value.GetVolumesDigest(),
		NetworkDigest: value.GetNetworkDigest(), RBACDigest: value.GetRbacDigest(),
	}
	for _, volume := range value.GetVolumes() {
		policy.Volumes = append(policy.Volumes, runtimecontract.RuntimeVolume{
			Name: volume.GetName(), Kind: runtimeVolumeKind(volume.GetKind()), SizeMiB: volume.GetSizeMib(), MountPath: volume.GetMountPath(),
		})
	}
	for _, egress := range value.GetNetwork().GetEgress() {
		policy.Network.Egress = append(policy.Network.Egress, runtimecontract.RuntimeNetworkEgress{
			Destination: runtimeNetworkDestination(egress.GetDestination()), Protocol: runtimeNetworkProtocol(egress.GetProtocol()), Port: egress.GetPort(),
		})
	}
	normalized, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil || normalized.ResourcesDigest != policy.ResourcesDigest || normalized.VolumesDigest != policy.VolumesDigest ||
		normalized.NetworkDigest != policy.NetworkDigest || normalized.RBACDigest != policy.RBACDigest {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errors.New("runtime environment policy digest mismatch")
	}
	return normalized, nil
}

func runtimeKubernetesAccessFromProto(value *controlplanev1.RuntimeKubernetesAccess) (runtimecontract.RuntimeKubernetesAccess, error) {
	if value == nil || value.GetProfile() == nil {
		return runtimecontract.RuntimeKubernetesAccess{}, errors.New("effective Kubernetes access is incomplete")
	}
	access := runtimecontract.RuntimeKubernetesAccess{
		Profile: runtimecontract.RuntimeKubernetesAccessProfile{
			Kind: runtimeKubernetesAccessKind(value.GetProfile().GetKind()), Namespace: value.GetProfile().GetNamespace(),
		},
		ServiceAccountName: value.GetServiceAccountName(), Rules: []runtimecontract.RuntimeKubernetesRule{}, Digest: value.GetDigest(),
	}
	for _, rule := range value.GetRules() {
		access.Rules = append(access.Rules, runtimecontract.RuntimeKubernetesRule{
			APIGroup: rule.GetApiGroup(), Resource: rule.GetResource(), Verbs: append([]string(nil), rule.GetVerbs()...),
			ResourceNames: append([]string(nil), rule.GetResourceNames()...),
		})
	}
	if err := runtimecontract.ValidateRuntimeKubernetesAccess(access); err != nil {
		return runtimecontract.RuntimeKubernetesAccess{}, err
	}
	return access, nil
}

func runtimeVolumeKind(value controlplanev1.RuntimeVolumeKind) string {
	switch value {
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK:
		return runtimecontract.RuntimeVolumeEphemeralDisk
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY:
		return runtimecontract.RuntimeVolumeEphemeralMemory
	default:
		return ""
	}
}

func runtimeNetworkDestination(value controlplanev1.RuntimeNetworkDestination) string {
	switch value {
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS:
		return runtimecontract.RuntimeEgressDNS
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK:
		return runtimecontract.RuntimeEgressRuntimeCallback
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY:
		return runtimecontract.RuntimeEgressProviderProxy
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_KUBERNETES_API:
		return runtimecontract.RuntimeEgressKubernetesAPI
	default:
		return ""
	}
}

func runtimeNetworkProtocol(value controlplanev1.RuntimeNetworkProtocol) string {
	switch value {
	case controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_TCP:
		return runtimecontract.RuntimeProtocolTCP
	case controlplanev1.RuntimeNetworkProtocol_RUNTIME_NETWORK_PROTOCOL_UDP:
		return runtimecontract.RuntimeProtocolUDP
	default:
		return ""
	}
}

func runtimeKubernetesAccessKind(value controlplanev1.RuntimeKubernetesAccessKind) string {
	switch value {
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE:
		return runtimecontract.RuntimeKubernetesAccessNone
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_READ_OWN_EXECUTION:
		return runtimecontract.RuntimeKubernetesAccessReadOwnExecution
	default:
		return ""
	}
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
	for _, set := range revision.GetAttachmentSets() {
		input.AttachmentSets = append(input.AttachmentSets, runtimecontract.RunnerAttachmentSet{
			Ref: set.GetRef(), ManifestDigest: set.GetManifestDigest(), Purpose: set.GetPurpose(),
			Scope: set.GetScope(), Provenance: set.GetProvenance(), TurnRef: set.GetTurnRef(),
		})
	}
	for _, runtimeArtifact := range revision.GetInputArtifacts() {
		artifact := runtimeArtifact.GetArtifact()
		if artifact == nil {
			continue
		}
		input.InputArtifacts = append(input.InputArtifacts, runtimecontract.RunnerInputArtifact{
			Ref: artifact.GetRef(), FileName: artifact.GetFileName(), MediaType: artifact.GetMediaType(),
			Digest: artifact.GetDigest(), SizeBytes: artifact.GetSizeBytes(), Revision: int64(artifact.GetRevision()),
			Version: artifact.GetVersion(), Scope: runtimeArtifact.GetScope(), Position: runtimeArtifact.GetPosition(),
			Source: runnerArtifactSource(artifact.GetSource()), AttachmentSetRef: runtimeArtifact.GetAttachmentSetRef(),
			AttachmentPurpose: runtimeArtifact.GetAttachmentPurpose(), Provenance: runtimeArtifact.GetProvenance(),
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
		return fmt.Errorf("validate provider credential projection: %w", err)
	}
	environmentSecrets, err := manager.materializeEnvironmentSecrets(ctx, input)
	if err != nil {
		return fmt.Errorf("materialize runtime environment secrets: %w", err)
	}
	if err := manager.ensureSessionPVC(ctx, input); err != nil {
		return fmt.Errorf("ensure runtime session volume: %w", err)
	}
	token, err := newTicket()
	if err != nil {
		return fmt.Errorf("generate runtime execution ticket: %w", err)
	}
	secretName := ticketName(input.LeaseRef)
	podName := runtimecontract.RuntimeTurnPodName(input.LeaseRef)
	if err := manager.ensureExecutionPolicy(ctx, input, podName); err != nil {
		return fmt.Errorf("ensure runtime execution policy: %w", err)
	}
	if err := manager.ensureProjection(ctx, podName, "turn", input); err != nil {
		return fmt.Errorf("ensure runtime VFS projection: %w", err)
	}
	if err := manager.ensureTicket(ctx, secretName, podName, "turn", input, token, environmentSecrets, &providerBinding); err != nil {
		return fmt.Errorf("ensure runtime execution ticket: %w", err)
	}
	pod := manager.runtimePod(input, providerBinding, secretName, podName, "turn")
	_, err = manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(ctx, pod, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
		if getErr != nil || !runtimePodMatches(existing, pod) {
			return errors.New("existing runtime turn pod conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create runtime turn pod: %w", err)
	}
	return nil
}

func (manager *Manager) ensureExecutionPolicy(ctx context.Context, input runtimecontract.RunnerInput, podName string) error {
	if input.EffectiveKubernetesAccess.ServiceAccountName != runtimecontract.RuntimeServiceAccountName(input.LeaseRef) ||
		input.EnvironmentPolicy.KubernetesAccess != input.EffectiveKubernetesAccess.Profile ||
		runtimecontract.ValidateRuntimeKubernetesAccess(input.EffectiveKubernetesAccess) != nil {
		return errors.New("effective Kubernetes access does not match execution identity")
	}
	metadata := manager.executionMetadata(input)
	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metadata, AutomountServiceAccountToken: boolPointer(false)}
	serviceAccount.Name = input.EffectiveKubernetesAccess.ServiceAccountName
	if err := manager.ensureServiceAccount(ctx, serviceAccount); err != nil {
		return err
	}
	if input.EffectiveKubernetesAccess.Profile.Kind == runtimecontract.RuntimeKubernetesAccessReadOwnExecution {
		role := manager.executionRole(input)
		if err := manager.ensureRole(ctx, role); err != nil {
			return err
		}
		binding := manager.executionRoleBinding(input)
		if err := manager.ensureRoleBinding(ctx, binding); err != nil {
			return err
		}
	} else if err := manager.ensureExecutionRBACAbsent(ctx, input); err != nil {
		return err
	}
	networkPolicy, err := manager.executionNetworkPolicy(input, podName)
	if err != nil {
		return err
	}
	return manager.ensureNetworkPolicy(ctx, networkPolicy)
}

func (manager *Manager) executionMetadata(input runtimecontract.RunnerInput) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: manager.config.RuntimeNamespace,
		Labels: map[string]string{managedLabel: "true", modeLabel: "turn", executionHashLabel: shortHash(input.LeaseRef)},
		Annotations: map[string]string{
			revisionAnnotation: input.RuntimeRevisionDigest, leaseAnnotation: input.LeaseRef, controllerAnnotation: manager.config.ControllerPodUID,
			resourcesAnnotation: input.EnvironmentPolicy.ResourcesDigest, volumesAnnotation: input.EnvironmentPolicy.VolumesDigest,
			networkAnnotation: input.EnvironmentPolicy.NetworkDigest, rbacProfileAnnotation: input.EnvironmentPolicy.RBACDigest,
			effectiveRBACAnnotation: input.EffectiveKubernetesAccess.Digest,
		},
	}
}

func (manager *Manager) executionRole(input runtimecontract.RunnerInput) *rbacv1.Role {
	metadata := manager.executionMetadata(input)
	metadata.Name = runtimecontract.RuntimeRoleName(input.LeaseRef)
	rules := make([]rbacv1.PolicyRule, 0, len(input.EffectiveKubernetesAccess.Rules))
	for _, rule := range input.EffectiveKubernetesAccess.Rules {
		rules = append(rules, rbacv1.PolicyRule{APIGroups: []string{rule.APIGroup}, Resources: []string{rule.Resource},
			Verbs: append([]string(nil), rule.Verbs...), ResourceNames: append([]string(nil), rule.ResourceNames...)})
	}
	return &rbacv1.Role{ObjectMeta: metadata, Rules: rules}
}

func (manager *Manager) executionRoleBinding(input runtimecontract.RunnerInput) *rbacv1.RoleBinding {
	metadata := manager.executionMetadata(input)
	metadata.Name = runtimecontract.RuntimeRoleBindingName(input.LeaseRef)
	return &rbacv1.RoleBinding{ObjectMeta: metadata,
		Subjects: []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: input.EffectiveKubernetesAccess.ServiceAccountName, Namespace: manager.config.RuntimeNamespace}},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: runtimecontract.RuntimeRoleName(input.LeaseRef)},
	}
}

func (manager *Manager) executionNetworkPolicy(input runtimecontract.RunnerInput, podName string) (*networkingv1.NetworkPolicy, error) {
	if podName != runtimecontract.RuntimeTurnPodName(input.LeaseRef) {
		return nil, errors.New("runtime network policy Pod identity is invalid")
	}
	metadata := manager.executionMetadata(input)
	metadata.Name = runtimecontract.RuntimeNetworkPolicyName(input.LeaseRef)
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metadata, Spec: networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{managedLabel: "true", modeLabel: "turn", executionHashLabel: shortHash(input.LeaseRef)}},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
	}}
	for _, item := range input.EnvironmentPolicy.Network.Egress {
		protocol := corev1.Protocol(item.Protocol)
		port := intstr.FromInt32(item.Port)
		rule := networkingv1.NetworkPolicyEgressRule{Ports: []networkingv1.NetworkPolicyPort{{Protocol: &protocol, Port: &port}}}
		switch item.Destination {
		case runtimecontract.RuntimeEgressDNS:
			rule.To = []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}},
				PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}},
			}}
		case runtimecontract.RuntimeEgressRuntimeCallback:
			rule.To = []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": manager.config.ControlNamespace}},
				PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "runtime-controller", "app.kubernetes.io/component": "internal-controller"}},
			}}
		case runtimecontract.RuntimeEgressProviderProxy:
			rule.To = []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": manager.config.ControlNamespace}},
				PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "egress-gateway", "app.kubernetes.io/component": "platform-egress"}},
			}}
		case runtimecontract.RuntimeEgressKubernetesAPI:
			rule.To = []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: manager.config.KubernetesAPIServiceIP + "/32"}}}
		default:
			return nil, errors.New("runtime network destination is unsupported")
		}
		policy.Spec.Egress = append(policy.Spec.Egress, rule)
	}
	return policy, nil
}

func (manager *Manager) ensureServiceAccount(ctx context.Context, desired *corev1.ServiceAccount) error {
	_, err := manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) ||
			!apiequality.Semantic.DeepEqual(existing.AutomountServiceAccountToken, desired.AutomountServiceAccountToken) {
			return errors.New("existing runtime ServiceAccount conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime ServiceAccount")
	}
	return nil
}

func (manager *Manager) ensureRole(ctx context.Context, desired *rbacv1.Role) error {
	_, err := manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) || !apiequality.Semantic.DeepEqual(existing.Rules, desired.Rules) {
			return errors.New("existing runtime Role conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime Role")
	}
	return nil
}

func (manager *Manager) ensureRoleBinding(ctx context.Context, desired *rbacv1.RoleBinding) error {
	_, err := manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) ||
			!apiequality.Semantic.DeepEqual(existing.Subjects, desired.Subjects) || existing.RoleRef != desired.RoleRef {
			return errors.New("existing runtime RoleBinding conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime RoleBinding")
	}
	return nil
}

func (manager *Manager) ensureNetworkPolicy(ctx context.Context, desired *networkingv1.NetworkPolicy) error {
	_, err := manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) || !apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
			return errors.New("existing runtime NetworkPolicy conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create runtime NetworkPolicy")
	}
	return nil
}

func (manager *Manager) ensureExecutionRBACAbsent(ctx context.Context, input runtimecontract.RunnerInput) error {
	if _, err := manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeRoleName(input.LeaseRef), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		return errors.New("runtime Role exists for a no-access execution")
	}
	if _, err := manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Get(ctx, runtimecontract.RuntimeRoleBindingName(input.LeaseRef), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		return errors.New("runtime RoleBinding exists for a no-access execution")
	}
	return nil
}

func managedMetadataMatches(existing, desired metav1.ObjectMeta) bool {
	for key, value := range desired.Labels {
		if existing.Labels[key] != value {
			return false
		}
	}
	for key, value := range desired.Annotations {
		if existing.Annotations[key] != value {
			return false
		}
	}
	return true
}

func (manager *Manager) EnsureWarm(ctx context.Context, input runtimecontract.RunnerInput, providerBinding ProviderSecretBinding) (bool, error) {
	if input.Mode != runtimecontract.RunnerModeWarm || input.Validate() != nil || manager.validateImage(input) != nil ||
		input.EnvironmentPolicy.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone ||
		input.EffectiveKubernetesAccess.ServiceAccountName != manager.config.RunnerServiceAccount {
		return false, errors.New("warm runtime input is invalid")
	}
	if err := manager.validateProviderSecret(ctx, input, providerBinding); err != nil {
		return false, err
	}
	environmentSecrets, err := manager.materializeEnvironmentSecrets(ctx, input)
	if err != nil {
		return false, err
	}
	if err := manager.ensureSessionPVC(ctx, input); err != nil {
		return false, err
	}
	const podName = "system-assistant-warm"
	secretName := manager.warmTicketName(input.RuntimeRevisionRef, input.RuntimeRevisionDigest)
	compatibilityDigest, _ := runtimecontract.WarmCompatibilityDigest(input)
	existing, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil && (existing.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest ||
		existing.Annotations[warmCompatibilityAnnotation] != compatibilityDigest ||
		existing.Annotations[controllerAnnotation] != manager.config.ControllerPodUID ||
		runtimePodTerminal(existing)) {
		boundTicket := runtimeInputSecretName(existing)
		if deleteErr := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, podName, metav1.DeleteOptions{GracePeriodSeconds: int64Pointer(0)}); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			return false, errors.New("replace stale warm runtime pod")
		}
		if boundTicket != "" {
			if deleteErr := manager.deleteOwnedWarmTicket(ctx, boundTicket); deleteErr != nil {
				return false, deleteErr
			}
		}
		// Pod deletion is asynchronous. Recreating an immutable Secret with the
		// same name while the old Pod still runs would make its mounted token
		// differ from the callback verifier. A later reconciliation creates the
		// fresh ticket and Pod only after Kubernetes reports the old Pod absent.
		return false, nil
	}
	if apierrors.IsNotFound(err) {
		if ticketErr := manager.removeConflictingWarmTicket(ctx, secretName, input, environmentSecrets, &providerBinding); ticketErr != nil {
			return false, ticketErr
		}
		token, ticketErr := newTicket()
		if ticketErr != nil {
			return false, ticketErr
		}
		if ticketErr = manager.ensureTicket(ctx, secretName, podName, "warm", input, token, environmentSecrets, &providerBinding); ticketErr != nil {
			return false, ticketErr
		}
		if projectionErr := manager.ensureProjection(ctx, podName, "warm", input); projectionErr != nil {
			return false, projectionErr
		}
		pod := manager.runtimePod(input, providerBinding, secretName, podName, "warm")
		existing, err = manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Create(ctx, pod, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, err = manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
		}
		if err != nil {
			return false, fmt.Errorf("create warm runtime Pod: %w", err)
		}
	} else if err != nil {
		return false, errors.New("read warm runtime pod")
	}
	if err := manager.cleanupStaleWarmTickets(ctx, secretName); err != nil {
		return false, err
	}
	if err := manager.cleanupStaleWarmProjections(ctx, runtimeProjectionName(input)); err != nil {
		return false, err
	}
	return podReady(existing), nil
}

func (manager *Manager) deleteOwnedWarmTicket(ctx context.Context, name string) error {
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read stale warm runtime ticket")
	}
	if secret.Labels[managedLabel] != "true" || secret.Labels[modeLabel] != "warm" {
		return errors.New("stale warm runtime ticket ownership is invalid")
	}
	if input, decodeErr := runtimecontract.DecodeRunnerInput(secret.Data[inputKey]); decodeErr == nil {
		if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, runtimeProjectionName(input), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return errors.New("delete stale warm runtime projection")
		}
	}
	if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("delete stale warm runtime ticket")
	}
	return nil
}

func (manager *Manager) cleanupStaleWarmProjections(ctx context.Context, current string) error {
	selector := labels.Set{managedLabel: "true", modeLabel: "warm"}.AsSelector().String()
	projections, err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256})
	if err != nil {
		return errors.New("list stale warm runtime projections")
	}
	var result error
	for index := range projections.Items {
		if projections.Items[index].Name == current {
			continue
		}
		if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, projections.Items[index].Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete stale warm runtime projection"))
		}
	}
	return result
}

func (manager *Manager) cleanupStaleWarmTickets(ctx context.Context, current string) error {
	selector := labels.Set{managedLabel: "true", modeLabel: "warm"}.AsSelector().String()
	secrets, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: 256})
	if err != nil {
		return errors.New("list stale warm runtime tickets")
	}
	var result error
	for index := range secrets.Items {
		if secrets.Items[index].Name == current {
			continue
		}
		if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, secrets.Items[index].Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
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
		if volume.Name == "runtime-ticket" && volume.Secret != nil {
			return volume.Secret.SecretName
		}
	}
	return ""
}

func (manager *Manager) removeConflictingWarmTicket(ctx context.Context, secretName string, input runtimecontract.RunnerInput, environmentSecrets map[string][]byte, providerBinding *ProviderSecretBinding) error {
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return errors.New("read warm runtime ticket")
	}
	if runtimeTicketMatches(secret, "system-assistant-warm", "warm", input, "", environmentSecrets, providerBinding) {
		return nil
	}
	if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return errors.New("replace stale warm runtime ticket")
	}
	return nil
}

func (manager *Manager) RegisterWarmTurn(ctx context.Context, input runtimecontract.RunnerInput, token string) error {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil || token == "" {
		return errors.New("warm turn registration is invalid")
	}
	binding, err := manager.warmProviderSecretBinding(ctx, input)
	if err != nil {
		return err
	}
	return manager.ensureTicket(ctx, ticketName(input.LeaseRef), "system-assistant-warm", "warm-turn", input, token, nil, &binding)
}

func (manager *Manager) warmProviderSecretBinding(ctx context.Context, input runtimecontract.RunnerInput) (ProviderSecretBinding, error) {
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, "system-assistant-warm", metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read warm runtime Pod credential binding")
	}
	name := runtimeInputSecretName(pod)
	if name == "" {
		return ProviderSecretBinding{}, errors.New("warm runtime Pod credential binding is missing")
	}
	ticket, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read warm runtime credential binding")
	}
	bound, err := runtimecontract.DecodeRunnerInput(ticket.Data[inputKey])
	if err != nil || bound.Mode != runtimecontract.RunnerModeWarm ||
		bound.ProviderAccountRef != input.ProviderAccountRef ||
		bound.ProviderCredentialRef != input.ProviderCredentialRef ||
		bound.ProviderCredentialSHA256 != input.ProviderCredentialSHA256 {
		return ProviderSecretBinding{}, errors.New("warm runtime credential binding is invalid")
	}
	binding, err := providerSecretBindingFromTicket(ticket, bound)
	if err != nil || manager.validateProviderSecret(ctx, input, binding) != nil {
		return ProviderSecretBinding{}, errors.New("warm runtime provider credential is unavailable")
	}
	return binding, nil
}

func (manager *Manager) WarmTicket(ctx context.Context, revisionRef, revisionDigest string) (string, error) {
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, manager.warmTicketName(revisionRef, revisionDigest), metav1.GetOptions{})
	if err != nil || len(secret.Data[ticketKey]) < 32 {
		return "", errors.New("read warm runtime ticket")
	}
	return string(secret.Data[ticketKey]), nil
}

func (manager *Manager) ResolveWarm(ctx context.Context, revisionRef, revisionDigest, token string) (runtimecontract.RunnerInput, error) {
	if revisionRef == "" || len(revisionDigest) != sha256.Size*2 || token == "" {
		return runtimecontract.RunnerInput{}, errors.New("warm runtime callback authority is incomplete")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, manager.warmTicketName(revisionRef, revisionDigest), metav1.GetOptions{})
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
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, ticketName(leaseRef), metav1.GetOptions{})
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
	secret, _ := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, secretName, metav1.GetOptions{})
	podName := runtimecontract.RuntimeTurnPodName(leaseRef)
	if secret != nil {
		if boundName := secret.Annotations[podAnnotation]; boundName != "" {
			podName = boundName
		}
	}
	var result error
	if podName != "" && podName != "system-assistant-warm" {
		if err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Delete(ctx, podName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete completed runtime pod"))
		}
	}
	if err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Delete(ctx, secretName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		result = errors.Join(result, errors.New("delete completed runtime ticket"))
	}
	projectionName := "runtime-projection-" + shortHash(leaseRef)
	if err := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Delete(ctx, projectionName, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		result = errors.Join(result, errors.New("delete completed runtime VFS projection"))
	}
	if err := manager.deleteExecutionPolicy(ctx, leaseRef); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

func (manager *Manager) deleteExecutionPolicy(ctx context.Context, leaseRef string) error {
	return manager.deleteExecutionPolicyByHash(ctx, shortHash(leaseRef))
}

func (manager *Manager) deleteExecutionPolicyByHash(ctx context.Context, executionHash string) error {
	if len(executionHash) != 16 {
		return errors.New("runtime execution identity is unavailable for policy cleanup")
	}
	var result error
	deletions := []struct {
		name   string
		remove func(string) error
	}{
		{name: "runtime-net-" + executionHash, remove: func(name string) error {
			return manager.client.NetworkingV1().NetworkPolicies(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
		{name: "runtime-rb-" + executionHash, remove: func(name string) error {
			return manager.client.RbacV1().RoleBindings(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
		{name: "runtime-role-" + executionHash, remove: func(name string) error {
			return manager.client.RbacV1().Roles(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
		{name: "runtime-sa-" + executionHash, remove: func(name string) error {
			return manager.client.CoreV1().ServiceAccounts(manager.config.RuntimeNamespace).Delete(ctx, name, metav1.DeleteOptions{})
		}},
	}
	for _, deletion := range deletions {
		if err := deletion.remove(deletion.name); err != nil && !apierrors.IsNotFound(err) {
			result = errors.Join(result, errors.New("delete completed runtime execution policy"))
		}
	}
	return result
}

type TurnPodObservation struct {
	State          string
	DiagnosticCode string
}

func (manager *Manager) ObserveTurnPod(ctx context.Context, input runtimecontract.RunnerInput, warmExecution bool) (TurnPodObservation, error) {
	podName := runtimecontract.RuntimeTurnPodName(input.LeaseRef)
	if warmExecution {
		podName = "system-assistant-warm"
	}
	pod, err := manager.client.CoreV1().Pods(manager.config.RuntimeNamespace).Get(ctx, podName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return TurnPodObservation{State: "MISSING", DiagnosticCode: "POD_MISSING"}, nil
	}
	if err != nil {
		return TurnPodObservation{}, errors.New("read runtime execution pod")
	}
	if warmExecution {
		compatibility, compatibilityErr := runtimecontract.WarmCompatibilityDigest(input)
		if compatibilityErr != nil || pod.Annotations[warmCompatibilityAnnotation] != compatibility {
			return TurnPodObservation{State: "CONFLICT", DiagnosticCode: "WARM_COMPATIBILITY_CONFLICT"}, nil
		}
	} else if pod.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest {
		return TurnPodObservation{State: "CONFLICT", DiagnosticCode: "RUNTIME_REVISION_CONFLICT"}, nil
	}
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		return TurnPodObservation{State: "SUCCEEDED", DiagnosticCode: "POD_SUCCEEDED_WITHOUT_CALLBACK"}, nil
	case corev1.PodFailed:
		return TurnPodObservation{State: "FAILED", DiagnosticCode: terminalContainerDiagnostic(pod)}, nil
	case corev1.PodRunning:
		if !warmExecution && runtimePodTerminal(pod, "role-runtime", "provider-runtime", "provider-credential-relay") {
			return TurnPodObservation{State: "FAILED", DiagnosticCode: terminalContainerDiagnostic(pod)}, nil
		}
		if podReady(pod) {
			return TurnPodObservation{State: "READY"}, nil
		}
		return TurnPodObservation{State: "STARTING"}, nil
	case corev1.PodPending:
		return TurnPodObservation{State: "STARTING"}, nil
	default:
		return TurnPodObservation{State: "UNKNOWN"}, nil
	}
}

func (manager *Manager) TurnPodState(ctx context.Context, input runtimecontract.RunnerInput, warmExecution bool) (string, error) {
	observation, err := manager.ObserveTurnPod(ctx, input, warmExecution)
	return observation.State, err
}

func terminalContainerDiagnostic(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Terminated == nil {
			continue
		}
		switch status.Name {
		case "role-runtime":
			if status.State.Terminated.ExitCode == 0 {
				return "ROLE_RUNTIME_EXITED_ZERO"
			}
			return "ROLE_RUNTIME_EXITED_NONZERO"
		case "provider-runtime":
			return "PROVIDER_RUNTIME_EXITED"
		case "provider-credential-relay":
			return "PROVIDER_CREDENTIAL_RELAY_EXITED"
		}
	}
	return "POD_FAILED"
}

func runtimePodMatches(existing, desired *corev1.Pod) bool {
	if existing == nil || desired == nil || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) ||
		existing.Spec.ServiceAccountName != desired.Spec.ServiceAccountName ||
		!apiequality.Semantic.DeepEqual(existing.Spec.AutomountServiceAccountToken, desired.Spec.AutomountServiceAccountToken) ||
		!apiequality.Semantic.DeepEqual(canonicalRuntimeContainers(existing.Spec.InitContainers), canonicalRuntimeContainers(desired.Spec.InitContainers)) ||
		!apiequality.Semantic.DeepEqual(canonicalRuntimeContainers(existing.Spec.Containers), canonicalRuntimeContainers(desired.Spec.Containers)) ||
		!apiequality.Semantic.DeepEqual(existing.Spec.Volumes, desired.Spec.Volumes) {
		return false
	}
	return true
}

func canonicalRuntimeContainers(values []corev1.Container) []corev1.Container {
	result := make([]corev1.Container, len(values))
	for index := range values {
		result[index] = *values[index].DeepCopy()
		result[index].TerminationMessagePath = ""
		result[index].TerminationMessagePolicy = ""
		for portIndex := range result[index].Ports {
			result[index].Ports[portIndex].Protocol = ""
		}
		for _, probe := range []*corev1.Probe{result[index].StartupProbe, result[index].ReadinessProbe, result[index].LivenessProbe} {
			if probe != nil {
				probe.SuccessThreshold = 0
			}
		}
	}
	return result
}

func runtimeProjectionName(input runtimecontract.RunnerInput) string {
	identity := input.LeaseRef
	if identity == "" {
		identity = input.RuntimeRevisionRef + "|" + input.RuntimeRevisionDigest + "|" + input.WorkloadInstance
	}
	return "runtime-projection-" + shortHash(identity)
}

func runtimeProjectionData(input runtimecontract.RunnerInput) (map[string]string, error) {
	runtimeRaw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		return nil, err
	}
	workspaceRaw, err := json.Marshal(input.WorkspacePolicy)
	if err != nil {
		return nil, errors.New("encode runtime workspace policy")
	}
	inputs, err := runtimecontract.BuildWorkspaceAttachmentManifest(input.AttachmentSets, input.InputArtifacts)
	if err != nil {
		return nil, errors.New("encode runtime input projection")
	}
	memories := make([]runtimecontract.RunnerInputArtifact, 0)
	for _, artifact := range input.InputArtifacts {
		if artifact.Scope == runtimecontract.AttachmentScopeKnowledge {
			memories = append(memories, artifact)
		}
	}
	identity := map[string]any{
		"organization_ref": input.OrganizationRef, "project_ref": input.ProjectRef, "run_ref": input.RunRef, "node_ref": input.NodeRef,
		"session_ref": input.SessionRef, "turn_ref": input.TurnRef, "attempt": input.Attempt,
		"runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_version": input.RuntimeRevisionVersion,
		"runtime_revision_digest": input.RuntimeRevisionDigest, "input_digest": input.InputDigest,
	}
	encode := func(value any, diagnostic string) (string, error) {
		raw, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			return "", errors.New(diagnostic)
		}
		return string(raw), nil
	}
	skills, err := encode(map[string]any{"identity": identity, "tools": input.EnvironmentTools}, "encode runtime skill projection")
	if err != nil {
		return nil, err
	}
	memory, err := encode(map[string]any{"identity": identity, "artifacts": memories}, "encode runtime memory projection")
	if err != nil {
		return nil, err
	}
	mcp, err := encode(map[string]any{"identity": identity, "binding_digest": input.MCPBindingDigest, "capabilities": input.Capabilities, "grants": input.IntegrationGrants}, "encode runtime MCP projection")
	if err != nil {
		return nil, err
	}
	callback, err := encode(map[string]any{"identity": identity, "binding_digest": input.ExecutionBindingDigest, "url": input.CallbackURL, "tls_server_name": input.CallbackTLS.ServerName}, "encode runtime callback projection")
	if err != nil {
		return nil, err
	}
	results, err := encode(map[string]any{"identity": identity, "root": input.OutboxRoot, "maximum_writable_bytes": input.WorkspacePolicy.MaximumWritableBytes, "maximum_file_count": input.WorkspacePolicy.MaximumFileCount}, "encode runtime result projection")
	if err != nil {
		return nil, err
	}
	return map[string]string{
		inputKey: string(runtimeRaw), workspacePolicyKey: string(workspaceRaw), inputManifestKey: string(inputs.Bytes),
		resultManifestKey: results, skillManifestKey: skills, memoryManifestKey: memory,
		mcpManifestKey: mcp, callbackManifestKey: callback,
	}, nil
}

func runtimeProjectionAnnotations(input runtimecontract.RunnerInput, podName string) map[string]string {
	return map[string]string{
		revisionAnnotation: input.RuntimeRevisionDigest, configAnnotation: input.RuntimeConfigDigest,
		environmentAnnotation: input.RuntimeEnvironmentDigest, workspacePolicyAnnotation: input.WorkspacePolicy.Digest,
		executionBindingAnnotation: input.ExecutionBindingDigest, mcpBindingAnnotation: input.MCPBindingDigest,
		controllerAnnotation: input.WorkloadInstance, podAnnotation: podName,
		resourcesAnnotation: input.EnvironmentPolicy.ResourcesDigest, volumesAnnotation: input.EnvironmentPolicy.VolumesDigest,
		networkAnnotation: input.EnvironmentPolicy.NetworkDigest, rbacProfileAnnotation: input.EnvironmentPolicy.RBACDigest,
		effectiveRBACAnnotation:    input.EffectiveKubernetesAccess.Digest,
		organizationHashAnnotation: shortHash(input.OrganizationRef), projectHashAnnotation: shortHash(input.ProjectRef), sessionHashAnnotation: shortHash(input.SessionRef),
		turnHashAnnotation: shortHash(input.TurnRef), attemptAnnotation: strconv.FormatInt(int64(input.Attempt), 10),
	}
}

func (manager *Manager) ensureProjection(ctx context.Context, podName, mode string, input runtimecontract.RunnerInput) error {
	data, err := runtimeProjectionData(input)
	if err != nil {
		return err
	}
	if stringDataSize(data) > maximumKubernetesProjectionBytes {
		return errors.New("runtime revision projection exceeds Kubernetes object limit")
	}
	immutable := true
	desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: runtimeProjectionName(input), Namespace: manager.config.RuntimeNamespace,
		Labels: map[string]string{managedLabel: "true", modeLabel: mode}, Annotations: runtimeProjectionAnnotations(input, podName)},
		Immutable: &immutable, Data: data}
	_, err = manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().ConfigMaps(manager.config.RuntimeNamespace).Get(ctx, desired.Name, metav1.GetOptions{})
		if getErr != nil || existing.Immutable == nil || !*existing.Immutable || !managedMetadataMatches(existing.ObjectMeta, desired.ObjectMeta) || !apiequality.Semantic.DeepEqual(existing.Data, desired.Data) || len(existing.BinaryData) != 0 {
			return errors.New("existing runtime projection conflicts with immutable revision")
		}
		return nil
	}
	if err != nil {
		return errors.New("create immutable runtime projection")
	}
	return nil
}

func (manager *Manager) ensureTicket(ctx context.Context, name, podName, mode string, input runtimecontract.RunnerInput, token string, environmentSecrets map[string][]byte, providerBinding *ProviderSecretBinding) error {
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil {
		return err
	}
	immutable := true
	data := map[string][]byte{inputKey: raw, ticketKey: []byte(token)}
	for key, value := range environmentSecrets {
		data[key] = append([]byte(nil), value...)
	}
	if byteDataSize(data) > maximumKubernetesProjectionBytes {
		return errors.New("runtime execution ticket exceeds Kubernetes object limit")
	}
	annotations := runtimeProjectionAnnotations(input, podName)
	if providerBinding != nil {
		annotations[providerSecretNameAnnotation] = providerBinding.Name
		annotations[providerSecretUIDAnnotation] = providerBinding.UID
		annotations[providerSecretResourceVersionAnnotation] = providerBinding.ResourceVersion
		annotations[providerAccountRefAnnotation] = input.ProviderAccountRef
		annotations[providerCredentialRefAnnotation] = input.ProviderCredentialRef
		annotations[providerCredentialDigestAnnotation] = providerBinding.ContentSHA256
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: manager.config.RuntimeNamespace,
		Labels: map[string]string{managedLabel: "true", modeLabel: mode}, Annotations: annotations},
		Immutable: &immutable, Type: corev1.SecretTypeOpaque, Data: data}
	_, err = manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil || !runtimeTicketMatches(existing, podName, mode, input, token, environmentSecrets, providerBinding) {
			return errors.New("existing runtime ticket conflicts with immutable execution")
		}
		return nil
	}
	if err != nil {
		return errors.New("create immutable runtime ticket")
	}
	return nil
}

func stringDataSize(data map[string]string) int {
	result := 0
	for key, value := range data {
		result += len(key) + len(value)
	}
	return result
}

func byteDataSize(data map[string][]byte) int {
	result := 0
	for key, value := range data {
		result += len(key) + len(value)
	}
	return result
}

func runtimeTicketMatches(existing *corev1.Secret, podName, mode string, input runtimecontract.RunnerInput, token string, environmentSecrets map[string][]byte, providerBinding *ProviderSecretBinding) bool {
	expectedMetadata := metav1.ObjectMeta{Labels: map[string]string{managedLabel: "true", modeLabel: mode}, Annotations: runtimeProjectionAnnotations(input, podName)}
	if existing == nil || existing.Immutable == nil || !*existing.Immutable || !managedMetadataMatches(existing.ObjectMeta, expectedMetadata) ||
		existing.Labels[managedLabel] != "true" || existing.Labels[modeLabel] != mode ||
		existing.Annotations[revisionAnnotation] != input.RuntimeRevisionDigest ||
		existing.Annotations[configAnnotation] != input.RuntimeConfigDigest ||
		existing.Annotations[environmentAnnotation] != input.RuntimeEnvironmentDigest ||
		existing.Annotations[controllerAnnotation] != input.WorkloadInstance ||
		existing.Annotations[podAnnotation] != podName ||
		existing.Annotations[resourcesAnnotation] != input.EnvironmentPolicy.ResourcesDigest ||
		existing.Annotations[volumesAnnotation] != input.EnvironmentPolicy.VolumesDigest ||
		existing.Annotations[networkAnnotation] != input.EnvironmentPolicy.NetworkDigest ||
		existing.Annotations[rbacProfileAnnotation] != input.EnvironmentPolicy.RBACDigest ||
		existing.Annotations[effectiveRBACAnnotation] != input.EffectiveKubernetesAccess.Digest ||
		!providerTicketBindingMatches(existing, input, providerBinding) ||
		len(existing.Data) != len(environmentSecrets)+2 {
		return false
	}
	raw, err := runtimecontract.EncodeRunnerInput(input)
	if err != nil || subtle.ConstantTimeCompare(existing.Data[inputKey], raw) != 1 {
		return false
	}
	if len(existing.Data[ticketKey]) != 64 {
		return false
	}
	if _, err := hex.DecodeString(string(existing.Data[ticketKey])); err != nil {
		return false
	}
	if mode == "warm-turn" && subtle.ConstantTimeCompare(existing.Data[ticketKey], []byte(token)) != 1 {
		return false
	}
	for key, value := range environmentSecrets {
		if subtle.ConstantTimeCompare(existing.Data[key], value) != 1 {
			return false
		}
	}
	return true
}

func (manager *Manager) ensureSessionPVC(ctx context.Context, input runtimecontract.RunnerInput) error {
	name, err := runtimecontract.SessionPVCName(input.SessionRef)
	if err != nil {
		return err
	}
	existing, err := manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		request := existing.Spec.Resources.Requests[corev1.ResourceStorage]
		if existing.Labels[managedLabel] != "true" || existing.Labels[sessionHashAnnotation] != shortHash(input.SessionRef) ||
			existing.Annotations[organizationHashAnnotation] != shortHash(input.OrganizationRef) ||
			existing.Annotations[projectHashAnnotation] != shortHash(input.ProjectRef) || request.Cmp(manager.pvcRequest) != 0 ||
			len(existing.Spec.AccessModes) != 1 || existing.Spec.AccessModes[0] != corev1.ReadWriteOnce ||
			existing.Spec.Selector != nil || existing.Spec.DataSource != nil || existing.Spec.DataSourceRef != nil ||
			(manager.config.StorageClass != "" && (existing.Spec.StorageClassName == nil || *existing.Spec.StorageClassName != manager.config.StorageClass)) {
			return errors.New("existing runtime session volume conflicts with exact session binding")
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return errors.New("read runtime session volume")
	}
	var storageClassName *string
	if manager.config.StorageClass != "" {
		storageClassName = &manager.config.StorageClass
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: manager.config.RuntimeNamespace,
		Labels:      map[string]string{managedLabel: "true", sessionHashAnnotation: shortHash(input.SessionRef)},
		Annotations: map[string]string{organizationHashAnnotation: shortHash(input.OrganizationRef), projectHashAnnotation: shortHash(input.ProjectRef)}},
		Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: storageClassName,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: manager.pvcRequest}}}}
	_, err = manager.client.CoreV1().PersistentVolumeClaims(manager.config.RuntimeNamespace).Create(ctx, pvc, metav1.CreateOptions{})
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
	workspaceLimit := *resource.NewQuantity(input.WorkspacePolicy.MaximumWritableBytes, resource.BinarySI)
	volumes := []corev1.Volume{
		{Name: "session", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: sessionVolumeName}}},
		{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workspaceLimit}}},
		{Name: "vfs-input", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workspaceLimit}}},
		{Name: "vfs-knowledge", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &workspaceLimit}}},
		{Name: "runtime-input", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: runtimeProjectionName(input)}, DefaultMode: int32Pointer(0o440)}}},
		{Name: "runtime-ticket", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: ticketSecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "callback-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: manager.config.CallbackClientCASecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "callback-client", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: manager.config.CallbackClientTLSSecret, DefaultMode: int32Pointer(0o440)}}},
		{Name: "provider-auth", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: providerBinding.Name, DefaultMode: int32Pointer(0o400)}}},
		{Name: "provider-socket", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		{Name: "provider-credential-relay", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("8Mi"))}}},
		{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("512Mi"))}}},
		{Name: "provider-tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(resource.MustParse("512Mi"))}}},
	}
	materializerWorkspaceMounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: "session", MountPath: "/workspace/.kodex/state"},
		{Name: "vfs-input", MountPath: "/workspace/input"},
		{Name: "vfs-knowledge", MountPath: "/workspace/knowledge"},
	}
	sandboxWorkspaceMounts := []corev1.VolumeMount{
		{Name: "workspace", MountPath: "/workspace"},
		{Name: "session", MountPath: "/workspace/.kodex/state"},
		{Name: "vfs-input", MountPath: "/workspace/input", ReadOnly: true},
		{Name: "vfs-knowledge", MountPath: "/workspace/knowledge", ReadOnly: true},
	}
	roleMounts := append([]corev1.VolumeMount(nil), materializerWorkspaceMounts...)
	roleMounts = append(roleMounts, corev1.VolumeMount{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true}, corev1.VolumeMount{Name: "runtime-ticket", MountPath: "/var/run/secrets/kodex/runtime/ticket/token", SubPath: ticketKey, ReadOnly: true}, corev1.VolumeMount{Name: "callback-ca", MountPath: "/var/run/config/kodex/runtime/callback", ReadOnly: true}, corev1.VolumeMount{Name: "callback-client", MountPath: "/var/run/secrets/kodex/runtime/callback-client", ReadOnly: true}, corev1.VolumeMount{Name: "provider-socket", MountPath: "/run/kodex/provider"}, corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"})
	initMounts := append([]corev1.VolumeMount(nil), materializerWorkspaceMounts...)
	initMounts = append(initMounts, corev1.VolumeMount{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true}, corev1.VolumeMount{Name: "tmp", MountPath: "/tmp"})
	for _, item := range input.EnvironmentPolicy.Volumes {
		volumeName := "environment-" + shortHash(item.Name)
		source := &corev1.EmptyDirVolumeSource{SizeLimit: quantityPointer(*resource.NewQuantity(item.SizeMiB<<20, resource.BinarySI))}
		if item.Kind == runtimecontract.RuntimeVolumeEphemeralMemory {
			source.Medium = corev1.StorageMediumMemory
		}
		volumes = append(volumes, corev1.Volume{Name: volumeName, VolumeSource: corev1.VolumeSource{EmptyDir: source}})
		roleMounts = append(roleMounts, corev1.VolumeMount{Name: volumeName, MountPath: item.MountPath})
	}
	serviceAccountName := manager.config.RunnerServiceAccount
	if mode == "turn" {
		serviceAccountName = input.EffectiveKubernetesAccess.ServiceAccountName
	}
	if mode == "turn" && input.EffectiveKubernetesAccess.Profile.Kind == runtimecontract.RuntimeKubernetesAccessReadOwnExecution {
		expirationSeconds := int64(3600)
		volumes = append(volumes, corev1.Volume{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
			DefaultMode: int32Pointer(0o440), Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token", ExpirationSeconds: &expirationSeconds}},
				{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
				{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"}}}}},
			},
		}}})
		roleMounts = append(roleMounts, corev1.VolumeMount{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true})
	}
	resources := runtimePolicyResourceRequirements(input.EnvironmentPolicy.Resources)
	role := corev1.Container{Name: "role-runtime", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: roleArgs,
		Env: []corev1.EnvVar{
			{Name: "KODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/kodex/runtime/runtime.json"},
			{Name: "OTEL_SDK_DISABLED", Value: "true"},
			{Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment},
		},
		Ports: []corev1.ContainerPort{{Name: "runtime-health", ContainerPort: 9090}}, SecurityContext: restrictedSecurityContext(10001), VolumeMounts: roleMounts,
		Resources:    resources,
		StartupProbe: httpProbe("/readyz", "runtime-health", 2, 60), ReadinessProbe: httpProbe("/readyz", "runtime-health", 5, 3), LivenessProbe: httpProbe("/healthz", "runtime-health", 10, 3)}
	provider := corev1.Container{Name: "provider-runtime", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-provider"},
		Env: []corev1.EnvVar{{Name: "HOME", Value: "/tmp"}, {Name: "CODEX_HOME", Value: input.CodexHome},
			{Name: "HTTPS_PROXY", Value: manager.config.ProviderHTTPSProxy}, {Name: "HTTP_PROXY", Value: manager.config.ProviderHTTPSProxy},
			{Name: "NO_PROXY", Value: "127.0.0.1,localhost"}, {Name: "OTEL_SDK_DISABLED", Value: "true"}, {Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment}}, SecurityContext: providerSandboxSecurityContext(10002, manager.config.ProviderAppArmorProfile),
		VolumeMounts: append(append([]corev1.VolumeMount(nil), sandboxWorkspaceMounts...), []corev1.VolumeMount{
			{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true},
			{Name: "provider-auth", MountPath: "/run/secrets/kodex/runtime/provider/auth.json", SubPath: "auth.json", ReadOnly: true},
			{Name: "provider-auth", MountPath: "/run/secrets/kodex/runtime/provider/auth.sha256", SubPath: "auth.sha256", ReadOnly: true},
			{Name: "provider-socket", MountPath: "/run/kodex/provider"},
			{Name: "provider-credential-relay", MountPath: "/run/kodex/provider-credential-relay"},
			{Name: "provider-tmp", MountPath: "/tmp"},
		}...),
		Resources: smallResources(), ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/bin/test", "-S", "/run/kodex/provider/provider.sock"}}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30}}
	relay := corev1.Container{Name: "provider-credential-relay", Image: manager.config.DefaultRoleImageReference, ImagePullPolicy: corev1.PullIfNotPresent,
		Args: []string{"runtime-provider-credential-relay"},
		Env: []corev1.EnvVar{
			{Name: "KODEX_RUNTIME_REVISION_FILE", Value: "/var/run/config/kodex/runtime/runtime.json"},
			{Name: "OTEL_SDK_DISABLED", Value: "true"},
			{Name: "DEPLOYMENT_ENVIRONMENT", Value: manager.config.Environment},
		},
		SecurityContext: restrictedSecurityContext(10003),
		VolumeMounts: []corev1.VolumeMount{
			{Name: "runtime-input", MountPath: "/var/run/config/kodex/runtime", ReadOnly: true},
			{Name: "runtime-ticket", MountPath: "/var/run/secrets/kodex/runtime/ticket/token", SubPath: ticketKey, ReadOnly: true},
			{Name: "callback-ca", MountPath: "/var/run/config/kodex/runtime/callback", ReadOnly: true},
			{Name: "callback-client", MountPath: "/var/run/secrets/kodex/runtime/callback-client", ReadOnly: true},
			{Name: "provider-credential-relay", MountPath: "/run/kodex/provider-credential-relay"},
		},
		Resources: smallResources(), ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/bin/test", "-S", "/run/kodex/provider-credential-relay/relay.sock"}}}, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 30}}
	for _, item := range input.EnvironmentValues {
		provider.Env = append(provider.Env, corev1.EnvVar{Name: item.Name, Value: item.Value})
	}
	for _, item := range input.SecretProjections {
		provider.Env = append(provider.Env, corev1.EnvVar{Name: item.Name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: ticketSecret}, Key: environmentProjectionKey(item.Name), Optional: boolPointer(false),
		}}})
	}
	annotations := runtimeProjectionAnnotations(input, podName)
	labels := map[string]string{managedLabel: "true", modeLabel: mode, "app.kubernetes.io/name": "agent-runner", "app.kubernetes.io/component": "role-runtime", "kodex.dev/environment": manager.config.Environment}
	if mode == "turn" {
		annotations[leaseAnnotation] = input.LeaseRef
		labels[executionHashLabel] = shortHash(input.LeaseRef)
	}
	if mode == "warm" {
		compatibility, _ := runtimecontract.WarmCompatibilityDigest(input)
		annotations[warmCompatibilityAnnotation] = compatibility
	}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: manager.config.RuntimeNamespace,
		Labels:      labels,
		Annotations: annotations},
		Spec: corev1.PodSpec{ServiceAccountName: serviceAccountName, AutomountServiceAccountToken: boolPointer(false), EnableServiceLinks: boolPointer(false), RestartPolicy: corev1.RestartPolicyNever, TerminationGracePeriodSeconds: int64Pointer(30),
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: boolPointer(true), FSGroup: int64Pointer(29000), FSGroupChangePolicy: fsGroupChangePolicyPointer(corev1.FSGroupChangeOnRootMismatch), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			InitContainers:  []corev1.Container{{Name: "workspace-init", Image: input.ImageReference, ImagePullPolicy: corev1.PullIfNotPresent, Args: []string{"runtime-init-workspace"}, SecurityContext: restrictedSecurityContext(10001), VolumeMounts: initMounts, Resources: smallResources()}},
			Containers:      []corev1.Container{role, provider, relay}, Volumes: volumes}}
}

func runtimePolicyResourceRequirements(policy runtimecontract.RuntimeResourcePolicy) corev1.ResourceRequirements {
	requests := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(policy.CPURequestMilli, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(policy.MemoryRequestMiB<<20, resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(policy.EphemeralStorageRequestMiB<<20, resource.BinarySI),
	}
	limits := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(policy.CPULimitMilli, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(policy.MemoryLimitMiB<<20, resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(policy.EphemeralStorageLimitMiB<<20, resource.BinarySI),
	}
	return corev1.ResourceRequirements{Requests: requests, Limits: limits}
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
	_, err := manager.readProviderAuthentication(ctx, input, binding)
	return err
}

func (manager *Manager) readProviderAuthentication(ctx context.Context, input runtimecontract.RunnerInput, binding ProviderSecretBinding) ([]byte, error) {
	if !validDNSLabel(binding.Name) || binding.UID == "" || binding.ResourceVersion == "" ||
		binding.ContentSHA256 != input.ProviderCredentialSHA256 {
		return nil, errors.New("provider credential binding is invalid")
	}
	secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, binding.Name, metav1.GetOptions{})
	if err != nil || secret.Immutable == nil || !*secret.Immutable || string(secret.UID) != binding.UID ||
		secret.ResourceVersion != binding.ResourceVersion {
		return nil, errors.New("provider credential revision is unavailable")
	}
	authentication := secret.Data["auth.json"]
	digestFile := strings.TrimSpace(string(secret.Data["auth.sha256"]))
	digest := sha256.Sum256(authentication)
	actual := hex.EncodeToString(digest[:])
	if len(authentication) == 0 || len(authentication) > 1<<20 || digestFile != actual || actual != binding.ContentSHA256 {
		return nil, errors.New("provider credential revision digest is invalid")
	}
	return append([]byte(nil), authentication...), nil
}

// MaterializeProviderCredentialRefresh создаёт только новую immutable Secret;
// переключение current revision остаётся атомарной обязанностью control-plane.
func (manager *Manager) MaterializeProviderCredentialRefresh(ctx context.Context, input runtimecontract.RunnerInput, request runtimecontract.RunnerProviderCredentialRefreshRequest) (ProviderSecretBinding, error) {
	if input.Mode != runtimecontract.RunnerModeTurn || input.Validate() != nil || request.Validate() != nil ||
		request.RuntimeRevisionDigest != input.RuntimeRevisionDigest ||
		request.PreviousCredentialRevisionRef != input.ProviderCredentialRef ||
		request.PreviousContentSHA256 != input.ProviderCredentialSHA256 {
		return ProviderSecretBinding{}, ErrProviderCredentialRefreshRejected
	}
	ticket, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, ticketName(input.LeaseRef), metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read runtime provider credential binding")
	}
	oldBinding, err := providerSecretBindingFromTicket(ticket, input)
	if err != nil {
		return ProviderSecretBinding{}, fmt.Errorf("%w: pinned provider credential binding is invalid", ErrProviderCredentialRefreshRejected)
	}
	oldAuthentication, err := manager.readProviderAuthentication(ctx, input, oldBinding)
	if err != nil {
		return ProviderSecretBinding{}, fmt.Errorf("%w: pinned provider credential is unavailable", ErrProviderCredentialRefreshRejected)
	}
	defer clear(oldAuthentication)
	oldSnapshot, err := parseManagedChatGPTAuthentication(oldAuthentication)
	if err != nil {
		return ProviderSecretBinding{}, fmt.Errorf("%w: pinned provider credential is not managed OAuth", ErrProviderCredentialRefreshRejected)
	}
	newSnapshot, err := parseManagedChatGPTAuthentication(request.Authentication)
	if err != nil || subtle.ConstantTimeCompare([]byte(oldSnapshot.Tokens.AccountID), []byte(newSnapshot.Tokens.AccountID)) != 1 {
		return ProviderSecretBinding{}, fmt.Errorf("%w: refreshed provider account does not match", ErrProviderCredentialRefreshRejected)
	}
	digest := sha256.Sum256(request.Authentication)
	contentSHA256 := hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(contentSHA256), []byte(input.ProviderCredentialSHA256)) == 1 {
		return ProviderSecretBinding{}, fmt.Errorf("%w: provider credential did not change", ErrProviderCredentialRefreshRejected)
	}
	name := providerCredentialRefreshSecretName(input.ProviderAccountRef, contentSHA256)
	immutable := true
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: manager.config.RuntimeNamespace,
			Labels: map[string]string{managedLabel: "true", "app.kubernetes.io/part-of": "kodex", "app.kubernetes.io/managed-by": "runtime-controller"},
			Annotations: map[string]string{
				providerAccountRefAnnotation:       input.ProviderAccountRef,
				previousCredentialRefAnnotation:    input.ProviderCredentialRef,
				previousCredentialDigestAnnotation: input.ProviderCredentialSHA256,
				providerCredentialDigestAnnotation: contentSHA256,
			},
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"auth.json":   append([]byte(nil), request.Authentication...),
			"auth.sha256": []byte(contentSHA256),
		},
	}
	if _, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Create(ctx, desired, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return ProviderSecretBinding{}, errors.New("create refreshed provider credential Secret")
	}
	materialized, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return ProviderSecretBinding{}, errors.New("read back refreshed provider credential Secret")
	}
	return validateProviderCredentialRefreshReadback(materialized, desired, contentSHA256)
}

func parseManagedChatGPTAuthentication(authentication []byte) (managedChatGPTAuthentication, error) {
	var snapshot managedChatGPTAuthentication
	var root map[string]json.RawMessage
	var tokens map[string]json.RawMessage
	if len(authentication) == 0 || len(authentication) > runtimecontract.MaximumProviderAuthBytes || rejectDuplicateJSONKeys(authentication) != nil ||
		json.Unmarshal(authentication, &root) != nil || json.Unmarshal(root["auth_mode"], &snapshot.AuthMode) != nil ||
		json.Unmarshal(root["tokens"], &tokens) != nil ||
		json.Unmarshal(tokens["account_id"], &snapshot.Tokens.AccountID) != nil ||
		json.Unmarshal(tokens["access_token"], &snapshot.Tokens.AccessToken) != nil ||
		json.Unmarshal(tokens["refresh_token"], &snapshot.Tokens.RefreshToken) != nil || snapshot.AuthMode != "chatgpt" ||
		snapshot.Tokens.AccountID == "" || len(snapshot.Tokens.AccountID) > 512 ||
		strings.TrimSpace(snapshot.Tokens.AccountID) != snapshot.Tokens.AccountID ||
		snapshot.Tokens.AccessToken == "" || strings.TrimSpace(snapshot.Tokens.AccessToken) != snapshot.Tokens.AccessToken ||
		snapshot.Tokens.RefreshToken == "" || strings.TrimSpace(snapshot.Tokens.RefreshToken) != snapshot.Tokens.RefreshToken {
		return managedChatGPTAuthentication{}, errors.New("managed ChatGPT authentication is invalid")
	}
	return snapshot, nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := consumeUniqueJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("managed ChatGPT authentication has trailing JSON")
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return errors.New("managed ChatGPT authentication object is invalid")
			}
			if _, duplicate := keys[key]; duplicate {
				return errors.New("managed ChatGPT authentication has duplicate keys")
			}
			keys[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return errors.New("managed ChatGPT authentication JSON is invalid")
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	expected := json.Delim('}')
	if delimiter == '[' {
		expected = ']'
	}
	if closing != expected {
		return errors.New("managed ChatGPT authentication JSON is invalid")
	}
	return nil
}

func providerCredentialRefreshSecretName(accountRef, digest string) string {
	return "runtime-provider-refresh-" + shortHash(accountRef) + "-" + digest[:12]
}

func validateProviderCredentialRefreshReadback(actual, desired *corev1.Secret, digest string) (ProviderSecretBinding, error) {
	if actual == nil || desired == nil || actual.Name != desired.Name || actual.Namespace != desired.Namespace ||
		actual.UID == "" || actual.ResourceVersion == "" || actual.Immutable == nil || !*actual.Immutable ||
		actual.Type != corev1.SecretTypeOpaque || len(actual.Data) != 2 ||
		actual.Labels[managedLabel] != "true" || actual.Labels["app.kubernetes.io/part-of"] != "kodex" ||
		actual.Labels["app.kubernetes.io/managed-by"] != "runtime-controller" ||
		actual.Annotations[providerAccountRefAnnotation] != desired.Annotations[providerAccountRefAnnotation] ||
		actual.Annotations[previousCredentialRefAnnotation] != desired.Annotations[previousCredentialRefAnnotation] ||
		actual.Annotations[previousCredentialDigestAnnotation] != desired.Annotations[previousCredentialDigestAnnotation] ||
		actual.Annotations[providerCredentialDigestAnnotation] != digest ||
		subtle.ConstantTimeCompare(actual.Data["auth.json"], desired.Data["auth.json"]) != 1 ||
		strings.TrimSpace(string(actual.Data["auth.sha256"])) != digest {
		return ProviderSecretBinding{}, fmt.Errorf("%w: refreshed provider credential readback does not match", ErrProviderCredentialRefreshRejected)
	}
	computed := sha256.Sum256(actual.Data["auth.json"])
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(computed[:])), []byte(digest)) != 1 {
		return ProviderSecretBinding{}, fmt.Errorf("%w: refreshed provider credential digest does not match", ErrProviderCredentialRefreshRejected)
	}
	return ProviderSecretBinding{Name: actual.Name, UID: string(actual.UID), ResourceVersion: actual.ResourceVersion, ContentSHA256: digest}, nil
}

func providerSecretBindingFromTicket(ticket *corev1.Secret, input runtimecontract.RunnerInput) (ProviderSecretBinding, error) {
	if ticket == nil || ticket.Immutable == nil || !*ticket.Immutable ||
		ticket.Annotations[providerAccountRefAnnotation] != input.ProviderAccountRef ||
		ticket.Annotations[providerCredentialRefAnnotation] != input.ProviderCredentialRef ||
		ticket.Annotations[providerCredentialDigestAnnotation] != input.ProviderCredentialSHA256 {
		return ProviderSecretBinding{}, errors.New("runtime provider credential ticket binding is invalid")
	}
	binding := ProviderSecretBinding{
		Name:            ticket.Annotations[providerSecretNameAnnotation],
		UID:             ticket.Annotations[providerSecretUIDAnnotation],
		ResourceVersion: ticket.Annotations[providerSecretResourceVersionAnnotation],
		ContentSHA256:   ticket.Annotations[providerCredentialDigestAnnotation],
	}
	if !validDNSLabel(binding.Name) || binding.UID == "" || binding.ResourceVersion == "" || len(binding.ContentSHA256) != sha256.Size*2 {
		return ProviderSecretBinding{}, errors.New("runtime provider credential ticket binding is incomplete")
	}
	return binding, nil
}

func providerTicketBindingMatches(ticket *corev1.Secret, input runtimecontract.RunnerInput, binding *ProviderSecretBinding) bool {
	if binding == nil {
		return true
	}
	actual, err := providerSecretBindingFromTicket(ticket, input)
	return err == nil && actual == *binding
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
		secret, err := manager.client.CoreV1().Secrets(manager.config.RuntimeNamespace).Get(ctx, item.SecretName, metav1.GetOptions{})
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
func turnPodName(value string) string                            { return runtimecontract.RuntimeTurnPodName(value) }
func int64Pointer(value int64) *int64                            { return &value }
func int32Pointer(value int32) *int32                            { return &value }
func boolPointer(value bool) *bool                               { return &value }
func stringPointer(value string) *string                         { return &value }
func quantityPointer(value resource.Quantity) *resource.Quantity { return &value }
func fsGroupChangePolicyPointer(value corev1.PodFSGroupChangePolicy) *corev1.PodFSGroupChangePolicy {
	return &value
}

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

func providerSandboxSecurityContext(uid int64, appArmorProfile string) *corev1.SecurityContext {
	securityContext := restrictedSecurityContext(uid)
	// Codex строит внутреннюю файловую и сетевую границу через bubblewrap.
	// Default seccomp блокирует создание его user namespace. Host-owned dev
	// профиль может дополнительно назначить node-local AppArmor policy, но base
	// профиль не предполагает наличие такого артефакта на чужих узлах.
	securityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
	if appArmorProfile != "" {
		securityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost,
			LocalhostProfile: stringPointer(appArmorProfile)}
	}
	return securityContext
}

func smallResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("25m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}
}

func httpProbe(path, port string, period, failures int32) *corev1.Probe {
	return &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromString(port)}}, PeriodSeconds: period, TimeoutSeconds: 2, FailureThreshold: failures}
}

func (manager *Manager) DebugSummary() string {
	return fmt.Sprintf("namespace=%s controller=%s", manager.config.RuntimeNamespace, shortHash(manager.config.ControllerPodUID))
}
