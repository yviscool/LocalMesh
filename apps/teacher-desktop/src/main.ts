// LocalMesh 课堂 — 启动装配：命令栏、事件订阅、视图渲染

import { store, normalizeGrid, type ActivityItem } from "./state";
import { clear, el, showMenuAt, type MenuItemDef, toast } from "./ui";
import { fuzzyMatch, groupColor, groupOfDevice, sortDevices } from "./logic";
import { t } from "./i18n";
import { GroupService } from "./mygo";
import {
  BroadcastService,
  ClassroomService,
  ControlService,
  InteractionService,
  MonitorService,
  SceneService,
  TeachingService,
  events,
} from "./mygo";
import { applyClassroom } from "./views/mosaic";
import { refreshSeating, renderSeating } from "./views/seating";
import { installRubberBand } from "./views/mosaic";
import { openStudentDetail } from "./views/detail";
import { buildPaletteEntries, openPalette } from "./views/notify";
import { playCue, setSoundEnabled, soundEnabled } from "./sound";
import { refreshChatTargets, renderActivity, renderChat, renderHands, sendChat } from "./views/drawer";
import { renderStatusbar, startClock } from "./views/statusbar";
import {
  openBroadcastModal,
  openGroupModal,
  openChannelModal,
  openFileCollectModal,
  openFileDistributeModal,
  openFirewallModal,
  openLockModal,
  openPowerModal,
  quickUnlock,
  openSettingsModal,
  openGroupCastModal,
  openSystemPolicyModal,
} from "./views/modals";
import {
  openAttendanceModal,
  openBuzzerModal,
  openInventoryModal,
  openQuizModal,
  openScriptModal,
  openWhiteboardModal,
} from "./views/teaching-modals";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

const nonNull = <T>(v: T | null | undefined): v is T => v != null;

/* ---------- 命令栏 ---------- */

type CmdDef = { icon: string; label: string; run: () => void; toggle?: () => boolean; key?: string };

type MenuGroup = { icon: string; label: string; items: (CmdDef | "sep")[] };

const quickCommands: (CmdDef | "sep")[] = [
  { icon: "▶", label: "上课", run: () => runSceneStart() },
  { icon: "⏹", label: "下课", run: () => runSceneStop() },
  "sep",
  { icon: "📣", label: "屏幕广播", run: () => openBroadcastModal() },
  {
    icon: "🔒",
    label: "黑屏肃静",
    run: () => openLockModal(),
  },
];

const menuGroups: MenuGroup[] = [
  { icon: "🎓", label: "课堂", items: [
    { icon: "🎙️", label: "伴音同传", run: () => toggleAudio(), toggle: () => store.get().audio.streaming },
    { icon: "👥", label: "分组教学", run: () => openGroupModal() },
    { icon: "📡", label: "分组广播", run: () => openGroupCastModal() },
    { icon: "📄", label: "文件分发", run: () => openFileDistributeModal() },
    { icon: "📥", label: "文件收集", run: () => openFileCollectModal() },
  ]},
  { icon: "🛡️", label: "管控", items: [
    { icon: "🔓", label: "一键解锁", run: () => quickUnlock() },
    { icon: "🛡️", label: "考试模式", run: () => openFirewallModal() },
    { icon: "🚫", label: "系统限制", run: () => openSystemPolicyModal() },
    { icon: "📡", label: "机房频道", run: () => openChannelModal() },
  ]},
  { icon: "📚", label: "教学", items: [
    { icon: "🎨", label: "电子画板", run: () => openWhiteboardModal() },
    { icon: "📝", label: "随堂测验", run: () => openQuizModal() },
    { icon: "⏱️", label: "抢答竞赛", run: () => openBuzzerModal() },
    { icon: "🙋", label: "点名考勤", run: () => openAttendanceModal() },
  ]},
  { icon: "🧰", label: "工具", items: [
    { icon: "💻", label: "脚本终端", run: () => openScriptModal() },
    { icon: "📋", label: "资产盘点", run: () => openInventoryModal() },
    { icon: "⚙️", label: "设置", run: () => openSettingsModal() },
  ]},
];

const allCommands: CmdDef[] = [
  ...quickCommands.filter((c): c is CmdDef => c !== "sep"),
  ...menuGroups.flatMap((g) => g.items.filter((c): c is CmdDef => c !== "sep")),
  { icon: "🖥️", label: "伴音同传", run: () => toggleAudio(), toggle: () => store.get().audio.streaming },
];

