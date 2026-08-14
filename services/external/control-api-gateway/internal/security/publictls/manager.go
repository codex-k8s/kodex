package publictls

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	maximumTLSFile             = 1 << 20
	idempotencyProtocolVersion = "v2"
)

type ControlPlane interface {
	PrepareGatewayPublicTLS(context.Context, *controlplanev1.PrepareGatewayPublicTLSRequest, ...grpc.CallOption) (*controlplanev1.PrepareGatewayPublicTLSResponse, error)
	ConfirmGatewayPublicTLS(context.Context, *controlplanev1.ConfirmGatewayPublicTLSRequest, ...grpc.CallOption) (*controlplanev1.ConfirmGatewayPublicTLSResponse, error)
	CheckGatewayPublicTLS(context.Context, *controlplanev1.CheckGatewayPublicTLSRequest, ...grpc.CallOption) (*controlplanev1.CheckGatewayPublicTLSResponse, error)
}

type Config struct {
	CertificateFile string
	PrivateKeyFile  string
	CAFile          string
	MaterialFile    string
	ServerName      string
}

type metadata struct {
	Generation                   uint64 `json:"generation"`
	CertificateSHA256            string `json:"certificateSha256"`
	PredecessorGeneration        uint64 `json:"predecessorGeneration"`
	PredecessorCertificateSHA256 string `json:"predecessorCertificateSha256"`
}

type Manager struct {
	config            Config
	certificate       tls.Certificate
	roots             *x509.CertPool
	generation        uint64
	predecessor       uint64
	predecessorSHA256 string
	certificateSHA256 string
	notBefore         time.Time
	notAfter          time.Time
	needsConfirm      bool
}

func New(config Config) (*Manager, error) {
	paths := []string{config.CertificateFile, config.PrivateKeyFile, config.CAFile, config.MaterialFile}
	materialDirectory := filepath.Dir(paths[0])
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, errors.New("public TLS path is invalid")
		}
		if filepath.Dir(path) != materialDirectory {
			return nil, errors.New("public TLS material is not atomic")
		}
	}
	dataLink := filepath.Join(materialDirectory, "..data")
	linkInfo, err := os.Lstat(dataLink)
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		return nil, errors.New("public TLS material revision is unavailable")
	}
	materialRevision, err := filepath.EvalSymlinks(dataLink)
	if err != nil || filepath.Dir(materialRevision) != materialDirectory {
		return nil, errors.New("public TLS material revision is invalid")
	}
	for index, path := range paths {
		paths[index] = filepath.Join(materialRevision, filepath.Base(path))
		path = paths[index]
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumTLSFile || info.Mode().Perm()&0o007 != 0 {
			return nil, errors.New("public TLS file is unsafe")
		}
	}
	if config.ServerName == "" || net.ParseIP(config.ServerName) != nil {
		return nil, errors.New("public TLS server name is invalid")
	}
	certificate, err := tls.LoadX509KeyPair(paths[0], paths[1])
	if err != nil || len(certificate.Certificate) == 0 {
		return nil, errors.New("load public TLS identity")
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	now := time.Now().UTC()
	if err != nil || leaf.VerifyHostname(config.ServerName) != nil || now.Before(leaf.NotBefore) || !now.Add(5*time.Minute).Before(leaf.NotAfter) {
		return nil, errors.New("public TLS identity is invalid or near expiry")
	}
	certificate.Leaf = leaf
	caRaw, err := os.ReadFile(paths[2])
	if err != nil {
		return nil, errors.New("read public TLS CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse public TLS CA")
	}
	stateRaw, err := os.ReadFile(paths[3])
	if err != nil {
		return nil, errors.New("read public TLS state")
	}
	decoder := json.NewDecoder(bytes.NewReader(stateRaw))
	decoder.DisallowUnknownFields()
	var state metadata
	if decoder.Decode(&state) != nil || decoder.Decode(&struct{}{}) != io.EOF || state.Generation == 0 || !validSHA256(state.CertificateSHA256) ||
		(state.Generation == 1 && (state.PredecessorGeneration != 0 || state.PredecessorCertificateSHA256 != "")) ||
		(state.Generation > 1 && (state.PredecessorGeneration+1 != state.Generation || !validSHA256(state.PredecessorCertificateSHA256))) {
		return nil, errors.New("public TLS state is invalid")
	}
	digest := sha256.Sum256(leaf.Raw)
	if state.CertificateSHA256 != hex.EncodeToString(digest[:]) {
		return nil, errors.New("public TLS material digest mismatch")
	}
	return &Manager{config: config, certificate: certificate, roots: roots, generation: state.Generation, predecessor: state.PredecessorGeneration, predecessorSHA256: state.PredecessorCertificateSHA256, certificateSHA256: hex.EncodeToString(digest[:]), notBefore: leaf.NotBefore.UTC(), notAfter: leaf.NotAfter.UTC()}, nil
}

