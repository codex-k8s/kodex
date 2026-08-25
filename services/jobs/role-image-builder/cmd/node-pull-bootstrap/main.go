package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/jobs/role-image-builder/internal/nodepullidentity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

const (
	configurationError  = "node pull bootstrap configuration invalid"
	vaultClientError    = "node pull Vault client initialization failed"
	vaultTokenError     = "node pull Vault token read failed"
	vaultLoginError     = "node pull Vault authentication failed"
	certificateError    = "node pull certificate issue failed"
	certificateKeyError = "node pull certificate key validation failed"
	trustWriteError     = "node pull runtime trust write failed"
	containerdError     = "node pull containerd readback failed"
	readinessError      = "node pull readiness marker write failed"
)

type config struct{ nodeName, hostIP, generation, registry, image, vaultCA, vaultToken, socket, output string }

func main() {
	registry := os.Getenv("REGISTRY_PULL_HOST")
	configuration := config{nodeName: os.Getenv("NODE_NAME"), hostIP: os.Getenv("HOST_IP"), generation: os.Getenv("PULL_CREDENTIAL_GENERATION"),
		registry: registry, image: os.Getenv("READBACK_IMAGE"), vaultCA: "/vault/ca.pem",
		vaultToken: "/vault-token/token", socket: "/run/containerd/containerd.sock",
		output: filepath.Join("/host/etc/containerd/certs.d", registry)}
	for {
		if err := bootstrap(context.Background(), configuration); err != nil {
			_, _ = os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		time.Sleep(10 * time.Minute)
	}
}

func bootstrap(ctx context.Context, configuration config) error {
	generation, err := strconv.ParseUint(configuration.generation, 10, 64)
	if err != nil || configuration.nodeName == "" || net.ParseIP(configuration.hostIP) == nil || generation == 0 ||
		!validNodeReadbackImage(configuration.registry, configuration.image) {
		return errors.New(configurationError)
	}
	client, err := vaultClient(configuration.vaultCA)
	if err != nil {
		return err
	}
	jwt, err := os.ReadFile(configuration.vaultToken)
	if err != nil || len(jwt) == 0 || len(jwt) > 16<<10 {
		return errors.New(vaultTokenError)
	}
	token, err := vaultLogin(ctx, client, strings.TrimSpace(string(jwt)))
	if err != nil {
		return err
	}
	defer vaultRevoke(context.WithoutCancel(ctx), client, token)
	commonName := nodepullidentity.CommonName(configuration.nodeName, generation)
	certificate, privateKey, ca, err := issueCertificate(ctx, client, token, commonName, configuration.hostIP)
	if err != nil {
		return err
	}
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return errors.New(certificateKeyError)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if rsaKey, fallback := x509.ParsePKCS1PrivateKey(block.Bytes); fallback == nil {
			key = rsaKey
		} else {
			return errors.New(certificateKeyError)
		}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return errors.New(certificateKeyError)
	}
	digest := sha256.Sum256([]byte(commonName + "\n" + configuration.generation + "\n" + configuration.registry))
	signature, err := rsa.SignPSS(rand.Reader, rsaKey, crypto.SHA256, digest[:], nil)
	if err != nil {
		return errors.New(certificateKeyError)
	}
	password := "v1." + configuration.generation + "." + base64.RawURLEncoding.EncodeToString(signature)
	if err := writeRuntimeTrust(configuration.output, configuration.registry, ca, certificate, privateKey, commonName, password); err != nil {
		return err
	}
	connection, err := grpc.NewClient("unix://"+configuration.socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return errors.New(containerdError)
	}
	defer connection.Close()
	pullCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := runtimeapi.NewImageServiceClient(connection).PullImage(pullCtx, &runtimeapi.PullImageRequest{
		Image: &runtimeapi.ImageSpec{Image: configuration.image}, Auth: &runtimeapi.AuthConfig{Username: commonName, Password: password, ServerAddress: configuration.registry}})
	if err != nil || response.GetImageRef() == "" {
		return errors.New(containerdError)
	}
	if os.WriteFile("/ready/node-pull", []byte(configuration.image+"\n"), 0o400) != nil {
		return errors.New(readinessError)
	}
	return nil
}

