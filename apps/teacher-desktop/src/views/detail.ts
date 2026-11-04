// 学生详情抽屉 — 单个学生的信息/上报记录/控制

import { store } from "../state";
import { clear, el, fmtTime, toast } from "../ui";
import { AuditService, ControlService } from "../mygo";
import { t } from "../i18n";

let currentDevice: string | null = null;

/** 打开学生详情（抽屉切换到详情面板）。 */
export function openStudentDetail(deviceID: string) {
  currentDevice = deviceID;
  // 注入详情 tab + 面板
  const tabs = document.getElementById("drawer-tabs")!;
  if (!document.getElementById("detail-tab")) {
    tabs.append(el("button", { class: "drawer-tab", id: "detail-tab", "data-tab": "detail" }, "详情"));
    tabs.addEventListener("click", () => {
      if (!document.querySelector('.drawer-tab[data-tab="detail"]')?.classList.contains("active")) {
        const tab = document.getElementById("detail-tab");
        if (tab) tab.textContent = "详情";
      }
    });
  }
  for (const t2 of document.querySelectorAll(".drawer-tab")) {
    t2.classList.toggle("active", (t2 as HTMLElement).dataset.tab === "detail");
  }
  for (const p of document.querySelectorAll(".drawer-panel")) {
    p.classList.remove("active");
  }

  const panels = document.getElementById("drawer-panels")!;
  let panel = document.getElementById("panel-detail");
  if (!panel) {
    panel = el("div", { class: "drawer-panel active", id: "panel-detail" });
    panels.append(panel);
  }
  panel.classList.add("active");
  renderDetail(panel, deviceID);
}

export function closeStudentDetail() {
  currentDevice = null;
  document.getElementById("detail-tab")?.remove();
  document.getElementById("panel-detail")?.remove();
}

async function renderDetail(panel: HTMLElement, deviceID: string) {
  const s = store.get();
  const device = s.devices.find((d) => d.device_id === deviceID);
  const seat = s.seating.flat().find((seat) => seat.device_id === deviceID);

  panel.replaceChildren(
    el("div", { style: "display:flex; align-items:center; gap:8px; margin-bottom:10px" },
      el("b", { style: "font-size:14px" }, deviceID),
      el("span", { class: "status-dot" }),
      el("span", { style: "margin-left:auto" }, el("button", {
        class: "btn-sm", onclick: () => closeStudentDetail(),
      }, "关闭")),
    ),
    el("div", { class: "kv-list" },
      el("div", { class: "kv" }, el("span", { class: "k" }, t("status.online")), el("span", { class: "v" }, device ? fmtTime(device.seen_at) : "—")),
      el("div", { class: "kv" }, el("span", { class: "k" }, "地址"), el("span", { class: "v" }, device?.endpoint ?? "—")),
      el("div", { class: "kv" }, el("span", { class: "k" }, "座位"),
        el("span", { class: "v" }, seat ? `${seat.row + 1}排${seat.col + 1}列` : "未安排")),
    ),
    el("div", { style: "display:flex; gap:6px; margin:12px 0" },
      el("button", { class: "btn-sm primary", onclick: () => document.dispatchEvent(new CustomEvent("monitor-device", { detail: deviceID })) }, "🔍 监控"),
      el("button", { class: "btn-sm", onclick: () => document.dispatchEvent(new CustomEvent("whisper", { detail: deviceID })) }, "💬 私信"),
      el("button", { class: "btn-sm danger", onclick: async () => {
          try {
            await ControlService.toggleRemoteControl(deviceID, true);
            toast("🖱", "远程接管已开启（30 分钟内有效）");
          } catch { toast("⚠️", "开启失败"); }
        } }, "🖱 接管"),
    ),
    el("div", { style: "font-size:11px; color:var(--faint); margin-bottom:6px" }, "上报记录"),
    el("div", { id: "detail-audit" }, el("div", { class: "event-empty" }, "加载中…")),
  );

  try {
    const events = await AuditService.recent(20, deviceID);
    const audit = document.getElementById("detail-audit");
    if (!audit) return;
    clear(audit);
    if (events.length === 0) {
      audit.append(el("div", { class: "event-empty" }, "暂无上报记录"));
      return;
    }
    for (const e of events) {
      audit.append(el("div", { class: "kv" },
        el("span", { class: "k" }, e.detail || e.kind),
        el("span", { class: "v" }, new Date(e.occurred_at).toLocaleTimeString("zh-CN")),
      ));
    }
  } catch {
    const audit = document.getElementById("detail-audit");
    if (audit) {
      clear(audit);
      audit.append(el("div", { class: "event-empty" }, "加载失败"));
    }
  }
}

export function detailDevice(): string | null {
  return currentDevice;
}

void toast;
