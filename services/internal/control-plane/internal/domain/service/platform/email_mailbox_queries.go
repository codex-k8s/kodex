package platform

import (
	"context"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func validMailboxQuery(connectionRef string, page query.Page) bool {
	return runtimeDiffReference.MatchString(connectionRef) && page.Size >= 0 && page.Size <= 100 && len(page.Token) <= 4096
}

func (service *Service) ReportEmailConfigurationReadback(ctx context.Context, p value.Principal, revision int64, digest string) error {
	p, err := service.principal(ctx, p)
	if err != nil {
		return err
	}
	return service.repository.ReportEmailConfigurationReadback(ctx, p, revision, digest)
}

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
