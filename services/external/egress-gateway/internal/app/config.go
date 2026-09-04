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
	PolicyFile        string `env:"EGRESS_GATEWAY_POLICY_FILE,required"`
	ExpectedRevision  string `env:"EGRESS_GATEWAY_EXPECTED_POLICY_REVISION,required"`
	ExpectedDigest    string `env:"EGRESS_GATEWAY_EXPECTED_POLICY_DIGEST,required"`
	ConnectAddress    string `env:"EGRESS_GATEWAY_CONNECT_LISTEN,required"`
	STTConnectAddress string `env:"EGRESS_GATEWAY_STT_CONNECT_LISTEN,required"`
	TechnicalAddress  string `env:"EGRESS_GATEWAY_TECHNICAL_LISTEN,required"`
	ResolverConfig    string `env:"EGRESS_GATEWAY_RESOLV_CONF,required"`
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
	for _, path := range []string{config.PolicyFile, config.ResolverConfig} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("egress gateway configuration path is invalid")
		}
	}
	for _, listener := range []struct{ address, port string }{
		{config.ConnectAddress, "8080"}, {config.STTConnectAddress, "8081"}, {config.TechnicalAddress, "9090"},
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
	decoded, err := hex.DecodeString(config.ExpectedDigest)
	if err != nil || len(decoded) != sha256.Size || config.ExpectedDigest != strings.ToLower(config.ExpectedDigest) {
		return errors.New("egress gateway expected policy digest is invalid")
	}
	return nil
}
