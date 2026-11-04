// 命令类弹层：广播 / 黑屏肃静 / 电源 / 考试模式 / 频道

import { store } from "../state";
import { clear, el, field, onModalClose, openModal, optionCards, toast } from "../ui";
import { AuditService, BroadcastService, ClassroomService, ConfigService, ControlService, GroupCastService, GroupService, events } from "../mygo";
import { errMsg } from "./seating";
import { groupColor } from "../logic";

/* ---------- 屏幕广播 ---------- */

export async function openBroadcastModal(presetDevice?: string) {
  const { body, foot, close } = openModal("📣", "屏幕广播");
  const status = await BroadcastService.status();

  const statusLine = el(
    "div",
    { class: "note info" },
    status.active
      ? `当前广播中 · ${status.mode === "student_demo" ? `学生演示（${status.active_sender}）` : "教师屏幕"}`
      : "当前没有进行中的广播",
  );

  let mode = presetDevice ? "demo" : "teacher";
  const cards = optionCards(
    [
      { value: "teacher", icon: "🖥️", title: "教师屏幕广播", desc: "把教师机画面推送给全班学生" },
      { value: "demo", icon: "🙋", title: "学生演示", desc: "把指定学生机的画面广播给全班" },
    ],
    (v) => (mode = v),
    mode,
  );

  const deviceSelect = el("select", {}, ...store.get().devices.map((d) => el("option", { value: d.device_id }, d.device_id)));
  if (presetDevice) deviceSelect.value = presetDevice;
  const deviceField = field("演示来源学生机", deviceSelect, presetDevice ? undefined : "选择一台在线学生机作为演示主机");

  body.append(
    statusLine,
    el("div", { class: "field" }, cards),
    deviceField,
  );

  foot.append(
    el("button", {
      class: "btn",
      onclick: async () => {
        await BroadcastService.stop();
        store.set({ broadcast: await BroadcastService.status() });
        store.log("🛑", "屏幕广播已停止");
        toast("🛑", "广播已停止");
        close();
      },
    }, "🛑 停止广播"),
    el("button", {
      class: "btn primary",
      onclick: async () => {
        try {
          const next =
            mode === "demo"
              ? await BroadcastService.startDemo(deviceSelect.value || store.get().devices[0]?.device_id || "")
              : await BroadcastService.startTeacher();
          store.set({ broadcast: next });
          store.log("📣", next.mode === "student_demo" ? `学生演示：${next.active_sender}` : "教师屏幕广播已开启");
          toast("📣", "广播已开启");
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "▶ 开始广播"),
  );
}

/* ---------- 黑屏肃静 ---------- */

export function openLockModal() {
  const { body, foot, close } = openModal("🔒", "黑屏肃静");

  const note = el(
    "div",
    { class: "note info" },
    "ⓘ 主按钮「下发学生机」通过指令通道锁定全部学生机；「本机演示」仅锁定教师机。",
  );

  const presets = optionCards(
    [
      { value: "focus", icon: "🎯", title: "全屏专注", desc: "锁定屏幕并展示课堂提示" },
      { value: "exam", icon: "📝", title: "考试模式", desc: "锁定屏幕，仅保留考试提示" },
      { value: "input", icon: "⌨️", title: "仅锁键鼠", desc: "屏幕保持可见，只禁用键鼠输入" },
    ],
    (v) => (preset = v),
    "focus",
  );
  let preset = "focus";

  const titleInput = el("input", { type: "text", value: "全屏专注模式" }) as HTMLInputElement;
  const msgInput = el("input", { type: "text", value: "请注意听讲，注意看教师屏幕演示！" }) as HTMLInputElement;

  body.append(
    note,
    el("div", { class: "field" }, presets),
    field("标题", titleInput),
    field("提示语", msgInput),
  );

  foot.append(
    el("button", {
      class: "btn",
      onclick: async () => {
        try {
          await ControlService.unlockStudents();
          store.log("🔓", "已解锁全部学生机");
          toast("🔓", "学生机解锁指令已下发");
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "🔓 解锁学生机"),
    el("button", {
      class: "btn",
      title: "仅在教师机本机演示锁定效果",
      onclick: async () => {
        try {
          if (preset === "input") {
            await ControlService.lockScreen(titleInput.value || "课堂管控", msgInput.value, true);
          } else {
            await ControlService.lockScreen(titleInput.value || "全屏专注模式", msgInput.value, false);
          }
          const lock = await ControlService.lockState();
          store.set({ lock: { state: lock.state, is_trapping: lock.is_trapping } });
          store.log("🔒", "本机演示锁定已开启");
          toast("🔒", "本机锁定已执行");
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "🧪 本机演示"),
    el("button", {
      class: "btn danger",
      onclick: async () => {
        try {
          await ControlService.lockStudents(titleInput.value || "全屏专注模式", msgInput.value, preset === "input");
          store.log("🔒", "黑屏肃静已下发全部学生机");
          toast("🔒", "锁定指令已下发学生机");
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "🔒 下发学生机"),
  );
}

export async function quickUnlock() {
  try {
    await ControlService.unlockScreen();
    const lock = await ControlService.lockState();
    store.set({ lock: { state: lock.state, is_trapping: lock.is_trapping } });
    store.log("🔓", "一键解锁");
    toast("🔓", "已解锁");
  } catch (err) {
    toast("⚠️", errMsg(err));
  }
}

/* ---------- 电源控制 ---------- */

export function openPowerModal() {
  const { body, foot, close } = openModal("⚡", "电源控制");

  const note = el(
    "div",
    { class: "note danger" },
    "ⓘ 电源指令通过指令通道下发到选中的学生机（10 秒宽限期）。「本机」作用于教师机。",
  );

  let action = "lock";
  const cards = optionCards(
    [
      { value: "lock", icon: "🔐", title: "锁定", desc: "锁定屏幕" },
      { value: "logoff", icon: "🚪", title: "注销", desc: "注销当前用户会话" },
      { value: "reboot", icon: "🔄", title: "重启", desc: "重启计算机" },
      { value: "shutdown", icon: "⏻", title: "关机", desc: "关闭计算机" },
    ],
    (v) => (action = v),
    "lock",
  );

  const allCheck = el("input", { type: "checkbox", checked: true }) as HTMLInputElement;
  const targetField = field(
    "目标",
    el("label", { class: "check-row" }, allCheck, "全部在线学生机（取消勾选则下发到画面墙选中的设备）"),
  );

  body.append(note, el("div", { class: "field" }, cards), targetField);

  foot.append(
    el("button", { class: "btn", onclick: () => close() }, "取消"),
    el("button", {
      class: "btn",
      onclick: async () => {
        try {
          await ControlService.power({ action, grace_period_seconds: 10 });
          store.log("⚡", `本机电源指令：${action}`);
          toast("⚡", `本机电源指令 ${action} 已下发`);
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "🧪 本机"),
    el("button", {
      class: "btn danger",
      onclick: async () => {
        const targets = allCheck.checked ? [] : [...store.get().selected];
        if (!allCheck.checked && targets.length === 0) {
          toast("💡", "请先在画面墙选中目标学生机");
          return;
        }
        try {
          await ControlService.powerToStudents(action, targets);
          store.log("⚡", `电源指令 ${action} → ${allCheck.checked ? "全部学生机" : targets.join("、")}`);
          toast("⚡", `电源指令 ${action} 已下发`);
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "⚡ 下发学生机"),
  );
}

/* ---------- 考试模式（网络封锁 + 进程限制 + 离线策略 + 拦截记录） ---------- */

export async function openFirewallModal() {
  const { body, close } = openModal("🛡️", "考试模式", { wide: true });

  const policy = await ControlService.firewallPolicy();
  const note = el(
    "div",
    { class: "note info" },
    "ⓘ 策略通过指令通道下发到全部在线学生机；「本机演示」仅在教师机生效。",
  );

  // 目标：下发学生机 / 本机演示
  let target = "students";
  const targetCards = optionCards(
    [
      { value: "students", icon: "🖥️", title: "下发学生机", desc: "全部在线学生机执行（推荐）" },
      { value: "local", icon: "🧪", title: "本机演示", desc: "仅在教师机本机应用" },
    ],
    (v) => (target = v),
    "students",
  );

  let fwMode = policy.mode || "allow_all";
  const fwCards = optionCards(
    [
      { value: "allow_all", icon: "🌐", title: "开放网络", desc: "允许全部网络访问" },
      { value: "block_internet", icon: "🚫", title: "禁止上网", desc: "仅保留局域网，阻断互联网" },
      { value: "whitelist_only", icon: "📋", title: "网站白名单", desc: "只允许指定网站，其余全封" },
      { value: "block_sites", icon: "⛔", title: "网站黑名单", desc: "只封指定网站，其余放行" },
    ],
    (v) => (fwMode = v),
    fwMode,
  );

  const domainsInput = el("input", {
    type: "text",
    value: (policy.allowed_domains ?? []).join(", "),
    placeholder: "exam.example.com, moodle.school.edu",
  }) as HTMLInputElement;
  const domainsField = field("白名单域名（逗号分隔，仅白名单模式）", domainsInput);

  const blockedInput = el("input", {
    type: "text",
    value: (policy.blocked_domains ?? []).join(", "),
    placeholder: "game.com, video.example.com",
  }) as HTMLInputElement;
  const blockedField = field("黑名单网站（逗号分隔，仅黑名单模式）", blockedInput);

  // 进程限制：黑名单 / 白名单
  let procMode = "blocklist";
  const procCards = optionCards(
    [
      { value: "blocklist", icon: "🚫", title: "禁止运行", desc: "黑名单：禁止运行列出的程序" },
      { value: "allowlist", icon: "✅", title: "仅允许运行", desc: "白名单：只允许运行列出的程序" },
    ],
    (v) => (procMode = v),
    "blocklist",
  );
  const procInput = el("input", { type: "text", value: "game.exe, steam.exe", placeholder: "进程名, 逗号分隔" }) as HTMLInputElement;
  const procField = field("进程列表（匹配进程名）", procInput, "在学生机上查找并终止（黑名单）或保留（白名单）匹配进程");

  // 离线策略
  let offlineEnabled = false;
  const offlineCheck = el("input", { type: "checkbox" }) as HTMLInputElement;
  offlineCheck.addEventListener("change", () => (offlineEnabled = offlineCheck.checked));
  const leaseInput = el("input", { type: "number", value: "10", min: "3", max: "120" }) as HTMLInputElement;
  const offlineField = field("断网锁定宽限（秒）", leaseInput, "勾选后：学生机断网超过宽限期将自动锁屏警告，恢复连接后解除");
  offlineField.prepend(el("label", { class: "check-row" }, offlineCheck, "启用离线守护"));

  const results = el("div", { class: "kv-list" });

  const applyFw = async () => {
    try {
      if (target === "students") {
        await ControlService.applyFirewallToStudents({
          mode: fwMode,
          allowed_domains: domainsInput.value.split(",").map((s) => s.trim()).filter(Boolean),
          blocked_domains: blockedInput.value.split(",").map((s) => s.trim()).filter(Boolean),
          allowed_subnets: ["192.168.0.0/16", "127.0.0.1/32"],
        });
      } else {
        await ControlService.applyFirewall({
          mode: fwMode,
          allowed_domains: domainsInput.value.split(",").map((s) => s.trim()).filter(Boolean),
          blocked_domains: blockedInput.value.split(",").map((s) => s.trim()).filter(Boolean),
          allowed_subnets: ["192.168.0.0/16", "127.0.0.1/32"],
        });
      }
      store.log("🛡️", `网络策略 ${fwMode} → ${target === "students" ? "全部学生机" : "本机"}`);
      toast("🛡️", "网络策略已应用");
    } catch (err) {
      toast("⚠️", errMsg(err));
    }
  };

  const applyProc = async () => {
    const names = procInput.value.split(",").map((s) => s.trim()).filter(Boolean);
    if (names.length === 0) {
      toast("💡", "请先填写进程名");
      return;
    }
    const pol = { mode: procMode, processes: names, reason: "考试模式" };
    try {
      if (target === "students") {
        await ControlService.applyProcessPolicyToStudents(pol);
        toast("🛡️", "进程策略已下发学生机");
      } else {
        const enforcement = await ControlService.applyProcessPolicy(pol);
        clear(results);
        if (enforcement.length === 0) results.append(el("div", { class: "event-empty" }, "未发现匹配进程"));
        for (const r of enforcement) {
          results.append(el("div", { class: "kv" },
            el("span", { class: "k" }, `${r.name} (PID ${r.pid})`),
            el("span", { class: "v" }, r.terminated ? "✅ 已终止" : `⚠️ ${r.error || "未终止"}`),
          ));
        }
      }
      store.log("🛡️", `进程策略(${procMode}) → ${target === "students" ? "全部学生机" : "本机"}`);
    } catch (err) {
      toast("⚠️", errMsg(err));
    }
  };

  const applyOffline = async () => {
    try {
      await ControlService.setOfflinePolicy({ enabled: offlineEnabled, lease_seconds: Number(leaseInput.value) || 10 });
      toast("🛡️", offlineEnabled ? "离线守护已开启" : "离线守护已关闭");
      store.log("🛡️", `离线守护 ${offlineEnabled ? "开启" : "关闭"}`);
    } catch (err) {
      toast("⚠️", errMsg(err));
    }
  };

  // 拦截记录（实时推送 + 初始加载）
  const auditList = el("div", { class: "kv-list", style: "max-height:220px; overflow-y:auto" });
  const renderAudit = (list: { device_id: string; kind: string; detail: string; occurred_at: string }[]) => {
    clear(auditList);
    if (list.length === 0) {
      auditList.append(el("div", { class: "event-empty" }, "暂无学生机上报记录"));
      return;
    }
    for (const e of list) {
      auditList.append(el("div", { class: "kv" },
        el("span", { class: "k" }, `${e.device_id} · ${e.detail || e.kind}`),
        el("span", { class: "v" }, new Date(e.occurred_at).toLocaleTimeString("zh-CN")),
      ));
    }
  };
  const offAudit = events.agentAudit.on((e) => {
    auditList.prepend(el("div", { class: "kv" },
      el("span", { class: "k" }, `${e.DeviceID} · ${e.Detail || e.Kind}`),
      el("span", { class: "v" }, new Date(e.OccurredAt).toLocaleTimeString("zh-CN")),
    ));
  });
  onModalClose(offAudit);

  body.append(
    note,
    el("div", { class: "field" }, targetCards),
    el("h3", { style: "font-size:13px; margin-bottom:10px" }, "🌐 网络封锁"),
    el("div", { class: "field" }, fwCards),
    domainsField,
    blockedField,
    el("div", { style: "display:flex; gap:8px" },
      el("button", { class: "btn primary", onclick: applyFw }, "应用网络策略"),
    ),
    el("h3", { style: "font-size:13px; margin:16px 0 10px" }, "🚫 进程限制"),
    el("div", { class: "field" }, procCards),
    procField,
    el("div", { style: "display:flex; gap:8px" },
      el("button", { class: "btn danger", onclick: applyProc }, "应用进程策略"),
    ),
    el("h3", { style: "font-size:13px; margin:16px 0 10px" }, "🔌 离线守护"),
    offlineField,
    el("button", { class: "btn", onclick: applyOffline }, "应用离线策略"),
    el("h3", { style: "font-size:13px; margin:16px 0 10px" }, "📜 学生机上报记录"),
    el("button", {
      class: "btn",
      onclick: async () => {
        try { renderAudit(await AuditService.recent(50, "")); } catch (err) { toast("⚠️", errMsg(err)); }
      },
    }, "🔄 刷新记录"),
    auditList,
  );
}

/* ---------- 机房频道 ---------- */

export async function openChannelModal() {
  const { body, foot, close } = openModal("📡", "机房频道");

  const ch = store.get().channel;
  const info = el(
    "div",
    { class: "kv-list" },
    el("div", { class: "kv" }, el("span", { class: "k" }, "教室"), el("span", { class: "v" }, ch.classroom_id)),
    el("div", { class: "kv" }, el("span", { class: "k" }, "屏幕组播"), el("span", { class: "v" }, ch.screen_multicast || "—")),
    el("div", { class: "kv" }, el("span", { class: "k" }, "音频组播"), el("span", { class: "v" }, ch.audio_multicast || "—")),
    el("div", { class: "kv" }, el("span", { class: "k" }, "频道指纹"), el("span", { class: "v" }, ch.fingerprint ? ch.fingerprint.slice(0, 20) + "…" : "—")),
  );

  const input = el("input", { type: "number", min: "1", max: "255", value: String(ch.channel_id) }) as HTMLInputElement;

  body.append(
    el("div", { class: "note info" }, "ⓘ 不同频道的教室使用不同组播地址，互相隔离，防止串课堂。"),
    info,
    field("频道 ID（1–255）", input),
  );

  foot.append(
    el("button", { class: "btn", onclick: () => close() }, "取消"),
    el("button", {
      class: "btn primary",
      onclick: async () => {
        try {
          const next = await ClassroomService.setChannel(Number(input.value));
          store.set({ channel: next });
          store.log("📡", `切换到频道 ${next.channel_id}`);
          toast("📡", `已切换到频道 ${next.channel_id}`);
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "切换频道"),
  );
}

/* ---------- 文件分发 / 收集 ---------- */

import { FilesService } from "../mygo";

export function openFileDistributeModal() {
  const { body, foot, close } = openModal("📄", "文件分发");
  const note = el("div", { class: "note info" }, "ⓘ 文件通过教师端 HTTP 服务下发，学生机下载后保存到桌面「LocalMesh 收到的文件」并弹窗提示。");
  const allCheck = el("input", { type: "checkbox", checked: true }) as HTMLInputElement;
  const targetField = field("目标", el("label", { class: "check-row" }, allCheck, "全部在线学生机（取消勾选则下发到选中设备）"));
  const statusEl = el("div", { class: "note info" });

  body.append(note, targetField, statusEl);
  foot.append(
    el("button", {
      class: "btn primary",
      onclick: async () => {
        const targets = allCheck.checked ? [] : [...store.get().selected];
        if (!allCheck.checked && targets.length === 0) {
          toast("💡", "请先在画面墙选中目标学生机");
          return;
        }
        try {
          const name = await FilesService.distribute(targets);
          if (!name) return; // 用户取消
          statusEl.textContent = `已下发「${name}」`;
          store.log("📄", `文件分发：${name}`);
          toast("📄", `「${name}」分发指令已发出`);
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "📂 选择文件并分发"),
  );
}

export async function openFileCollectModal() {
  const { body, foot } = openModal("📥", "文件收集");
  const patternInput = el("input", { type: "text", value: "*.docx, *.doc, *.zip", placeholder: "通配符模式" }) as HTMLInputElement;
  const statusEl = el("div", { class: "note info" }, "ⓘ 收集学生机桌面上匹配的文件，打包回传到教师端。");
  const list = el("div", { class: "kv-list", style: "max-height:260px; overflow-y:auto" });

  const render = (files: { device_id: string; name: string; size_bytes: number; updated_at: string }[]) => {
    clear(list);
    if (files.length === 0) {
      list.append(el("div", { class: "event-empty" }, "尚未收到任何文件"));
      return;
    }
    for (const f of files) {
      list.append(el("div", { class: "kv" },
        el("span", { class: "k" }, `${f.device_id} · ${f.name} (${(f.size_bytes / 1024).toFixed(0)} KB)`),
        el("span", { class: "v" }, new Date(f.updated_at).toLocaleTimeString("zh-CN")),
      ));
    }
  };

  try { render(await FilesService.listCollected()); } catch { /* ignore */ }

  body.append(
    field("收集模式（文件通配符）", patternInput),
    statusEl,
    list,
  );
  foot.append(
    el("button", { class: "btn", onclick: async () => { try { render(await FilesService.listCollected()); } catch { /* ignore */ } } }, "🔄 刷新"),
    el("button", {
      class: "btn primary",
      onclick: async () => {
        const pattern = patternInput.value.split(",").map((s) => s.trim()).filter(Boolean).join(",");
        try {
          await ControlService.queueCollect(pattern);
          store.log("📥", "文件收集指令已下发");
          toast("📥", "收集指令已下发学生机");
          setTimeout(async () => { try { render(await FilesService.listCollected()); } catch { /* ignore */ } }, 6000);
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "📤 发起收集"),
  );
}

/* ---------- 首次运行引导 ---------- */

export async function maybeShowWizard() {
  if (localStorage.getItem("lm.wizard") === "done") return;
  localStorage.setItem("lm.wizard", "done");

  const { body, foot, close } = openModal("🧭", "欢迎使用 LocalMesh 课堂");
  const chInput = el("input", { type: "number", value: "1", min: "1", max: "255" }) as HTMLInputElement;
  const guardCheck = el("input", { type: "checkbox", checked: true }) as HTMLInputElement;

  body.append(
    el("div", { class: "note info" }, "三步开始第一节课："),
    el("div", { class: "kv-list" },
      el("div", { class: "kv" }, el("span", { class: "k" }, "① 选择机房频道"), el("span", { class: "v" }, "同一频道的学生机互相隔离")),
      el("div", { class: "kv" }, el("span", { class: "k" }, "② 启动学生机 Agent"), el("span", { class: "v" }, el("code", {}, "go run ./apps/agent"))),
      el("div", { class: "kv" }, el("span", { class: "k" }, "③ 点命令栏「▶ 上课」"), el("span", { class: "v" }, "广播+伴音+离线守护一键开启")),
    ),
    field("机房频道 (1-255)", chInput),
    el("label", { class: "check-row" }, guardCheck, "启用离线守护（断网自动锁屏）"),
  );
  foot.append(el("button", {
    class: "btn primary",
    onclick: async () => {
      try {
        await ClassroomService.setChannel(Number(chInput.value) || 1);
        await ControlService.setOfflinePolicy({ enabled: guardCheck.checked, lease_seconds: 10 });
      } catch { /* ignore */ }
      close();
    },
  }, "完成"));
}

/* ---------- 分组教学 ---------- */

export async function openGroupModal() {
  const { body, foot } = openModal("👥", "分组教学", { wide: true });
  const note = el("div", { class: "note info" }, "ⓘ 组建分组后，锁定/解锁等指令可通过组目标下发，广播与策略随后支持按组隔离。");
  const nameInput = el("input", { type: "text", placeholder: "分组名称，如：第一组" }) as HTMLInputElement;
  const listWrap = el("div");

  const deviceSelect = el("select", {}, ...store.get().devices.map((d) => el("option", { value: d.device_id }, d.device_id))) as HTMLSelectElement;
  const groupSelect = el("select") as HTMLSelectElement;

  async function refresh() {
    const groups = await GroupService.listGroups();
    clear(listWrap);
    if (groups.length === 0) {
      listWrap.append(el("div", { class: "event-empty" }, "还没有分组，先创建一个"));
    }
    clear(groupSelect);
    for (const g of groups) {
      groupSelect.append(el("option", { value: g.group_id }, g.name));
      const wrap = el("div", { class: "stat-card", style: "margin-bottom:9px" },
        el("div", { style: "display:flex; align-items:center; gap:8px; margin-bottom:6px" },
          el("b", { style: "flex:1" }, g.name),
          el("button", { class: "btn-sm danger", onclick: async () => {
              await GroupService.deleteGroup(g.group_id);
              await refresh();
            } }, "删除"),
        ),
        el("div", { style: "font-size:11.5px; color:var(--muted)" },
          g.members.length ? g.members.join("、") : "（空组）"),
      );
      listWrap.append(wrap);
    }
  }

  foot.append(
    el("button", { class: "btn", onclick: async () => {
        if (!deviceSelect.value) { toast("💡", "没有在线学生机"); return; }
        try {
          await GroupService.addMember(groupSelect.value, deviceSelect.value);
          await refresh();
          toast("👥", `${deviceSelect.value} 已加入分组`);
        } catch (err) { toast("⚠️", errMsg(err)); }
      } }, "➕ 成员加入分组"),
    el("button", {
      class: "btn danger",
      onclick: async () => {
        try {
          await ControlService.issueToGroup(groupSelect.value, "screen.lock");
          toast("🔒", "锁定指令已下发该分组");
        } catch (err) { toast("⚠️", errMsg(err)); }
      },
    }, "🔒 锁定该组"),
    el("button", { class: "btn primary", onclick: async () => {
        if (!nameInput.value.trim()) { toast("💡", "请输入分组名称"); return; }
        try {
          await GroupService.upsertGroup("group-" + Date.now(), nameInput.value.trim());
          nameInput.value = "";
          await refresh();
        } catch (err) { toast("⚠️", errMsg(err)); }
      } }, "创建分组"),
  );

  body.append(
    note,
    el("div", { style: "display:flex; gap:10px; margin-bottom:12px" }, field("新分组名称", nameInput), field("在线设备", deviceSelect), field("目标分组", groupSelect)),
    listWrap,
  );
  await refresh();
}

/* ---------- 设置 ---------- */

export async function openSettingsModal() {
  const { body, foot, close } = openModal("⚙️", "设置");
  const cfg = await ConfigService.get();

  const fpsInput = el("input", { type: "number", value: String(cfg.broadcast.fps), min: "5", max: "30" }) as HTMLInputElement;
  const qualityInput = el("input", { type: "number", value: String(cfg.broadcast.quality), min: "20", max: "95" }) as HTMLInputElement;
  const budgetInput = el("input", { type: "number", value: String(cfg.broadcast.budget_mb_per_sec), min: "0", max: "100" }) as HTMLInputElement;
  const thumbInterval = el("input", { type: "number", value: String(cfg.thumbnails.interval_ms), min: "500", max: "10000" }) as HTMLInputElement;
  const leaseInput = el("input", { type: "number", value: String(cfg.offline.lease_seconds), min: "3", max: "120" }) as HTMLInputElement;
  const logLevel = el("select", {},
    el("option", { value: "debug" }, "debug"),
    el("option", { value: "info" }, "info"),
    el("option", { value: "warn" }, "warn"),
    el("option", { value: "error" }, "error"),
  ) as HTMLSelectElement;
  logLevel.value = cfg.log.level;

  body.append(
    el("h3", { style: "font-size:13px; margin-bottom:10px" }, "📡 屏幕广播"),
    el("div", { style: "display:flex; gap:10px" },
      field("帧率 (fps)", fpsInput),
      field("JPEG 质量", qualityInput),
      field("带宽预算 (MB/s, 0=不限)", budgetInput),
    ),
    el("div", { class: "hint", style: "color:var(--faint); font-size:11px; margin-bottom:12px" }, "高丢包时系统会自动逐级下调，此为初始值"),
    el("h3", { style: "font-size:13px; margin-bottom:10px" }, "🖼 缩略图"),
    field("推送间隔 (ms)", thumbInterval),
    el("h3", { style: "font-size:13px; margin-bottom:10px" }, "🔌 离线守护默认"),
    field("宽限 (秒)", leaseInput),
    el("h3", { style: "font-size:13px; margin-bottom:10px" }, "📜 日志"),
    field("级别", logLevel, "端口等启动项暂用命令行参数，设置页覆盖运行时行为"),
  );

  foot.append(
    el("button", { class: "btn", onclick: () => close() }, "取消"),
    el("button", {
      class: "btn primary",
      onclick: async () => {
        try {
          await ConfigService.update({
            broadcast: { fps: Number(fpsInput.value), quality: Number(qualityInput.value), budget_mb_per_sec: Number(budgetInput.value) },
            thumbnails: { interval_ms: Number(thumbInterval.value), quality: cfg.thumbnails.quality },
            offline: { enabled: cfg.offline.enabled, lease_seconds: Number(leaseInput.value) },
            log: { level: logLevel.value, file: cfg.log.file },
            network: cfg.network,
          });
          store.log("⚙️", "设置已保存");
          toast("⚙️", "设置已保存（广播项下次开播生效）");
          close();
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "保存"),
  );
}

/* ---------- 分组广播调度（分组看不同画面） ---------- */

export async function openGroupCastModal() {
  const { body, foot, close } = openModal("📡", "分组广播调度", { wide: true });
  const note = el("div", { class: "note info" }, "ⓘ 每个分组可独立选源开播：教师屏幕或某个学生的演示直推。组成员自动加入本组画面。");
  const listWrap = el("div");

  async function refresh() {
    const [groups, streams] = await Promise.all([GroupService.listGroups(), GroupCastService.status()]);
    clear(listWrap);
    if (groups.length === 0) {
      listWrap.append(el("div", { class: "event-empty" }, "请先在「分组教学」创建分组"));
      return;
    }
    for (const g of groups) {
      const stream = streams.find((s) => s.group_id === g.group_id);
      const srcSel = el("select", { class: "mini-select" },
        el("option", { value: "teacher" }, "教师屏幕"),
        ...store.get().devices.filter((d) => g.members.includes(d.device_id)).map((d) => el("option", { value: d.device_id }, `演示：${d.device_id}`)),
      ) as HTMLSelectElement;
      if (stream?.source && stream.source !== "teacher") srcSel.value = stream.source;

      listWrap.append(el("div", { class: "stat-card", style: "margin-bottom:9px" },
        el("div", { style: "display:flex; align-items:center; gap:8px" },
          el("span", { class: "dot", style: `background:${groupColor(g.name)}` }),
          el("b", { style: "flex:1" }, g.name),
          el("span", { class: "hud-chip" }, `端口 ${stream?.port ?? "—"}`),
          stream?.active ? el("span", { class: "hud-chip hd" }, "开播中") : el("span", { class: "hud-chip" }, "未开播"),
        ),
        el("div", { style: "display:flex; gap:8px; margin-top:8px; align-items:center" },
          el("div", { style: "width:220px" }, srcSel),
          el("button", { class: "btn-sm primary", onclick: async () => {
              try {
                await GroupCastService.start(g.group_id, srcSel.value);
                store.log("📡", `分组 ${g.name} 开播（源=${srcSel.value === "teacher" ? "教师屏幕" : srcSel.value}）`);
                toast("📡", `${g.name} 广播已开播`);
                await refresh();
              } catch (err) { toast("⚠️", errMsg(err)); }
            } }, "▶ 开播"),
          el("button", { class: "btn-sm", onclick: async () => {
              try {
                await GroupCastService.stop(g.group_id, srcSel.value === "teacher" ? "teacher" : srcSel.value);
                toast("📡", `${g.name} 广播已停止`);
                await refresh();
              } catch (err) { toast("⚠️", errMsg(err)); }
            } }, "⏹ 停播"),
        ),
      ));
    }
  }

  foot.append(el("button", { class: "btn", onclick: () => close() }, "关闭"));
  body.append(note, listWrap);
  await refresh();
}

/* ---------- 系统限制（考试模式分区） ---------- */

export async function openSystemPolicyModal() {
  const { body, foot, close } = openModal("🚫", "学生机系统限制", { wide: true });
  const note = el("div", { class: "note info" }, "ⓘ 通过注册表策略下发到全部学生机；USB 存储限制需要学生机管理员权限。「还原」解除全部限制。");
  const restrictions = await ControlService.systemRestrictions();
  const checks = restrictions.map((r) => {
    const check = el("input", { type: "checkbox" }) as HTMLInputElement;
    return { view: r, check };
  });

  const list = el("div", { class: "kv-list" });
  for (const { view, check } of checks) {
    list.append(el("label", { class: "check-row", style: "padding:8px 11px; background:rgba(255,255,255,0.03); border-radius:8px; margin-bottom:6px" }, check, view.label));
  }

  body.append(note, list);
  foot.append(
    el("button", { class: "btn", onclick: async () => {
        try {
          await ControlService.applySystemPolicyToStudents([]);
          store.log("🚫", "系统限制已全部还原");
          toast("🚫", "限制已还原");
          close();
        } catch (err) { toast("⚠️", errMsg(err)); }
      } }, "♻️ 还原全部"),
    el("button", { class: "btn danger", onclick: async () => {
        const enabled = checks.filter((c) => c.check.checked).map((c) => c.view.name);
        if (enabled.length === 0) { toast("💡", "请至少勾选一项"); return; }
        try {
          await ControlService.applySystemPolicyToStudents(enabled);
          store.log("🚫", `系统限制已下发（${enabled.length} 项）`);
          toast("🚫", "系统限制已下发学生机");
          close();
        } catch (err) { toast("⚠️", errMsg(err)); }
      } }, "🚫 下发限制"),
  );
}


