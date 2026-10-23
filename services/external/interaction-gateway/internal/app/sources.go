package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sort"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// sourceSession tracks one source snapshot's fingerprint, cancellation and completion signal for serialized listener replacement.
// RU: sourceSession хранит fingerprint снимка источника, отмену и сигнал завершения для последовательной замены listener.
type sourceSession struct {
	fingerprint string
	cancel      context.CancelFunc
	done        <-chan struct{}
}

// sourceListener isolates external source subscriptions from owner discovery and message acceptance.
// RU: sourceListener отделяет внешние подписки источников от обнаружения и приёма сообщений у владельца.
type sourceListener interface {
	// Listen runs the selected source subscription until cancellation or failure and forwards messages to the supplied handler.
	// RU: Listen ведёт подписку выбранного источника до отмены или отказа и передаёт сообщения указанному обработчику.
	Listen(context.Context, *controlplanev1.InteractionSource, mattermost.MessageHandler) error
}

// sourceManager owns active and draining listeners under a mutex, joins predecessor generations and rejects reconciliation after closure.
// RU: sourceManager владеет активными и завершающимися listener под mutex, ожидает предыдущие поколения и запрещает согласование после закрытия.
// Owner snapshots choose desired connections; the manager does not infer user permissions from external messages.
// RU: Желаемые подключения определяют снимки владельца; manager не выводит полномочия пользователя из внешних сообщений.
type sourceManager struct {
	control  controlplanev1.InteractionWorkServiceClient
	adapter  sourceListener
	logger   *slog.Logger
	config   Config
	mu       sync.Mutex
	sources  map[string]sourceSession
	draining map[string]<-chan struct{}
	wait     sync.WaitGroup
	closed   bool
}

// newSourceManager initializes empty listener registries without starting subscriptions or background work.
// RU: newSourceManager создаёт пустые реестры listener без открытия подписок или запуска фоновой работы.
func newSourceManager(control controlplanev1.InteractionWorkServiceClient, adapter sourceListener, logger *slog.Logger, config Config) *sourceManager {
	return &sourceManager{control: control, adapter: adapter, logger: logger, config: config, sources: map[string]sourceSession{}, draining: map[string]<-chan struct{}{}}
}

