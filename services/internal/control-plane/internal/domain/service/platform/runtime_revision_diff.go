package platform

import (
	"context"
	"regexp"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

var runtimeDiffReference = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func (service *Service) GetRuntimeRevisionDiff(ctx context.Context, p value.Principal, runRef, revisionRef string) (entity.RuntimeRevisionDiff, error) {
	if !runtimeDiffReference.MatchString(runRef) || revisionRef != "" && !runtimeDiffReference.MatchString(revisionRef) {
		return entity.RuntimeRevisionDiff{}, errs.ErrInvalid
	}
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.RuntimeRevisionDiff{}, err
	}
	current, previous, err := service.repository.GetRuntimeRevisionPublicPair(ctx, p, runRef, revisionRef)
	if err != nil {
		return entity.RuntimeRevisionDiff{}, err
	}
	return runtimeRevisionDiff(current, previous), nil
}

func runtimeRevisionDiff(current entity.RuntimeRevisionPublicProjection, previous *entity.RuntimeRevisionPublicProjection) entity.RuntimeRevisionDiff {
	result := entity.RuntimeRevisionDiff{Current: current.Identity}
	var old entity.RuntimeRevisionPublicProjection
	if previous != nil {
		old = *previous
		result.Previous = &old.Identity
	}
	fields := []struct {
		name              string
		current, previous entity.RuntimeRevisionDiffValue
	}{
		{"PROVIDER", current.Provider, old.Provider}, {"MODEL", current.Model, old.Model},
		{"RUNTIME_PROFILE", current.RuntimeProfile, old.RuntimeProfile},
		{"RUNTIME_CONFIGURATION", current.RuntimeConfiguration, old.RuntimeConfiguration},
		{"PROVIDER_POLICY", current.ProviderPolicy, old.ProviderPolicy},
		{"CONFIG_OVERLAY", current.ConfigOverlay, old.ConfigOverlay},
		{"ENVIRONMENT", current.Environment, old.Environment},
		{"ENVIRONMENT_BINDING", current.EnvironmentBinding, old.EnvironmentBinding},
		{"INSTRUCTION", current.Instruction, old.Instruction},
		{"INTEGRATION_GRANTS", current.IntegrationGrants, old.IntegrationGrants},
		{"IMAGE", current.Image, old.Image},
	}
	for _, field := range fields {
		if previous != nil && field.current == field.previous {
			continue
		}
		change := entity.RuntimeRevisionDiffChange{Component: field.name, Current: &field.current}
		if previous != nil {
			change.Previous = &field.previous
		}
		result.Changes = append(result.Changes, change)
	}
	return result
}
