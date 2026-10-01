import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import { computed, ref } from "vue";
const hooks = vi.hoisted(() => ({
  mounted: vi.fn(),
  unmounted: vi.fn(),
  beforeEach: vi.fn(),
  removeGuard: vi.fn(),
  requestConfirmation: vi.fn(),
}));
vi.mock("vue", async (importOriginal) => ({
  ...(await importOriginal<typeof import("vue")>()),
  onMounted: hooks.mounted,
  onBeforeUnmount: hooks.unmounted,
}));
vi.mock("vue-router", () => ({
  useRouter: () => ({
    beforeEach: hooks.beforeEach.mockReturnValue(hooks.removeGuard),
  }),
}));
vi.mock("./confirmation", () => ({
  requestConfirmation: hooks.requestConfirmation,
}));
import { useUnsavedChanges } from "./unsaved-changes";
describe("unsaved changes guard", () => {
  beforeEach(() => vi.resetAllMocks());
  afterEach(() => vi.unstubAllGlobals());
  it("сохраняет черновики при смене query вкладки и защищает смену объекта", async () => {
    hooks.requestConfirmation.mockResolvedValue(false);
    useUnsavedChanges(
      computed(() => true),
      () => "Discard changes?",
      { ignoreQueryOnly: true },
    );
    const guard = hooks.beforeEach.mock.calls[0]?.[0] as (
      to: { path: string },
      from: { path: string },
    ) => boolean | Promise<boolean>;
    expect(guard({ path: "/agents/one" }, { path: "/agents/one" })).toBe(true);
    expect(hooks.requestConfirmation).not.toHaveBeenCalled();
    await expect(
      guard({ path: "/agents/two" }, { path: "/agents/one" }),
    ).resolves.toBe(false);
  });
  it("сохраняет dirty-форму при отмене, разрешает чистую навигацию и удаляет listener", async () => {
    hooks.requestConfirmation.mockResolvedValue(false);
    const addEventListener = vi.fn();
    const removeEventListener = vi.fn();
    vi.stubGlobal("window", { addEventListener, removeEventListener });
    const dirty = ref(false);
    useUnsavedChanges(
      computed(() => dirty.value),
      () => "Discard changes?",
    );
    const guard = hooks.beforeEach.mock.calls[0]?.[0] as () =>
      | boolean
      | Promise<boolean>;
    expect(guard()).toBe(true);
    expect(hooks.requestConfirmation).not.toHaveBeenCalled();
    dirty.value = true;
    await expect(guard()).resolves.toBe(false);
    expect(dirty.value).toBe(true);
    hooks.requestConfirmation.mockResolvedValue(true);
    await expect(guard()).resolves.toBe(true);
    const mount = hooks.mounted.mock.calls[0]?.[0] as () => void;
    const unmount = hooks.unmounted.mock.calls[0]?.[0] as () => void;
    mount();
    const listener = addEventListener.mock.calls[0]?.[1] as (
      event: Partial<BeforeUnloadEvent>,
    ) => void;
    const preventDefault = vi.fn();
    listener({ preventDefault });
    expect(preventDefault).toHaveBeenCalledOnce();
    dirty.value = false;
    preventDefault.mockClear();
    listener({ preventDefault });
    expect(preventDefault).not.toHaveBeenCalled();
    unmount();
    expect(hooks.removeGuard).toHaveBeenCalledOnce();
    expect(removeEventListener).toHaveBeenCalledWith("beforeunload", listener);
  });
});
