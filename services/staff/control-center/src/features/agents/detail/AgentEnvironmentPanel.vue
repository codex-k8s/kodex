<script setup lang="ts">
import { Archive, Layers3, RotateCcw, Save } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { agentDetailCopy } from "@/features/agents/detail/copy";
import {
  createLocalEnvironmentLoader,
  type EnvironmentPickerItem,
} from "@/features/agents/detail/model";
import type {
  RoleEnvironment,
  RoleImageBuild,
  RoleImageRecipe,
} from "@/shared/api/generated/openapi/types.gen";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  modelValue: string;
  environments: readonly RoleEnvironment[];
  recipe?: RoleImageRecipe;
  latestBuild?: RoleImageBuild;
  canEdit: boolean;
  busy: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  save: [];
  archive: [];
  restore: [];
}>();
const { locale, t } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const currentKey = computed(
  () => props.recipe?.environment.environmentKey ?? "",
);
const selectedEnvironment = computed(() =>
  props.environments.find(
    (environment) => environment.key === props.modelValue,
  ),
);
const currentEnvironment = computed(() =>
  props.environments.find(
    (environment) => environment.key === currentKey.value,
  ),
);
const pickerItems = computed<EnvironmentPickerItem[]>(() =>
  props.environments.map((environment) => {
    const software = environment.softwareMessageKeys.map((key) => t(key));
    return {
      id: environment.key,
      label: t(environment.nameMessageKey),
      description: t(environment.descriptionMessageKey),
      disabled: !environment.available,
      environment,
      software,
    };
  }),
);
const catalogKey = computed(() =>
  props.environments
    .map((environment) => `${environment.key}:${String(environment.available)}`)
    .join("|"),
);
const loadItems = createLocalEnvironmentLoader(() => pickerItems.value);
const pickerLabels = computed(() => ({
  label: copy.value.environment.catalog,
  searchPlaceholder: copy.value.environment.choose,
  loading: t("common.loading"),
  loadingMore: copy.value.environment.loadingMore,
  empty: t("common.empty"),
  error: t("common.error"),
  retry: t("common.retry"),
}));

function select(value: string | null | readonly string[]): void {
  if (typeof value === "string") emit("update:modelValue", value);
}
</script>

<template>
  <div class="environment-layout">
    <article class="environment-current panel">
      <div class="environment-current__head">
        <div>
          <h2>{{ copy.environment.current }}</h2>
          <p>{{ $t("roleEnvironments.description") }}</p>
        </div>
        <StatusBadge v-if="latestBuild" :state="latestBuild.stage" />
        <StatusBadge
          v-else-if="recipe?.promotedImageReady"
          state="READY"
          :label="copy.environment.imageReady"
        />
      </div>
      <div v-if="currentEnvironment" class="environment-current__identity">
        <span class="environment-current__icon"
          ><Layers3 :size="21" aria-hidden="true"
        /></span>
        <div>
          <h3>{{ $t(currentEnvironment.nameMessageKey) }}</h3>
          <p>{{ $t(currentEnvironment.descriptionMessageKey) }}</p>
          <div class="environment-current__tags">
            <code
              v-for="key in currentEnvironment.softwareMessageKeys"
              :key="key"
            >
              {{ $t(key) }}
            </code>
          </div>
        </div>
      </div>
      <p v-else class="environment-current__empty">{{ $t("common.noData") }}</p>
      <dl v-if="recipe || latestBuild" class="environment-current__meta">
        <div v-if="recipe">
          <dt>{{ $t("common.version", { version: recipe.version }) }}</dt>
          <dd>{{ $t("states." + recipe.state) }}</dd>
        </div>
        <div v-if="latestBuild">
          <dt>{{ $t("roleEnvironments.lastBuild") }}</dt>
          <dd>
            {{ $t("states." + latestBuild.stage) }} ·
            {{ latestBuild.progressPercent }}%
          </dd>
        </div>
      </dl>
      <div class="environment-current__actions">
        <button
          v-if="recipe?.nextActions.includes('ARCHIVE')"
          class="button"
          type="button"
          :disabled="busy"
          @click="emit('archive')"
        >
          <Archive :size="16" aria-hidden="true" />{{ $t("common.archive") }}
        </button>
        <button
          v-if="recipe?.nextActions.includes('RESTORE')"
          class="button"
          type="button"
          :disabled="busy"
          @click="emit('restore')"
        >
          <RotateCcw :size="16" aria-hidden="true" />{{
            $t("roleEnvironments.restore")
          }}
        </button>
      </div>
    </article>

    <article class="environment-catalog panel">
      <div class="environment-catalog__head">
        <div>
          <h2>{{ copy.environment.catalog }}</h2>
          <p>{{ copy.environment.localSearch }}</p>
        </div>
        <code>listRoleEnvironments</code>
      </div>
      <AsyncEntityPicker
        :key="catalogKey"
        :model-value="modelValue || null"
        :load-items="loadItems"
        :labels="pickerLabels"
        :disabled="!canEdit || busy"
        @update:model-value="select"
      >
        <template #option="{ item, selected }">
          <span class="environment-option__icon">
            <Layers3 :size="18" aria-hidden="true" />
          </span>
          <span class="environment-option__copy">
            <strong>{{ item.label }}</strong>
            <span>{{ item.description }}</span>
            <span class="environment-option__software">
              <code v-for="software in item.software" :key="software">{{
                software
              }}</code>
            </span>
          </span>
          <StatusBadge
            :state="item.environment.available ? 'AVAILABLE' : 'UNAVAILABLE'"
          />
          <span v-if="selected" class="sr-only">{{
            $t("common.selected")
          }}</span>
        </template>
      </AsyncEntityPicker>
      <div class="environment-catalog__selection">
        <div>
          <span>{{ $t("common.selected") }}</span>
          <strong>{{
            selectedEnvironment
              ? $t(selectedEnvironment.nameMessageKey)
              : $t("common.noData")
          }}</strong>
        </div>
        <button
          v-if="canEdit"
          class="button button--primary"
          type="button"
          :disabled="
            busy || !selectedEnvironment?.available || modelValue === currentKey
          "
          @click="emit('save')"
        >
          <Save :size="16" aria-hidden="true" />{{
            $t("roleEnvironments.prepare")
          }}
        </button>
      </div>
    </article>
  </div>
