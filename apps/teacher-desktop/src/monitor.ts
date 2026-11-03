// 独立监控子窗 — 双击机位弹出，MJPEG 高清实时流

import { MonitorService, WindowService } from "./mygo";
import { Channel } from "mygo-runtime";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

const deviceID = new URLSearchParams(location.search).get("device") ?? "";

const img = $<HTMLImageElement>("#m-img");
const loading = $<HTMLElement>("#m-loading");
const resEl = $<HTMLElement>("#m-res");
const fpsEl = $<HTMLElement>("#m-fps");

document.title = `实时监控 — ${deviceID || "未指定"}`;
$<HTMLElement>("#m-name").textContent = deviceID || "未指定设备";

/* 全屏切换（作用于本窗口） */
const fullscreenBtn = $<HTMLButtonElement>("#m-fullscreen");
fullscreenBtn.addEventListener("click", () => {
  WindowService.toggleFullscreen();
});
document.addEventListener("keydown", async (e) => {
  if (e.key !== "Escape") return;
  try {
    if (await WindowService.isFullscreen()) WindowService.toggleFullscreen();
  } catch { /* window may be closing */ }
});

/* 启动：先出一张高清快照，随后 MJPEG 流接管 */
async function boot() {
  if (!deviceID) {
    loading.textContent = "未指定设备";
    return;
  }

  let frames = 0;
  window.setInterval(() => {
    fpsEl.textContent = `${frames} fps`;
    frames = 0;
  }, 1000);

  try {
    const snap = await MonitorService.snapshot(deviceID);
    img.src = `data:image/jpeg;base64,${snap.frame}`;
    img.style.display = "block";
    loading.style.display = "none";
    resEl.textContent = `${snap.info.width}×${snap.info.height}`;
  } catch (err) {
    // 旧版 Agent 无高清端点：退回主控制台的缩略图
    loading.textContent = `高清画面不可用（${err instanceof Error ? err.message : String(err)}）`;
  }

  const stream = new Channel<string>();
  (async () => {
    try {
      for await (const frame of stream) {
        img.src = `data:image/jpeg;base64,${frame}`;
        frames++;
      }
    } catch { /* 流结束（窗口关闭） */ }
  })();
  MonitorService.stream(deviceID, 12, stream).catch(() => {});
}

boot();
