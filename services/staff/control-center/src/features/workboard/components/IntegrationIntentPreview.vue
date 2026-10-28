<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { integrationEffectPreview } from "@/features/workboard/integration-effect-preview";
import type { IntegrationIntent } from "@/shared/api/generated/openapi/types.gen";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";

// Обе поверхности согласования используют только безопасную owner-проекцию:
// raw input и неизвестные preview-поля не получают запасного отображения.
const props = defineProps<{ intent: IntegrationIntent }>();
const { t } = useI18n();
const risk = computed(() => {
  const value = props.intent.effectPreview.risk;
  return value === "READ" ||
    value === "WRITE" ||
    value === "SENSITIVE" ||
    value === "DESTRUCTIVE"
    ? value
    : "UNKNOWN";
});
type ApprovalScopePreview = {
  selected: Array<{ path: string; type: string; value: unknown }>;
  mutablePaths: string[];
};
function isApprovalScopeField(
  value: unknown,
): value is ApprovalScopePreview["selected"][number] {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const field = value as Record<string, unknown>;
  return (
    typeof field.path === "string" &&
    typeof field.type === "string" &&
    "value" in field
  );
}
const approvalScope = computed<ApprovalScopePreview | undefined>(() => {
  const candidate = props.intent.effectPreview.approvalScope;
  if (!candidate || typeof candidate !== "object" || Array.isArray(candidate))
    return undefined;
  const scope = candidate as Record<string, unknown>;
  if (
    !Array.isArray(scope.selected) ||
    !scope.selected.every(isApprovalScopeField) ||
    !Array.isArray(scope.mutablePaths) ||
    !scope.mutablePaths.every((path) => typeof path === "string")
  )
    return undefined;
  return scope as ApprovalScopePreview;
});
const effect = computed(() => integrationEffectPreview(props.intent));
const hasTechnicalPreview = computed(
  () => Object.keys(effect.value.technical).length > 0,
);
function approvalPathLabel(path: string): string {
  return path
    .split("/")
    .filter(Boolean)
    .map((part) => part.replaceAll("~1", "/").replaceAll("~0", "~"))
    .join(".");
}
function effectFieldLabel(key: string): string {
  if (key === "body" && effect.value.commentIssue)
    return t("decisions.integrationCommentBody");
  const translation = `decisions.integrationFieldLabels.${key}`;
  return Object.hasOwn(
    {
      issue_number: true,
      body: true,
      title: true,
      comment: true,
      text: true,
      merged_sha: true,
      tree_sha: true,
      bundle_sha256: true,
      runtime_image: true,
    },
    key,
  )
    ? t(translation)
    : key;
}
</script>

