package app

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/url"
	"path/filepath"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
)

// mailEgressAddress pins the dedicated internal mail tunnel rather than the general HTTP or STT proxy ports.
// mailEgressAddress фиксирует выделенный внутренний почтовый туннель, не порты общего HTTP- или STT-прокси.
const mailEgressAddress = "egress-gateway.kodex-system.svc:8082"

// Config carries process listeners, protected file paths and exact owner/egress/configuration pins; it contains no raw mailbox passwords.
// Config содержит listeners процесса, пути защищённых файлов и точные привязки owner, egress и конфигурации; сырых паролей ящиков здесь нет.
type Config struct {
	RPCProfile                    string `env:"KODEX_RPC_PROFILE"`
	ReconciliationIntervalSeconds int    `env:"EMAIL_BRIDGE_RECONCILIATION_INTERVAL_SECONDS"`
	ReconciliationBatch           int    `env:"EMAIL_BRIDGE_RECONCILIATION_BATCH"`
	Listen                        string `env:"EMAIL_BRIDGE_LISTEN"`
	Technical                     string `env:"EMAIL_BRIDGE_TECHNICAL_LISTEN"`
	SecretsRoot                   string `env:"EMAIL_BRIDGE_SECRETS_ROOT,required,notEmpty"`
	DSNFile                       string `env:"EMAIL_BRIDGE_DSN_FILE,required,notEmpty"`
	CertificateFile               string `env:"EMAIL_BRIDGE_CERTIFICATE_FILE,required,notEmpty"`
	PrivateKeyFile                string `env:"EMAIL_BRIDGE_PRIVATE_KEY_FILE,required,notEmpty"`
	CAFile                        string `env:"EMAIL_BRIDGE_CA_FILE,required,notEmpty"`
	AuthorityTarget               string `env:"EMAIL_BRIDGE_AUTHORITY_TARGET"`
	ApplicationGrantFile          string `env:"EMAIL_BRIDGE_APPLICATION_GRANT_FILE,required,notEmpty"`
	EgressAddress                 string `env:"EMAIL_BRIDGE_EGRESS_ADDRESS"`
	EgressPolicyDigest            string `env:"EMAIL_BRIDGE_EGRESS_POLICY_DIGEST,required,notEmpty"`
	ConfigurationMode             string `env:"EMAIL_BRIDGE_CONFIGURATION_MODE,required,notEmpty"`
	ExpectedConfigurationRevision int64  `env:"EMAIL_BRIDGE_EXPECTED_CONFIGURATION_REVISION"`
	ExpectedConfigurationDigest   string `env:"EMAIL_BRIDGE_EXPECTED_CONFIGURATION_DIGEST"`
}

// loadConfig applies process defaults, parses required environment fields and rejects unsupported profiles, destinations, limits or unclean paths.
// File contents and remote readiness are checked later; parser details are replaced by bounded English diagnostics.
// loadConfig применяет defaults процесса, разбирает обязательные поля окружения и отвергает неподдержанные профили, адреса, лимиты и неочищенные пути.
// Содержимое файлов и удалённая готовность проверяются позже; подробности parser заменяются ограниченной английской диагностикой.
func loadConfig() (Config, error) {
	c := Config{ReconciliationIntervalSeconds: 15, ReconciliationBatch: 16, Listen: ":8443", Technical: ":9090", AuthorityTarget: "control-plane.kodex-system.svc.cluster.local:8443", EgressAddress: mailEgressAddress}
	if env.ParseWithOptions(&c, env.Options{}) != nil {
		return c, errors.New("invalid email bridge environment")
	}
	if c.RPCProfile != "" && c.RPCProfile != transportprofile.TrustedCluster {
		return c, errors.New("invalid email bridge RPC profile")
	}
	if c.ReconciliationIntervalSeconds < 5 || c.ReconciliationIntervalSeconds > 300 || c.ReconciliationBatch < 1 || c.ReconciliationBatch > 64 {
		return c, errors.New("invalid email reconciliation limits")
	}
	if c.AuthorityTarget != "control-plane.kodex-system.svc.cluster.local:8443" || c.EgressAddress != mailEgressAddress || !mailtransport.ValidEgressDigest(c.EgressPolicyDigest) {
		return c, errors.New("invalid email bridge destinations")
	}
	if !c.configurationPins().valid() {
		return c, errors.New(configurationPinError)
	}
	for _, p := range []string{c.SecretsRoot, c.DSNFile, c.CertificateFile, c.PrivateKeyFile, c.CAFile, c.ApplicationGrantFile} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return c, errors.New("invalid email bridge file path")
		}
	}
	return c, nil
}