async function toggleAudio() {
  try {
    const next = store.get().audio.streaming ? await BroadcastService.stopAudio() : await BroadcastService.startAudio();
    store.set({ audio: next });
    store.log(next.streaming ? "🎙️" : "🔇", next.streaming ? "伴音同传已开启" : "伴音同传已停止");
    toast(next.streaming ? "🎙️" : "🔇", next.streaming ? "同传开启" : "同传停止");
  } catch (err) {
    toast("⚠️", String(err));
  }
}

async function runSceneStart() {
  try {
    await SceneService.startClass({ lock: false, net_block: false, broadcast: true, audio: true, offline_guard: true });
    store.log("▶", "上课场景已启动（广播+伴音+离线守护）");
    toast("▶", "上课！广播已开启");
  } catch (err) { toast("⚠️", String(err)); }
}

async function runSceneStop() {
  try {
    await SceneService.stopClass();
    store.log("⏹", "下课场景已还原");
    toast("⏹", "下课，全部还原");
  } catch (err) { toast("⚠️", String(err)); }
}

function renderCommandbar() {
  const bar = $("#commandbar");
  clear(bar);
  for (const cmd of quickCommands) {
    if (cmd === "sep") {
      bar.append(el("span", { class: "cmd-sep" }));
      continue;
    }
    const btn = el("button", { class: "cmd", title: cmd.label, onclick: () => cmd.run() },
      el("span", { class: "ico" }, cmd.icon),
      el("span", { class: "cmd-label" }, cmd.label));
    btn.dataset.cmd = cmd.label;
    bar.append(btn);
  }
  for (const group of menuGroups) {
    const btn = el("button", { class: "cmd", title: group.label, onclick: (e) => openDropdown(e as MouseEvent, group) },
      el("span", { class: "ico" }, group.icon),
      el("span", {}, group.label),
      el("span", { class: "ctx-key" }, "▾"));
    bar.append(btn);
  }
}

function openDropdown(e: MouseEvent, group: MenuGroup) {
  const btn = e.currentTarget as HTMLElement;
  const rect = btn.getBoundingClientRect();
  const items: MenuItemDef[] = group.items.map((item) => {
    if (item === "sep") return { separator: true, label: "", run: () => {} };
    return { icon: item.icon, label: item.label, run: item.run, danger: false };
  });
  showMenuAt(rect.left, rect.bottom + 6, items);
}

function refreshCommandStates() {
  const bar = $("#commandbar");
  for (const btn of bar.querySelectorAll<HTMLButtonElement>(".cmd")) {
    const label = btn.dataset.cmd;
    const def = [...quickCommands, ...menuGroups.flatMap((g) => g.items)].filter(
      (c): c is CmdDef => c !== "sep" && c.label === label,
    )[0];
    if (def?.toggle) btn.classList.toggle("toggle-on", def.toggle());
  }
}

/* ---------- 视图切换 ---------- */

function switchView(view: "mosaic" | "seating") {
  store.set({ view });
  for (const tab of document.querySelectorAll(".view-tab")) {
    tab.classList.toggle("active", (tab as HTMLElement).dataset.view === view);
  }
  for (const section of document.querySelectorAll(".view")) {
    section.classList.toggle("active", section.id === view);
  }
  if (view === "seating") refreshSeating();
}

/* ---------- 抽屉 Tab 切换 ---------- */

function switchDrawerTab(tab: string) {
  for (const t of document.querySelectorAll(".drawer-tab")) {
    t.classList.toggle("active", (t as HTMLElement).dataset.tab === tab);
  }
  for (const p of document.querySelectorAll(".drawer-panel")) {
    p.classList.toggle("active", p.id === `panel-${tab}`);
  }
}

/* ---------- 事件订阅 ---------- */

events.classroomDelta.on((delta) => {
  const s = store.get();
  const thumbs = new Map(s.thumbs);
  let devices = s.devices;
  let selected = s.selected;

  if (delta.full) {
    devices = delta.devices ?? [];
    thumbs.clear();
    (delta.thumbs ?? []).forEach((t) => thumbs.set(t.device_id, t));
  } else {
    if (delta.added?.length) {
      const known = new Set(devices.map((d) => d.device_id));
      const fresh = delta.added.filter((d) => !known.has(d.device_id));
      devices = [...devices, ...fresh];
      fresh.forEach((d) => {
        store.log("🟢", `${d.device_id} 上线`);
        store.notify("🟢", `${d.device_id} 上线`);
      });
    }
    if (delta.removed?.length) {
      const gone = new Set(delta.removed);
      devices = devices.filter((d) => !gone.has(d.device_id));
      delta.removed.forEach((id) => {
        thumbs.delete(id);
        store.log("🔴", `${id} 离线`);
        store.notify("🔴", `${id} 离线`, "danger");
        playCue("offline");
      });
      selected = new Set(selected);
      delta.removed.forEach((id) => selected.delete(id));
    }
    (delta.updates ?? []).forEach((t) => thumbs.set(t.device_id, t));
  }

  store.set({ devices, thumbs, selected });
  applyClassroom(); // 增量 DOM 更新，无闪烁
});

