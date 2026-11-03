// 状态栏 — 教室全局状态一眼尽览 + 时钟

import type { AppState } from "../state";
import { store } from "../state";
import { el } from "../ui";

export function renderStatusbar() {
  const s: AppState = store.get();
  const left = document.getElementById("status-left")!;
  left.replaceChildren(
    st(s.devices.length > 0, "🖥️", "在线", `${s.devices.length} 台`),
    st(true, "📡", "频道", `${s.channel.channel_id}`),
    broadcastStatus(s),
    audioStatus(s),
    lockStatus(s),
  );
}

function st(ok: boolean, icon: string, label: string, value: string): HTMLElement {
  return el(
    "span",
    { class: "st" },
    el("span", {}, icon),
    el("span", {}, label),
    el("b", {}, value),
  );
}

function broadcastStatus(s: AppState): HTMLElement {
  const active = s.broadcast.active;
  const modeText =
    s.broadcast.mode === "teacher" ? "教师广播中" : s.broadcast.mode === "student_demo" ? `演示 ${s.broadcast.active_sender}` : "未广播";
  const dot = el("span", { class: `st-dot${active ? "" : " off"}` });
  const wrap = el(
    "span",
    { class: "st" },
    dot,
    el("span", {}, "广播"),
    el("b", {}, active ? modeText : "关闭"),
  );
  return wrap;
}

function audioStatus(s: AppState): HTMLElement {
  return el(
    "span",
    { class: "st" },
    el("span", { class: `st-dot${s.audio.streaming ? "" : " off"}` }),
    el("span", {}, "同传"),
    el("b", {}, s.audio.streaming ? "开启" : "关闭"),
  );
}

function lockStatus(s: AppState): HTMLElement {
  const locked = s.lock.state !== "unlocked";
  return el(
    "span",
    { class: "st" },
    el("span", { class: `st-dot${locked ? " danger" : ""}` }),
    el("span", {}, "锁定"),
    el("b", {}, locked ? (s.lock.state === "input_only" ? "仅锁键鼠" : "黑屏") : "未锁定"),
  );
}

export function startClock() {
  const right = document.getElementById("status-right")!;
  const tick = () => {
    right.replaceChildren(el("span", {}, new Date().toLocaleTimeString("zh-CN")));
  };
  tick();
  setInterval(tick, 1000);
}
