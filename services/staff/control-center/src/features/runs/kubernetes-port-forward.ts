import {
  computed,
  onScopeDispose,
  ref,
  shallowRef,
  watch,
  type Ref,
} from "vue";
import * as api from "@/shared/api/kubernetes-port-forward";
import {
  readKubernetesStreamInput,
  KubernetesInputProtocolError,
} from "@/shared/api/kubernetes-stream-input";
import type {
  KubernetesPortForwardConnectionView as Connection,
  KubernetesPortForwardConnectionProfile as Profile,
  KubernetesStreamInputView as Input,
  KubernetesPortForwardConnectionReadback as Readback,
} from "@/shared/api/generated/openapi/types.gen";
import { idempotencyKey } from "@/shared/api/mutation";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";
export type PortForwardAPI = typeof api & {
  readInput: typeof readKubernetesStreamInput;
};
const closed = new Set(["CLOSED", "CANCELLED", "EXPIRED"]);
// UI управляет exact logical connection; браузер не объявляется TCP listener либо protected OUTPUT reader.
export function useKubernetesPortForward(
  context: Ref<Input>,
  port: PortForwardAPI = { ...api, readInput: readKubernetesStreamInput },
) {
  const input = shallowRef(context.value),
    items = shallowRef<Connection[]>([]),
    selected = shallowRef<Connection>(),
    profile = shallowRef<Profile>();
  const next = ref(""),
    slot = ref<number | undefined>(),
    draft = ref(""),
    busy = ref(false),
    uncertain = ref(false),
    problem = ref<AppProblem>();
  const owner = ownerInvalidationSignal();
  let lifetime = 0,
    disposed = false,
    controller: AbortController | undefined,
    timer: ReturnType<typeof setTimeout> | undefined,
    openKey: string | undefined,
    inputKey: string | undefined,
    closingRef: string | undefined;
  function clearTimer() {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  }
  function invalidate() {
    lifetime++;
    controller?.abort();
    clearTimer();
    busy.value = false;
    draft.value = "";
  }
  function forget() {
    items.value = [];
    selected.value = undefined;
    profile.value = undefined;
    next.value = "";
    slot.value = undefined;
    draft.value = "";
    openKey = undefined;
    inputKey = undefined;
    closingRef = undefined;
  }
  function dispose() {
    disposed = true;
    invalidate();
    forget();
  }
  owner.addEventListener("abort", dispose, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", dispose);
    dispose();
  });
  watch(
    () => [
      context.value.invocationRef,
      context.value.streamRef,
      context.value.generation,
    ],
    () => {
      invalidate();
      forget();
      uncertain.value = false;
      problem.value = undefined;
      input.value = context.value;
      void refresh();
    },
    { immediate: true },
  );
  watch(context, (value) => {
    if (
      value.invocationRef === input.value.invocationRef &&
      value.streamRef === input.value.streamRef &&
      value.version >= input.value.version
    ) {
      const changed = value.version > input.value.version;
      input.value = value;
      if (changed && !busy.value) void refresh();
    }
  });
  function start() {
    clearTimer();
    controller?.abort();
    controller = new AbortController();
    busy.value = true;
    return { token: ++lifetime, signal: controller.signal };
  }
  function current(scope: { token: number; signal: AbortSignal }) {
    return !disposed && !scope.signal.aborted && scope.token === lifetime;
  }
  function failure(error: unknown) {
    problem.value = asProblem(error);
    if ([401, 403, 404].includes(problem.value.status)) forget();
  }
  function accept(result: Readback) {
    selected.value = result.connection;
    items.value = items.value.map((item) =>
      item.connectionRef === result.connection.connectionRef
        ? result.connection
        : item,
    );
    if (result.submission?.state === "UNKNOWN") uncertain.value = true;
  }
  function schedule() {
    clearTimer();
    if (
      !disposed &&
      !uncertain.value &&
      !problem.value &&
      input.value.state === "ACTIVE"
    )
      timer = setTimeout(() => void refresh(), 2000);
  }
  async function refresh(pageToken = "") {
    if (busy.value || disposed) return;
    const scope = start();
    problem.value = undefined;
    try {
      const parent = await port.readInput(input.value, scope.signal, inputKey);
      if (!current(scope)) return;
      input.value = parent.view;
      if (inputKey) {
        uncertain.value =
          !parent.submission || parent.submission.state === "UNKNOWN";
        if (!uncertain.value) inputKey = undefined;
      }
      if (openKey) {
        const result = await port.getPortForwardConnection(
          input.value,
          scope.signal,
          { openIdempotencyKey: openKey },
        );
        if (!current(scope)) return;
        accept(result);
        openKey = undefined;
        uncertain.value = result.submission?.state === "UNKNOWN";
      }
      const page = await port.listPortForwardConnections(
        input.value,
        scope.signal,
        pageToken,
      );
      if (!current(scope)) return;
      profile.value = page.profile;
      items.value = page.items;
      next.value = page.nextPageToken;
      if (selected.value) {
        const result = await port.getPortForwardConnection(
          input.value,
          scope.signal,
          { connectionRef: selected.value.connectionRef },
        );
        if (current(scope)) {
          accept(result);
          if (
            closingRef === result.connection.connectionRef &&
            closed.has(result.connection.state)
          ) {
            closingRef = undefined;
            uncertain.value = false;
          }
        }
      }
    } catch (error) {
      if (current(scope)) failure(error);
    } finally {
      if (current(scope)) {
        busy.value = false;
        schedule();
      }
    }
  }
  async function choose(connection: Connection) {
    if (busy.value || !items.value.includes(connection)) return;
    selected.value = connection;
    draft.value = "";
    await refresh();
  }
  const live = computed(
    () =>
      input.value.state === "ACTIVE" &&
      Date.parse(input.value.hardDeadline) > Date.now(),
  );
  const canOpen = computed(
    () =>
      !disposed &&
      !busy.value &&
      !uncertain.value &&
      !problem.value &&
      live.value &&
      !input.value.pending &&
      profile.value?.protocol === "PORT_FORWARD_SPDY_V1" &&
      profile.value.admissionReasonCode === "" &&
      profile.value.activeConnections <
        profile.value.maximumActiveConnections &&
      Number.isInteger(slot.value) &&
      (slot.value ?? -1) >= 0 &&
      (slot.value ?? -1) < profile.value.ports.length,
  );
  const canSend = computed(
    () =>
      !disposed &&
      !busy.value &&
      !uncertain.value &&
      !problem.value &&
      live.value &&
      !input.value.pending &&
      selected.value?.state === "OPEN" &&
      selected.value.inputVersion >= input.value.version &&
      !selected.value.localWriteClosed,
  );
  const canClose = computed(
    () =>
      !disposed &&
      !busy.value &&
      !!selected.value &&
      selected.value.inputVersion >= input.value.version &&
      !closed.has(selected.value.state),
  );
  function lost(error: unknown) {
    const p = asProblem(error);
    return (
      !(error instanceof KubernetesInputProtocolError) &&
      (p.status === 0 || p.status === 408 || p.status >= 500)
    );
  }
  async function open() {
    if (!canOpen.value) return;
    const scope = start(),
      key = idempotencyKey(),
      chosen = slot.value ?? -1;
    problem.value = undefined;
    openKey = key;
    try {
      const result = await port.openPortForwardConnection(
        input.value,
        chosen,
        scope.signal,
        key,
      );
      if (!current(scope)) return;
      input.value = result.input;
      accept(result);
      openKey = undefined;
    } catch (error) {
      if (!current(scope)) return;
      if (lost(error)) {
        uncertain.value = true;
        try {
          const result = await port.getPortForwardConnection(
            input.value,
            scope.signal,
            { openIdempotencyKey: key },
          );
          if (!current(scope)) return;
          accept(result);
          const read = await port.readInput(input.value, scope.signal);
          if (!current(scope)) return;
          input.value = read.view;
          openKey = undefined;
          uncertain.value = result.submission?.state === "UNKNOWN";
        } catch (readError) {
          if (current(scope)) failure(readError);
        }
      } else {
        if (error instanceof KubernetesInputProtocolError)
          uncertain.value = true;
        else openKey = undefined;
        failure(error);
      }
    } finally {
      if (current(scope)) {
        busy.value = false;
        schedule();
      }
    }
  }
  async function closeConnection() {
    if (!canClose.value || !selected.value) return;
    const scope = start(),
      connection = selected.value,
      key = idempotencyKey();
    problem.value = undefined;
    draft.value = "";
    try {
      const result = await port.closePortForwardConnection(
        input.value,
        connection,
        scope.signal,
        key,
      );
      if (!current(scope)) return;
      input.value = result.input;
      accept(result);
    } catch (error) {
      if (!current(scope)) return;
      if (lost(error)) {
        uncertain.value = true;
        closingRef = connection.connectionRef;
        try {
          const result = await port.getPortForwardConnection(
            input.value,
            scope.signal,
            { connectionRef: connection.connectionRef },
          );
          if (current(scope)) {
            accept(result);
            uncertain.value = !closed.has(result.connection.state);
            if (!uncertain.value) closingRef = undefined;
          }
        } catch (readError) {
          if (current(scope)) failure(readError);
        }
      } else {
        if (error instanceof KubernetesInputProtocolError) {
          uncertain.value = true;
          closingRef = connection.connectionRef;
        }
        failure(error);
      }
    } finally {
      if (current(scope)) {
        busy.value = false;
        schedule();
      }
    }
  }
  async function send(halfClose = false) {
    if (!canSend.value || !selected.value) return;
    const bytes = halfClose
      ? new Uint8Array()
      : new TextEncoder().encode(draft.value);
    if (
      !halfClose &&
      (!bytes.length ||
        bytes.length > Math.min(32768, input.value.maximumFrameBytes) - 51)
    ) {
      bytes.fill(0);
      return;
    }
    draft.value = "";
    const scope = start(),
      connection = selected.value,
      key = idempotencyKey();
    inputKey = key;
    problem.value = undefined;
    try {
      const result = await port.sendPortForwardInput(
        input.value,
        connection,
        bytes,
        halfClose,
        scope.signal,
        key,
      );
      if (current(scope)) {
        input.value = result.view;
        if (result.submission?.state === "UNKNOWN") uncertain.value = true;
        else inputKey = undefined;
        const readback = await port.getPortForwardConnection(
          input.value,
          scope.signal,
          { connectionRef: connection.connectionRef },
        );
        if (current(scope)) accept(readback);
      }
    } catch (error) {
      if (!current(scope)) return;
      if (lost(error)) {
        uncertain.value = true;
        try {
          const result = await port.readInput(input.value, scope.signal, key);
          if (current(scope)) {
            input.value = result.view;
            uncertain.value =
              !result.submission || result.submission.state === "UNKNOWN";
            if (!uncertain.value) inputKey = undefined;
          }
        } catch (readError) {
          if (current(scope)) failure(readError);
        }
      } else {
        if (error instanceof KubernetesInputProtocolError)
          uncertain.value = true;
        else inputKey = undefined;
        failure(error);
      }
    } finally {
      bytes.fill(0);
      if (current(scope)) {
        busy.value = false;
        schedule();
      }
    }
  }
  return {
    input,
    items,
    selected,
    profile,
    next,
    slot,
    draft,
    busy,
    uncertain,
    problem,
    canOpen,
    canSend,
    canClose,
    refresh,
    choose,
    open,
    closeConnection,
    send,
  };
}
