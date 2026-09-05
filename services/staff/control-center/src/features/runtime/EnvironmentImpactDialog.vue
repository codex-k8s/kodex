<script setup lang="ts">
import { Link2, RefreshCw } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type {
  RuntimeEnvironmentImpact,
  RuntimeEnvironmentRebindResult,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import {
  applyEnvironmentRebind,
  consumerKey,
  readEnvironmentImpact,
} from "./revision-impact";
const props = defineProps<{ environmentRef: string; versionRef: string }>();
const emit = defineEmits<{ close: []; applied: [] }>();
const impact = ref<RuntimeEnvironmentImpact>();
const selected = ref(new Set<string>());
const receipt = ref<RuntimeEnvironmentRebindResult>();
const problem = ref<AppProblem>();
const loading = ref(false);
const busy = ref(false);
let generation = 0;
let controller: AbortController | undefined;
const selection = computed(
  () =>
    impact.value?.consumers.filter((item) =>
      selected.value.has(consumerKey(item)),
    ) ?? [],
);
async function load(more = false): Promise<void> {
  if (busy.value || (more && (loading.value || !impact.value?.nextPageToken)))
    return;
  const previous = impact.value;
  const current = ++generation;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loading.value = true;
  problem.value = undefined;
  if (!more) {
    selected.value.clear();
    receipt.value = undefined;
    impact.value = undefined;
  }
  try {
    const page = await readEnvironmentImpact(
      props.environmentRef,
      props.versionRef,
      more ? previous?.nextPageToken : undefined,
      active.signal,
    );
    if (current !== generation) return;
    if (more && previous) {
      if (
        page.environmentVersion !== previous.environmentVersion ||
        page.targetDigest !== previous.targetDigest ||
        page.total !== previous.total ||
        previous.consumers.length + page.consumers.length > page.total ||
        page.consumers.some((item) =>
          previous.consumers.some((old) => old.agentRef === item.agentRef),
        )
      )
        throw new Error("Environment impact snapshot changed");
      impact.value = {
        ...page,
        consumers: [...previous.consumers, ...page.consumers],
      };
    } else impact.value = page;
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function toggle(key: string): void {
  if (busy.value || receipt.value) return;
  if (selected.value.has(key)) selected.value.delete(key);
  else if (selected.value.size < 100) selected.value.add(key);
}
async function apply(): Promise<void> {
  if (
    !impact.value ||
    !selection.value.length ||
    busy.value ||
    loading.value ||
    problem.value ||
    receipt.value
  )
    return;
  const current = generation;
  busy.value = true;
  try {
    const result = await applyEnvironmentRebind(impact.value, selection.value);
    if (current !== generation) return;
    receipt.value = result;
    selected.value.clear();
    emit("applied");
  } catch (error) {
    if (current === generation) problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
watch(
  () => [props.environmentRef, props.versionRef],
  () => void load(),
  { immediate: true },
);
onBeforeUnmount(() => {
  generation += 1;
  controller?.abort();
});
</script>
<template>
  <ModalDialog
    :title="$t('impact.environmentTitle')"
    :busy="busy"
    size="xl"
    @close="emit('close')"
  >
    <div class="impact-summary">
      <code>{{ environmentRef }} / {{ versionRef }}</code
      ><button
        class="icon-button"
        :disabled="loading || busy"
        :title="$t('vfs.refresh')"
        :aria-label="$t('vfs.refresh')"
        @click="load()"
      >
        <RefreshCw :size="18" />
      </button>
    </div>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <template v-if="impact">
      <p>
        {{ $t("impact.total", { count: impact.total }) }} · v{{
          impact.environmentVersion
        }}
      </p>
      <code class="impact-digest">{{ impact.targetDigest }}</code>
      <div class="impact-list">
        <label
          v-for="consumer in impact.consumers"
          :key="consumer.agentRef"
          class="impact-consumer"
        >
          <input
            type="checkbox"
            :checked="selected.has(consumerKey(consumer))"
            :disabled="
              busy ||
              !!receipt ||
              consumer.versionRef === versionRef ||
              (!selected.has(consumerKey(consumer)) && selected.size >= 100)
            "
            @change="toggle(consumerKey(consumer))"
          />
          <span
            ><strong>{{ consumer.agentRef }}</strong
            ><small>{{ consumer.projectRef }}</small
            ><code>{{ consumer.versionRef }}</code
            ><small>{{
              $t("impact.bindingVersion", { version: consumer.bindingVersion })
            }}</small></span
          >
          <span v-if="consumer.versionRef === versionRef">{{
            $t("impact.current")
          }}</span>
        </label>
        <p v-if="!impact.consumers.length">{{ $t("common.empty") }}</p>
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
          busy || loading || !!problem || !!receipt || !selection.length
        "
        @click="apply"
      >
        <Link2 :size="17" />{{
          $t("impact.rebind", { count: selection.length })
        }}
      </button></template
    >
  </ModalDialog>
</template>
<style scoped>
.impact-summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.impact-summary code,
.impact-digest,
.impact-receipt {
  overflow-wrap: anywhere;
}
.impact-list {
  max-height: 672px;
  overflow: auto;
  margin: 12px 0;
}
.impact-consumer {
  display: grid;
  grid-template-columns: 20px minmax(0, 1fr) auto;
  gap: 12px;
  align-items: start;
  min-height: 112px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
  overflow-wrap: anywhere;
}
.impact-consumer input {
  width: 18px;
  height: 18px;
  min-height: 18px;
}
.impact-consumer > span:first-of-type {
  display: grid;
  gap: 5px;
}
.impact-receipt h3 {
  font-size: 16px;
}
@media (max-width: 600px) {
  .impact-consumer {
    grid-template-columns: 20px minmax(0, 1fr);
  }
  .impact-consumer > span:last-child {
    grid-column: 2;
  }
}
</style>
