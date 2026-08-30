<script setup lang="ts">
import type { Agent } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineProps<{ agents: readonly Agent[] }>();

function initials(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part.slice(0, 1).toLocaleUpperCase())
    .join("");
}
</script>

<template>
  <div class="project-agent-list" role="list">
    <RouterLink
      v-for="agent in agents"
      :key="agent.ref"
      class="project-agent-list__item"
      :to="`/projects/${encodeURIComponent(agent.projectRef)}/agents/${encodeURIComponent(agent.ref)}`"
      role="listitem"
    >
      <span class="project-agent-list__avatar" aria-hidden="true">
        <img v-if="agent.avatarUrl" :src="agent.avatarUrl" alt="" />
        <span v-else>{{ initials(agent.name) }}</span>
      </span>
      <span class="project-agent-list__copy">
        <strong>{{ agent.name }}</strong>
        <small>{{ agent.purpose || agent.roleDescription }}</small>
        <small>{{ agent.runtimeName }}</small>
      </span>
      <StatusBadge :state="agent.state" />
    </RouterLink>
  </div>
</template>

<style scoped>
.project-agent-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 1px;
  background: var(--hairline);
}
.project-agent-list__item {
  display: grid;
  min-width: 0;
  min-height: 82px;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 11px;
  padding: 11px 14px;
  background: var(--surface);
  color: inherit;
  text-decoration: none;
}
.project-agent-list__item:hover {
  background: var(--panel);
  text-decoration: none;
}
.project-agent-list__avatar {
  display: grid;
  width: 42px;
  height: 42px;
  place-items: center;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 50%;
  background: var(--accent-soft);
  color: var(--accent-strong);
  font-weight: 700;
}
.project-agent-list__avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.project-agent-list__copy {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.project-agent-list__copy strong,
.project-agent-list__copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.project-agent-list__copy small {
  color: var(--muted);
  font-size: 0.76rem;
}
@media (max-width: 620px) {
  .project-agent-list {
    grid-template-columns: 1fr;
  }
}
</style>
