<script setup lang="ts">
import { useSessionStore } from "@/features/session/store";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const session = useSessionStore();
</script>

<template>
  <main class="auth-gate">
    <section class="auth-card">
      <div class="brand-mark" aria-hidden="true">
        <img src="/logo.png" alt="" />
      </div>
      <h1>{{ $t("auth.title") }}</h1>
      <p>{{ $t("auth.description") }}</p>
      <p v-if="session.phase === 'checking'" role="status">
        {{ $t("auth.checking") }}
      </p>
      <button
        v-else-if="session.phase === 'unauthenticated'"
        class="button button--primary button--large"
        type="button"
        @click="session.beginLogin"
      >
        {{ $t("auth.signIn") }}
      </button>
      <template v-else>
        <ProblemNotice :problem="session.problem" @retry="session.probe" />
      </template>
    </section>
  </main>
</template>
