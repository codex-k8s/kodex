package snapshot

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

const maxResponseBytes = 2 << 20

// Config задаёт exact Kubernetes API и заранее созданный Secret.
type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	TokenFile     string
	Namespace     string
	SecretName    string
	Timeout       time.Duration
}

// Delivery реализует atomic resourceVersion CAS и served readback.
type Delivery struct {
	config      Config
	client      *http.Client
	resourceURL string
}

type secretEnvelope struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name            string            `json:"name"`
		Namespace       string            `json:"namespace"`
		UID             string            `json:"uid,omitempty"`
		ResourceVersion string            `json:"resourceVersion"`
		Annotations     map[string]string `json:"annotations"`
	} `json:"metadata"`
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

// New создаёт fail-closed клиент только для канонического snapshot Secret.
func New(config Config) (*Delivery, error) {
	if config.Address != "https://kubernetes.default.svc:443" ||
		config.TLSServerName != "kubernetes.default.svc" ||
		config.Namespace != "mattercodex-system" ||
		config.SecretName != "internal-rpc-authority-snapshot" ||
		config.Timeout < time.Second ||
		config.Timeout > 10*time.Second {
		return nil, errors.New("authority snapshot Kubernetes configuration is invalid")
	}
	caRaw, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, errors.New("read authority snapshot Kubernetes CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("authority snapshot Kubernetes CA is invalid")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs:    roots,
			ServerName: config.TLSServerName,
		},
		ForceAttemptHTTP2: true,
	}
	return &Delivery{
		config: config,
		client: &http.Client{
			Transport: transport,
			Timeout:   config.Timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("kubernetes API redirect is forbidden")
			},
		},
		resourceURL: config.Address + "/api/v1/namespaces/" +
			url.PathEscape(config.Namespace) + "/secrets/" +
			url.PathEscape(config.SecretName),
	}, nil
}

// Close закрывает простаивающие соединения Kubernetes API.
func (delivery *Delivery) Close() {
	delivery.client.CloseIdleConnections()
}

// Publish обновляет existing Secret по resourceVersion и читает результат.
func (delivery *Delivery) Publish(
	ctx context.Context,
	publication model.AuthoritySnapshotPublication,
) (model.AuthoritySnapshotPublication, error) {
	if publication.SourceRevision == 0 ||
		!validDigest(publication.SourceDigestSHA256) ||
		publication.SnapshotCompactJWS == "" ||
		len(publication.SnapshotCompactJWS) > internalrpcauth.MaxCompactJWSBytes {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority snapshot publication is invalid",
		)
	}
	for attempt := 0; attempt < 4; attempt++ {
		current, err := delivery.readEnvelope(ctx)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, err
		}
		currentRevision, _ := strconv.ParseUint(
			current.Metadata.Annotations["mattercodex.dev/source-revision"],
			10,
			64,
		)
		currentDigest := current.Metadata.Annotations["mattercodex.dev/source-digest-sha256"]
		if currentRevision > publication.SourceRevision ||
			currentRevision == publication.SourceRevision &&
				currentDigest != "" &&
				currentDigest != publication.SourceDigestSHA256 {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"authority snapshot rollback or same-revision mutation rejected",
			)
		}
		if currentRevision == publication.SourceRevision &&
			currentDigest == publication.SourceDigestSHA256 {
			return delivery.publication(current, publication)
		}
		current.Metadata.Annotations = map[string]string{
			"mattercodex.dev/source-revision": strconv.FormatUint(
				publication.SourceRevision,
				10,
			),
			"mattercodex.dev/source-digest-sha256": publication.SourceDigestSHA256,
			"mattercodex.dev/signer-generation": strconv.FormatUint(
				publication.SignerGeneration,
				10,
			),
		}
		current.Type = "Opaque"
		current.Data = map[string]string{
			"snapshot.jws": base64.StdEncoding.EncodeToString(
				[]byte(publication.SnapshotCompactJWS),
			),
		}
		body, err := json.Marshal(current)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"encode authority snapshot Secret",
			)
		}
		response, raw, err := delivery.do(ctx, http.MethodPut, body)
		if err != nil {
			return model.AuthoritySnapshotPublication{}, err
		}
		if response.StatusCode == http.StatusConflict {
			continue
		}
		if response.StatusCode != http.StatusOK {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"authority snapshot Secret CAS rejected",
			)
		}
		var served secretEnvelope
		if err := json.Unmarshal(raw, &served); err != nil {
			return model.AuthoritySnapshotPublication{}, errors.New(
				"decode authority snapshot Secret CAS response",
			)
		}
		return delivery.publication(served, publication)
	}
	return model.AuthoritySnapshotPublication{}, errors.New(
		"authority snapshot Secret CAS retries exhausted",
	)
}

