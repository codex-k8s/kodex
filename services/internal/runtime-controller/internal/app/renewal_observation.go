package app

import (
	"context"
	"errors"
	"regexp"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type renewalPhase string

const (
	renewalPhaseTrack           renewalPhase = "TRACK"
	renewalPhaseDrain           renewalPhase = "DRAIN"
	renewalPhaseMaterialization renewalPhase = "MATERIALIZATION"
	renewalFailureMessage                    = "runtime lease renewal failed before cleanup cancellation"
	renewalPhaseKey                          = "phase"
	renewalCodeKey                           = "grpc_code"
	renewalRequestContextKey                 = "request_context_state"
	renewalParentContextKey                  = "parent_context_state"
	observationContextActive                 = "ACTIVE"
	observationContextCanceled               = "CANCELED"
	observationContextDeadline               = "DEADLINE_EXCEEDED"
	observationUnknown                       = "UNKNOWN"
)

var (
	renewalReferencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	renewalDigestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Это observation отказа RPC, не доказательство отзыва lease или owner commit.
// Caller вызывает helper до cancelRequest; исходные error/cause/payload не пишутся.
func (runtime *runtime) logRenewalFailure(request, parent context.Context, input runtimecontract.RunnerInput, phase renewalPhase, err error) {
	if err == nil || runtime.logger == nil {
		return
	}
	requestState, parentState := renewalContextState(request), renewalContextState(parent)
	lease, run, node := renewalReference(input.LeaseRef), renewalReference(input.RunRef), renewalReference(input.NodeRef)
	session, turn := renewalReference(input.SessionRef), renewalReference(input.TurnRef)
	inputDigest, revisionDigest := renewalDigest(input.InputDigest), renewalDigest(input.RuntimeRevisionDigest)
	executionDigest := renewalDigest(input.ExecutionBindingDigest)
	generation, attempt := input.LeaseGeneration, input.Attempt
	if generation < 1 {
		generation = 0
	}
	if attempt < 1 {
		attempt = 0
	}
	binding := "INCOMPLETE_PINS"
	if lease != "" && run != "" && node != "" && session != "" && turn != "" && generation > 0 && attempt > 0 && inputDigest != "" && revisionDigest != "" {
		binding = "CLAIM_PINS"
		if executionDigest != "" {
			binding = "EXECUTION_PINS"
		}
	}
	runtime.logger.WarnContext(parent, renewalFailureMessage,
		renewalPhaseKey, closedRenewalPhase(phase), renewalCodeKey, closedRenewalCode(err),
		renewalRequestContextKey, requestState, renewalParentContextKey, parentState,
		"binding_status", binding, "lease_ref", lease, "lease_generation", generation,
		"run_ref", run, "node_ref", node, "session_ref", session, "turn_ref", turn, "attempt", attempt,
		"input_digest", inputDigest, "runtime_revision_digest", revisionDigest, "execution_binding_digest", executionDigest)
}

func closedRenewalPhase(phase renewalPhase) string {
	switch phase {
	case renewalPhaseTrack, renewalPhaseDrain, renewalPhaseMaterialization:
		return string(phase)
	default:
		return observationUnknown
	}
}

func closedRenewalCode(err error) string {
	code := status.Code(err)
	switch code {
	case codes.OK, codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded,
		codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted,
		codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented,
		codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated:
		return code.String()
	default:
		return codes.Unknown.String()
	}
}

func renewalContextState(ctx context.Context) string {
	err := ctx.Err()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return observationContextDeadline
	case errors.Is(err, context.Canceled):
		return observationContextCanceled
	case err == nil:
		return observationContextActive
	default:
		return observationUnknown
	}
}

func renewalReference(value string) string {
	if !renewalReferencePattern.MatchString(value) {
		return ""
	}
	return value
}

func renewalDigest(value string) string {
	if !renewalDigestPattern.MatchString(value) {
		return ""
	}
	return value
}
