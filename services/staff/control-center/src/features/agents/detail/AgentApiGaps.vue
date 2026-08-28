<script setup lang="ts">
import { LockKeyhole, ServerOff } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { agentDetailCopy } from "@/features/agents/detail/copy";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const gaps = computed(() => [
  { code: "config.toml", text: copy.value.gaps.overlay },
  { code: "template_variables", text: copy.value.gaps.variables },
  { code: "avatar_asset", text: copy.value.gaps.avatar },
  { code: "role_environments_search", text: copy.value.gaps.environmentSearch },
]);
</script>

<template>
  <section class="api-gaps panel" aria-labelledby="agent-api-gaps-title">
    <div class="api-gaps__head">
      <ServerOff :size="18" aria-hidden="true" />
      <div>
        <h2 id="agent-api-gaps-title">{{ copy.gaps.title }}</h2>
        <p>{{ copy.gaps.description }}</p>
      </div>
    </div>
    <ul>
      <li v-for="gap in gaps" :key="gap.code">
        <LockKeyhole :size="15" aria-hidden="true" />
        <div>
          <code>{{ gap.code }}</code
          ><span>{{ gap.text }}</span>
        </div>
        <StatusBadge state="UNAVAILABLE" />
      </li>
    </ul>
  </section>
</template>

<style scoped>
.api-gaps {
  display: grid;
  gap: 12px;
}
.api-gaps__head {
  display: flex;
  align-items: flex-start;
  gap: 9px;
}
.api-gaps__head > svg {
  margin-top: 2px;
  color: var(--warning);
}
.api-gaps h2,
.api-gaps p {
  margin: 0;
}
.api-gaps h2 {
  font-size: 0.94rem;
}
.api-gaps p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.8rem;
}
.api-gaps ul {
  display: grid;
  padding: 0;
  margin: 0;
  list-style: none;
  border-top: 1px solid var(--border);
}
.api-gaps li {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 9px;
  padding: 9px 0;
  border-bottom: 1px solid var(--hairline);
}
.api-gaps li > svg {
  margin-top: 2px;
  color: var(--subtle);
}
.api-gaps li div {
  display: grid;
  gap: 2px;
}
.api-gaps code {
  color: var(--text);
  font-family: var(--font-mono);
  font-size: 0.78rem;
}
.api-gaps li span:not(.status-badge) {
  color: var(--muted);
  font-size: 0.78rem;
}
@media (max-width: 640px) {
  .api-gaps li {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .api-gaps .status-badge {
    grid-column: 2;
  }
}
</style>