func validNodeReadbackImage(registry, image string) bool {
	prefix := registry + "/kodex/agent-runner@sha256:"
	digest := strings.TrimPrefix(image, prefix)
	return strings.Contains(registry, ".") && !strings.ContainsAny(registry, "/:@?# \\\r\n\t") &&
		digest != image && digest != strings.Repeat("0", 64) && len(digest) == 64 &&
		strings.Trim(digest, "0123456789abcdef") == ""
}

func vaultClient(caPath string) (*http.Client, error) {
	ca, err := os.ReadFile(caPath)
	if err != nil {
		return nil, errors.New(vaultClientError)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New(vaultClientError)
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "vault.kodex-system.svc.cluster.local", RootCAs: pool}}}, nil
}

func vaultLogin(ctx context.Context, client *http.Client, jwt string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"role": "kodex-node-pull-bootstrap", "jwt": jwt})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://vault.kodex-system.svc:8200/v1/auth/kubernetes/login", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", errors.New(vaultLoginError)
	}
	defer response.Body.Close()
	var envelope struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&envelope) != nil || envelope.Auth.ClientToken == "" {
		return "", errors.New(vaultLoginError)
	}
	return envelope.Auth.ClientToken, nil
}

func issueCertificate(ctx context.Context, client *http.Client, token, cn, ip string) (string, string, string, error) {
	payload, _ := json.Marshal(map[string]string{"common_name": cn, "ip_sans": ip, "ttl": "30m"})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://vault.kodex-system.svc:8200/v1/pki-node-pull/issue/kodex-node-pull", bytes.NewReader(payload))
	request.Header.Set("X-Vault-Token", token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", "", "", errors.New(certificateError)
	}
	defer response.Body.Close()
	var envelope struct {
		Data struct {
			Certificate string `json:"certificate"`
			PrivateKey  string `json:"private_key"`
			IssuingCA   string `json:"issuing_ca"`
		} `json:"data"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&envelope) != nil || envelope.Data.Certificate == "" || envelope.Data.PrivateKey == "" || envelope.Data.IssuingCA == "" {
		return "", "", "", errors.New(certificateError)
	}
	return envelope.Data.Certificate, envelope.Data.PrivateKey, envelope.Data.IssuingCA, nil
}

func vaultRevoke(ctx context.Context, client *http.Client, token string) {
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://vault.kodex-system.svc:8200/v1/auth/token/revoke-self", bytes.NewReader([]byte("{}")))
	request.Header.Set("X-Vault-Token", token)
	if response, err := client.Do(request); err == nil {
		_ = response.Body.Close()
	}
}

func writeRuntimeTrust(directory, registry, ca, certificate, privateKey, username, password string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errors.New(trustWriteError)
	}
	files := map[string][]byte{"ca.pem": []byte(ca), "client.cert": []byte(certificate), "client.key": []byte(privateKey),
		"hosts.toml": []byte("server = \"https://" + registry + "\"\n[host.\"https://" + registry + "\"]\n  capabilities = [\"pull\", \"resolve\"]\n  ca = \"" + filepath.Join(directory, "ca.pem") + "\"\n  client = [[\"" + filepath.Join(directory, "client.cert") + "\", \"" + filepath.Join(directory, "client.key") + "\"]]\n  [host.\"https://" + registry + "\".header]\n    authorization = \"Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password)) + "\"\n")}
	for name, value := range files {
		temporary := filepath.Join(directory, name+".next")
		if os.WriteFile(temporary, value, 0o600) != nil || os.Rename(temporary, filepath.Join(directory, name)) != nil {
			return errors.New(trustWriteError)
		}
	}
	return nil
}
