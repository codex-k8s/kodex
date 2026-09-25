<script setup lang="ts">
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
}>();
const emit = defineEmits<{
  "update:tools": [value: RuntimeEnvironmentTool[]];
}>();

function selected(command: string): RuntimeEnvironmentTool | undefined {
  return props.tools.find((item) => item.command === command);
}

function toggle(tool: RoleImageArtifactTool): void {
  if (props.disabled) return;
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
      <span>
        {{
          $t("runtime.selectedToolsCount", {
            selected: tools.length,
            total: catalog.length,
          })
        }}
      </span>
    </div>
    <div v-if="loading" class="secondary-text" role="status">
      {{ $t("common.loading") }}
    </div>
    <div v-else-if="catalog.length" class="tool-catalog">
      <article v-for="tool in catalog" :key="tool.name" class="tool-option">
        <label>
          <input
            type="checkbox"
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
        <div v-if="selected(tool.name)" class="tool-fields">
          <label class="field">
            <span>{{ $t("runtime.toolDisplayName") }}</span>
            <input
              :value="selected(tool.name)?.name"
              maxlength="160"
              :disabled="disabled"
              @input="update(tool.name, 'name', $event)"
            />
          </label>
          <label class="field">
            <span>{{ $t("runtime.toolCommand") }}</span>
            <input :value="tool.name" readonly />
          </label>
          <label class="field field--wide">
            <span>{{ $t("common.description") }}</span>
            <VoiceTextarea
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
              :disabled="disabled"
              :value="selected(tool.name)?.usageHint"
              maxlength="500"
              @input="update(tool.name, 'usageHint', $event)"
            />
          </label>
        </div>
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
.tool-heading > span,
.tool-option small {
  color: var(--text-secondary);
  font-size: 0.8rem;
}
.tool-option {
  display: grid;
  gap: 12px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.tool-option > label {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
}
.tool-option > label > span {
  display: grid;
  gap: 2px;
}
.tool-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  padding-left: 26px;
}
.tool-fields .field--wide {
  grid-column: 1 / -1;
}
@media (max-width: 700px) {
  .tool-fields {
    grid-template-columns: 1fr;
  }
  .tool-fields .field--wide {
    grid-column: auto;
  }
}
</style>
