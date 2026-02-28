<template>
  <div id="app">
    <NavBar />
    <main class="container">
      <router-view />
    </main>
  </div>
</template>

<script setup>
import NavBar from './components/NavBar.vue'
</script>

<style>
:root {
  --c-primary: #4f6ef7;
  --c-primary-hover: #3b5ae0;
  --c-primary-light: #60a5fa;
  --c-primary-bg: #eef1fe;
  --c-success: #10b981;
  --c-success-bg: #d1fae5;
  --c-warning: #f59e0b;
  --c-warning-bg: #fef3c7;
  --c-danger: #ef4444;
  --c-danger-bg: #fee2e2;
  --c-bg: #f1f5f9;
  --c-surface: #ffffff;
  --c-border: #e2e8f0;
  --c-border-light: #f1f5f9;
  --c-text: #1e293b;
  --c-text-2: #475569;
  --c-text-3: #94a3b8;
  --radius: 8px;
  --radius-sm: 6px;
  --radius-lg: 12px;
  --shadow: 0 1px 3px rgba(0,0,0,0.06), 0 1px 2px rgba(0,0,0,0.04);
  --shadow-md: 0 4px 12px rgba(0,0,0,0.07);
  --font-mono: 'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace;
  --transition: 150ms ease;
}

* { margin: 0; padding: 0; box-sizing: border-box; }

body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif;
  background: var(--c-bg);
  color: var(--c-text);
  font-size: 14px;
  line-height: 1.5;
  -webkit-font-smoothing: antialiased;
}

.container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 28px 24px;
}

a { color: var(--c-primary); text-decoration: none; }
a:hover { text-decoration: underline; }

code {
  font-family: var(--font-mono);
  font-size: 0.9em;
  background: var(--c-border-light);
  padding: 1px 5px;
  border-radius: 3px;
}

/* ---- Page title ---- */
.page-title {
  font-size: 20px;
  font-weight: 700;
  margin-bottom: 20px;
}

/* ---- Breadcrumb ---- */
.breadcrumb {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 14px;
  color: var(--c-text-3);
  margin-bottom: 24px;
}
.breadcrumb a {
  color: var(--c-text-3);
  font-weight: 500;
  transition: color var(--transition);
}
.breadcrumb a:hover { color: var(--c-primary); text-decoration: none; }
.breadcrumb .current { color: var(--c-text); font-weight: 600; }
.breadcrumb .sep { color: var(--c-border); }

