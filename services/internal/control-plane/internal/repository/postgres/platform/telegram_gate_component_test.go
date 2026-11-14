package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// testTelegramNativeGateComponent creates actual workflow result Gates through owner execution/completion, then proves closing one Gate cancels its unsent notification while closing an already admitted one preserves unknown history and creates a separate terminal source.
// testTelegramNativeGateComponent создаёт настоящие workflow result Gates через owner execution/completion и проверяет, что закрытие Gate отменяет неотправленное уведомление, а закрытие уже допущенного сохраняет unknown history и создаёт отдельный terminal source.
func testTelegramNativeGateComponent(t *testing.T) {
	t.Helper()
	f := newWorkflowGateRegression(t, false, true)
	if err := f.repository.ConfigureInteractionPublicOrigin("https://kodex.example.test"); err != nil {
		t.Fatal(err)
	}
	connection := telegramCreateFixtureConnection(t, f.ctx, f.repository, f.service, map[string]any{"base_url": integrationpackage.TelegramAPIOrigin, "bot_id": float64(123456), "chat_id": "-1001234", "topic": "77", "quiet_enabled": false}, "telegram-workflow-gate-fixture")
	owner := telegramFixturePrincipal(t, f.ctx, f.repository, "platform.command.telegram-subscription.configure", f.projectRef, false)
	subscription, err := f.service.ConfigureTelegramSubscription(f.ctx, owner, value.Mutation{IdempotencyKey: "telegram-workflow-gates-enable"}, port.TelegramSubscriptionConfigure{ProjectRef: f.projectRef, ConnectionRef: connection.Ref, Enabled: true, EventTypes: []string{"HUMAN_GATE", "FINISHED"}, DurationSeconds: 3600, MaxEffects: 10})
	if err != nil {
		t.Fatal("enable native workflow Gate subscription", err)
	}
	leases := f.claims("telegram-result-gates-initial")
	if len(leases) != 2 {
		t.Fatal("native Gate workflow did not admit two branches")
	}
	for index, lease := range leases {
		file := "a.md"
		if stringMap(lease, "agentRef") == f.agents["b"] {
			file = "b.md"
		}
		f.complete([]string{"telegram-result-gates-a", "telegram-result-gates-b"}[index], lease, file, true)
	}
	root, _, err := f.service.GetRunGraph(f.ctx, f.owner, f.runRef)
	if err != nil || root.State != "WAITING_HUMAN" || len(root.GateRefs) != 2 {
		t.Fatal("native workflow did not open two result Gates", err)
	}
	worker := telegramFixturePrincipal(t, f.ctx, f.repository, "platform.interactions.deliveries.claim", "", true)
	claims, err := f.service.ClaimInteractionDeliveries(f.ctx, worker, "telegram-native-gate-fixture", 2)
	if err != nil || len(claims) != 2 {
		t.Fatalf("claim actual Gate notifications: count=%d error=%v", len(claims), err)
	}
	snapshots := make([]integrationpackage.TelegramDeliverySnapshot, 0, 2)
	begins := make([]port.InteractionDeliveryBeginInput, 0, 2)
	for _, claim := range claims {
		snapshot, ok := claim["telegram"].(integrationpackage.TelegramDeliverySnapshot)
		if !ok || snapshot.EventType != "HUMAN_GATE" || snapshot.TemplateKey != "TELEGRAM_HUMAN_GATE_V1" || snapshot.GateVersion != 1 || snapshot.RunRef != f.runRef || snapshot.SubscriptionRef != subscription.Ref {
			t.Fatal("actual Gate notification lost its owner event pins")
		}
		snapshots = append(snapshots, snapshot)
		begins = append(begins, port.InteractionDeliveryBeginInput{DeliveryRef: stringMap(claim, "deliveryRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), InputDigest: stringMap(claim, "inputDigest")})
	}
	if snapshots[0].GateRef == snapshots[1].GateRef {
		t.Fatal("different native Gates reused one source event")
	}
	firstGate, err := f.service.GetOwnerGate(f.ctx, f.owner, snapshots[0].GateRef)
	if err != nil {
		t.Fatal(err)
	}
	f.execute(command.ResolveOwnerGate, "telegram-gate-before-begin", &firstGate.Version, command.GateResolutionInput{GateRef: firstGate.Ref, Decision: "APPROVE"})
	worker = telegramFixturePrincipal(t, f.ctx, f.repository, "platform.interactions.deliveries.begin", "", true)
	if admission, err := f.service.BeginInteractionDelivery(f.ctx, worker, begins[0]); !errors.Is(err, errs.ErrConflict) || admission.Allowed {
		t.Fatal("resolved Gate retained an unsent lease", err)
	}
	if admission, err := f.service.BeginInteractionDelivery(f.ctx, worker, begins[1]); err != nil || !admission.Allowed {
		t.Fatal("open sibling Gate lost its exact admission", err)
	}
	secondGate, err := f.service.GetOwnerGate(f.ctx, f.owner, snapshots[1].GateRef)
	if err != nil {
		t.Fatal(err)
	}
	f.execute(command.ResolveOwnerGate, "telegram-gate-after-begin", &secondGate.Version, command.GateResolutionInput{GateRef: secondGate.Ref, Decision: "APPROVE"})
	if admission, err := f.service.BeginInteractionDelivery(f.ctx, worker, begins[1]); !errors.Is(err, errs.ErrConflict) || admission.Allowed {
		t.Fatal("Gate resolution retained an already admitted lease", err)
	}
	root, err = f.service.GetRun(f.ctx, f.owner, f.runRef)
	if err != nil || root.State != "SUCCEEDED" {
		t.Fatal("optional Telegram notification blocked native workflow completion", err)
	}
	viewer := telegramFixturePrincipal(t, f.ctx, f.repository, "platform.query.interaction-deliveries.list", f.projectRef, false)
	history, _, err := f.service.ListInteractionDeliveries(f.ctx, viewer, query.Filter{ProjectRef: f.projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(history) != 3 {
		t.Fatal("Gate and terminal native events did not retain separate owner history", err)
	}
	for _, delivery := range history {
		switch delivery.Ref {
		case begins[0].DeliveryRef:
			if delivery.State != "CANCELLED" {
				t.Fatal("unsent resolved Gate was not cancelled")
			}
		case begins[1].DeliveryRef:
			if delivery.State != "UNKNOWN_OUTCOME" {
				t.Fatal("possibly sent Gate was rewritten as no-effect")
			}
		default:
			if delivery.EventType != "FINISHED" || delivery.State != "DUE" {
				t.Fatal("native terminal source lost its distinct queued notification")
			}
		}
	}
	owner = telegramFixturePrincipal(t, f.ctx, f.repository, "platform.command.telegram-subscription.configure", f.projectRef, false)
	if _, err := f.service.ConfigureTelegramSubscription(f.ctx, owner, value.Mutation{IdempotencyKey: "telegram-workflow-gates-revoke", ExpectedVersion: &subscription.Version}, port.TelegramSubscriptionConfigure{ProjectRef: f.projectRef}); err != nil {
		t.Fatal("close native Gate fixture subscription", err)
	}
}
