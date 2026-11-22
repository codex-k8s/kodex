package kubernetesoperation

import (
	"context"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	targetrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/kubernetestarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

const (
	// maximumIdleAuthorizationInterval bounds idle authority checks independently from client polling.
	// maximumIdleAuthorizationInterval ограничивает idle authority checks независимо от client polling.
	maximumIdleAuthorizationInterval = 5 * time.Second
	// streamExchangeWait bounds an idle unary exchange without cancelling its long-lived native stream.
	// streamExchangeWait ограничивает idle unary exchange без отмены long-lived native stream.
	streamExchangeWait = 250 * time.Millisecond
)

// streamReadEvent transfers one owned native chunk and its simultaneous error to the exchange lane.
// streamReadEvent передаёт owned native chunk и одновременную ошибку в exchange lane.
type streamReadEvent struct {
	read value.KubernetesStreamRead
	err  error
}

// ActiveStream owns two joined workers and bounded local delivery state for one owner-opened transport.
// Its private buffer is not a durable recovery source. A missing session must become LOST, not reopen.
// ActiveStream владеет двумя joined workers и bounded local delivery state одного owner-opened transport.
// Private buffer не является durable recovery source. Missing session становится LOST без reopen.
type ActiveStream struct {
	owner                            targetrepo.LiveStreamOwner
	access                           targetrepo.StreamAccess
	transport                        targetrepo.LiveStream
	binding                          managedkubernetes.Binding
	grant                            value.KubernetesStreamGrant
	handshake                        managedkubernetes.HandshakeReceipt
	handshakeDigest                  string
	ctx                              context.Context
	cancel                           context.CancelFunc
	done, readerDone                 chan struct{}
	events                           chan streamReadEvent
	exchanges                        chan struct{}
	input                            *InputRelay
	output                           *OutputWindow
	mu                               sync.Mutex
	credit                           time.Time
	intentState, intentReason        string
	observed                         value.KubernetesStreamRead
	result                           value.KubernetesStreamExchange
	finalError                       error
	terminalEvidence                 *managedkubernetes.TerminalReceipt
	acknowledgedInput, pendingOutput uint64
	pendingTerminal                  bool
}

// StartActiveStream transfers transport ownership only after validating an already-opened owner grant.
// The lifecycle context must belong to the broker worker group after its startup barrier, not one RPC.
// No goroutine, target read or local transport close occurs on constructor refusal; the caller still owns it.
// StartActiveStream передаёт transport ownership только после проверки already-opened owner grant.
// Lifecycle context принадлежит broker worker group после startup barrier, а не отдельному RPC.
// При отказе нет goroutine, target read либо local transport close: transport ещё принадлежит caller.
func StartActiveStream(lifecycle context.Context, owner targetrepo.LiveStreamOwner, access targetrepo.StreamAccess, transport targetrepo.LiveStream, binding managedkubernetes.Binding, grant value.KubernetesStreamGrant, handshake managedkubernetes.HandshakeReceipt) (*ActiveStream, error) {
	if lifecycle == nil || lifecycle.Err() != nil || owner == nil || access == nil || transport == nil || binding.Validate() != nil || binding.InvocationRef == "" || !grant.Valid(time.Now()) || grant.Generation != binding.Generation {
		return nil, targetrepo.ErrFrameUnavailable
	}
	digest, digestErr := handshake.Digest()
	bindingDigest, bindingErr := managedkubernetes.BindingDigest(binding)
	if digestErr != nil || bindingErr != nil || handshake.Origin.BindingDigest != bindingDigest || handshake.Origin.StreamRef != grant.Ref || handshake.Origin.StreamGeneration != binding.Generation {
		return nil, targetrepo.ErrFrameUnavailable
	}
	check, finish := context.WithTimeout(lifecycle, outputOwnerTimeout)
	credit, err := access.AuthorizeStream(check, binding, handshake.Origin)
	finish()
	if err != nil || !credit.After(time.Now()) || lifecycle.Err() != nil {
		return nil, targetrepo.ErrFrameUnavailable
	}
	if grant.ExpiresAt.Before(credit) {
		credit = grant.ExpiresAt
	}
	grant.InputChannels = append([]string(nil), grant.InputChannels...)
	grant.OutputChannels = append([]string(nil), grant.OutputChannels...)
	output, err := NewOutputWindow(owner, binding, grant)
	if err != nil {
		return nil, err
	}
	input, err := NewInputRelay(owner)
	if err != nil {
		output.Close()
		return nil, err
	}
	ctx, cancel := context.WithDeadline(lifecycle, grant.ExpiresAt)
	stream := &ActiveStream{owner: owner, access: access, transport: transport, binding: binding, grant: grant, handshake: handshake, handshakeDigest: digest, ctx: ctx, cancel: cancel, done: make(chan struct{}), readerDone: make(chan struct{}), events: make(chan streamReadEvent), exchanges: make(chan struct{}, 1), input: input, output: output, credit: credit, result: value.KubernetesStreamExchange{State: "ACTIVE", RemoteOutcome: "RUNNING"}}
	stream.exchanges <- struct{}{}
	go stream.read()
	go stream.supervise()
	return stream, nil
}

// read holds at most one native chunk while the single output window awaits acknowledgement.
// Explicit terminal evidence is remembered separately, even if its final output cannot be delivered.
// read хранит не более одного native chunk, пока single output window ждёт acknowledgement.
// Explicit terminal evidence хранится отдельно, даже если final output не удаётся доставить.
func (stream *ActiveStream) read() {
	defer close(stream.readerDone)
	for {
		read, err := stream.transport.ReadStream()
		terminal := err != nil || read.ExitCode != nil || read.CleanEOF || read.RemoteStatusCode != 0
		if len(read.Payload) > int(stream.grant.MaximumFrameBytes) || len(read.Payload) == 0 && !terminal {
			clear(read.Payload)
			stream.stop("LOST", "LIMIT_EXCEEDED")
			return
		}
		if terminal {
			observed := read
			observed.Payload = nil
			if read.ExitCode != nil {
				code := *read.ExitCode
				observed.ExitCode = &code
			}
			stream.mu.Lock()
			stream.observed = observed
			stream.mu.Unlock()
		}
		select {
		case stream.events <- streamReadEvent{read: read, err: err}:
		case <-stream.ctx.Done():
			clear(read.Payload)
			return
		}
		if terminal {
			return
		}
	}
}

// authorize refreshes only before the previous credit expires, with bounded owner and identity checks.
// It cannot bridge an expired lease gap or extend the immutable hard stream deadline.
// authorize обновляет credit лишь до истечения прежнего срока через bounded owner и identity checks.
// Не перекрывает expired lease gap и не продлевает immutable hard stream deadline.
func (stream *ActiveStream) authorize(ctx context.Context) (time.Time, error) {
	stream.mu.Lock()
	credit := stream.credit
	stream.mu.Unlock()
	if !credit.After(time.Now()) {
		stream.stop("EXPIRED", "EXPIRED")
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	check, cancel := context.WithDeadline(ctx, credit)
	defer cancel()
	bounded, finish := context.WithTimeout(check, outputOwnerTimeout)
	defer finish()
	until, err := stream.access.AuthorizeStream(bounded, stream.binding, stream.handshake.Origin)
	if ctx.Err() != nil {
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	if err != nil || bounded.Err() != nil || !until.After(time.Now()) {
		stream.stop("LOST", "AUTHORITY_UNAVAILABLE")
		return time.Time{}, targetrepo.ErrFrameUnavailable
	}
	if stream.grant.ExpiresAt.Before(until) {
		until = stream.grant.ExpiresAt
	}
	return until, nil
}

// supervise checks idle authority, then closes and joins the reader before destroying output or reporting.
// supervise проверяет idle authority, затем закрывает и join-ит reader до очистки output либо отчёта.
func (stream *ActiveStream) supervise() {
	for stream.ctx.Err() == nil {
		stream.mu.Lock()
		credit := stream.credit
		stream.mu.Unlock()
		delay := time.Until(credit) / 2
		if delay > maximumIdleAuthorizationInterval {
			delay = maximumIdleAuthorizationInterval
		}
		if delay <= 0 {
			stream.stop("EXPIRED", "EXPIRED")
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-stream.ctx.Done():
			timer.Stop()
		case <-timer.C:
			until, err := stream.authorize(stream.ctx)
			if err == nil {
				stream.mu.Lock()
				stream.credit = until
				stream.mu.Unlock()
			}
		}
	}
	joined := stream.transport.Close() == nil
	<-stream.readerDone
	// Cancellation prevents new work; taking the lane joins every accepted writer and receipt call.
	// Cancellation запрещает новую работу; захват lane ожидает всех принятых writers и receipt calls.
	<-stream.exchanges
	defer func() { stream.exchanges <- struct{}{} }()
	// Publish the completed snapshot before returning the lane, so a queued poll cannot observe a closed context without its terminal receipt.
	// Публикуем завершённый snapshot до возврата lane, чтобы ожидающий poll не увидел закрытый context без terminal receipt.
	defer close(stream.done)
	stream.output.Close()
	stream.complete(joined)
}

// stop selects the first local terminal cause and interrupts all in-flight stream work.
// stop выбирает первую local terminal cause и прерывает всю in-flight stream work.
func (stream *ActiveStream) stop(state, reason string) {
	stream.mu.Lock()
	if stream.intentState == "" {
		stream.intentState, stream.intentReason = state, reason
	}
	stream.mu.Unlock()
	stream.cancel()
}

// complete reports protocol-specific terminal facts only after positive local reader/writer join.
// Current owner cancellation or revocation supersedes the locally requested state.
// complete сообщает protocol-specific terminal facts лишь после positive local reader/writer join.
// Current owner cancellation либо revocation приоритетнее local requested state.
func (stream *ActiveStream) complete(joined bool) {
	stream.mu.Lock()
	state, reason, observed := stream.intentState, stream.intentReason, stream.observed
	stream.mu.Unlock()
	if state == "" {
		state, reason = "LOST", "TRANSPORT_LOST"
		if !time.Now().Before(stream.grant.ExpiresAt) {
			state, reason = "EXPIRED", "EXPIRED"
		}
	}
	receipt := managedkubernetes.TerminalReceipt{Origin: stream.handshake.Origin, HandshakeDigest: stream.handshakeDigest, Protocol: stream.handshake.Protocol, TransportJoined: joined, LocalState: state, RemoteOutcome: "UNKNOWN", ReasonCode: reason}
	if joined {
		receipt.ExitCode, receipt.CleanEOF, receipt.RemoteStatusCode = observed.ExitCode, observed.CleanEOF, observed.RemoteStatusCode
		if observed.ExitCode != nil {
			receipt.RemoteOutcome = "FAILED"
			if *observed.ExitCode == 0 {
				receipt.RemoteOutcome = "SUCCEEDED"
			}
		} else if observed.CleanEOF {
			receipt.RemoteOutcome = "SUCCEEDED"
		} else if observed.RemoteStatusCode != 0 {
			receipt.RemoteOutcome = "FAILED"
		}
	}
	digest, err := receipt.Digest()
	if err != nil {
		receipt.LocalState, receipt.ReasonCode, receipt.RemoteOutcome = "LOST", "TRANSPORT_LOST", "UNKNOWN"
		receipt.ExitCode, receipt.CleanEOF, receipt.RemoteStatusCode = nil, false, 0
		digest, err = receipt.Digest()
	}
	var final targetrepo.StreamReceipt
	if err == nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(stream.ctx), outputOwnerTimeout)
		final, err = stream.owner.CloseKubernetesStream(cleanup, stream.binding, targetrepo.StreamClosing{StreamRef: stream.grant.Ref, ExpectedState: "ACTIVE", TerminalState: receipt.LocalState, RemoteOutcome: receipt.RemoteOutcome, TerminalReceiptDigest: digest, TransportJoined: joined, ObservedExitCode: receipt.ExitCode, TerminalReceipt: receipt})
		cancel()
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if joined && receipt.Validate() == nil {
		owned := cloneTerminalReceipt(receipt)
		stream.terminalEvidence = &owned
	}
	// The supervisor holds the exchange lane after joining writers; this is the last confirmed input ACK, not a new completion credit.
	// Supervisor владеет exchange lane после join writers; это последний подтверждённый input ACK, а не новый completion credit.
	stream.result = value.KubernetesStreamExchange{State: "LOST", RemoteOutcome: receipt.RemoteOutcome, ReasonCode: receipt.ReasonCode, AcknowledgedInput: stream.acknowledgedInput}
	if err != nil || !validFinalStreamReceipt(final) {
		stream.finalError = targetrepo.ErrFrameUnavailable
		return
	}
	stream.result.State, stream.result.RemoteOutcome = final.State, final.RemoteOutcome
}

// TerminalEvidence returns a copy of the actual native closing receipt only after local workers joined.
// It is evidence for owner reconciliation, not output credit, a successful owner ACK or business completion.
// TerminalEvidence возвращает копию фактической native-квитанции только после завершения локальных workers.
// Это данные для сверки владельцем, не разрешение выдачи, не подтверждение owner ACK и не завершение задачи.
func (stream *ActiveStream) TerminalEvidence() (managedkubernetes.TerminalReceipt, bool) {
	if stream == nil {
		return managedkubernetes.TerminalReceipt{}, false
	}
	select {
	case <-stream.done:
	default:
		return managedkubernetes.TerminalReceipt{}, false
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.terminalEvidence == nil {
		return managedkubernetes.TerminalReceipt{}, false
	}
	return cloneTerminalReceipt(*stream.terminalEvidence), true
}

// cloneTerminalReceipt detaches the optional exit-code pointer while preserving exact immutable native facts.
// cloneTerminalReceipt отделяет указатель exit-code, сохраняя точные неизменяемые native-данные.
func cloneTerminalReceipt(receipt managedkubernetes.TerminalReceipt) managedkubernetes.TerminalReceipt {
	if receipt.ExitCode != nil {
		code := *receipt.ExitCode
		receipt.ExitCode = &code
	}
	return receipt
}

// validFinalStreamReceipt rejects absent/nonterminal owner responses and impossible completed uncertainty.
// validFinalStreamReceipt отклоняет absent/nonterminal owner responses и completed uncertainty.
func validFinalStreamReceipt(receipt targetrepo.StreamReceipt) bool {
	switch receipt.State {
	case "COMPLETED", "CANCELED", "REVOKED", "EXPIRED", "LOST":
	default:
		return false
	}
	return (receipt.RemoteOutcome == "SUCCEEDED" || receipt.RemoteOutcome == "FAILED" || receipt.RemoteOutcome == "UNKNOWN") && !(receipt.State == "COMPLETED" && receipt.RemoteOutcome == "UNKNOWN")
}

// snapshot returns only current finite status, never the retained native output buffer.
// snapshot возвращает только current finite status без retained native output buffer.
func (stream *ActiveStream) snapshot() (value.KubernetesStreamExchange, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.result, stream.finalError
}

// Close cancels and positively joins both owned workers; it never reopens or retries a remote command.
// Close отменяет и положительно join-ит обоих owned workers без reopen либо retry remote command.
func (stream *ActiveStream) Close() (value.KubernetesStreamExchange, error) {
	if stream == nil {
		return value.KubernetesStreamExchange{}, targetrepo.ErrFrameUnavailable
	}
	stream.stop("LOST", "TRANSPORT_LOST")
	<-stream.done
	return stream.snapshot()
}

// Exchange serializes exact-bound input and output acknowledgement while the watchdog runs independently.
// A lost unary output reply can reuse only the retained buffer; a lost input ACK never resends stdin.
// Exchange сериализует exact-bound input и output acknowledgement независимо от watchdog.
// Потерянный unary output reply повторяет только retained buffer; lost input ACK не повторяет stdin.
func (stream *ActiveStream) Exchange(ctx context.Context, binding managedkubernetes.Binding, acknowledgedOutput uint64, input *value.KubernetesStreamFrame) (value.KubernetesStreamExchange, error) {
	var empty value.KubernetesStreamExchange
	if stream == nil || ctx == nil || ctx.Err() != nil || binding != stream.binding {
		return empty, targetrepo.ErrFrameUnavailable
	}
	select {
	case <-ctx.Done():
		return empty, targetrepo.ErrFrameUnavailable
	case <-stream.done:
		return stream.snapshot()
	case <-stream.exchanges:
	}
	// A completed stream also returns the lane; selecting that ready case must not bypass the immutable terminal snapshot.
	// Завершённый stream также возвращает lane; выбор этой готовой ветви не должен обходить immutable terminal snapshot.
	select {
	case <-stream.done:
		stream.exchanges <- struct{}{}
		return stream.snapshot()
	default:
	}
	result, terminal, err := stream.exchange(ctx, binding, acknowledgedOutput, input)
	stream.exchanges <- struct{}{}
	if !terminal {
		return result, err
	}
	select {
	case <-stream.done:
		return stream.snapshot()
	case <-ctx.Done():
		return empty, targetrepo.ErrFrameUnavailable
	}
}

// exchange owns the serialized lane only while doing I/O; terminal waiting belongs to its caller.
// This lets shutdown positively join writers before recording the terminal owner receipt.
// exchange владеет serialized lane только во время I/O; terminal completion ожидает caller.
// Благодаря этому shutdown ожидает writers до записи terminal owner receipt.
func (stream *ActiveStream) exchange(ctx context.Context, binding managedkubernetes.Binding, acknowledgedOutput uint64, input *value.KubernetesStreamFrame) (value.KubernetesStreamExchange, bool, error) {
	var empty value.KubernetesStreamExchange
	select {
	case <-stream.ctx.Done():
		return empty, false, targetrepo.ErrFrameUnavailable
	default:
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(stream.ctx, cancel)
	defer stop()
	credit, err := stream.authorize(work)
	if err != nil {
		return empty, false, err
	}
	authorized, finish := context.WithDeadline(work, credit)
	defer finish()
	if err := stream.output.Acknowledge(authorized, acknowledgedOutput); err != nil {
		return empty, false, err
	}
	if stream.pendingOutput != 0 && acknowledgedOutput == stream.pendingOutput {
		stream.pendingOutput = 0
	}
	if input != nil {
		ack, _, err := stream.input.Send(authorized, binding, stream.grant, *input, stream.transport)
		if err != nil {
			stream.stop("LOST", "TRANSPORT_LOST")
			return empty, false, err
		}
		stream.acknowledgedInput = ack
	}
	if stream.pendingOutput == 0 && stream.pendingTerminal {
		stream.finishExchange()
		return value.KubernetesStreamExchange{AcknowledgedInput: stream.acknowledgedInput}, true, nil
	}
	if stream.pendingOutput == 0 {
		timer := time.NewTimer(streamExchangeWait)
		defer timer.Stop()
		select {
		case <-authorized.Done():
			return empty, false, targetrepo.ErrFrameUnavailable
		case <-timer.C:
			return value.KubernetesStreamExchange{State: "ACTIVE", RemoteOutcome: "RUNNING", AcknowledgedInput: stream.acknowledgedInput}, false, nil
		case event := <-stream.events:
			stream.pendingTerminal = event.err != nil || event.read.ExitCode != nil || event.read.CleanEOF || event.read.RemoteStatusCode != 0
			if len(event.read.Payload) == 0 {
				stream.finishExchange()
				return value.KubernetesStreamExchange{AcknowledgedInput: stream.acknowledgedInput}, true, nil
			}
			err := stream.output.Stage(authorized, event.read.Channel, event.read.Payload)
			clear(event.read.Payload)
			if err != nil {
				stream.stop("LOST", "OUTPUT_LOST")
				return empty, false, err
			}
		}
	}
	frame, err := stream.output.Deliver(authorized)
	if err != nil {
		stream.stop("LOST", "OUTPUT_LOST")
		return empty, false, err
	}
	if frame.Sequence == 0 {
		// A closed PF candidate spent only durable loss accounting; no fake output frame or ACK is emitted.
		// Закрытый PF candidate расходовал лишь durable loss accounting; fake output frame либо ACK не выдаётся.
		return value.KubernetesStreamExchange{State: "ACTIVE", RemoteOutcome: "RUNNING", AcknowledgedInput: stream.acknowledgedInput}, false, nil
	}
	stream.pendingOutput = frame.Sequence
	return value.KubernetesStreamExchange{State: "ACTIVE", RemoteOutcome: "RUNNING", AcknowledgedInput: stream.acknowledgedInput, Output: []value.KubernetesStreamFrame{frame}}, false, nil
}

// finishExchange requests closure only after final output acknowledgement and never waits under the I/O lane.
// finishExchange запрашивает closure после final output acknowledgement и не ожидает под I/O lane.
func (stream *ActiveStream) finishExchange() {
	stream.mu.Lock()
	observed := stream.observed
	stream.mu.Unlock()
	state, reason := "LOST", "TRANSPORT_LOST"
	if observed.ExitCode != nil {
		state, reason = "COMPLETED", "REMOTE_EXIT"
	} else if observed.CleanEOF {
		state, reason = "COMPLETED", "CLEAN_EOF"
	} else if observed.RemoteStatusCode != 0 {
		state, reason = "COMPLETED", "REMOTE_FAILURE"
	}
	stream.stop(state, reason)
}
