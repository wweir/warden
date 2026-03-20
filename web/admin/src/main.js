import { createApp } from "vue";
import { createRouter, createWebHistory } from "vue-router";
import i18n from "./i18n/index.js";
import App from "./App.vue";

const Dashboard = () => import("./views/Dashboard.vue");
const Config = () => import("./views/Config.vue");
const Logs = () => import("./views/Logs.vue");
const ProviderDetail = () => import("./views/ProviderDetail.vue");
const RouteDetail = () => import("./views/RouteDetail.vue");
const Routes = () => import("./views/Routes.vue");
const Providers = () => import("./views/Providers.vue");
const Chat = () => import("./views/Chat.vue");
const ToolHooks = () => import("./views/ToolHooks.vue");

const router = createRouter({
	history: createWebHistory("/_admin/"),
	routes: [
		{ path: "/", component: Dashboard },
		{ path: "/chat", component: Chat },
		{ path: "/routes", component: Routes },
		{ path: "/providers", component: Providers },
		{ path: "/providers/new", component: ProviderDetail, props: { create: true, name: "" } },
		{ path: "/tool-hooks", component: ToolHooks },
		{ path: "/config", component: Config },
		{ path: "/logs", component: Logs },
		{ path: "/routes/new", component: RouteDetail, props: { create: true, prefix: "" } },
		{ path: "/providers/:name", component: ProviderDetail, props: true },
		{ path: "/routes/:prefix(.*)", component: RouteDetail, props: true },
	],
});

createApp(App).use(router).use(i18n).mount("#app");
