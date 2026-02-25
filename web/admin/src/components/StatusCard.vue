<template>
  <div class="status-card panel" :class="'sc-' + status">
    <div class="sc-header">
      <span class="sc-dot"></span>
      <strong class="sc-name">{{ name }}</strong>
    </div>
    <div class="sc-body">
      <slot />
    </div>
  </div>
</template>

<script setup>
defineProps({
  name: String,
  status: { type: String, default: 'ok' },
})
</script>

<style scoped>
.status-card {
  padding: 14px 16px;
  border-left: 3px solid var(--c-success);
  transition: box-shadow var(--transition), transform var(--transition);
}
.status-card.sc-warn { border-left-color: var(--c-warning); }
.status-card.sc-error { border-left-color: var(--c-danger); }
.sc-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.sc-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--c-success);
  flex-shrink: 0;
}
.sc-warn .sc-dot { background: var(--c-warning); }
.sc-error .sc-dot { background: var(--c-danger); }
.sc-name { font-size: 14px; font-weight: 600; }
.sc-body {
  font-size: 13px;
  color: var(--c-text-2);
  line-height: 1.7;
}
</style>
