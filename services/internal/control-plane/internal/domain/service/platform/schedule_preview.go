package platform

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (s *Service) PreviewScheduleMaterialization(ctx context.Context, p value.Principal, input command.ScheduleInput, ref string, expected int64, scheduledFor time.Time, expectedDigest string, full bool, mode string) (promptservice.Materialization, entity.SchedulePromptPreviewPin, []entity.TemplateVariable, error) {
	var result promptservice.Materialization
	var pin entity.SchedulePromptPreviewPin
	if expectedDigest != "" {
		decoded, err := hex.DecodeString(expectedDigest)
		if err != nil || len(decoded) != 32 || strings.ToLower(expectedDigest) != expectedDigest {
			return result, pin, nil, errs.ErrInvalid
		}
	}
	p, err := s.principal(ctx, p)
	if err != nil {
		return result, pin, nil, err
	}
	if p.CallerWorkload != "control-api-gateway" || p.Permission != "platform.query.schedules.preview" {
		return result, pin, nil, errs.ErrForbidden
	}
	snapshot, pin, variables, err := s.repository.GetSchedulePromptPreviewSnapshot(ctx, p, input, ref, expected, scheduledFor, mode)
	if err != nil {
		return result, pin, nil, err
	}
	if expectedDigest != "" && expectedDigest != snapshot.ContextPin.Digest {
		return result, pin, nil, errs.ErrVersionMismatch
	}
	if full {
		target := entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: snapshot.ProjectRef, ResourceKind: input.Target.Type, ResourceRef: input.Target.Ref}
		if pin.Continuation {
			target.ResourceKind = "SESSION"
			target.ResourceRef = pin.SessionRef
		}
		access, accessErr := s.repository.QueryEffectiveAccess(ctx, p, "", target, []string{"prompt.full.view"}, time.Now().UTC())
		if accessErr != nil || len(access.Decisions) != 1 || !access.Decisions[0].Allowed {
			return result, pin, nil, errs.ErrForbidden
		}
		if !p.InteractiveAuthenticationIsFresh(time.Now().UTC(), promptFullMaterializationMaximumAuthenticationAge) {
			return result, pin, nil, errs.ErrFreshAuthenticationRequired
		}
	}
	result, err = promptservice.Materialize(snapshot.TemplateContent, promptservice.FromSnapshot(snapshot))
	result.ContextPin = snapshot.ContextPin
	if err != nil {
		return result, pin, nil, errs.ErrInvalid
	}
	return result, pin, variables, nil
}
