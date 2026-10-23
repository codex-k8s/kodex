package app

import (
	"context"
	"errors"
	"sync/atomic"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/configuration"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
)

const (
	// configurationBootstrap permits only the empty release-owned seed before the first managed mailbox publication.
	// configurationBootstrap допускает только пустой release-owned seed до первой управляемой публикации ящиков.
	configurationBootstrap = "bootstrap"
	// configurationManaged requires an exact deployment-pinned revision and digest for every local snapshot.
	// configurationManaged требует точные закреплённые развёртыванием ревизию и дайджест каждого локального снимка.
	configurationManaged = "managed"
	// configurationPinError reports a bounded pin mismatch without including configuration contents or private paths.
	// configurationPinError сообщает ограниченную ошибку привязок без содержимого конфигурации или закрытых путей.
	configurationPinError = "email configuration deployment pin mismatch"
)

// configurationPins keeps the publication mode and exact expected revision/digest outside the caller-provided configuration.
// configurationPins хранит режим публикации и точные ожидаемые ревизию и дайджест отдельно от входной конфигурации.
type configurationPins struct {
	mode     string
	revision int64
	digest   string
}

// valid accepts only unpinned bootstrap or positive-revision managed mode with a canonical digest; hybrid modes fail closed.
// valid допускает только bootstrap без привязок либо managed с положительной ревизией и каноническим дайджестом; смешанные режимы закрыто отклоняются.
func (p configurationPins) valid() bool {
	return p.mode == configurationBootstrap && p.revision == 0 && p.digest == "" ||
		p.mode == configurationManaged && p.revision > 0 && mailtransport.ValidEgressDigest(p.digest)
}

// check requires the exact managed revision/digest or the specific empty release-bootstrap seed after pin syntax validation.
// It checks deployment correlation, not durable anti-rollback state; the repository acceptance step owns that boundary.
// check после проверки формата требует точную managed-ревизию и дайджест либо конкретный пустой seed release-bootstrap.
// Проверяется связь с развёртыванием, не устойчивый запрет отката; этой границей владеет шаг приёма в репозитории.
func (p configurationPins) check(value api.Configuration, digest string) error {
	if !p.valid() {
		return errors.New(configurationPinError)
	}
	if p.mode == configurationManaged && value.Revision == p.revision && digest == p.digest {
		return nil
	}
	// Только пустой release seed допускается до первой owner-публикации.
	if p.mode == configurationBootstrap && value.Version == "email-bridge/v1" && value.Revision == 1 && value.ManagedBy == "git" && value.Source == "release-bootstrap" && len(value.Mailboxes) == 0 {
		return nil
	}
	return errors.New(configurationPinError)
}

// configurationRuntime publishes complete service snapshots atomically after local pin and durable watermark checks.
// Existing callers retain their chosen service pointer; a failed new local refresh clears only the current selection.
// configurationRuntime атомарно публикует полные снимки сервиса после проверки локальных привязок и устойчивого watermark.
// Текущие вызовы сохраняют выбранный указатель сервиса; отказ нового локального обновления очищает только текущий выбор.
type configurationRuntime struct {
	root    string
	check   func(api.Configuration, string) error
	accept  func(context.Context, api.Configuration, string) error
	report  func(context.Context, int64, string) error
	build   func(*configuration.Snapshot) *mail.Service
	current atomic.Pointer[mail.Service]
}

// Refresh publishes the locally validated snapshot before optional owner acknowledgement; acknowledgement failure remains a work-path diagnostic.
// Refresh публикует локально проверенный снимок до необязательного подтверждения владельцем; отказ подтверждения остаётся диагностикой рабочего пути.
func (r *configurationRuntime) Refresh(ctx context.Context) (result error) {
	return r.refresh(ctx, true)
}

// RefreshLocal loads, accepts and publishes a complete local snapshot without invoking the owner's readback callback.
// RefreshLocal читает, принимает и публикует полный локальный снимок без вызова callback подтверждения владельцем.
func (r *configurationRuntime) RefreshLocal(ctx context.Context) (result error) {
	return r.refresh(ctx, false)
}

// refresh orders load, digest/pin check, durable acceptance, service construction and cancellation check before atomic publication.
// Any local failure removes current eligibility; a later owner-report failure returns a staged error without retracting the valid local service.
// refresh выполняет чтение, digest/pin, устойчивый приём, создание сервиса и проверку отмены до атомарной публикации.
// Любой локальный отказ снимает текущий допуск; поздний сбой отчёта владельцу возвращает ошибку этапа, не отзывая допустимый локальный сервис.
func (r *configurationRuntime) refresh(ctx context.Context, report bool) (result error) {
	stage := stageConfiguration
	defer func() { result = failure(stage, result) }()
	snapshot, err := configuration.Load(ctx, r.root)
	var digest string
	if err == nil {
		digest = api.Digest(snapshot.Configuration)
		if r.check != nil {
			stage = stagePins
			err = r.check(snapshot.Configuration, digest)
		}
	}
	if err == nil {
		stage = stageWatermark
		err = r.accept(ctx, snapshot.Configuration, digest)
	}
	var service *mail.Service
	if err == nil {
		stage = stageService
		service = r.build(snapshot)
		if service == nil {
			err = errors.New("email configuration service unavailable")
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		r.current.Store(nil)
		return err
	}
	r.current.Store(service)
	if report && r.report != nil {
		stage = stageReadback
		if err = r.report(ctx, snapshot.Configuration.Revision, digest); err != nil {
			return err
		}
	}
	return nil
}

// Service atomically reads the currently eligible snapshot or nil, without altering any already-selected in-flight snapshot.
// Service атомарно читает текущий допустимый снимок либо nil, не меняя уже выбранные снимки исполняемых вызовов.
func (r *configurationRuntime) Service() *mail.Service { return r.current.Load() }