</template>

<style scoped>
.environment-layout {
  display: grid;
  grid-template-columns: minmax(300px, 0.75fr) minmax(0, 1.25fr);
  gap: 16px;
  align-items: start;
}
.environment-current,
.environment-catalog {
  display: grid;
  gap: 14px;
}
.environment-current__head,
.environment-catalog__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.environment-current h2,
.environment-current h3,
.environment-current p,
.environment-catalog h2,
.environment-catalog p {
  margin: 0;
}
.environment-current__head p,
.environment-catalog__head p {
  margin-top: 4px;
  color: var(--muted);
  font-size: 0.8rem;
}
.environment-catalog__head > code {
  color: var(--subtle);
  font-family: var(--font-mono);
  font-size: 0.72rem;
}
.environment-current__identity {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.environment-current__icon,
.environment-option__icon {
  display: inline-grid;
  flex: 0 0 auto;
  place-items: center;
  width: 38px;
  height: 38px;
  border: 1px solid color-mix(in srgb, var(--accent) 25%, var(--border));
  border-radius: 7px;
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.environment-current__identity p {
  margin-top: 4px;
  color: var(--muted);
  font-size: 0.82rem;
}
.environment-current__tags,
.environment-option__software {
  display: flex;
  gap: 5px;
  flex-wrap: wrap;
  margin-top: 8px;
}
.environment-current__tags code,
.environment-option__software code {
  padding: 2px 5px;
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--muted);
  background: var(--surface);
  font-family: var(--font-mono);
  font-size: 0.7rem;
}
.environment-current__meta {
  display: grid;
  gap: 8px;
  margin: 0;
}
.environment-current__meta div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding-top: 8px;
  border-top: 1px solid var(--hairline);
}
.environment-current__meta dt {
  color: var(--subtle);
}
.environment-current__meta dd {
  margin: 0;
  text-align: right;
}
.environment-current__actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.environment-current__empty {
  color: var(--subtle);
}
.environment-option__copy {
  display: grid;
  min-width: 0;
  flex: 1;
  gap: 3px;
  text-align: left;
}
.environment-option__copy > span:not(.environment-option__software) {
  color: var(--muted);
  font-size: 0.78rem;
}
.environment-catalog :deep(.async-picker__option) {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-height: 78px;
}
.environment-catalog :deep(.async-picker__list) {
  max-height: 410px;
}
.environment-catalog__selection {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.environment-catalog__selection > div {
  display: grid;
  gap: 2px;
}
.environment-catalog__selection span {
  color: var(--subtle);
  font-size: 0.74rem;
}
@media (max-width: 960px) {
  .environment-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .environment-catalog__selection {
    align-items: stretch;
    flex-direction: column;
  }
}
</style>