func (manager *Manager) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{manager.certificate}}
}

func (manager *Manager) Prepare(ctx context.Context, control ControlPlane) error {
	if manager == nil || control == nil {
		return errors.New("public TLS admission is unavailable")
	}
	key := manager.idempotencyKey("prepare")
	response, err := control.PrepareGatewayPublicTLS(ctx, &controlplanev1.PrepareGatewayPublicTLSRequest{
		IdempotencyKey: key.String(), Generation: manager.generation, CertificateSha256: manager.certificateSHA256,
		PredecessorGeneration: manager.predecessor, PredecessorCertificateSha256: manager.predecessorSHA256,
		NotBefore: timestamppb.New(manager.notBefore), NotAfter: timestamppb.New(manager.notAfter),
	})
	if err != nil {
		return err
	}
	state := response.GetState()
	if !manager.stateContainsExact(state) {
		return errors.New("public TLS authoritative readback mismatch")
	}
	manager.needsConfirm = manager.materialMatches(state.GetPending())
	return nil
}

func (manager *Manager) Confirm(ctx context.Context, control ControlPlane) error {
	if manager == nil || control == nil {
		return errors.New("public TLS confirmation is unavailable")
	}
	if !manager.needsConfirm {
		return manager.Check(ctx, control)
	}
	response, err := control.ConfirmGatewayPublicTLS(ctx, &controlplanev1.ConfirmGatewayPublicTLSRequest{
		IdempotencyKey: manager.idempotencyKey("confirm").String(),
		Generation:     manager.generation, CertificateSha256: manager.certificateSHA256,
	})
	if err != nil {
		return err
	}
	if !manager.materialMatches(response.GetState().GetApplied()) {
		return errors.New("public TLS confirmation readback mismatch")
	}
	manager.needsConfirm = false
	return nil
}

func (manager *Manager) Check(ctx context.Context, control ControlPlane) error {
	if manager == nil || control == nil {
		return errors.New("public TLS readback is unavailable")
	}
	response, err := control.CheckGatewayPublicTLS(ctx, &controlplanev1.CheckGatewayPublicTLSRequest{
		Generation: manager.generation, CertificateSha256: manager.certificateSHA256,
	})
	if err != nil {
		return err
	}
	if !manager.stateContainsExact(response.GetState()) {
		return errors.New("public TLS served-state readback mismatch")
	}
	return nil
}

func (manager *Manager) idempotencyKey(operation string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("control-api-gateway-public-tls:"+idempotencyProtocolVersion+":"+operation+":"+strconv.FormatUint(manager.generation, 10)+":"+manager.certificateSHA256))
}

func (manager *Manager) stateContainsExact(state *controlplanev1.GatewayPublicTLSState) bool {
	return state != nil && (manager.materialMatches(state.GetApplied()) ||
		manager.materialMatches(state.GetPending()) || manager.materialMatches(state.GetPrevious()))
}

func (manager *Manager) materialMatches(material *controlplanev1.GatewayPublicTLSMaterial) bool {
	return material != nil && material.GetGeneration() == manager.generation &&
		material.GetCertificateSha256() == manager.certificateSHA256 &&
		material.GetNotBefore() != nil && material.GetNotAfter() != nil &&
		material.GetNotBefore().AsTime().Equal(manager.notBefore) &&
		material.GetNotAfter().AsTime().Equal(manager.notAfter)
}

func (manager *Manager) VerifyServed(ctx context.Context, listenAddress string) error {
	_, port, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return errors.New("public TLS listen address is invalid")
	}
	dialer := &tls.Dialer{Config: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: manager.config.ServerName, RootCAs: manager.roots}}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
	if err != nil {
		return errors.New("public TLS served path is unavailable")
	}
	defer connection.Close()
	tlsConnection, ok := connection.(*tls.Conn)
	if !ok || len(tlsConnection.ConnectionState().PeerCertificates) == 0 {
		return errors.New("public TLS peer is unavailable")
	}
	leaf := tlsConnection.ConnectionState().PeerCertificates[0]
	digest := sha256.Sum256(leaf.Raw)
	now := time.Now().UTC()
	if hex.EncodeToString(digest[:]) != manager.certificateSHA256 || now.Before(leaf.NotBefore) || !now.Add(5*time.Minute).Before(leaf.NotAfter) {
		return errors.New("public TLS served state mismatch")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
