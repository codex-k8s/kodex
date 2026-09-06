<script setup lang="ts">
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { InstructionVersion } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  versions: InstructionVersion[];
  currentRef?: string;
  currentEffective?: boolean;
  canRollback: boolean;
  busy: boolean;
}>();
const emit = defineEmits<{ rollback: [ref: string] }>();
const { locale, t } = useI18n();
const pendingRef = ref("");

watch(
  () => [props.currentRef, props.versions] as const,
  () => {
    pendingRef.value = "";
  },
);

function formatDate(value?: string): string {
  if (!value) return t("common.noData");
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function confirmRollback(ref: string): void {
  emit("rollback", ref);
  pendingRef.value = "";
}
</script>

<template>
  <section class="instruction-history" :aria-label="$t('agents.history')">
    <div class="section-header">
      <div>
        <h3>{{ $t("agents.history") }}</h3>
        <p class="muted">{{ $t("agents.historyHelp") }}</p>
      </div>
    </div>
    <p v-if="versions.length === 0" class="muted">
      {{ $t("agents.historyEmpty") }}
    </p>
    <ol v-else class="instruction-history__list">
      <li
        v-for="version in versions"
        :key="version.ref"
        :data-version="version.version"
        :data-revision="version.revision"
      >
        <div class="instruction-history__summary">
          <span>
            <strong>{{
              $t("agents.revision", { revision: version.revision })
            }}</strong>
            <small>{{
              formatDate(version.publishedAt ?? version.createdAt)
            }}</small>
          </span>
          <span class="instruction-history__state">
            <StatusBadge :state="version.state" />
            <span
              v-if="version.ref === currentRef"
              class="instruction-history__current"
            >
              {{
                $t(
                  currentEffective === false
                    ? "publicationImpact.instructionsSelected"
                    : "agents.currentRevision",
                )
              }}
            </span>
          </span>
          <button
            v-if="version.ref !== currentRef && canRollback"
            class="button"
            type="button"
            :disabled="busy"
            @click="pendingRef = version.ref"
          >
            {{ $t("agents.rollback") }}
          </button>
        </div>
        <details>
          <summary>{{ $t("common.details") }}</summary>
          <pre>{{ version.content }}</pre>
        </details>
        <div
          v-if="pendingRef === version.ref"
          class="instruction-history__confirm"
          role="group"
        >
          <p>
            {{ $t("agents.rollbackConfirm", { revision: version.revision }) }}
          </p>
          <div class="inline-actions">
            <button
              class="button"
              type="button"
              :disabled="busy"
              @click="pendingRef = ''"
            >
              {{ $t("common.cancel") }}
            </button>
            <button
              class="button button--primary"
              type="button"
              :disabled="busy"
              @click="confirmRollback(version.ref)"
            >
              {{ $t("agents.rollback") }}
            </button>
          </div>
        </div>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.instruction-history {
  padding-top: 16px;
  margin-top: 16px;
  border-top: 1px solid var(--border);
}
.instruction-history__list {
  display: grid;
  gap: 0;
  padding: 0;
  margin: 0;
  list-style: none;
  border-top: 1px solid var(--border);
}
.instruction-history__list > li {
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}
.instruction-history__summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.instruction-history__summary > span:first-child {
  display: grid;
  gap: 2px;
}
.instruction-history__summary small,
.instruction-history__current {
  color: var(--muted);
}
.instruction-history__state {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin-left: auto;
}
.instruction-history details {
  margin-top: 8px;
}
.instruction-history pre {
  max-height: 240px;
  overflow: auto;
  padding: 10px;
  margin-bottom: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: var(--panel);
}
.instruction-history__confirm {
  padding: 10px;
  margin-top: 10px;
  background: var(--warning-soft);
  border: 1px solid var(--border);
}
.instruction-history__confirm p {
  margin-bottom: 8px;
}
</style>
