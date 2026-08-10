<script setup lang="ts">
import { CheckCircle2, RefreshCw } from "@lucide/vue";
import { onMounted, reactive, ref } from "vue";
import { useI18n } from "vue-i18n";

import { useRunsStore } from "@/features/runs/store";
import type {
  OwnerGateListItem,
  OwnerGateResolution,
} from "@/features/runs/model";
import {
  type RunAction,
  useRunDetailsStore,
} from "@/features/runs/details-store";
import {
  formatDateTime,
  formatDuration,
  shortDigest,
} from "@/shared/lib/format";
import AsyncPanel from "@/shared/ui/AsyncPanel.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageHeader from "@/shared/ui/PageHeader.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const { locale } = useI18n();
const runs = useRunsStore();
const owner = useRunDetailsStore();
const detailOpen = ref(false);
const runCommand = reactive<{ action: RunAction; reasonCode: string }>({
  action: "CANCEL",
  reasonCode: "",
});
type OwnerGateDecision = {
  decision: OwnerGateResolution;
  reason: string;
};

const decisions = reactive<Record<string, OwnerGateDecision>>({});

function decisionFor(gate: OwnerGateListItem): OwnerGateDecision {
  return (decisions[gate.id] ??= { decision: "APPROVED", reason: "" });
}

function canResolve(gate: OwnerGateListItem): boolean {
  return gate.resolvable && gate.nextAction === "RESOLVE";
}

async function resolve(gate: OwnerGateListItem): Promise<void> {
  const decision = decisionFor(gate);
  if (decision.reason.trim().length < 1) return;
  const success = await runs.resolveOwnerGate(
    gate,
    decision.decision,
    decision.reason.trim(),
  );
  if (success) decisions[gate.id] = { decision: "APPROVED", reason: "" };
}

async function load(): Promise<void> {
  await Promise.all([runs.loadRuns(), runs.loadGates()]);
}

async function showRun(runRef: string): Promise<void> {
  detailOpen.value = true;
  runCommand.reasonCode = "";
  await owner.loadRun(runRef);
  const nextAction = owner.runDetail.data?.run.nextActions[0];
  if (nextAction) runCommand.action = nextAction;
}

async function executeRunCommand(): Promise<void> {
  if (runCommand.reasonCode.trim().length < 1) return;
  const success = await owner.executeRunAction(
    runCommand.action,
    runCommand.reasonCode.trim(),
  );
  const nextAction = owner.runDetail.data?.run.nextActions[0];
  if (success && nextAction) runCommand.action = nextAction;
}

onMounted(load);
</script>

