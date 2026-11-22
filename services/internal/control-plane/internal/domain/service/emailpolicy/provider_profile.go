package emailpolicy

import (
	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

const (
	// ProviderCustom preserves explicit, owner-validated endpoint configuration without claiming a provider-specific authentication profile.
	// ProviderCustom сохраняет явную конфигурацию endpoints, проверяемую владельцем, без заявления о специальном профиле входа провайдера.
	ProviderCustom = "CUSTOM"
	// ProviderTimewebPassword binds the Timeweb mailbox password profile to the provider's exact SMTP and IMAP TLS endpoints.
	// ProviderTimewebPassword связывает профиль пароля ящика Timeweb с точными SMTP и IMAP TLS endpoints провайдера.
	ProviderTimewebPassword = "TIMEWEB_PASSWORD"
	// ProviderGmailAppPassword denotes an explicitly selected application password, whose availability must still be checked by authenticating the actual account.
	// ProviderGmailAppPassword обозначает явно выбранный пароль приложения; его доступность по-прежнему проверяется входом в настоящий аккаунт.
	ProviderGmailAppPassword = "GMAIL_APP_PASSWORD"
)

// KnownProviderProfile admits an omitted historical profile or one closed setup choice; arbitrary labels cannot claim validated provider behavior.
// KnownProviderProfile допускает отсутствующий исторический профиль либо один закрытый вариант настройки; произвольная метка не подтверждает поведение провайдера.
func KnownProviderProfile(profile string) bool {
	return profile == "" || profile == ProviderCustom || profile == ProviderTimewebPassword || profile == ProviderGmailAppPassword
}

// ValidateProviderProfile constrains selected provider profiles before publication. It does not test an account, infer a secret's type, enable app passwords, or implement OAuth refresh.
// ValidateProviderProfile ограничивает выбранные профили до публикации. Не проверяет аккаунт, не выводит вид секрета, не включает пароли приложений и не реализует OAuth refresh.
func ValidateProviderProfile(spec entity.EmailMailboxSpecification) error {
	if !KnownProviderProfile(spec.ProviderProfile) {
		return errs.ErrInvalid
	}
	if spec.ProviderProfile == "" || spec.ProviderProfile == ProviderCustom {
		return nil
	}
	if spec.ReceiveProtocol != "imap" || spec.IMAP == nil || spec.POP != nil {
		return errs.ErrInvalid
	}
	smtpHost, imapHost := "smtp.timeweb.ru", "imap.timeweb.ru"
	if spec.ProviderProfile == ProviderGmailAppPassword {
		smtpHost, imapHost = "smtp.gmail.com", "imap.gmail.com"
	}
	if !passwordProfileEndpoint(spec.SMTP, smtpHost, 465, 587) || !passwordProfileEndpoint(*spec.IMAP, imapHost, 993, 143) {
		return errs.ErrInvalid
	}
	if spec.ProviderProfile == ProviderGmailAppPassword && (spec.IMAP.Port != 993 || spec.IMAP.TlsMode != "implicit") {
		return errs.ErrInvalid
	}
	return nil
}

// passwordProfileEndpoint requires exact hostname/SNI, password authentication and a documented implicit-TLS or STARTTLS port pair; no alternative server or bearer transport is inferred.
// passwordProfileEndpoint требует точные hostname/SNI, вход по паролю и документированную пару порта с implicit TLS либо STARTTLS; иной сервер или bearer transport не подставляется.
func passwordProfileEndpoint(endpoint api.Endpoint, host string, implicit, startTLS int) bool {
	return endpoint.Host == host && endpoint.ServerName == host && endpoint.AuthMethod == "password" &&
		(endpoint.Port == implicit && endpoint.TlsMode == "implicit" || endpoint.Port == startTLS && endpoint.TlsMode == "starttls")
}
