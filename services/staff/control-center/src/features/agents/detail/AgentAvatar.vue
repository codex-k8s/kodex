<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { sameOriginAvatarUrl } from "@/features/agents/catalog/model";
import { agentInitials } from "@/features/agents/detail/model";

const props = withDefaults(
  defineProps<{
    name: string;
    url?: string;
    label: string;
    size?: "medium" | "large";
  }>(),
  { size: "large", url: "" },
);
const imageFailed = ref(false);
const initials = computed(() => agentInitials(props.name));
const safeUrl = computed(() => sameOriginAvatarUrl(props.url));
const showImage = computed(() => Boolean(safeUrl.value) && !imageFailed.value);

watch(
  () => props.url,
  () => {
    imageFailed.value = false;
  },
);
</script>

<template>
  <span
    class="agent-avatar"
    :class="`agent-avatar--${size}`"
    :aria-label="label"
    role="img"
  >
    <img
      v-if="showImage"
      :src="safeUrl"
      alt=""
      referrerpolicy="no-referrer"
      @error="imageFailed = true"
    />
    <span v-else aria-hidden="true">{{ initials }}</span>
  </span>
</template>

<style scoped>
.agent-avatar {
  display: inline-grid;
  flex: 0 0 auto;
  place-items: center;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--accent) 36%, var(--border));
  border-radius: 50%;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-family: var(--font-mono);
  font-weight: 700;
}
.agent-avatar--medium {
  width: 44px;
  height: 44px;
  font-size: 0.95rem;
}
.agent-avatar--large {
  width: 88px;
  height: 88px;
  font-size: 1.55rem;
}
.agent-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
</style>
