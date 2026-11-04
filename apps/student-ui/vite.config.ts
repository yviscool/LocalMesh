import { defineConfig } from "vite";

export default defineConfig({
  server: {
    // student-ui dev page (distinct port from the teacher console)
    port: 5174,
    strictPort: true,
    watch: { ignored: ["**/.mygo/**", "**/build/**"] },
  },
  build: {
    rollupOptions: {
      // 悬浮条 + 广播全屏接收窗 两个入口
      input: {
        bar: "bar.html",
        viewer: "viewer.html",
      },
    },
  },
});
