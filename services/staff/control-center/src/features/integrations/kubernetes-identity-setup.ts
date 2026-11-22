import {
  computed,
  onScopeDispose,
  ref,
  shallowRef,
  watch,
  type Ref,
} from "vue";
import * as api from "./kubernetes-identity-setup-api";
import type {
  KubernetesIdentitySetupAdmission,
  KubernetesIdentitySetupView,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { idempotencyKey } from "@/shared/api/mutation";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export const kubernetesIdentityInspectCommand =
  "python3 -I -B tools/dev/managed-kubernetes-identity.py inspect --kubeconfig <private-export-file>";
export const kubernetesIdentityProtectCommand = "chmod 600 <admission-file>";
export const kubernetesIdentityUploadCommand =
  "python3 -I -B tools/dev/managed-kubernetes-identity.py upload --admission-file <admission-file> --kubeconfig <private-export-file>";
export type KubernetesSetupAPI = Pick<
  typeof api,
  "beginKubernetesSetup" | "getKubernetesSetup" | "cancelKubernetesSetup"
>;
const terminal = new Set(["COMPLETED", "CANCELLED", "EXPIRED", "ACK_REJECTED"]);

// Permit не является reactive/public view и не попадает в storage/DOM/URL.
// Потерянная mutation сверяется только GET; повторного Begin либо Cancel нет.
export function useKubernetesIdentitySetup(
  context: Ref<api.KubernetesSetupContext>,
  disabled: Ref<boolean>,
  port: KubernetesSetupAPI = api,
) {
  const contentSHA256 = ref("");
  const contentBytes = ref("");
  const selectorKind = ref<"SETUP" | "BEGIN_KEY">("BEGIN_KEY");
  const selector = ref("");
  const beginKey = ref("");
  const setup = shallowRef<KubernetesIdentitySetupView>();
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const uncertain = ref(false);
  const hasAdmission = ref(false);
  const inactive = ref(false);
  const requiresConnectionRefresh = ref(false);
  const owner = ownerInvalidationSignal();
  let admission: KubernetesIdentitySetupAdmission | undefined;
  let recovery: api.KubernetesSetupSelector | undefined;
  let controller: AbortController | undefined;
  let generation = 0;
  let disposed = false;
  function clearAdmission() {
    admission = undefined;
    hasAdmission.value = false;
  }
  function invalidate() {
    generation++;
    controller?.abort();
    controller = undefined;
    busy.value = false;
    clearAdmission();
  }
  function suspend() {
    if (busy.value || hasAdmission.value) uncertain.value = true;
    invalidate();
  }
  function reset() {
    invalidate();
    setup.value = undefined;
    beginKey.value = "";
    selector.value = "";
    contentSHA256.value = "";
    contentBytes.value = "";
    uncertain.value = false;
    recovery = undefined;
    requiresConnectionRefresh.value = false;
    problem.value = undefined;
  }
  function close() {
    disposed = true;
    inactive.value = true;
    reset();
  }
  owner.addEventListener("abort", close, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", close);
    close();
  });
  // Сравнивается весь immutable UI context: даже возврат к прежнему Ref после
  // смены пользователя/версии не позволяет принять старый response.
  watch(() => JSON.stringify(context.value), reset, { flush: "sync" });
  const usable = computed(
    () =>
      !inactive.value &&
      !disabled.value &&
      api.kubernetesSetupContextValid(context.value) &&
      context.value.available &&
      context.value.enabled,
  );
  const requirements = computed(() =>
    api.kubernetesSetupRequirements(context.value),
  );
  const exportCommand = computed(() => {
    if (!requirements.value) return "";
    // POSIX single quotes сохраняют opaque context одним буквальным аргументом;
    // кавычки и metacharacters не превращают его в исполняемый shell input.
    const quoted =
      "'" +
      requirements.value.canonicalContext.replaceAll("'", "'\"'\"'") +
      "'";
    return (
      "python3 -I -B tools/dev/managed-kubernetes-identity.py export --kubectl <absolute-kubectl-binary> --kubeconfig <local-source-file> --context <selected-local-context> --target-context " +
      quoted +
      " --output <new-private-export-file>"
    );
  });
  const canBegin = computed(
    () =>
      usable.value &&
      !!requirements.value &&
      !busy.value &&
      !uncertain.value &&
      !requiresConnectionRefresh.value &&
      (!setup.value || terminal.has(setup.value.state)) &&
      /^[a-f0-9]{64}$/.test(contentSHA256.value) &&
      /^[1-9][0-9]{0,6}$/.test(contentBytes.value) &&
      Number(contentBytes.value) <= 1 << 20,
  );
  const canCancel = computed(
    () =>
      usable.value &&
      !busy.value &&
      !uncertain.value &&
      !!setup.value &&
      !terminal.has(setup.value.state),
  );
  function start() {
    controller?.abort();
    controller = new AbortController();
    busy.value = true;
    problem.value = undefined;
    return {
      epoch: ++generation,
      snapshot: { ...context.value },
      signal: AbortSignal.any([owner, controller.signal]),
    };
  }
  function current(epoch: number) {
    return !disposed && !owner.aborted && epoch === generation;
  }
  async function begin() {
    if (!canBegin.value) return;
    clearAdmission();
    const operation = start();
    const key = idempotencyKey();
    beginKey.value = key;
    selectorKind.value = "BEGIN_KEY";
    selector.value = key;
    recovery = { beginIdempotencyKey: key };
    // Intent сохраняется до I/O. Ни timeout, ни invalid ACK не разрешает
    // вторую запись; пользователь видит исходный recovery selector.
    uncertain.value = true;
    try {
      const result = await port.beginKubernetesSetup(
        operation.snapshot,
        contentSHA256.value,
        Number(contentBytes.value),
        key,
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      setup.value = api.kubernetesSetupView(result.setup, operation.snapshot);
      admission = api.kubernetesSetupAdmission(result, operation.snapshot);
      hasAdmission.value = !result.reconcileOnly;
      requiresConnectionRefresh.value = terminal.has(result.setup.state);
      uncertain.value = false;
      recovery = undefined;
      selectorKind.value = "SETUP";
      selector.value = result.setup.setupRef;
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
  }
  async function refresh() {
    if (!usable.value || busy.value || !selector.value) return;
    const selected: api.KubernetesSetupSelector =
      selectorKind.value === "SETUP"
        ? { setupRef: selector.value }
        : { beginIdempotencyKey: selector.value };
    if (recovery && JSON.stringify(recovery) !== JSON.stringify(selected))
      return;
    clearAdmission();
    const operation = start();
    try {
      const result = await port.getKubernetesSetup(
        operation.snapshot,
        selected,
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      const view = api.kubernetesSetupView(result, operation.snapshot);
      if (
        setup.value?.setupRef === view.setupRef &&
        (!api.sameKubernetesSetupIdentity(setup.value, view) ||
          view.version < setup.value.version)
      )
        throw new Error("Kubernetes setup identity changed");
      setup.value = view;
      requiresConnectionRefresh.value = terminal.has(view.state);
      selectorKind.value = "SETUP";
      selector.value = result.setupRef;
      uncertain.value = false;
      recovery = undefined;
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
  }
  async function cancel() {
    if (!canCancel.value || !setup.value) return;
    clearAdmission();
    const previous = setup.value,
      operation = start();
    uncertain.value = true;
    selectorKind.value = "SETUP";
    selector.value = previous.setupRef;
    recovery = { setupRef: previous.setupRef };
    try {
      const result = await port.cancelKubernetesSetup(
        operation.snapshot,
        previous,
        idempotencyKey(),
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      const view = api.kubernetesSetupView(result, operation.snapshot);
      if (
        !api.sameKubernetesSetupIdentity(previous, view) ||
        view.version < previous.version
      )
        throw new Error("Kubernetes setup cancellation response is invalid");
      setup.value = view;
      requiresConnectionRefresh.value = terminal.has(view.state);
      uncertain.value = false;
      recovery = undefined;
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
  }
  function download() {
    if (
      !usable.value ||
      busy.value ||
      !admission ||
      !hasAdmission.value ||
      setup.value?.state !== "PREPARED"
    )
      return;
    // Передаётся исходный server admission, без добавления target/identity или
    // преобразования permit в CLI args. Object URL освобождается после скачивания.
    const blob = new Blob([JSON.stringify(admission) + "\n"], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob),
      link = document.createElement("a");
    link.href = url;
    link.download = "kubernetes-identity-admission.json";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }
  return {
    requirements,
    exportCommand,
    contentSHA256,
    contentBytes,
    selectorKind,
    selector,
    beginKey,
    setup,
    busy,
    problem,
    uncertain,
    hasAdmission,
    inactive,
    requiresConnectionRefresh,
    usable,
    canBegin,
    canCancel,
    begin,
    refresh,
    cancel,
    download,
    suspend,
  };
}
