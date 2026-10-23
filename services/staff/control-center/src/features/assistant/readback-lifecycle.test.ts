import { effectScope, nextTick, ref, shallowRef } from "vue";
import { afterEach, describe, expect, test } from "vitest";

import { watchAssistantReadback } from "./readback-lifecycle";

type Target = { ref: string };
type Result = Target & { label: string };
type PendingRead = {
  target: Target;
  signal: AbortSignal;
  resolve: (result: Result) => void;
  reject: (error: Error) => void;
};

const scopes: ReturnType<typeof effectScope>[] = [];
afterEach(() => {
  for (const scope of scopes.splice(0)) scope.stop();
});

function fixture(initial?: Target, cached?: Result) {
  const scope = effectScope();
  scopes.push(scope);
  const target = shallowRef(initial);
  const result = shallowRef<Result>();
  const loading = ref(false);
  const problem = ref(false);
  const reads: PendingRead[] = [];
  const refresh = scope.run(() =>
    watchAssistantReadback(target, {
      loading,
      problem,
      reset: () => {
        result.value = undefined;
      },
      read: (value, signal) =>
        new Promise<Result>((resolve, reject) => {
          reads.push({ target: value, signal, resolve, reject });
        }),
      apply: (value, next) => {
        if (value.ref !== next.ref) throw new Error("Readback mismatch");
        result.value = next;
      },
      restore: (value) => {
        if (cached?.ref !== value.ref) return false;
        result.value = cached;
        return true;
      },
    }),
  );
  if (!refresh) throw new Error("Inactive test scope");
  return { target, result, loading, problem, reads, refresh, scope };
}

describe("жизненный цикл чтения карточки помощника", () => {
  test("ожидает цель и запускает первое чтение после её появления", async () => {
    const state = fixture();
    await state.refresh();
    expect(state.reads).toHaveLength(0);
    expect(state.loading.value).toBe(false);
    state.target.value = { ref: "schedule_one" };
    await nextTick();
    expect(state.reads).toHaveLength(1);
    expect(state.reads[0]?.target.ref).toBe("schedule_one");
    expect(state.loading.value).toBe(true);
    state.reads[0]?.resolve({ ref: "schedule_one", label: "Текущий" });
    await nextTick();
    expect(state.result.value?.label).toBe("Текущий");
    expect(state.loading.value).toBe(false);
    expect(state.problem.value).toBe(false);
  });

  test.each(["resolve", "reject"] as const)(
    "после смены цели игнорирует поздний %s и не меняет флаги нового чтения",
    async (outcome) => {
      const state = fixture({ ref: "old" });
      const previous = state.reads[0];
      state.target.value = { ref: "current" };
      await nextTick();
      expect(previous?.signal.aborted).toBe(true);
      expect(state.reads).toHaveLength(2);
      if (outcome === "resolve")
        previous?.resolve({ ref: "old", label: "Устаревший" });
      else previous?.reject(new Error("Old read failed"));
      await nextTick();
      expect(state.result.value).toBeUndefined();
      expect(state.loading.value).toBe(true);
      expect(state.problem.value).toBe(false);
      state.reads[1]?.resolve({ ref: "current", label: "Новый" });
      await nextTick();
      expect(state.result.value?.ref).toBe("current");
      expect(state.loading.value).toBe(false);
    },
  );

  test("сохраняет readback при ошибке обновления и восстанавливается следующим чтением", async () => {
    const state = fixture({ ref: "connection" });
    state.reads[0]?.resolve({ ref: "connection", label: "Прежний" });
    await nextTick();
    const failed = state.refresh();
    state.reads[1]?.reject(new Error("Read failed"));
    await failed;
    expect(state.problem.value).toBe(true);
    expect(state.loading.value).toBe(false);
    expect(state.result.value?.label).toBe("Прежний");
    const recovered = state.refresh();
    state.reads[2]?.resolve({ ref: "connection", label: "Обновлённый" });
    await recovered;
    expect(state.result.value?.label).toBe("Обновлённый");
    expect(state.problem.value).toBe(false);
    expect(state.loading.value).toBe(false);
  });

  test("не применяет ответ, который отклонила проверка точной цели", async () => {
    const state = fixture({ ref: "expected" });
    state.reads[0]?.resolve({ ref: "different", label: "Чужой" });
    await nextTick();
    expect(state.result.value).toBeUndefined();
    expect(state.problem.value).toBe(true);
    expect(state.loading.value).toBe(false);
  });

  test("использует уже проверенный кэш и сохраняет явное обновление", async () => {
    const cached = { ref: "run", label: "Кэш" };
    const state = fixture({ ref: "run" }, cached);
    expect(state.result.value).toBe(cached);
    expect(state.reads).toHaveLength(0);
    expect(state.loading.value).toBe(false);
    const updated = state.refresh();
    expect(state.reads).toHaveLength(1);
    state.reads[0]?.resolve({ ref: "run", label: "Сервер" });
    await updated;
    expect(state.result.value?.label).toBe("Сервер");
  });

  test("очищает результат и флаги после удаления цели", async () => {
    const state = fixture({ ref: "workflow" });
    state.reads[0]?.resolve({ ref: "workflow", label: "Процесс" });
    await nextTick();
    const pending = state.refresh();
    state.target.value = undefined;
    await nextTick();
    expect(state.reads[1]?.signal.aborted).toBe(true);
    expect(state.result.value).toBeUndefined();
    expect(state.loading.value).toBe(false);
    expect(state.problem.value).toBe(false);
    state.reads[1]?.resolve({ ref: "workflow", label: "Поздний" });
    await pending;
    await state.refresh();
    expect(state.result.value).toBeUndefined();
    expect(state.reads).toHaveLength(2);
  });

  test("отмена области закрывает запрос и дальнейшее обновление", async () => {
    const state = fixture({ ref: "schedule" });
    state.scope.stop();
    expect(state.reads[0]?.signal.aborted).toBe(true);
    await state.refresh();
    expect(state.reads).toHaveLength(1);
    state.reads[0]?.resolve({ ref: "schedule", label: "Поздний" });
    await nextTick();
    expect(state.result.value).toBeUndefined();
  });
});
