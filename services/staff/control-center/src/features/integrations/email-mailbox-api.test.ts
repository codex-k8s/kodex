import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { EmailMailboxConfigurationView } from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  getEmailMailboxConfiguration: vi.fn(),
  listEmailMailboxCredentials: vi.fn(),
  previewEmailMailboxConfiguration: vi.fn(),
  createEmailMailboxDraft:
    vi.fn<(options: { headers: Record<string, string> }) => Promise<unknown>>(),
  saveEmailMailboxDraft: vi.fn(),
  bindEmailMailboxConfiguration: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) => signal,
}));
import {
  checkedMailbox,
  readMailbox,
  listMailboxCredentials,
  previewMailbox,
  createMailboxDraft,
  saveMailboxDraft,
  bindMailbox,
} from "./email-mailbox-api";
const signal = new AbortController().signal;
const key = "00000000-0000-4000-8000-000000000001";
function view(): EmailMailboxConfigurationView {
  return {
    connectionRef: "connection",
    connectionVersion: 8,
    mailboxRef: "mailbox",
    configuration: {
      ref: "configuration",
      version: 3,
      kind: "EMAIL_MAILBOX",
      name: "Mail",
      managedBy: "UI",
      source: "",
      sourceRevision: "",
      updatedAt: "2026-09-05T00:00:00Z",
    },
    revision: {
      ref: "revision",
      revision: 2,
      state: "DRAFT",
      contentFormat: "YAML",
      content: "enabled: false\n",
      digest: "a".repeat(64),
      validationDiagnostics: [],
      createdAt: "2026-09-05T00:00:00Z",
    },
    specification: { enabled: false },
    diagnostics: [],
    boundRevisionRef: "previous",
    publication: {
      ref: "publication",
      revision: 1,
      digest: "b".repeat(64),
      state: "READY",
      configurationRevisionRef: "previous",
      createdAt: "2026-09-05T00:00:00Z",
      failureCode: "",
    },
  };
}
const response = (data: unknown) => ({
  data,
  response: new Response(null, { headers: { ETag: '"3"' } }),
});
beforeEach(() => {
  vi.resetAllMocks();
  vi.stubGlobal("document", { cookie: `__Host-kodex-csrf=${"c".repeat(43)}` });
});
afterEach(() => vi.unstubAllGlobals());
describe("mailbox owner contract", () => {
  it("читает exact history и не приписывает latest delivery выбранной revision", async () => {
    const result = view();
    sdk.getEmailMailboxConfiguration.mockResolvedValue(response(result));
    expect(
      await readMailbox("connection", signal, "configuration", "revision"),
    ).toEqual(result);
    expect(sdk.getEmailMailboxConfiguration).toHaveBeenCalledWith(
      expect.objectContaining({
        query: { configurationRef: "configuration", revisionRef: "revision" },
        cache: "no-store",
        signal,
      }),
    );
    expect(() => checkedMailbox(result, "foreign")).toThrow("receipt mismatch");
    expect(() =>
      checkedMailbox(result, "connection", "configuration", "foreign"),
    ).toThrow("receipt mismatch");
  });
  it("сохраняет syntax-valid incomplete spec с semantic diagnostics и закрывает syntax recovery", async () => {
    sdk.previewEmailMailboxConfiguration.mockResolvedValue(
      response({
        specification: {},
        canonicalYaml: "{}\n",
        valid: false,
        diagnostics: [
          {
            code: "EMAIL_MAILBOX_CONFIGURATION_INVALID",
            path: "smtp",
            message: "SMTP is required",
            line: 0,
            column: 0,
          },
        ],
      }),
    );
    expect(
      (await previewMailbox("connection", { yaml: "{}" }, signal))
        .specification,
    ).toEqual({});
    sdk.previewEmailMailboxConfiguration.mockResolvedValue(
      response({
        specification: {},
        canonicalYaml: "{}\n",
        valid: false,
        diagnostics: [{ code: "EMAIL_MAILBOX_SYNTAX_INVALID" }],
      }),
    );
    await expect(
      previewMailbox("connection", { yaml: "broken: [" }, signal),
    ).rejects.toThrow("preview is invalid");
  });
  it("сохраняет одинаковые имена разных поколений и отклоняет дубликаты tuple", async () => {
    const first = {
      connectionRef: "connection",
      connectionVersion: 8,
      kind: "AUTH_SECRET",
      name: "credential",
      generation: 1,
    };
    sdk.listEmailMailboxCredentials.mockResolvedValue(
      response({
        items: [first, { ...first, generation: 2 }],
        total: 2,
        nextPageToken: "",
      }),
    );
    expect(
      (await listMailboxCredentials("connection", "AUTH_SECRET", signal)).items,
    ).toHaveLength(2);
    sdk.listEmailMailboxCredentials.mockResolvedValue(
      response({ items: [first, first], total: 2, nextPageToken: "" }),
    );
    await expect(
      listMailboxCredentials("connection", "AUTH_SECRET", signal),
    ).rejects.toThrow("page is invalid");
  });
  it("новый draft не подменяет owner OCC, existing draft требует exact base", async () => {
    sdk.createEmailMailboxDraft.mockResolvedValue(response(view()));
    await createMailboxDraft(
      "connection",
      { name: "Mail", content: { specification: {} } },
      key,
    );
    expect(
      sdk.createEmailMailboxDraft.mock.calls[0]?.[0].headers["If-Match"],
    ).toBeUndefined();
    await expect(
      createMailboxDraft(
        "connection",
        {
          name: "Mail",
          configurationRef: "configuration",
          content: { specification: {} },
        },
        key,
      ),
    ).rejects.toThrow("base version");
    await createMailboxDraft(
      "connection",
      {
        name: "Mail",
        configurationRef: "configuration",
        content: { specification: {} },
      },
      key,
      3,
    );
    expect(
      sdk.createEmailMailboxDraft.mock.calls[1]?.[0].headers,
    ).toMatchObject({ "If-Match": '"3"', "Idempotency-Key": key });
  });
  it("save допускает новую immutable revision и bind передаёт два разных owner pin", async () => {
    const original = view();
    const saved = view();
    saved.revision = { ...saved.revision, ref: "next", revision: 3 };
    sdk.saveEmailMailboxDraft.mockResolvedValue(response(saved));
    expect(
      (await saveMailboxDraft(original, { specification: {} }, key)).revision
        .ref,
    ).toBe("next");
    expect(sdk.saveEmailMailboxDraft).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { configurationRef: "configuration", revisionRef: "revision" },
        headers: {
          "If-Match": '"3"',
          "Idempotency-Key": key,
          "X-CSRF-Token": "c".repeat(43),
        },
      }),
    );
    sdk.bindEmailMailboxConfiguration.mockResolvedValue(response(original));
    await bindMailbox(original, key);
    expect(sdk.bindEmailMailboxConfiguration).toHaveBeenCalledWith(
      expect.objectContaining({
        body: { connectionRef: "connection", expectedConnectionVersion: 8 },
        headers: {
          "If-Match": '"3"',
          "Idempotency-Key": key,
          "X-CSRF-Token": "c".repeat(43),
        },
      }),
    );
  });
});
