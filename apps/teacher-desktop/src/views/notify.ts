// 通知中心 + 命令面板

import { store } from "../state";
import { clear, el, fmtTime } from "../ui";
import { fuzzyMatch } from "../logic";
import { t } from "../i18n";
import { GroupService } from "../mygo";

/* ---------- 通知中心（状态栏 🔔） ---------- */

let popOpen = false;

export function toggleNotifyCenter() {
  const existing = document.getElementById("notify-pop");
  if (existing) {
    existing.remove();
    popOpen = false;
    return;
  }
  popOpen = true;
  store.set({ unreadNotifications: 0 });

  const pop = el(
    "div",
    { class: "notify-pop", id: "notify-pop" },
    el("div", { style: "display:flex; align-items:center; margin-bottom:6px" },
      el("b", { style: "flex:1; font-size:12.5px" }, "通知中心"),
      el("button", { class: "btn-sm", onclick: () => { store.set({ notifications: [] }); renderList(); } }, "清空"),
    ),
  );
  const list = el("div");
  pop.append(list);
  const renderList = () => {
    clear(list);
    const { notifications } = store.get();
    if (notifications.length === 0) {
      list.append(el("div", { class: "event-empty" }, "暂无通知"));
      return;
    }
    for (const n of notifications) {
      list.append(el("div", { class: `notify-item ${n.level}` },
        el("span", {}, n.icon),
        el("span", { class: "n-text", style: "font-size:12px" }, n.text),
        el("span", { class: "n-time" }, fmtTime(n.at)),
      ));
    }
  };
  renderList();
  document.body.append(pop);

  const dismiss = (e: MouseEvent) => {
    const target = e.target as HTMLElement;
    if (!pop.contains(target) && target.id !== "notify-bell") {
      pop.remove();
      document.removeEventListener("mousedown", dismiss);
      popOpen = false;
    }
  };
  setTimeout(() => document.addEventListener("mousedown", dismiss));
}

export function isNotifyOpen(): boolean {
  return popOpen;
}

/* ---------- 命令面板（Ctrl+K） ---------- */

export interface PaletteEntry {
  icon: string;
  label: string;
  keywords?: string;
  run: () => void;
}

export function openPalette(entries: PaletteEntry[]) {
  closeModalSafely();
  const backdrop = el("div", { class: "palette-backdrop" });
  const input = el("input", { placeholder: "搜索命令或学生机… (Ctrl+K)" }) as HTMLInputElement;
  const list = el("div", { class: "palette-list" });
  const box = el("div", { class: "palette" }, input, list);
  backdrop.append(box);
  document.getElementById("palette-root")!.append(backdrop);
  requestAnimationFrame(() => backdrop.classList.add("open"));

  let hot = 0;
  let current: PaletteEntry[] = [];

  const render = () => {
    const q = input.value.trim();
    current = entries.filter((e) => fuzzyMatch(q, e.label + " " + (e.keywords ?? ""))).slice(0, 10);
    clear(list);
    if (current.length === 0) {
      list.append(el("div", { class: "event-empty" }, "无匹配结果"));
      return;
    }
    hot = Math.min(hot, current.length - 1);
    current.forEach((entry, i) => {
      list.append(el("button", {
        class: `palette-item${i === hot ? " hot" : ""}`,
        onclick: () => {
          close();
          entry.run();
        },
      }, el("span", { class: "p-ico" }, entry.icon), entry.label));
    });
  };

  const close = () => {
    backdrop.classList.remove("open");
    setTimeout(() => backdrop.remove(), 160);
    document.removeEventListener("keydown", keyNav);
  };

  const keyNav = (e: KeyboardEvent) => {
    if (e.key === "Escape") {
      close();
      return;
    }
    if (e.key === "ArrowDown") {
      hot = Math.min(hot + 1, current.length - 1);
      render();
      e.preventDefault();
    } else if (e.key === "ArrowUp") {
      hot = Math.max(hot - 1, 0);
      render();
      e.preventDefault();
    } else if (e.key === "Enter") {
      const entry = current[hot];
      if (entry) {
        close();
        entry.run();
      }
    }
  };
  input.addEventListener("input", render);
  document.addEventListener("keydown", keyNav);
  backdrop.addEventListener("mousedown", (e) => {
    if (e.target === backdrop) close();
  });

  render();
  input.focus();
}

function closeModalSafely() {
  document.querySelector<HTMLButtonElement>(".modal-close")?.click();
}

/** 全量条目：命令 + 学生机操作。 */
export function buildPaletteEntries(
  commandEntries: PaletteEntry[],
  devices: { device_id: string }[],
): PaletteEntry[] {
  const deviceEntries: PaletteEntry[] = [];
  for (const d of devices) {
    deviceEntries.push({ icon: "🔍", label: `监控 ${d.device_id}`, keywords: "monitor", run: () => document.dispatchEvent(new CustomEvent("monitor-device", { detail: d.device_id })) });
    deviceEntries.push({ icon: "📣", label: `演示 ${d.device_id}`, keywords: "demo", run: () => document.dispatchEvent(new CustomEvent("demo-device", { detail: d.device_id })) });
    deviceEntries.push({ icon: "🔒", label: `锁定 ${d.device_id}`, keywords: "lock", run: () => void import("../mygo").then((m) => m.ControlService.lockStudents(d.device_id, "请专心听讲", false)) });
  }
  return [...commandEntries, ...deviceEntries];
}

// i18n 引用占位：保持骨架被引用
void t;
void (async () => { await GroupService; })();
