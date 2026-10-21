package mailtransport

import (
	"io"
	"net"
	"strings"
	"testing"
)

// healthReadConn supplies a synthetic byte reader behind net.Conn for status-prefix tests, without opening a socket.
// healthReadConn задаёт синтетический reader за net.Conn для проверки префикса статуса без открытия сокета.
type healthReadConn struct {
	net.Conn
	reader io.Reader
}

// Read delegates only the read operation needed by the observer test; other embedded connection methods remain unused.
// Read передаёт только нужное тесту observer чтение; прочие встроенные методы соединения не используются.
func (c healthReadConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// TestPOPRejectionUsesOnlyExactStatusPrefix reads one byte at a time and distinguishes -ERR from -ERROR while requiring prefix cleanup after newline.
// TestPOPRejectionUsesOnlyExactStatusPrefix читает по одному байту и различает -ERR и -ERROR, требуя очистки префикса после перевода строки.
func TestPOPRejectionUsesOnlyExactStatusPrefix(t *testing.T) {
	for _, tc := range []struct {
		line     string
		rejected bool
	}{{"-ERR fixture-private-text\r\n", true}, {"-ERR\r\n", true}, {"-ERROR fixture-private-text\r\n", false}, {"+OK\r\n", false}, {"malformed fixture-private-text\r\n", false}} {
		c := &popReplyObserver{Conn: healthReadConn{reader: strings.NewReader(tc.line)}}
		for {
			b := make([]byte, 1)
			_, err := c.Read(b)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if c.rejected != tc.rejected || c.prefix != [5]byte{} {
			t.Fatal("POP status boundary failed")
		}
	}
}