/* ---- Buttons ---- */
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 7px 16px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all var(--transition);
  line-height: 1.4;
  white-space: nowrap;
}
.btn:disabled { opacity: 0.5; cursor: not-allowed; }
.btn-primary { background: var(--c-primary); color: #fff; }
.btn-primary:not(:disabled):hover { background: var(--c-primary-hover); }
.btn-secondary {
  background: var(--c-surface);
  color: var(--c-text-2);
  border-color: var(--c-border);
}
.btn-secondary:not(:disabled):hover {
  background: var(--c-border-light);
  border-color: #cbd5e1;
}
.btn-danger { background: var(--c-danger); color: #fff; }
.btn-danger:not(:disabled):hover { background: #dc2626; }
.btn-sm { padding: 4px 10px; font-size: 12px; }
.btn-icon {
  background: none;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  cursor: pointer;
  padding: 4px 8px;
  font-size: 14px;
  line-height: 1;
  transition: all var(--transition);
}
.btn-icon:hover { background: var(--c-border-light); }

/* ---- Panel ---- */
.panel {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  box-shadow: var(--shadow);
}

/* ---- Messages ---- */
.msg {
  padding: 10px 14px;
  border-radius: var(--radius-sm);
  font-size: 13px;
  font-weight: 500;
  margin-bottom: 16px;
}
.msg-success, .msg.success { background: var(--c-success-bg); color: #065f46; }
.msg-error, .msg.error { background: var(--c-danger-bg); color: #991b1b; }
.msg-warning, .msg.warning { background: var(--c-warning-bg); color: #92400e; }

/* ---- Tables ---- */
.data-table {
  width: 100%;
  border-collapse: collapse;
}
.data-table th,
.data-table td {
  padding: 10px 14px;
  text-align: left;
  border-bottom: 1px solid var(--c-border-light);
  font-size: 13px;
}
.data-table th {
  font-weight: 600;
  color: var(--c-text-2);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  background: #f8fafc;
}
.data-table tbody tr { transition: background var(--transition); }
.data-table tbody tr:hover { background: #f8fafc; }
.data-table tbody tr:last-child td { border-bottom: none; }

/* ---- Info table (label-value pairs) ---- */
.info-table { width: 100%; }
.info-table td {
  padding: 7px 0;
  border-bottom: 1px solid var(--c-border-light);
  font-size: 13px;
}
.info-table tr:last-child td { border-bottom: none; }
.info-table td:first-child {
  font-weight: 600;
  width: 160px;
  color: var(--c-text-2);
  font-size: 12px;
}

/* ---- Badge ---- */
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}
.badge-ok { background: var(--c-success-bg); color: #065f46; }
.badge-warn { background: var(--c-warning-bg); color: #92400e; }
.badge-error { background: var(--c-danger-bg); color: #991b1b; }
.badge-muted { background: var(--c-border-light); color: var(--c-text-3); }

/* ---- Forms ---- */
.form-input {
  padding: 7px 10px;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  font-size: 13px;
  font-family: var(--font-mono);
  width: 100%;
  transition: border-color var(--transition), box-shadow var(--transition);
  background: var(--c-surface);
  color: var(--c-text);
}
.form-input:focus {
  outline: none;
  border-color: var(--c-primary);
  box-shadow: 0 0 0 3px var(--c-primary-bg);
}
.form-input:read-only {
  background: var(--c-border-light);
  color: var(--c-text-3);
}
select.form-input { cursor: pointer; }

/* ---- Resource link ---- */
.resource-link {
  color: var(--c-primary);
  text-decoration: none;
  font-weight: 500;
}
.resource-link:hover { text-decoration: underline; }

/* ---- Empty state ---- */
.empty { color: var(--c-text-3); font-size: 13px; font-style: italic; }

/* ---- Code block ---- */
.code-block {
  background: #f8fafc;
  border: 1px solid var(--c-border);
  border-radius: var(--radius-sm);
  padding: 12px;
  font-family: var(--font-mono);
  font-size: 12px;
  overflow-x: auto;
  max-height: 400px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-all;
  line-height: 1.6;
}

/* ---- Section within a page ---- */
.detail-layout { display: flex; flex-direction: column; gap: 20px; }
.info-section {
  background: var(--c-surface);
  border: 1px solid var(--c-border);
  border-radius: var(--radius);
  padding: 18px;
  box-shadow: var(--shadow);
}
.info-section h3 {
  margin: 0 0 14px;
  font-size: 13px;
  font-weight: 600;
  color: var(--c-text-2);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

/* ---- Text utilities ---- */
.text-success { color: var(--c-success); }
.text-error { color: var(--c-danger); font-weight: 600; }
.text-warning { color: var(--c-warning); }
.text-muted { color: var(--c-text-3); }

/* ---- Mobile responsive ---- */
@media (max-width: 768px) {
  .container {
    padding: 16px 12px;
  }

  .page-title {
    font-size: 18px;
    margin-bottom: 14px;
  }

  .breadcrumb {
    font-size: 13px;
    margin-bottom: 16px;
    flex-wrap: wrap;
  }

  /* Tables: horizontal scroll wrapper */
  .panel:has(.data-table) {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  .data-table th,
  .data-table td {
    padding: 8px 10px;
    font-size: 12px;
    white-space: nowrap;
  }

  .info-table td:first-child {
    width: 100px;
    font-size: 11px;
  }

  .info-section {
    padding: 14px;
  }

  .btn {
    padding: 8px 14px;
    font-size: 13px;
  }

  .code-block {
    font-size: 11px;
    padding: 10px;
  }
}
</style>
