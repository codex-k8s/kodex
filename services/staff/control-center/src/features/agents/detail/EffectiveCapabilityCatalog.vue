<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type {
  AgentEffectiveCapability,
  AgentEffectiveCapabilityPage,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import {
  canChangePlatformCapability,
  effectiveCapabilityIdentity,
  loadEffectiveCapabilities,
} from "./effective-capabilities";

const props = defineProps<{
  agentRef: string;
  agentVersion?: number;
  projectRef?: string;
  workflowRef?: string;
  stepKey?: string;
  mode: "GRANTS" | "REQUIREMENTS" | "READ";
  selectedKeys?: readonly string[];
  canManage?: boolean;
  busy?: boolean;
}>();
const emit = defineEmits<{
  toggle: [key: string, enabled: boolean, agentVersion: number];
  refresh: [];
}>();
const items = ref<AgentEffectiveCapability[]>([]);
const page = ref<AgentEffectiveCapabilityPage>();
const query = ref("");
const loading = ref(false);
const problem = ref<AppProblem>();
const selected = computed(() => new Set(props.selectedKeys ?? []));
let controller: AbortController | undefined;
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
const seenCursors = new Set<string>();

function checked(item: AgentEffectiveCapability) {
  return props.mode === "REQUIREMENTS"
    ? selected.value.has(item.key)
    : item.requested;
}
function editable(item: AgentEffectiveCapability) {
  if (props.busy || loading.value || !props.canManage) return false;
  if (props.mode === "REQUIREMENTS") return checked(item) || item.effective;
  return props.mode === "GRANTS" && canChangePlatformCapability(item, true);
}
function toggle(item: AgentEffectiveCapability) {
  if (!editable(item) || !page.value) return;
  emit("toggle", item.key, !checked(item), page.value.agentVersion);
}
async function load(more = false) {
  if (more && (loading.value || !page.value?.nextPageToken)) return;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  const current = ++generation;
  const token = more ? page.value?.nextPageToken : undefined;
  const digest = more ? page.value?.digest : undefined;
  if (!more) {
    items.value = [];
    page.value = undefined;
    seenCursors.clear();
  }
  loading.value = true;
  problem.value = undefined;
  try {
    const result = await loadEffectiveCapabilities(
      props,
      query.value,
      token,
      digest,
      active.signal,
    );
    if (current !== generation || active.signal.aborted) return;
    const next = more ? [...items.value, ...result.items] : result.items;
    if (
      new Set(next.map(effectiveCapabilityIdentity)).size !== next.length ||
      (result.nextPageToken && seenCursors.has(result.nextPageToken))
    )
      throw new Error("Repeated effective capability page");
    items.value = next;
    page.value = result;
    if (result.nextPageToken) seenCursors.add(result.nextPageToken);
  } catch (error) {
    if (current !== generation || active.signal.aborted) return;
    const value = asProblem(error);
    if (more && value.status === 412) {
      await load();
      return;
    }
    problem.value = value;
    if (value.status === 412) emit("refresh");
  } finally {
    if (current === generation) loading.value = false;
  }
}
function refresh() {
  controller?.abort();
  generation++;
  clearTimeout(timer);
  items.value = [];
  page.value = undefined;
  problem.value = undefined;
  loading.value = false;
  timer = setTimeout(() => {
    if (props.agentRef) void load();
  }, 200);
}
watch(
  () => [
    props.agentRef,
    props.agentVersion,
    props.projectRef,
    props.workflowRef,
    props.stepKey,
    query.value,
  ],
  refresh,
  { immediate: true },
);
watch(
  () => props.busy,
  (busy, previous) => {
    if (!busy && previous) refresh();
  },
);
onBeforeUnmount(() => {
  controller?.abort();
  generation++;
  clearTimeout(timer);
});
</script>

<template>
  <div class="effective-capabilities" :aria-busy="loading">
    <label class="effective-capabilities__search">
      <span>{{ $t("common.search") }}</span>
      <input v-model="query" type="search" maxlength="200" />
    </label>
    <p v-if="mode === 'REQUIREMENTS'" class="secondary-copy">
      {{ $t("capabilityAuthority.draftIntent") }}
    </p>
    <div
      v-if="mode === 'REQUIREMENTS' && selectedKeys?.length"
      class="effective-capabilities__states"
    >
      <span v-for="key in selectedKeys" :key="key"
        >{{ key }}
        <button
          type="button"
          :disabled="!canManage || busy"
          :aria-label="`${$t('common.delete')}: ${key}`"
          @click="emit('toggle', key, false, page?.agentVersion ?? 0)"
        >
          ×
        </button>
      </span>
    </div>
    <p v-if="page" class="secondary-copy">
      {{ $t("capabilityAuthority.total") }}: {{ page.total }}
    </p>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-else-if="loading && !page" role="status">{{ $t("common.loading") }}</p>
    <p v-else-if="page && !items.length">{{ $t("common.empty") }}</p>
    <div class="effective-capabilities__rows">
      <label
        v-for="item in items"
        :key="effectiveCapabilityIdentity(item)"
        class="effective-capabilities__row"
      >
        <input
          v-if="
            mode === 'REQUIREMENTS' ||
            (mode === 'GRANTS' && item.source === 'PLATFORM')
          "
          type="checkbox"
          :checked="checked(item)"
          :disabled="!editable(item)"
          @change="toggle(item)"
        />
        <span>
          <strong>{{ item.name }}</strong>
          <small>{{ item.description }}</small>
          <small v-if="item.connectionRef"
            >{{ $t("capabilityAuthority.connection") }}:
            {{ item.connectionRef }} · {{ item.grantRef }}</small
          >
          <span class="effective-capabilities__states">
            <span>{{
              $t(
                item.requested
                  ? "capabilityAuthority.requested"
                  : "capabilityAuthority.notRequested",
              )
            }}</span>
            <span>{{
              $t(
                item.effective
                  ? "capabilityAuthority.effective"
                  : "capabilityAuthority.unavailable",
              )
            }}</span>
            <span v-if="workflowRef && item.required">{{
              $t("capabilityAuthority.required")
            }}</span>
          </span>
          <small>{{ $t(`capabilityAuthority.reasons.${item.reason}`) }}</small>
        </span>
      </label>
    </div>
    <button
      v-if="page?.nextPageToken"
      type="button"
      class="button button--secondary"
      :disabled="loading || busy"
      @click="load(true)"
    >
      {{ $t("common.loadMore") }}
    </button>
  </div>
</template>

<style scoped>
.effective-capabilities {
  display: grid;
  gap: 10px;
  min-width: 0;
}
.effective-capabilities__search {
  display: grid;
  gap: 5px;
}
.effective-capabilities__rows {
  max-height: 520px;
  overflow: auto;
}
.effective-capabilities__row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 2px;
  border-bottom: 1px solid var(--hairline);
}
.effective-capabilities__row > span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.effective-capabilities__row strong,
.effective-capabilities__row small {
  display: block;
}
.effective-capabilities__row small {
  margin-top: 4px;
  color: var(--muted);
}
.effective-capabilities__row input {
  flex: 0 0 17px;
  width: 17px;
  min-height: 17px;
  margin-top: 3px;
}
.effective-capabilities__states {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
  font-size: 0.78rem;
}
.effective-capabilities__states > span {
  border: 1px solid var(--hairline);
  border-radius: 5px;
  padding: 2px 5px;
}
</style>
