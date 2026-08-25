package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/external/interaction-gateway/internal/mattermost"
)

type sourceSession struct {
	fingerprint string
	cancel      context.CancelFunc
}

type sourceManager struct {
	control *controlplaneclient.Client
	adapter *mattermost.Adapter
	logger  *slog.Logger
	config  Config
	mu      sync.Mutex
	sources map[string]sourceSession
	wait    sync.WaitGroup
}

func newSourceManager(control *controlplaneclient.Client, adapter *mattermost.Adapter, logger *slog.Logger, config Config) *sourceManager {
	return &sourceManager{control: control, adapter: adapter, logger: logger, config: config, sources: map[string]sourceSession{}}
}

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
			} else if !degraded {
				degraded = true
				logger.WarnContext(ctx, "interaction source discovery degraded", "error_class", "control_plane")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
}

func (manager *sourceManager) Reconcile(parent context.Context, desired []*controlplanev1.InteractionSource) {
	wanted := map[string]*controlplanev1.InteractionSource{}
	for _, source := range desired {
		if source == nil || source.GetConnectionRef() == "" || !sourceListens(source.GetEnabledCapabilities()) {
			continue
		}
		wanted[source.GetConnectionRef()] = source
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for reference, session := range manager.sources {
		source, ok := wanted[reference]
		if ok && session.fingerprint == sourceFingerprint(source) {
			delete(wanted, reference)
			continue
		}
		session.cancel()
		delete(manager.sources, reference)
	}
	for reference, source := range wanted {
		child, cancel := context.WithCancel(parent)
		manager.sources[reference] = sourceSession{fingerprint: sourceFingerprint(source), cancel: cancel}
		manager.wait.Add(1)
		go manager.run(child, source)
	}
}

func (manager *sourceManager) run(ctx context.Context, source *controlplanev1.InteractionSource) {
	defer manager.wait.Done()
	degraded := false
	for {
		err := manager.adapter.Listen(ctx, source, func(messageContext context.Context, message mattermost.Message) (string, error) {
			response, err := manager.control.Interaction.AcceptInteractionMessage(messageContext, &controlplanev1.AcceptInteractionMessageRequest{
				Mutation:      &controlplanev1.MutationContext{IdempotencyKey: stableKey(source.GetConnectionRef(), message.EventRef)},
				ConnectionRef: source.GetConnectionRef(), ExternalEventRef: message.EventRef,
				ExternalPostRef: message.PostRef, ExternalRootPostRef: message.RootPostRef,
				ExternalChannelRef: message.ChannelRef, ExternalUserDigest: message.UserDigest,
				Message: message.Text, Decision: message.Decision,
			})
			if err != nil {
				return "", err
			}
			return response.GetMessageKey(), nil
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

func (manager *sourceManager) Close(ctx context.Context) error {
	manager.mu.Lock()
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

func sourceFingerprint(source *controlplanev1.InteractionSource) string {
	capabilities := append([]string(nil), source.GetEnabledCapabilities()...)
	sort.Strings(capabilities)
	value := strings.Join([]string{
		source.GetConnectionRef(), source.GetCredentialMaterializationRef(), source.GetBaseUrl(),
		source.GetTeamName(), source.GetChannelName(), source.GetLocale(), strings.Join(capabilities, ","),
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func sourceListens(capabilities []string) bool {
	for _, capability := range capabilities {
		if capability == "mattermost.inbound" || capability == "mattermost.gate_decisions" {
			return true
		}
	}
	return false
}
