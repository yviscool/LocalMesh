// 画面墙 2.0 — 排序/筛选/分组色环/三档缩放/右键菜单/选中操作条/拖框多选
//
// Cards are long-lived DOM nodes keyed by device id; a delta only swaps
// img sources for frames whose sequence advanced, so nothing flickers.

import type { DeviceInfo, ThumbnailInfo } from "../state";
import { store, thumbURL } from "../state";
import { clear, el, showMenuAt, toast } from "../ui";
import { filterByGroup, groupColor, groupOfDevice, sortDevices } from "../logic";
import { ControlService, GroupService } from "../mygo";

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
  const s = store.get();

  // 尺寸档位
  grid.classList.remove("mosaic-S", "mosaic-L");
  if (s.cardSize === "S") grid.classList.add("mosaic-S");
  if (s.cardSize === "L") grid.classList.add("mosaic-L");

  // 排序 + 分组筛选
  const visible = sortDevices(
    filterByGroup(s.devices, s.filterGroup, (gid) => s.groups.find((g) => g.group_id === gid)?.members ?? []),
    s.sortMode,
    (id) => groupOfDevice(id, s.groups),
  );

  const wantEmpty = visible.length === 0;
  if (wantEmpty) {
    for (const [, card] of cards) card.root.remove();
    cards.clear();
    if (!emptyEl || !emptyEl.isConnected) {
      emptyEl = emptyState();
      grid.replaceChildren(emptyEl);
    }
    renderToolbar();
    renderSelectionBar(visible);
    return;
  }
  if (emptyEl) {
    emptyEl.remove();
    emptyEl = null;
  }

  const wanted = new Set(visible.map((d) => d.device_id));
  for (const [id, card] of cards) {
    if (!wanted.has(id)) {
      card.root.remove();
      cards.delete(id);
    }
  }

  visible.forEach((device, i) => {
    let card = cards.get(device.device_id);
    if (!card) {
      card = createCard(device);
      cards.set(device.device_id, card);
    }
    if (card.addrEl.textContent !== device.endpoint) {
      card.addrEl.textContent = device.endpoint;
    }
    card.root.classList.toggle("selected", s.selected.has(device.device_id));
    // 分组色环
    const gid = groupOfDevice(device.device_id, s.groups);
    const gname = s.groups.find((g) => g.group_id === gid)?.name;
    let ring = card.root.querySelector<HTMLElement>(".group-ring");
    if (gname) {
      if (!ring) {
        ring = el("div", { class: "group-ring" });
        card.root.prepend(ring);
      }
      ring.style.background = groupColor(gname);
    } else if (ring) {
      ring.remove();
    }
    if (grid.children[i] !== card.root) {
      grid.insertBefore(card.root, grid.children[i] ?? null);
    }
    updateFrame(card, s.thumbs.get(device.device_id));
  });

  renderSelectionBar(visible);
  renderToolbar();
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

  // 单击延迟选中（避免与双击冲突），双击打开大画面监控，右键快捷菜单
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
      oncontextmenu: (e) => {
        e.preventDefault();
        const me = e as MouseEvent;
        showCardMenu(me.clientX, me.clientY, device.device_id);
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
        devAct("ℹ️", "学生详情", () => document.dispatchEvent(new CustomEvent("student-detail", { detail: device.device_id }))),
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

/** 卡片右键菜单。 */
function showCardMenu(x: number, y: number, id: string) {
  showMenuAt(x, y, [
    { icon: "🔍", label: "实时监控", run: () => document.dispatchEvent(new CustomEvent("monitor-device", { detail: id })) },
    { icon: "📣", label: "以此机位演示", run: () => document.dispatchEvent(new CustomEvent("demo-device", { detail: id })) },
    { icon: "💬", label: "私信", run: () => document.dispatchEvent(new CustomEvent("whisper", { detail: id })) },
    { separator: true, label: "", run: () => {} },
    { icon: "🪑", label: "安排座位", run: () => document.dispatchEvent(new CustomEvent("assign-seat", { detail: id })) },
    { icon: "ℹ️", label: "学生详情", run: () => document.dispatchEvent(new CustomEvent("student-detail", { detail: id })) },
    { separator: true, label: "", run: () => {} },
    { icon: "🔒", label: "锁定该机", run: () => ControlService.lockStudents(id, "请专心听讲", false).catch(() => {}) },
  ]);
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
}

/** 选中操作条：选中 ≥1 台时浮现于墙底部。 */
function renderSelectionBar(visible: DeviceInfo[]) {
  const grid = document.getElementById("mosaic")!;
  const stage = grid.parentElement as HTMLElement;
  let bar = document.getElementById("selection-bar");
  const selected = [...store.get().selected].filter((id) => visible.some((d) => d.device_id === id));
  if (selected.length === 0) {
    bar?.remove();
    return;
  }
  if (!bar || bar.parentElement !== stage) {
    bar?.remove();
    bar = el("div", { id: "selection-bar" });
    stage.append(bar);
  }
  bar.replaceChildren(
    el("span", { class: "sel-count" }, `已选 ${selected.length}`),
    el("span", { class: "sep" }),
    el("button", { class: "btn-sm", onclick: () => ControlService.lockStudents("课堂管控", "请专心听讲", false).catch(() => {}) }, "🔒 锁定"),
    el("button", { class: "btn-sm", onclick: () => ControlService.unlockStudents().catch(() => {}) }, "🔓 解锁"),
    el("button", { class: "btn-sm", onclick: () => document.dispatchEvent(new CustomEvent("whisper", { detail: selected[0] })) }, "💬 私信"),
    el("button", { class: "btn-sm", onclick: () => void addToGroup(selected) }, "👥 加组"),
    el("button", { class: "btn-sm", title: "下发脚本到选中设备", onclick: () => {
      void ControlService.runScriptOnStudents({ script_type: "powershell", content: "hostname", timeout_seconds: 10 }, selected);
      toast("💻", "脚本已下发选中设备");
    } }, "💻 脚本"),
    el("span", { class: "sep" }),
    el("button", { class: "btn-sm", onclick: () => store.set({ selected: new Set() }) }, "取消"),
  );
}

async function addToGroup(selected: string[]) {
  const groups = store.get().groups;
  if (groups.length === 0) {
    toast("💡", "请先创建分组");
    return;
  }
  const group = groups[0];
  if (!group) return;
  for (const id of selected) await GroupService.addMember(group.group_id, id);
  window.dispatchEvent(new CustomEvent("groups-changed"));
  toast("👥", `已加入 ${group.name}`);
}

/** 墙顶部工具栏：分组筛选 chips + 排序 + 尺寸档位。 */
function renderToolbar() {
  const grid = document.getElementById("mosaic")!;
  const stage = grid.parentElement as HTMLElement;
  let toolbar = document.getElementById("mosaic-toolbar");
  if (!toolbar || toolbar.parentElement !== stage) {
    toolbar?.remove();
    toolbar = el("div", { class: "mosaic-toolbar", id: "mosaic-toolbar" });
    stage.prepend(toolbar);
  }
  const s = store.get();
  const key = JSON.stringify([s.groups, s.filterGroup, s.sortMode, s.cardSize]);
  if (toolbar.dataset.key === key) return;
  toolbar.dataset.key = key;
  clear(toolbar);

  toolbar.append(el("button", {
    class: "chip" + (s.filterGroup === null ? " active" : ""),
    onclick: () => { store.set({ filterGroup: null }); applyClassroom(); },
  }, "全部"));
  for (const g of s.groups) {
    toolbar.append(el("button", {
      class: "chip" + (s.filterGroup === g.group_id ? " active" : ""),
      onclick: () => { store.set({ filterGroup: g.group_id }); applyClassroom(); },
    }, el("span", { class: "dot", style: `background:${groupColor(g.name)}` }), g.name));
  }
  toolbar.append(el("span", { class: "spacer" }));
  const sortSel = el("select", { class: "mini-select", onchange: (e) => {
    const v = (e.currentTarget as HTMLSelectElement).value as "name" | "online" | "group";
    store.set({ sortMode: v });
    localStorage.setItem("lm.sort", v);
    applyClassroom();
  } },
    el("option", { value: "name" }, "按名称"),
    el("option", { value: "online" }, "按上线"),
    el("option", { value: "group" }, "按分组")) as HTMLSelectElement;
  sortSel.value = s.sortMode;
  toolbar.append(sortSel);
  for (const size of ["S", "M", "L"] as const) {
    toolbar.append(el("button", {
      class: "chip" + (s.cardSize === size ? " active" : ""),
      onclick: () => { store.set({ cardSize: size }); localStorage.setItem("lm.card", size); applyClassroom(); },
    }, size));
  }
}

/** 拖框多选：空白处按下拖拽 → 橡皮筋选框。 */
export function installRubberBand() {
  const stage = document.getElementById("stage")!;
  let startX = 0, startY = 0, active = false;
  let band: HTMLElement | null = null;

  stage.addEventListener("pointerdown", (e) => {
    if (e.button !== 0) return;
    const target = e.target as HTMLElement;
    if (target.closest(".dev-card, #selection-bar, .mosaic-toolbar, button, select, input")) return;
    if (!document.getElementById("mosaic")?.classList.contains("active")) return;

    active = true;
    startX = e.clientX;
    startY = e.clientY;
    band = el("div", { id: "rubber-band" });
    document.body.append(band);
    position(e);
  });
  stage.addEventListener("pointermove", (e) => {
    if (!active) return;
    position(e);
  });
  stage.addEventListener("pointerup", (e) => {
    if (!active) return;
    active = false;
    band?.remove();
    band = null;

    const x1 = Math.min(startX, e.clientX), x2 = Math.max(startX, e.clientX);
    const y1 = Math.min(startY, e.clientY), y2 = Math.max(startY, e.clientY);
    if (x2 - x1 < 8 || y2 - y1 < 8) return; // 视为点击空白：清除选择

    const selected = new Set<string>();
    for (const [, card] of cards) {
      const r = card.root.getBoundingClientRect();
      if (r.right > x1 && r.left < x2 && r.bottom > y1 && r.top < y2) {
        selected.add(card.root.dataset.device ?? "");
      }
    }
    selected.delete("");
    store.set({ selected });
  });

  function position(e: PointerEvent) {
    if (!band) return;
    band.style.left = `${Math.min(startX, e.clientX)}px`;
    band.style.top = `${Math.min(startY, e.clientY)}px`;
    band.style.width = `${Math.abs(e.clientX - startX)}px`;
    band.style.height = `${Math.abs(e.clientY - startY)}px`;
  }
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