// Read возвращает только cryptographically bound served state.
func (delivery *Delivery) Read(
	ctx context.Context,
) (model.AuthoritySnapshotPublication, error) {
	envelope, err := delivery.readEnvelope(ctx)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, err
	}
	revision, err := strconv.ParseUint(
		envelope.Metadata.Annotations["mattercodex.dev/source-revision"],
		10,
		64,
	)
	if err != nil {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"served authority snapshot revision rejected",
		)
	}
	digest := envelope.Metadata.Annotations["mattercodex.dev/source-digest-sha256"]
	encoded, ok := envelope.Data["snapshot.jws"]
	if !ok || len(envelope.Data) != 1 || !validDigest(digest) {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"served authority snapshot data rejected",
		)
	}
	compactRaw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil ||
		len(compactRaw) == 0 ||
		len(compactRaw) > internalrpcauth.MaxCompactJWSBytes {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"served authority snapshot compact JWS rejected",
		)
	}
	return model.AuthoritySnapshotPublication{
		SourceRevision:          revision,
		SourceDigestSHA256:      digest,
		SnapshotCompactJWS:      string(compactRaw),
		SnapshotResourceVersion: envelope.Metadata.ResourceVersion,
	}, nil
}

func (delivery *Delivery) readEnvelope(
	ctx context.Context,
) (secretEnvelope, error) {
	response, raw, err := delivery.do(ctx, http.MethodGet, nil)
	if err != nil {
		return secretEnvelope{}, err
	}
	if response.StatusCode != http.StatusOK {
		return secretEnvelope{}, errors.New("read authority snapshot Secret rejected")
	}
	var envelope secretEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil ||
		envelope.APIVersion != "v1" ||
		envelope.Kind != "Secret" ||
		envelope.Metadata.Name != delivery.config.SecretName ||
		envelope.Metadata.Namespace != delivery.config.Namespace ||
		envelope.Metadata.UID == "" ||
		envelope.Metadata.ResourceVersion == "" ||
		(envelope.Type != "" && envelope.Type != "Opaque") {
		return secretEnvelope{}, errors.New("authority snapshot Secret identity rejected")
	}
	return envelope, nil
}

func (delivery *Delivery) publication(
	envelope secretEnvelope,
	expected model.AuthoritySnapshotPublication,
) (model.AuthoritySnapshotPublication, error) {
	if envelope.Metadata.Annotations["mattercodex.dev/source-revision"] !=
		strconv.FormatUint(expected.SourceRevision, 10) ||
		envelope.Metadata.Annotations["mattercodex.dev/source-digest-sha256"] !=
			expected.SourceDigestSHA256 ||
		envelope.Metadata.Annotations["mattercodex.dev/signer-generation"] !=
			strconv.FormatUint(expected.SignerGeneration, 10) {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority snapshot Secret annotations rejected",
		)
	}
	encoded := envelope.Data["snapshot.jws"]
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || !bytes.Equal(raw, []byte(expected.SnapshotCompactJWS)) {
		return model.AuthoritySnapshotPublication{}, errors.New(
			"authority snapshot Secret readback mismatch",
		)
	}
	expected.SnapshotResourceVersion = envelope.Metadata.ResourceVersion
	return expected, nil
}

func (delivery *Delivery) do(
	ctx context.Context,
	method string,
	body []byte,
) (*http.Response, []byte, error) {
	tokenRaw, err := os.ReadFile(delivery.config.TokenFile)
	token := strings.TrimSpace(string(tokenRaw))
	if err != nil || token == "" || len(token) > 16<<10 {
		return nil, nil, errors.New("read authority snapshot Kubernetes token")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		delivery.resourceURL,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, nil, errors.New("create authority snapshot Kubernetes request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := delivery.client.Do(request)
	if err != nil {
		return nil, nil, errors.New("perform authority snapshot Kubernetes request")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(raw) > maxResponseBytes {
		return nil, nil, errors.New("read authority snapshot Kubernetes response")
	}
	return response, raw, nil
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}
