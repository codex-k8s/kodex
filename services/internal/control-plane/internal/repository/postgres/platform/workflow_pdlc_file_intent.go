package platform

import (
	"context"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/jackc/pgx/v5"
)

// workflowPDLCEffectLimit bounds the explicit new profile without increasing model concurrency,
// execution attempts or the historical profile's sixteen-record journal.
// workflowPDLCEffectLimit ограничивает явный новый профиль без увеличения параллелизма моделей,
// числа попыток исполнения либо прежнего журнала на шестнадцать записей.
const workflowPDLCEffectLimit = 64

// workflowEffectLimitForProfile changes the journal budget only for the explicit PDLC profile.
// workflowEffectLimitForProfile меняет бюджет журнала только явного PDLC профиля.
func workflowEffectLimitForProfile(profile string) int {
	if profile == workflowservice.GitHubPDLCV1 {
		return workflowPDLCEffectLimit
	}
	return workflowEffectLimit
}

// workflowPDLCFileEpochDigest distinguishes a new deliberate file revision from retry of its
// predecessor. The predecessor is owner-selected from the append-only native journal.
// workflowPDLCFileEpochDigest отличает новую намеренную ревизию файла от retry предшественника.
// Предшественника выбирает owner из append-only native журнала.
func workflowPDLCFileEpochDigest(desiredDigest, predecessorRef string) string {
	return workflowResultSHA([]byte("kodex.workflow-file-epoch.v1\x00" + desiredDigest + "\x00" + predecessorRef))
}

// selectWorkflowPDLCFileIntent prefers an exact same-node native call over the latest file
// revision, and rejects forks, duplicated native calls and a journal exceeding its budget.
// It selects locators only; the caller must prove all selected native data and current authority.
// selectWorkflowPDLCFileIntent предпочитает exact same-node native call последней ревизии файла
// и отвергает forks, повторные native calls и превышение бюджета журнала. Выбирает только locators;
// вызывающий доказывает native данные и текущие полномочия отдельно.
func selectWorkflowPDLCFileIntent(intents []workflowEffectIntent, current workflowEffectIntent, callKey string) (int, bool, error) {
	if len(intents) > workflowPDLCEffectLimit || len(callKey) < 8 || len(callKey) > 128 {
		return -1, false, errs.ErrConflict
	}
	latest, exact := -1, -1
	previous := map[string]string{}
	seenCalls, seenInvocations := map[string]bool{}, map[string]bool{}
	for index, old := range intents {
		if !workflowPDLCFileOperation(old.Operation) {
			continue
		}
		call := old.OriginNodeID + "\x00" + old.FileCallKey
		if old.Schema != "kodex.workflow-effect-intent.v1" || old.OriginNodeID == "" || old.InvocationRef == "" || len(old.FileCallKey) < 8 || len(old.FileCallKey) > 128 ||
			seenCalls[call] || seenInvocations[old.InvocationRef] || old.FilePredecessorRef != previous[old.ResultKey] {
			return -1, false, errs.ErrConflict
		}
		seenCalls[call], seenInvocations[old.InvocationRef] = true, true
		previous[old.ResultKey] = old.InvocationRef
		if old.OriginNodeID == current.OriginNodeID && old.FileCallKey == callKey {
			if old.ResultKey != current.ResultKey {
				return -1, false, errs.ErrConflict
			}
			exact = index
		}
		if old.ResultKey == current.ResultKey {
			latest = index
		}
	}
	if exact >= 0 {
		return exact, true, nil
	}
	return latest, false, nil
}

