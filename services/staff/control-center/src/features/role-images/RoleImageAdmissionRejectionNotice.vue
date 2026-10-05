<template>
  <section class="admission-rejection" role="alert">
    <strong>{{ $t("roleImages.admissionRejectedTitle") }}</strong>
    <p>{{ $t("roleImages.admissionRejectedHelp") }}</p>
  </section>
</template>

<style scoped>
.admission-rejection {
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--danger);
  border-radius: 8px;
  background: var(--danger-soft);
  color: var(--danger);
  font-size: 13px;
  overflow-wrap: anywhere;
}
.admission-rejection p {
  margin: 4px 0 0;
}
</style>
