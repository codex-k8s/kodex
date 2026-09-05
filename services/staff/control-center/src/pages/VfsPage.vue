<script setup lang="ts">
import VfsBrowser from "@/features/files/VfsBrowser.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import { computed } from "vue";
import { useRoute, useRouter } from "vue-router";
import ContextCatalog from "@/features/context-resources/ContextCatalog.vue";
const route = useRoute();
const router = useRouter();
const view = computed(() =>
  route.query.view === "skills" || route.query.view === "memory"
    ? route.query.view
    : "vfs",
);
function choose(value: string): void {
  void router.replace({ query: { ...route.query, view: value } });
}
</script>
<template>
  <PageFrame :title="$t('files.title')"
    ><template #actions
      ><div
        class="segmented-control"
        role="tablist"
        :aria-label="$t('files.tabs')"
      >
        <button
          role="tab"
          :aria-selected="view === 'vfs'"
          @click="choose('vfs')"
        >
          {{ $t("vfs.structure") }}</button
        ><button
          v-for="kind in ['skills', 'memory']"
          :key="kind"
          role="tab"
          :aria-selected="view === kind"
          @click="choose(kind)"
        >
          {{ $t(`contextResources.${kind}`) }}
        </button>
      </div></template
    ><ContextCatalog
      v-if="view !== 'vfs'"
      :key="view"
      :kind="view" /><VfsBrowser v-else
  /></PageFrame>
</template>
