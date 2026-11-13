import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const demo = process.env.DEVPILOT_ISOLATED_DEMO === "1";
const privateLoop = process.env.DEVPILOT_PRIVATE_LOOP === "1";
const port = demo || privateLoop ? 5174 : 5173;
export default defineConfig({
  define: {
    "import.meta.env.VITE_PRIVATE_LOOP": JSON.stringify(privateLoop ? "true" : "false"),
    "import.meta.env.VITE_ISOLATED_DEMO": JSON.stringify(
      demo ? "true" : "false",
    ),
  },
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port,
    strictPort: true,
    fs: {
      deny: [
        ".env",
        ".env.*",
        "*.{crt,pem}",
        "**/.devpilot-isolated-demo/**",
        "**/.devpilot-private-loop/**",
        "**/.devpilot-dashboard/**",
        "**/.devpilot-dashboard.env",
      ],
    },
    proxy: { "/api": { target: `http://[::1]:${port}`, changeOrigin: false } },
  },
  preview: { host: "127.0.0.1", port: 4173, strictPort: true },
  test: { include: ["src/**/*.test.ts"] },
});
