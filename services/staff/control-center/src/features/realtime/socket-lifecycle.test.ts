import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  connectingSocketRetirementTimeoutMs,
  retireWebSocket,
} from "./socket-lifecycle";

class FakeSocket extends EventTarget {
  readyState: number = WebSocket.CONNECTING;
  readonly close = vi.fn((code?: number, reason?: string) => {
    void code;
    void reason;
    this.readyState = WebSocket.CLOSING;
  });
}

describe("retireWebSocket", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("window", { setTimeout, clearTimeout });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("закрывает OPEN socket сразу", () => {
    const socket = new FakeSocket();
    socket.readyState = WebSocket.OPEN;
    retireWebSocket(socket as unknown as WebSocket, "STORE_CLOSED");
    expect(socket.close).toHaveBeenCalledWith(1000, "STORE_CLOSED");
    expect(vi.getTimerCount()).toBe(0);
  });

  it("не закрывает CONNECTING до open и затем завершает его без timeout", () => {
    const socket = new FakeSocket();
    retireWebSocket(socket as unknown as WebSocket, "DOCUMENT_SUSPENDED");
    expect(socket.close).not.toHaveBeenCalled();
    socket.readyState = WebSocket.OPEN;
    socket.dispatchEvent(new Event("open"));
    expect(socket.close).toHaveBeenCalledWith(1000, "DOCUMENT_SUSPENDED");
    expect(vi.getTimerCount()).toBe(0);
  });

  it("bounded timeout закрывает handshake, который не открылся", () => {
    const socket = new FakeSocket();
    retireWebSocket(socket as unknown as WebSocket, "SESSION_RENEWED");
    vi.advanceTimersByTime(connectingSocketRetirementTimeoutMs - 1);
    expect(socket.close).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(socket.close).toHaveBeenCalledWith(1000, "SESSION_RENEWED");
    socket.readyState = WebSocket.OPEN;
    socket.dispatchEvent(new Event("open"));
    expect(socket.close).toHaveBeenCalledTimes(1);
  });
});
