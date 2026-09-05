// Package skillscanclient передаёт содержимое локальному CP-owned clamd без файловых путей.
package skillscanclient

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/skillpolicy"
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
	ctx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	if len(body) > 32<<20 {
		return skillpolicy.ScanVerdict{}, errProtocol
	}
	before, err := client.version(ctx)
	if err != nil {
		return skillpolicy.ScanVerdict{}, err
	}
	connection, closeConnection, err := client.connect(ctx)
	if err != nil {
		return skillpolicy.ScanVerdict{}, err
	}
	defer closeConnection()
	if _, err := io.WriteString(connection, "zINSTREAM\x00"); err != nil {
		return skillpolicy.ScanVerdict{}, errProtocol
	}
	for offset := 0; offset < len(body); {
		end := offset + 32768
		if end > len(body) {
			end = len(body)
		}
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(end-offset))
		if _, err := connection.Write(size[:]); err != nil {
			return skillpolicy.ScanVerdict{}, errProtocol
		}
		if _, err := connection.Write(body[offset:end]); err != nil {
			return skillpolicy.ScanVerdict{}, errProtocol
		}
		offset = end
	}
	if _, err := connection.Write([]byte{0, 0, 0, 0}); err != nil {
		return skillpolicy.ScanVerdict{}, errProtocol
	}
	reply, err := readReply(connection)
	if err != nil {
		return skillpolicy.ScanVerdict{}, err
	}
	infected := strings.HasPrefix(reply, "stream: ") && strings.HasSuffix(reply, " FOUND")
	if reply != "stream: OK" && !infected {
		return skillpolicy.ScanVerdict{}, errProtocol
	}
	after, err := client.version(ctx)
	if err != nil || before != after {
		return skillpolicy.ScanVerdict{}, errProtocol
	}
	return skillpolicy.ScanVerdict{Engine: before, Infected: infected}, nil
}
