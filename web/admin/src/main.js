import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import Dashboard from './views/Dashboard.vue'
import Config from './views/Config.vue'
import Logs from './views/Logs.vue'
import ProviderDetail from './views/ProviderDetail.vue'
import McpDetail from './views/McpDetail.vue'
import McpToolDetail from './views/McpToolDetail.vue'
import RouteDetail from './views/RouteDetail.vue'
import Chat from './views/Chat.vue'

const router = createRouter({
  history: createWebHistory('/_admin/'),
  routes: [
    { path: '/', component: Dashboard },
    { path: '/chat', component: Chat },
    { path: '/config', component: Config },
    { path: '/logs', component: Logs },
    { path: '/providers/:name', component: ProviderDetail, props: true },
    { path: '/mcp/:name', component: McpDetail, props: true },
    { path: '/mcp/:mcp/tools/:tool', component: McpToolDetail, props: true },
    { path: '/routes/:prefix(.*)', component: RouteDetail, props: true },
  ],
})

createApp(App).use(router).mount('#app')
