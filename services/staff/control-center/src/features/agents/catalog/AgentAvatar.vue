<script setup lang="ts">
import { ref, watch } from "vue";

const props = defineProps<{
  initials: string;
  source?: string;
  tone: number;
  size?: "compact" | "regular";
}>();

const failed = ref(false);
watch(
  () => props.source,
  () => {
    failed.value = false;
  },
);
</script>

<template>
  <span
    class="agent-avatar"
    :class="[
      `agent-avatar--tone-${tone}`,
      size === 'compact' ? 'agent-avatar--compact' : '',
    ]"
    aria-hidden="true"
  >
    <img
      v-if="source && !failed"
      :src="source"
      alt=""
      loading="lazy"
      @error="failed = true"
    />
    <span v-else>{{ initials }}</span>
  </span>
</template>

<style scoped>
.agent-avatar {
  display: inline-grid;
  width: 44px;
  height: 44px;
  flex: 0 0 44px;
  overflow: hidden;
  place-items: center;
  border: 1px solid color-mix(in srgb, currentColor 18%, var(--border));
  border-radius: 50%;
  color: #24455f;
  background: #dcebf4;
  font-size: 0.78rem;
  font-weight: 700;
}
.agent-avatar--compact {
  width: 34px;
  height: 34px;
  flex-basis: 34px;
  font-size: 0.7rem;
}
.agent-avatar img {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.agent-avatar--tone-1 {
  color: #38532b;
  background: #e2edd8;
}
.agent-avatar--tone-2 {
  color: #6a412d;
  background: #f4e2d7;
}
.agent-avatar--tone-3 {
  color: #594071;
  background: #ece2f3;
}
.agent-avatar--tone-4 {
  color: #624c19;
  background: #f4ebc9;
}
.agent-avatar--tone-5 {
  color: #2f5351;
  background: #dceceb;
}
</style>
