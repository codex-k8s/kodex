<script setup lang="ts">
import { Clock3, FileWarning, LockKeyhole, ShieldCheck } from "@lucide/vue";
import { useI18n } from "vue-i18n";

const { t } = useI18n();
</script>

<template>
  <section class="approval-panel" aria-labelledby="approval-title">
    <header class="panel-heading">
      <div>
        <h2 id="approval-title">
          {{ t("integrationsRedesign.approvalsTitle") }}
        </h2>
        <p>{{ t("integrationsRedesign.approvalsDescription") }}</p>
      </div>
      <span class="locked-state">
        <LockKeyhole :size="14" aria-hidden="true" />
        {{ t("integrationsRedesign.backendUnavailableShort") }}
      </span>
    </header>

    <div class="approval-workspace">
      <section class="approval-queue" aria-label="approval queue">
        <header>
          <h3>{{ t("integrationsRedesign.approvalQueue") }}</h3>
          <span>—</span>
        </header>
        <div class="locked-empty">
          <Clock3 :size="26" aria-hidden="true" />
          <strong>{{
            t("integrationsRedesign.approvalReadUnavailable")
          }}</strong>
          <p>{{ t("integrationsRedesign.approvalReadGap") }}</p>
        </div>
      </section>

      <section class="approval-preview" aria-label="effect preview">
        <header>
          <div>
            <span class="preview-eyebrow">Human Gate</span>
            <h3>{{ t("integrationsRedesign.effectPreview") }}</h3>
          </div>
          <span class="locked-state">
            <FileWarning :size="14" aria-hidden="true" />
            {{ t("integrationsRedesign.noIntentSelected") }}
          </span>
        </header>
        <div class="preview-grid" aria-hidden="true">
          <span v-for="index in 6" :key="index"></span>
        </div>
        <div class="approval-actions">
          <button class="button button--primary" type="button" disabled>
            <ShieldCheck :size="15" aria-hidden="true" />
            {{ t("common.approve") }}
          </button>
          <button class="button" type="button" disabled>
            {{ t("common.requestChanges") }}
          </button>
          <button class="button button--danger" type="button" disabled>
            {{ t("common.reject") }}
          </button>
        </div>
      </section>
    </div>

    <div class="fail-closed-notice">
      <LockKeyhole :size="18" aria-hidden="true" />
      <span>{{ t("integrationsRedesign.approvalFailClosed") }}</span>
    </div>
  </section>
</template>

<style scoped>
.approval-panel {
  display: grid;
  gap: 14px;
}
.panel-heading,
.approval-queue > header,
.approval-preview > header,
.approval-actions,
.locked-state,
.fail-closed-notice {
  display: flex;
  align-items: center;
  gap: 8px;
}
.panel-heading,
.approval-queue > header,
.approval-preview > header {
  justify-content: space-between;
  align-items: flex-start;
}
.panel-heading h2,
.panel-heading p,
.approval-queue h3,
.approval-preview h3,
.locked-empty p {
  margin-bottom: 0;
}
.panel-heading p,
.locked-empty p {
  color: var(--muted);
}
.locked-state {
  width: max-content;
  padding: 4px 7px;
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--muted);
  background: var(--panel);
  font-size: 0.76rem;
  white-space: nowrap;
}
.approval-workspace {
  display: grid;
  grid-template-columns: minmax(260px, 0.75fr) minmax(0, 1.4fr);
  gap: 14px;
}
.approval-queue,
.approval-preview {
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.approval-queue > header,
.approval-preview > header {
  min-height: 54px;
  padding: 12px 13px;
  border-bottom: 1px solid var(--border);
  background: var(--panel);
}
.locked-empty {
  display: grid;
  justify-items: center;
  gap: 8px;
  padding: 62px 24px;
  text-align: center;
}
.preview-eyebrow {
  color: var(--warning);
  font-family: var(--font-mono);
  font-size: 0.7rem;
}
.preview-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  padding: 18px;
}
.preview-grid span {
  min-height: 58px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background:
    linear-gradient(var(--hairline), var(--hairline)) 10px 12px / 44% 7px
      no-repeat,
    linear-gradient(var(--panel), var(--panel)) 10px 28px / 72% 18px no-repeat;
}
.approval-actions {
  flex-wrap: wrap;
  padding: 13px 18px;
  border-top: 1px solid var(--border);
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
@media (max-width: 800px) {
  .panel-heading,
  .approval-workspace {
    align-items: stretch;
  }
  .panel-heading {
    flex-direction: column;
  }
  .approval-workspace {
    grid-template-columns: 1fr;
  }
  .preview-grid {
    grid-template-columns: 1fr 1fr;
  }
  .approval-actions .button {
    flex: 1 1 150px;
    min-height: 44px;
  }
}
</style>
