<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { kubernetesChildGateSummary } from "../kubernetes-child-gate";
const props = defineProps<{ gate: OwnerGate }>();
const { t } = useI18n();
const child = computed(() => kubernetesChildGateSummary(props.gate));
</script>
<template>
  <section
    v-if="gate.kubernetesChild"
    class="kubernetes-child-gate"
    :aria-label="t('kubernetesChildGate.title')"
  >
    <h3>{{ t("kubernetesChildGate.title") }}</h3>
    <p v-if="!child" role="alert">{{ t("kubernetesChildGate.invalid") }}</p>
    <template v-else>
      <p>{{ t("kubernetesChildGate.singleAction") }}</p>
      <section :aria-label="t('kubernetesChildGate.submittedChanges')">
        <h4>{{ t("kubernetesChildGate.submittedChanges") }}</h4>
        <p>{{ t("kubernetesChildGate.submittedOnly") }}</p>
        <ul v-if="child.preview.limitations.length" class="preview-limitations">
          <li v-for="reason in child.preview.limitations" :key="reason">
            {{ t(`kubernetesChildGate.limitations.${reason}`) }}
          </li>
        </ul>
        <p v-if="child.preview.secretMaterialRef">
          {{ t("kubernetesChildGate.protectedMaterial") }}
          <code>{{ child.preview.secretMaterialRef }}</code>
        </p>
        <dl v-if="child.preview.options.length">
          <div v-for="option in child.preview.options" :key="option.key">
            <dt>{{ option.key }}</dt>
            <dd>
              <span v-if="option.redacted">{{
                t("kubernetesChildGate.hidden")
              }}</span
              ><code v-else>{{ option.valueJson }}</code>
            </dd>
          </div>
        </dl>
        <ol v-if="child.preview.changes.length">
          <li v-for="(change, index) in child.preview.changes" :key="index">
            <code>{{ change.operation }} {{ change.path }}</code>
            <span v-if="change.from">
              ← <code>{{ change.from }}</code></span
            >
            <p v-if="change.redacted">{{ t("kubernetesChildGate.hidden") }}</p>
            <pre
              v-else-if="change.valueKind !== 'NONE'"
            ><code>{{ change.valueJson }}</code></pre>
          </li>
        </ol>
      </section>
      <dl>
        <div>
          <dt>{{ t("kubernetesChildGate.operation") }}</dt>
          <dd>
            <code>{{ child.operation }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesChildGate.target") }}</dt>
          <dd>
            <code>{{ child.targetRef }}</code> ·
            {{
              t("kubernetesChildGate.revision", { value: child.targetRevision })
            }}
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesChildGate.resource") }}</dt>
          <dd>
            <code
              >{{ child.resource.group || "(core)" }}/{{
                child.resource.version
              }}/{{ child.resource.resource
              }}<template v-if="child.resource.subresource"
                >/{{ child.resource.subresource }}</template
              ></code
            >
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesChildGate.scope") }}</dt>
          <dd>
            <code v-if="child.resource.namespaced">{{
              child.resource.namespace
            }}</code
            ><span v-else>{{ t("kubernetesChildGate.cluster") }}</span>
          </dd>
        </div>
        <div v-if="child.resource.name">
          <dt>{{ t("kubernetesChildGate.name") }}</dt>
          <dd>
            <code>{{ child.resource.name }}</code>
          </dd>
        </div>
        <div v-if="child.intendedName">
          <dt>{{ t("kubernetesChildGate.intendedName") }}</dt>
          <dd>
            <code>{{ child.intendedName }}</code>
          </dd>
        </div>
        <div v-if="child.intendedGenerateName">
          <dt>{{ t("kubernetesChildGate.generatedPrefix") }}</dt>
          <dd>
            <code>{{ child.intendedGenerateName }}</code>
            <p>{{ t("kubernetesChildGate.notCreated") }}</p>
          </dd>
        </div>
        <div v-if="child.resource.uid">
          <dt>UID</dt>
          <dd>
            <code>{{ child.resource.uid }}</code>
          </dd>
        </div>
        <div v-if="child.resource.resourceVersion">
          <dt>ResourceVersion</dt>
          <dd>
            <code>{{ child.resource.resourceVersion }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesChildGate.plan") }}</dt>
          <dd>
            <code>{{ child.planRef }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesChildGate.child") }}</dt>
          <dd>
            <code>{{ child.childRef }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesChildGate.planDigest") }}</dt>
          <dd>
            <code>{{ child.planDigest }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesChildGate.inputDigest") }}</dt>
          <dd>
            <code>{{ child.inputDigest }}</code>
          </dd>
        </div>
      </dl>
    </template>
  </section>
</template>
<style scoped>
.kubernetes-child-gate {
  display: grid;
  gap: 0.75rem;
  padding: 1rem;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  min-width: 0;
}
dl {
  display: grid;
  gap: 0.6rem;
  margin: 0;
}
dl > div {
  display: grid;
  gap: 0.25rem;
  min-width: 0;
}
dt {
  font-weight: 600;
}
dd {
  margin: 0;
  min-width: 0;
}
code,
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
