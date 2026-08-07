<script setup lang="ts">
import { LogIn, RefreshCw, ShieldCheck } from "@lucide/vue";

import { useSessionStore } from "@/features/session/store";

const session = useSessionStore();
</script>

<template>
  <main class="auth-gate">
    <section class="auth-gate__panel">
      <div class="brand-mark" aria-hidden="true">M</div>
      <ShieldCheck :size="34" aria-hidden="true" />
      <h1>{{ $t("auth.title") }}</h1>
      <p>{{ $t("auth.description") }}</p>
      <div
        v-if="session.phase === 'checking'"
        class="auth-gate__status"
        role="status"
      >
        {{ $t("auth.checking") }}
      </div>
      <template v-else-if="session.phase === 'unauthenticated'">
        <button
          class="button button--primary"
          type="button"
          @click="session.beginLogin"
        >
          <LogIn :size="17" aria-hidden="true" />{{ $t("auth.signIn") }}
        </button>
      </template>
      <template v-else>
        <div class="notice notice--danger" role="alert">
          {{
            session.phase === "forbidden"
              ? $t("common.forbidden")
              : $t("common.error")
          }}
        </div>
        <button
          class="button button--secondary"
          type="button"
          @click="session.probe"
        >
          <RefreshCw :size="16" aria-hidden="true" />{{ $t("common.retry") }}
        </button>
      </template>
    </section>
  </main>
</template>
