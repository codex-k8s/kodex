import { watch, type Ref, type WatchSource } from "vue";

import { requestSignal } from "@/shared/api/client";
import {
  isEligibleAgent,
  isEligibleWorkflow,
  toExecutionTargetOption,
  type ExecutionTargetPickerOption,
  type ExecutionTargetType,
} from "@/shared/api/execution-target-picker";
import { getAgent, getWorkflow } from "@/shared/api/generated/openapi/sdk.gen";
import type { Agent, Workflow } from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

// Формы запуска и расписания используют одну границу чтения выбранной цели.
// Проверка точного проекта и права LAUNCH остаётся обязательной после ответа.
export function watchAssistantExecutionTarget(
  sources: readonly [
    WatchSource<string | undefined>,
    WatchSource<ExecutionTargetType | undefined>,
    WatchSource<string>,
  ],
  selected: Ref<ExecutionTargetPickerOption | undefined>,
  problem: Ref<boolean>,
  mismatchMessage: string,
): void {
  watch(
    sources,
    ([projectRef, type, ref], _previous, onCleanup) => {
      if (
        selected.value?.ref === ref &&
        selected.value.targetType === type &&
        selected.value.target.projectRef === projectRef
      )
        return;
      selected.value = undefined;
      problem.value = false;
      if (!projectRef || !type || !ref) return;
      const controller = new AbortController();
      onCleanup(() => controller.abort());
      void (async () => {
        try {
          const target =
            type === "AGENT"
              ? (
                  await unwrap(
                    getAgent({
                      path: { agentRef: ref },
                      signal: requestSignal(controller.signal),
                    }),
                  )
                ).data
              : (
                  await unwrap(
                    getWorkflow({
                      path: { workflowRef: ref },
                      signal: requestSignal(controller.signal),
                    }),
                  )
                ).data;
          if (controller.signal.aborted) return;
          if (
            target.ref !== ref ||
            target.projectRef !== projectRef ||
            (type === "AGENT"
              ? !isEligibleAgent(target as Agent)
              : !isEligibleWorkflow(target as Workflow))
          )
            throw new Error(mismatchMessage);
          selected.value = toExecutionTargetOption(type, target);
        } catch {
          if (!controller.signal.aborted) problem.value = true;
        }
      })();
    },
    { immediate: true },
  );
}
