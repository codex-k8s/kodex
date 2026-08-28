<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import StatusBadge from "@/shared/ui/StatusBadge.vue";

defineOptions({ name: "SafeStructuredData" });

const props = withDefaults(
  defineProps<{
    value: unknown;
    depth?: number;
  }>(),
  { depth: 0 },
);

const translator = useI18n();
const opaqueRefPattern =
  /^(?:agt|art|bld|cap|cnv|con|edg|evt|gat|inc|int|job|mbr|msg|nod|pln|prj|rev|rol|rti|run|sch|ses|trn|usr|wfl)_[A-Za-z0-9_-]{8,}$/;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isContainer(value: unknown): boolean {
  return Array.isArray(value) || isRecord(value);
}

function fieldLabel(key: string): string {
  const normalized = key.trim().toLowerCase();
  if (normalized === "status" || normalized === "state")
    return translator.t("common.status");
  if (normalized === "result" || normalized === "outcome")
    return translator.t("common.result");

  const words = key
    .replace(/([a-z\d])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim();
  if (!words) return "—";
  return `${words.charAt(0).toLocaleUpperCase()}${words.slice(1)}`;
}

function statusState(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined;
  const normalized = value.trim().toLowerCase();
  switch (normalized) {
    case "success":
    case "succeeded":
    case "ok":
      return "SUCCEEDED";
    case "blocked":
    case "completed_with_issues":
    case "needs_attention":
    case "warning":
      return "NEEDS_ATTENTION";
    case "error":
    case "failed":
    case "rejected":
      return "FAILED";
    case "canceled":
    case "cancelled":
      return "CANCELLED";
    default: {
      const state = normalized.toUpperCase();
      return translator.te(`states.${state}`) ? state : undefined;
    }
  }
}

function isStatusField(key: string): boolean {
  const normalized = key.trim().toLowerCase();
  return normalized === "status" || normalized === "state";
}

function scalarText(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "boolean")
    return translator.t(value ? "common.yes" : "common.no");
  if (typeof value !== "string" && typeof value !== "number") return "—";
  const text = String(value);
  return opaqueRefPattern.test(text) ? "—" : text;
}

const entries = computed(() =>
  isRecord(props.value) ? Object.entries(props.value) : [],
);
</script>

<template>
  <span v-if="depth >= 5">—</span>
  <ol v-else-if="Array.isArray(value)" class="structured-list">
    <li v-for="(item, index) in value" :key="index">
      <SafeStructuredData
        v-if="isContainer(item)"
        :value="item"
        :depth="depth + 1"
      />
      <span v-else>{{ scalarText(item) }}</span>
    </li>
  </ol>
  <dl v-else-if="isRecord(value)" class="structured-fields">
    <template v-for="([key, item], index) in entries" :key="`${key}-${index}`">
      <dt>{{ fieldLabel(key) }}</dt>
      <dd>
        <StatusBadge
          v-if="statusState(item)"
          :state="statusState(item) ?? ''"
        />
        <SafeStructuredData
          v-else-if="isContainer(item)"
          :value="item"
          :depth="depth + 1"
        />
        <span v-else-if="isStatusField(key)">—</span>
        <span v-else>{{ scalarText(item) }}</span>
      </dd>
    </template>
  </dl>
  <span v-else>{{ scalarText(value) }}</span>
</template>

<style scoped>
.structured-fields {
  display: grid;
  grid-template-columns: minmax(120px, 0.35fr) minmax(0, 1fr);
  gap: 8px 16px;
  margin: 0;
}
.structured-fields dt {
  color: var(--subtle);
  font-size: 0.82rem;
}
.structured-fields dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.structured-list {
  display: grid;
  gap: 6px;
  margin: 0;
  padding-left: 20px;
}
@media (max-width: 560px) {
  .structured-fields {
    grid-template-columns: 1fr;
    gap: 3px;
  }
  .structured-fields dd + dt {
    margin-top: 8px;
  }
}
</style>
