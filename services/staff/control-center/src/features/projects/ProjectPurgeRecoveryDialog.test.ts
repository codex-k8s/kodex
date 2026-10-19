import { readFileSync } from "node:fs";
import { createSSRApp, h } from "vue";
import { renderToString } from "@vue/server-renderer";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import ProjectPurgeRecoveryDialog from "./ProjectPurgeRecoveryDialog.vue";

const mocks = vi.hoisted(() => ({
  view: {
    preview: undefined as unknown,
    loading: false,
    submitting: false,
    acknowledged: false,
    stale: false,
    uncertain: false,
    problem: undefined,
    canSubmit: false,
    load: vi.fn(),
    close: vi.fn(),
    clearOwner: vi.fn(),
    resolve: vi.fn(),
  },
}));
vi.mock("./purge-recovery-store", () => ({
  useProjectPurgeRecoveryStore: () => mocks.view,
}));
vi.mock("@/features/platform/store", () => ({
  usePlatformStore: () => ({
    bootstrap: { organizationRef: "org_synthetic" },
    trashedProjects: {},
    $onAction: () => () => {},
  }),
}));
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));

beforeEach(() => {
  mocks.view.preview = {
    projectRef: "prj_synthetic",
    version: 2,
    snapshotDigest: "a".repeat(64),
    projectPresent: true,
    canResolve: true,
    state: "WAITING_OWNER",
    unknownContentCount: 1,
    cleanupPendingCount: 0,
    projectVersion: 4,
    resolutionRef: "",
  };
  mocks.view.loading = false;
  mocks.view.submitting = false;
  mocks.view.acknowledged = false;
  mocks.view.stale = false;
  mocks.view.uncertain = false;
  mocks.view.canSubmit = false;
});
async function render(): Promise<string> {
  const app = createSSRApp({
    render: () =>
      h(ProjectPurgeRecoveryDialog, {
        projectRef: "prj_synthetic",
        projectName: "Рабочий проект",
      }),
  });
  app.use(i18n);
  return renderToString(app);
}
describe("Модалка разрешения неизвестного результата", () => {
  it("показывает необратимость и unchecked согласие, не маскирует queued как очищено", async () => {
    const html = await render();
    expect(html).toContain("Отказ необратим");
    expect(html).toContain("Минимальный технический контроль");
    expect(html).toContain('type="checkbox"');
    expect(html).not.toContain(" checked");
    expect(html).toContain("disabled");
    expect(html).not.toContain("Удаление объектов подтверждено");
    expect(html).toContain('role="dialog"');
    expect(html).not.toContain("a".repeat(64));
  });
  it("не требует отказаться от новых данных при повторе уже одобренной очистки", async () => {
    mocks.view.preview = {
      ...(mocks.view.preview as object),
      unknownContentCount: 0,
      cleanupPendingCount: 1,
      projectPresent: false,
    };
    const html = await render();
    expect(html).toContain("Первоначальное решение");
    expect(html).toContain("Разрешаю новую попытку очистки");
    expect(html).not.toContain("Отказ необратим");
  });
  it("после принятия решения явно отличает pending от физического удаления", async () => {
    mocks.view.preview = {
      ...(mocks.view.preview as object),
      state: "QUEUED",
      canResolve: false,
      cleanupPendingCount: 2,
    };
    const html = await render();
    expect(html).toContain("Незавершённых проверок очистки: 2");
    expect(html).not.toContain('type="checkbox"');
    expect(html).not.toContain("Подтвердить и продолжить");
  });
  it("сохраняет общий outside-close/focus и мобильную геометрию без таймера polling", () => {
    const dialog = readFileSync(
      new URL("./ProjectPurgeRecoveryDialog.vue", import.meta.url),
      "utf8",
    );
    const list = readFileSync(
      new URL("./ProjectList.vue", import.meta.url),
      "utf8",
    );
    const page = readFileSync(
      new URL("../../pages/ProjectsPage.vue", import.meta.url),
      "utf8",
    );
    expect(dialog).toContain("<ModalDialog");
    expect(dialog).not.toContain(":busy=");
    expect(dialog).not.toContain("setInterval");
    expect(dialog).not.toContain("setTimeout");
    expect(dialog).toContain("onBeforeUnmount");
    expect(list).toContain("@media (max-width: 640px)");
    expect(list).toContain("flex-wrap: wrap");
    expect(page).toContain(':key="recoveryTarget.ref"');
  });
});
