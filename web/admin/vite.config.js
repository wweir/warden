import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import compression from "vite-plugin-compression";

export default defineConfig({
	plugins: [
		vue(),
		compression({
			algorithm: "brotliCompress",
			ext: ".br",
			threshold: 0,
			filter: /\.(js|mjs|json|css|html|svg)$/i,
		}),
	],
	base: "/_admin/",
	build: {
		outDir: "dist",
		emptyOutDir: true,
	},
	server: {
		proxy: {
			"/_admin/api": "http://localhost:9832",
			"/api": "http://localhost:9832",
		},
	},
});
