// 画面墙 — the hero view: live student screen mosaic.
//
// Cards are long-lived DOM nodes keyed by device id; a delta only swaps
// img sources for frames whose sequence advanced, so nothing flickers.

import type { DeviceInfo, ThumbnailInfo } from "../state";
import { store, thumbURL } from "../state";
import { el } from "../ui";

interface CardRefs {
  root: HTMLElement;
  img: HTMLImageElement;
  placeholder: HTMLElement;
  addrEl: HTMLElement;
  seq: number;
}

const cards = new Map<string, CardRefs>();
let emptyEl: HTMLElement | null = null;

/** Apply the current store state to the mosaic, diffing DOM in place. */
export function applyClassroom() {
  const grid = document.getElementById("mosaic")!;
  const { devices, thumbs, selected } = store.get();

  const wantEmpty = devices.length === 0;
  if (wantEmpty) {
    for (const [, card] of cards) card.root.remove();
    cards.clear();
    if (!emptyEl || !emptyEl.isConnected) {
      emptyEl = emptyState();
      grid.replaceChildren(emptyEl);
    }
    return;
  }
  if (emptyEl) {
    emptyEl.remove();
    emptyEl = null;
  }

  const wanted = new Set(devices.map((d) => d.device_id));
  for (const [id, card] of cards) {
    if (!wanted.has(id)) {
      card.root.remove();
      cards.delete(id);
    }
  }

  devices.forEach((device, i) => {
    let card = cards.get(device.device_id);
    if (!card) {
      card = createCard(device);
      cards.set(device.device_id, card);
    }
    if (card.addrEl.textContent !== device.endpoint) {
      card.addrEl.textContent = device.endpoint;
    }
    card.root.classList.toggle("selected", selected.has(device.device_id));
    // 保持 Go 端的排序（按设备 ID），appendChild 对已挂载节点是移动
    if (grid.children[i] !== card.root) {
      grid.insertBefore(card.root, grid.children[i] ?? null);
    }
    updateFrame(card, thumbs.get(device.device_id));
  });
}

function updateFrame(card: CardRefs, thumb: ThumbnailInfo | undefined) {
  if (!thumb || thumb.sequence === card.seq) return;
  const url = thumbURL(thumb);
  if (!url) return;
  card.img.src = url;
  card.seq = thumb.sequence;
  card.placeholder.style.display = "none";
  card.img.style.display = "block";
}

function createCard(device: DeviceInfo): CardRefs {
  const img = el("img", { alt: "机位实时画面", draggable: "false", style: "display:none" }) as HTMLImageElement;
  const placeholder = el("span", { class: "screen-placeholder" }, "等待画面…");
  const addrEl = el("span", { class: "dev-addr" }, device.endpoint);

  // 单击延迟选中（避免与双击冲突），双击打开大画面监控
  let clickTimer: number | null = null;
  const root = el(
    "div",
    {
      class: "dev-card",
      "data-device": device.device_id,
      onclick: () => {
        if (clickTimer !== null) return;
        clickTimer = window.setTimeout(() => {
          clickTimer = null;
          toggleSelect(device.device_id);
        }, 220);
      },
      ondblclick: () => {
        if (clickTimer !== null) {
          clearTimeout(clickTimer);
          clickTimer = null;
        }
        document.dispatchEvent(new CustomEvent("monitor-device", { detail: device.device_id }));
      },
    },
    el(
      "div",
      { class: "dev-screen" },
      placeholder,
      img,
      el(
        "div",
        { class: "dev-actions" },
        devAct("🔍", "实时监控（双击）", () => document.dispatchEvent(new CustomEvent("monitor-device", { detail: device.device_id }))),
        devAct("📣", "以此机位演示", () => document.dispatchEvent(new CustomEvent("demo-device", { detail: device.device_id }))),
        devAct("💬", "私信该学生", () => document.dispatchEvent(new CustomEvent("whisper", { detail: device.device_id }))),
        devAct("🪑", "安排座位", () => document.dispatchEvent(new CustomEvent("assign-seat", { detail: device.device_id }))),
      ),
    ),
    el(
      "div",
      { class: "dev-meta" },
      el("span", { class: "dev-name" }, device.device_id),
      addrEl,
      el("span", { class: "status-dot" }),
    ),
  );

  return { root, img, placeholder, addrEl, seq: -1 };
}

function devAct(icon: string, title: string, onClick: () => void): HTMLElement {
  return el("button", {
    class: "dev-act",
    title,
    onclick: (e) => {
      e.stopPropagation();
      onClick();
    },
  }, icon);
}

function toggleSelect(deviceID: string) {
  const selected = new Set(store.get().selected);
  if (selected.has(deviceID)) selected.delete(deviceID);
  else selected.add(deviceID);
  store.set({ selected });
  applyClassroom();
}

function emptyState(): HTMLElement {
  return el(
    "div",
    { id: "mosaic-empty" },
    el("div", { class: "orb" }, "🖥️"),
    el("h2", {}, "教室已就绪，等待学生入场"),
    el(
      "p",
      {},
      "学生机启动 Agent 后将通过 UDP 广播自动上线，",
      el("br"),
      "实时画面会出现在这面墙上。双击机位可打开大画面监控。",
    ),
    el("p", { style: "margin-top:14px" }, el("code", {}, "go run ./apps/agent")),
  );
}
