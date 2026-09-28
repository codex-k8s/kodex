<script setup lang="ts">
import {
  Activity,
  BookOpenText,
  Bot,
  CalendarClock,
  Container,
  FileStack,
  FolderKanban,
  KeyRound,
  Layers3,
  PackageOpen,
  PlugZap,
  Settings,
  ShieldCheck,
  UsersRound,
  Workflow,
} from "@lucide/vue";
import { computed } from "vue";

type EntityIconKind =
  | "PROJECT"
  | "AGENT"
  | "WORKFLOW"
  | "AUTOMATION"
  | "ENVIRONMENT"
  | "SECRET"
  | "MEMBER"
  | "ROLE_IMAGE"
  | "INTEGRATION"
  | "CONFIGURATION"
  | "RUN"
  | "FILE"
  | "GATE"
  | "SKILL"
  | "MEMORY";

const props = defineProps<{ kind: EntityIconKind; size?: number }>();
const icons = {
  PROJECT: FolderKanban,
  AGENT: Bot,
  WORKFLOW: Workflow,
  AUTOMATION: CalendarClock,
  ENVIRONMENT: Layers3,
  SECRET: KeyRound,
  MEMBER: UsersRound,
  ROLE_IMAGE: Container,
  INTEGRATION: PlugZap,
  CONFIGURATION: Settings,
  RUN: Activity,
  FILE: FileStack,
  GATE: ShieldCheck,
  SKILL: PackageOpen,
  MEMORY: BookOpenText,
} as const;
const icon = computed(() => icons[props.kind]);
</script>

<template>
  <span class="entity-icon" :class="`entity-icon--${kind.toLowerCase()}`">
    <component :is="icon" :size="size ?? 18" aria-hidden="true" />
  </span>
</template>

<style scoped>
.entity-icon {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 34px;
  height: 34px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--accent-strong);
  background: var(--panel);
}
.entity-icon--secret,
.entity-icon--gate {
  color: var(--warning);
}
.entity-icon--role_image {
  color: var(--text-secondary);
}
</style>
