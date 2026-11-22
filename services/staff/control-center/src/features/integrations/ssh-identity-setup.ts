import { onScopeDispose, ref } from "vue";
import * as api from "./ssh-identity-setup-api";
import type {
  SSHSetupContext,
  SSHUploadCapability,
} from "./ssh-identity-setup-api";
import type { SshIdentitySetup } from "@/shared/api/generated/openapi/types.gen";
import { idempotencyKey } from "@/shared/api/mutation";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export type SSHSetupPort = Pick<
  typeof api,
  | "beginSSHSetup"
  | "readSSHSetup"
  | "recoverSSHSetup"
  | "cancelSSHSetup"
  | "uploadSSHMaterial"
>;

// Сценарий не помещает key/permit в Pinia, URL, storage либо recovery metadata.
// Локальный abort не объявляет material Create отменённым: после него нужен owner read.
export function useSSHIdentitySetup(
  context: () => SSHSetupContext,
  port: SSHSetupPort = api,
) {
  const privateKey = ref("");
  const setup = ref<SshIdentitySetup>();
  const pendingBeginKey = ref("");
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const needsReadback = ref(false);
  const unavailable = ref(false);
  const owner = ownerInvalidationSignal();
  let capability: SSHUploadCapability | undefined;
  let controller: AbortController | undefined;
  let generation = 0;
  let disposed = false;
  let source = JSON.stringify(context());

  function current(epoch: number, expected: string): boolean {
    return (
      !disposed &&
      !owner.aborted &&
      epoch === generation &&
      JSON.stringify(context()) === expected
    );
  }
  function suspend(): void {
    generation++;
    controller?.abort();
    privateKey.value = "";
    capability = undefined;
    if (busy.value) needsReadback.value = true;
    busy.value = false;
  }
  function resetContext(): void {
    suspend();
    source = JSON.stringify(context());
    setup.value = undefined;
    pendingBeginKey.value = "";
    needsReadback.value = false;
    problem.value = undefined;
  }
  function closeOwner(): void {
    disposed = true;
    resetContext();
    unavailable.value = true;
  }
  owner.addEventListener("abort", closeOwner, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", closeOwner);
    closeOwner();
  });

  function begin() {
    if (disposed || owner.aborted || busy.value) return undefined;
    if (source !== JSON.stringify(context())) resetContext();
    controller?.abort();
    controller = new AbortController();
    const operation = {
      epoch: ++generation,
      source,
      signal: AbortSignal.any([owner, controller.signal]),
      context: { ...context() },
    };
    busy.value = true;
    problem.value = undefined;
    return operation;
  }

  async function submit(): Promise<void> {
    const operation = begin();
    if (!operation) return;
    const material = new TextEncoder().encode(privateKey.value);
    privateKey.value = "";
    capability = undefined;
    setup.value = undefined;
    try {
      if (material.byteLength < 1 || material.byteLength > 16384)
        throw new Error("SSH identity material bounds are invalid");
      const hash = new Uint8Array(
        await crypto.subtle.digest("SHA-256", material),
      );
      if (!current(operation.epoch, operation.source)) return;
      const digest = Array.from(hash, (byte) =>
        byte.toString(16).padStart(2, "0"),
      ).join("");
      hash.fill(0);
      const key = idempotencyKey();
      pendingBeginKey.value = key;
      needsReadback.value = true;
      const admission = await port.beginSSHSetup(
        operation.context,
        digest,
        material.byteLength,
        key,
        operation.signal,
      );
      if (!current(operation.epoch, operation.source)) return;
      setup.value = admission.setup;
      if (admission.reconcileOnly) {
        needsReadback.value = false;
        return;
      }
      capability = admission.upload;
      let failure: unknown;
      try {
        await port.uploadSSHMaterial(
          capability,
          admission.setup.ref,
          material,
          operation.signal,
        );
      } catch (error) {
        failure = error;
      }
      if (!current(operation.epoch, operation.source)) return;
      // Любой receipt/ошибка требует authoritative read; upload reply не публикует ready.
      const observed = await port.readSSHSetup(
        operation.context.connectionRef,
        admission.setup.ref,
        operation.signal,
      );
      if (!current(operation.epoch, operation.source)) return;
      setup.value = observed;
      needsReadback.value = false;
      if (failure) problem.value = asProblem(failure);
      if (setup.value.state !== "UNKNOWN") capability = undefined;
    } catch (error) {
      if (current(operation.epoch, operation.source))
        problem.value = asProblem(error);
    } finally {
      material.fill(0);
      if (current(operation.epoch, operation.source)) busy.value = false;
    }
  }

  async function refresh(): Promise<void> {
    const operation = begin();
    if (!operation) return;
    try {
      const value = setup.value
        ? await port.readSSHSetup(
            operation.context.connectionRef,
            setup.value.ref,
            operation.signal,
          )
        : pendingBeginKey.value
          ? await port.recoverSSHSetup(
              operation.context.connectionRef,
              pendingBeginKey.value,
              operation.signal,
            )
          : undefined;
      if (!current(operation.epoch, operation.source)) return;
      setup.value = value;
      needsReadback.value = false;
      if (value?.state !== "UNKNOWN") capability = undefined;
    } catch (error) {
      if (current(operation.epoch, operation.source))
        problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch, operation.source)) busy.value = false;
    }
  }

  async function observe(): Promise<void> {
    const operation = begin();
    if (!operation) return;
    const saved = setup.value;
    const upload = capability;
    try {
      if (!saved || !upload || needsReadback.value || saved.state !== "UNKNOWN")
        throw new Error("SSH material observation is unavailable");
      const fresh = await port.readSSHSetup(
        operation.context.connectionRef,
        saved.ref,
        operation.signal,
      );
      if (!current(operation.epoch, operation.source)) return;
      setup.value = fresh;
      if (fresh.state !== "UNKNOWN") {
        capability = undefined;
        return;
      }
      needsReadback.value = true;
      let failure: unknown;
      try {
        await port.uploadSSHMaterial(
          upload,
          fresh.ref,
          undefined,
          operation.signal,
        );
      } catch (error) {
        failure = error;
      }
      if (!current(operation.epoch, operation.source)) return;
      const result = await port.readSSHSetup(
        operation.context.connectionRef,
        fresh.ref,
        operation.signal,
      );
      if (!current(operation.epoch, operation.source)) return;
      setup.value = result;
      needsReadback.value = false;
      if (failure) problem.value = asProblem(failure);
      if (result.state !== "UNKNOWN") capability = undefined;
    } catch (error) {
      if (current(operation.epoch, operation.source))
        problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch, operation.source)) busy.value = false;
    }
  }

  async function cancel(): Promise<void> {
    const operation = begin();
    if (!operation) return;
    try {
      if (!setup.value) return;
      const value = await port.cancelSSHSetup(setup.value, operation.signal);
      if (!current(operation.epoch, operation.source)) return;
      setup.value = value;
      capability = undefined;
      privateKey.value = "";
      needsReadback.value = false;
    } catch (error) {
      if (current(operation.epoch, operation.source))
        problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch, operation.source)) busy.value = false;
    }
  }

  function canObserve(): boolean {
    return (
      !!capability &&
      setup.value?.state === "UNKNOWN" &&
      !needsReadback.value &&
      !busy.value &&
      !disposed &&
      !owner.aborted
    );
  }
  return {
    privateKey,
    setup,
    pendingBeginKey,
    busy,
    problem,
    needsReadback,
    unavailable,
    submit,
    refresh,
    observe,
    cancel,
    canObserve,
    suspend,
    resetContext,
  };
}
