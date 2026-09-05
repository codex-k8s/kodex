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
	PolicyFile         string `env:"EGRESS_GATEWAY_POLICY_FILE,required"`
	ExpectedRevision   string `env:"EGRESS_GATEWAY_EXPECTED_POLICY_REVISION,required"`
	ExpectedDigest     string `env:"EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST,required"`
	ConnectAddress     string `env:"EGRESS_GATEWAY_CONNECT_LISTEN,required"`
	STTConnectAddress  string `env:"EGRESS_GATEWAY_STT_CONNECT_LISTEN,required"`
	MailConnectAddress string `env:"EGRESS_GATEWAY_MAIL_CONNECT_LISTEN,required"`
	MailPolicyFile     string `env:"EGRESS_GATEWAY_MAIL_POLICY_FILE,required"`
	MailExpectedDigest string `env:"EGRESS_GATEWAY_MAIL_POLICY_DIGEST,required"`
	TechnicalAddress   string `env:"EGRESS_GATEWAY_TECHNICAL_LISTEN,required"`
	ResolverConfig     string `env:"EGRESS_GATEWAY_RESOLV_CONF,required"`
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
	for _, path := range []string{config.PolicyFile, config.ResolverConfig, config.MailPolicyFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("egress gateway configuration path is invalid")
		}
	}
	for _, listener := range []struct{ address, port string }{
		{config.ConnectAddress, "8080"}, {config.STTConnectAddress, "8081"}, {config.TechnicalAddress, "9090"},
		{config.MailConnectAddress, "8082"},
	} {
		if _, port, err := net.SplitHostPort(listener.address); err != nil || port != listener.port {
			return errors.New("egress gateway listen address is invalid")
		}
	}
	if config.ConnectAddress == config.TechnicalAddress || config.STTConnectAddress == config.ConnectAddress ||
		config.STTConnectAddress == config.TechnicalAddress || len(config.ExpectedRevision) < 3 || len(config.ExpectedRevision) > 64 ||
		strings.TrimSpace(config.ExpectedRevision) != config.ExpectedRevision {
		return errors.New("egress gateway deployment expectation is invalid")
	}
	for _, digest := range []string{config.ExpectedDigest, config.MailExpectedDigest} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
			return errors.New("egress gateway expected policy digest is invalid")
		}
	}
	return nil
}
