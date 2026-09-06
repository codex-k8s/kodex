<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Link2, Unlink, RefreshCw } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import type { ContextKind } from "./api";
import {
  bindingAgents,
  readBindings,
  currentBinding,
  changeBinding,
  type ContextBindingSnapshot,
} from "./bindings";
const props = defineProps<{
  kind: ContextKind;
  projectRef: string;
  resourceRef: string;
  revisionRef: string;
  digest: string;
  eligible: boolean;
  agentRef?: string;
  ownerAgentRef?: string;
  disabled?: boolean;
}>();
const { t } = useI18n();
const agent = ref(props.ownerAgentRef ?? props.agentRef ?? "");
const snapshot = ref<ContextBindingSnapshot>();
const problem = ref<AppProblem>();
const busy = ref(false);
let controller = new AbortController();
const binding = computed(() =>
  snapshot.value
    ? currentBinding(snapshot.value, props.kind, props.resourceRef)
    : undefined,
);
const selected = computed(() =>
  snapshot.value
    ? {
        ref: snapshot.value.agentRef,
        title: snapshot.value.agentName,
        meta: `v${String(snapshot.value.agentVersion)}`,
      }
    : undefined,
);
async function loadAgents(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = await bindingAgents(props.projectRef, query, cursor, signal);
  return {
    ...page,
    items: page.items.map((item) => ({
      ...item,
      disabled:
        item.disabled ||
        (!!props.ownerAgentRef && item.ref !== props.ownerAgentRef),
    })),
  };
}
async function load(): Promise<void> {
  controller.abort();
  controller = new AbortController();
  const signal = controller.signal;
  snapshot.value = undefined;
  problem.value = undefined;
  busy.value = false;
  if (!agent.value) return;
  busy.value = true;
  try {
    const value = await readBindings(props.projectRef, agent.value, signal);
    if (!signal.aborted) snapshot.value = value;
  } catch (error) {
    if (!signal.aborted) problem.value = asProblem(error);
  } finally {
    if (!signal.aborted) busy.value = false;
  }
}
watch(
  () => [
    agent.value,
    props.resourceRef,
    props.projectRef,
    props.revisionRef,
    props.eligible,
  ],
  () => {
    void load();
  },
  { immediate: true },
);
async function change(action: "bind" | "unbind"): Promise<void> {
  const before = snapshot.value;
  if (
    !before ||
    busy.value ||
    props.disabled ||
    (action === "bind" && !props.eligible)
  )
    return;
  if (
    action === "unbind" &&
    !window.confirm(t("contextResources.unbindConfirm"))
  )
    return;
  busy.value = true;
  problem.value = undefined;
  snapshot.value = undefined;
  const signal = controller.signal;
  try {
    const receipt = await changeBinding(
      before,
      props.kind,
      props.resourceRef,
      { ref: props.revisionRef, digest: props.digest },
      action,
      signal,
    );
    const after = await readBindings(props.projectRef, before.agentRef, signal);
    const bound = currentBinding(after, props.kind, props.resourceRef);
    if (
      after.agentVersion <= before.agentVersion ||
      (action === "bind"
        ? bound?.ref !== receipt.ref ||
          bound.version !== receipt.version ||
          bound.revisionRef !== receipt.revisionRef ||
          bound.digest !== receipt.digest
        : !!bound)
    )
      throw new Error("Context binding readback mismatch");
    if (!signal.aborted) snapshot.value = after;
  } catch (error) {
    if (!signal.aborted) problem.value = asProblem(error);
  } finally {
    if (!signal.aborted) busy.value = false;
  }
}
onBeforeUnmount(() => controller.abort());
</script>
<template>
  <section class="context-binding">
    <h3>{{ $t("contextResources.agentBinding") }}</h3>
    <code>{{ revisionRef }} / {{ digest }}</code>
    <AsyncEntityPicker
      v-model="agent"
      :selected="selected"
      :load-page="loadAgents"
      :disabled="busy || disabled"
      :trigger-label="$t('contextResources.agentBinding')"
    />
    <ProblemNotice v-if="problem" :problem="problem" @retry="load" />
    <p v-if="busy" role="status">{{ $t("common.loading") }}</p>
    <dl v-if="binding">
      <dt>{{ $t("contextResources.boundRevision") }}</dt>
      <dd>
        <code>{{ binding.revisionRef }} / {{ binding.digest }}</code>
      </dd>
      <dt>{{ $t("impact.bindingVersion", { version: binding.version }) }}</dt>
    </dl>
    <div class="context-binding-actions">
      <button
        class="button"
        :disabled="
          busy ||
          disabled ||
          !snapshot ||
          !eligible ||
          binding?.revisionRef === revisionRef
        "
        @click="change('bind')"
      >
        <Link2 :size="18" />{{ $t("contextResources.bind") }}
      </button>
      <button
        class="button"
        :disabled="busy || disabled || !binding"
        @click="change('unbind')"
      >
        <Unlink :size="18" />{{ $t("contextResources.unbind") }}
      </button>
      <button
        class="icon-button"
        :disabled="busy || !agent"
        :title="$t('common.retry')"
        :aria-label="$t('common.retry')"
        @click="load"
      >
        <RefreshCw :size="18" />
      </button>
    </div>
  </section>
</template>
<style scoped>
.context-binding {
  display: grid;
  gap: 12px;
  min-width: 0;
  border-top: 1px solid var(--border);
  padding-top: 16px;
}
.context-binding h3 {
  font-size: 16px;
  margin: 0;
}
.context-binding dd {
  margin: 8px 0;
  overflow-wrap: anywhere;
}
.context-binding > code {
  overflow-wrap: anywhere;
}
.context-binding-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
</style>
