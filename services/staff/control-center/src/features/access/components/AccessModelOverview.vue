<script setup lang="ts">
import { FolderKey, KeyRound, ShieldCheck, UsersRound } from "@lucide/vue";

defineProps<{
  projectContext: boolean;
}>();

const layers = [
  { key: "identity", icon: UsersRound },
  { key: "platform", icon: ShieldCheck },
  { key: "project", icon: FolderKey },
  { key: "effective", icon: KeyRound },
] as const;
</script>

<template>
  <section class="access-model" :aria-label="$t('access.model.title')">
    <header>
      <div>
        <h2>{{ $t("access.model.title") }}</h2>
        <p>
          {{
            $t(
              projectContext
                ? "access.model.projectContext"
                : "access.model.organizationContext",
            )
          }}
        </p>
      </div>
      <span class="authority-badge">{{
        $t("access.model.authorityBadge")
      }}</span>
    </header>
    <ol>
      <li v-for="(layer, index) in layers" :key="layer.key">
        <span class="layer-index">{{ index + 1 }}</span>
        <component :is="layer.icon" :size="18" aria-hidden="true" />
        <div>
          <strong>{{ $t(`access.model.layers.${layer.key}.title`) }}</strong>
          <p>{{ $t(`access.model.layers.${layer.key}.description`) }}</p>
        </div>
      </li>
    </ol>
    <p class="model-rule">{{ $t("access.model.rule") }}</p>
  </section>
</template>

<style scoped>
.access-model {
  display: grid;
  gap: 12px;
  margin-bottom: 18px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.access-model > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.access-model h2,
.access-model p {
  margin: 0;
}
.access-model header p,
.access-model li p {
  color: var(--muted);
}
.authority-badge {
  flex: 0 0 auto;
  padding: 4px 7px;
  border-radius: 6px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-family: var(--font-mono);
  font-size: 0.72rem;
}
.access-model ol {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.access-model li {
  position: relative;
  display: grid;
  grid-template-columns: auto auto minmax(0, 1fr);
  align-items: start;
  gap: 8px;
  min-height: 94px;
  padding: 11px;
  border: 1px solid var(--hairline);
  border-radius: 7px;
  background: var(--panel);
}
.layer-index {
  display: inline-grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  color: var(--muted);
  background: var(--surface);
  font-family: var(--font-mono);
  font-size: 0.72rem;
}
.access-model li svg {
  margin-top: 1px;
  color: var(--accent-strong);
}
.access-model li p {
  margin-top: 4px;
  font-size: 0.82rem;
}
.model-rule {
  padding: 9px 11px;
  border-left: 3px solid var(--accent);
  background: var(--accent-soft);
  font-size: 0.86rem;
}
@media (max-width: 980px) {
  .access-model ol {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 620px) {
  .access-model > header {
    flex-direction: column;
  }
  .access-model ol {
    grid-template-columns: 1fr;
  }
  .access-model li {
    min-height: auto;
  }
}
</style>
