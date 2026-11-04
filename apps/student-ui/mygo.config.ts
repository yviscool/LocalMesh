import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "LocalMesh Student",
  identifier: "dev.localmesh.student",
  version: "0.1.0",
  devUrl: "http://localhost:5174",
  devCommand: "bun run dev:web",
  buildCommand: "bun run build:web",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
});
