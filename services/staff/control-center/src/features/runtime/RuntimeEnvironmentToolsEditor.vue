<script setup lang="ts">
import { reactive, useId, watch } from "vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import type {
  RoleImageArtifactTool,
  RuntimeEnvironmentTool,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  tools: RuntimeEnvironmentTool[];
  catalog: RoleImageArtifactTool[];
  imageSelected: boolean;
  disabled: boolean;
  loading?: boolean;
  inventoryAvailable: boolean;
}>();
const fieldPrefix = `environment-tools-${useId()}`;
const expandedTools = reactive(new Map<string, boolean>());
watch(
  () => props.tools,
  (tools) => {
    const names = new Set(tools.map((tool) => tool.command));
    for (const name of expandedTools.keys())
      if (!names.has(name)) expandedTools.delete(name);
    for (const tool of tools)
      if (!expandedTools.has(tool.command))
        expandedTools.set(tool.command, !tool.description.trim());
  },
  { immediate: true },
);
const emit = defineEmits<{
  "update:tools": [value: RuntimeEnvironmentTool[]];
}>();

function selected(command: string): RuntimeEnvironmentTool | undefined {
  return props.tools.find((item) => item.command === command);
}
function toggleDetails(command: string, event: Event): void {
  const element = event.currentTarget;
  if (element instanceof HTMLDetailsElement)
    expandedTools.set(command, element.open);
}

function toggle(tool: RoleImageArtifactTool): void {
  if (props.disabled || !props.inventoryAvailable) return;
  const existing = selected(tool.name);
  emit(
    "update:tools",
    existing
      ? props.tools.filter((item) => item.command !== tool.name)
      : [
          ...props.tools,
          {
            name: tool.name,
            command: tool.name,
            description: "",
            usageHint: "",
          },
        ],
  );
}

function update(
  command: string,
  field: "name" | "description" | "usageHint",
  event: Event,
): void {
  if (props.disabled) return;
  const target = event.target;
  if (
    !(
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement
    )
  )
    return;
  emit(
    "update:tools",
    props.tools.map((tool) =>
      tool.command === command ? { ...tool, [field]: target.value } : tool,
    ),
  );
}
</script>

<template>
  <div class="environment-tools-editor">
    <div class="tool-heading">
      <div>
        <h3>{{ $t("runtime.verifiedTools") }}</h3>
        <p>{{ $t("runtime.verifiedToolsHelp") }}</p>
      </div>
      <span class="tool-count">
        {{
          inventoryAvailable && !loading && catalog.length > 0
            ? $t("runtime.selectedToolsCount", {
                selected: tools.length,
                total: catalog.length,
              })
            : $t("common.selectedCount", { count: tools.length })
        }}
      </span>
    </div>
    <div v-if="loading" class="secondary-text" role="status">
      {{ $t("common.loading") }}
    </div>
    <div
      v-else-if="imageSelected && inventoryAvailable === false"
      class="secondary-text"
      role="status"
    >
      {{ $t("runtime.imageInventoryUnavailable") }}
    </div>
    <div
      v-else-if="catalog.length"
      class="tool-catalog"
      role="region"
      :aria-label="$t('runtime.verifiedTools')"
      tabindex="0"
    >
      <article
        v-for="(tool, index) in catalog"
        :key="tool.name"
        class="tool-option"
      >
        <label>
          <input
            type="checkbox"
            :id="`${fieldPrefix}-${index}-enabled`"
            :name="`${fieldPrefix}-${index}-enabled`"
            :checked="!!selected(tool.name)"
            :disabled="disabled"
            @change="toggle(tool)"
          />
          <span>
            <strong
              ><code>{{ tool.name }}</code></strong
            >
            <small>{{ tool.version }}</small>
          </span>
        </label>
        <details
          v-if="selected(tool.name)"
          class="tool-details"
          :open="expandedTools.get(tool.name)"
          @toggle="toggleDetails(tool.name, $event)"
        >
          <summary :aria-label="`${$t('common.edit')}: ${tool.name}`">
            {{ $t("common.edit") }}
          </summary>
          <div class="tool-fields">
            <label class="field">
              <span>{{ $t("runtime.toolDisplayName") }}</span>
              <input
                :value="selected(tool.name)?.name"
                :id="`${fieldPrefix}-${index}-name`"
                :name="`${fieldPrefix}-${index}-name`"
                maxlength="160"
                :disabled="disabled"
                @input="update(tool.name, 'name', $event)"
              />
            </label>
            <label class="field">
              <span>{{ $t("runtime.toolCommand") }}</span>
              <input
                :value="tool.name"
                :id="`${fieldPrefix}-${index}-command`"
                :name="`${fieldPrefix}-${index}-command`"
                readonly
              />
            </label>
            <label class="field field--wide">
              <span>{{ $t("common.description") }}</span>
              <VoiceTextarea
                class="tool-metadata-editor"
                :disabled="disabled"
                :value="selected(tool.name)?.description"
                maxlength="500"
                required
                @input="update(tool.name, 'description', $event)"
              />
            </label>
            <label class="field field--wide">
              <span>{{ $t("runtime.toolUsageHint") }}</span>
              <VoiceTextarea
                class="tool-metadata-editor"
                :disabled="disabled"
                :value="selected(tool.name)?.usageHint"
                maxlength="500"
                @input="update(tool.name, 'usageHint', $event)"
              />
            </label>
          </div>
        </details>
      </article>
    </div>
    <p v-else class="secondary-text">
      {{
        imageSelected
          ? $t("runtime.noVerifiedTools")
          : $t("runtime.chooseImageFirst")
      }}
    </p>
  </div>
</template>

<style scoped>
.environment-tools-editor,
.tool-catalog {
  display: grid;
  gap: 10px;
}
.tool-catalog {
  gap: 6px;
  max-height: 360px;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
  padding: 2px;
}
.tool-heading {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  padding-top: 6px;
  border-top: 1px solid var(--hairline);
}
.tool-heading p {
  margin: 3px 0 0;
  color: var(--text-secondary);
}
.tool-count {
  flex-shrink: 0;
  white-space: nowrap;
}
.tool-heading > span,
.tool-option small {
  color: var(--text-secondary);
  font-size: 0.8rem;
}
.tool-option {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  min-height: 48px;
  gap: 8px 12px;
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.tool-option > label {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  min-width: 0;
}
.tool-option > label > input {
  flex-shrink: 0;
}
.tool-option > label > span {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 2px 8px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.tool-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin-top: 10px;
}
.tool-fields .field--wide {
  grid-column: 1 / -1;
}
.tool-details > summary {
  cursor: pointer;
  color: var(--text-secondary);
  line-height: 32px;
}
.tool-details[open] {
  grid-column: 1 / -1;
  min-width: 0;
}
:deep(textarea.tool-metadata-editor) {
  min-height: 72px;
  height: 72px;
}
@media (max-width: 700px) {
  .tool-heading {
    flex-wrap: wrap;
  }
  .tool-fields {
    grid-template-columns: 1fr;
  }
  .tool-fields .field--wide {
    grid-column: auto;
  }
}
</style>
