package postgresinbox

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/eventing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestOrderingKeyAndDigestAreDeterministic(t *testing.T) {
	t.Parallel()
	envelope := validEnvelope()
	envelope.OrganizationID = `org,"with delimiter"<&>`
	envelope.AggregateID = `aggregate, "quoted"`
	first, err := newEventRecord(envelope)
	if err != nil {
		t.Fatalf("newEventRecord() error = %v", err)
	}
	second, err := newEventRecord(envelope)
	if err != nil {
		t.Fatalf("newEventRecord() second error = %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("digest is not deterministic")
	}
	if !sameOrderingKey(first.OrderingKey, second.OrderingKey) ||
		!validOrderingKey(first.OrderingKey) {
		t.Fatal("ordering key is not canonical JSON sequence")
	}

	changed := envelope
	changed.Data = []byte(`{"value":"changed"}`)
	changedRecord, err := newEventRecord(changed)
	if err != nil {
		t.Fatalf("newEventRecord() changed error = %v", err)
	}
	if first.Digest == changedRecord.Digest {
		t.Fatal("different payload reused the same digest")
	}
}

func TestEventSnapshotDoesNotShareMutablePayload(t *testing.T) {
	t.Parallel()
	envelope := validEnvelope()
	original := append([]byte(nil), envelope.Data...)
	record, err := newEventRecord(envelope)
	if err != nil {
		t.Fatalf("newEventRecord() error = %v", err)
	}
	copy(envelope.Data, []byte(`{"value":"evil"}`))
	if string(record.Envelope.Data) != string(original) {
		t.Fatal("record shares caller payload backing array")
	}
	snapshot := EventSnapshot{envelope: record.Envelope}
	first := snapshot.Data()
	first[0] = '['
	if string(snapshot.Data()) != string(original) {
		t.Fatal("snapshot returned shared payload backing array")
	}
	view := snapshot.Envelope()
	view.Data[0] = '['
	if string(snapshot.Data()) != string(original) {
		t.Fatal("envelope view mutated immutable snapshot")
	}
	decision, err := classifyExisting(record, cursorRow{}, inboxRow{
		EventID:       record.Envelope.EventID,
		EventDigest:   record.Digest[:],
		OrderingKey:   record.OrderingKey,
		EventSequence: record.Envelope.EventSequence,
		State:         stateCompleted,
	})
	if err != nil || decision.result.Outcome != OutcomeDuplicate {
		t.Fatalf("durable decision changed after caller mutation: %#v, %v", decision, err)
	}
}

func TestClassifyExistingUsesDurableOrderingEvidence(t *testing.T) {
	t.Parallel()
	record, err := newEventRecord(validEnvelope())
	if err != nil {
		t.Fatalf("newEventRecord() error = %v", err)
	}
	row := inboxRow{
		EventID:       record.Envelope.EventID,
		EventDigest:   record.Digest[:],
		OrderingKey:   record.OrderingKey,
		EventSequence: record.Envelope.EventSequence,
		State:         stateCompleted,
	}
	decision, err := classifyExisting(record, cursorRow{}, row)
	if err != nil {
		t.Fatalf("classifyExisting() error = %v", err)
	}
	if decision.result.Outcome != OutcomeDuplicate ||
		decision.result.Action != BrokerActionACK || !decision.result.Durable {
		t.Fatalf("duplicate decision = %#v", decision)
	}

	row.EventDigest = make([]byte, sha256.Size)
	decision, err = classifyExisting(record, cursorRow{}, row)
	if !errors.Is(err, ErrEventConflict) ||
		decision.result.Action != BrokerActionNACKTerminal {
		t.Fatalf("conflict decision = %#v, error = %v", decision, err)
	}

	row.EventDigest = record.Digest[:]
	row.State = stateReceived
	row.EventSequence = 3
	record.Envelope.EventSequence = 3
	decision, err = classifyExisting(record, cursorRow{LastSequence: 1}, row)
	if !errors.Is(err, ErrSequenceGap) ||
		decision.result.Action != BrokerActionNACKRetry {
		t.Fatalf("gap decision = %#v, error = %v", decision, err)
	}
}

func TestClaimFenceRejectsStaleGeneration(t *testing.T) {
	t.Parallel()
	digest := sha256.Sum256([]byte("event"))
	claim := Claim{
		Consumer:        Consumer{Name: "consumer", Scope: "v1"},
		EventID:         uuid.NewString(),
		EventDigest:     digest,
		OrderingKey:     `["event","aggregate","id"]`,
		EventSequence:   1,
		LeaseOwner:      "worker-1",
		LeaseToken:      uuid.NewString(),
		LeaseGeneration: 2,
		LeaseFence:      7,
	}
	owner := claim.LeaseOwner
	token := claim.LeaseToken
	row := inboxRow{
		EventID:         claim.EventID,
		EventDigest:     digest[:],
		OrderingKey:     claim.OrderingKey,
		EventSequence:   claim.EventSequence,
		State:           stateProcessing,
		LeaseOwner:      &owner,
		LeaseToken:      &token,
		LeaseGeneration: claim.LeaseGeneration,
		LeaseFence:      claim.LeaseFence,
	}
	if !claimMatchesRow(claim, row) {
		t.Fatal("exact claim did not match")
	}
	row.LeaseGeneration++
	if claimMatchesRow(claim, row) {
		t.Fatal("stale generation matched")
	}
	row.LeaseGeneration = claim.LeaseGeneration
	row.LeaseFence++
	if claimMatchesRow(claim, row) {
		t.Fatal("stale fence matched")
	}
}

func TestBackoffIsBounded(t *testing.T) {
	t.Parallel()
	processor := Processor{config: Config{
		InitialBackoff: 100 * time.Millisecond,
		MaximumBackoff: 2 * time.Second,
	}}
	tests := []struct {
		attempt uint32
		want    time.Duration
	}{
		{1, 100 * time.Millisecond},
		{2, 200 * time.Millisecond},
		{5, 1600 * time.Millisecond},
		{6, 2 * time.Second},
		{100, 2 * time.Second},
	}
	for _, test := range tests {
		if got := processor.backoff(test.attempt); got != test.want {
			t.Fatalf("backoff(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func TestDurationValidationIsOverflowSafe(t *testing.T) {
	t.Parallel()
	config := validConfig()
	config.EffectTimeout = time.Duration(math.MaxInt64)
	config.FinalizeTimeout = time.Duration(math.MaxInt64)
	if !errors.Is(config.validate(), ErrInvalidConfiguration) {
		t.Fatal("overflowing timeout pair was accepted")
	}
	config = validConfig()
	config.EffectTimeout = config.LeaseDuration - time.Nanosecond
	config.FinalizeTimeout = time.Nanosecond
	if !errors.Is(config.validate(), ErrInvalidConfiguration) {
		t.Fatal("timeout pair equal to lease was accepted")
	}
	config = validConfig()
	config.EffectTimeout = 20 * time.Second
	config.FinalizeTimeout = 5 * time.Second
	if err := config.validate(); err != nil {
		t.Fatalf("valid timeout pair rejected: %v", err)
	}
}

func TestEffectOperationIsSchemaBoundAndOpaque(t *testing.T) {
	t.Parallel()
	operation, err := NewEffectOperation("apply_projection", "consumer_service", "apply_projection")
	if err != nil {
		t.Fatalf("NewEffectOperation() error = %v", err)
	}
	if operation.Name() != "apply_projection" ||
		strings.Count(operation.query, ";") != 1 ||
		!strings.Contains(operation.query, `"consumer_service"."apply_projection"`) {
		t.Fatal("effect operation is not a single opaque call")
	}
	if _, _, err := (pgx.StrictNamedArgs{"effect_input": nil}).RewriteQuery(
		context.Background(), nil, operation.query, nil,
	); err != nil {
		t.Fatalf("effect operation StrictNamedArgs error = %v", err)
	}
	if _, err := NewEffectOperation("set_role", "pg_catalog", "set_config"); !errors.Is(err, ErrInvalidEffectOperation) {
		t.Fatalf("unsafe effect operation error = %v", err)
	}
	processor, err := New(
		failingBeginner{},
		validConfig(),
		WithEffectOperations(operation),
	)
	if err != nil {
		t.Fatalf("New() rejected exact schema operation: %v", err)
	}
	if len(processor.effectOperations) != 1 {
		t.Fatal("effect operation was not registered")
	}
	otherSchema, err := NewEffectOperation("other", "other_service", "apply_projection")
	if err != nil {
		t.Fatalf("NewEffectOperation() other schema error = %v", err)
	}
	if _, err := New(
		failingBeginner{}, validConfig(), WithEffectOperations(otherSchema),
	); !errors.Is(err, ErrInvalidEffectOperation) {
		t.Fatalf("cross-schema operation error = %v", err)
	}
}

func TestReadinessRejectsBehaviorChangingExtras(t *testing.T) {
	t.Parallel()
	manifest := requiredSchemaObjects()
	manifest["extension_index/postgresinbox_ext_lookup"] = "1"
	if err := validateSchemaObjects(manifest); err != nil {
		t.Fatalf("safe performance index rejected: %v", err)
	}
	manifest = requiredSchemaObjects()
	manifest["column/runtime_inbox_events.hidden_default"] = "text|1|-|-|x"
	if !errors.Is(validateSchemaObjects(manifest), ErrSchemaMismatch) {
		t.Fatal("behavior-changing column was accepted")
	}
	manifest = requiredSchemaObjects()
	manifest["extension_index/postgresinbox_ext_unique"] = "0"
	if !errors.Is(validateSchemaObjects(manifest), ErrSchemaMismatch) {
		t.Fatal("unsafe extension index was accepted")
	}
}

func TestLifecycleStopsAdmissionAndJoinsWithoutWorker(t *testing.T) {
	t.Parallel()
	processor, err := New(failingBeginner{}, validConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := processor.enter(); err != nil {
		t.Fatalf("enter() error = %v", err)
	}
	processor.Cancel()
	if err := processor.enter(); !errors.Is(err, ErrProcessorStopped) {
		t.Fatalf("enter() after Cancel error = %v", err)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := processor.Join(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Join() before leave error = %v", err)
	}
	processor.leave()
	if err := processor.Join(context.Background()); err != nil {
		t.Fatalf("Join() error = %v", err)
	}
}

func TestEffectFailureDoesNotExposeCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("sensitive provider detail")
	err := NewEffectFailure("provider_rejected", false, cause)
	if strings.Contains(err.Error(), cause.Error()) {
		t.Fatal("effect failure exposed cause text")
	}
	if !errors.Is(err, cause) {
		t.Fatal("effect failure did not preserve cause for classification")
	}
}

func TestRepairDigestBindsFenceAndEvidence(t *testing.T) {
	t.Parallel()
	eventDigest := sha256.Sum256([]byte("event"))
	evidenceDigest := sha256.Sum256([]byte("evidence"))
	request := RepairRequest{
		Consumer:           Consumer{Name: "consumer", Scope: "v1"},
		IdempotencyKey:     "repair-key-1",
		EventID:            uuid.NewString(),
		EventDigest:        eventDigest,
		ExpectedGeneration: 3,
		ExpectedFence:      9,
		Reason:             "cause removed",
		EvidenceDigest:     evidenceDigest,
	}
	authority := validOperatorAuthority("repair")
	first := repairRequestDigest(request, authority)
	second := repairRequestDigest(request, authority)
	if first != second {
		t.Fatal("repair digest is not deterministic")
	}
	request.ExpectedFence++
	if first == repairRequestDigest(request, authority) {
		t.Fatal("repair digest did not bind expected fence")
	}
	request.ExpectedFence--
	otherAuthority := authority
	otherAuthority.Actor = "other-operator"
	if first == repairRequestDigest(request, otherAuthority) {
		t.Fatal("repair digest did not bind authorized actor")
	}
	request.EvidenceDigest = sha256.Sum256([]byte("other evidence"))
	if first == repairRequestDigest(request, authority) {
		t.Fatal("repair digest did not bind evidence")
	}
}

func TestRepairDigestUsesAuthorizedDurableScope(t *testing.T) {
	t.Parallel()
	request := RepairRequest{
		Consumer:           Consumer{Name: "consumer", Scope: "v1"},
		IdempotencyKey:     "caller-key-1",
		EventID:            uuid.NewString(),
		EventDigest:        sha256.Sum256([]byte("event")),
		ExpectedGeneration: 1,
		ExpectedFence:      2,
		Reason:             "cause removed",
		EvidenceDigest:     sha256.Sum256([]byte("evidence")),
	}
	authority := validOperatorAuthority("repair")
	first := repairRequestDigest(request, authority)
	request.IdempotencyKey = "caller-key-2"
	if first != repairRequestDigest(request, authority) {
		t.Fatal("untrusted caller key leaked into canonical request digest")
	}
	authority.Project = "project-b"
	if first == repairRequestDigest(request, authority) {
		t.Fatal("authorized project scope was not bound")
	}
}

func TestRecoveryDecisionKeepsPredecessorBlocking(t *testing.T) {
	t.Parallel()
	row := inboxRow{State: stateRetry, EventSequence: 3, Attempts: 1, MaxAttempts: 3, AvailableNow: true}
	directive, action := recoveryDecision(row, cursorRow{LastSequence: 1})
	if directive != RecoveryWaitPredecessor || action != "WAIT" {
		t.Fatalf("gap recovery = %s/%s", directive, action)
	}
	row.EventSequence = 2
	directive, action = recoveryDecision(row, cursorRow{LastSequence: 1})
	if directive != RecoveryReplayRequired || action != "REJOIN" {
		t.Fatalf("eligible recovery = %s/%s", directive, action)
	}
	row.Attempts = row.MaxAttempts
	directive, action = recoveryDecision(row, cursorRow{LastSequence: 1})
	if directive != RecoveryRepairRequired || action != "TERMINALIZE" {
		t.Fatalf("exhausted recovery = %s/%s", directive, action)
	}
	row.State = stateCompleted
	directive, action = recoveryDecision(row, cursorRow{LastSequence: 2})
	if directive != RecoveryACKEligible || action != "WAIT" {
		t.Fatalf("completed recovery = %s/%s", directive, action)
	}
}

func TestBlockagePaginationIsBounded(t *testing.T) {
	t.Parallel()
	request, err := (BlockageListRequest{}).validate()
	if err != nil || request.Limit != defaultBlockagePage {
		t.Fatalf("default page = %#v, error = %v", request, err)
	}
	if _, err := (BlockageListRequest{Limit: maximumBlockagePage + 1}).validate(); !errors.Is(err, ErrInvalidBlockageRead) {
		t.Fatalf("oversized page error = %v", err)
	}
	if _, err := (BlockageListRequest{After: &BlockageCursor{
		ReceivedAt: time.Now().UTC(), EventID: "not-a-uuid",
	}}).validate(); !errors.Is(err, ErrInvalidBlockageRead) {
		t.Fatalf("invalid cursor error = %v", err)
	}
}

func TestRecoveryCoordinatesAreFencedAsPair(t *testing.T) {
	t.Parallel()
	request := RecoveryRequest{
		Consumer:           Consumer{Name: "consumer", Scope: "v1"},
		IdempotencyKey:     "recovery-key-1",
		EventID:            uuid.NewString(),
		EventDigest:        sha256.Sum256([]byte("event")),
		ExpectedGeneration: 1,
		ExpectedFence:      0,
		Reason:             "broker attempts exhausted",
		EvidenceDigest:     sha256.Sum256([]byte("evidence")),
	}
	if !errors.Is(request.validate(), ErrInvalidRecovery) {
		t.Fatal("asymmetric generation/fence was accepted")
	}
	request.ExpectedGeneration = 0
	if err := request.validate(); err != nil {
		t.Fatalf("initial unclaimed coordinates rejected: %v", err)
	}
}

func TestRepairDeniedWithoutAuthorizer(t *testing.T) {
	t.Parallel()
	processor, err := New(failingBeginner{}, validConfig())
	if err != nil {
		t.Fatalf("new processor: %v", err)
	}
	eventDigest := sha256.Sum256([]byte("event"))
	evidenceDigest := sha256.Sum256([]byte("evidence"))
	_, err = processor.Repair(context.Background(), RepairRequest{
		Consumer:           Consumer{Name: "consumer", Scope: "v1"},
		IdempotencyKey:     "repair-key-1",
		EventID:            uuid.NewString(),
		EventDigest:        eventDigest,
		ExpectedGeneration: 3,
		ExpectedFence:      9,
		Reason:             "cause removed",
		EvidenceDigest:     evidenceDigest,
	})
	if !errors.Is(err, ErrOperatorNotAllowed) {
		t.Fatalf("unexpected repair error: %v", err)
	}
}

func validOperatorAuthority(operation string) OperatorAuthority {
	return OperatorAuthority{
		Actor:        "operator",
		Organization: "organization-a",
		Project:      "project-a",
		Operation:    operation,
		KeyHash:      sha256.Sum256([]byte("server-scoped-key")),
	}
}

func validConfig() Config {
	return Config{
		Schema:           "consumer_service",
		InstanceID:       "worker-1",
		LeaseDuration:    30 * time.Second,
		EffectTimeout:    20 * time.Second,
		FinalizeTimeout:  5 * time.Second,
		InitialBackoff:   time.Second,
		MaximumBackoff:   time.Minute,
		MaxAttempts:      8,
		MaxRepairs:       3,
		RetentionHorizon: 35 * 24 * time.Hour,
		CleanupBatchSize: 100,
	}
}

func validEnvelope() eventing.Envelope {
	return eventing.Envelope{
		EventID:          uuid.NewString(),
		EventName:        "example.changed",
		EventVersion:     1,
		SchemaVersion:    1,
		OccurredAt:       time.Now().UTC().Truncate(time.Microsecond),
		AggregateType:    "Example",
		AggregateID:      "aggregate-1",
		AggregateVersion: 1,
		EventSequence:    1,
		CorrelationID:    uuid.NewString(),
		Data:             []byte(`{"value":"safe"}`),
	}
}

type failingBeginner struct{}

func (failingBeginner) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("transaction unavailable")
}
