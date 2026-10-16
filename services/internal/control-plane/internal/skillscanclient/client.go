// Package skillscanclient передаёт содержимое локальному CP-owned clamd без файловых путей.
package skillscanclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/skillpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

var errProtocol = errors.New("skill malware scanner protocol failed")

type Client struct {
	socket  string
	timeout time.Duration
}

func New(socket string, timeout time.Duration) (*Client, error) {
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || strings.ContainsAny(socket, "\x00\n\r") || timeout <= 0 || timeout > time.Minute {
		return nil, errors.New("invalid skill scanner configuration")
	}
	return &Client{socket: socket, timeout: timeout}, nil
}

func (client *Client) connect(ctx context.Context) (net.Conn, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, client.timeout)
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", client.socket)
	if err != nil {
		cancel()
		return nil, func() {}, errProtocol
	}
	deadline, _ := ctx.Deadline()
	if connection.SetDeadline(deadline) != nil {
		_ = connection.Close()
		cancel()
		return nil, func() {}, errProtocol
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	return connection, func() { stop(); _ = connection.Close(); cancel() }, nil
}

func readReply(connection net.Conn) (string, error) {
	reader := bufio.NewReader(io.LimitReader(connection, 4097))
	reply, err := reader.ReadString(0)
	if err != nil || len(reply) > 4096 || strings.ContainsAny(reply[:len(reply)-1], "\r\n") {
		return "", errProtocol
	}
	return strings.TrimSuffix(reply, "\x00"), nil
}

func (client *Client) version(ctx context.Context) (string, error) {
	connection, closeConnection, err := client.connect(ctx)
	if err != nil {
		return "", err
	}
	defer closeConnection()
	if _, err := io.WriteString(connection, "zVERSION\x00"); err != nil {
		return "", errProtocol
	}
	reply, err := readReply(connection)
	if err != nil || !strings.HasPrefix(reply, "ClamAV ") {
		return "", errProtocol
	}
	parts := strings.SplitN(reply, "/", 3)
	if len(parts) != 3 || parts[1] == "" {
		return "", errProtocol
	}
	if revision, err := strconv.ParseUint(parts[1], 10, 64); err != nil || revision == 0 {
		return "", errProtocol
	}
	date, err := time.Parse("Mon Jan _2 15:04:05 2006", parts[2])
	if err != nil || time.Since(date) > 7*24*time.Hour || time.Until(date) > 24*time.Hour {
		return "", errProtocol
	}
	return reply, nil
}

func (client *Client) Scan(ctx context.Context, body []byte) (skillpolicy.ScanVerdict, error) {
	if len(body) > 32<<20 {
		return skillpolicy.ScanVerdict{}, errProtocol
	}
	report, err := client.ScanReader(ctx, bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return skillpolicy.ScanVerdict{}, err
	}
	return skillpolicy.ScanVerdict{Engine: report.Engine, Infected: report.Infected}, nil
}

// ScanReader не загружает большой artifact в память и не считает усечённый
// поток успешным. Один общий deadline охватывает VERSION/INSTREAM/VERSION.
func (client *Client) ScanReader(ctx context.Context, body io.Reader, sizeBytes int64) (value.ArtifactScan, error) {
	ctx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	if body == nil || sizeBytes < 0 || sizeBytes > 512<<20 {
		return value.ArtifactScan{}, errProtocol
	}
	before, err := client.version(ctx)
	if err != nil {
		return value.ArtifactScan{}, err
	}
	connection, closeConnection, err := client.connect(ctx)
	if err != nil {
		return value.ArtifactScan{}, err
	}
	defer closeConnection()
	if _, err := io.WriteString(connection, "zINSTREAM\x00"); err != nil {
		return value.ArtifactScan{}, errProtocol
	}
	digest := sha256.New()
	var chunk [32768]byte
	for remaining := sizeBytes; remaining > 0; {
		if ctx.Err() != nil {
			return value.ArtifactScan{}, errProtocol
		}
		length := min(int64(len(chunk)), remaining)
		if _, err := io.ReadFull(body, chunk[:length]); err != nil {
			return value.ArtifactScan{}, errProtocol
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(length))
		if n, err := connection.Write(size[:]); err != nil || n != len(size) {
			return value.ArtifactScan{}, errProtocol
		}
		if n, err := connection.Write(chunk[:length]); err != nil || int64(n) != length {
			return value.ArtifactScan{}, errProtocol
		}
		_, _ = digest.Write(chunk[:length])
		remaining -= length
	}
	var extra [1]byte
	if n, err := io.ReadFull(body, extra[:]); n != 0 || err != io.EOF {
		return value.ArtifactScan{}, errProtocol
	}
	if n, err := connection.Write([]byte{0, 0, 0, 0}); err != nil || n != 4 {
		return value.ArtifactScan{}, errProtocol
	}
	reply, err := readReply(connection)
	if err != nil {
		return value.ArtifactScan{}, err
	}
	infected := strings.HasPrefix(reply, "stream: ") && strings.HasSuffix(reply, " FOUND")
	if reply != "stream: OK" && !infected {
		return value.ArtifactScan{}, errProtocol
	}
	after, err := client.version(ctx)
	if err != nil || before != after {
		return value.ArtifactScan{}, errProtocol
	}
	return value.ArtifactScan{Engine: before, Infected: infected, Policy: value.ArtifactAntivirusPolicy,
		Digest: "sha256:" + hex.EncodeToString(digest.Sum(nil)), SizeBytes: sizeBytes, ScannedAt: time.Now().UTC()}, nil
}
