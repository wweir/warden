<template>
  <nav class="navbar">
    <div class="navbar-inner">
      <router-link to="/" class="brand">
        <span class="brand-icon">W</span>
        <span class="brand-text">Warden</span>
      </router-link>
      <button class="menu-toggle" @click="menuOpen = !menuOpen" aria-label="Toggle menu">
        <span class="menu-bar"></span>
        <span class="menu-bar"></span>
        <span class="menu-bar"></span>
      </button>
      <div :class="['nav-links', { open: menuOpen }]">
        <router-link to="/" exact @click="menuOpen = false">Dashboard</router-link>
        <router-link to="/chat" @click="menuOpen = false">Chat</router-link>
        <router-link to="/config" @click="menuOpen = false">Config</router-link>
        <router-link to="/logs" @click="menuOpen = false">Logs</router-link>
      </div>
    </div>
  </nav>
</template>

<script setup>
import { ref } from 'vue'

const menuOpen = ref(false)
</script>

<style scoped>
.navbar {
  background: #1e293b;
  padding: 0 24px;
  position: sticky;
  top: 0;
  z-index: 50;
  box-shadow: 0 1px 3px rgba(0,0,0,0.2);
}
.navbar-inner {
  max-width: 1200px;
  margin: 0 auto;
  display: flex;
  align-items: center;
  height: 52px;
  gap: 32px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  text-decoration: none;
  color: #fff;
}
.brand-icon {
  width: 28px;
  height: 28px;
  background: var(--c-primary);
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 800;
  font-size: 15px;
  color: #fff;
}
.brand-text {
  font-weight: 700;
  font-size: 16px;
  letter-spacing: -0.01em;
}
.nav-links {
  display: flex;
  gap: 4px;
}
.nav-links a {
  color: #94a3b8;
  text-decoration: none;
  font-size: 13px;
  font-weight: 500;
  padding: 6px 12px;
  border-radius: var(--radius-sm);
  transition: all var(--transition);
}
.nav-links a:hover {
  color: #e2e8f0;
  background: rgba(255,255,255,0.08);
}
.nav-links a.router-link-active,
.nav-links a.router-link-exact-active {
  color: #fff;
  background: rgba(255,255,255,0.12);
}

/* Hamburger button - hidden on desktop */
.menu-toggle {
  display: none;
  flex-direction: column;
  justify-content: center;
  gap: 4px;
  background: none;
  border: none;
  cursor: pointer;
  padding: 6px;
  margin-left: auto;
}
.menu-bar {
  display: block;
  width: 20px;
  height: 2px;
  background: #e2e8f0;
  border-radius: 1px;
  transition: all var(--transition);
}

@media (max-width: 768px) {
  .navbar {
    padding: 0 12px;
  }
  .navbar-inner {
    flex-wrap: wrap;
    height: auto;
    min-height: 48px;
    gap: 0;
    padding: 0;
  }
  .brand {
    padding: 10px 0;
  }
  .menu-toggle {
    display: flex;
  }
  .nav-links {
    display: none;
    width: 100%;
    flex-direction: column;
    padding-bottom: 8px;
    gap: 2px;
  }
  .nav-links.open {
    display: flex;
  }
  .nav-links a {
    padding: 10px 12px;
    font-size: 14px;
    border-radius: var(--radius-sm);
  }
}
</style>
