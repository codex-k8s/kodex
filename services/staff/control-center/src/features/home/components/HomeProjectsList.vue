<script setup lang="ts">
import { FolderKanban } from "@lucide/vue";
import type { Project } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
defineProps<{ items: Project[]; expanded?: boolean }>();
const emit = defineEmits<{ more: [] }>();
function scroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (element.scrollTop + element.clientHeight >= element.scrollHeight - 80)
    emit("more");
}
</script>
<template>
  <div
    class="home-projects"
    :class="{ 'home-projects--expanded': expanded }"
    @scroll="scroll"
  >
    <RouterLink
      v-for="project in items"
      :key="project.ref"
      :to="`/projects/${project.ref}`"
      class="home-project"
    >
      <span class="home-project__icon">
        <FolderKanban :size="20" aria-hidden="true" />
      </span>
      <div class="home-project__copy">
        <h3>{{ project.name }}</h3>
        <p>{{ project.purpose }}</p>
        <small>{{
          $t("workboard.projectActivity", {
            runs: project.activeRunCount,
            gates: project.pendingGateCount,
          })
        }}</small>
      </div>
      <StatusBadge :state="project.lifecycle" />
    </RouterLink>
  </div>
</template>
<style scoped>
.home-projects {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1px;
  background: var(--hairline);
}
.home-project {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 12px;
  min-height: 108px;
  padding: 15px 16px;
  color: inherit;
  background: var(--surface);
  text-decoration: none;
}
.home-project:hover {
  background: var(--panel);
  text-decoration: none;
}
.home-project__icon {
  display: grid;
  place-items: center;
  width: 38px;
  height: 38px;
  border-radius: 8px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.home-project__copy {
  min-width: 0;
}
.home-project h3,
.home-project p {
  margin: 0;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  overflow-wrap: anywhere;
}
.home-project p {
  margin-top: 3px;
  color: var(--muted);
}
.home-project small {
  display: block;
  margin-top: 9px;
  color: var(--muted);
}

.home-projects {
  grid-auto-rows: 160px;
  max-height: 965px;
  overflow: auto;
}
.home-project {
  min-height: 0;
  overflow: hidden;
}
.home-projects--expanded {
  max-height: calc(100dvh - 230px);
}
@media (max-width: 700px) {
  .home-projects {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
