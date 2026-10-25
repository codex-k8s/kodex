<script lang="ts">
// Токены содержат только очищенный текст и разрешённый родителем href.
export type InlineToken = {
  type: "text" | "code" | "strong" | "emphasis" | "link";
  text: string;
  href?: string;
};
</script>

<script setup lang="ts">
defineProps<{ tokens: readonly InlineToken[] }>();

function linkTarget(href: string | undefined): "_self" | "_blank" {
  return href?.startsWith("/") || href?.startsWith("#") ? "_self" : "_blank";
}
</script>

<template>
  <template v-for="(token, tokenIndex) in tokens" :key="tokenIndex">
    <code class="markdown-inline-code" v-if="token.type === 'code'">{{
      token.text
    }}</code>
    <strong v-else-if="token.type === 'strong'">{{ token.text }}</strong>
    <em v-else-if="token.type === 'emphasis'">{{ token.text }}</em>
    <a
      v-else-if="token.type === 'link'"
      :href="token.href"
      rel="noopener noreferrer"
      :target="linkTarget(token.href)"
      >{{ token.text }}</a
    >
    <span v-else>{{ token.text }}</span>
  </template>
</template>