<template>
  <div class="page">
    <PageHeader :title="$t('runs.title')" :subtitle="$t('runs.ownerSubtitle')"
      ><template #actions
        ><button class="button button--secondary" type="button" @click="load">
          <RefreshCw :size="15" aria-hidden="true" />{{ $t("common.refresh") }}
        </button></template
      ></PageHeader
    >
    <ProblemNotice :problem="runs.mutationProblem ?? owner.mutationProblem" />
    <div class="section-stack" style="margin-top: 15px">
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("runs.run") }}</h2>
        </header>
        <AsyncPanel
          :phase="runs.runs.phase"
          :problem="runs.runs.problem"
          @retry="runs.loadRuns"
          ><div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>{{ $t("common.name") }}</th>
                  <th>{{ $t("common.state") }}</th>
                  <th>{{ $t("runs.workspace") }}</th>
                  <th>{{ $t("runs.agent") }}</th>
                  <th>{{ $t("runs.duration") }}</th>
                  <th>{{ $t("common.updatedAt") }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in runs.runs.data" :key="item.runRef">
                  <td class="data-table__name">
                    <button
                      class="button button--text"
                      type="button"
                      :aria-label="`${$t('common.details')}: ${item.displayName}`"
                      @click="showRun(item.runRef)"
                    >
                      {{ item.displayName }}
                    </button>
                  </td>
                  <td><StatusBadge :state="item.state" /></td>
                  <td>{{ item.workspaceName }}</td>
                  <td>{{ item.agentName }}</td>
                  <td>{{ formatDuration(item.durationSeconds, locale) }}</td>
                  <td>{{ formatDateTime(item.updatedAt, locale) }}</td>
                </tr>
              </tbody>
            </table>
          </div></AsyncPanel
        >
      </section>
      <section class="panel">
        <header class="panel__header">
          <h2>{{ $t("runs.gates") }}</h2>
        </header>
        <AsyncPanel
          :phase="runs.gates.phase"
          :problem="runs.gates.problem"
          @retry="runs.loadGates"
          ><div class="panel__body section-stack">
            <article
              v-for="gate in runs.gates.data"
              :key="gate.id"
              class="gate-row"
            >
              <div class="gate-row__summary">
                <div>
                  <strong>{{ gate.name }}</strong
                  ><small
                    >{{ $t("runs.expires") }}:
                    {{ formatDateTime(gate.expiresAt, locale) }}</small
                  >
                </div>
                <StatusBadge :state="gate.decision" />
              </div>
              <form
                v-if="canResolve(gate)"
                class="form-grid gate-row__form"
                @submit.prevent="resolve(gate)"
              >
                <label class="form-field"
                  ><span>{{ $t("runs.decision") }}</span
                  ><select v-model="decisionFor(gate).decision">
                    <option value="APPROVED">{{ $t("runs.approve") }}</option>
                    <option value="REJECTED">{{ $t("runs.reject") }}</option>
                    <option value="CHANGES_REQUESTED">
                      {{ $t("runs.requestChanges") }}
                    </option>
                    <option value="CANCELLED">{{ $t("common.cancel") }}</option>
                  </select></label
                ><label class="form-field"
                  ><span>{{ $t("runs.reason") }}</span
                  ><input
                    v-model="decisionFor(gate).reason"
                    required
                    minlength="1"
                    maxlength="2048"
                    autocomplete="off"
                /></label>
                <div class="button-row form-field--full">
                  <button
                    class="button button--primary"
                    type="submit"
                    :disabled="runs.mutating"
                  >
                    <CheckCircle2 :size="15" aria-hidden="true" />{{
                      $t("runs.resolve")
                    }}
                  </button>
                </div>
              </form>
            </article>
          </div></AsyncPanel
        >
      </section>
    </div>

    <ModalDialog
      :open="detailOpen"
      :title="owner.runDetail.data?.run.displayName ?? $t('runs.details')"
      @close="detailOpen = false"
      ><AsyncPanel
        :phase="owner.runDetail.phase"
        :problem="owner.runDetail.problem"
        @retry="
          owner.runDetail.data && owner.loadRun(owner.runDetail.data.run.runRef)
        "
        ><div v-if="owner.runDetail.data" class="section-stack">
          <div class="summary-grid">
            <div class="summary-card">
              <small>{{ $t("common.state") }}</small
              ><StatusBadge :state="owner.runDetail.data.run.state" /><span>{{
                owner.runDetail.data.run.runtimeStatus.value
              }}</span>
            </div>
            <div class="summary-card">
              <small>{{ $t("runs.attempt") }}</small
              ><strong>{{ owner.runDetail.data.run.attempt }}</strong
              ><span>{{ owner.runDetail.data.run.trigger.value }}</span>
            </div>
          </div>
          <dl class="detail-list">
            <div>
              <dt>{{ $t("runs.initiator") }}</dt>
              <dd>{{ owner.runDetail.data.run.initiator.value }}</dd>
            </div>
            <div>
              <dt>{{ $t("runs.agent") }}</dt>
              <dd>{{ owner.runDetail.data.run.agent.value }}</dd>
            </div>
            <div>
              <dt>{{ $t("runs.role") }}</dt>
              <dd>{{ owner.runDetail.data.run.role.value }}</dd>
            </div>
            <div>
              <dt>{{ $t("runs.model") }}</dt>
              <dd>{{ owner.runDetail.data.run.model.value }}</dd>
            </div>
            <div>
              <dt>{{ $t("runs.provider") }}</dt>
              <dd>{{ owner.runDetail.data.run.provider.value }}</dd>
            </div>
          </dl>
          <form
            v-if="owner.runDetail.data.run.nextActions.length"
            class="inline-form"
            @submit.prevent="executeRunCommand"
          >
            <label class="form-field"
              ><span>{{ $t("common.actions") }}</span
              ><select v-model="runCommand.action">
                <option
                  v-for="action in owner.runDetail.data.run.nextActions"
                  :key="action"
                  :value="action"
                >
                  {{ action }}
                </option>
              </select></label
            ><label class="form-field"
              ><span>{{ $t("runs.reasonCode") }}</span
              ><input
                v-model="runCommand.reasonCode"
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
            <h3>{{ $t("runs.timeline") }}</h3>
            <div class="timeline">
              <article
                v-for="entry in owner.runTimeline.data"
                :key="entry.eventRef"
              >
                <strong>{{ entry.display }}</strong
                ><span
                  >{{ entry.outcome }} ·
                  {{ formatDateTime(entry.occurredAt, locale) }}</span
                >
              </article>
            </div>
          </section>
          <section>
            <h3>{{ $t("runs.lineage") }}</h3>
            <div class="lineage">
              <article
                v-for="node in owner.runLineage.data?.nodes ?? []"
                :key="node.nodeRef"
                :style="{ marginInlineStart: node.parentRef ? '22px' : '0' }"
              >
                <strong>{{ node.displayName }}</strong
                ><StatusBadge :state="node.state" /><span
                  >{{ node.kind }} · {{ $t("runs.attempt") }}
                  {{ node.attempt }}</span
                >
              </article>
            </div>
          </section>
          <section>
            <h3>{{ $t("runs.artifacts") }}</h3>
            <div class="data-table-wrap">
              <table class="data-table">
                <thead>
                  <tr>
                    <th>{{ $t("common.name") }}</th>
                    <th>{{ $t("runs.mediaType") }}</th>
                    <th>{{ $t("runs.size") }}</th>
                    <th>{{ $t("common.state") }}</th>
                    <th>SHA-256</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="artifact in owner.runArtifacts.data"
                    :key="artifact.artifactRef"
                  >
                    <td>{{ artifact.displayName }}</td>
                    <td>{{ artifact.mediaType }}</td>
                    <td>{{ artifact.sizeBytes }}</td>
                    <td><StatusBadge :state="artifact.status" /></td>
                    <td>
                      <code>{{ shortDigest(artifact.sha256) }}</code>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </div></AsyncPanel
      ></ModalDialog
    >
  </div>
</template>
