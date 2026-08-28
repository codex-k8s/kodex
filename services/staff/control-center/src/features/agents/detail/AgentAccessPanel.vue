<script setup lang="ts">
import { BookOpenCheck, PlugZap, ShieldCheck } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { agentDetailCopy } from "@/features/agents/detail/copy";
import type { PlatformCapability } from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  capabilities: readonly PlatformCapability[];
  grantedKeys: readonly string[];
  integrations: readonly string[];
  knowledgeCount: number;
  canManage: boolean;
  busyKey: string;
}>();
const emit = defineEmits<{ toggle: [key: string] }>();
const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const granted = computed(() => new Set(props.grantedKeys));
</script>

<template>
  <div class="access-layout">
    <article class="access-panel panel">
      <div class="access-panel__head">
        <ShieldCheck :size="19" aria-hidden="true" />
        <div>
          <h2>{{ $t("agents.capabilities") }}</h2>
          <p>{{ $t("agents.capabilitiesHelp") }}</p>
        </div>
      </div>
      <div class="access-panel__list">
        <label v-for="capability in capabilities" :key="capability.key">
          <input
            v-if="canManage"
            type="checkbox"
            :checked="granted.has(capability.key)"
            :disabled="Boolean(busyKey)"
            @change="emit('toggle', capability.key)"
          />
          <span class="access-panel__check" aria-hidden="true" />
          <span>
            <strong>{{ capability.name }}</strong>
            <small>{{ capability.description }}</small>
          </span>
        </label>
        <p v-if="capabilities.length === 0">{{ $t("common.empty") }}</p>
      </div>
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
.access-panel__list {
  display: grid;
  border-top: 1px solid var(--border);
}
.access-panel__list label {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: start;
  gap: 10px;
  padding: 11px 2px;
  border-bottom: 1px solid var(--hairline);
  cursor: pointer;
}
.access-panel__list label:has(input) {
  grid-template-columns: auto minmax(0, 1fr);
}
.access-panel__list label:not(:has(input)) {
  grid-template-columns: minmax(0, 1fr);
}
.access-panel__list input {
  width: 17px;
  min-height: 17px;
  margin: 1px 0 0;
}
.access-panel__check {
  display: none;
}
.access-panel__list strong,
.access-panel__list small {
  display: block;
}
.access-panel__list small {
  margin-top: 3px;
  color: var(--muted);
  font-weight: 400;
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
