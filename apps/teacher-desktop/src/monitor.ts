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

/* ---------- 远程接管模式 ---------- */

import { ControlService } from "./mygo";

let controlActive = false;

const controlBtn = document.getElementById("m-control") as HTMLButtonElement | null;
const stage = document.querySelector<HTMLElement>(".monitor-stage") ?? document.body;

function sendInput(payload: Partial<import("./mygo").InputEventPayload> & { type: number }) {
  if (!controlActive) return;
  const full = {
    type: payload.type,
    x: payload.x ?? 0,
    y: payload.y ?? 0,
    button: payload.button ?? 0,
    wheel_delta: payload.wheel_delta ?? 0,
    key_code: payload.key_code ?? 0,
    normalized: payload.normalized ?? true,
  };
  void ControlService.sendInputEvent(deviceID ?? "", full).catch(() => {});
}

if (controlBtn) {
  controlBtn.addEventListener("click", async () => {
    if (!deviceID) return;
    controlActive = !controlActive;
    controlBtn.classList.toggle("active", controlActive);
    controlBtn.textContent = controlActive ? "🖱 接管中" : "🖱 接管";
    stage.style.cursor = controlActive ? "none" : "crosshair";
    try {
      await ControlService.toggleRemoteControl(deviceID, controlActive);
      if (!controlActive) {
        // 释放学生端残留的按键状态
        void ControlService.sendInputEvent(deviceID, { type: 6, x: 0, y: 0, button: 0, wheel_delta: 0, key_code: 0, normalized: true });
      }
    } catch { /* gate command failed */ }
  });
}

// 指针事件 → 归一化坐标转发
stage.addEventListener("pointermove", (e) => {
  if (!controlActive) return;
  const r = stage.getBoundingClientRect();
  sendInput({ type: 1, x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height, normalized: true });
});
stage.addEventListener("pointerdown", (e) => {
  if (!controlActive) return;
  const r = stage.getBoundingClientRect();
  sendInput({ type: 2, x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height, button: e.button === 2 ? 2 : 1, normalized: true });
});
stage.addEventListener("pointerup", (e) => {
  if (!controlActive) return;
  const r = stage.getBoundingClientRect();
  sendInput({ type: 3, x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height, button: e.button === 2 ? 2 : 1, normalized: true });
});
stage.addEventListener("wheel", (e) => {
  if (!controlActive) return;
  sendInput({ type: 4, wheel_delta: Math.round(e.deltaY) });
}, { passive: true });
stage.addEventListener("keydown", (e) => {
  if (!controlActive) return;
  e.preventDefault();
  sendInput({ type: 5, key_code: e.keyCode });
});
stage.addEventListener("keyup", (e) => {
  if (!controlActive) return;
  sendInput({ type: 6, key_code: e.keyCode });
});
