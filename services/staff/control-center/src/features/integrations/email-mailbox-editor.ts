import { computed, ref, shallowRef } from "vue";
import type {
  EmailMailboxConfigurationView,
  EmailMailboxDraftContent,
  EmailMailboxSpecification,
  EmailMailboxDiagnostic,
  EmailMailboxActionAvailability,
} from "@/shared/api/generated/openapi/types.gen";
import { idempotencyKey } from "@/shared/api/mutation";
import { AppProblem, asProblem } from "@/shared/api/problem";
import {
  bindMailbox,
  changeMailboxDraft,
  changeMailboxSource,
  createMailboxDraft,
  listMailboxes,
  previewMailbox,
  readMailbox,
  saveMailboxDraft,
  unbindMailbox,
  type MailboxAction,
} from "./email-mailbox-api";

export function mailboxEditor(connectionRef: string) {
  const view = shallowRef<EmailMailboxConfigurationView>();
  const list = shallowRef<EmailMailboxConfigurationView[]>([]);
  const listActions = shallowRef<EmailMailboxActionAvailability[]>([]);
  const nextPageToken = ref("");
  const total = ref(0);
  const query = ref("");
  const name = ref("");
  const copyName = ref("");
  const specification = shallowRef<EmailMailboxSpecification>({});
  const yaml = ref("");
  const mode = ref<"FORM" | "YAML">("FORM");
  const diagnostics = shallowRef<EmailMailboxDiagnostic[]>([]);
  const previewYaml = ref("");
  const previewSource = ref("");
  const busy = ref(false);
  const problem = shallowRef<AppProblem>();
  const uncertain = ref(false);
  let generation = 0;
  let disposed = false;
  let controller: AbortController | undefined;
  let baseline = "";
  let pending: (() => Promise<EmailMailboxConfigurationView>) | undefined;
  const content = (): EmailMailboxDraftContent =>
    mode.value === "YAML"
      ? { yaml: yaml.value }
      : { specification: structuredClone(specification.value) };
  const fingerprint = () =>
    JSON.stringify({
      name: name.value,
      copyName: copyName.value,
      content: content(),
    });
  const dirty = computed(() => Boolean(baseline) && fingerprint() !== baseline);
  const allowed = (action: MailboxAction) =>
    (view.value?.nextActions ?? listActions.value).find(
      (item) => item.action === action,
    )?.enabled === true;
  const writable = computed(
    () => allowed("SAVE") || (!view.value && allowed("CREATE_DRAFT")),
  );
  function accept(value: EmailMailboxConfigurationView): void {
    view.value = value;
    name.value = value.configuration.name;
    copyName.value = "";
    specification.value = structuredClone(value.specification);
    yaml.value = value.revision.content;
    diagnostics.value = value.diagnostics;
    previewYaml.value = "";
    previewSource.value = "";
    baseline = fingerprint();
  }
  function fail(error: unknown): void {
    const value = asProblem(error);
    problem.value = new AppProblem({
      status: value.status,
      code: value.code,
      retryable: value.retryable,
      kind: value.kind,
    });
  }
  async function task(
    work: (signal: AbortSignal, current: () => boolean) => Promise<void>,
  ): Promise<void> {
    if (busy.value || disposed) return;
    const token = ++generation;
    controller?.abort();
    controller = new AbortController();
    const signal = controller.signal;
    busy.value = true;
    problem.value = undefined;
    const current = () => token === generation && !disposed && !signal.aborted;
    try {
      await work(signal, current);
    } catch (error) {
      if (current()) fail(error);
    } finally {
      if (current()) busy.value = false;
    }
  }
  async function catalog(more = false): Promise<void> {
    if (uncertain.value) return;
    await task(async (signal, current) => {
      const page = await listMailboxes(
        connectionRef,
        query.value,
        signal,
        more ? nextPageToken.value : undefined,
      );
      if (!current()) return;
      const items = more ? [...list.value, ...page.items] : page.items;
      if (
        new Set(items.map((item) => item.configuration.ref)).size !==
        items.length
      )
        throw new Error("Mailbox catalog contains repeated configuration");
      list.value = items;
      listActions.value = page.nextActions;
      total.value = page.total;
      nextPageToken.value = page.nextPageToken;
      if (!baseline) baseline = fingerprint();
    });
  }
  async function open(
    configurationRef?: string,
    revisionRef?: string,
  ): Promise<void> {
    if (uncertain.value) return;
    await task(async (signal, current) => {
      try {
        const result = await readMailbox(
          connectionRef,
          signal,
          configurationRef,
          revisionRef,
        );
        if (current()) accept(result);
      } catch (error) {
        if (asProblem(error).status !== 404 || configurationRef) throw error;
        if (current()) {
          view.value = undefined;
          baseline = fingerprint();
        }
      }
    });
  }
  function newConfiguration(): void {
    if (
      busy.value ||
      uncertain.value ||
      !listActions.value.some(
        (item) => item.action === "CREATE_DRAFT" && item.enabled,
      )
    )
      return;
    view.value = undefined;
    name.value = "";
    copyName.value = "";
    specification.value = {};
    yaml.value = "";
    mode.value = "FORM";
    diagnostics.value = [];
    previewYaml.value = "";
    previewSource.value = "";
    baseline = fingerprint();
  }
  async function preview(target?: "FORM" | "YAML"): Promise<void> {
    if (uncertain.value) return;
    const snapshot = content();
    const before = fingerprint();
    await task(async (signal, current) => {
      const result = await previewMailbox(connectionRef, snapshot, signal);
      if (!current() || fingerprint() !== before) return;
      diagnostics.value = result.diagnostics;
      previewYaml.value = result.canonicalYaml;
      previewSource.value = before;
      if (!target || !result.specification) return;
      const wasDirty = dirty.value;
      specification.value = result.specification;
      yaml.value = result.canonicalYaml;
      mode.value = target;
      if (!wasDirty) baseline = fingerprint();
      previewSource.value = fingerprint();
    });
  }
  async function execute(action?: MailboxAction): Promise<void> {
    if (!pending && (!action || !allowed(action))) return;
    await task(async (signal, current) => {
      if (!pending) {
        if (!action) return;
        const snapshot = content();
        const original = view.value;
        const draftName = name.value;
        const copiedName = copyName.value;
        const key = idempotencyKey();
        if (original) {
          const fresh = await readMailbox(
            connectionRef,
            signal,
            original.configuration.ref,
            original.revision.ref,
          );
          if (!current()) return;
          if (
            fresh.configuration.version !== original.configuration.version ||
            fresh.connectionVersion !== original.connectionVersion ||
            !fresh.nextActions.some(
              (item) => item.action === action && item.enabled,
            )
          )
            throw new AppProblem({
              status: 412,
              code: "PRECONDITION_FAILED",
              kind: "conflict",
              retryable: false,
            });
        } else {
          if (action !== "CREATE_DRAFT") return;
          const page = await listMailboxes(connectionRef, "", signal);
          if (!current()) return;
          listActions.value = page.nextActions;
          if (
            !page.nextActions.some(
              (item) => item.action === action && item.enabled,
            )
          )
            return;
        }
        if (action === "CREATE_DRAFT")
          pending = () =>
            createMailboxDraft(
              connectionRef,
              {
                name: draftName,
                ...(original
                  ? { configurationRef: original.configuration.ref }
                  : {}),
                content: snapshot,
              },
              key,
              original?.configuration.version,
            );
        else if (original && action === "SAVE")
          pending = () => saveMailboxDraft(original, snapshot, key);
        else if (
          original &&
          (action === "VALIDATE" ||
            action === "PUBLISH" ||
            action === "DISCARD")
        ) {
          const command =
            action === "VALIDATE"
              ? "validate"
              : action === "PUBLISH"
                ? "publish"
                : "discard";
          pending = () => changeMailboxDraft(original, command, key);
        } else if (original && action === "BIND")
          pending = () => bindMailbox(original, key);
        else if (original && (action === "DETACH" || action === "COPY"))
          pending = () =>
            changeMailboxSource(
              original,
              action,
              copiedName,
              key,
              controller?.signal ?? signal,
            );
        else if (original && action === "UNBIND")
          pending = async () => {
            await unbindMailbox(original, key);
            return readMailbox(
              connectionRef,
              controller?.signal ?? signal,
              original.configuration.ref,
              original.revision.ref,
            );
          };
      }
      if (!pending) return;
      try {
        const result = await pending();
        if (!current()) return;
        pending = undefined;
        uncertain.value = false;
        accept(result);
      } catch (error) {
        if (!current()) return;
        const status = asProblem(error).status;
        if (
          [400, 401, 403, 404, 409, 412, 413, 422].includes(status) &&
          !uncertain.value
        )
          pending = undefined;
        uncertain.value = Boolean(pending);
        throw error;
      }
    });
  }
  function dispose(): void {
    disposed = true;
    generation++;
    controller?.abort();
    pending = undefined;
    busy.value = false;
  }
  return {
    view,
    list,
    listActions,
    nextPageToken,
    total,
    query,
    name,
    copyName,
    specification,
    yaml,
    mode,
    diagnostics,
    previewYaml,
    previewSource,
    busy,
    problem,
    uncertain,
    dirty,
    writable,
    allowed,
    fingerprint,
    catalog,
    open,
    newConfiguration,
    preview,
    execute,
    dispose,
  };
}