<template>
  <section class="decision-integration-intent">
    <h3>
      {{
        effect.commentIssue
          ? $t("decisions.integrationIssueComment", {
              issue: effect.commentIssue,
            })
          : $t("integrationsRedesign.effectPreview")
      }}
    </h3>
    <dl>
      <div v-if="effect.repositoryName">
        <dt>{{ $t("decisions.integrationRepository") }}</dt>
        <dd>{{ effect.repositoryName }}</dd>
      </div>
      <div>
        <dt>{{ $t("integrations.connections") }}</dt>
        <dd>{{ intent.connectionName }}</dd>
      </div>
      <div>
        <dt>{{ $t("decisions.integrationRiskLabel") }}</dt>
        <dd>
          {{ $t(`decisions.integrationRisk.${risk}`) }}
        </dd>
      </div>
    </dl>
    <p v-if="!effect.complete" class="decision-preview-warning" role="status">
      {{ $t("decisions.integrationPreviewIncomplete") }}
    </p>
    <section v-if="effect.fields.length" class="decision-effect-fields">
      <h4>{{ $t("decisions.integrationParameters") }}</h4>
      <dl>
        <div
          v-for="field in effect.fields"
          :key="field.key"
          :class="{
            'decision-effect-field--text': typeof field.value === 'string',
          }"
        >
          <dt>{{ effectFieldLabel(field.key) }}</dt>
          <dd>
            <span v-if="field.hidden">{{
              $t("decisions.integrationValueHidden")
            }}</span>
            <SafeStructuredData
              v-else
              :value="field.value"
              class="decision-effect-value"
              literal
            />
            <small v-if="field.truncated" class="decision-preview-warning">{{
              $t("decisions.integrationValueTruncated")
            }}</small>
          </dd>
        </div>
      </dl>
    </section>
    <section v-if="approvalScope" class="decision-approval-scope">
      <h4>{{ $t("decisions.approvalScopeTitle") }}</h4>
      <p>{{ $t("decisions.approvalScopeExplanation") }}</p>
      <dl>
        <div v-for="field in approvalScope.selected" :key="field.path">
          <dt>
            <code>{{ approvalPathLabel(field.path) }}</code>
          </dt>
          <dd><SafeStructuredData :value="field.value" literal /></dd>
        </div>
      </dl>
      <p>{{ $t("decisions.approvalScopeMutable") }}</p>
      <ul>
        <li v-for="path in approvalScope.mutablePaths" :key="path">
          <code>{{ approvalPathLabel(path) }}</code>
        </li>
      </ul>
    </section>
    <section class="decision-resource-scope">
      <h4>{{ $t("integrations.resourceScope") }}</h4>
      <SafeStructuredData :value="intent.resourceScope.values" literal />
    </section>
    <details class="decision-technical-details">
      <summary>{{ $t("decisions.technicalDetails") }}</summary>
      <dl>
        <div>
          <dt>{{ $t("decisions.integrationOperation") }}</dt>
          <dd>
            <code>{{ intent.operation }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ $t("decisions.integrationCapability") }}</dt>
          <dd>
            <code>{{ intent.capabilityKey }}</code>
          </dd>
        </div>
        <div>
          <dt>{{ $t("decisions.integrationEffectKey") }}</dt>
          <dd>
            <code>{{ intent.effectKey }}</code>
          </dd>
        </div>
      </dl>
      <SafeStructuredData :value="intent.resourceScope" literal />
      <SafeStructuredData
        v-if="hasTechnicalPreview"
        :value="effect.technical"
        literal
      />
    </details>
  </section>
</template>

<style scoped>
.decision-technical-details {
  padding-top: 4px;
  border-top: 1px solid var(--hairline);
  color: var(--muted);
}
.decision-technical-details summary {
  width: fit-content;
  cursor: pointer;
  font-weight: 600;
}
.decision-technical-details > dl {
  display: grid;
  gap: 6px;
  margin: 10px 0;
}
.decision-technical-details > dl > div {
  display: grid;
  grid-template-columns: minmax(120px, 0.35fr) minmax(0, 1fr);
  gap: 10px;
}
.decision-technical-details dt,
.decision-technical-details dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.decision-effect-fields > dl {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 6px;
}
.decision-effect-fields > dl > div {
  display: grid;
  grid-template-columns: minmax(120px, 0.35fr) minmax(0, 1fr);
  gap: 10px;
}
.decision-effect-fields > dl > .decision-effect-field--text {
  grid-template-columns: minmax(0, 1fr);
  gap: 4px;
}
.decision-effect-fields dt,
.decision-effect-fields dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.decision-preview-warning {
  color: var(--warning);
}
.decision-effect-value {
  display: block;
  max-height: 16rem;
  overflow: auto;
}
small.decision-preview-warning {
  display: block;
  margin-top: 4px;
}
.decision-integration-intent {
  display: grid;
  gap: 9px;
  margin-top: 14px;
  padding: 14px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--accent);
  background: var(--panel);
}
.decision-integration-intent h3,
.decision-integration-intent > dl {
  margin: 0;
}
.decision-integration-intent > dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.decision-integration-intent dt {
  color: var(--subtle);
  font-size: 0.74rem;
}
.decision-integration-intent dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.decision-approval-scope {
  display: grid;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.decision-approval-scope h4,
.decision-approval-scope p,
.decision-approval-scope ul {
  margin: 0;
}
.decision-approval-scope ul {
  padding-left: 20px;
}

.decision-resource-scope {
  min-width: 0;
}
.decision-resource-scope h4 {
  margin: 0 0 8px;
}
@media (max-width: 720px) {
  .decision-integration-intent > dl,
  .decision-effect-fields > dl > div {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
