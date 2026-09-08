import { createSSRApp, h, reactive } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createI18n } from "vue-i18n";
import { beforeEach, expect, it, vi } from "vitest";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
const api = vi.hoisted(() => ({ createDraft: vi.fn(), history: vi.fn() }));
vi.mock("./api", () => api);
import { canRestoreRevision, restoreRevision } from "./restore-revision";
import RestoreRevisionButton from "./RestoreRevisionButton.vue";
import { restoreRevisionMessages } from "./restore-revision-messages";

const source: ManagedConfigurationRevision = {
  ref: "mrev_old",
  revision: 2,
  state: "SUPERSEDED",
  contentFormat: "JSON",
  content:
    '{"name":"Fixture","roleImage":{"environment":{"dockerfile":"FROM scratch"}}}',
  digest: "a".repeat(64),
  sourceAvailable: true,
  validationDiagnostics: [],
  createdAt: "2026-09-08T00:00:00Z",
};
const current: ManagedConfiguration = {
  ref: "mcfg_fixture",
  kind: "ROLE_IMAGE",
  projectRef: "project_fixture",
  name: "Fixture",
  version: 12,
  managedBy: "UI",
  source: "control-center",
  sourceRevision: "",
  sourceEditable: true,
  archived: false,
  nextActions: [],
  updatedAt: source.createdAt,
  currentRevision: {
    ...source,
    ref: "mrev_current",
    revision: 5,
    state: "PUBLISHED",
  },
};
function fixture(kind: ManagedConfiguration["kind"] = "ROLE_IMAGE") {
  const configuration = { ...current, kind };
  const draft = {
    ...source,
    ref: "mrev_new",
    revision: 6,
    state: "DRAFT" as const,
    parentRevisionRef: "mrev_current",
  };
  const readback = {
    configuration: { ...configuration, version: 13 },
    items: [draft, configuration.currentRevision, source],
  };
  api.createDraft.mockResolvedValue({
    configuration: { ...readback.configuration, currentRevision: draft },
    revision: draft,
  });
  api.history.mockResolvedValue(readback);
  return { configuration, draft, readback };
}
beforeEach(() => vi.resetAllMocks());
it("принимает реактивные карточку и revision из редактора", async () => {
  const { configuration, draft } = fixture();
  const restored = await restoreRevision(
    reactive(configuration),
    reactive(source),
    new AbortController().signal,
  );
  expect(restored.revision.ref).toBe(draft.ref);
});
it.each(["ROLE_IMAGE", "INTEGRATION_DEFINITION"] as const)(
  "%s создаёт новую revision из неизменённого старого source и перечитывает прежний published pin",
  async (kind) => {
    const { configuration, draft, readback } = fixture(kind);
    const original = structuredClone(configuration);
    const result = await restoreRevision(
      configuration,
      source,
      new AbortController().signal,
    );
    expect(api.createDraft).toHaveBeenCalledExactlyOnceWith(
      kind,
      {
        configurationRef: current.ref,
        projectRef: current.projectRef,
        name: current.name,
        contentFormat: source.contentFormat,
        content: source.content,
      },
      12,
    );
    expect(result).toEqual({
      configuration: readback.configuration,
      revision: draft,
    });
    expect(result.configuration.currentRevision?.ref).toBe("mrev_current");
    expect(configuration).toEqual(original);
    expect(source.state).toBe("SUPERSEDED");
  },
);
it.each(["GIT", "ARCHIVED", "NO_SOURCE", "NO_EDIT", "DRAFT", "PROMPT"])(
  "не предлагает обход lifecycle %s",
  async (condition) => {
    const { configuration } = fixture();
    const revision = { ...source };
    if (condition === "GIT") configuration.managedBy = "GIT";
    if (condition === "ARCHIVED") configuration.archived = true;
    if (condition === "NO_SOURCE") revision.sourceAvailable = false;
    if (condition === "NO_EDIT") configuration.sourceEditable = false;
    if (condition === "DRAFT") revision.state = "DRAFT";
    if (condition === "PROMPT") configuration.kind = "PROMPT_TEMPLATE";
    expect(canRestoreRevision(configuration, revision)).toBe(false);
    await expect(
      restoreRevision(configuration, revision, new AbortController().signal),
    ).rejects.toThrow();
    expect(api.createDraft).not.toHaveBeenCalled();
  },
);
it.each([
  "same-ref",
  "old-number",
  "wrong-parent",
  "different-content",
  "published",
  "wrong-owner",
])("отклоняет неверную квитанцию %s", async (condition) => {
  const { configuration, draft } = fixture();
  const result = {
    configuration: { ...configuration, version: 13 },
    revision: { ...draft },
  };
  if (condition === "same-ref") result.revision.ref = source.ref;
  if (condition === "old-number") result.revision.revision = 3;
  if (condition === "wrong-parent")
    result.revision.parentRevisionRef = source.ref;
  if (condition === "different-content") result.revision.content = "{}";
  if (condition === "published")
    Object.assign(result.revision, { state: "PUBLISHED" });
  if (condition === "wrong-owner") result.configuration.projectRef = "foreign";
  api.createDraft.mockResolvedValue(result);
  await expect(
    restoreRevision(configuration, source, new AbortController().signal),
  ).rejects.toThrow("receipt mismatch");
  expect(api.history).not.toHaveBeenCalled();
});
it("не подменяет публикацию успешным созданием draft и не повторяет потерянный ответ", async () => {
  const { configuration, draft, readback } = fixture();
  api.history.mockResolvedValue({
    ...readback,
    configuration: { ...readback.configuration, currentRevision: draft },
  });
  await expect(
    restoreRevision(configuration, source, new AbortController().signal),
  ).rejects.toThrow("readback mismatch");
  expect(api.createDraft).toHaveBeenCalledOnce();
  api.createDraft.mockRejectedValue(new Error("Lost acknowledgement"));
  await expect(
    restoreRevision(configuration, source, new AbortController().signal),
  ).rejects.toThrow("Lost acknowledgement");
  expect(api.createDraft).toHaveBeenCalledTimes(2);
});
it("не принимает поздний draft ACK после закрытия редактора", async () => {
  const { configuration } = fixture();
  const abort = new AbortController();
  api.createDraft.mockImplementation(() => {
    abort.abort();
    return Promise.resolve({});
  });
  await expect(
    restoreRevision(configuration, source, abort.signal),
  ).rejects.toThrow();
  expect(api.history).not.toHaveBeenCalled();
});
it.each(["ru", "en"] as const)(
  "история показывает явную доступную кнопку без изменения source: %s",
  async (locale) => {
    const { configuration } = fixture();
    const app = createSSRApp({
      render: () =>
        h(RestoreRevisionButton, { configuration, revision: source }),
    });
    app.use(
      createI18n({
        legacy: false,
        locale,
        messages: {
          ru: { configurationRestore: restoreRevisionMessages.ru },
          en: { configurationRestore: restoreRevisionMessages.en },
        },
      }),
    );
    const html = await renderToString(app);
    expect(html).toContain(restoreRevisionMessages[locale].action);
    expect(html).not.toContain("disabled");
  },
);
