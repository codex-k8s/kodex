package app

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/url"
	"path/filepath"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/clients/mailtransport"
)

const mailEgressAddress = "egress-gateway.kodex-system.svc:8082"

type Config struct {
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
	Environment                   string `env:"DEPLOYMENT_ENVIRONMENT,required,notEmpty"`
	OTLPEndpoint                  string `env:"OTEL_EXPORTER_OTLP_ENDPOINT,required,notEmpty"`
	OTLPServerName                string `env:"OTEL_EXPORTER_OTLP_TLS_SERVER_NAME,required,notEmpty"`
	OTLPCAFile                    string `env:"OTEL_EXPORTER_OTLP_CA_FILE,required,notEmpty"`
}

func loadConfig() (Config, error) {
	c := Config{ReconciliationIntervalSeconds: 15, ReconciliationBatch: 16, Listen: ":8443", Technical: ":9090", AuthorityTarget: "control-plane.kodex-system.svc.cluster.local:8443", EgressAddress: mailEgressAddress}
	if env.ParseWithOptions(&c, env.Options{}) != nil {
		return c, errors.New("invalid email bridge environment")
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
	for _, p := range []string{c.SecretsRoot, c.DSNFile, c.CertificateFile, c.PrivateKeyFile, c.CAFile, c.ApplicationGrantFile, c.OTLPCAFile} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return c, errors.New("invalid email bridge file path")
		}
	}
	return c, nil
}

func (c Config) configurationPins() configurationPins {
	return configurationPins{mode: c.ConfigurationMode, revision: c.ExpectedConfigurationRevision, digest: c.ExpectedConfigurationDigest}
}
func tlsConfig(c Config) (*tls.Config, error) {
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
