<script setup lang="ts">
import { computed, onMounted } from "vue";
import { useRoute } from "vue-router";
import { usePlatformStore } from "@/features/platform/store";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
const platform = usePlatformStore();
const route = useRoute();
const projectRef = computed(() => String(route.params.projectRef));
const list = computed(() =>
  Object.values(platform.artifacts).filter(
    (i) => i.projectRef === projectRef.value,
  ),
);
onMounted(() => void platform.loadArtifacts(projectRef.value));
</script>
<template>
  <PageFrame :title="$t('files.title')" :subtitle="$t('files.subtitle')"
    ><AsyncState
      :loading="platform.loading.artifacts"
      :problem="platform.problems.artifacts"
      :empty="list.length === 0"
      :empty-title="$t('files.emptyTitle')"
      @retry="platform.loadArtifacts(projectRef)"
      ><div class="entity-list">
        <a
          v-for="artifact in list"
          :key="artifact.ref"
          :href="`/api/v1/artifacts/${artifact.ref}/content`"
          class="entity-row"
          ><div>
            <h3>{{ artifact.fileName }}</h3>
            <p>{{ artifact.mediaType }} · {{ artifact.source }}</p>
          </div>
          <StatusBadge :state="artifact.scanState" /><span
            >{{ artifact.sizeBytes }} B</span
          ></a
        >
      </div></AsyncState
    ></PageFrame
  >
</template>
