import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  ConfigureEmailMailboxCredentialData,
  EmailMailboxCredential,
  EmailMailboxCredentialKind,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  configureEmailMailboxCredential:
    vi.fn<
      (
        options: Omit<ConfigureEmailMailboxCredentialData, "url">,
      ) => Promise<{ data: EmailMailboxCredential; response: Response }>
    >(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal: AbortSignal) => signal,
}));
import {
  prepareMailboxCredential,
  saveMailboxCredential,
  validMailboxCredential,
  MailboxCredentialMismatch,
} from "./email-credentials";

const connection: IntegrationConnection = {
  ref: "connection_synthetic",
  version: 3,
  definitionKey: "email",
  name: "Email",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: ["CONFIGURE_CREDENTIAL"],
  definitionVersion: "1",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
const receipt: EmailMailboxCredential = {
  connectionRef: connection.ref,
  connectionVersion: 4,
  kind: "AUTH_SECRET",
  name: "credential_synthetic",
  generation: 1,
};
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
  sdk.configureEmailMailboxCredential.mockResolvedValue({
    data: receipt,
    response: new Response(null, { headers: { ETag: '"4"' } }),
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("EMAIL write-only credential", () => {
  it.each<[EmailMailboxCredentialKind, number]>([
    ["CA_CERTIFICATE", 65536],
    ["USERNAME", 320],
    ["AUTH_SECRET", 16384],
  ])("проверяет UTF-8 лимит %s", (kind, limit) => {
    expect(validMailboxCredential(kind, "я".repeat(limit / 2))).toBe(true);
    expect(validMailboxCredential(kind, "я".repeat(limit / 2) + "a")).toBe(
      false,
    );
    expect(validMailboxCredential(kind, "")).toBe(false);
  });
  it("не обрезает пробелы и разрешает строки PEM только для CA", () => {
    expect(validMailboxCredential("AUTH_SECRET", "  ")).toBe(true);
    expect(validMailboxCredential("CA_CERTIFICATE", "synthetic\nPEM")).toBe(
      true,
    );
    for (const character of ["\0", "\r", "\n"]) {
      expect(validMailboxCredential("USERNAME", `a${character}b`)).toBe(false);
      expect(validMailboxCredential("AUTH_SECRET", `a${character}b`)).toBe(
        false,
      );
    }
  });
  it("отправляет exact значение, OCC, CSRF и возвращает только safe descriptor", async () => {
    const value = "  synthetic credential  ";
    const attempt = await prepareMailboxCredential(
      connection,
      "AUTH_SECRET",
      value,
    );
    const signal = new AbortController().signal;
    expect(JSON.stringify(attempt)).not.toContain(value);
    expect(await saveMailboxCredential(attempt, value, signal)).toEqual(
      receipt,
    );
    expect(sdk.configureEmailMailboxCredential).toHaveBeenCalledWith({
      path: { connectionRef: connection.ref },
      body: { kind: "AUTH_SECRET", value },
      signal,
      headers: {
        "If-Match": '"3"',
        "Idempotency-Key": attempt.key,
        "X-CSRF-Token": "c".repeat(43),
      },
    });
  });
  it("после timeout не повторяет автоматически; ручной повтор сохраняет старую версию и ключ", async () => {
    const attempt = await prepareMailboxCredential(
      connection,
      "AUTH_SECRET",
      "synthetic",
    );
    sdk.configureEmailMailboxCredential.mockRejectedValueOnce(
      new TypeError("Synthetic timeout"),
    );
    await expect(
      saveMailboxCredential(attempt, "synthetic", new AbortController().signal),
    ).rejects.toThrow();
    expect(sdk.configureEmailMailboxCredential).toHaveBeenCalledOnce();
    const retry = await prepareMailboxCredential(
      { ...connection, version: 4 },
      "AUTH_SECRET",
      "synthetic",
      attempt,
    );
    expect(retry).toEqual(attempt);
    await expect(
      prepareMailboxCredential(connection, "AUTH_SECRET", "changed", attempt),
    ).rejects.toBeInstanceOf(MailboxCredentialMismatch);
    await saveMailboxCredential(
      retry,
      "synthetic",
      new AbortController().signal,
    );
    expect(
      sdk.configureEmailMailboxCredential.mock.calls[1]?.[0].headers,
    ).toEqual(sdk.configureEmailMailboxCredential.mock.calls[0]?.[0].headers);
  });
  it.each<Partial<EmailMailboxCredential>>([
    { connectionRef: "foreign" },
    { kind: "USERNAME" },
    { connectionVersion: 3 },
    { generation: 0 },
    { name: "invalid/name" },
  ])("отклоняет неверную квитанцию %j", async (change) => {
    const attempt = await prepareMailboxCredential(
      connection,
      "AUTH_SECRET",
      "synthetic",
    );
    sdk.configureEmailMailboxCredential.mockResolvedValue({
      data: { ...receipt, ...change },
      response: new Response(null, { headers: { ETag: '"4"' } }),
    });
    await expect(
      saveMailboxCredential(attempt, "synthetic", new AbortController().signal),
    ).rejects.toThrow();
  });
  it("не начинает команду без разрешённого nextAction", async () => {
    await expect(
      prepareMailboxCredential(
        { ...connection, nextActions: [] },
        "AUTH_SECRET",
        "synthetic",
      ),
    ).rejects.toThrow();
    expect(sdk.configureEmailMailboxCredential).not.toHaveBeenCalled();
  });
});
