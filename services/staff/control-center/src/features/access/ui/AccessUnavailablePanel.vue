<script setup lang="ts">
import {
  Bot,
  KeyRound,
  LockKeyhole,
  Search,
  ShieldCheck,
  UserRound,
} from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { AccessSection } from "@/features/access/ui/model";

const props = defineProps<{
  section: Extract<AccessSection, "GROUPS" | "EFFECTIVE" | "AGENT_SCOPE">;
}>();

const { t } = useI18n();
const sectionIcon = computed(() => {
  if (props.section === "GROUPS") return KeyRound;
  if (props.section === "EFFECTIVE") return ShieldCheck;
  return Bot;
});
</script>

<template>
  <section class="unavailable-panel" :aria-labelledby="`access-${section}`">
    <header class="panel-heading">
      <div class="heading-copy">
        <span class="heading-icon" aria-hidden="true">
          <component :is="sectionIcon" :size="19" />
        </span>
        <div>
          <h2 :id="`access-${section}`">
            {{ t(`accessRedesign.surface.${section}.title`) }}
          </h2>
          <p>{{ t(`accessRedesign.surface.${section}.description`) }}</p>
        </div>
      </div>
      <span class="unavailable-badge">
        <LockKeyhole :size="13" aria-hidden="true" />
        {{ t("accessRedesign.backendUnavailableShort") }}
      </span>
    </header>

    <template v-if="section === 'GROUPS'">
      <div class="surface-toolbar">
        <label class="disabled-search">
          <Search :size="16" aria-hidden="true" />
          <input
            type="search"
            :placeholder="t('accessRedesign.groupsSearch')"
            disabled
          />
        </label>
        <span class="source-chip">OIDC</span>
        <button class="button button--primary" type="button" disabled>
          {{ t("accessRedesign.addBinding") }}
        </button>
      </div>
      <div class="groups-workspace">
        <div class="locked-list">
          <KeyRound :size="27" aria-hidden="true" />
          <strong>{{ t("accessRedesign.surface.GROUPS.emptyTitle") }}</strong>
          <p>{{ t("accessRedesign.surface.GROUPS.gap") }}</p>
        </div>
        <aside class="locked-inspector" aria-hidden="true">
          <span v-for="index in 5" :key="index"></span>
        </aside>
      </div>
    </template>

    <template v-else-if="section === 'EFFECTIVE'">
      <div class="surface-toolbar effective-toolbar">
        <label>
          <span>{{ t("accessRedesign.subject") }}</span>
          <select disabled>
            <option>—</option>
          </select>
        </label>
        <label>
          <span>{{ t("accessRedesign.resource") }}</span>
          <select disabled>
            <option>—</option>
          </select>
        </label>
        <label>
          <span>{{ t("accessRedesign.action") }}</span>
          <select disabled>
            <option>—</option>
          </select>
        </label>
      </div>
      <div class="effective-workspace">
        <div class="locked-list">
          <ShieldCheck :size="27" aria-hidden="true" />
          <strong>{{
            t("accessRedesign.surface.EFFECTIVE.emptyTitle")
          }}</strong>
          <p>{{ t("accessRedesign.surface.EFFECTIVE.gap") }}</p>
        </div>
        <aside class="explanation-chain">
          <h3>{{ t("accessRedesign.why") }}</h3>
          <div v-for="index in 4" :key="index" class="chain-step">
            <span>{{ index }}</span>
            <i aria-hidden="true"></i>
          </div>
        </aside>
      </div>
    </template>

    <template v-else>
      <div class="agent-scope-workspace">
        <div class="scope-builder">
          <label>
            <span>{{ t("accessRedesign.projectSelector") }}</span>
            <select disabled>
              <option>—</option>
            </select>
          </label>
          <label>
            <span>{{ t("accessRedesign.agentSelector") }}</span>
            <select disabled>
              <option>—</option>
            </select>
          </label>
          <div class="action-matrix">
            <label v-for="index in 6" :key="index">
              <input type="checkbox" disabled />
              <span aria-hidden="true"></span>
            </label>
          </div>
        </div>
        <aside class="scope-preview">
          <header>
            <h3>{{ t("accessRedesign.effectiveResult") }}</h3>
            <LockKeyhole :size="15" aria-hidden="true" />
          </header>
          <div class="locked-list">
            <UserRound :size="25" aria-hidden="true" />
            <strong>{{
              t("accessRedesign.surface.AGENT_SCOPE.emptyTitle")
            }}</strong>
            <p>{{ t("accessRedesign.surface.AGENT_SCOPE.gap") }}</p>
          </div>
        </aside>
      </div>
    </template>

    <div class="fail-closed-notice">
      <LockKeyhole :size="18" aria-hidden="true" />
      <span>{{ t("accessRedesign.failClosed") }}</span>
    </div>
  </section>
