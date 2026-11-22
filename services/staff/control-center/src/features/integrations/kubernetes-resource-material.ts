import {
  computed,
  reactive,
  onScopeDispose,
  ref,
  shallowRef,
  watch,
  type Ref,
} from "vue";
import * as api from "./kubernetes-resource-material-api";
import type {
  KubernetesResourceMaterialAdmission,
  KubernetesResourceMaterialView,
} from "@/shared/api/generated/openapi/types.gen";
import { kubernetesSetupContextValid } from "./kubernetes-identity-setup-api";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { idempotencyKey } from "@/shared/api/mutation";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export const kubernetesMaterialInspectCommand =
  "python3 -I -B tools/dev/managed-kubernetes-resource-material.py inspect --secret-file <local-path> --operation <operation>";
export const kubernetesMaterialProtectCommand = "chmod 600 <admission-file>";
export const kubernetesMaterialUploadCommand =
  "python3 -I -B tools/dev/managed-kubernetes-resource-material.py upload --admission-file <admission-file> --secret-file <local-path> --operation <operation>";
export type KubernetesMaterialAPI = Pick<
  typeof api,
  | "beginKubernetesMaterial"
  | "getKubernetesMaterial"
  | "cancelKubernetesMaterial"
>;
const terminal = new Set(["CANCELLED", "EXPIRED", "ACK_REJECTED", "CONSUMED"]);

// Permit не является reactive/public view и не попадает в storage/DOM/URL.
// Потерянная mutation сверяется только GET; повторного Begin либо Cancel нет.
export function useKubernetesResourceMaterial(
  context: Ref<api.KubernetesMaterialContext>,
  disabled: Ref<boolean>,
  port: KubernetesMaterialAPI = api,
) {
  const destination = reactive<
    Pick<
      api.KubernetesMaterialMetadata,
      | "operation"
      | "namespace"
      | "name"
      | "generateName"
      | "secretType"
      | "patchType"
    >
  >({
    operation: "kubernetes.create",
    namespace: "",
    name: "",
    generateName: "",
    secretType: "Opaque",
    patchType: "",
  });
  watch(
    () => destination.operation,
    (operation) => {
      if (operation !== "kubernetes.create") destination.generateName = "";
      if (operation !== "kubernetes.patch") destination.patchType = "";
    },
  );
  const contentSHA256 = ref("");
  const contentBytes = ref("");
  const selectorKind = ref<"MATERIAL" | "BEGIN_KEY">("BEGIN_KEY");
  const selector = ref("");
  const beginKey = ref("");
  const material = shallowRef<KubernetesResourceMaterialView>();
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const uncertain = ref(false);
  const hasAdmission = ref(false);
  const inactive = ref(false);
  const owner = ownerInvalidationSignal();
  let admission: KubernetesResourceMaterialAdmission | undefined;
  let recovery: api.KubernetesMaterialSelector | undefined;
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
    material.value = undefined;
    beginKey.value = "";
    selector.value = "";
    contentSHA256.value = "";
    contentBytes.value = "";
    uncertain.value = false;
    recovery = undefined;
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
      kubernetesSetupContextValid(context.value) &&
      context.value.available &&
      context.value.enabled,
  );
  const canBegin = computed(
    () =>
      usable.value &&
      !busy.value &&
      !uncertain.value &&
      (!material.value || terminal.has(material.value.state)) &&
      /^[a-f0-9]{64}$/.test(contentSHA256.value) &&
      /^[1-9][0-9]{0,6}$/.test(contentBytes.value) &&
      Number(contentBytes.value) <= 1 << 20 &&
      api.materialDestinationValid(destination),
  );
  const canCancel = computed(
    () =>
      usable.value &&
      !busy.value &&
      !uncertain.value &&
      !!material.value &&
      !terminal.has(material.value.state),
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
      const result = await port.beginKubernetesMaterial(
        operation.snapshot,
        {
          ...destination,
          contentSHA256: contentSHA256.value,
          contentBytes: Number(contentBytes.value),
        },
        key,
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      material.value = api.kubernetesMaterialView(
        result.material,
        operation.snapshot,
      );
      admission = api.kubernetesMaterialAdmission(result, operation.snapshot);
      hasAdmission.value = !result.reconcileOnly;
      uncertain.value = false;
      recovery = undefined;
      selectorKind.value = "MATERIAL";
      selector.value = result.material.materialRef;
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
  }
  async function refresh() {
    if (!usable.value || busy.value || !selector.value) return;
    const selected: api.KubernetesMaterialSelector =
      selectorKind.value === "MATERIAL"
        ? { materialRef: selector.value }
        : { beginIdempotencyKey: selector.value };
    if (recovery && JSON.stringify(recovery) !== JSON.stringify(selected))
      return;
    clearAdmission();
    const operation = start();
    try {
      const result = await port.getKubernetesMaterial(
        operation.snapshot,
        selected,
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      const view = api.kubernetesMaterialView(result, operation.snapshot);
      if (
        material.value?.materialRef === view.materialRef &&
        (!api.sameKubernetesMaterial(material.value, view) ||
          view.version < material.value.version)
      )
        throw new Error("Kubernetes material identity changed");
      material.value = view;
      selectorKind.value = "MATERIAL";
      selector.value = result.materialRef;
      uncertain.value = false;
      recovery = undefined;
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
  }
  async function cancel() {
    if (!canCancel.value || !material.value) return;
    clearAdmission();
    const previous = material.value,
      operation = start();
    uncertain.value = true;
    selectorKind.value = "MATERIAL";
    selector.value = previous.materialRef;
    recovery = { materialRef: previous.materialRef };
    try {
      const result = await port.cancelKubernetesMaterial(
        operation.snapshot,
        previous,
        idempotencyKey(),
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      const view = api.kubernetesMaterialView(result, operation.snapshot);
      if (
        !api.sameKubernetesMaterial(previous, view) ||
        view.version < previous.version
      )
        throw new Error("Kubernetes material cancellation response is invalid");
      material.value = view;
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
      material.value?.state !== "PREPARED"
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
    link.download = "kubernetes-resource-material-admission.json";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }
  return {
    destination,
    contentSHA256,
    contentBytes,
    selectorKind,
    selector,
    beginKey,
    material,
    busy,
    problem,
    uncertain,
    hasAdmission,
    inactive,
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