events.chatMessage.on((msg) => {
  if (msg.sender_role === "teacher") return; // 自己的消息走历史回流
  const chat = [...store.get().chat, msg].slice(-200);
  store.set({ chat });
  store.log("💬", `${msg.sender_name}: ${msg.content}`);
  renderChat();
});

events.handsChanged.on((incoming) => {
  const hands = incoming.filter((h): h is NonNullable<(typeof incoming)[number]> => h != null);
  const prev = store.get().hands.length;
  store.set({ hands, unreadHands: hands.length > prev ? hands.length : store.get().unreadHands });
  store.log("✋", `${hands[hands.length - 1]?.student_name || "有学生"} 举手`);
  renderHands();
  const badge = $("#hand-badge");
  badge.textContent = String(hands.length);
  badge.classList.toggle("hidden", hands.length === 0);
  badge.classList.toggle("pulse", hands.length > 0);
  if (hands.length > prev) {
    const name = hands[hands.length - 1]?.student_name || "有学生";
    toast("✋", `${name} 举手了`);
    store.notify("✋", `${name} 举手了`, "warn");
    playCue("hand");
  }
});
events.broadcastMode.on((status) => {
  store.set({ broadcast: status });
  renderStatusbar();
});

// 托盘/任意来源的锁定状态变化即时同步到状态栏
events.lockStatus.on((lock) => {
  store.set({ lock: { state: lock.state, is_trapping: lock.is_trapping } });
});

/* ---------- 卡片快捷动作（自定义事件桥） ---------- */

document.addEventListener("demo-device", async (e) => {
  const deviceID = (e as CustomEvent<string>).detail;
  try {
    const next = await BroadcastService.startDemo(deviceID);
    store.set({ broadcast: next });
    store.log("📣", `学生演示：${deviceID}`);
    toast("📣", `${deviceID} 的画面正在广播`);
  } catch (err) {
    toast("⚠️", String(err));
  }
});

document.addEventListener("monitor-device", (e) => {
  const deviceID = (e as CustomEvent<string>).detail;
  MonitorService.openWindow(deviceID).then((state) => {
    if (state === "focused") toast("🔍", `${deviceID} 的监控窗口已在最前`);
  }).catch((err) => {
    toast("⚠️", String(err));
  });
});

document.addEventListener("whisper", () => {
  switchDrawerTab("chat");
  ($("#chat-input") as HTMLInputElement).focus();
});

document.addEventListener("assign-seat", () => switchView("seating"));

/* ---------- 静态交互绑定 ---------- */

function wireStaticUI() {
  // 视图 Tab
  for (const tab of document.querySelectorAll<HTMLButtonElement>(".view-tab")) {
    tab.addEventListener("click", () => switchView(tab.dataset.view as "mosaic" | "seating"));
  }
  // 抽屉 Tab
  for (const tab of document.querySelectorAll<HTMLButtonElement>(".drawer-tab")) {
    tab.addEventListener("click", () => {
      switchDrawerTab(tab.dataset.tab!);
      if (tab.dataset.tab === "hands") store.set({ unreadHands: 0 });
    });
  }

  // 聊天表单
  $("#chat-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const input = $("#chat-input") as HTMLInputElement;
    const target = ($("#chat-target") as HTMLSelectElement).value;
    const text = input.value.trim();
    if (!text) return;
    try {
      await sendChat(text, target);
      input.value = "";
    } catch (err) {
      toast("⚠️", String(err));
    }
  });

  // Esc：先关弹层
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape") return;
    if (document.querySelector(".modal-backdrop")) {
      document.querySelector<HTMLButtonElement>(".modal-close")?.click();
    }
  });

  // Ctrl+K 命令面板
  document.addEventListener("keydown", (e) => {
    if (e.ctrlKey && (e.key === "k" || e.key === "K")) {
      e.preventDefault();
      openPalette(buildPaletteEntries(
        allCommands.map((c) => ({ icon: c.icon, label: c.label, run: c.run })),
        store.get().devices,
      ));
    }
  });
}

/* ---------- 启动 ---------- */

