<script setup lang="ts">
import { useRoute } from "vue-router";

import AppShell from "@/app/AppShell.vue";
import AuthGate from "@/app/AuthGate.vue";
import { useSessionStore } from "@/features/session/store";

const route = useRoute();
const session = useSessionStore();
</script>

<template>
  <RouterView v-if="route.meta.public" />
  <AppShell v-else-if="session.phase === 'authenticated'" />
  <AuthGate v-else />
</template>