// runSourceRefresh periodically reads owner source snapshots and cancels all previous subscriptions when discovery fails.
// RU: runSourceRefresh периодически читает снимки источников владельца и отменяет все прежние подписки при отказе обнаружения.
// Each RPC is bounded; degradation logs omit raw dependency errors, and cancellation ends the polling worker.
// RU: Каждый RPC ограничен; журнал деградации не содержит сырых ошибок зависимостей, а отмена завершает worker опроса.
func runSourceRefresh(manager *sourceManager, control *controlplaneclient.Client, logger *slog.Logger, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.SourceRefreshInterval)
		defer ticker.Stop()
		degraded := false
		for {
			cycle, cancel := context.WithTimeout(ctx, config.RequestTimeout)
			response, err := control.Interaction.ListInteractionSources(cycle, &controlplanev1.ListInteractionSourcesRequest{})
			cancel()
			if err == nil {
				manager.Reconcile(ctx, response.GetSources())
				if degraded {
					degraded = false
					logger.InfoContext(ctx, "interaction source discovery restored")
				}
			} else {
				// Неподтверждённый owner snapshot не продлевает внешнюю подписку.
				manager.Reconcile(ctx, nil)
				if !degraded {
					degraded = true
					logger.WarnContext(ctx, "interaction source discovery degraded", "error_class", "control_plane")
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

// Reconcile retains unchanged valid listening sources, cancels removed or changed ones and starts replacements only after predecessors finish.
// RU: Reconcile сохраняет неизменные допустимые источники подписки, отменяет удалённые или изменённые и запускает замену лишь после завершения предшественников.
// Every started generation belongs to the parent's cancellation and the manager's join group, including superseded waiting generations.
// RU: Каждое запущенное поколение связано с отменой родителя и группой ожидания manager, включая заменённые ожидающие поколения.
func (manager *sourceManager) Reconcile(parent context.Context, desired []*controlplanev1.InteractionSource) {
	wanted := map[string]*controlplanev1.InteractionSource{}
	for _, source := range desired {
		if source == nil || source.GetConnectionRef() == "" || !sourceListens(source.GetEnabledCapabilities()) || sourceFingerprint(source) == "" {
			continue
		}
		wanted[source.GetConnectionRef()] = source
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || parent.Err() != nil {
		return
	}
	for reference, done := range manager.draining {
		select {
		case <-done:
			delete(manager.draining, reference)
		default:
		}
	}
	for reference, session := range manager.sources {
		source, ok := wanted[reference]
		if ok && session.fingerprint == sourceFingerprint(source) {
			delete(wanted, reference)
			continue
		}
		session.cancel()
		manager.draining[reference] = session.done
		delete(manager.sources, reference)
	}
	for reference, source := range wanted {
		child, cancel := context.WithCancel(parent)
		done := make(chan struct{})
		manager.sources[reference] = sourceSession{fingerprint: sourceFingerprint(source), cancel: cancel, done: done}
		manager.wait.Add(1)
		go func(previous <-chan struct{}) {
			defer manager.wait.Done()
			defer close(done)
			if previous != nil {
				// Отменённое промежуточное поколение тоже дожидается предшественника.
				<-previous
			}
			if child.Err() == nil {
				manager.run(child, source)
			}
		}(manager.draining[reference])
	}
}

// run reconnects one source and forwards message identity, gate pins and a stable event key to bounded owner acceptance RPCs.
// RU: run переподключает один источник и передаёт идентичность сообщения, координаты gate и устойчивый ключ события в ограниченные RPC приёма владельца.
// Defined per-message rejections leave the channel connected; transport failures propagate to the listener, and cancellation stops reconnects.
// RU: Заданные отказы отдельному сообщению не разрывают канал; транспортные ошибки возвращаются listener, а отмена прекращает переподключения.
func (manager *sourceManager) run(ctx context.Context, source *controlplanev1.InteractionSource) {
	degraded := false
	for {
		err := manager.adapter.Listen(ctx, source, func(messageContext context.Context, message mattermost.Message) error {
			acceptContext, cancel := context.WithTimeout(messageContext, manager.config.RequestTimeout)
			defer cancel()
			response, err := manager.control.AcceptInteractionMessage(acceptContext, &controlplanev1.AcceptInteractionMessageRequest{
				Mutation:      &controlplanev1.MutationContext{IdempotencyKey: stableKey(source.GetConnectionRef(), message.EventRef)},
				ConnectionRef: source.GetConnectionRef(), ExternalEventRef: message.EventRef,
				ExternalPostRef: message.PostRef, ExternalRootPostRef: message.RootPostRef,
				ExternalChannelRef: message.ChannelRef, ExternalUserDigest: message.UserDigest,
				Message: message.Text, Decision: message.Decision,
				ExternalTeamRef: message.TeamRef, GateRef: message.GateRef,
				ExpectedGateVersion: message.GateVersion, RunRef: message.RunRef,
			})
			if err != nil {
				// Отказ отдельному отправителю не разрывает подписку всего канала.
				switch status.Code(err) {
				case codes.InvalidArgument, codes.PermissionDenied, codes.NotFound, codes.FailedPrecondition:
					return nil
				}
				return err
			}
			switch response.GetOutcome() {
			case controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_IGNORED,
				controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_RUN_STARTED,
				controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_GATE_RESOLVED,
				controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_STALE,
				controlplanev1.InteractionMessageOutcome_INTERACTION_MESSAGE_OUTCOME_DUPLICATE:
				return nil
			default:
				return errDeliveryResponse
			}
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil && !degraded {
			degraded = true
			manager.logger.WarnContext(ctx, "interaction source degraded", "connection_ref", source.GetConnectionRef(), "error_class", "mattermost_or_control_plane")
		}
		timer := time.NewTimer(manager.config.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

// Close permanently rejects new reconciliation, cancels current listeners and waits for all generations within the supplied shutdown deadline.
// RU: Close навсегда запрещает новое согласование, отменяет текущие listener и ждёт все поколения в пределах переданного срока shutdown.
// A timeout returns cancellation without pretending that an uncooperative adapter has already joined.
// RU: Истечение срока возвращает ошибку отмены и не выдаёт ещё работающий несогласованный адаптер за завершённый.
func (manager *sourceManager) Close(ctx context.Context) error {
	manager.mu.Lock()
	manager.closed = true
	for _, session := range manager.sources {
		session.cancel()
	}
	manager.sources = map[string]sourceSession{}
	manager.mu.Unlock()
	done := make(chan struct{})
	go func() {
		manager.wait.Wait()
		close(done)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

// sourceFingerprint hashes a deterministic cloned protobuf snapshot with sorted capabilities, preserving all other source and credential pins.
// RU: sourceFingerprint хеширует детерминированную protobuf-копию снимка с сортированными capabilities, сохраняя остальные координаты источника и реквизитов.
// Nil or unencodable snapshots yield an empty fingerprint; the caller's message is not mutated.
// RU: Nil или несериализуемый снимок даёт пустой fingerprint; сообщение вызывающей стороны не изменяется.
func sourceFingerprint(source *controlplanev1.InteractionSource) string {
	if source == nil {
		return ""
	}
	snapshot := proto.Clone(source).(*controlplanev1.InteractionSource)
	sort.Strings(snapshot.EnabledCapabilities)
	value, err := (proto.MarshalOptions{Deterministic: true}).Marshal(snapshot)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// sourceListens selects sources with inbound or gate-decision subscription capabilities, without authorizing any particular message.
// RU: sourceListens выбирает источники с capability входящей подписки или решений gate, не разрешая конкретное сообщение.
func sourceListens(capabilities []string) bool {
	for _, capability := range capabilities {
		if capability == "mattermost.inbound" || capability == "mattermost.gate_decisions" {
			return true
		}
	}
	return false
}
