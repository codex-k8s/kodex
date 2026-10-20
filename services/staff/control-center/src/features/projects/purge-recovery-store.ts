import { defineStore } from "pinia";
import { computed, ref } from "vue";

import type { ProjectPurgeRecovery } from "@/shared/api/generated/openapi/types.gen";
import {
  ownerRequestSignal,
  ownerInvalidationSignal,
} from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { idempotencyKey } from "@/shared/api/mutation";
import {
  loadProjectPurgeRecovery,
  resolveProjectPurgeRecovery,
} from "./purge-recovery-api";
import {
  canResolvePurgeRecovery,
  purgeRecoveryBinding,
} from "./purge-recovery";

export const useProjectPurgeRecoveryStore = defineStore(
  "project-purge-recovery",
  () => {
    const projectRef = ref("");
    const preview = ref<ProjectPurgeRecovery>();
    const loading = ref(false);
    const submitting = ref(false);
    const acknowledged = ref(false);
    const stale = ref(false);
    const uncertain = ref(false);
    const problem = ref<AppProblem>();
    const attempts = new Map<
      string,
      { binding: string; key: string; owner: AbortSignal }
    >();
    let controller: AbortController | undefined;
    let generation = 0;
    const canSubmit = computed(
      () =>
        !loading.value &&
        !submitting.value &&
        acknowledged.value &&
        canResolvePurgeRecovery(preview.value),
    );

    function close(): void {
      controller?.abort();
      generation += 1;
      projectRef.value = "";
      preview.value = undefined;
      loading.value = false;
      submitting.value = false;
      acknowledged.value = false;
      problem.value = undefined;
      stale.value = false;
      uncertain.value = false;
    }

    async function load(refValue: string, fromStale = false): Promise<void> {
      if (submitting.value) return;
      controller?.abort();
      const request = new AbortController();
      controller = request;
      const current = ++generation;
      const lifetime = ownerRequestSignal();
      projectRef.value = refValue;
      preview.value = undefined;
      acknowledged.value = false;
      problem.value = undefined;
      loading.value = true;
      stale.value = fromStale;
      uncertain.value = false;
      try {
        const response = await loadProjectPurgeRecovery(
          refValue,
          request.signal,
        );
        if (
          request.signal.aborted ||
          lifetime.aborted ||
          generation !== current
        )
          return;
        if (response.projectRef !== refValue)
          throw new Error("Project purge recovery owner mismatch");
        preview.value = response;
        const attempt = attempts.get(refValue);
        if (
          attempt &&
          (attempt.owner.aborted ||
            attempt.binding !== purgeRecoveryBinding(response) ||
            !response.canResolve)
        ) {
          attempts.delete(refValue);
        } else uncertain.value = Boolean(attempt);
      } catch (error) {
        if (
          !request.signal.aborted &&
          !lifetime.aborted &&
          generation === current
        )
          problem.value = asProblem(error);
      } finally {
        if (!lifetime.aborted && generation === current) loading.value = false;
      }
    }

    async function resolve(): Promise<boolean> {
      const value = preview.value;
      if (!value || !canSubmit.value) return false;
      const refValue = projectRef.value;
      const binding = purgeRecoveryBinding(value);
      let attempt = attempts.get(refValue);
      if (attempt?.owner.aborted || attempt?.binding !== binding)
        attempt = undefined;
      if (!attempt) {
        attempt = {
          binding,
          key: idempotencyKey(),
          owner: ownerInvalidationSignal(),
        };
        attempts.set(refValue, attempt);
      }
      controller?.abort();
      const request = new AbortController();
      controller = request;
      const current = ++generation;
      const lifetime = ownerRequestSignal();
      submitting.value = true;
      acknowledged.value = false;
      problem.value = undefined;
      let refreshStale = false;
      try {
        const response = await resolveProjectPurgeRecovery(
          value,
          attempt.key,
          request.signal,
        );
        if (attempts.get(refValue) === attempt) attempts.delete(refValue);
        if (
          request.signal.aborted ||
          lifetime.aborted ||
          generation !== current
        )
          return false;
        if (response.projectRef !== refValue)
          throw new Error("Project purge recovery owner mismatch");
        preview.value = response;
        uncertain.value = false;
        return true;
      } catch (error) {
        const failure = asProblem(error);
        if (
          failure.status >= 400 &&
          failure.status < 500 &&
          attempts.get(refValue) === attempt
        )
          attempts.delete(refValue);
        if (
          request.signal.aborted ||
          lifetime.aborted ||
          generation !== current
        )
          return false;
        refreshStale = failure.status === 409 || failure.status === 412;
        uncertain.value = attempts.has(refValue);
        problem.value = failure;
        preview.value = undefined;
        return false;
      } finally {
        if (!lifetime.aborted && generation === current) {
          submitting.value = false;
          if (refreshStale) await load(refValue, true);
        }
      }
    }

    function clearOwner(): void {
      close();
      attempts.clear();
    }

    return {
      projectRef,
      preview,
      loading,
      submitting,
      acknowledged,
      stale,
      uncertain,
      problem,
      canSubmit,
      load,
      resolve,
      close,
      clearOwner,
    };
  },
);
