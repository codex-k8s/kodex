package app

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/url"
	"path/filepath"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/securefile"
)

type Config struct {
	Listen              string `env:"EMAIL_BRIDGE_LISTEN"`
	Technical           string `env:"EMAIL_BRIDGE_TECHNICAL_LISTEN"`
	ConfigurationFile   string `env:"EMAIL_BRIDGE_CONFIGURATION_FILE,required,notEmpty"`
	SecretsRoot         string `env:"EMAIL_BRIDGE_SECRETS_ROOT,required,notEmpty"`
	DSNFile             string `env:"EMAIL_BRIDGE_DSN_FILE,required,notEmpty"`
	CertificateFile     string `env:"EMAIL_BRIDGE_CERTIFICATE_FILE,required,notEmpty"`
	PrivateKeyFile      string `env:"EMAIL_BRIDGE_PRIVATE_KEY_FILE,required,notEmpty"`
	CAFile              string `env:"EMAIL_BRIDGE_CA_FILE,required,notEmpty"`
	AuthorityURL        string `env:"EMAIL_BRIDGE_AUTHORITY_URL"`
	AuthorityBearerFile string `env:"EMAIL_BRIDGE_AUTHORITY_BEARER_FILE,required,notEmpty"`
	HealthTokenFile     string `env:"EMAIL_BRIDGE_HEALTH_TOKEN_FILE,required,notEmpty"`
	EgressAddress       string `env:"EMAIL_BRIDGE_EGRESS_ADDRESS"`
	Environment         string `env:"DEPLOYMENT_ENVIRONMENT,required,notEmpty"`
	OTLPEndpoint        string `env:"OTEL_EXPORTER_OTLP_ENDPOINT,required,notEmpty"`
	OTLPServerName      string `env:"OTEL_EXPORTER_OTLP_TLS_SERVER_NAME,required,notEmpty"`
	OTLPCAFile          string `env:"OTEL_EXPORTER_OTLP_CA_FILE,required,notEmpty"`
}

func loadConfig() (Config, error) {
	c := Config{Listen: ":8443", Technical: ":9090", AuthorityURL: "https://control-plane.kodex-system.svc.cluster.local:8444", EgressAddress: "egress-gateway.kodex-system.svc:8080"}
	if env.ParseWithOptions(&c, env.Options{}) != nil {
		return c, errors.New("invalid email bridge environment")
	}
	if c.AuthorityURL != "https://control-plane.kodex-system.svc.cluster.local:8444" || c.EgressAddress != "egress-gateway.kodex-system.svc:8080" {
		return c, errors.New("invalid email bridge destinations")
	}
	for _, p := range []string{c.ConfigurationFile, c.SecretsRoot, c.DSNFile, c.CertificateFile, c.PrivateKeyFile, c.CAFile, c.AuthorityBearerFile, c.HealthTokenFile, c.OTLPCAFile} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return c, errors.New("invalid email bridge file path")
		}
	}
	return c, nil
}
func tlsConfig(c Config) (*tls.Config, error) {
	cert, e := securefile.Read(c.CertificateFile, 1<<20)
	if e != nil {
		return nil, errors.New("TLS certificate unavailable")
	}
	key, e := securefile.Read(c.PrivateKeyFile, 1<<20)
	if e != nil {
		return nil, errors.New("TLS key unavailable")
	}
	pair, e := tls.X509KeyPair(cert, key)
	if e != nil {
		return nil, errors.New("TLS keypair invalid")
	}
	ca, e := securefile.Read(c.CAFile, 1<<20)
	if e != nil {
		return nil, errors.New("TLS CA unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("TLS CA invalid")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, RootCAs: roots, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}, nil
}
func databaseDSN(path string) (string, error) {
	raw, e := securefile.Read(path, 16384)
	if e != nil {
		return "", errors.New("database credential unavailable")
	}
	u, e := url.Parse(string(raw))
	if e != nil || u.Scheme != "postgresql" || u.Hostname() != "email-bridge-postgresql.kodex-system.svc.cluster.local" || (u.Port() != "" && u.Port() != "5432") || u.Path != "/email_bridge" || u.Fragment != "" || u.Query().Get("sslmode") != "verify-full" || u.User == nil || u.User.Username() != "email_bridge_runtime" {
		return "", errors.New("database transport invalid")
	}
	query, e := url.ParseQuery(u.RawQuery)
	if e != nil || query.Get("sslrootcert") != "/var/run/email/tls/ca.crt" {
		return "", errors.New("database transport invalid")
	}
	// Параметры pgx не должны переопределять проверенные host/user/TLS из URI.
	for key, values := range query {
		if len(values) != 1 || (key != "sslmode" && key != "sslrootcert" && key != "connect_timeout" && key != "application_name") {
			return "", errors.New("database transport invalid")
		}
	}
	return string(raw), nil
}