// admitWorkflowPDLCFileTx preserves successful file effects as immutable native facts. Identical
// desired bytes rejoin the known success; a different revision gets a server-linked predecessor
// only after exact native success. Uncertain effects cannot be replaced by a new write.
// admitWorkflowPDLCFileTx сохраняет успешные file effects как immutable native facts. Одинаковые
// желаемые bytes повторно читают known success; иная ревизия получает server-linked predecessor
// только после exact native success. Неопределённый effect не заменяется новой записью.
func (repository *Repository) admitWorkflowPDLCFileTx(ctx context.Context, tx pgx.Tx, current scope, admission *workflowEffectAdmission, intent workflowEffectIntent,
	intents []workflowEffectIntent, rejoins []workflowEffectRejoin, rejoinCount int, callKey string, canonicalInput []byte, sessionRef, turnRef string,
) (*workflowEffectAdmission, map[string]any, error) {
	if intent.ApprovalPolicy != "NONE" {
		return nil, nil, errs.ErrForbidden
	}
	index, exactCall, err := selectWorkflowPDLCFileIntent(intents, intent, callKey)
	if err != nil {
		return nil, nil, err
	}
	desired := intent.SemanticDigest
	intent.FileCallKey = callKey
	intent.SemanticDigest = workflowPDLCFileEpochDigest(desired, "")
	if index >= 0 {
		old := intents[index]
		if old.ConnectionRef != intent.ConnectionRef || old.ConnectionVersion != intent.ConnectionVersion || old.DefinitionDigest != intent.DefinitionDigest || old.ScopeDigest != intent.ScopeDigest || old.ApprovalPolicy != "NONE" ||
			old.Operation == intent.Operation && (old.GrantRef != intent.GrantRef || old.GrantVersion != intent.GrantVersion) {
			return nil, nil, errs.ErrConflict
		}
		proof, err := readWorkflowEffectNativeTx(ctx, tx, current, admission.RootID, admission.AnchorID, old)
		if err != nil {
			return nil, nil, err
		}
		oldDesired, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, old.ResultKey, old.Operation, old.ScopeDigest, proof.Input)
		var original map[string]string
		if err != nil || json.Unmarshal(proof.Input, &original) != nil || workflowResultSHA(asJSON(original)) != old.InputDigest || proof.OriginalKey != old.FileCallKey ||
			workflowPDLCFileEpochDigest(oldDesired, old.FilePredecessorRef) != old.SemanticDigest ||
			exactCall && (old.Operation != intent.Operation || workflowResultSHA(canonicalInput) != old.InputDigest) {
			return nil, nil, errs.ErrConflict
		}
		known := proof.State == "SUCCEEDED" && proof.ReceiptRef != "" && repositorybundle.ValidDigest(proof.ReceiptDigest)
		if proof.State == "SUCCEEDED" && !known {
			return nil, nil, errs.ErrConflict
		}
		if oldDesired == desired {
			sameExecution := sameWorkflowEffectExecution(old, intent)
			if !known && !(sameExecution && exactCall) {
				return nil, nil, errs.ErrConflict
			}
			if known && !sameExecution {
				rejoin := workflowEffectRejoin{Schema: "kodex.workflow-effect-rejoin.v1", ResultKey: old.ResultKey, Operation: old.Operation, SemanticDigest: old.SemanticDigest,
					InvocationRef: proof.InvocationRef, ReceiptRef: proof.ReceiptRef, ReceiptDigest: proof.ReceiptDigest, RuntimeRevisionID: intent.OriginRevisionID, LeaseID: intent.OriginLeaseID,
					Generation: intent.OriginGeneration, RuntimeAttempt: intent.OriginAttempt, BusinessAttempt: intent.OriginBusiness}
				found := false
				for _, existing := range rejoins {
					if existing.ResultKey == rejoin.ResultKey && existing.InvocationRef == rejoin.InvocationRef && sameWorkflowEffectRejoinExecution(existing, rejoin) {
						if existing != rejoin {
							return nil, nil, errs.ErrConflict
						}
						found = true
					}
				}
				if !found {
					if rejoinCount >= workflowPDLCEffectLimit {
						return nil, nil, errs.ErrConflict
					}
					if err := appendWorkflowEffectTx(ctx, tx, current, admission.RootID, admission.NodeID, "rejoin", rejoin); err != nil {
						return nil, nil, err
					}
				}
			}
			return nil, map[string]any{"invocationRef": proof.InvocationRef, "grantRef": old.GrantRef, "operation": old.Operation, "state": proof.State, "gateRef": proof.GateRef,
				"workflowRejoined": known, "publicationRef": "", "receiptRef": proof.ReceiptRef, "semanticDigest": old.SemanticDigest, "receiptDigest": proof.ReceiptDigest,
				"resultKey": old.ResultKey, "runtimeAttempt": intent.OriginAttempt, "businessAttempt": intent.OriginBusiness, "sessionRef": sessionRef, "turnRef": turnRef, "connectionVersion": intent.ConnectionVersion}, nil
		}
		if !known || exactCall {
			return nil, nil, errs.ErrConflict
		}
		intent.FilePredecessorRef = proof.InvocationRef
		intent.SemanticDigest = workflowPDLCFileEpochDigest(desired, proof.InvocationRef)
	}
	if len(intents) >= workflowPDLCEffectLimit {
		return nil, nil, errs.ErrConflict
	}
	admission.Intent = intent
	return admission, nil, nil
}
