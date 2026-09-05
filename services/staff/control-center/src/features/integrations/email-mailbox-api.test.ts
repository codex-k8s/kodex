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
  copyGitManagedConfiguration: vi.fn(),
  detachGitManagedConfiguration: vi.fn(),
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
  changeMailboxSource,
} from "./email-mailbox-api";
const signal = new AbortController().signal;
const key = "00000000-0000-4000-8000-000000000001";
function view(): EmailMailboxConfigurationView {
  const action = (
    action: EmailMailboxConfigurationView["nextActions"][number]["action"],
  ) => ({ action, enabled: false, reason: "STATE" as const });
  return {
    nextActions: [
      action("CREATE_DRAFT"),
      action("SAVE"),
      action("VALIDATE"),
      action("PUBLISH"),
      action("DISCARD"),
      action("BIND"),
      action("UNBIND"),
      action("DETACH"),
      action("COPY"),
    ],
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
  it("COPY читает новую owner association без старой revision/binding, DETACH сохраняет set", async () => {
    const original = view();
    original.configuration.managedBy = "GIT";
    const copied = view();
    copied.configuration = { ...copied.configuration, ref: "copy", version: 1 };
    copied.mailboxRef = "copied-mailbox";
    copied.boundRevisionRef = "";
    sdk.copyGitManagedConfiguration.mockResolvedValue(
      response({
        configuration: copied.configuration,
        revision: copied.revision,
      }),
    );
    sdk.getEmailMailboxConfiguration.mockResolvedValue(response(copied));
    expect(
      await changeMailboxSource(original, "COPY", "Copy", key, signal),
    ).toEqual(copied);
    expect(sdk.getEmailMailboxConfiguration.mock.calls[0]?.[0]).toMatchObject({
      path: { connectionRef: "connection" },
      query: { configurationRef: "copy" },
    });
    expect(
      sdk.getEmailMailboxConfiguration.mock.calls[0]?.[0],
    ).not.toHaveProperty("query.revisionRef");
    expect(sdk.copyGitManagedConfiguration.mock.calls[0]?.[0]).toMatchObject({
      body: { name: "Copy" },
      headers: { "If-Match": '"3"', "Idempotency-Key": key },
    });
    const detached = view();
    sdk.detachGitManagedConfiguration.mockResolvedValue(
      response({ configuration: detached.configuration }),
    );
    sdk.getEmailMailboxConfiguration.mockResolvedValue(response(detached));
    await changeMailboxSource(original, "DETACH", "", key, signal);
    expect(sdk.getEmailMailboxConfiguration.mock.calls[1]?.[0]).toMatchObject({
      query: { configurationRef: "configuration" },
    });
    sdk.copyGitManagedConfiguration.mockResolvedValue(
      response({
        configuration: detached.configuration,
        revision: detached.revision,
      }),
    );
    await expect(
      changeMailboxSource(original, "COPY", "Copy", key, signal),
    ).rejects.toThrow("receipt mismatch");
  });
  it("закрыто отклоняет повторяющиеся или противоречивые authority actions", () => {
    const repeated = view();
    repeated.nextActions[1] = repeated.nextActions[0];
    expect(() => checkedMailbox(repeated, "connection")).toThrow(
      "action projection",
    );
    const mismatch = view();
    mismatch.nextActions[0] = {
      action: "CREATE_DRAFT",
      enabled: true,
      reason: "STATE",
    };
    expect(() => checkedMailbox(mismatch, "connection")).toThrow(
      "action projection",
    );
  });
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
