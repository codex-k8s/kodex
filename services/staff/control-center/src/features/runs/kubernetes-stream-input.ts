import {
  computed,
  onScopeDispose,
  ref,
  shallowRef,
  watch,
  type Ref,
} from "vue";
import * as api from "@/shared/api/kubernetes-stream-input";
import type {
  KubernetesStreamInputView,
  KubernetesStreamInputReadback,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { idempotencyKey } from "@/shared/api/mutation";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export type KubernetesInputAPI = Pick<
  typeof api,
  | "listKubernetesStreamInputs"
  | "readKubernetesStreamInput"
  | "submitKubernetesStreamInput"
  | "stopKubernetesStream"
  | "kubernetesInputDigest"
>;
const active = new Set(["PREPARED", "ACTIVE"]);

// Только exact owner view; выбор stream явный. После потерянного write ACK выполняется один Get без повторной отправки bytes.
export function useKubernetesStreamInput(
  context: Ref<api.KubernetesInputContext>,
  port: KubernetesInputAPI = api,
) {
  const items = shallowRef<KubernetesStreamInputView[]>([]);
  const selected = shallowRef<KubernetesStreamInputView>();
  const nextPageToken = ref("");
  const draft = ref("");
  const columns = ref(80),
    rows = ref(24);
  const busy = ref(false),
    writing = ref(false),
    uncertain = ref(false),
    inactive = ref(false);
  const problem = ref<AppProblem>();
  const owner = ownerInvalidationSignal();
  let disposed = false,
    generation = 0,
    controller: AbortController | undefined,
    timer: ReturnType<typeof setTimeout> | undefined;
  let key: string | undefined;
  function clearTimer() {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
  }
  function invalidate() {
    generation++;
    controller?.abort();
    controller = undefined;
    clearTimer();
    busy.value = false;
    writing.value = false;
    draft.value = "";
    key = undefined;
  }
  function reset() {
    invalidate();
    items.value = [];
    selected.value = undefined;
    nextPageToken.value = "";
    uncertain.value = false;
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
  function start() {
    clearTimer();
    controller?.abort();
    controller = new AbortController();
    busy.value = true;
    return { token: ++generation, signal: controller.signal };
  }
  function current(token: number, signal: AbortSignal) {
    return (
      !disposed && !inactive.value && !signal.aborted && token === generation
    );
  }
  function schedule() {
    clearTimer();
    if (
      !disposed &&
      !inactive.value &&
      !uncertain.value &&
      selected.value &&
      active.has(selected.value.state)
    )
      timer = setTimeout(() => void refresh(), 2000);
  }
  function accept(result: KubernetesStreamInputReadback) {
    selected.value = result.view;
    if (result.submission?.state === "UNKNOWN") uncertain.value = true;
    if (
      result.submission &&
      ["ACKNOWLEDGED", "REJECTED", "CANCELLED"].includes(
        result.submission.state,
      )
    )
      key = undefined;
  }
  async function discover(pageToken = "") {
    if (
      busy.value ||
      inactive.value ||
      !api.kubernetesInputContextValid(context.value)
    )
      return;
    const scope = start();
    problem.value = undefined;
    try {
      const page = await port.listKubernetesStreamInputs(
        { ...context.value },
        scope.signal,
        pageToken,
      );
      if (!current(scope.token, scope.signal)) return;
      items.value = page.items;
      nextPageToken.value = page.nextPageToken;
    } catch (error) {
      if (current(scope.token, scope.signal)) {
        problem.value = asProblem(error);
        forgetHidden(error);
      }
    } finally {
      if (current(scope.token, scope.signal)) {
        busy.value = false;
        if (!selected.value && !problem.value)
          timer = setTimeout(() => void discover(pageToken), 2000);
      }
    }
  }
  function unselect() {
    if (busy.value || writing.value) return;
    reset();
    void discover();
  }
  async function choose(stream: KubernetesStreamInputView) {
    if (busy.value || writing.value || !items.value.includes(stream)) return;
    invalidate();
    selected.value = stream;
    uncertain.value = false;
    problem.value = undefined;
    await refresh();
  }
  function forgetHidden(error: unknown) {
    if ([401, 403, 404].includes(asProblem(error).status)) {
      selected.value = undefined;
      items.value = [];
      nextPageToken.value = "";
      draft.value = "";
      key = undefined;
    }
  }
  async function refresh() {
    if (busy.value || writing.value || inactive.value || !selected.value)
      return;
    const original = selected.value;
    const scope = start();
    problem.value = undefined;
    try {
      const result = await port.readKubernetesStreamInput(
        original,
        scope.signal,
        key,
      );
      if (current(scope.token, scope.signal)) accept(result);
    } catch (error) {
      if (current(scope.token, scope.signal)) {
        problem.value = asProblem(error);
        forgetHidden(error);
      }
    } finally {
      if (current(scope.token, scope.signal)) {
        busy.value = false;
        if (!problem.value) schedule();
      }
    }
  }
  const live = computed(
    () =>
      !inactive.value &&
      !!selected.value &&
      active.has(selected.value.state) &&
      Date.parse(selected.value.hardDeadline) > Date.now(),
  );
  const canSend = computed(
    () =>
      live.value &&
      !busy.value &&
      !writing.value &&
      !uncertain.value &&
      !problem.value &&
      !selected.value?.pending &&
      selected.value?.inputChannels.includes("STDIN") &&
      !selected.value.closedInputChannels.includes("STDIN"),
  );
  const canResize = computed(
    () =>
      live.value &&
      !busy.value &&
      !writing.value &&
      !uncertain.value &&
      !problem.value &&
      !selected.value?.pending &&
      selected.value?.inputChannels.includes("TERMINAL_RESIZE") &&
      Number.isInteger(columns.value) &&
      columns.value >= 1 &&
      columns.value <= 1024 &&
      Number.isInteger(rows.value) &&
      rows.value >= 1 &&
      rows.value <= 512,
  );
  const canStop = computed(
    () =>
      !inactive.value &&
      !busy.value &&
      !writing.value &&
      !!selected.value &&
      active.has(selected.value.state),
  );
  function lostACK(error: unknown) {
    const value = asProblem(error);
    return (
      !(error instanceof api.KubernetesInputProtocolError) &&
      (value.status === 0 || value.status >= 500 || value.status === 408)
    );
  }
  async function submit(
    channel: api.KubernetesPublicInputChannel,
    bytes: Uint8Array,
    eof: boolean,
  ) {
    if (
      !selected.value ||
      (channel === "STDIN" ? !canSend.value : !canResize.value)
    ) {
      bytes.fill(0);
      return;
    }
    const original = selected.value;
    const scope = start();
    writing.value = true;
    problem.value = undefined;
    const mutationKey = idempotencyKey();
    key = mutationKey;
    draft.value = "";
    try {
      const expectedDigest = await port.kubernetesInputDigest(bytes);
      const expectedBytes = bytes.length;
      if (!current(scope.token, scope.signal)) return;
      let result: KubernetesStreamInputReadback;
      try {
        result = await port.submitKubernetesStreamInput(
          original,
          channel,
          bytes,
          eof,
          scope.signal,
          mutationKey,
        );
      } catch (error) {
        if (!current(scope.token, scope.signal) || !lostACK(error)) throw error;
        // Один metadata read в том же owner scope; payload не передаётся повторно.
        result = await port.readKubernetesStreamInput(
          original,
          scope.signal,
          mutationKey,
        );
        if (
          !result.submission ||
          result.submission.channel !== channel ||
          result.submission.payloadDigest !== expectedDigest ||
          result.submission.byteCount !== expectedBytes ||
          result.submission.endOfInput !== eof
        )
          throw new api.KubernetesInputProtocolError();
      }
      if (current(scope.token, scope.signal)) accept(result);
    } catch (error) {
      if (current(scope.token, scope.signal)) {
        uncertain.value =
          lostACK(error) || error instanceof api.KubernetesInputProtocolError;
        problem.value = asProblem(error);
      }
    } finally {
      bytes.fill(0);
      if (current(scope.token, scope.signal)) {
        writing.value = false;
        busy.value = false;
        if (!problem.value) schedule();
      }
    }
  }
  async function send() {
    if (!draft.value) return;
    await submit("STDIN", new TextEncoder().encode(draft.value), false);
  }
  async function eof() {
    await submit("STDIN", new Uint8Array(), true);
  }
  async function resize() {
    await submit(
      "TERMINAL_RESIZE",
      new TextEncoder().encode(
        JSON.stringify({ columns: columns.value, rows: rows.value }),
      ),
      false,
    );
  }
  async function stop() {
    if (!canStop.value || !selected.value) return;
    const original = selected.value;
    const scope = start();
    writing.value = true;
    draft.value = "";
    problem.value = undefined;
    try {
      let view: KubernetesStreamInputView;
      try {
        view = await port.stopKubernetesStream(
          original,
          scope.signal,
          idempotencyKey(),
        );
      } catch (error) {
        if (!current(scope.token, scope.signal) || !lostACK(error)) throw error;
        view = (await port.readKubernetesStreamInput(original, scope.signal))
          .view;
        if (active.has(view.state))
          throw new api.KubernetesInputProtocolError();
      }
      if (current(scope.token, scope.signal)) {
        selected.value = view;
        uncertain.value = false;
        key = undefined;
      }
    } catch (error) {
      if (current(scope.token, scope.signal)) {
        uncertain.value = true;
        problem.value = asProblem(error);
        forgetHidden(error);
      }
    } finally {
      if (current(scope.token, scope.signal)) {
        busy.value = false;
        writing.value = false;
      }
    }
  }
  watch(
    () => JSON.stringify(context.value),
    () => {
      reset();
      void discover();
    },
    { immediate: true, flush: "sync" },
  );
  return {
    items,
    selected,
    nextPageToken,
    draft,
    columns,
    rows,
    busy,
    writing,
    uncertain,
    problem,
    inactive,
    canSend,
    canResize,
    canStop,
    discover,
    choose,
    unselect,
    refresh,
    send,
    eof,
    resize,
    stop,
  };
}
