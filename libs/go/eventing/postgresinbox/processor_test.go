package postgresinbox

import (
	"context"
	"crypto/sha256"
	"errors"
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
	first := repairRequestDigest(request, "operator")
	second := repairRequestDigest(request, "operator")
	if first != second {
		t.Fatal("repair digest is not deterministic")
	}
	request.ExpectedFence++
	if first == repairRequestDigest(request, "operator") {
		t.Fatal("repair digest did not bind expected fence")
	}
	request.ExpectedFence--
	if first == repairRequestDigest(request, "other-operator") {
		t.Fatal("repair digest did not bind authorized actor")
	}
	request.EvidenceDigest = sha256.Sum256([]byte("other evidence"))
	if first == repairRequestDigest(request, "operator") {
		t.Fatal("repair digest did not bind evidence")
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
	if !errors.Is(err, ErrRepairNotAllowed) {
		t.Fatalf("unexpected repair error: %v", err)
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