async function boot() {
  wireStaticUI();
  renderCommandbar();
  startClock();

  // 初始状态拉取
  try {
    const [devices, thumbs, channel, broadcast, audio, lock, chat, hands, seating] = await Promise.all([
      ClassroomService.devices(),
      ClassroomService.thumbnails(),
      ClassroomService.channel(),
      BroadcastService.status(),
      BroadcastService.audioStatus(),
      ControlService.lockState(),
      InteractionService.chatHistory(),
      InteractionService.hands(),
      InteractionService.seatingGrid(),
    ]);

    const thumbMap = new Map(thumbs.map((t) => [t.device_id, t]));
    const activity: ActivityItem[] = devices.map((d) => ({
      icon: "🟢",
      text: `${d.device_id} 已在线`,
      at: new Date(d.seen_at),
    }));

    store.set({
      devices,
      thumbs: thumbMap,
      channel,
      broadcast,
      audio,
      lock: { state: lock.state, is_trapping: lock.is_trapping },
      chat: chat.filter(nonNull),
      hands: hands.filter(nonNull),
      seating: seating.flat().length ? normalizeGrid(seating) : [],
      activity,
      unreadHands: hands.length,
    });

    const badge = $("#hand-badge");
    badge.textContent = String(hands.length);
    badge.classList.toggle("hidden", hands.length === 0);
  } catch (err) {
    toast("⚠️", `初始化失败：${String(err)}`);
  }

  // 首次运行引导（localStorage 一次性）
  void (async () => {
    const { maybeShowWizard } = await import("./views/modals");
    void maybeShowWizard();
  })();

  refreshChatTargets();
  applyClassroom();
  renderChat();
  renderHands();
  renderActivity();
  renderStatusbar();
  refreshCommandStates();

  // 全局状态订阅：任何状态变化都同步时间线、状态栏与命令栏按钮态
  store.on(() => {
    renderActivity();
    renderStatusbar();
    refreshCommandStates();
  });

  // 分组载入 + 变更事件
  void refreshGroups();
  window.addEventListener("groups-changed", () => void refreshGroups());

  // 拖框多选
  installRubberBand();

  // 抽屉折叠（记忆 + 窄屏自动折叠）
  const drawer = document.getElementById("drawer")!;
  const toggle = el("button", { id: "drawer-toggle", title: "收起/展开侧栏", onclick: () => {
    const collapsed = document.body.classList.toggle("drawer-collapsed");
    localStorage.setItem("lm.drawer", collapsed ? "1" : "0");
  } }, "»");
  drawer.append(toggle);
  if (localStorage.getItem("lm.drawer") === "1" || window.innerWidth <= 1280) {
    document.body.classList.add("drawer-collapsed");
  }

  // 主题
  document.body.dataset.theme = localStorage.getItem("lm.theme") || "dark";

  // 声音开关（设置页可改）
  setSoundEnabled(localStorage.getItem("lm.sound") !== "off");

  // 测验提交流（实时）
  events.quizSubmission.on((sub) => {
    store.log("📝", `${sub.student_name || sub.device_id} 已交卷 (${sub.score}/${sub.max_score})`);
    store.notify("📝", `${sub.student_name || sub.device_id} 已交卷`, "info");
    const s = store.get().quizLive;
    if (s) store.set({ quizLive: { ...s, submissions: s.submissions + 1 } });
  });

  // 拦截/执行类上报 → 通知中心 + 声音
  events.agentAudit.on((e) => {
    if (String(e.Kind).startsWith("process.blocked")) {
      store.notify("🚫", `${e.DeviceID}: ${e.Detail}`, "danger");
      playCue("alert");
    }
  });

  // 状态微轮询：伴音/锁定/测验常驻条
  setInterval(async () => {
    try {
      const [audio, lock, quiz] = await Promise.all([
        BroadcastService.audioStatus(),
        ControlService.lockState(),
        TeachingService.activeQuiz(),
      ]);
      store.set({
        audio,
        lock: { state: lock.state, is_trapping: lock.is_trapping },
        quizLive: quiz.quiz ? { active: quiz.active, time_remain: quiz.time_remain, submissions: quiz.submissions } : null,
      });
    } catch { /* ignore */ }
  }, 5000);
  setInterval(refreshCommandStates, 1000);

  // 全局快捷键：Ctrl+Shift+L 一键解锁
  document.addEventListener("keydown", (e) => {
    if (e.ctrlKey && e.shiftKey && (e.key === "L" || e.key === "l")) {
      e.preventDefault();
      quickUnlock();
    }
  });

  void renderSeating; // 视图切换时按需渲染
}

async function refreshGroups() {
  try {
    const groups = await GroupService.listGroups();
    store.set({ groups, filterGroup: store.get().filterGroup });
    applyClassroom();
  } catch { /* ignore */ }
}

boot();
