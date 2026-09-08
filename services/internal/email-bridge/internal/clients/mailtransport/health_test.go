package mailtransport

import (
	"io"
	"net"
	"strings"
	"testing"
)

type healthReadConn struct {
	net.Conn
	reader io.Reader
}

func (c healthReadConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
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
