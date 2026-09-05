import { useI18n } from "vue-i18n";

const keys = new Set([
  "INTERACTION_AUTHORITY_CHANGED",
  "INTERACTION_DELIVERY_GATE_TITLE",
  "INTERACTION_DELIVERY_GATE_PROMPT",
  "INTERACTION_DELIVERY_APPROVAL_REQUIRED",
]);
export function serverMessageKey(value: string): string | undefined {
  if (!value.startsWith("i18n:")) return undefined;
  const key = value.slice(5);
  return keys.has(key) ? `serverMessages.${key}` : undefined;
}
export function useServerMessage(): (value: string) => string {
  const { t } = useI18n();
  return (value) => {
    const key = serverMessageKey(value);
    return key ? t(key) : value;
  };
}
