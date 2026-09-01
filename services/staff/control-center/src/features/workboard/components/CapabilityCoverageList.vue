<script setup lang="ts">
import { History, PauseCircle, ShieldAlert } from "@lucide/vue";

import type {
  HomeCapabilityCoverage,
  HomeCapabilityKey,
} from "@/features/workboard/model";

defineProps<{ items: HomeCapabilityCoverage[] }>();

const iconByKey: Record<HomeCapabilityKey, typeof PauseCircle> = {
  STOPPED_RUNS: PauseCircle,
  PROVIDER_AUTH_EXPIRY: ShieldAlert,
  SESSION_CONTINUATION: History,
};
</script>

<template>
  <section
    v-if="items.length"
    class="capability-coverage"
    role="note"
    :aria-label="$t('workboard.coverage.title')"
  >
    <header>
      <strong>{{ $t("workboard.coverage.title") }}</strong>
      <span>{{ $t("workboard.coverage.subtitle") }}</span>
    </header>
    <ul>
      <li v-for="item in items" :key="item.key">
        <component :is="iconByKey[item.key]" :size="17" aria-hidden="true" />
        <span>
          <strong>{{
            $t(`workboard.coverage.capabilities.${item.key}.title`)
          }}</strong>
          <small>{{
            $t(`workboard.coverage.capabilities.${item.key}.description`)
          }}</small>
        </span>
        <b>{{ $t("workboard.coverage.unavailable") }}</b>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.capability-coverage {
  border-top: 1px solid var(--hairline);
  background: color-mix(in srgb, var(--panel) 72%, var(--surface));
}
.capability-coverage > header {
  display: flex;
  align-items: baseline;
  gap: 8px 12px;
  padding: 11px 16px 8px;
}
.capability-coverage > header span {
  color: var(--muted);
  font-size: 0.78rem;
}
.capability-coverage ul {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1px;
  margin: 0;
  padding: 0;
  background: var(--hairline);
  list-style: none;
}
.capability-coverage li {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 9px;
  min-height: 72px;
  padding: 11px 14px;
  background: var(--surface);
}
.capability-coverage li > svg {
  margin-top: 1px;
  color: var(--muted);
}
.capability-coverage li > span {
  display: grid;
  gap: 3px;
  min-width: 0;
}
.capability-coverage small {
  color: var(--muted);
  line-height: 1.35;
}
.capability-coverage li > b {
  padding: 2px 7px;
  border-radius: 999px;
  color: var(--muted);
  background: var(--hairline);
  font-size: 0.7rem;
  font-weight: 600;
  white-space: nowrap;
}
@media (max-width: 980px) {
  .capability-coverage ul {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 620px) {
  .capability-coverage > header {
    display: grid;
  }
  .capability-coverage li {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .capability-coverage li > b {
    grid-column: 2;
    justify-self: start;
  }
}
</style>
