<script setup lang="ts">
import {
  Activity,
  Bot,
  Maximize2,
  PlugZap,
  UserRoundCheck,
  Workflow,
  X,
} from "@lucide/vue";
import { computed } from "vue";
import type { Component } from "vue";
import { useI18n } from "vue-i18n";

import type {
  Agent,
  Artifact,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = withDefaults(
  defineProps<{
    node: RunNode;
    nodes: RunNode[];
    artifacts: Artifact[];
    projectRef: string;
    run?: Run;
    agent?: Agent;
  }>(),
  { run: undefined, agent: undefined },
);
const emit = defineEmits<{
  close: [];
  activity: [nodeRef: string];
  details: [];
}>();
const { locale } = useI18n();

const parentNode = computed(() =>
  props.nodes.find((node) => node.ref === props.node.parentNodeRef),
);
const nodeArtifacts = computed(() => {
  const refs = new Set(props.node.artifactRefs);
  return props.artifacts.filter((artifact) => refs.has(artifact.ref));
});
const sessionNode = computed(
  () =>
    props.node.type === "ROOT_PROCESS" || props.node.type === "AGENT_EXECUTION",
);

function nodeIcon(type: RunNode["type"]): Component {
  switch (type) {
    case "ROOT_PROCESS":
      return Workflow;
    case "HUMAN_GATE":
      return UserRoundCheck;
    case "EXTERNAL_ACTION":
      return PlugZap;
    case "AGENT_EXECUTION":
      return Bot;
  }
}

function formatDate(value: string): string {
  return new Date(value).toLocaleString(locale.value);
}
</script>

<template>
  <section class="run-node-inspector" :aria-label="$t('runs.context')">
    <header class="run-node-inspector__header">
      <span class="run-node-inspector__icon">
        <img v-if="agent?.avatarUrl" :src="agent.avatarUrl" :alt="agent.name" />
        <component
          :is="nodeIcon(node.type)"
          v-else
          :size="20"
          aria-hidden="true"
        />
      </span>
      <div>
        <h2>{{ node.displayName }}</h2>
        <p>
          {{ $t(sessionNode ? "runs.sessionNode" : "runs.controlNode") }} ·
          {{ agent?.name || node.role || $t(`runs.nodeTypes.${node.type}`) }}
        </p>
      </div>
      <StatusBadge :state="node.state" />
      <button
        class="icon-button run-node-inspector__close"
        type="button"
        :aria-label="$t('common.close')"
        :title="$t('common.close')"
        @click="emit('close')"
      >
        <X :size="18" aria-hidden="true" />
      </button>
    </header>

    <div class="run-node-inspector__body">
      <ProblemNotice
        v-if="node.safeErrorCode"
        :problem="
          asProblem({
            status: 500,
            code: node.safeErrorCode,
            detail: node.safeErrorMessage,
            correlationId: '',
          })
        "
        compact
      />

      <div class="run-node-inspector__status">
        <SafeMarkdown
          :content="node.progressSummary || $t('runs.waitingForActivity')"
        />
      </div>

      <dl class="run-node-inspector__metadata">
        <div v-if="run">
          <dt>{{ $t("runs.runContext") }}</dt>
          <dd>
            <strong>{{ run.title }}</strong>
            <small>
              {{ run.target.displayName }} ·
              {{ $t("runs.attempt", { attempt: run.attempt }) }}
            </small>
          </dd>
        </div>
        <div v-if="agent">
          <dt>{{ $t("agents.profile") }}</dt>
          <dd>{{ agent.name }}</dd>
        </div>
        <div>
          <dt>{{ $t("runs.attempt", { attempt: node.attempt }) }}</dt>
          <dd>{{ node.attempt }}</dd>
        </div>
        <div v-if="parentNode">
          <dt>{{ $t("common.source") }}</dt>
          <dd>{{ parentNode.displayName }}</dd>
        </div>
        <div v-if="node.inputSummary">
          <dt>{{ $t("common.input") }}</dt>
          <dd><SafeMarkdown :content="node.inputSummary" /></dd>
        </div>
        <div v-if="node.startedAt">
          <dt>{{ $t("runs.startedAt") }}</dt>
          <dd>{{ formatDate(node.startedAt) }}</dd>
        </div>
        <div v-if="node.finishedAt">
          <dt>{{ $t("runs.finishedAt") }}</dt>
          <dd>{{ formatDate(node.finishedAt) }}</dd>
        </div>
        <div v-if="node.integrationNames?.length">
          <dt>{{ $t("agents.integrations") }}</dt>
          <dd class="run-node-inspector__chips">
            <span v-for="name in node.integrationNames" :key="name">
              {{ name }}
            </span>
          </dd>
        </div>
        <div v-if="agent?.runtimeRevision">
          <dt>{{ $t("agents.runtimeRevision") }}</dt>
          <dd>
            <code>{{ agent.runtimeRevision }}</code>
            <small v-if="agent.runtimeProvider || agent.runtimeModel">
              {{
                [agent.runtimeProvider, agent.runtimeModel]
                  .filter(Boolean)
                  .join(" · ")
              }}
            </small>
          </dd>
        </div>
        <div v-if="node.callbackSummary">
          <dt>{{ $t("runs.callback") }}</dt>
          <dd><SafeMarkdown :content="node.callbackSummary" /></dd>
        </div>
      </dl>

      <section
        v-if="node.childRunRefs.length"
        class="run-node-inspector__section"
      >
        <h3>{{ $t("runs.childRuns") }}</h3>
        <RouterLink
          v-for="childRef in node.childRunRefs"
          :key="childRef"
          :to="runPath(childRef, projectRef)"
        >
          {{ $t("runs.openChildRun") }}
        </RouterLink>
      </section>

      <section
        v-if="node.artifactRefs.length"
        class="run-node-inspector__section"
      >
        <h3>{{ $t("runs.artifacts") }}</h3>
        <div v-if="nodeArtifacts.length" class="run-node-inspector__artifacts">
          <div v-for="artifact in nodeArtifacts" :key="artifact.ref">
            <span>{{ artifact.fileName }}</span>
            <StatusBadge :state="artifact.scanState" />
          </div>
        </div>
        <p v-else class="run-node-inspector__muted">
          {{ $t("common.unavailable") }}
        </p>
      </section>
    </div>

    <footer class="run-node-inspector__footer">
      <button class="button" type="button" @click="emit('activity', node.ref)">
        <Activity :size="17" aria-hidden="true" />
        {{ $t("runs.activity") }}
      </button>
      <button
        class="button button--primary"
        type="button"
        @click="emit('details')"
      >
        <Maximize2 :size="16" aria-hidden="true" />
        {{ $t("common.details") }}
      </button>
    </footer>
  </section>
</template>

<style scoped>
.run-node-inspector {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  height: 100%;
  background: var(--panel);
}
.run-node-inspector__header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 10px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.run-node-inspector__header h2,
.run-node-inspector__header p {
  margin: 0;
}
.run-node-inspector__header h2 {
  overflow-wrap: anywhere;
  font-size: 1rem;
}
.run-node-inspector__header p {
  margin-top: 3px;
  color: var(--muted);
  font-size: 0.78rem;
}
.run-node-inspector__icon {
  display: grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--accent);
  background: var(--panel);
}
.run-node-inspector__icon img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.run-node-inspector__close {
  display: none;
}
.run-node-inspector__body {
  display: grid;
  align-content: start;
  gap: 10px;
  min-height: 0;
  padding: 12px 14px 16px;
  overflow: auto;
}
.run-node-inspector__status {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--accent) 24%, var(--border));
  border-radius: 8px;
  background: var(--accent-soft);
  font-size: 0.86rem;
}
.run-node-inspector__status :deep(p) {
  margin: 0;
}
.run-node-inspector__metadata {
  display: grid;
  gap: 0;
  margin: 0;
}
.run-node-inspector__metadata > div {
  display: grid;
  grid-template-columns: minmax(105px, 0.36fr) minmax(0, 1fr);
  gap: 12px;
  padding: 7px 0;
  border-bottom: 1px solid var(--border);
}
.run-node-inspector__metadata dt {
  color: var(--subtle);
  font-size: 0.77rem;
}
.run-node-inspector__metadata dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 0.84rem;
}
.run-node-inspector__metadata dd > strong,
.run-node-inspector__metadata dd > small {
  display: block;
}
.run-node-inspector__metadata dd > small {
  margin-top: 3px;
  color: var(--subtle);
  font-size: 0.72rem;
}
.run-node-inspector__metadata :deep(p) {
  margin: 0;
}
.run-node-inspector__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
}
.run-node-inspector__chips span {
  padding: 3px 6px;
  border-radius: 6px;
  background: var(--accent-soft);
  font-size: 0.75rem;
}
.run-node-inspector__section {
  display: grid;
  gap: 7px;
}
.run-node-inspector__section h3 {
  margin: 0;
  font-size: 0.86rem;
}
.run-node-inspector__section a {
  width: fit-content;
}
.run-node-inspector__artifacts {
  display: grid;
}
.run-node-inspector__artifacts > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
.run-node-inspector__muted {
  margin: 0;
  color: var(--muted);
  font-size: 0.82rem;
}
.run-node-inspector__footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: auto;
  padding: 9px 14px;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
@media (max-width: 760px) {
  .run-node-inspector {
    box-shadow: 0 -18px 44px rgba(16, 22, 30, 0.18);
  }
  .run-node-inspector__header {
    grid-template-columns: auto minmax(0, 1fr) auto auto;
  }
  .run-node-inspector__close {
    display: inline-grid;
  }
  .run-node-inspector__body {
    padding-bottom: 14px;
  }
}
</style>
