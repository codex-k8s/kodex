package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	governancev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/governance/v1"
	projectsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/projects/v1"
	runtimev1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/runtime/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const (
	defaultActorType                 = "service"
	defaultActorID                   = "self-deploy-chain-acceptance"
	defaultRequestSource             = "cmd/self-deploy-chain-acceptance"
	defaultAgentManagerTokenEnv      = "KODEX_AGENT_MANAGER_GRPC_AUTH_TOKEN"
	defaultProjectCatalogTokenEnv    = "KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN"
	defaultGovernanceManagerTokenEnv = "KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN"
	defaultRuntimeManagerTokenEnv    = "KODEX_RUNTIME_MANAGER_GRPC_AUTH_TOKEN"
	maxSummaryBytes                  = 2000
	maxIdentifierBytes               = 256
	maxStaffSummaryBodyBytes         = 64 * 1024
	selfDeployPlanTargetPrefix       = "agent:self-deploy-plan:"
)

const (
	stageProviderSignal     = "provider_signal"
	stageProjectSignal      = "project_signal"
	stageSelfDeployPlan     = "self_deploy_plan"
	stageGovernanceGate     = "governance_gate"
	stageGateDecision       = "gate_decision"
	stageBuildContext       = "build_context"
	stageBuildJob           = "build_job"
	stageDeployJob          = "deploy_job"
	stageStaffSummary       = "staff_summary"
	stageStatusOK           = "ok"
	stageStatusWaiting      = "waiting"
	stageStatusBlocked      = "blocked"
	stageStatusUnavailable  = "unavailable"
	stageStatusSkipped      = "skipped"
	reportStatusOK          = "ok"
	reportStatusWaiting     = "waiting"
	reportStatusBlocked     = "blocked"
	reportStatusUnavailable = "unavailable"
)

type chainOptions struct {
	AgentManagerAddr              string
	ProjectCatalogAddr            string
	GovernanceManagerAddr         string
	RuntimeManagerAddr            string
	StaffGatewayURL               string
	AgentManagerAuthTokenEnv      string
	ProjectCatalogAuthTokenEnv    string
	GovernanceManagerAuthTokenEnv string
	RuntimeManagerAuthTokenEnv    string
	SelfDeployPlanID              string
	ProjectRef                    string
	RepositoryRef                 string
	ProviderSignalRef             string
	ProviderSignalID              string
	ProviderSignalKey             string
	ActorType                     string
	ActorID                       string
	RequestID                     string
	TraceID                       string
	SessionID                     string
	Timeout                       time.Duration
}

type chainClients struct {
	AgentManager      agentManagerAPI
	ProjectCatalog    projectCatalogAPI
	GovernanceManager governanceManagerAPI
	RuntimeManager    runtimeManagerAPI
	StaffGateway      staffSummaryAPI
}

type agentManagerAPI interface {
	GetSelfDeployPlan(context.Context, *agentsv1.GetSelfDeployPlanRequest, ...grpc.CallOption) (*agentsv1.SelfDeployPlanResponse, error)
	ListSelfDeployPlans(context.Context, *agentsv1.ListSelfDeployPlansRequest, ...grpc.CallOption) (*agentsv1.ListSelfDeployPlansResponse, error)
}

type projectCatalogAPI interface {
	GetSelfDeploySignal(context.Context, *projectsv1.GetSelfDeploySignalRequest, ...grpc.CallOption) (*projectsv1.SelfDeploySignalResponse, error)
}

type governanceManagerAPI interface {
	GetGovernanceSummary(context.Context, *governancev1.GetGovernanceSummaryRequest, ...grpc.CallOption) (*governancev1.GovernanceSummaryResponse, error)
}

type runtimeManagerAPI interface {
	GetBuildContext(context.Context, *runtimev1.GetBuildContextRequest, ...grpc.CallOption) (*runtimev1.BuildContextResponse, error)
	GetJob(context.Context, *runtimev1.GetJobRequest, ...grpc.CallOption) (*runtimev1.JobResponse, error)
}

type staffSummaryAPI interface {
	GetSelfDeploySummary(context.Context, chainOptions, *agentsv1.SelfDeployPlan) (*staffSummaryResponse, error)
}

type chainReport struct {
	RequestID    string        `json:"request_id"`
	GeneratedAt  string        `json:"generated_at"`
	Status       string        `json:"status"`
	CurrentStage string        `json:"current_stage"`
	Blocker      *chainBlocker `json:"blocker,omitempty"`
	Refs         chainRefs     `json:"refs"`
	Stages       []chainStage  `json:"stages"`
}

type chainRefs struct {
	SelfDeployPlanID  string `json:"self_deploy_plan_id,omitempty"`
	ProjectRef        string `json:"project_ref,omitempty"`
	RepositoryRef     string `json:"repository_ref,omitempty"`
	ProviderSignalRef string `json:"provider_signal_ref,omitempty"`
	SourceRef         string `json:"source_ref,omitempty"`
	MergeCommitSHA    string `json:"merge_commit_sha,omitempty"`
	PlanFingerprint   string `json:"plan_fingerprint,omitempty"`
}

type chainBlocker struct {
	Stage   string `json:"stage"`
	Code    string `json:"code"`
	Summary string `json:"summary"`
	Ref     string `json:"ref,omitempty"`
}

type chainStage struct {
	Name    string           `json:"name"`
	Status  string           `json:"status"`
	Ref     string           `json:"ref,omitempty"`
	Code    string           `json:"code,omitempty"`
	Summary string           `json:"summary,omitempty"`
	Items   []chainStageItem `json:"items,omitempty"`
}

