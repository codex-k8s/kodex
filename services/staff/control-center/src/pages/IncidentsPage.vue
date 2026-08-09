<script setup lang="ts">
import { ExternalLink, RefreshCw } from "@lucide/vue";
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useOperationsStore } from "@/features/operations/store";
import {
  type IncidentAction,
  useIncidentDetailsStore,
} from "@/features/incident-details/store";
import { formatDateTime } from "@/shared/lib/format";
import { safeHttpsUrl } from "@/shared/lib/url";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const operations = useOperationsStore();
const owner = useIncidentDetailsStore();
const detailsOpen = ref(false);
const action = reactive<{ action: IncidentAction; reasonCode: string }>({
  action: "ACKNOWLEDGE",
  reasonCode: "",
});

async function show(incidentRef: string): Promise<void> {
  detailsOpen.value = true;
  action.reasonCode = "";
  await owner.loadIncident(incidentRef);
  const nextAction = owner.incident.data?.nextActions[0];
  if (nextAction) action.action = nextAction;
}

async function execute(): Promise<void> {
  if (action.reasonCode.trim().length < 1) return;
  const success = await owner.executeIncidentAction(
    action.action,
    action.reasonCode.trim(),
  );
  const nextAction = owner.incident.data?.nextActions[0];
  if (success && nextAction) action.action = nextAction;
}

onMounted(operations.loadIncidents);
</script>

<template>
  <div class="page">
    <PageHeader
      :title="$t('incidents.title')"
      :subtitle="$t('incidents.ownerSubtitle')"
      ><template #actions
        ><button
          class="button button--secondary"
          type="button"
          @click="operations.loadIncidents"
        >
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="owner.mutationProblem" />
    <AsyncPanel
      :phase="operations.incidents.phase"
      :problem="operations.incidents.problem"
      @retry="operations.loadIncidents"
      ><section class="panel" style="margin-top: 15px">
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>{{ $t("incidents.kind") }}</th>
                <th>{{ $t("incidents.impact") }}</th>
                <th>{{ $t("incidents.workspace") }}</th>
                <th>{{ $t("incidents.severity") }}</th>
                <th>{{ $t("common.state") }}</th>
                <th>{{ $t("incidents.occurredAt") }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in operations.incidents.data"
                :key="item.incidentRef"
              >
                <td class="data-table__name">
                  <button
                    class="button button--text"
                    type="button"
                    :aria-label="`${$t('common.details')}: ${item.kind}`"
                    @click="show(item.incidentRef)"
                  >
                    {{ item.kind }}
                  </button>
                </td>
                <td>{{ item.impact }}</td>
                <td>{{ item.workspace.value }}</td>
                <td><StatusBadge :state="item.severity" /></td>
                <td><StatusBadge :state="item.state" /></td>
                <td>{{ formatDateTime(item.occurredAt, locale) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section></AsyncPanel
    >
    <ModalDialog
      :open="detailsOpen"
      :title="$t('incidents.details')"
      @close="detailsOpen = false"
      ><AsyncPanel
        :phase="owner.incident.phase"
        :problem="owner.incident.problem"
        @retry="
          owner.incident.data &&
          owner.loadIncident(owner.incident.data.incidentRef)
        "
        ><div v-if="owner.incident.data" class="section-stack">
          <div class="summary-grid">
            <div class="summary-card">
              <small>{{ $t("common.state") }}</small
              ><StatusBadge :state="owner.incident.data.state" /><span>{{
                owner.incident.data.severity
              }}</span>
            </div>
            <div class="summary-card">
              <small>{{ $t("incidents.workspace") }}</small
              ><strong>{{ owner.incident.data.workspace.value }}</strong
              ><span>{{ owner.incident.data.run.value }}</span>
            </div>
          </div>
          <div class="callout">
            <div>
              <strong>{{ owner.incident.data.impact }}</strong
              ><span>{{ owner.incident.data.diagnosticSummary }}</span>
            </div>
            <a
              v-if="safeHttpsUrl(owner.incident.data.runbookUrl)"
              class="button button--secondary"
              :href="safeHttpsUrl(owner.incident.data.runbookUrl)"
              target="_blank"
              rel="noopener noreferrer"
              ><ExternalLink :size="14" aria-hidden="true" />Runbook</a
            >
          </div>
          <dl class="detail-list">
            <div>
              <dt>{{ $t("common.correlation") }}</dt>
              <dd>{{ owner.incident.data.safeCorrelation }}</dd>
            </div>
            <div>
              <dt>{{ $t("incidents.fence") }}</dt>
              <dd>{{ owner.incident.data.executionFence }}</dd>
            </div>
          </dl>
          <form
            v-if="owner.incident.data.nextActions.length"
            class="inline-form"
            @submit.prevent="execute"
          >
            <label class="form-field"
              ><span>{{ $t("common.actions") }}</span
              ><select v-model="action.action">
                <option
                  v-for="item in owner.incident.data.nextActions"
                  :key="item"
                  :value="item"
                >
                  {{ item }}
                </option>
              </select></label
            ><label class="form-field"
              ><span>{{ $t("incidents.reasonCode") }}</span
              ><input
                v-model="action.reasonCode"
                required
                minlength="1"
                maxlength="96" /></label
            ><button
              class="button button--danger"
              type="submit"
              :disabled="owner.mutating"
            >
              {{ $t("common.confirm") }}
            </button>
          </form>
          <section>
            <h3>{{ $t("incidents.history") }}</h3>
            <div class="timeline">
              <article
                v-for="entry in owner.incidentHistory.data"
                :key="entry.version"
              >
                <strong>{{ entry.action }} · {{ entry.state }}</strong
                ><span
                  >{{ entry.reasonCode }} ·
                  {{ formatDateTime(entry.occurredAt, locale) }}</span
                >
              </article>
            </div>
          </section>
        </div></AsyncPanel
      ></ModalDialog
    >
  </div>
</template>
