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

// errProtocol reports a fixed scanner protocol failure without reflecting scanner replies or
// uploaded content.
// errProtocol обозначает фиксированную ошибку протокола сканирования без отражения ответов сканера
// либо загруженного содержимого.
var errProtocol = errors.New("skill malware scanner protocol failed")

// Client holds the absolute local clamd socket and bounded per-operation timeout; it owns no
// persistent connection or background worker.
// Client хранит абсолютный путь локального clamd socket и ограниченный timeout операции;
// постоянным соединением либо фоновым worker не владеет.
type Client struct {
	socket  string
	timeout time.Duration
}

// New validates a clean absolute socket pathname and positive timeout no greater than one minute
// without connecting to the scanner.
// New проверяет чистый абсолютный socket pathname и положительный timeout не больше минуты без
// подключения к сканеру.
func New(socket string, timeout time.Duration) (*Client, error) {
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket || strings.ContainsAny(socket, "\x00\n\r") || timeout <= 0 || timeout > time.Minute {
		return nil, errors.New("invalid skill scanner configuration")
	}
	return &Client{socket: socket, timeout: timeout}, nil
}

// connect dials the Unix socket under a timeout, sets the connection deadline and closes it on
// cancellation; the returned cleanup releases the callback, connection and context.
// connect подключается к Unix socket под timeout, задаёт deadline соединения и закрывает его при
// отмене; возвращённый cleanup освобождает callback, соединение и контекст.
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

// readReply reads one NUL-terminated record capped at4096 bytes, rejecting transport failure,
// oversized replies and embedded CR/LF.
// readReply читает одну запись с конечным NUL и пределом4096 bytes, отклоняя transport failure,
// слишком длинный ответ и внутренние CR либо LF.
func readReply(connection net.Conn) (string, error) {
	reader := bufio.NewReader(io.LimitReader(connection, 4097))
	reply, err := reader.ReadString(0)
	if err != nil || len(reply) > 4096 || strings.ContainsAny(reply[:len(reply)-1], "\r\n") {
		return "", errProtocol
	}
	return strings.TrimSuffix(reply, "\x00"), nil
}

// version requires a framed ClamAV reply with a positive database revision, no older than seven
// days or more than twenty-four hours ahead; the accepted reply pins both scan observations.
// version требует ответ ClamAV с framing, положительной database revision и датой не старше
// семи дней и не более суток в будущем; принятый ответ закрепляет обе проверки вокруг scan.
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

// Scan keeps the skill byte-slice limit at32 MiB, delegates exact streaming validation and
// projects only engine and infection verdict.
// Scan сохраняет предел32 MiB для skill byte slice, передаёт точную проверку потока и возвращает
// только engine и infection verdict.
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

// ScanReader streams exactly the declared0..512 MiB through bounded INSTREAM frames, requires EOF
// and identical fresh VERSION replies, and reports digest/size under one timeout without granting
// publication authority.
// ScanReader передаёт ровно заявленные0..512 MiB ограниченными INSTREAM frames, требует EOF и
// одинаковые свежие VERSION ответы и возвращает digest и size под одним timeout без выдачи
// полномочий публикации.
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
