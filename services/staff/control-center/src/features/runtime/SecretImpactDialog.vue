<script setup lang="ts">
import { Link2, RefreshCw } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type {
  RuntimeSecretImpact,
  RuntimeSecretRebindResult,
  RuntimeSecretRebindSelection,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import {
  applySecretRebind,
  consumerKey,
  readSecretImpact,
} from "./revision-impact";
const props = defineProps<{ secretRef: string; revision: number }>();
const emit = defineEmits<{ close: []; applied: [] }>();
const impact = ref<RuntimeSecretImpact>();
const receipt = ref<RuntimeSecretRebindResult>();
const selections = ref<RuntimeSecretRebindSelection[]>([]);
const problem = ref<AppProblem>();
const loading = ref(false);
const busy = ref(false);
const query = ref("");
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const cursors = new Set<string>();
let generation = 0;
let controller: AbortController | undefined;
const groups = computed(() => {
  const result = new Map<
    string,
    RuntimeSecretRebindSelection & { key: string; projectRef: string }
  >();
  for (const row of impact.value?.consumers ?? []) {
    const key = JSON.stringify([row.environmentRef, row.environmentVersionRef]);
    let group = result.get(key);
    if (!group) {
      group = {
        key,
        projectRef: row.projectRef,
        environmentRef: row.environmentRef,
        expectedEnvironmentVersion: row.environmentVersion,
        sourceVersionRef: row.environmentVersionRef,
        consumers: [],
      };
      result.set(key, group);
    }
    if (row.consumer) group.consumers.push(row.consumer);
  }
  return [...result.values()];
});
const agentCount = computed(() =>
  selections.value.reduce((sum, item) => sum + item.consumers.length, 0),
);
function selection(group: RuntimeSecretRebindSelection) {
  return selections.value.find(
    (item) =>
      item.environmentRef === group.environmentRef &&
      item.sourceVersionRef === group.sourceVersionRef,
  );
}
function toggleEnvironment(group: RuntimeSecretRebindSelection): void {
  if (busy.value || receipt.value) return;
  if (selection(group))
    selections.value = selections.value.filter(
      (item) => item.environmentRef !== group.environmentRef,
    );
  else if (
    selections.value.length < 32 &&
    !selections.value.some(
      (item) => item.environmentRef === group.environmentRef,
    )
  ) {
    selections.value.push({
      environmentRef: group.environmentRef,
      expectedEnvironmentVersion: group.expectedEnvironmentVersion,
      sourceVersionRef: group.sourceVersionRef,
      consumers: [],
    });
  }
}
function toggleAgent(group: RuntimeSecretRebindSelection, key: string): void {
  if (busy.value || receipt.value) return;
  const target = selection(group);
  const consumer = group.consumers.find((item) => consumerKey(item) === key);
  if (!target || !consumer) return;
  if (target.consumers.some((item) => consumerKey(item) === key))
    target.consumers = target.consumers.filter(
      (item) => consumerKey(item) !== key,
    );
  else if (
    agentCount.value < 100 &&
    !selections.value.some((item) =>
      item.consumers.some((agent) => agent.agentRef === consumer.agentRef),
    )
  )
    target.consumers.push(consumer);
}
async function load(more = false): Promise<void> {
  if (busy.value || (more && (loading.value || !impact.value?.nextPageToken)))
    return;
  clearTimeout(searchTimer);
  const previous = impact.value;
  const current = ++generation;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loading.value = true;
  problem.value = undefined;
  if (!more) {
    cursors.clear();
    impact.value = undefined;
    receipt.value = undefined;
    selections.value = [];
  }
  try {
    const page = await readSecretImpact(
      props.secretRef,
      props.revision,
      more ? previous?.nextPageToken : undefined,
      active.signal,
      query.value,
    );
    if (current !== generation) return;
    if (more && previous?.nextPageToken) cursors.add(previous.nextPageToken);
    if (page.nextPageToken && cursors.has(page.nextPageToken))
      throw new Error("Secret impact cursor repeated");
    const rows =
      more && previous
        ? [...previous.consumers, ...page.consumers]
        : page.consumers;
    if (
      more &&
      previous &&
      (page.secretVersion !== previous.secretVersion ||
        page.total !== previous.total)
    )
      throw new Error("Secret impact snapshot changed");
    const seen = new Set<string>();
    for (const row of rows) {
      const key = JSON.stringify([
        row.environmentRef,
        row.environmentVersionRef,
        row.consumer?.agentRef ?? null,
      ]);
      if (
        seen.has(key) ||
        rows.some(
          (other) =>
            other.environmentRef === row.environmentRef &&
            (other.environmentVersion !== row.environmentVersion ||
              other.projectRef !== row.projectRef),
        )
      )
        throw new Error("Inconsistent secret impact snapshot");
      seen.add(key);
    }
    if (rows.length > page.total)
      throw new Error("Secret impact total mismatch");
    impact.value = { ...page, consumers: rows };
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
async function apply(): Promise<void> {
  if (
    !impact.value ||
    !selections.value.length ||
    busy.value ||
    loading.value ||
    problem.value ||
    receipt.value
  )
    return;
  busy.value = true;
  const current = generation;
  try {
    const result = await applySecretRebind(impact.value, selections.value);
    if (current !== generation) return;
    receipt.value = result;
    selections.value = [];
    emit("applied");
  } catch (error) {
    if (current === generation) problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
watch(
  query,
  () => {
    clearTimeout(searchTimer);
    controller?.abort();
    generation += 1;
    impact.value = undefined;
    selections.value = [];
    receipt.value = undefined;
    problem.value = undefined;
    loading.value = true;
    searchTimer = setTimeout(() => void load(), 500);
  },
  { flush: "sync" },
);
watch(
  () => [props.secretRef, props.revision],
  () => void load(),
  { immediate: true },
);
onBeforeUnmount(() => {
  clearTimeout(searchTimer);
  generation += 1;
  controller?.abort();
});
</script>
<template>
  <ModalDialog
    :title="$t('impact.secretTitle')"
    :busy="busy"
    size="xl"
    @close="emit('close')"
  >
    <header class="impact-heading">
      <code>{{ secretRef }} / {{ revision }}</code
      ><button
        class="icon-button"
        :disabled="busy || loading"
        :title="$t('vfs.refresh')"
        :aria-label="$t('vfs.refresh')"
        @click="load()"
      >
        <RefreshCw :size="18" />
      </button>
    </header>
    <input
      v-model="query"
      type="search"
      :aria-label="$t('common.search')"
      :placeholder="$t('common.search')"
      :disabled="busy"
    />
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <template v-if="impact">
      <p>
        {{ $t("impact.total", { count: impact.total }) }} · v{{
          impact.secretVersion
        }}
      </p>
      <div class="impact-groups">
        <section v-for="group in groups" :key="group.key" class="impact-group">
          <label class="impact-row"
            ><input
              type="checkbox"
              :checked="!!selection(group)"
              :disabled="
                busy ||
                !!receipt ||
                (!selection(group) &&
                  (selections.length >= 32 ||
                    selections.some(
                      (item) => item.environmentRef === group.environmentRef,
                    )))
              "
              @change="toggleEnvironment(group)"
            /><span
              ><strong>{{ group.environmentRef }}</strong
              ><small>{{ group.projectRef }}</small
              ><code>{{ group.sourceVersionRef }}</code></span
            ></label
          >
          <div class="impact-agents">
            <label
              v-for="consumer in group.consumers"
              :key="consumer.agentRef"
              class="impact-row"
              ><input
                type="checkbox"
                :checked="
                  selection(group)?.consumers.some(
                    (item) => consumerKey(item) === consumerKey(consumer),
                  ) ?? false
                "
                :disabled="
                  busy ||
                  !!receipt ||
                  !selection(group) ||
                  (!selection(group)?.consumers.some(
                    (item) => item.agentRef === consumer.agentRef,
                  ) &&
                    (agentCount >= 100 ||
                      selections.some((item) =>
                        item.consumers.some(
                          (agent) => agent.agentRef === consumer.agentRef,
                        ),
                      )))
                "
                @change="toggleAgent(group, consumerKey(consumer))"
              /><span
                ><strong>{{ consumer.agentRef }}</strong
                ><small>{{
                  $t("impact.bindingVersion", {
                    version: consumer.bindingVersion,
                  })
                }}</small></span
              ></label
            >
          </div>
        </section>
        <p v-if="!groups.length">{{ $t("common.empty") }}</p>
      </div>
      <button
        v-if="impact.nextPageToken"
        class="button"
        :disabled="loading || busy"
        @click="load(true)"
      >
        {{ $t("impact.more") }}
      </button>
    </template>
    <section v-if="receipt" class="impact-receipt" role="status">
      <h3>{{ $t("impact.applied") }}</h3>
      <p
        v-for="environment in receipt.environments"
        :key="environment.environmentRef"
      >
        <code
          >{{ environment.environmentRef }} → {{ environment.versionRef }}</code
        >
      </p>
      <p v-for="binding in receipt.bindings" :key="binding.ref">
        <code>{{ binding.agentRef }} → {{ binding.versionRef }}</code>
      </p>
    </section>
    <template #actions
      ><button class="button" :disabled="busy" @click="emit('close')">
        {{ $t("common.close") }}</button
      ><button
        class="button button--primary"
        :disabled="
          busy || loading || !!problem || !!receipt || !selections.length
        "
        @click="apply"
      >
        <Link2 :size="17" />{{
          $t("impact.rebind", { count: selections.length })
        }}
      </button></template
    >
  </ModalDialog>
</template>
<style scoped>
.impact-heading {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
}
.impact-groups {
  max-height: 672px;
  overflow: auto;
}
.impact-group {
  min-height: 112px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}
.impact-row {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
  padding: 8px 0;
}
.impact-row span {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.impact-agents {
  margin-left: 32px;
  max-height: 384px;
  overflow: auto;
}
code,
strong,
small {
  overflow-wrap: anywhere;
}
.impact-heading code,
.impact-receipt {
  min-width: 0;
}
</style>
