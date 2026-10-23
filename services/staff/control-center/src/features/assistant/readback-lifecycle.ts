import { watch, type Ref, type WatchSource } from "vue";

type AssistantReadbackBinding<Target, Result> = {
  loading: Ref<boolean>;
  problem: Ref<boolean>;
  reset: () => void;
  read: (target: Target, signal: AbortSignal) => Promise<Result>;
  apply: (target: Target, result: Result) => void;
  restore?: (target: Target) => boolean;
};

// Проверку точного ресурса выполняет apply только после проверки отмены чтения.
export function watchAssistantReadback<Target extends object, Result>(
  target: WatchSource<Target | undefined>,
  binding: AssistantReadbackBinding<Target, Result>,
): () => Promise<void> {
  let refresh: (() => Promise<void>) | undefined;
  watch(
    target,
    (value, _previous, onCleanup) => {
      binding.reset();
      binding.loading.value = false;
      binding.problem.value = false;
      if (!value) return;
      const controller = new AbortController();
      const closed = () => controller.signal.aborted;
      onCleanup(() => {
        controller.abort();
        refresh = undefined;
      });
      refresh = async () => {
        if (closed()) return;
        binding.loading.value = true;
        try {
          const result = await binding.read(value, controller.signal);
          if (closed()) return;
          binding.apply(value, result);
          binding.problem.value = false;
        } catch {
          if (!closed()) binding.problem.value = true;
        } finally {
          if (!closed()) binding.loading.value = false;
        }
      };
      if (!binding.restore?.(value)) void refresh();
    },
    { immediate: true },
  );
  return async () => {
    await refresh?.();
  };
}