// configurationPins selects the deployment-owned publication mode and expected immutable configuration identity.
// configurationPins выбирает назначенный развёртыванием режим публикации и ожидаемую неизменяемую идентичность конфигурации.
func (c Config) configurationPins() configurationPins {
	return configurationPins{mode: c.ConfigurationMode, revision: c.ExpectedConfigurationRevision, digest: c.ExpectedConfigurationDigest}
}

// tlsConfig loads bounded protected TLS material and requires a verified client certificate in the protected profile.
// TrustedCluster explicitly returns nil for plaintext serving; this choice is not evidence of mTLS or user authorization.
// tlsConfig читает ограниченные защищённые TLS-материалы и требует проверенный клиентский сертификат в защищённом профиле.
// TrustedCluster явно возвращает nil для plaintext; этот выбор не доказывает mTLS или пользовательские полномочия.
func tlsConfig(c Config) (*tls.Config, error) {
	if c.RPCProfile == transportprofile.TrustedCluster {
		return nil, nil
	}
	cert, e := securefile.Read(c.CertificateFile, 1<<20)
	if e != nil {
		return nil, failure(stageCertificate, e)
	}
	key, e := securefile.Read(c.PrivateKeyFile, 1<<20)
	if e != nil {
		return nil, failure(stagePrivateKey, e)
	}
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil {
		return nil, failure(stageKeyPair, e)
	}
	ca, e := securefile.Read(c.CAFile, 1<<20)
	if e != nil {
		return nil, failure(stageCA, e)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, failure(stageCA, errors.New("TLS CA invalid"))
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, RootCAs: roots, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}

// databaseDSN reads bounded protected bytes and allows only the exact database host, role, database, verify-full mode and CA path.
// Duplicate or non-allowlisted query keys are rejected so pgx options cannot override the validated URI boundary.
// databaseDSN читает ограниченные защищённые байты и допускает только точные host, роль, БД, verify-full и путь CA.
// Повторные и неразрешённые query keys отклоняются, чтобы параметры pgx не переопределили проверенную URI-границу.
func databaseDSN(path string) (string, error) {
	raw, e := securefile.Read(path, 16384)
	if e != nil {
		return "", failure(stageDSN, e)
	}
	u, e := url.Parse(string(raw))
	if e != nil || u.Scheme != "postgresql" || u.Hostname() != "email-bridge-postgresql.kodex-system.svc.cluster.local" || (u.Port() != "" && u.Port() != "5432") || u.Path != "/email_bridge" || u.Fragment != "" || u.Query().Get("sslmode") != "verify-full" || u.User == nil || u.User.Username() != "email_bridge_runtime" {
		return "", failure(stageDSNValidation, errors.New("database transport invalid"))
	}
	query, e := url.ParseQuery(u.RawQuery)
	if e != nil || query.Get("sslrootcert") != "/var/run/email/tls/ca.crt" {
		return "", failure(stageDSNValidation, errors.New("database transport invalid"))
	}
	// Параметры pgx не должны переопределять проверенные host/user/TLS из URI.
	for key, values := range query {
		if len(values) != 1 || (key != "sslmode" && key != "sslrootcert" && key != "connect_timeout" && key != "application_name") {
			return "", failure(stageDSNValidation, errors.New("database transport invalid"))
		}
	}
	return string(raw), nil
}
