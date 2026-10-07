package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestProtocolStreamsPublishedActivitiesImmediatelyAndExactlyOnce(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID, state.workspaceRoot = testThreadID, testTurnID, "/workspace"
	var activities []runtimecontract.RuntimeActivity
	state.onActivity = func(activity runtimecontract.RuntimeActivity) error {
		activities = append(activities, activity)
		return nil
	}
	commentary := raw(`{"id":"commentary-one","text":"Проверяю состояние","phase":"commentary","type":"agentMessage"}`)
	if err := state.consumeItem(commentary, false, 10); err != nil || len(activities) != 0 {
		t.Fatal("started message was published")
	}
	if err := state.consumeItem(commentary, true, 20); err != nil || len(activities) != 1 || activities[0].Message.Phase != runtimecontract.RuntimeMessageCommentary || state.terminals != 0 {
		t.Fatal("commentary did not stream before turn completion")
	}
	started := raw(`{"command":"SECRET_COMMAND","commandActions":[],"cwd":"/workspace","exitCode":null,"id":"native-one","status":"inProgress","type":"commandExecution"}`)
	if err := state.consumeItem(started, false, 30); err != nil {
		t.Fatal(err)
	}
	if len(activities) != 2 || activities[1].ToolCall.State != runtimecontract.NativeToolStateRunning || activities[1].ToolCall.Revision != 1 || activities[1].ToolCall.DurationMS != 0 || activities[1].ToolCall.SafeResult != "" {
		t.Fatal("native start did not stream")
	}
	completed := raw(`{"command":"SECRET_COMMAND","commandActions":[],"cwd":"/workspace","exitCode":0,"id":"native-one","status":"completed","type":"commandExecution"}`)
	if err := state.consumeItem(completed, true, 80); err != nil {
		t.Fatal(err)
	}
	if len(activities) != 3 || activities[2].ToolCall.Revision != 2 || activities[2].ToolCall.State != runtimecontract.NativeToolStateSucceeded || activities[2].ToolCall.DurationMS != 50 {
		t.Fatal("native completion did not stream")
	}
	final := raw(`{"id":"final-one","text":"Готово","phase":"final_answer","type":"agentMessage"}`)
	if err := state.consumeItem(final, true, 90); err != nil || len(activities) != 4 || activities[3].Message.Phase != runtimecontract.RuntimeMessageFinal {
		t.Fatal("final did not stream")
	}
	for _, item := range []json.RawMessage{commentary, completed, final} {
		if err := state.consumeItem(item, true, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(activities) != 4 {
		t.Fatal("authoritative snapshot duplicated activity")
	}
	encoded, _ := json.Marshal(activities)
	if bytes.Contains(encoded, []byte("SECRET_COMMAND")) {
		t.Fatal("native raw command escaped projection")
	}
	if err := state.consumeItem(raw(`{"id":"reasoning-one","summary":["SECRET_REASONING"],"content":[],"type":"reasoning"}`), true, 0); err != nil {
		t.Fatal(err)
	}
	if len(activities) != 4 {
		t.Fatal("reasoning was published")
	}
}

func TestProtocolActivityBoundaryAndStickyDeliveryFailure(t *testing.T) {
	state := newProtocolState(testThreadID)
	calls := 0
	state.onActivity = func(runtimecontract.RuntimeActivity) error { calls++; return errors.New("PRIVATE_CALLBACK_DIAGNOSTIC") }
	item := raw(`{"id":"message-one","text":"safe","phase":"commentary","type":"agentMessage"}`)
	if err := state.consumeItem(item, true, 0); err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatal("callback failure was not safely rejected")
	}
	if err := state.publishActivity(runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "message-two", Phase: "COMMENTARY", Revision: 1, Text: "safe"}}); err == nil || calls != 1 {
		t.Fatal("delivery continued after failure")
	}
	state = newProtocolState(testThreadID)
	state.onActivity = func(runtimecontract.RuntimeActivity) error { calls++; return nil }
	large, _ := json.Marshal(map[string]any{"id": "large", "text": strings.Repeat("x", runtimecontract.MaximumRuntimeMessageBytes+1), "phase": "commentary", "type": "agentMessage"})
	if err := state.consumeItem(large, true, 0); err == nil {
		t.Fatal("oversized public message accepted")
	}
	if err := state.consumeItem(raw(`{"id":"missing-phase","text":"safe","type":"agentMessage"}`), true, 0); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("unknown phase was guessed")
	}
}

