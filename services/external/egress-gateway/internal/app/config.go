package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"path/filepath"
	"strings"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Mode                      string `env:"EGRESS_GATEWAY_MODE"`
	PolicyFile                string `env:"EGRESS_GATEWAY_POLICY_FILE,required"`
	ExpectedRevision          string `env:"EGRESS_GATEWAY_EXPECTED_POLICY_REVISION,required"`
	ExpectedDigest            string `env:"EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST,required"`
	ConnectAddress            string `env:"EGRESS_GATEWAY_CONNECT_LISTEN,required"`
	STTConnectAddress         string `env:"EGRESS_GATEWAY_STT_CONNECT_LISTEN"`
	MailConnectAddress        string `env:"EGRESS_GATEWAY_MAIL_CONNECT_LISTEN"`
	MailPolicyFile            string `env:"EGRESS_GATEWAY_MAIL_POLICY_FILE"`
	MailExpectedDigest        string `env:"EGRESS_GATEWAY_MAIL_POLICY_DIGEST"`
	IntegrationConnectAddress string `env:"EGRESS_GATEWAY_INTEGRATION_CONNECT_LISTEN"`
	RuntimeConnectAddress     string `env:"EGRESS_GATEWAY_RUNTIME_CONNECT_LISTEN"`
	RuntimeSigningKeyFile     string `env:"EGRESS_GATEWAY_RUNTIME_SIGNING_KEY_FILE"`
	RuntimeProxyCACertificate string `env:"EGRESS_GATEWAY_RUNTIME_PROXY_CA_CERTIFICATE_FILE"`
	RuntimeProxyCAPrivateKey  string `env:"EGRESS_GATEWAY_RUNTIME_PROXY_CA_PRIVATE_KEY_FILE"`
	IntegrationPolicyFile     string `env:"EGRESS_GATEWAY_INTEGRATION_POLICY_FILE"`
	IntegrationExpectedDigest string `env:"EGRESS_GATEWAY_INTEGRATION_POLICY_DIGEST"`
	TechnicalAddress          string `env:"EGRESS_GATEWAY_TECHNICAL_LISTEN,required"`
	ResolverConfig            string `env:"EGRESS_GATEWAY_RESOLV_CONF,required"`
}

func loadConfig() (Config, error) {
	var config Config
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, errors.New("egress gateway environment configuration is invalid")
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	if config.Mode != "" && config.Mode != "clamav" {
		return errors.New("egress gateway mode is invalid")
	}
	paths := []string{config.PolicyFile, config.ResolverConfig}
	if config.Mode == "" {
		paths = append(paths, config.MailPolicyFile, config.IntegrationPolicyFile, config.RuntimeSigningKeyFile,
			config.RuntimeProxyCACertificate, config.RuntimeProxyCAPrivateKey)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("egress gateway configuration path is invalid")
		}
	}
	listeners := []struct{ address, port string }{{config.ConnectAddress, "8080"}, {config.TechnicalAddress, "9090"}}
	if config.Mode == "" {
		listeners = append(listeners,
			struct{ address, port string }{config.STTConnectAddress, "8081"},
			struct{ address, port string }{config.MailConnectAddress, "8082"},
			struct{ address, port string }{config.IntegrationConnectAddress, "8083"},
			struct{ address, port string }{config.RuntimeConnectAddress, "8084"})
	}
	for _, listener := range listeners {
		if _, port, err := net.SplitHostPort(listener.address); err != nil || port != listener.port {
			return errors.New("egress gateway listen address is invalid")
		}
	}
	if config.ConnectAddress == config.TechnicalAddress || len(config.ExpectedRevision) < 3 || len(config.ExpectedRevision) > 64 ||
		strings.TrimSpace(config.ExpectedRevision) != config.ExpectedRevision {
		return errors.New("egress gateway deployment expectation is invalid")
	}
	if config.Mode == "" && (config.STTConnectAddress == config.ConnectAddress || config.STTConnectAddress == config.TechnicalAddress) {
		return errors.New("egress gateway deployment expectation is invalid")
	}
	digests := []string{config.ExpectedDigest}
	if config.Mode == "" {
		digests = append(digests, config.MailExpectedDigest, config.IntegrationExpectedDigest)
	}
	for _, digest := range digests {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
			return errors.New("egress gateway expected policy digest is invalid")
		}
	}
	return nil
}
