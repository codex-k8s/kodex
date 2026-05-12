package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/value"
)

// PutPlacementRule creates or updates one placement rule.
func (s *Service) PutPlacementRule(ctx context.Context, input PutPlacementRuleInput) (entity.PlacementRule, error) {
	if _, err := s.repository.GetFleetScope(ctx, input.FleetScopeID); err != nil {
		return entity.PlacementRule{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, fleetActionPlacementRulePut, fleetResource(accesscatalog.ResourceFleetPlacementRule, uuid.Nil, &input.FleetScopeID)); err != nil {
		return entity.PlacementRule{}, err
	}
	if replay, ok, err := s.replayPlacementRule(ctx, input); ok || err != nil {
		return replay, err
	}

	rule, previousVersion, err := s.buildPlacementRuleForPut(ctx, input)
	if err != nil {
		return entity.PlacementRule{}, err
	}
	if err := validatePlacementRule(rule); err != nil {
		return entity.PlacementRule{}, err
	}
	result, err := commandResult(input.Meta, fleetOperationPutPlacementRule, fleetAggregatePlacementRule, rule.ID, rule.UpdatedAt)
	if err != nil {
		return entity.PlacementRule{}, err
	}
	if input.PlacementRuleID == nil && previousVersion == 0 {
		if err := s.repository.CreatePlacementRule(ctx, rule, result); err != nil {
			return entity.PlacementRule{}, err
		}
		return rule, nil
	}
	if err := s.repository.UpdatePlacementRule(ctx, rule, previousVersion, result); err != nil {
		return entity.PlacementRule{}, err
	}
	return rule, nil
}

// GetPlacementRule returns authoritative placement rule state.
func (s *Service) GetPlacementRule(ctx context.Context, id uuid.UUID, meta value.QueryMeta) (entity.PlacementRule, error) {
	return getAggregate(s, ctx, id, meta, fleetActionPlacementRuleRead, s.repository.GetPlacementRule, placementRuleResource)
}

// ListPlacementRules returns placement rules matching filter.
func (s *Service) ListPlacementRules(ctx context.Context, input ListPlacementRulesInput) (ListPlacementRulesResult, error) {
	if err := s.authorizeList(ctx, input.Meta, fleetActionPlacementRuleList, accesscatalog.ResourceFleetPlacementRule); err != nil {
		return ListPlacementRulesResult{}, err
	}
	rules, page, err := s.repository.ListPlacementRules(ctx, query.PlacementRuleFilter{
		FleetScopeID: input.FleetScopeID,
		Statuses:     input.Statuses,
		Page:         input.Page,
	})
	return ListPlacementRulesResult{Rules: rules, Page: page}, err
}

// ResolvePlacement returns an explained placement decision for one runtime request.
func (s *Service) ResolvePlacement(ctx context.Context, input ResolvePlacementInput) (entity.PlacementDecision, error) {
	if err := s.authorizeCommand(ctx, input.Meta, fleetActionPlacementResolve, globalFleetResource(accesscatalog.ResourceFleetPlacementDecision)); err != nil {
		return entity.PlacementDecision{}, err
	}
	request, fingerprint, err := buildPlacementRequest(input)
	if err != nil {
		return entity.PlacementDecision{}, err
	}
	if replay, ok, err := s.replayPlacementDecision(ctx, input.Meta, fingerprint); ok || err != nil {
		return replay, err
	}

	scopes, err := s.listAllActiveFleetScopes(ctx)
	if err != nil {
		return entity.PlacementDecision{}, err
	}
	servers, err := s.listAllActiveServers(ctx)
	if err != nil {
		return entity.PlacementDecision{}, err
	}
	clusters, err := s.listAllActiveClusters(ctx)
	if err != nil {
		return entity.PlacementDecision{}, err
	}
	rules, err := s.listAllActivePlacementRules(ctx, request.PreferredFleetScopeID)
	if err != nil {
		return entity.PlacementDecision{}, err
	}

	decision := s.resolvePlacementDecision(request, fingerprint, scopes, servers, clusters, rules)
	decision.CommandID = commandIDPtr(input.Meta.CommandID)
	if err := validatePlacementDecision(decision); err != nil {
		return entity.PlacementDecision{}, err
	}
	eventType := fleetEventPlacementRejected
	if decision.Status == enum.PlacementDecisionStatusResolved {
		eventType = fleetEventPlacementResolved
	}
	result, event, err := mutationArtifacts(input.Meta, fleetOperationResolvePlacement, fleetAggregatePlacementDecision, decision.ID, decision.CreatedAt, func() (entity.OutboxEvent, error) {
		return s.placementDecisionEvent(eventType, decision)
	})
	if err != nil {
		return entity.PlacementDecision{}, err
	}
	if err := s.repository.CreatePlacementDecision(ctx, decision, event, result); err != nil {
		return entity.PlacementDecision{}, err
	}
	return decision, nil
}

// GetPlacementDecision returns one stored placement decision.
func (s *Service) GetPlacementDecision(ctx context.Context, input GetPlacementDecisionInput) (entity.PlacementDecision, error) {
	return getAggregate(s, ctx, input.PlacementDecisionID, input.Meta, fleetActionPlacementDecisionRead, s.repository.GetPlacementDecision, placementDecisionResource)
}

// ListPlacementDecisions returns placement decisions matching filter.
func (s *Service) ListPlacementDecisions(ctx context.Context, input ListPlacementDecisionsInput) (ListPlacementDecisionsResult, error) {
	if err := s.authorizeList(ctx, input.Meta, fleetActionPlacementDecisionList, accesscatalog.ResourceFleetPlacementDecision); err != nil {
		return ListPlacementDecisionsResult{}, err
	}
	decisions, page, err := s.repository.ListPlacementDecisions(ctx, query.PlacementDecisionFilter{
		ProjectID:    input.ProjectID,
		RepositoryID: input.RepositoryID,
		FleetScopeID: input.FleetScopeID,
		ClusterID:    input.ClusterID,
		Statuses:     input.Statuses,
		Page:         input.Page,
	})
	return ListPlacementDecisionsResult{Decisions: decisions, Page: page}, err
}

type placementRuleMatcher struct {
	ProjectIDs      []uuid.UUID
	RepositoryIDs   []uuid.UUID
	ServiceKeys     []string
	RuntimeModes    []enum.RuntimeMode
	RuntimeProfiles []string
}

type placementConstraintSet struct {
	FleetScopeIDs   []uuid.UUID
	ClusterIDs      []uuid.UUID
	ClusterKeys     []string
	Regions         []string
	CapacityClasses []string
	RequireDefault  *bool
	AllowDegraded   *bool
}

type placementRequest struct {
	ProjectID             *uuid.UUID
	RepositoryID          *uuid.UUID
	ServiceKey            string
	RuntimeMode           enum.RuntimeMode
	RuntimeProfile        string
	PreferredFleetScopeID *uuid.UUID
	PreferredClusterID    *uuid.UUID
	RequestConstraints    placementConstraintSet
	RuntimeRequirements   placementConstraintSet
	InputJSON             []byte
}

type placementRuleDecoded struct {
	Aggregate   entity.PlacementRule
	Match       placementRuleMatcher
	Constraints placementConstraintSet
}

type placementCandidate struct {
	Scope           entity.FleetScope
	Cluster         entity.KubernetesCluster
	MatchedRule     *entity.PlacementRule
	MatchedPriority int64
	UsedDefaultPath bool
}

type placementDecisionInputJSON struct {
	ProjectID             string                  `json:"project_id,omitempty"`
	RepositoryID          string                  `json:"repository_id,omitempty"`
	ServiceKey            string                  `json:"service_key,omitempty"`
	RuntimeMode           string                  `json:"runtime_mode"`
	RuntimeProfile        string                  `json:"runtime_profile"`
	PreferredFleetScopeID string                  `json:"preferred_fleet_scope_id,omitempty"`
	PreferredClusterID    string                  `json:"preferred_cluster_id,omitempty"`
	RequestConstraints    placementConstraintJSON `json:"placement_constraints"`
	RuntimeRequirements   placementConstraintJSON `json:"runtime_requirements"`
}

type placementConstraintJSON struct {
	FleetScopeIDs   []string `json:"fleet_scope_ids,omitempty"`
	ClusterIDs      []string `json:"cluster_ids,omitempty"`
	ClusterKeys     []string `json:"cluster_keys,omitempty"`
	Regions         []string `json:"regions,omitempty"`
	CapacityClasses []string `json:"capacity_classes,omitempty"`
	RequireDefault  *bool    `json:"require_default,omitempty"`
	AllowDegraded   *bool    `json:"allow_degraded,omitempty"`
}

type placementRuleJSON struct {
	ProjectIDs      []string `json:"project_ids,omitempty"`
	RepositoryIDs   []string `json:"repository_ids,omitempty"`
	ServiceKeys     []string `json:"service_keys,omitempty"`
	RuntimeModes    []string `json:"runtime_modes,omitempty"`
	RuntimeProfiles []string `json:"runtime_profiles,omitempty"`
}

type placementDecisionContext struct {
	Request      placementRequest
	Scopes       []entity.FleetScope
	Servers      map[uuid.UUID]entity.Server
	Clusters     []entity.KubernetesCluster
	RulesByScope map[uuid.UUID][]placementRuleDecoded
}

func (s *Service) replayPlacementRule(ctx context.Context, input PutPlacementRuleInput) (entity.PlacementRule, bool, error) {
	rule, ok, err := replayAggregate(s, ctx, input.Meta, fleetOperationPutPlacementRule, fleetAggregatePlacementRule, s.repository.GetPlacementRule)
	if err != nil || !ok {
		return entity.PlacementRule{}, ok, err
	}
	if input.PlacementRuleID != nil && *input.PlacementRuleID != rule.ID {
		return entity.PlacementRule{}, true, errs.ErrConflict
	}
	if input.PlacementRuleID == nil && (rule.FleetScopeID != input.FleetScopeID || rule.RuleKey != trimString(input.RuleKey)) {
		return entity.PlacementRule{}, true, errs.ErrConflict
	}
	queryMeta := value.QueryMeta{Actor: input.Meta.Actor, RequestID: input.Meta.RequestID, RequestContext: input.Meta.RequestContext}
	if err := s.authorizeQuery(ctx, queryMeta, fleetActionPlacementRuleRead, placementRuleResource(rule)); err != nil {
		return entity.PlacementRule{}, true, err
	}
	return rule, true, nil
}

func (s *Service) buildPlacementRuleForPut(ctx context.Context, input PutPlacementRuleInput) (entity.PlacementRule, int64, error) {
	now := s.clock.Now()
	if input.PlacementRuleID != nil {
		current, err := s.loadPlacementRuleForMutation(ctx, *input.PlacementRuleID, input.Meta, fleetActionPlacementRulePut, fleetOperationPutPlacementRule)
		if err != nil {
			return entity.PlacementRule{}, 0, err
		}
		previousVersion, err := expectedVersion(input.Meta)
		if err != nil {
			return entity.PlacementRule{}, 0, err
		}
		updated := current
		updated.Base = updatedBase(current.Base, now)
		updated.FleetScopeID = input.FleetScopeID
		updated.RuleKey = trimString(input.RuleKey)
		updated.Status = defaultPlacementRuleStatus(input.Status)
		updated.Priority = input.Priority
		updated.MatchJSON = defaultJSON(input.MatchJSON)
		updated.ConstraintsJSON = defaultJSON(input.ConstraintsJSON)
		return updated, previousVersion, nil
	}
	existing, err := s.repository.GetPlacementRuleByScopeKey(ctx, input.FleetScopeID, trimString(input.RuleKey))
	switch {
	case err == nil:
		previousVersion, versionErr := expectedVersion(input.Meta)
		if versionErr != nil {
			return entity.PlacementRule{}, 0, versionErr
		}
		updated := existing
		updated.Base = updatedBase(existing.Base, now)
		updated.Status = defaultPlacementRuleStatus(input.Status)
		updated.Priority = input.Priority
		updated.MatchJSON = defaultJSON(input.MatchJSON)
		updated.ConstraintsJSON = defaultJSON(input.ConstraintsJSON)
		return updated, previousVersion, nil
	case err != nil && err != errs.ErrNotFound:
		return entity.PlacementRule{}, 0, err
	}
	return entity.PlacementRule{
		Base:            newBase(s.ids.New(), now),
		FleetScopeID:    input.FleetScopeID,
		RuleKey:         trimString(input.RuleKey),
		Status:          defaultPlacementRuleStatus(input.Status),
		Priority:        input.Priority,
		MatchJSON:       defaultJSON(input.MatchJSON),
		ConstraintsJSON: defaultJSON(input.ConstraintsJSON),
	}, 0, nil
}

func (s *Service) resolvePlacementDecision(request placementRequest, fingerprint string, scopes []entity.FleetScope, servers map[uuid.UUID]entity.Server, clusters []entity.KubernetesCluster, rules []entity.PlacementRule) entity.PlacementDecision {
	now := s.clock.Now()
	contextState := placementDecisionContext{
		Request:      request,
		Scopes:       sortedScopes(scopes),
		Servers:      servers,
		Clusters:     sortedClusters(clusters),
		RulesByScope: groupActiveRules(rules),
	}
	decision := entity.PlacementDecision{
		ID:                 s.ids.New(),
		RequestFingerprint: fingerprint,
		Status:             enum.PlacementDecisionStatusRejected,
		ProjectID:          request.ProjectID,
		RepositoryID:       request.RepositoryID,
		RuntimeMode:        request.RuntimeMode,
		RuntimeProfile:     request.RuntimeProfile,
		InputJSON:          append([]byte(nil), request.InputJSON...),
		CreatedAt:          now,
	}
	candidate, reasonCode, reasonMessage := selectPlacementCandidate(contextState)
	decision.ReasonCode = reasonCode
	decision.ReasonMessage = reasonMessage
	if candidate == nil {
		return decision
	}
	decision.Status = enum.PlacementDecisionStatusResolved
	decision.FleetScopeID = &candidate.Scope.ID
	decision.ClusterID = &candidate.Cluster.ID
	decision.UsedDefaultPath = candidate.UsedDefaultPath
	return decision
}

func selectPlacementCandidate(state placementDecisionContext) (*placementCandidate, string, string) {
	if len(state.Scopes) == 0 {
		return nil, "no_active_scope", "No active fleet scope is available"
	}
	if len(state.Clusters) == 0 {
		return nil, "no_active_cluster", "No active Kubernetes cluster is available"
	}
	candidates := make([]placementCandidate, 0)
	for index := range state.Scopes {
		scope := state.Scopes[index]
		if state.Request.PreferredFleetScopeID != nil && scope.ID != *state.Request.PreferredFleetScopeID {
			continue
		}
		if len(state.Request.RequestConstraints.FleetScopeIDs) > 0 && !containsUUID(state.Request.RequestConstraints.FleetScopeIDs, scope.ID) {
			continue
		}
		if len(state.Request.RuntimeRequirements.FleetScopeIDs) > 0 && !containsUUID(state.Request.RuntimeRequirements.FleetScopeIDs, scope.ID) {
			continue
		}
		decodedRules := state.RulesByScope[scope.ID]
		matchedRule, hasRules := firstMatchingRule(decodedRules, state.Request)
		if len(decodedRules) > 0 && !hasRules {
			continue
		}
		mergedConstraints, ok := mergePlacementConstraints(
			state.Request.RequestConstraints,
			state.Request.RuntimeRequirements,
			constraintsFromRule(matchedRule),
		)
		if !ok {
			continue
		}
		scopeCandidates := resolveScopeCandidates(scope, state.Clusters, state.Servers, state.Request, mergedConstraints, matchedRule)
		candidates = append(candidates, scopeCandidates...)
	}
	if len(candidates) == 0 {
		if state.Request.PreferredClusterID != nil {
			return nil, "preferred_cluster_unavailable", "Preferred Kubernetes cluster is not eligible for placement"
		}
		if state.Request.PreferredFleetScopeID != nil {
			return nil, "preferred_scope_unavailable", "Preferred fleet scope has no eligible Kubernetes cluster"
		}
		return nil, "placement_candidates_not_found", "No eligible Kubernetes cluster matched the placement constraints"
	}
	sort.SliceStable(candidates, func(i int, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if (left.MatchedRule != nil) != (right.MatchedRule != nil) {
			return left.MatchedRule != nil
		}
		if left.MatchedRule != nil && right.MatchedRule != nil && left.MatchedPriority != right.MatchedPriority {
			return left.MatchedPriority < right.MatchedPriority
		}
		if left.UsedDefaultPath != right.UsedDefaultPath {
			return left.UsedDefaultPath
		}
		if left.Cluster.IsDefault != right.Cluster.IsDefault {
			return left.Cluster.IsDefault
		}
		if left.Scope.ScopeKey != right.Scope.ScopeKey {
			return left.Scope.ScopeKey < right.Scope.ScopeKey
		}
		if left.Cluster.ClusterKey != right.Cluster.ClusterKey {
			return left.Cluster.ClusterKey < right.Cluster.ClusterKey
		}
		return left.Cluster.ID.String() < right.Cluster.ID.String()
	})
	selected := candidates[0]
	if state.Request.PreferredClusterID != nil {
		return &selected, "preferred_cluster_selected", "Preferred Kubernetes cluster matched placement constraints"
	}
	if selected.MatchedRule != nil {
		return &selected, "placement_rule_selected", "Placement rule selected the Kubernetes cluster"
	}
	if selected.UsedDefaultPath {
		return &selected, "default_platform_cluster_selected", "Platform default Kubernetes cluster selected as fallback"
	}
	if selected.Cluster.IsDefault {
		return &selected, "default_scope_cluster_selected", "Default Kubernetes cluster selected inside the fleet scope"
	}
	return &selected, "first_eligible_cluster_selected", "First eligible Kubernetes cluster selected deterministically"
}

func resolveScopeCandidates(scope entity.FleetScope, clusters []entity.KubernetesCluster, servers map[uuid.UUID]entity.Server, request placementRequest, constraints placementConstraintSet, matchedRule *entity.PlacementRule) []placementCandidate {
	candidates := make([]placementCandidate, 0)
	for index := range clusters {
		cluster := clusters[index]
		if cluster.FleetScopeID != scope.ID {
			continue
		}
		if request.PreferredClusterID != nil && cluster.ID != *request.PreferredClusterID {
			continue
		}
		if cluster.Status != enum.KubernetesClusterStatusActive {
			continue
		}
		if cluster.ServerID != nil {
			server, ok := servers[*cluster.ServerID]
			if !ok || server.Status != enum.ServerStatusActive {
				continue
			}
		}
		if !matchesPlacementConstraints(cluster, constraints) {
			continue
		}
		if !clusterHealthAllowsPlacement(cluster.LastHealthStatus, constraints.AllowDegraded) {
			continue
		}
		candidates = append(candidates, placementCandidate{
			Scope:           scope,
			Cluster:         cluster,
			MatchedRule:     matchedRule,
			MatchedPriority: matchedRulePriority(matchedRule),
			UsedDefaultPath: scope.ScopeKey == platformDefaultKey && scope.IsDefault && cluster.IsDefault && matchedRule == nil,
		})
	}
	return candidates
}

func buildPlacementRequest(input ResolvePlacementInput) (placementRequest, string, error) {
	if input.RuntimeMode == "" || trimString(input.RuntimeProfile) == "" {
		return placementRequest{}, "", errs.ErrInvalidArgument
	}
	requestConstraints, err := parsePlacementConstraintJSON(defaultJSON(input.PlacementConstraintsJSON))
	if err != nil {
		return placementRequest{}, "", err
	}
	runtimeRequirements, err := parsePlacementConstraintJSON(defaultJSON(input.RuntimeRequirementsJSON))
	if err != nil {
		return placementRequest{}, "", err
	}
	inputJSON, err := marshalPlacementDecisionInput(placementDecisionInputJSON{
		ProjectID:             uuidPtrString(input.ProjectID),
		RepositoryID:          uuidPtrString(input.RepositoryID),
		ServiceKey:            trimString(input.ServiceKey),
		RuntimeMode:           string(input.RuntimeMode),
		RuntimeProfile:        trimString(input.RuntimeProfile),
		PreferredFleetScopeID: uuidPtrString(input.PreferredFleetScopeID),
		PreferredClusterID:    uuidPtrString(input.PreferredClusterID),
		RequestConstraints:    placementConstraintSetToJSON(requestConstraints),
		RuntimeRequirements:   placementConstraintSetToJSON(runtimeRequirements),
	})
	if err != nil {
		return placementRequest{}, "", err
	}
	digest := sha256.Sum256(inputJSON)
	return placementRequest{
		ProjectID:             input.ProjectID,
		RepositoryID:          input.RepositoryID,
		ServiceKey:            trimString(input.ServiceKey),
		RuntimeMode:           input.RuntimeMode,
		RuntimeProfile:        trimString(input.RuntimeProfile),
		PreferredFleetScopeID: input.PreferredFleetScopeID,
		PreferredClusterID:    input.PreferredClusterID,
		RequestConstraints:    requestConstraints,
		RuntimeRequirements:   runtimeRequirements,
		InputJSON:             inputJSON,
	}, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func marshalPlacementDecisionInput(input placementDecisionInputJSON) ([]byte, error) {
	return json.Marshal(input)
}

func parsePlacementConstraintJSON(payload []byte) (placementConstraintSet, error) {
	if err := requireJSONObject(payload); err != nil {
		return placementConstraintSet{}, err
	}
	var decoded placementConstraintJSON
	if err := json.Unmarshal(defaultJSON(payload), &decoded); err != nil {
		return placementConstraintSet{}, errs.ErrInvalidArgument
	}
	fleetScopeIDs, err := parseUUIDStrings(decoded.FleetScopeIDs)
	if err != nil {
		return placementConstraintSet{}, err
	}
	clusterIDs, err := parseUUIDStrings(decoded.ClusterIDs)
	if err != nil {
		return placementConstraintSet{}, err
	}
	return placementConstraintSet{
		FleetScopeIDs:   fleetScopeIDs,
		ClusterIDs:      clusterIDs,
		ClusterKeys:     sortedUniqueStrings(decoded.ClusterKeys),
		Regions:         sortedUniqueStrings(decoded.Regions),
		CapacityClasses: sortedUniqueStrings(decoded.CapacityClasses),
		RequireDefault:  decoded.RequireDefault,
		AllowDegraded:   decoded.AllowDegraded,
	}, nil
}

func parsePlacementRule(rule entity.PlacementRule) (placementRuleDecoded, error) {
	if err := requireJSONObject(rule.MatchJSON); err != nil {
		return placementRuleDecoded{}, err
	}
	var matchJSON placementRuleJSON
	if err := json.Unmarshal(defaultJSON(rule.MatchJSON), &matchJSON); err != nil {
		return placementRuleDecoded{}, errs.ErrInvalidArgument
	}
	projectIDs, err := parseUUIDStrings(matchJSON.ProjectIDs)
	if err != nil {
		return placementRuleDecoded{}, err
	}
	repositoryIDs, err := parseUUIDStrings(matchJSON.RepositoryIDs)
	if err != nil {
		return placementRuleDecoded{}, err
	}
	runtimeModes, err := parseRuntimeModes(matchJSON.RuntimeModes)
	if err != nil {
		return placementRuleDecoded{}, err
	}
	constraints, err := parsePlacementConstraintJSON(rule.ConstraintsJSON)
	if err != nil {
		return placementRuleDecoded{}, err
	}
	return placementRuleDecoded{
		Aggregate: rule,
		Match: placementRuleMatcher{
			ProjectIDs:      projectIDs,
			RepositoryIDs:   repositoryIDs,
			ServiceKeys:     sortedUniqueStrings(matchJSON.ServiceKeys),
			RuntimeModes:    runtimeModes,
			RuntimeProfiles: sortedUniqueStrings(matchJSON.RuntimeProfiles),
		},
		Constraints: constraints,
	}, nil
}

func parseUUIDStrings(values []string) ([]uuid.UUID, error) {
	if len(values) == 0 {
		return nil, nil
	}
	unique := make(map[uuid.UUID]struct{}, len(values))
	parsed := make([]uuid.UUID, 0, len(values))
	for index := range values {
		trimmed := strings.TrimSpace(values[index])
		if trimmed == "" {
			continue
		}
		parsedID, err := uuid.Parse(trimmed)
		if err != nil {
			return nil, errs.ErrInvalidArgument
		}
		if _, exists := unique[parsedID]; exists {
			continue
		}
		unique[parsedID] = struct{}{}
		parsed = append(parsed, parsedID)
	}
	sort.Slice(parsed, func(i int, j int) bool { return parsed[i].String() < parsed[j].String() })
	return parsed, nil
}

func parseRuntimeModes(values []string) ([]enum.RuntimeMode, error) {
	if len(values) == 0 {
		return nil, nil
	}
	unique := make(map[enum.RuntimeMode]struct{}, len(values))
	parsed := make([]enum.RuntimeMode, 0, len(values))
	for index := range values {
		mode := enum.RuntimeMode(strings.TrimSpace(values[index]))
		if !isRuntimeMode(mode) {
			return nil, errs.ErrInvalidArgument
		}
		if _, exists := unique[mode]; exists {
			continue
		}
		unique[mode] = struct{}{}
		parsed = append(parsed, mode)
	}
	sort.Slice(parsed, func(i int, j int) bool { return parsed[i] < parsed[j] })
	return parsed, nil
}

func firstMatchingRule(rules []placementRuleDecoded, request placementRequest) (*entity.PlacementRule, bool) {
	for index := range rules {
		if !ruleMatchesRequest(rules[index].Match, request) {
			continue
		}
		rule := rules[index].Aggregate
		return &rule, true
	}
	return nil, len(rules) == 0
}

func ruleMatchesRequest(match placementRuleMatcher, request placementRequest) bool {
	if len(match.ProjectIDs) > 0 && (request.ProjectID == nil || !containsUUID(match.ProjectIDs, *request.ProjectID)) {
		return false
	}
	if len(match.RepositoryIDs) > 0 && (request.RepositoryID == nil || !containsUUID(match.RepositoryIDs, *request.RepositoryID)) {
		return false
	}
	if len(match.ServiceKeys) > 0 && !containsString(match.ServiceKeys, request.ServiceKey) {
		return false
	}
	if len(match.RuntimeModes) > 0 && !containsRuntimeMode(match.RuntimeModes, request.RuntimeMode) {
		return false
	}
	if len(match.RuntimeProfiles) > 0 && !containsString(match.RuntimeProfiles, request.RuntimeProfile) {
		return false
	}
	return true
}

func constraintsFromRule(rule *entity.PlacementRule) placementConstraintSet {
	if rule == nil {
		return placementConstraintSet{}
	}
	decoded, err := parsePlacementRule(*rule)
	if err != nil {
		return placementConstraintSet{}
	}
	return decoded.Constraints
}

func mergePlacementConstraints(parts ...placementConstraintSet) (placementConstraintSet, bool) {
	merged := placementConstraintSet{}
	for index := range parts {
		part := parts[index]
		var ok bool
		merged.FleetScopeIDs, ok = intersectOrAdoptUUIDs(merged.FleetScopeIDs, part.FleetScopeIDs)
		if !ok {
			return placementConstraintSet{}, false
		}
		merged.ClusterIDs, ok = intersectOrAdoptUUIDs(merged.ClusterIDs, part.ClusterIDs)
		if !ok {
			return placementConstraintSet{}, false
		}
		merged.ClusterKeys, ok = intersectOrAdoptStrings(merged.ClusterKeys, part.ClusterKeys)
		if !ok {
			return placementConstraintSet{}, false
		}
		merged.Regions, ok = intersectOrAdoptStrings(merged.Regions, part.Regions)
		if !ok {
			return placementConstraintSet{}, false
		}
		merged.CapacityClasses, ok = intersectOrAdoptStrings(merged.CapacityClasses, part.CapacityClasses)
		if !ok {
			return placementConstraintSet{}, false
		}
		merged.RequireDefault = mergeRequireDefault(merged.RequireDefault, part.RequireDefault)
		merged.AllowDegraded = mergeAllowDegraded(merged.AllowDegraded, part.AllowDegraded)
	}
	return merged, true
}

func matchesPlacementConstraints(cluster entity.KubernetesCluster, constraints placementConstraintSet) bool {
	if len(constraints.ClusterIDs) > 0 && !containsUUID(constraints.ClusterIDs, cluster.ID) {
		return false
	}
	if len(constraints.ClusterKeys) > 0 && !containsString(constraints.ClusterKeys, cluster.ClusterKey) {
		return false
	}
	if len(constraints.Regions) > 0 && !containsString(constraints.Regions, cluster.Region) {
		return false
	}
	if len(constraints.CapacityClasses) > 0 && !containsString(constraints.CapacityClasses, cluster.CapacityClass) {
		return false
	}
	return constraints.RequireDefault == nil || !*constraints.RequireDefault || cluster.IsDefault
}

func clusterHealthAllowsPlacement(status enum.ClusterHealthStatus, allowDegraded *bool) bool {
	switch defaultClusterHealth(status) {
	case enum.ClusterHealthStatusHealthy:
		return true
	case enum.ClusterHealthStatusDegraded:
		return allowDegraded != nil && *allowDegraded
	default:
		return false
	}
}

func intersectOrAdoptUUIDs(current []uuid.UUID, next []uuid.UUID) ([]uuid.UUID, bool) {
	switch {
	case len(current) == 0:
		return append([]uuid.UUID(nil), next...), true
	case len(next) == 0:
		return current, true
	}
	values := make([]uuid.UUID, 0)
	for index := range current {
		if containsUUID(next, current[index]) {
			values = append(values, current[index])
		}
	}
	if len(values) == 0 {
		return nil, false
	}
	return values, true
}

func intersectOrAdoptStrings(current []string, next []string) ([]string, bool) {
	switch {
	case len(current) == 0:
		return append([]string(nil), next...), true
	case len(next) == 0:
		return current, true
	}
	values := make([]string, 0)
	for index := range current {
		if containsString(next, current[index]) {
			values = append(values, current[index])
		}
	}
	if len(values) == 0 {
		return nil, false
	}
	return values, true
}

func mergeRequireDefault(current *bool, next *bool) *bool {
	switch {
	case current == nil:
		return next
	case next == nil:
		return current
	}
	value := *current || *next
	return &value
}

func mergeAllowDegraded(current *bool, next *bool) *bool {
	switch {
	case current == nil:
		return next
	case next == nil:
		return current
	}
	value := *current && *next
	return &value
}

func sortedUniqueStrings(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for index := range values {
		trimmed := strings.TrimSpace(values[index])
		if trimmed == "" {
			continue
		}
		if _, exists := unique[trimmed]; exists {
			continue
		}
		unique[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func containsUUID(values []uuid.UUID, expected uuid.UUID) bool {
	for index := range values {
		if values[index] == expected {
			return true
		}
	}
	return false
}

func containsString(values []string, expected string) bool {
	for index := range values {
		if values[index] == expected {
			return true
		}
	}
	return false
}

func containsRuntimeMode(values []enum.RuntimeMode, expected enum.RuntimeMode) bool {
	for index := range values {
		if values[index] == expected {
			return true
		}
	}
	return false
}

func isRuntimeMode(value enum.RuntimeMode) bool {
	switch value {
	case enum.RuntimeModeCodeOnly, enum.RuntimeModeFullEnv, enum.RuntimeModeReadOnlyProduction, enum.RuntimeModePlatformJob:
		return true
	default:
		return false
	}
}

func placementConstraintSetToJSON(constraints placementConstraintSet) placementConstraintJSON {
	return placementConstraintJSON{
		FleetScopeIDs:   uuidSliceStrings(constraints.FleetScopeIDs),
		ClusterIDs:      uuidSliceStrings(constraints.ClusterIDs),
		ClusterKeys:     append([]string(nil), constraints.ClusterKeys...),
		Regions:         append([]string(nil), constraints.Regions...),
		CapacityClasses: append([]string(nil), constraints.CapacityClasses...),
		RequireDefault:  constraints.RequireDefault,
		AllowDegraded:   constraints.AllowDegraded,
	}
}

func uuidSliceStrings(values []uuid.UUID) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for index := range values {
		result = append(result, values[index].String())
	}
	return result
}

func sortDefaultEntities[T any](values []T, isDefault func(T) bool, key func(T) string, id func(T) string) []T {
	items := append([]T(nil), values...)
	sort.SliceStable(items, func(i int, j int) bool {
		if isDefault(items[i]) != isDefault(items[j]) {
			return isDefault(items[i])
		}
		if key(items[i]) != key(items[j]) {
			return key(items[i]) < key(items[j])
		}
		return id(items[i]) < id(items[j])
	})
	return items
}

func fleetScopeIsDefault(item entity.FleetScope) bool {
	return item.IsDefault
}

func fleetScopeSortKey(item entity.FleetScope) string {
	return item.ScopeKey
}

func fleetScopeSortID(item entity.FleetScope) string {
	return item.ID.String()
}

func sortedScopes(values []entity.FleetScope) []entity.FleetScope {
	return sortDefaultEntities(values, fleetScopeIsDefault, fleetScopeSortKey, fleetScopeSortID)
}

func clusterIsDefault(item entity.KubernetesCluster) bool {
	return item.IsDefault
}

func clusterSortKey(item entity.KubernetesCluster) string {
	return item.ClusterKey
}

func clusterSortID(item entity.KubernetesCluster) string {
	return item.ID.String()
}

func sortedClusters(values []entity.KubernetesCluster) []entity.KubernetesCluster {
	return sortDefaultEntities(values, clusterIsDefault, clusterSortKey, clusterSortID)
}

func groupActiveRules(rules []entity.PlacementRule) map[uuid.UUID][]placementRuleDecoded {
	grouped := make(map[uuid.UUID][]placementRuleDecoded, len(rules))
	for index := range rules {
		decoded, err := parsePlacementRule(rules[index])
		if err != nil {
			continue
		}
		grouped[decoded.Aggregate.FleetScopeID] = append(grouped[decoded.Aggregate.FleetScopeID], decoded)
	}
	for scopeID := range grouped {
		sort.SliceStable(grouped[scopeID], func(i int, j int) bool {
			if grouped[scopeID][i].Aggregate.Priority != grouped[scopeID][j].Aggregate.Priority {
				return grouped[scopeID][i].Aggregate.Priority < grouped[scopeID][j].Aggregate.Priority
			}
			return grouped[scopeID][i].Aggregate.RuleKey < grouped[scopeID][j].Aggregate.RuleKey
		})
	}
	return grouped
}

func matchedRulePriority(rule *entity.PlacementRule) int64 {
	if rule == nil {
		return 0
	}
	return rule.Priority
}

func defaultPlacementRuleStatus(status enum.PlacementRuleStatus) enum.PlacementRuleStatus {
	if status == "" {
		return enum.PlacementRuleStatusActive
	}
	return status
}

func validatePlacementRule(rule entity.PlacementRule) error {
	if rule.ID == uuid.Nil || rule.FleetScopeID == uuid.Nil || trimString(rule.RuleKey) == "" || defaultPlacementRuleStatus(rule.Status) == "" {
		return errs.ErrInvalidArgument
	}
	if err := requireJSONObject(rule.MatchJSON); err != nil {
		return err
	}
	return requireJSONObject(rule.ConstraintsJSON)
}

func validatePlacementDecision(decision entity.PlacementDecision) error {
	if decision.ID == uuid.Nil || trimString(decision.RequestFingerprint) == "" || decision.CreatedAt.IsZero() {
		return errs.ErrInvalidArgument
	}
	if !isRuntimeMode(decision.RuntimeMode) || trimString(decision.RuntimeProfile) == "" {
		return errs.ErrInvalidArgument
	}
	if err := requireJSONObject(decision.InputJSON); err != nil {
		return err
	}
	switch decision.Status {
	case enum.PlacementDecisionStatusResolved:
		if decision.FleetScopeID == nil || *decision.FleetScopeID == uuid.Nil || decision.ClusterID == nil || *decision.ClusterID == uuid.Nil {
			return errs.ErrInvalidArgument
		}
	case enum.PlacementDecisionStatusRejected:
		if decision.FleetScopeID != nil || decision.ClusterID != nil {
			return errs.ErrInvalidArgument
		}
	default:
		return errs.ErrInvalidArgument
	}
	return nil
}

func placementRuleResource(rule entity.PlacementRule) resourceRef {
	return fleetResource(accesscatalog.ResourceFleetPlacementRule, rule.ID, &rule.FleetScopeID)
}

func placementDecisionResource(decision entity.PlacementDecision) resourceRef {
	return fleetResource(accesscatalog.ResourceFleetPlacementDecision, decision.ID, decision.FleetScopeID)
}

func (s *Service) loadPlacementRuleForMutation(ctx context.Context, id uuid.UUID, meta value.CommandMeta, action string, operation string) (entity.PlacementRule, error) {
	return loadForMutation(s, ctx, id, meta, action, operation, fleetAggregatePlacementRule, s.repository.GetPlacementRule, placementRuleResource)
}

func (s *Service) replayPlacementDecision(ctx context.Context, meta value.CommandMeta, requestFingerprint string) (entity.PlacementDecision, bool, error) {
	decision, ok, err := replayAggregate(s, ctx, meta, fleetOperationResolvePlacement, fleetAggregatePlacementDecision, s.repository.GetPlacementDecision)
	if err != nil || !ok {
		return entity.PlacementDecision{}, ok, err
	}
	if decision.RequestFingerprint != requestFingerprint {
		return entity.PlacementDecision{}, true, errs.ErrConflict
	}
	queryMeta := value.QueryMeta{Actor: meta.Actor, RequestID: meta.RequestID, RequestContext: meta.RequestContext}
	if err := s.authorizeQuery(ctx, queryMeta, fleetActionPlacementDecisionRead, placementDecisionResource(decision)); err != nil {
		return entity.PlacementDecision{}, true, err
	}
	return decision, true, nil
}

func collectPages[T any](fetch func(value.PageRequest) ([]T, value.PageResult, error)) ([]T, error) {
	items := make([]T, 0)
	page := value.PageRequest{}
	for {
		batch, result, err := fetch(page)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if result.NextPageToken == "" {
			return items, nil
		}
		page = value.PageRequest{PageToken: result.NextPageToken}
	}
}

func collectFilteredPages[T any, F any](
	ctx context.Context,
	filter F,
	setPage func(F, value.PageRequest) F,
	fetch func(context.Context, F) ([]T, value.PageResult, error),
) ([]T, error) {
	return collectPages(func(page value.PageRequest) ([]T, value.PageResult, error) {
		return fetch(ctx, setPage(filter, page))
	})
}

func setFleetScopeFilterPage(filter query.FleetScopeFilter, page value.PageRequest) query.FleetScopeFilter {
	filter.Page = page
	return filter
}

func setServerFilterPage(filter query.ServerFilter, page value.PageRequest) query.ServerFilter {
	filter.Page = page
	return filter
}

func setClusterFilterPage(filter query.KubernetesClusterFilter, page value.PageRequest) query.KubernetesClusterFilter {
	filter.Page = page
	return filter
}

func setPlacementRuleFilterPage(filter query.PlacementRuleFilter, page value.PageRequest) query.PlacementRuleFilter {
	filter.Page = page
	return filter
}

func (s *Service) listAllActiveFleetScopes(ctx context.Context) ([]entity.FleetScope, error) {
	return collectFilteredPages(ctx, query.FleetScopeFilter{
		Statuses: []enum.FleetScopeStatus{enum.FleetScopeStatusActive},
	}, setFleetScopeFilterPage, s.repository.ListFleetScopes)
}

func (s *Service) listAllActiveServers(ctx context.Context) (map[uuid.UUID]entity.Server, error) {
	items, err := collectFilteredPages(ctx, query.ServerFilter{
		Statuses: []enum.ServerStatus{enum.ServerStatusActive},
	}, setServerFilterPage, s.repository.ListServers)
	if err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]entity.Server)
	for index := range items {
		result[items[index].ID] = items[index]
	}
	return result, nil
}

func (s *Service) listAllActiveClusters(ctx context.Context) ([]entity.KubernetesCluster, error) {
	return collectFilteredPages(ctx, query.KubernetesClusterFilter{
		Statuses: []enum.KubernetesClusterStatus{enum.KubernetesClusterStatusActive},
	}, setClusterFilterPage, s.repository.ListKubernetesClusters)
}

func (s *Service) listAllActivePlacementRules(ctx context.Context, preferredScopeID *uuid.UUID) ([]entity.PlacementRule, error) {
	return collectFilteredPages(ctx, query.PlacementRuleFilter{
		FleetScopeID: preferredScopeID,
		Statuses:     []enum.PlacementRuleStatus{enum.PlacementRuleStatusActive},
	}, setPlacementRuleFilterPage, s.repository.ListPlacementRules)
}
