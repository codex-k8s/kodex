import { afterEach, describe, expect, it, vi } from "vitest";

import { observeRunOutlineClearance } from "./run-outline-clearance";

afterEach(() => vi.unstubAllGlobals());

describe("область списка ниже фактической сводки", () => {
  it.each([
    ["desktop", 185, 352],
    ["desktop с drawer", 185, 438],
    ["mobile", 100, 294],
    ["mobile с раскрытой сводкой", 100, 554],
  ])("учитывает %s без изменения размеров canvas", (_name, top, bottom) => {
    const workspace = {
      getBoundingClientRect: () => ({ top }),
    };
    const summary = {
      parentElement: workspace,
      getBoundingClientRect: () => ({ bottom }),
    } as unknown as HTMLElement;
    const onChange = vi.fn();
    vi.stubGlobal("ResizeObserver", undefined);
    observeRunOutlineClearance(summary, onChange)();
    const measured = onChange.mock.calls[0]?.[0] as number;
    expect(measured).toBe(bottom - top);
    const scrollAreaTop = top + Math.max(144, measured + 12);
    expect(scrollAreaTop).toBeGreaterThan(bottom);
  });

  it("повторно измеряет resize/новый текст и отключает observer", () => {
    let bottom = 352.2;
    let resized = (): void => {};
    const observe = vi.fn();
    const disconnect = vi.fn();
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(callback: () => void) {
          resized = callback;
        }
        observe = observe;
        disconnect = disconnect;
      },
    );
    const workspace = { getBoundingClientRect: () => ({ top: 185 }) };
    const summary = {
      parentElement: workspace,
      getBoundingClientRect: () => ({ bottom }),
    } as unknown as HTMLElement;
    const onChange = vi.fn();
    const dispose = observeRunOutlineClearance(summary, onChange);
    expect(onChange).toHaveBeenLastCalledWith(168);
    expect(observe.mock.calls).toEqual([[summary], [workspace]]);
    bottom = 510;
    resized();
    expect(onChange).toHaveBeenLastCalledWith(325);
    dispose();
    expect(disconnect).toHaveBeenCalledOnce();
  });

  it("не выдаёт нечисловые размеры и не требует DOM при отсутствии parent", () => {
    vi.stubGlobal("ResizeObserver", undefined);
    const onChange = vi.fn();
    observeRunOutlineClearance(
      {
        parentElement: { getBoundingClientRect: () => ({ top: 0 }) },
        getBoundingClientRect: () => ({ bottom: Number.NaN }),
      } as unknown as HTMLElement,
      onChange,
    )();
    observeRunOutlineClearance(
      { parentElement: null } as HTMLElement,
      onChange,
    )();
    expect(onChange).not.toHaveBeenCalled();
  });
});