func TestBrokerStreamDeliversActivityBeforeExactTerminal(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	observed := make(chan struct{}, 1)
	finish := make(chan struct{})
	written := make(chan error, 1)
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	go func() {
		frames := &brokerFrameWriter{writer: writer}
		err := frames.activity(runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "stream-one", Phase: "COMMENTARY", Revision: 1, Text: "safe"}})
		if err == nil {
			<-finish
			err = frames.finish(brokerResponse{Failure: providerBrokerFailureProvider, Result: Result{Usage: usage}})
		}
		_ = writer.CloseWithError(err)
		written <- err
	}()
	type outcome struct {
		result Result
		err    error
	}
	read := make(chan outcome, 1)
	go func() {
		result, err := readProviderBrokerResponse(reader, func(runtimecontract.RuntimeActivity) error { observed <- struct{}{}; return nil })
		read <- outcome{result, err}
	}()
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("activity buffered until terminal")
	}
	select {
	case <-read:
		t.Fatal("activity invented terminal")
	default:
	}
	close(finish)
	select {
	case got := <-read:
		if got.err == nil || got.result.Usage != usage {
			t.Fatal("terminal lost measured failure usage")
		}
	case <-time.After(time.Second):
		t.Fatal("terminal did not complete")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestBrokerStreamRejectsLegacyMalformedAndReorderedFrames(t *testing.T) {
	message := `{"message":{"item_id":"message-one","phase":"COMMENTARY","revision":1,"text":"safe"}}`
	activity := `{"version":1,"sequence":1,"kind":"ACTIVITY","activity":` + message + `}\n`
	terminal := `{"version":1,"sequence":2,"kind":"TERMINAL","terminal":{"result":{},"failure":"PROVIDER","ok":false}}` + "\n"
	for name, payload := range map[string]string{
		"legacy":            `{"result":{},"failure":"PROVIDER","ok":false}`,
		"wrong-version":     strings.ReplaceAll(activity, `"version":1`, `"version":2`),
		"wrong-sequence":    strings.ReplaceAll(activity, `"sequence":1`, `"sequence":2`),
		"unknown-field":     strings.ReplaceAll(activity, `"version":1`, `"unknown":true,"version":1`),
		"duplicate-field":   strings.ReplaceAll(activity, `"revision":1`, `"revision":1,"revision":1`),
		"missing-terminal":  activity,
		"double-terminal":   activity + terminal + terminal,
		"after-terminal":    activity + terminal + activity,
		"duplicate-message": activity + strings.ReplaceAll(activity, `"sequence":1`, `"sequence":2`) + strings.ReplaceAll(terminal, `"sequence":2`, `"sequence":3`),
		"oversized":         strings.Repeat("x", maximumBrokerFrameBytes+1),
		"terminal-batch":    strings.ReplaceAll(terminal, `"result":{}`, `"result":{"ToolCalls":[]}`),
		"terminal-null":     strings.ReplaceAll(terminal, `"result":{}`, `"result":{"ToolCalls":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			payload = strings.ReplaceAll(payload, `\n`, "\n")
			result, err := readProviderBrokerResponse(strings.NewReader(payload))
			if err == nil || result.Outcome != "" {
				t.Fatal("malformed broker stream accepted")
			}
		})
	}
}

func TestExecuteViaBrokerRequiresLiveActivityConsumer(t *testing.T) {
	if _, err := ExecuteViaBroker(context.Background(), model.Input{}, nil, "", "", nil); err == nil {
		t.Fatal("missing activity consumer accepted")
	}
}

func TestBrokerUDSCancelInterruptsPeerAndPreservesReadGrace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stop := bindBrokerConnectionContext(ctx, client)
	defer stop()
	peerCtx, join := bindBrokerPeerContext(t.Context(), server, bufio.NewScanner(server))
	defer join()
	select {
	case <-peerCtx.Done():
		t.Fatal("request was prematurely sealed")
	default:
	}
	cancel()
	select {
	case <-peerCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("runner cancellation did not interrupt isolated provider")
	}
	written := make(chan error, 1)
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	go func() {
		err := writeProviderBrokerResultFailure(server, Result{Usage: usage}, context.Canceled)
		_ = server.Close()
		written <- err
	}()
	result, err := readProviderBrokerResponse(client)
	if err == nil || result.Usage != usage {
		t.Fatal("cancel closed the usage delivery path")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestAppServerWaitTerminalStreamsNotificationBeforeTerminal(t *testing.T) {
	state := newProtocolState(testThreadID)
	state.threadID, state.turnID, state.turnStarted = testThreadID, testTurnID, 1
	observed := make(chan struct{}, 1)
	state.onActivity = func(runtimecontract.RuntimeActivity) error { observed <- struct{}{}; return nil }
	events := make(chan streamEvent)
	server := &appServer{messages: events}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.waitTerminal(ctx, state) }()
	events <- streamEvent{message: wireMessage{kind: messageNotification, method: "item/completed", payload: raw(`{"completedAtMs":1,"item":{"id":"final-live","text":"safe","phase":"final_answer","type":"agentMessage"},"threadId":"` + testThreadID + `","turnId":"` + testTurnID + `"}`)}}
	select {
	case <-observed:
	case <-ctx.Done():
		t.Fatal("process notification did not stream")
	}
	select {
	case <-done:
		t.Fatal("message invented terminal")
	default:
	}
	events <- streamEvent{message: wireMessage{kind: messageNotification, method: "turn/completed", payload: raw(`{"threadId":"` + testThreadID + `","turn":{"id":"` + testTurnID + `","items":[],"status":"completed"}}`)}}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestBrokerTerminalDropsPublishedCallsAndPreservesUsageOnOverflow(t *testing.T) {
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	for _, overflow := range []bool{false, true} {
		var output bytes.Buffer
		result := Result{Outcome: "SUCCEEDED", Usage: usage, FinalMessage: "safe", ToolCalls: make([]runtimecontract.NativeToolCall, runtimecontract.MaximumNativeToolCalls)}
		for index := range result.ToolCalls {
			result.ToolCalls[index].SafeParameters = map[string]any{"oversized": strings.Repeat("x", 3000)}
		}
		if overflow {
			result.FinalMessage = strings.Repeat("<", maximumFinalBytes)
		}
		writer := &brokerFrameWriter{writer: &output}
		if err := writer.finish(brokerResponse{OK: true, Result: result}); err != nil {
			t.Fatal(err)
		}
		got, err := readProviderBrokerResponse(&output)
		if got.Usage != usage || len(got.ToolCalls) != 0 || (err != nil) != overflow {
			t.Fatal("terminal duplicated calls or lost measured usage")
		}
	}
}

func TestBrokerActivityReservesTerminalBudget(t *testing.T) {
	for _, frameLimit := range []bool{false, true} {
		var output bytes.Buffer
		writer := &brokerFrameWriter{writer: &output}
		if frameLimit {
			writer.sequence = maximumBrokerFrames - 1
		} else {
			writer.bytes = maximumBrokerStreamBytes - maximumBrokerFrameBytes
		}
		activity := runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "reserved-one", Phase: "COMMENTARY", Revision: 1, Text: "safe"}}
		if writer.activity(activity) == nil || output.Len() != 0 {
			t.Fatal("activity consumed reserved terminal budget")
		}
		if err := writer.finish(brokerResponse{Failure: providerBrokerFailureProvider}); err != nil || !writer.terminal {
			t.Fatal("reserved terminal could not be written")
		}
	}
}

func TestBrokerUDSCallbackFailureCancelsPeerAndRetainsTerminalUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	peerCtx, join := bindBrokerPeerContext(ctx, server, bufio.NewScanner(server))
	defer join()
	usage := runtimecontract.TokenUsage{TotalTokens: 3, InputTokens: 2, OutputTokens: 1}
	written := make(chan error, 1)
	go func() {
		frames := &brokerFrameWriter{writer: server}
		activity := runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "failed-one", Phase: "COMMENTARY", Revision: 1, Text: "safe"}}
		err := frames.activity(activity)
		if err == nil {
			<-peerCtx.Done()
			if ctx.Err() != nil {
				err = errors.New("callback failure did not cancel isolated provider")
			} else {
				activity.Message.ItemID = "suppressed-two"
				err = frames.activity(activity)
			}
		}
		if err == nil {
			err = frames.finish(brokerResponse{Failure: providerBrokerFailureProvider, Result: Result{Usage: usage}})
		}
		_ = server.Close()
		written <- err
	}()
	calls := 0
	result, err := readProviderBrokerResponse(client, func(runtimecontract.RuntimeActivity) error {
		calls++
		return errors.New("synthetic private callback error")
	})
	if err == nil || strings.Contains(err.Error(), "private") || result.Usage != usage || calls != 1 {
		t.Fatal("callback failure lost usage, repeated delivery, or exposed private error")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}
