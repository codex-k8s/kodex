import { describe, expect, it } from "vitest";

import { resolveShellRealtimeState } from "@/app/realtime-presentation";

describe("resolveShellRealtimeState", () => {
  it("показывает начальную загрузку до запуска общего transport", () => {
    expect(
      resolveShellRealtimeState({
        online: true,
        started: false,
        streamState: "offline",
      }),
    ).toBe("initial-loading");
  });

  it("отражает live и reconnect без сброса route state", () => {
    expect(
      resolveShellRealtimeState({
        online: true,
        started: true,
        streamState: "live",
      }),
    ).toBe("live");
    expect(
      resolveShellRealtimeState({
        online: true,
        started: true,
        streamState: "recovering",
      }),
    ).toBe("reconnecting");
  });

  it("считает browser offline авторитетнее состояния socket", () => {
    expect(
      resolveShellRealtimeState({
        online: false,
        started: true,
        streamState: "live",
      }),
    ).toBe("offline");
  });
});
