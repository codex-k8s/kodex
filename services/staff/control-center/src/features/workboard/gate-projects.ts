import { shallowRef } from "vue";
import { getProject } from "@/shared/api/generated/openapi/sdk.gen";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { unwrap } from "@/shared/api/problem";

const concurrentProjectReads = 6;

export function useGateProjects() {
  const projects = shallowRef<Record<string, Project>>({});
  const pending = new Set<string>();
  let controller = new AbortController();
  let generation = 0;
  const ownerScope = ownerRequestSignal();
  let disposed = false;

  function invalidate(): void {
    generation += 1;
    controller.abort();
    if (!disposed) controller = new AbortController();
    pending.clear();
    projects.value = {};
  }

  function dispose(): void {
    disposed = true;
    ownerScope.removeEventListener("abort", dispose);
    invalidate();
  }

  ownerScope.addEventListener("abort", dispose, { once: true });
  if (ownerScope.aborted) dispose();

  async function ensure(refs: readonly string[]): Promise<void> {
    if (disposed || ownerScope.aborted) return;
    const current = generation;
    const active = controller;
    const missing = [...new Set(refs)].filter(
      (ref) => ref && !projects.value[ref] && !pending.has(ref),
    );
    for (const ref of missing) pending.add(ref);
    try {
      for (
        let index = 0;
        index < missing.length;
        index += concurrentProjectReads
      ) {
        // Между порциями await может завершить страницу или owner-сессию.
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition
        if (disposed || ownerScope.aborted || current !== generation) return;
        await Promise.all(
          missing
            .slice(index, index + concurrentProjectReads)
            .map(async (ref) => {
              try {
                const project = (
                  await unwrap(
                    getProject({
                      path: { projectRef: ref },
                      signal: requestSignal(active.signal),
                    }),
                  )
                ).data;
                if (
                  disposed ||
                  ownerScope.aborted ||
                  current !== generation ||
                  project.ref !== ref
                )
                  return;
                projects.value = { ...projects.value, [ref]: project };
              } catch {
                // Решение остаётся доступно, даже если связанный проект скрыт.
              } finally {
                if (current === generation) pending.delete(ref);
              }
            }),
        );
      }
    } finally {
      if (current === generation)
        for (const ref of missing) pending.delete(ref);
    }
  }

  return { projects, ensure, invalidate, dispose };
}