</template>

<style scoped>
.unavailable-panel {
  display: grid;
  gap: 14px;
}
.panel-heading,
.heading-copy,
.surface-toolbar,
.unavailable-badge,
.fail-closed-notice,
.scope-preview > header {
  display: flex;
  align-items: center;
  gap: 9px;
}
.panel-heading,
.scope-preview > header {
  justify-content: space-between;
  align-items: flex-start;
}
.heading-copy {
  align-items: flex-start;
}
.heading-copy h2,
.heading-copy p,
.locked-list p,
.explanation-chain h3,
.scope-preview h3 {
  margin-bottom: 0;
}
.heading-copy p,
.locked-list p {
  color: var(--muted);
}
.heading-icon {
  display: inline-grid;
  place-items: center;
  width: 38px;
  height: 38px;
  flex: 0 0 38px;
  border-radius: 7px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.unavailable-badge,
.source-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  width: max-content;
  padding: 4px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--warning);
  background: var(--warning-soft);
  font-size: 0.75rem;
  white-space: nowrap;
}
.source-chip {
  color: var(--muted);
  background: var(--surface);
  font-family: var(--font-mono);
}
.surface-toolbar {
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.disabled-search {
  position: relative;
  flex: 1;
}
.disabled-search svg {
  position: absolute;
  top: 50%;
  left: 10px;
  color: var(--subtle);
  transform: translateY(-50%);
}
.disabled-search input {
  padding-left: 34px;
}
.groups-workspace,
.effective-workspace,
.agent-scope-workspace {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 340px;
  gap: 14px;
  align-items: stretch;
}
.locked-list,
.locked-inspector,
.explanation-chain,
.scope-builder,
.scope-preview {
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.locked-list {
  display: grid;
  place-content: center;
  justify-items: center;
  min-height: 300px;
  gap: 8px;
  padding: 36px;
  text-align: center;
}
.locked-inspector {
  display: grid;
  align-content: start;
  gap: 12px;
  padding: 15px;
}
.locked-inspector span,
.action-matrix span,
.chain-step i {
  display: block;
  min-height: 42px;
  border-radius: 6px;
  background:
    linear-gradient(var(--hairline), var(--hairline)) 10px 10px / 42% 7px
      no-repeat,
    linear-gradient(var(--panel), var(--panel)) 10px 25px / 70% 8px no-repeat;
}
.effective-toolbar {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
.effective-toolbar label,
.scope-builder > label {
  display: grid;
  gap: 5px;
  color: var(--muted);
  font-size: 0.78rem;
}
.explanation-chain {
  padding: 15px;
}
.chain-step {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr);
  align-items: center;
  gap: 9px;
  padding: 9px 0;
  border-top: 1px solid var(--hairline);
}
.chain-step span {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  color: var(--muted);
  background: var(--panel);
  font-family: var(--font-mono);
}
.chain-step i {
  min-height: 32px;
}
.scope-builder {
  display: grid;
  align-content: start;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  padding: 15px;
}
.action-matrix {
  display: grid;
  grid-column: 1 / -1;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
}
.action-matrix label {
  display: grid;
  grid-template-columns: 18px 1fr;
  align-items: center;
  gap: 8px;
  min-height: 42px;
}
.action-matrix input {
  min-height: 18px;
}
.action-matrix span {
  min-height: 34px;
}
.scope-preview {
  overflow: hidden;
}
.scope-preview > header {
  padding: 12px 13px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.scope-preview .locked-list {
  min-height: 260px;
  border: 0;
  border-radius: 0;
}
.fail-closed-notice {
  align-items: flex-start;
  padding: 11px 13px;
  border: 1px solid color-mix(in srgb, var(--warning) 32%, var(--border));
  border-radius: 8px;
  color: var(--text-secondary);
  background: var(--warning-soft);
}
.fail-closed-notice svg {
  flex: 0 0 auto;
  color: var(--warning);
}
@media (max-width: 850px) {
  .panel-heading,
  .groups-workspace,
  .effective-workspace,
  .agent-scope-workspace {
    align-items: stretch;
  }
  .panel-heading {
    flex-direction: column;
  }
  .groups-workspace,
  .effective-workspace,
  .agent-scope-workspace {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 620px) {
  .surface-toolbar,
  .effective-toolbar,
  .scope-builder,
  .action-matrix {
    grid-template-columns: 1fr;
  }
  .surface-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .scope-builder > label,
  .action-matrix {
    grid-column: 1;
  }
}
</style>
