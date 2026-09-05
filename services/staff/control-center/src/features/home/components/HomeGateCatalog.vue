<script setup lang="ts">
import { Maximize2 } from "@lucide/vue";
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useGateCatalog } from "@/features/workboard/gate-catalog";
import { usePlatformStore } from "@/features/platform/store";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import HomeGateRows from "./HomeGateRows.vue";
import GateProjectFilter from "@/features/workboard/components/GateProjectFilter.vue";
const catalog = useGateCatalog();
const platform = usePlatformStore();
const expanded = ref(false);
const query = ref("");
const projectRef = ref("");
let timer: ReturnType<typeof setTimeout> | undefined;
function load(more = false): Promise<void> {
  return catalog.load(
    {
      projectRef: projectRef.value || undefined,
      query: query.value,
      view: "PENDING",
    },
    more,
  );
}
watch(query, () => {
  clearTimeout(timer);
  catalog.reset();
  timer = setTimeout(() => void load(), 250);
});
watch(projectRef, () => {
  clearTimeout(timer);
  void load();
});
onMounted(() => void load());
watch(
  () =>
    platform.gateList
      .map((gate) => `${gate.ref}:${String(gate.version)}`)
      .sort()
      .join("|"),
  () => {
    catalog.invalidate({
      projectRef: projectRef.value || undefined,
      query: query.value,
      view: "PENDING",
    });
  },
);
onBeforeUnmount(() => {
  clearTimeout(timer);
  catalog.reset();
});
</script>
<template>
  <section class="home-gate-catalog" :aria-label="$t('home.pending')">
    <header>
      <h3>{{ $t("home.pending") }}</h3>
      <span v-if="catalog.total.value !== undefined">{{
        catalog.total.value
      }}</span>
      <button
        type="button"
        class="button button--ghost"
        :aria-label="$t('common.expand')"
        @click="expanded = true"
      >
        <Maximize2 :size="16" />
      </button>
    </header>
    <label class="gate-search"
      ><span>{{ $t("common.search") }}</span
      ><input v-model="query" type="search" maxlength="200"
    /></label>
    <ProblemNotice
      v-if="catalog.problem.value"
      :problem="catalog.problem.value"
      @retry="load()"
    />
    <GateProjectFilter v-model="projectRef" />
    <p v-if="catalog.loading.value" role="status">{{ $t("common.loading") }}</p>
    <p v-else-if="catalog.total.value === 0">
      {{ $t("decisions.emptyTitle") }}
    </p>
    <HomeGateRows
      :items="catalog.items.value"
      :more="catalog.pageToken.value"
      :loading="catalog.loading.value"
      @more="load(true)"
    />
    <ModalDialog
      v-if="expanded"
      :title="$t('home.pending')"
      size="xl"
      @close="expanded = false"
    >
      <label class="gate-search"
        ><span>{{ $t("common.search") }}</span
        ><input v-model="query" type="search" maxlength="200"
      /></label>
      <p v-if="catalog.total.value !== undefined">
        {{ $t("decisions.pendingCount", { count: catalog.total.value }) }}
      </p>
      <GateProjectFilter v-model="projectRef" />
      <ProblemNotice
        v-if="catalog.problem.value"
        :problem="catalog.problem.value"
        @retry="load()"
      />
      <HomeGateRows
        :items="catalog.items.value"
        :more="catalog.pageToken.value"
        :loading="catalog.loading.value"
        @more="load(true)"
      />
    </ModalDialog>
  </section>
</template>
<style scoped>
.home-gate-catalog {
  min-width: 0;
}
header {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 44px;
  padding: 9px 16px;
}
header h3 {
  margin: 0;
  font-size: 0.84rem;
}
header button {
  margin-left: auto;
}
.gate-search {
  display: grid;
  gap: 4px;
  padding: 8px 16px;
}
.gate-search input {
  width: 100%;
  min-width: 0;
}
p {
  padding: 0 16px;
}
</style>
