<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
const props = defineProps<{ preview: PromptTemplatePreview }>();
const { t } = useI18n();
const platformSlots = computed(() =>
  props.preview.slots.filter((slot) => slot.source === "PLATFORM"),
);
</script>
<template>
  <div class="prompt-context-details">
    <ul v-if="preview.diagnostics.length" role="status">
      <li v-for="(diagnostic, index) in preview.diagnostics" :key="index">
        <code>{{ diagnostic.code }}</code> {{ diagnostic.message }}
        <span v-if="diagnostic.line > 0"
          >({{ diagnostic.line }}:{{ diagnostic.column }})</span
        >
      </li>
    </ul>
    <p v-if="!preview.complete" role="status">
      {{ t("promptDetails.incomplete") }}
    </p>
    <section>
      <h4>{{ t("promptDetails.platform") }}</h4>
      <ul>
        <li v-for="slot in platformSlots" :key="slot.slot">
          {{ t(`promptDetails.slots.${slot.slot}`) }}
        </li>
      </ul>
    </section>
    <details>
      <summary>{{ t("promptDetails.order") }}</summary>
      <ol>
        <li v-for="(section, index) in preview.sections" :key="index">
          <strong
            >{{ t(`promptDetails.${section.source}`)
            }}<template v-if="section.slot">
              · {{ t(`promptDetails.slots.${section.slot}`) }}</template
            ></strong
          ><SafeMarkdown :content="section.content" />
        </li>
      </ol>
    </details>
    <details v-if="preview.runtimeDiff">
      <summary>{{ t("promptDetails.changes") }}</summary>
      <p>
        <code>{{ preview.runtimeDiff.previousRevisionRef }}</code> ·
        <code>{{ preview.runtimeDiff.digest }}</code>
      </p>
      <ul>
        <li
          v-for="change in preview.runtimeDiff.changes"
          :key="change.component"
        >
          <strong>{{
            t(`promptDetails.components.${change.component}`)
          }}</strong>
          <dl>
            <template
              v-for="(values, side) in {
                previous: change.previous,
                current: change.current,
              }"
              :key="side"
              ><dt>{{ t(`promptDetails.${side}`) }}</dt>
              <dd v-for="(value, index) in values" :key="index">
                <code
                  >{{ value.ref }} {{ value.version }} {{ value.digest }}
                  {{ value.value }}</code
                >
              </dd></template
            >
          </dl>
        </li>
      </ul>
    </details>
    <details>
      <summary>{{ t("promptDetails.versions") }}</summary>
      <dl>
        <dt>{{ t("promptDetails.context") }}</dt>
        <dd>
          <code>{{ preview.contextPin?.digest }}</code>
        </dd>
        <dt>{{ t("promptDetails.template") }}</dt>
        <dd>
          <code>{{ preview.templateRef }} · {{ preview.templateDigest }}</code>
        </dd>
        <dt>{{ t("promptDetails.service") }}</dt>
        <dd>
          <code
            >{{ preview.serviceTemplateRevision }} ·
            {{ preview.serviceTemplateDigest }} · {{ preview.locale }}</code
          >
        </dd>
        <dt>{{ t("promptDetails.snapshot") }}</dt>
        <dd>
          <code>{{ preview.variableSnapshotDigest }}</code>
        </dd>
        <dt>{{ t("promptDetails.materialization") }}</dt>
        <dd>
          <code>{{ preview.materializationDigest }}</code>
        </dd>
      </dl>
    </details>
  </div>
</template>
<style scoped>
.prompt-context-details {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.prompt-context-details code {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.prompt-context-details dd {
  margin-inline-start: 0;
}
.prompt-context-details li {
  min-width: 0;
}
</style>
