import { beforeEach, describe, expect, it, vi } from "vitest";
const sdk = vi.hoisted(() => ({ createOwnerSessionTicket: vi.fn() }));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
vi.mock("@/shared/api/mutation", () => ({ csrfToken: () => "synthetic-csrf" }));
import { requestRealtimeTicket } from "./ticket";
describe("realtime ticket adapter", () => {
  beforeEach(() => vi.resetAllMocks());
  it("получает отдельный ticket через generated same-origin POST и CSRF", async () => {
    sdk.createOwnerSessionTicket.mockResolvedValue({
      data: { ticket: "t".repeat(43), expiresAt: "2026-09-08T12:00:30Z" },
      response: new Response(null, { status: 200 }),
    });
    const signal = new AbortController().signal;
    expect(await requestRealtimeTicket(signal)).toBe("t".repeat(43));
    expect(sdk.createOwnerSessionTicket).toHaveBeenCalledWith({
      headers: { "X-CSRF-Token": "synthetic-csrf" },
      signal,
    });
  });
  it("отбрасывает ответ после закрытия owner запроса", async () => {
    const controller = new AbortController();
    sdk.createOwnerSessionTicket.mockImplementation(() => {
      controller.abort();
      return Promise.resolve({
        data: { ticket: "t".repeat(43), expiresAt: "2026-09-08T12:00:30Z" },
        response: new Response(null, { status: 200 }),
      });
    });
    await expect(requestRealtimeTicket(controller.signal)).rejects.toThrow();
  });
  it("не принимает malformed ticket", async () => {
    sdk.createOwnerSessionTicket.mockResolvedValue({
      data: { ticket: "invalid", expiresAt: "bad" },
      response: new Response(null, { status: 200 }),
    });
    await expect(
      requestRealtimeTicket(new AbortController().signal),
    ).rejects.toThrow("invalid");
  });
});
