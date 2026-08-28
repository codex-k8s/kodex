package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const serviceName = "session-archive"

type Config struct {
	Environment                     string        `env:"SESSION_ARCHIVE_ENVIRONMENT"`
	Namespace                       string        `env:"SESSION_ARCHIVE_NAMESPACE"`
	InstanceID                      string        `env:"SESSION_ARCHIVE_INSTANCE_ID"`
	TechnicalListen                 string        `env:"SESSION_ARCHIVE_TECHNICAL_LISTEN"`
	ControlPlaneTarget              string        `env:"SESSION_ARCHIVE_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName       string        `env:"SESSION_ARCHIVE_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile              string        `env:"SESSION_ARCHIVE_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile     string        `env:"SESSION_ARCHIVE_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile      string        `env:"SESSION_ARCHIVE_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile            string        `env:"SESSION_ARCHIVE_APPLICATION_GRANT_FILE"`
	WorkerImage                     string        `env:"SESSION_ARCHIVE_WORKER_IMAGE"`
	WorkerServiceAccount            string        `env:"SESSION_ARCHIVE_WORKER_SERVICE_ACCOUNT"`
	StorageClass                    string        `env:"SESSION_ARCHIVE_STORAGE_CLASS"`
	SessionPVCSize                  string        `env:"SESSION_ARCHIVE_SESSION_PVC_SIZE"`
	ObjectStorageSecret             string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_SECRET"`
	ObjectStorageEndpoint           string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_ENDPOINT"`
	ObjectStorageRegion             string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_REGION"`
	ObjectStorageBucket             string        `env:"SESSION_ARCHIVE_OBJECT_STORAGE_BUCKET"`
	ObjectStorageAllowInsecureLocal bool          `env:"SESSION_ARCHIVE_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL"`
	StartupTimeout                  time.Duration `env:"SESSION_ARCHIVE_STARTUP_TIMEOUT"`
	ShutdownTimeout                 time.Duration `env:"SESSION_ARCHIVE_SHUTDOWN_TIMEOUT"`
	RPCDeadline                     time.Duration `env:"SESSION_ARCHIVE_RPC_DEADLINE"`
	PollInterval                    time.Duration `env:"SESSION_ARCHIVE_POLL_INTERVAL"`
	ReadinessInterval               time.Duration `env:"SESSION_ARCHIVE_READINESS_INTERVAL"`
	WorkerTimeout                   time.Duration `env:"SESSION_ARCHIVE_WORKER_TIMEOUT"`
}

func loadConfig() (Config, error) {
	value := Config{Namespace: "kodex-system", TechnicalListen: ":9090", ControlPlaneTarget: "control-plane.kodex-system.svc:8443",
		ControlPlaneTLSServerName:   "control-plane.kodex-system.svc.cluster.local",
		ControlPlaneCAFile:          "/var/run/config/kodex/session-archive/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/session-archive/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/session-archive/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/session-archive/application-grant/application-grant.jws",
		WorkerServiceAccount:        "session-archive-worker", SessionPVCSize: "20Gi", ObjectStorageSecret: "session-archive-object-storage",
		ObjectStorageRegion: "us-east-1", ObjectStorageBucket: "kodex-session-archives",
		StartupTimeout: 45 * time.Second, ShutdownTimeout: 30 * time.Second, RPCDeadline: 5 * time.Second,
		PollInterval: time.Second, ReadinessInterval: 10 * time.Second, WorkerTimeout: 8 * time.Minute}
	if err := env.Parse(&value); err != nil {
		return Config{}, errors.New("parse session archive environment")
	}
	if err := value.validate(); err != nil {
		return Config{}, err
	}
	return value, nil
}

func (value Config) validate() error {
	if value.Environment != "staging" && value.Environment != "production" {
		return errors.New("session archive environment is invalid")
	}
	for _, endpoint := range []string{value.TechnicalListen, value.ControlPlaneTarget} {
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return errors.New("session archive endpoint is invalid")
		}
	}
	for _, path := range []string{value.ControlPlaneCAFile, value.ControlPlaneCertificateFile, value.ControlPlanePrivateKeyFile, value.ApplicationGrantFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("session archive credential path is invalid")
		}
	}
	if value.Namespace == "" || value.InstanceID == "" || value.WorkerImage == "" || value.WorkerServiceAccount == "" || value.SessionPVCSize == "" || value.ObjectStorageSecret == "" ||
		value.ObjectStorageEndpoint == "" || value.ObjectStorageRegion == "" || value.ObjectStorageBucket == "" ||
		!validObjectStorageBoundary(value) || strings.ContainsAny(value.ControlPlaneTLSServerName, "*/") ||
		value.Environment == "production" && value.ObjectStorageAllowInsecureLocal ||
		value.RPCDeadline < time.Second || value.RPCDeadline > 15*time.Second || value.PollInterval < 250*time.Millisecond ||
		value.ReadinessInterval < time.Second || value.ReadinessInterval > time.Minute ||
		value.WorkerTimeout < time.Minute || value.WorkerTimeout > 15*time.Minute {
		return errors.New("session archive lifecycle configuration is invalid")
	}
	return nil
}

func validObjectStorageBoundary(config Config) bool {
	endpoint, err := url.Parse(config.ObjectStorageEndpoint)
	if err != nil || endpoint == nil {
		return false
	}
	localInsecure := config.ObjectStorageAllowInsecureLocal && endpoint.Scheme == "http" &&
		endpoint.Hostname() == "seaweedfs-s3.kodex-system.svc.cluster.local" &&
		endpoint.Port() == "8333"
	return (endpoint.Scheme == "https" || localInsecure) && endpoint.Host != "" &&
		endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == "" &&
		(endpoint.Path == "" || endpoint.Path == "/")
}
