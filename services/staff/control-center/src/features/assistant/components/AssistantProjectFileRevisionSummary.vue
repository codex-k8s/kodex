<script setup lang="ts">
import { computed } from "vue";
import type { AssistantPlanOperation } from "@/shared/api/generated/openapi/types.gen";
import { projectFileRevisionSource } from "../project-file-plan";

const props = defineProps<{
  operation: AssistantPlanOperation;
  edited: boolean;
}>();
const source = computed(() => projectFileRevisionSource(props.operation));
</script>
<template>
  <section
    class="project-file-comparison"
    :aria-label="$t('fileRevision.title')"
  >
    <p v-if="!source" class="field-error" role="alert">
      {{ $t("fileRevision.invalidTarget") }}
    </p>
    <template v-else>
      <strong>{{ operation.target.name }}</strong>
      <dl>
        <div>
          <dt>{{ $t("fileRevision.before") }}</dt>
          <dd>
            {{ $t("fileRevision.number", { revision: source.revision }) }} ·
            {{ operation.before.mediaType }} ·
            {{
              $t("fileRevision.bytes", { count: operation.before.sizeBytes })
            }}
          </dd>
        </div>
        <div>
          <dt>{{ $t("fileRevision.after") }}</dt>
          <dd>{{ $t("fileRevision.assignedOnApply") }}</dd>
        </div>
      </dl>
      <p v-if="edited" class="muted">{{ $t("fileRevision.digestPending") }}</p>
      <details v-else>
        <summary>{{ $t("common.details") }}</summary>
        <p>
          {{ operation.after.mediaType }} ·
          {{ $t("fileRevision.bytes", { count: operation.after.sizeBytes }) }}
        </p>
        <dl>
          <div>
            <dt>{{ $t("fileRevision.before") }}</dt>
            <dd>
              <code>{{ operation.before.digest }}</code>
            </dd>
          </div>
          <div>
            <dt>{{ $t("fileRevision.after") }}</dt>
            <dd>
              <code>{{ operation.after.digest }}</code>
            </dd>
          </div>
        </dl>
      </details>
    </template>
  </section>
</template>
<style scoped>
.project-file-comparison {
  display: grid;
  gap: 7px;
  min-width: 0;
}
.project-file-comparison p,
.project-file-comparison dl {
  margin: 0;
}
.project-file-comparison dl {
  display: grid;
  gap: 5px;
}
.project-file-comparison dt {
  color: var(--muted);
  font-size: 0.75rem;
}
.project-file-comparison dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
}
.project-file-comparison details {
  font-size: 0.8rem;
}
.project-file-comparison details dl {
  margin-top: 6px;
}
</style>
