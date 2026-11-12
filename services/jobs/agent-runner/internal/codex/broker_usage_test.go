package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestBrokerRejectsMalformedPartialMeasurements requires invalid failure kinds, usage, tool batches
// and trailing JSON to return no partial result, rather than laundering measurements through a bad envelope.
// TestBrokerRejectsMalformedPartialMeasurements требует пустого результата при неверном виде отказа,
// расходе, пакете инструментов и лишнем JSON вместо переноса измерений через недопустимый конверт.
func TestBrokerRejectsMalformedPartialMeasurements(t *testing.T) {
	for _, mode := range []string{"unknown failure", "invalid usage", "invalid call", "too many calls", "trailing data"} {
		t.Run(mode, func(t *testing.T) {
			response := brokerResponse{Failure: providerBrokerFailureProvider, Result: Result{Usage: runtimecontract.TokenUsage{TotalTokens: 2, InputTokens: 1, OutputTokens: 1}}}
			switch mode {
			case "unknown failure":
				response.Failure = "FUTURE"
			case "invalid usage":
				response.Result.Usage.TotalTokens = -1
			case "invalid call":
				response.Result.ToolCalls = []runtimecontract.NativeToolCall{{CallID: "invalid"}}
			case "too many calls":
				response.Result.ToolCalls = make([]runtimecontract.NativeToolCall, runtimecontract.MaximumNativeToolCalls+1)
			}
			encoded, _ := json.Marshal(brokerFrame{Version: providerBrokerVersion, Sequence: 1, Kind: brokerFrameTerminal, Terminal: &response})
			if mode == "trailing data" {
				encoded = append(encoded, []byte(" {}")...)
			}
			result, err := readProviderBrokerResponse(bytes.NewReader(encoded))
			if err == nil || !reflect.DeepEqual(result, Result{}) {
				t.Fatal("invalid broker envelope produced partial measurements")
			}
		})
	}
}

// deadlineRecordingConnection records read deadlines under a mutex while delegating all transport
// operations to the embedded connection; it observes cancellation grace without replacing real pipe I/O.
// deadlineRecordingConnection записывает сроки чтения под mutex, передавая транспорт встроенному
// соединению; это позволяет проверить период завершения при отмене без подмены ввода-вывода pipe.
type deadlineRecordingConnection struct {
	net.Conn
	mu       sync.Mutex
	deadline time.Time
}

// SetReadDeadline records the requested deadline before forwarding it and returning the real transport error.
// SetReadDeadline запоминает запрошенный срок до передачи соединению и возвращает настоящую транспортную ошибку.
func (connection *deadlineRecordingConnection) SetReadDeadline(deadline time.Time) error {
	connection.mu.Lock()
	connection.deadline = deadline
	connection.mu.Unlock()
	return connection.Conn.SetReadDeadline(deadline)
}

// TestCancelledBrokerConnectionReceivesBoundedFinalMeasurements joins cancellation handling, verifies
// its finite read grace and receives measured failure usage through the still-open net.Pipe.
// TestCancelledBrokerConnectionReceivesBoundedFinalMeasurements ждёт завершения обработчика отмены,
// проверяет ограниченный срок чтения и получает измеренный расход при отказе через открытый net.Pipe.
func TestCancelledBrokerConnectionReceivesBoundedFinalMeasurements(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	connection := &deadlineRecordingConnection{Conn: client}
	ctx, cancel := context.WithCancel(t.Context())
	stop := bindBrokerConnectionContext(ctx, connection)
	cancel()
	// Ждём callback отмены через тот же обязательный join, без sleep.
	stop()
	connection.mu.Lock()
	remaining := time.Until(connection.deadline)
	connection.mu.Unlock()
	if remaining <= 0 || remaining > providerResultDeliveryGrace {
		t.Fatal("cancelled broker has no bounded result delivery budget")
	}
	done := make(chan error, 1)
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	go func() {
		err := writeProviderBrokerResultFailure(server, Result{Usage: usage}, errors.New("synthetic cancellation"))
		_ = server.Close()
		done <- err
	}()
	result, err := readProviderBrokerResponse(connection)
	if writeErr := <-done; writeErr != nil || err == nil || result.Usage != usage {
		t.Fatal("cancellation discarded already measured usage")
	}
}