type chainStageItem struct {
	Kind        string `json:"kind"`
	ServiceKey  string `json:"service_key,omitempty"`
	Ref         string `json:"ref,omitempty"`
	Status      string `json:"status,omitempty"`
	Code        string `json:"code,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Digest      string `json:"digest,omitempty"`
	Version     int64  `json:"version,omitempty"`
}

type staffSummaryResponse struct {
	RequestID string             `json:"request_id"`
	Summary   staffDeploySummary `json:"summary"`
}

type staffDeploySummary struct {
	Availability     string                 `json:"availability"`
	ChainStatus      string                 `json:"chain_status"`
	NextStep         staffNextStep          `json:"next_step"`
	SelfDeployPlanID *string                `json:"self_deploy_plan_id,omitempty"`
	ProviderSignal   staffProviderSignal    `json:"provider_signal"`
	DeployPlan       staffDeployPlan        `json:"deploy_plan"`
	Governance       staffGovernanceSummary `json:"governance"`
	Runtime          staffRuntimeSummary    `json:"runtime"`
	SafeError        *staffSafeError        `json:"safe_error,omitempty"`
	SafeSummary      *string                `json:"safe_summary,omitempty"`
}

type staffNextStep struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
}

type staffProviderSignal struct {
	Status string  `json:"status"`
	Ref    *string `json:"ref,omitempty"`
}

type staffDeployPlan struct {
	Status string `json:"status"`
}

type staffGovernanceSummary struct {
	Status          string  `json:"status"`
	GateRequestRef  *string `json:"gate_request_ref,omitempty"`
	GateDecisionRef *string `json:"gate_decision_ref,omitempty"`
}

type staffRuntimeSummary struct {
	Status               string  `json:"status"`
	RuntimeJobRef        *string `json:"runtime_job_ref,omitempty"`
	RuntimeStatusSummary *string `json:"runtime_status_summary,omitempty"`
}

type staffSafeError struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
}

type grpcClientSet struct {
	clients chainClients
	conns   []*grpc.ClientConn
}

func main() {
	options := parseFlags()
	if options.RequestID == "" {
		options.RequestID = fmt.Sprintf("self-deploy-chain-acceptance-%d", time.Now().UTC().UnixNano())
	}
	if err := validateOptions(options); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "self-deploy-chain-acceptance: %s\n", redact(err.Error()))
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()

	clientSet, err := newGRPCClientSet(options)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "self-deploy-chain-acceptance: %s\n", redact(err.Error()))
		os.Exit(1)
	}
	defer clientSet.close()

	report, err := observeSelfDeployChain(ctx, options, clientSet.clients, time.Now().UTC())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "self-deploy-chain-acceptance: %s\n", redact(err.Error()))
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "self-deploy-chain-acceptance: %s\n", redact(err.Error()))
		os.Exit(1)
	}
}

func parseFlags() chainOptions {
	options := chainOptions{}
	flag.StringVar(&options.AgentManagerAddr, "agent-manager-addr", os.Getenv("KODEX_AGENT_MANAGER_GRPC_ADDR"), "agent-manager gRPC address")
	flag.StringVar(&options.ProjectCatalogAddr, "project-catalog-addr", os.Getenv("KODEX_PROJECT_CATALOG_GRPC_ADDR"), "project-catalog gRPC address")
	flag.StringVar(&options.GovernanceManagerAddr, "governance-manager-addr", os.Getenv("KODEX_GOVERNANCE_MANAGER_GRPC_ADDR"), "governance-manager gRPC address")
	flag.StringVar(&options.RuntimeManagerAddr, "runtime-manager-addr", os.Getenv("KODEX_RUNTIME_MANAGER_GRPC_ADDR"), "runtime-manager gRPC address")
	flag.StringVar(&options.StaffGatewayURL, "staff-gateway-url", os.Getenv("KODEX_STAFF_GATEWAY_URL"), "staff-gateway base URL for /v1/self-deploy/summary")
	flag.StringVar(&options.AgentManagerAuthTokenEnv, "agent-manager-auth-token-env", defaultAgentManagerTokenEnv, "env var name containing agent-manager shared gRPC token; empty disables metadata")
	flag.StringVar(&options.ProjectCatalogAuthTokenEnv, "project-catalog-auth-token-env", defaultProjectCatalogTokenEnv, "env var name containing project-catalog shared gRPC token; empty disables metadata")
	flag.StringVar(&options.GovernanceManagerAuthTokenEnv, "governance-manager-auth-token-env", defaultGovernanceManagerTokenEnv, "env var name containing governance-manager shared gRPC token; empty disables metadata")
	flag.StringVar(&options.RuntimeManagerAuthTokenEnv, "runtime-manager-auth-token-env", defaultRuntimeManagerTokenEnv, "env var name containing runtime-manager shared gRPC token; empty disables metadata")
	flag.StringVar(&options.SelfDeployPlanID, "self-deploy-plan-id", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_PLAN_ID"), "agent-manager SelfDeployPlan id")
	flag.StringVar(&options.ProjectRef, "project-ref", envOr("KODEX_SELF_DEPLOY_ACCEPTANCE_PROJECT_REF", os.Getenv("KODEX_AGENT_MANAGER_SELF_DEPLOY_SIGNAL_PROJECT_ID")), "project-catalog project ref/id")
	flag.StringVar(&options.ProjectRef, "project-id", options.ProjectRef, "alias for --project-ref")
	flag.StringVar(&options.RepositoryRef, "repository-ref", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_REPOSITORY_REF"), "project-catalog repository ref/id")
	flag.StringVar(&options.RepositoryRef, "repository-id", options.RepositoryRef, "alias for --repository-ref")
	flag.StringVar(&options.ProviderSignalRef, "provider-signal-ref", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_PROVIDER_SIGNAL_REF"), "safe provider/project signal ref stored in SelfDeployPlan")
	flag.StringVar(&options.ProviderSignalID, "provider-signal-id", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_PROVIDER_SIGNAL_ID"), "provider-hub repository change signal id for project-catalog lookup")
	flag.StringVar(&options.ProviderSignalKey, "provider-signal-key", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_PROVIDER_SIGNAL_KEY"), "provider-hub repository change signal idempotency key for project-catalog lookup")
	flag.StringVar(&options.ActorType, "actor-type", envOr("KODEX_SELF_DEPLOY_ACCEPTANCE_ACTOR_TYPE", defaultActorType), "safe actor type")
	flag.StringVar(&options.ActorID, "actor-id", envOr("KODEX_SELF_DEPLOY_ACCEPTANCE_ACTOR_ID", defaultActorID), "safe actor id")
	flag.StringVar(&options.RequestID, "request-id", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_REQUEST_ID"), "optional request id")
	flag.StringVar(&options.TraceID, "trace-id", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_TRACE_ID"), "optional trace id")
	flag.StringVar(&options.SessionID, "session-id", os.Getenv("KODEX_SELF_DEPLOY_ACCEPTANCE_SESSION_ID"), "optional session id")
	flag.DurationVar(&options.Timeout, "timeout", 30*time.Second, "acceptance read timeout")
	flag.Parse()
	return options
}

func validateOptions(options chainOptions) error {
	if strings.TrimSpace(options.AgentManagerAddr) == "" {
		return errors.New("agent-manager gRPC address is required")
	}
	if strings.TrimSpace(options.ProjectCatalogAddr) == "" {
		return errors.New("project-catalog gRPC address is required")
	}
	if strings.TrimSpace(options.GovernanceManagerAddr) == "" {
		return errors.New("governance-manager gRPC address is required")
	}
	if strings.TrimSpace(options.RuntimeManagerAddr) == "" {
		return errors.New("runtime-manager gRPC address is required")
	}
	if strings.TrimSpace(options.SelfDeployPlanID) == "" && strings.TrimSpace(options.ProjectRef) == "" && strings.TrimSpace(options.ProviderSignalRef) == "" {
		return errors.New("one of self-deploy-plan-id, project-ref or provider-signal-ref is required")
	}
	if strings.TrimSpace(options.ActorType) == "" || strings.TrimSpace(options.ActorID) == "" {
		return errors.New("actor type and actor id are required")
	}
	if options.Timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	return nil
}

func newGRPCClientSet(options chainOptions) (grpcClientSet, error) {
	agentToken, err := tokenFromEnvRef(options.AgentManagerAuthTokenEnv, "agent-manager")
	if err != nil {
		return grpcClientSet{}, err
	}
	projectToken, err := tokenFromEnvRef(options.ProjectCatalogAuthTokenEnv, "project-catalog")
	if err != nil {
		return grpcClientSet{}, err
	}
	governanceToken, err := tokenFromEnvRef(options.GovernanceManagerAuthTokenEnv, "governance-manager")
	if err != nil {
		return grpcClientSet{}, err
	}
	runtimeToken, err := tokenFromEnvRef(options.RuntimeManagerAuthTokenEnv, "runtime-manager")
	if err != nil {
		return grpcClientSet{}, err
	}
	clientSet := grpcClientSet{}
	connect := func(name string, addr string, token string) (*grpc.ClientConn, error) {
		conn, err := grpc.NewClient(addr, grpcClientDialOptions(token, options.ActorType, options.ActorID)...)
		if err != nil {
			clientSet.close()
			return nil, fmt.Errorf("connect %s: %w", name, err)
		}
		clientSet.conns = append(clientSet.conns, conn)
		return conn, nil
	}
	agentConn, err := connect("agent-manager", options.AgentManagerAddr, agentToken)
	if err != nil {
		return grpcClientSet{}, err
	}
	projectConn, err := connect("project-catalog", options.ProjectCatalogAddr, projectToken)
	if err != nil {
		return grpcClientSet{}, err
	}
	governanceConn, err := connect("governance-manager", options.GovernanceManagerAddr, governanceToken)
	if err != nil {
		return grpcClientSet{}, err
	}
	runtimeConn, err := connect("runtime-manager", options.RuntimeManagerAddr, runtimeToken)
	if err != nil {
		return grpcClientSet{}, err
	}
	clientSet.clients = chainClients{
		AgentManager:      agentsv1.NewAgentManagerServiceClient(agentConn),
		ProjectCatalog:    projectsv1.NewProjectCatalogServiceClient(projectConn),
		GovernanceManager: governancev1.NewGovernanceManagerServiceClient(governanceConn),
		RuntimeManager:    runtimev1.NewRuntimeManagerServiceClient(runtimeConn),
		StaffGateway:      httpStaffSummaryClient{client: http.DefaultClient},
	}
	return clientSet, nil
}

func (set grpcClientSet) close() {
	for _, conn := range set.conns {
		_ = conn.Close()
	}
}

func grpcClientDialOptions(authToken string, callerType string, callerID string) []grpc.DialOption {
	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if strings.TrimSpace(authToken) != "" {
		options = append(options, grpc.WithUnaryInterceptor(outgoingAuthUnaryInterceptor(authToken, callerType, callerID)))
	}
	return options
}

func outgoingAuthUnaryInterceptor(authToken string, callerType string, callerID string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req any, reply any, conn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx,
			grpcserver.MetadataAuthorization, "Bearer "+authToken,
			grpcserver.MetadataCallerType, callerType,
			grpcserver.MetadataCallerID, callerID,
		)
		return invoker(ctx, method, req, reply, conn, opts...)
	}
}

func observeSelfDeployChain(ctx context.Context, options chainOptions, clients chainClients, now time.Time) (chainReport, error) {
	report := chainReport{
		RequestID:   options.RequestID,
		GeneratedAt: now.Format(time.RFC3339),
		Status:      reportStatusOK,
		Refs: chainRefs{
			ProjectRef:        safeToken(options.ProjectRef),
			RepositoryRef:     safeToken(options.RepositoryRef),
			ProviderSignalRef: safeToken(options.ProviderSignalRef),
		},
	}

	plan, planStage := readSelfDeployPlan(ctx, options, clients.AgentManager)
	if plan != nil {
		report.Refs = refsFromPlan(options, plan)
	}
	report.Stages = append(report.Stages, providerSignalStage(options, plan))
	report.Stages = append(report.Stages, projectSignalStage(ctx, options, plan, clients.ProjectCatalog))
	report.Stages = append(report.Stages, planStage)

	governanceSummary, governanceStages := governanceStages(ctx, options, plan, clients.GovernanceManager)
	_ = governanceSummary
	report.Stages = append(report.Stages, governanceStages...)
	report.Stages = append(report.Stages, buildContextStage(ctx, options, plan, clients.RuntimeManager))
	report.Stages = append(report.Stages, buildJobStage(ctx, options, plan, clients.RuntimeManager))
	report.Stages = append(report.Stages, deployJobStage(ctx, options, plan, clients.RuntimeManager))
	report.Stages = append(report.Stages, staffSummaryStage(ctx, options, plan, clients.StaffGateway))

	report.Status, report.CurrentStage, report.Blocker = summarizeReport(report.Stages)
	return report, nil
}

func readSelfDeployPlan(ctx context.Context, options chainOptions, client agentManagerAPI) (*agentsv1.SelfDeployPlan, chainStage) {
	if client == nil {
		return nil, blockedStage(stageSelfDeployPlan, "", "agent_manager_client_missing", "agent-manager client is not configured")
	}
	meta := agentQueryMeta(options)
	if strings.TrimSpace(options.SelfDeployPlanID) != "" {
		response, err := client.GetSelfDeployPlan(ctx, &agentsv1.GetSelfDeployPlanRequest{
			Meta:             meta,
			SelfDeployPlanId: strings.TrimSpace(options.SelfDeployPlanID),
		})
		if err != nil {
			return nil, unavailableStage(stageSelfDeployPlan, options.SelfDeployPlanID, "self_deploy_plan_read_failed", err.Error())
		}
		plan := response.GetSelfDeployPlan()
		if plan == nil {
			return nil, waitingStage(stageSelfDeployPlan, options.SelfDeployPlanID, "self_deploy_plan_missing", "agent-manager returned empty SelfDeployPlan")
		}
		return plan, selfDeployPlanStage(plan)
	}
	request := &agentsv1.ListSelfDeployPlansRequest{
		Meta:              meta,
		ProjectRef:        optionalString(options.ProjectRef),
		RepositoryRef:     optionalString(options.RepositoryRef),
		ProviderSignalRef: optionalString(options.ProviderSignalRef),
		Page:              &agentsv1.PageRequest{PageSize: 1},
	}
	response, err := client.ListSelfDeployPlans(ctx, request)
	if err != nil {
		return nil, unavailableStage(stageSelfDeployPlan, "", "self_deploy_plan_list_failed", err.Error())
	}
	plans := response.GetSelfDeployPlans()
	if len(plans) == 0 {
		return nil, waitingStage(stageSelfDeployPlan, "", "self_deploy_plan_missing", "SelfDeployPlan ещё не создан или не виден по safe selector.")
	}
	return plans[0], selfDeployPlanStage(plans[0])
}

func providerSignalStage(options chainOptions, plan *agentsv1.SelfDeployPlan) chainStage {
	ref := firstNonEmpty(options.ProviderSignalRef, plan.GetProviderSignalRef())
	if ref == "" {
		return waitingStage(stageProviderSignal, "", "provider_signal_ref_missing", "Provider signal ref ещё не известен через safe selector или SelfDeployPlan.")
	}
	return okStage(stageProviderSignal, ref, "stored_ref", "Provider signal присутствует как safe ref; команда не читает provider payload.")
}

func projectSignalStage(ctx context.Context, options chainOptions, plan *agentsv1.SelfDeployPlan, client projectCatalogAPI) chainStage {
	projectRef := firstNonEmpty(options.ProjectRef, plan.GetProjectRef())
	if projectRef == "" {
		return skippedStage(stageProjectSignal, "project_ref_missing", "Project ref отсутствует; project-catalog readiness не проверяется.")
	}
	if client == nil {
		return unavailableStage(stageProjectSignal, projectRef, "project_catalog_client_missing", "project-catalog client is not configured")
	}
	request := &projectsv1.GetSelfDeploySignalRequest{
		ProjectId:         projectRef,
		RepositoryId:      optionalString(firstNonEmpty(options.RepositoryRef, plan.GetRepositoryRef())),
		ProviderSignalId:  optionalString(options.ProviderSignalID),
		ProviderSignalKey: optionalString(options.ProviderSignalKey),
		Meta:              projectQueryMeta(options),
	}
	response, err := client.GetSelfDeploySignal(ctx, request)
	if err != nil {
		return unavailableStage(stageProjectSignal, projectRef, "project_signal_read_failed", err.Error())
	}
	status := projectSignalStatus(response.GetStatus())
	signal := response.GetSignal()
	ref := firstNonEmpty(signal.GetProviderSignalRef(), options.ProviderSignalRef, plan.GetProviderSignalRef())
	summary := firstNonEmpty(response.GetSafeReason(), signal.GetSafeSummary(), "Project-side self-deploy signal read completed.")
	switch response.GetStatus() {
	case projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_READY:
		stage := okStage(stageProjectSignal, ref, status, summary)
		stage.Items = append(stage.Items, chainStageItem{
			Kind:        "project_signal",
			Ref:         ref,
			Status:      status,
			Fingerprint: safeToken(signal.GetProjectSignalFingerprint()),
			Digest:      safeToken(signal.GetProviderChangeFingerprint()),
			Version:     signal.GetVersion(),
		})
		return stage
	case projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_PROVIDER_SIGNAL_NOT_FOUND,
		projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_PROVIDER_SIGNAL_NOT_READY:
		return waitingStage(stageProjectSignal, ref, status, summary)
	case projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_REPOSITORY_BINDING_NOT_FOUND,
		projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_NEEDS_SERVICES_POLICY_RECONCILE,
		projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_SERVICES_POLICY_NOT_FOUND,
		projectsv1.SelfDeploySignalStatus_SELF_DEPLOY_SIGNAL_STATUS_SERVICES_POLICY_NOT_READY:
		return blockedStage(stageProjectSignal, ref, status, summary)
	default:
		return blockedStage(stageProjectSignal, ref, firstNonEmpty(status, "self_deploy_signal_blocked"), summary)
	}
}

func selfDeployPlanStage(plan *agentsv1.SelfDeployPlan) chainStage {
	status := selfDeployPlanStatus(plan.GetStatus())
	summary := firstNonEmpty(plan.GetSafeSummary(), "SelfDeployPlan read completed.")
	ref := safeToken(plan.GetId())
	stage := chainStage{
		Name:    stageSelfDeployPlan,
		Ref:     ref,
		Code:    status,
		Summary: safeSummary(summary),
		Items: []chainStageItem{{
			Kind:        "self_deploy_plan",
			Ref:         ref,
			Status:      status,
			Fingerprint: safeToken(plan.GetPlanFingerprint()),
			Digest:      safeToken(plan.GetServicesYamlDigest()),
			Version:     plan.GetVersion(),
		}},
	}
	switch plan.GetStatus() {
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL:
		stage.Status = stageStatusWaiting
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED:
		stage.Status = stageStatusOK
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_REJECTED,
		agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_CANCELLED,
		agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_FAILED:
		stage.Status = stageStatusBlocked
	default:
		stage.Status = stageStatusWaiting
	}
	return stage
}

func governanceStages(ctx context.Context, options chainOptions, plan *agentsv1.SelfDeployPlan, client governanceManagerAPI) (*governancev1.GovernanceSummary, []chainStage) {
	if plan == nil {
		return nil, []chainStage{
			skippedStage(stageGovernanceGate, "self_deploy_plan_missing", "Governance gate проверяется после появления SelfDeployPlan."),
			skippedStage(stageGateDecision, "self_deploy_plan_missing", "Gate decision проверяется после появления SelfDeployPlan."),
		}
	}
	if client == nil {
		return nil, []chainStage{
			unavailableStage(stageGovernanceGate, plan.GetGovernanceContext().GetGateRequestRef(), "governance_manager_client_missing", "governance-manager client is not configured"),
			unavailableStage(stageGateDecision, plan.GetGovernanceContext().GetGateDecisionRef(), "governance_manager_client_missing", "governance-manager client is not configured"),
		}
	}
	response, err := client.GetGovernanceSummary(ctx, &governancev1.GetGovernanceSummaryRequest{
		Scope: &governancev1.GovernanceSummaryScope{
			Target: &governancev1.TargetRef{
				Type: governancev1.GovernanceTargetType_GOVERNANCE_TARGET_TYPE_SELF_DEPLOY_PLAN,
				Ref:  selfDeployPlanTargetRef(plan.GetId()),
			},
		},
		Meta: governanceQueryMeta(options),
	})
	if err != nil {
		return nil, []chainStage{
			unavailableStage(stageGovernanceGate, plan.GetGovernanceContext().GetGateRequestRef(), "governance_summary_read_failed", err.Error()),
			unavailableStage(stageGateDecision, plan.GetGovernanceContext().GetGateDecisionRef(), "governance_summary_read_failed", err.Error()),
		}
	}
	summary := response.GetSummary()
	return summary, []chainStage{governanceGateStage(plan, summary), gateDecisionStage(plan, summary)}
}

func governanceGateStage(plan *agentsv1.SelfDeployPlan, summary *governancev1.GovernanceSummary) chainStage {
	context := plan.GetGovernanceContext()
	ref := context.GetGateRequestRef()
	pending := firstPendingGovernanceDecision(summary)
	if pending != nil && ref == "" {
		ref = pending.GetId()
	}
	if ref != "" || pending != nil {
		stage := chainStage{Name: stageGovernanceGate, Ref: safeToken(ref), Code: "gate_request_present", Summary: "Governance gate request найден через safe refs.", Status: stageStatusOK}
		if pending != nil {
			stage.Status = stageStatusWaiting
			stage.Code = gateRequestStatus(pending.GetGateRequestStatus())
			stage.Summary = safeSummary(firstNonEmpty(pending.GetSafeSummary(), "Governance gate ожидает owner decision."))
			stage.Items = append(stage.Items, governanceDecisionItem("gate_request", pending))
		}
		return stage
	}
	switch plan.GetStatus() {
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL:
		return waitingStage(stageGovernanceGate, "", "governance_gate_pending_creation", "SelfDeployPlan ожидает подготовки governance gate.")
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED:
		return okStage(stageGovernanceGate, context.GetGateRequestRef(), "gate_resolved_or_not_required", "SelfDeployPlan утверждён; активный gate request не найден.")
	default:
		return skippedStage(stageGovernanceGate, "gate_not_required_for_current_plan_status", "Governance gate не активен для текущего статуса плана.")
	}
}

func gateDecisionStage(plan *agentsv1.SelfDeployPlan, summary *governancev1.GovernanceSummary) chainStage {
	context := plan.GetGovernanceContext()
	ref := context.GetGateDecisionRef()
	completed := firstCompletedGateDecision(summary)
	if completed != nil && ref == "" {
		ref = completed.GetId()
	}
	if ref != "" || completed != nil {
		stage := okStage(stageGateDecision, ref, "gate_decision_present", "Gate decision найден через safe refs.")
		if completed != nil {
			stage.Code = gateOutcome(completed.GetGateOutcome())
			stage.Summary = safeSummary(firstNonEmpty(completed.GetSafeSummary(), "Gate decision recorded."))
			stage.Items = append(stage.Items, governanceDecisionItem("gate_decision", completed))
		}
		return stage
	}
	switch plan.GetStatus() {
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL:
		return waitingStage(stageGateDecision, context.GetGateRequestRef(), "owner_decision_pending", "Owner/governance decision ещё не записано.")
	case agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_APPROVED:
		return blockedStage(stageGateDecision, context.GetGateRequestRef(), "gate_decision_ref_missing", "Approved SelfDeployPlan не содержит safe gate decision ref.")
	default:
		return skippedStage(stageGateDecision, "gate_decision_not_available_for_current_plan_status", "Gate decision не ожидается для текущего статуса плана.")
	}
}

func buildContextStage(ctx context.Context, options chainOptions, plan *agentsv1.SelfDeployPlan, client runtimeManagerAPI) chainStage {
	if plan == nil {
		return skippedStage(stageBuildContext, "self_deploy_plan_missing", "Build context проверяется после появления SelfDeployPlan.")
	}
	if !expectsRuntimeJob(plan, runtimev1.JobType_JOB_TYPE_BUILD) {
		return skippedStage(stageBuildContext, "build_job_not_expected", "SelfDeployPlan не требует runtime build.")
	}
	if plan.GetStatus() == agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL {
		return waitingStage(stageBuildContext, "", "owner_decision_pending", "Build context не готовится до owner/governance approval.")
	}
	contexts := plan.GetRuntimeBuildContexts()
	if len(contexts) == 0 {
		return runtimeBuildWaitingStage(plan, stageBuildContext, "build_context_missing", "Runtime build context ref ещё не записан.")
	}
	stage := chainStage{Name: stageBuildContext, Status: stageStatusOK, Code: "build_context_ref_present", Summary: "Runtime build context refs прочитаны из SelfDeployPlan."}
	for _, item := range contexts {
		stage.Items = append(stage.Items, buildContextItem(ctx, options, item, client))
	}
	stage.Status, stage.Code, stage.Summary = runtimeStageSummaryFromItems(stage, "build_context_ref_present", "Runtime build context refs проверены.")
	return stage
}

func buildJobStage(ctx context.Context, options chainOptions, plan *agentsv1.SelfDeployPlan, client runtimeManagerAPI) chainStage {
	if plan == nil {
		return skippedStage(stageBuildJob, "self_deploy_plan_missing", "Build job проверяется после появления SelfDeployPlan.")
	}
	if !expectsRuntimeJob(plan, runtimev1.JobType_JOB_TYPE_BUILD) {
		return skippedStage(stageBuildJob, "build_job_not_expected", "SelfDeployPlan не требует runtime build.")
	}
	if plan.GetStatus() == agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL {
		return waitingStage(stageBuildJob, "", "owner_decision_pending", "Build job не создаётся до owner/governance approval.")
	}
	jobs := plan.GetRuntimeBuildJobs()
	if len(jobs) == 0 {
		return runtimeBuildWaitingStage(plan, stageBuildJob, "build_job_missing", "Runtime build job ref ещё не записан.")
	}
	stage := chainStage{Name: stageBuildJob, Status: stageStatusOK, Code: "build_job_ref_present", Summary: "Runtime build job refs прочитаны из SelfDeployPlan."}
	for _, item := range jobs {
		stage.Items = append(stage.Items, runtimeJobItem(ctx, options, "build_job", item.GetServiceKey(), item.GetRuntimeJobRef(), item.GetBuildPlanItemFingerprint(), client))
	}
	stage.Status, stage.Code, stage.Summary = runtimeStageSummaryFromItems(stage, "build_job_ref_present", "Runtime build job refs проверены.")
	return stage
}

func deployJobStage(ctx context.Context, options chainOptions, plan *agentsv1.SelfDeployPlan, client runtimeManagerAPI) chainStage {
	if plan == nil {
		return skippedStage(stageDeployJob, "self_deploy_plan_missing", "Deploy job проверяется после появления SelfDeployPlan.")
	}
	if !expectsRuntimeJob(plan, runtimev1.JobType_JOB_TYPE_DEPLOY) {
		return skippedStage(stageDeployJob, "deploy_job_not_expected", "SelfDeployPlan не требует runtime deploy.")
	}
	if plan.GetStatus() == agentsv1.SelfDeployPlanStatus_SELF_DEPLOY_PLAN_STATUS_PENDING_APPROVAL {
		return waitingStage(stageDeployJob, "", "owner_decision_pending", "Deploy job не создаётся до owner/governance approval.")
	}
	if plan.GetRuntimeBuildStatus() != agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_SUCCEEDED {
		return waitingStage(stageDeployJob, "", runtimeBuildStatus(plan.GetRuntimeBuildStatus()), "Deploy job не создаётся до successful build.")
	}
	jobs := plan.GetRuntimeDeployJobs()
	if len(jobs) == 0 {
		return runtimeDeployWaitingStage(plan, stageDeployJob, "deploy_job_missing", "Runtime deploy job ref ещё не записан.")
	}
	stage := chainStage{Name: stageDeployJob, Status: stageStatusOK, Code: "deploy_job_ref_present", Summary: "Runtime deploy job refs прочитаны из SelfDeployPlan."}
	for _, item := range jobs {
		stage.Items = append(stage.Items, runtimeJobItem(ctx, options, "deploy_job", item.GetServiceKey(), item.GetRuntimeJobRef(), item.GetDeployPlanItemFingerprint(), client))
	}
	stage.Status, stage.Code, stage.Summary = runtimeStageSummaryFromItems(stage, "deploy_job_ref_present", "Runtime deploy job refs проверены.")
	return stage
}

func buildContextItem(ctx context.Context, options chainOptions, ref *agentsv1.SelfDeployRuntimeBuildContextRef, client runtimeManagerAPI) chainStageItem {
	item := chainStageItem{
		Kind:        "build_context",
		ServiceKey:  safeToken(ref.GetServiceKey()),
		Ref:         safeToken(firstNonEmpty(ref.GetRuntimeBuildContextRef(), ref.GetBuildContextRef())),
		Status:      safeToken(firstNonEmpty(ref.GetRuntimeBuildContextStatus(), "stored_ref")),
		Fingerprint: safeToken(firstNonEmpty(ref.GetMaterializationFingerprint(), ref.GetBuildPlanItemFingerprint())),
		Digest:      safeToken(firstNonEmpty(ref.GetBuildContextDigest(), ref.GetManifestBundleDigest(), ref.GetDockerfileDigest())),
	}
	if client == nil {
		item.Status = stageStatusUnavailable
		item.Code = "runtime_manager_client_missing"
		item.Summary = "runtime-manager client is not configured"
		return item
	}
	request := &runtimev1.GetBuildContextRequest{
		BuildContextId:     optionalString(ref.GetRuntimeBuildContextRef()),
		ContextFingerprint: optionalString(ref.GetMaterializationFingerprint()),
		Meta:               runtimeQueryMeta(options),
	}
	if request.GetBuildContextId() == "" && request.GetContextFingerprint() == "" {
		item.Summary = "Stored build context ref не является runtime-manager lookup key."
		return item
	}
	response, err := client.GetBuildContext(ctx, request)
	if err != nil {
		item.Status = stageStatusUnavailable
		item.Code = "runtime_build_context_read_failed"
		item.Summary = safeSummary(err.Error())
		return item
	}
	context := response.GetBuildContext()
	if context == nil {
		item.Status = stageStatusUnavailable
		item.Code = "runtime_build_context_empty"
		item.Summary = "runtime-manager returned empty BuildContext"
		return item
	}
	item.Ref = safeToken(firstNonEmpty(context.GetBuildContextId(), item.Ref))
	item.Status = buildContextStatus(context.GetStatus())
	item.Code = safeToken(context.GetLastErrorCode())
	item.Summary = safeSummary(firstNonEmpty(context.GetLastErrorMessage(), context.GetNextAction(), "Runtime BuildContext read completed."))
	item.Fingerprint = safeToken(firstNonEmpty(context.GetContextFingerprint(), item.Fingerprint))
	item.Digest = safeToken(firstNonEmpty(context.GetBuildContextDigest(), context.GetSourceSnapshotDigest(), item.Digest))
	item.Version = context.GetVersion()
	return item
}

func runtimeJobItem(ctx context.Context, options chainOptions, kind string, serviceKey string, jobRef string, fingerprint string, client runtimeManagerAPI) chainStageItem {
	item := chainStageItem{
		Kind:        kind,
		ServiceKey:  safeToken(serviceKey),
		Ref:         safeToken(jobRef),
		Fingerprint: safeToken(fingerprint),
	}
	if jobRef == "" {
		item.Status = stageStatusWaiting
		item.Code = kind + "_ref_missing"
		item.Summary = "Runtime job ref ещё не записан."
		return item
	}
	if client == nil {
		item.Status = stageStatusUnavailable
		item.Code = "runtime_manager_client_missing"
		item.Summary = "runtime-manager client is not configured"
		return item
	}
	response, err := client.GetJob(ctx, &runtimev1.GetJobRequest{JobId: jobRef, Meta: runtimeQueryMeta(options)})
	if err != nil {
		item.Status = stageStatusUnavailable
		item.Code = "runtime_job_read_failed"
		item.Summary = safeSummary(err.Error())
		return item
	}
	job := response.GetJob()
	if job == nil {
		item.Status = stageStatusUnavailable
		item.Code = "runtime_job_empty"
		item.Summary = "runtime-manager returned empty Job"
		return item
	}
	item.Status = runtimeJobStatus(job.GetStatus())
	item.Code = safeToken(job.GetLastErrorCode())
	item.Summary = safeSummary(firstNonEmpty(job.GetLastErrorMessage(), job.GetNextAction(), "Runtime job read completed."))
	item.Version = job.GetVersion()
	return item
}

func staffSummaryStage(ctx context.Context, options chainOptions, plan *agentsv1.SelfDeployPlan, client staffSummaryAPI) chainStage {
	if strings.TrimSpace(options.StaffGatewayURL) == "" {
		return unavailableStage(stageStaffSummary, "", "staff_gateway_url_missing", "staff-gateway URL не задан; owner-facing summary не проверена.")
	}
	if client == nil {
		return unavailableStage(stageStaffSummary, "", "staff_gateway_client_missing", "staff-gateway client is not configured")
	}
	response, err := client.GetSelfDeploySummary(ctx, options, plan)
	if err != nil {
		return unavailableStage(stageStaffSummary, "", "staff_summary_read_failed", err.Error())
	}
	summary := response.Summary
	stage := chainStage{
		Name:    stageStaffSummary,
		Status:  stageStatusOK,
		Ref:     deref(summary.SelfDeployPlanID),
		Code:    firstNonEmpty(summary.ChainStatus, summary.Availability),
		Summary: safeSummary(firstNonEmpty(summary.NextStep.Summary, deref(summary.SafeSummary), "Staff self-deploy summary read completed.")),
	}
	if summary.SafeError != nil {
		stage.Status = stageStatusBlocked
		stage.Code = firstNonEmpty(summary.SafeError.Code, stage.Code)
		stage.Summary = safeSummary(summary.SafeError.Summary)
	}
	if summary.Availability == "unavailable" && summary.SafeError == nil {
		stage.Status = stageStatusUnavailable
	}
	stage.Items = append(stage.Items,
		chainStageItem{Kind: "provider_signal", Ref: deref(summary.ProviderSignal.Ref), Status: summary.ProviderSignal.Status},
		chainStageItem{Kind: "self_deploy_plan", Ref: deref(summary.SelfDeployPlanID), Status: summary.DeployPlan.Status},
		chainStageItem{Kind: "governance", Ref: firstNonEmpty(deref(summary.Governance.GateRequestRef), deref(summary.Governance.GateDecisionRef)), Status: summary.Governance.Status},
		chainStageItem{Kind: "runtime", Ref: deref(summary.Runtime.RuntimeJobRef), Status: summary.Runtime.Status, Summary: safeSummary(deref(summary.Runtime.RuntimeStatusSummary))},
	)
	return stage
}

type httpStaffSummaryClient struct {
	client *http.Client
}

func (client httpStaffSummaryClient) GetSelfDeploySummary(ctx context.Context, options chainOptions, plan *agentsv1.SelfDeployPlan) (*staffSummaryResponse, error) {
	baseURL, err := url.Parse(strings.TrimRight(options.StaffGatewayURL, "/") + "/v1/self-deploy/summary")
	if err != nil {
		return nil, fmt.Errorf("parse staff-gateway url: %w", err)
	}
	query := baseURL.Query()
	if projectRef := firstNonEmpty(options.ProjectRef, plan.GetProjectRef()); projectRef != "" {
		query.Set("project_ref", projectRef)
	}
	if repositoryRef := firstNonEmpty(options.RepositoryRef, plan.GetRepositoryRef()); repositoryRef != "" {
		query.Set("repository_ref", repositoryRef)
	}
	if providerSignalRef := firstNonEmpty(options.ProviderSignalRef, plan.GetProviderSignalRef()); providerSignalRef != "" {
		query.Set("provider_signal_ref", providerSignalRef)
	}
	baseURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Kodex-Request-Id", options.RequestID)
	request.Header.Set("X-Kodex-Actor-Type", options.ActorType)
	request.Header.Set("X-Kodex-Actor-Id", options.ActorID)
	if options.TraceID != "" {
		request.Header.Set("X-Kodex-Trace-Id", options.TraceID)
	}
	if options.SessionID != "" {
		request.Header.Set("X-Kodex-Session-Id", options.SessionID)
	}
	response, err := client.http().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxStaffSummaryBodyBytes))
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("staff-gateway returned status %d", response.StatusCode)
	}
	var output staffSummaryResponse
	if err := json.Unmarshal(body, &output); err != nil {
		return nil, fmt.Errorf("decode staff summary: %w", err)
	}
	return &output, nil
}

func (client httpStaffSummaryClient) http() *http.Client {
	if client.client != nil {
		return client.client
	}
	return http.DefaultClient
}

func runtimeBuildWaitingStage(plan *agentsv1.SelfDeployPlan, name string, code string, summary string) chainStage {
	switch plan.GetRuntimeBuildStatus() {
	case agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_BLOCKED,
		agentsv1.SelfDeployRuntimeBuildStatus_SELF_DEPLOY_RUNTIME_BUILD_STATUS_FAILED:
		return blockedStage(name, "", firstNonEmpty(plan.GetRuntimeBuildErrorCode(), runtimeBuildStatus(plan.GetRuntimeBuildStatus()), code), firstNonEmpty(plan.GetRuntimeBuildSummary(), summary))
	default:
		return waitingStage(name, "", firstNonEmpty(runtimeBuildStatus(plan.GetRuntimeBuildStatus()), code), firstNonEmpty(plan.GetRuntimeBuildSummary(), summary))
	}
}

func runtimeDeployWaitingStage(plan *agentsv1.SelfDeployPlan, name string, code string, summary string) chainStage {
	switch plan.GetRuntimeDeployStatus() {
	case agentsv1.SelfDeployRuntimeDeployStatus_SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_BLOCKED,
		agentsv1.SelfDeployRuntimeDeployStatus_SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_FAILED:
		return blockedStage(name, "", firstNonEmpty(plan.GetRuntimeDeployErrorCode(), runtimeDeployStatus(plan.GetRuntimeDeployStatus()), code), firstNonEmpty(plan.GetRuntimeDeploySummary(), summary))
	default:
		return waitingStage(name, "", firstNonEmpty(runtimeDeployStatus(plan.GetRuntimeDeployStatus()), code), firstNonEmpty(plan.GetRuntimeDeploySummary(), summary))
	}
}

func runtimeStageSummaryFromItems(stage chainStage, defaultCode string, defaultSummary string) (string, string, string) {
	status := stageStatusOK
	code := defaultCode
	summary := defaultSummary
	for _, item := range stage.Items {
		switch item.Status {
		case stageStatusUnavailable:
			if status != stageStatusBlocked {
				status = stageStatusUnavailable
				code = firstNonEmpty(item.Code, code)
				summary = firstNonEmpty(item.Summary, summary)
			}
		case stageStatusBlocked, "failed", "cancelled", "timeout", "timed_out":
			status = stageStatusBlocked
			code = firstNonEmpty(item.Code, item.Status, code)
			summary = firstNonEmpty(item.Summary, summary)
		case stageStatusWaiting, "queued", "running", "pending":
			if status == stageStatusOK {
				status = stageStatusWaiting
				code = firstNonEmpty(item.Code, item.Status, code)
				summary = firstNonEmpty(item.Summary, summary)
			}
		}
	}
	return status, safeToken(code), safeSummary(summary)
}

func summarizeReport(stages []chainStage) (string, string, *chainBlocker) {
	if stage, ok := firstStageWithStatus(stages, stageStatusBlocked); ok {
		return reportStatusBlocked, stage.Name, blockerFromStage(stage)
	}
	if stage, ok := firstStageWithStatus(stages, stageStatusUnavailable); ok {
		return reportStatusUnavailable, stage.Name, blockerFromStage(stage)
	}
	if stage, ok := firstStageWithStatus(stages, stageStatusWaiting); ok {
		return reportStatusWaiting, stage.Name, nil
	}
	if len(stages) == 0 {
		return reportStatusUnavailable, "", &chainBlocker{Code: "empty_report", Summary: "Acceptance report has no stages."}
	}
	return reportStatusOK, stages[len(stages)-1].Name, nil
}

func firstStageWithStatus(stages []chainStage, status string) (chainStage, bool) {
	for _, stage := range stages {
		if stage.Status == status {
			return stage, true
		}
	}
	return chainStage{}, false
}

func blockerFromStage(stage chainStage) *chainBlocker {
	return &chainBlocker{
		Stage:   stage.Name,
		Code:    firstNonEmpty(stage.Code, stage.Status),
		Summary: firstNonEmpty(stage.Summary, "Self-deploy acceptance stage is blocked."),
		Ref:     stage.Ref,
	}
}

func okStage(name string, ref string, code string, summary string) chainStage {
	return chainStage{Name: name, Status: stageStatusOK, Ref: safeToken(ref), Code: safeToken(code), Summary: safeSummary(summary)}
}

func waitingStage(name string, ref string, code string, summary string) chainStage {
	return chainStage{Name: name, Status: stageStatusWaiting, Ref: safeToken(ref), Code: safeToken(code), Summary: safeSummary(summary)}
}

func blockedStage(name string, ref string, code string, summary string) chainStage {
	return chainStage{Name: name, Status: stageStatusBlocked, Ref: safeToken(ref), Code: safeToken(code), Summary: safeSummary(summary)}
}

func unavailableStage(name string, ref string, code string, summary string) chainStage {
	return chainStage{Name: name, Status: stageStatusUnavailable, Ref: safeToken(ref), Code: safeToken(code), Summary: safeSummary(summary)}
}

func skippedStage(name string, code string, summary string) chainStage {
	return chainStage{Name: name, Status: stageStatusSkipped, Code: safeToken(code), Summary: safeSummary(summary)}
}

func refsFromPlan(options chainOptions, plan *agentsv1.SelfDeployPlan) chainRefs {
	return chainRefs{
		SelfDeployPlanID:  safeToken(plan.GetId()),
		ProjectRef:        safeToken(firstNonEmpty(options.ProjectRef, plan.GetProjectRef())),
		RepositoryRef:     safeToken(firstNonEmpty(options.RepositoryRef, plan.GetRepositoryRef())),
		ProviderSignalRef: safeToken(firstNonEmpty(options.ProviderSignalRef, plan.GetProviderSignalRef())),
		SourceRef:         safeToken(plan.GetSourceRef()),
		MergeCommitSHA:    safeToken(plan.GetMergeCommitSha()),
		PlanFingerprint:   safeToken(plan.GetPlanFingerprint()),
	}
}

func firstPendingGovernanceDecision(summary *governancev1.GovernanceSummary) *governancev1.GovernanceDecisionSummary {
	for _, item := range summary.GetPendingDecisions() {
		if item.GetKind() == governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_REQUEST {
			return item
		}
	}
	return nil
}

func firstCompletedGateDecision(summary *governancev1.GovernanceSummary) *governancev1.GovernanceDecisionSummary {
	for _, item := range summary.GetCompletedDecisions() {
		if item.GetKind() == governancev1.GovernanceDecisionSummaryKind_GOVERNANCE_DECISION_SUMMARY_KIND_GATE_DECISION {
			return item
		}
	}
	return nil
}

func governanceDecisionItem(kind string, item *governancev1.GovernanceDecisionSummary) chainStageItem {
	return chainStageItem{
		Kind:    kind,
		Ref:     safeToken(item.GetId()),
		Status:  governanceDecisionAttention(item.GetAttention()),
		Code:    safeToken(firstNonEmpty(gateRequestStatus(item.GetGateRequestStatus()), gateOutcome(item.GetGateOutcome()))),
		Summary: safeSummary(item.GetSafeSummary()),
		Version: item.GetVersion(),
	}
}

func expectsRuntimeJob(plan *agentsv1.SelfDeployPlan, want runtimev1.JobType) bool {
	for _, item := range plan.GetExpectedRuntimeJobTypes() {
		if item == want {
			return true
		}
	}
	return false
}

func agentQueryMeta(options chainOptions) *agentsv1.QueryMeta {
	return &agentsv1.QueryMeta{
		Actor:     &agentsv1.Actor{Type: options.ActorType, Id: options.ActorID},
		RequestId: options.RequestID,
		RequestContext: &agentsv1.RequestContext{
			Source:    defaultRequestSource,
			TraceId:   optionalString(options.TraceID),
			SessionId: optionalString(options.SessionID),
		},
	}
}

func projectQueryMeta(options chainOptions) *projectsv1.QueryMeta {
	return &projectsv1.QueryMeta{
		Actor:     &projectsv1.Actor{Type: options.ActorType, Id: options.ActorID},
		RequestId: options.RequestID,
		RequestContext: &projectsv1.RequestContext{
			Source:    defaultRequestSource,
			TraceId:   optionalString(options.TraceID),
			SessionId: optionalString(options.SessionID),
		},
	}
}

func governanceQueryMeta(options chainOptions) *governancev1.QueryMeta {
	return &governancev1.QueryMeta{
		Actor:     &governancev1.Actor{Type: options.ActorType, Id: options.ActorID},
		RequestId: options.RequestID,
		RequestContext: &governancev1.RequestContext{
			Source:    defaultRequestSource,
			TraceId:   optionalString(options.TraceID),
			SessionId: optionalString(options.SessionID),
		},
	}
}

func runtimeQueryMeta(options chainOptions) *runtimev1.QueryMeta {
	return &runtimev1.QueryMeta{
		Actor:     &runtimev1.Actor{Type: options.ActorType, Id: options.ActorID},
		RequestId: options.RequestID,
		RequestContext: &runtimev1.RequestContext{
			Source:    defaultRequestSource,
			TraceId:   optionalString(options.TraceID),
			SessionId: optionalString(options.SessionID),
		},
	}
}

func selfDeployPlanTargetRef(planID string) string {
	planID = strings.TrimSpace(planID)
	if strings.HasPrefix(planID, selfDeployPlanTargetPrefix) {
		return planID
	}
	return selfDeployPlanTargetPrefix + planID
}

func selfDeployPlanStatus(value agentsv1.SelfDeployPlanStatus) string {
	return enumName(value.String(), "SELF_DEPLOY_PLAN_STATUS_")
}

func runtimeBuildStatus(value agentsv1.SelfDeployRuntimeBuildStatus) string {
	return enumName(value.String(), "SELF_DEPLOY_RUNTIME_BUILD_STATUS_")
}

func runtimeDeployStatus(value agentsv1.SelfDeployRuntimeDeployStatus) string {
	return enumName(value.String(), "SELF_DEPLOY_RUNTIME_DEPLOY_STATUS_")
}

func projectSignalStatus(value projectsv1.SelfDeploySignalStatus) string {
	return enumName(value.String(), "SELF_DEPLOY_SIGNAL_STATUS_")
}

func buildContextStatus(value runtimev1.BuildContextStatus) string {
	return enumName(value.String(), "BUILD_CONTEXT_STATUS_")
}

func runtimeJobStatus(value runtimev1.JobStatus) string {
	return enumName(value.String(), "JOB_STATUS_")
}

func gateRequestStatus(value governancev1.GateRequestStatus) string {
	return enumName(value.String(), "GATE_REQUEST_STATUS_")
}

func gateOutcome(value governancev1.GateOutcome) string {
	return enumName(value.String(), "GATE_OUTCOME_")
}

func governanceDecisionAttention(value governancev1.GovernanceDecisionAttention) string {
	return enumName(value.String(), "GOVERNANCE_DECISION_ATTENTION_")
}

func enumName(value string, prefix string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, prefix)
	value = strings.TrimSuffix(value, "_UNSPECIFIED")
	value = strings.ToLower(value)
	if value == "" {
		return "unspecified"
	}
	return value
}

func tokenFromEnvRef(envName string, service string) (string, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return "", nil
	}
	token := strings.TrimSpace(os.Getenv(envName))
	if token == "" {
		return "", fmt.Errorf("%s auth token env %s is empty", service, envName)
	}
	return token, nil
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return safeToken(*value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func envOr(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func safeToken(value string) string {
	value = strings.TrimSpace(value)
	if len([]byte(value)) > maxIdentifierBytes {
		value = string([]byte(value)[:maxIdentifierBytes])
	}
	return value
}

func safeSummary(value string) string {
	value = redact(strings.TrimSpace(value))
	if len([]byte(value)) > maxSummaryBytes {
		value = string([]byte(value)[:maxSummaryBytes])
	}
	return value
}

var (
	urlPattern       = regexp.MustCompile(`https?://[^\s)"']+`)
	secretKeyPattern = regexp.MustCompile(`(?i)(token|secret|authorization|password|payload|validated_payload_json|webhook_body|provider_response|kubeconfig|manifest|diff)(\s*[:=]\s*)[^\s,;]+`)
)

func redact(value string) string {
	value = urlPattern.ReplaceAllString(value, "[url hidden]")
	value = secretKeyPattern.ReplaceAllString(value, "$1$2[hidden]")
	return value
}
