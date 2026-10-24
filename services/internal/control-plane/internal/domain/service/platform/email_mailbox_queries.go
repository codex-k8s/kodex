package platform

import (
	"context"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// validMailboxQuery checks the local connection-reference pattern, a zero-to-one-hundred page size
// and a cursor no larger than 4096 bytes without resolving ownership.
// validMailboxQuery проверяет локальную форму ссылки подключения, размер страницы от нуля до ста и
// курсор до 4096 байт без разрешения владения.
func validMailboxQuery(connectionRef string, page query.Page) bool {
	return runtimeDiffReference.MatchString(connectionRef) && page.Size >= 0 && page.Size <= 100 && len(page.Token) <= 4096
}

// ReportEmailConfigurationReadback resolves principal and forwards observed configuration
// revision/digest to the authoritative repository for readback validation and state handling.
// ReportEmailConfigurationReadback разрешает principal и передаёт наблюдаемые revision и digest
// конфигурации авторитетному репозиторию для проверки readback и обработки состояния.
func (service *Service) ReportEmailConfigurationReadback(ctx context.Context, p value.Principal, revision int64, digest string) error {
	p, err := service.principal(ctx, p)
	if err != nil {
		return err
	}
	return service.repository.ReportEmailConfigurationReadback(ctx, p, revision, digest)
}

// PreviewEmailMailboxConfiguration resolves principal, accepts only bounded JSON/YAML input and
// delegates mailbox parsing and preview semantics to the owner.
// PreviewEmailMailboxConfiguration разрешает principal, принимает только ограниченный ввод JSON
// либо YAML и передаёт разбор ящика и семантику предпросмотра владельцу.
func (service *Service) PreviewEmailMailboxConfiguration(ctx context.Context, p value.Principal, connectionRef, format, content string) (entity.EmailMailboxPreview, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.EmailMailboxPreview{}, err
	}
	if !validMailboxQuery(connectionRef, query.Page{}) || len(content) > 256<<10 || format != "JSON" && format != "YAML" {
		return entity.EmailMailboxPreview{}, errs.ErrInvalid
	}
	return service.repository.PreviewEmailMailboxConfiguration(ctx, p, connectionRef, format, content)
}

// GetEmailMailboxConfiguration resolves principal and validates connection plus optional
// configuration/revision locators before owner-scoped mailbox readback.
// GetEmailMailboxConfiguration разрешает principal и проверяет подключение и необязательные
// locators конфигурации и ревизии до чтения почтового ящика в области владельца.
func (service *Service) GetEmailMailboxConfiguration(ctx context.Context, p value.Principal, connectionRef, configurationRef, revisionRef string) (entity.EmailMailboxConfigurationView, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.EmailMailboxConfigurationView{}, err
	}
	if !validMailboxQuery(connectionRef, query.Page{}) || configurationRef != "" && !runtimeDiffReference.MatchString(configurationRef) || revisionRef != "" && !runtimeDiffReference.MatchString(revisionRef) {
		return entity.EmailMailboxConfigurationView{}, errs.ErrInvalid
	}
	return service.repository.GetEmailMailboxConfiguration(ctx, p, connectionRef, configurationRef, revisionRef)
}

// ListEmailMailboxConfigurations resolves principal and bounds connection/page fields and a
// 256-byte UTF-8 search string before authoritative mailbox listing.
// ListEmailMailboxConfigurations разрешает principal и ограничивает поля подключения и страницы и
// UTF-8 строку поиска до 256 байт до авторитетного списка почтовых конфигураций.
func (service *Service) ListEmailMailboxConfigurations(ctx context.Context, p value.Principal, connectionRef, search string, page query.Page) (entity.EmailMailboxPage, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.EmailMailboxPage{}, err
	}
	if !validMailboxQuery(connectionRef, page) || len(search) > 256 || !utf8.ValidString(search) {
		return entity.EmailMailboxPage{}, errs.ErrInvalid
	}
	return service.repository.ListEmailMailboxConfigurations(ctx, p, connectionRef, search, page)
}

// ListEmailMailboxCredentials resolves principal and accepts only empty or known
// CA-certificate/username/auth-secret filters with bounded mailbox pagination before owner
// metadata reads.
// ListEmailMailboxCredentials разрешает principal и принимает только пустой либо известный фильтр
// CA certificate, username или auth secret с ограниченной пагинацией до чтения метаданных
// владельца.
func (service *Service) ListEmailMailboxCredentials(ctx context.Context, p value.Principal, connectionRef, kind string, page query.Page) ([]entity.EmailMailboxCredential, int64, string, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return nil, 0, "", err
	}
	if !validMailboxQuery(connectionRef, page) || kind != "" && kind != "CA_CERTIFICATE" && kind != "USERNAME" && kind != "AUTH_SECRET" {
		return nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListEmailMailboxCredentials(ctx, p, connectionRef, kind, page)
}

// GetEmailMailboxCredentialReceipt resolves principal and validates the connection and bounded
// UTF-8 idempotency key before authoritative receipt readback without repeating credential
// materialization.
// GetEmailMailboxCredentialReceipt разрешает principal и проверяет подключение и ограниченный
// UTF-8 ключ идемпотентности до авторитетного чтения receipt без повтора материализации
// реквизитов.
func (service *Service) GetEmailMailboxCredentialReceipt(ctx context.Context, p value.Principal, connectionRef, key string) (entity.EmailMailboxCredential, error) {
	p, err := service.principal(ctx, p)
	if err != nil {
		return entity.EmailMailboxCredential{}, err
	}
	if !validMailboxQuery(connectionRef, query.Page{}) || len(key) < 8 || len(key) > 128 || !utf8.ValidString(key) {
		return entity.EmailMailboxCredential{}, errs.ErrInvalid
	}
	return service.repository.GetEmailMailboxCredentialReceipt(ctx, p, connectionRef, key)
}
