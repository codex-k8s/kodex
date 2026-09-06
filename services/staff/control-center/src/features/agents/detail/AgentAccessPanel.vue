<script setup lang="ts">
import { BookOpenCheck, Brain, PlugZap, ShieldCheck } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { agentDetailCopy } from "@/features/agents/detail/copy";
import EffectiveCapabilityCatalog from "./EffectiveCapabilityCatalog.vue";

defineProps<{
  agentVersion: number;
  integrations: readonly string[];
  knowledgeCount: number;
  canManage: boolean;
  busyKey: string;
  projectRef?: string;
  agentRef?: string;
}>();
const emit = defineEmits<{
  toggle: [key: string, enabled: boolean, agentVersion: number];
  refresh: [];
}>();
const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
</script>

<template>
  <div class="access-layout">
    <article class="access-panel panel">
      <div class="access-panel__head">
        <ShieldCheck :size="19" aria-hidden="true" />
        <div>
          <h2>{{ $t("agents.capabilities") }}</h2>
        </div>
      </div>
      <EffectiveCapabilityCatalog
        v-if="agentRef"
        :agent-ref="agentRef"
        :agent-version="agentVersion"
        :project-ref="projectRef"
        mode="GRANTS"
        :can-manage="canManage"
        :busy="Boolean(busyKey)"
        @toggle="
          (key, enabled, version) => emit('toggle', key, enabled, version)
        "
        @refresh="emit('refresh')"
      />
      <p v-if="busyKey" class="access-panel__saving" aria-live="polite">
        {{ $t("agents.capabilitySaving") }}
      </p>
    </article>

    <aside class="access-summary">
      <section class="panel">
        <div class="access-summary__title">
          <PlugZap :size="18" aria-hidden="true" />
          <h2>{{ $t("agents.integrations") }}</h2>
        </div>
        <ul v-if="integrations.length">
          <li v-for="integration in integrations" :key="integration">
            {{ integration }}
          </li>
        </ul>
        <p v-else>{{ copy.access.integrationsEmpty }}</p>
      </section>
      <section class="panel">
        <div class="access-summary__title">
          <BookOpenCheck :size="18" aria-hidden="true" />
          <h2>{{ $t("agents.knowledge") }}</h2>
        </div>
        <strong class="access-summary__count">{{ knowledgeCount }}</strong>
        <p>{{ copy.access.knowledgeBindings }}</p>
        <nav v-if="projectRef && agentRef" class="context-links">
          <RouterLink
            :to="{
              path: `/projects/${encodeURIComponent(projectRef)}/files`,
              query: { view: 'skills', agentRef },
            }"
            ><BookOpenCheck :size="18" />{{
              $t("contextResources.skills")
            }}</RouterLink
          >
          <RouterLink
            :to="{
              path: `/projects/${encodeURIComponent(projectRef)}/files`,
              query: { view: 'memory', agentRef },
            }"
            ><Brain :size="18" />{{ $t("contextResources.memory") }}</RouterLink
          >
        </nav>
      </section>
    </aside>
  </div>
</template>

<style scoped>
.access-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.4fr) minmax(280px, 0.6fr);
  gap: 16px;
  align-items: start;
}
.context-links {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.context-links a {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.access-panel {
  display: grid;
  gap: 14px;
}
.access-panel__head,
.access-summary__title {
  display: flex;
  align-items: flex-start;
  gap: 9px;
}
.access-panel__head > svg,
.access-summary__title > svg {
  margin-top: 1px;
  color: var(--accent-strong);
}
.access-panel h2,
.access-panel p,
.access-summary h2,
.access-summary p {
  margin: 0;
}
.access-panel__head p,
.access-summary p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.8rem;
}
.access-panel__saving {
  color: var(--warning);
}
.access-summary {
  display: grid;
  gap: 16px;
}
.access-summary section {
  display: grid;
  gap: 10px;
}
.access-summary ul {
  padding-left: 18px;
  margin: 0;
}
.access-summary__count {
  font-family: var(--font-mono);
  font-size: 1.6rem;
}
@media (max-width: 820px) {
  .access-layout {
    grid-template-columns: 1fr;
  }
}
</style>
