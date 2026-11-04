// 活动抽屉 — 事件时间线 / 聊天 / 举手队列

import type { ChatMessage, HandRecord } from "../state";
import { store } from "../state";
import { clear, el, fmtTime, toast } from "../ui";
import { InteractionService } from "../mygo";

/* ---------- 事件时间线 ---------- */

let lastActivity: import("../state").ActivityItem[] | null = null;

export function renderActivity() {
  const panel = document.getElementById("panel-events")!;
  const { activity } = store.get();
  if (activity === lastActivity) return; // 无变化不重绘
  lastActivity = activity;
  clear(panel);

  if (activity.length === 0) {
    panel.append(el("div", { class: "event-empty" }, "课堂动态会实时出现在这里"));
    return;
  }
  for (const item of activity) {
    panel.append(
      el(
        "div",
        { class: "event-item" },
        el("div", { class: "event-icon" }, item.icon),
        el(
          "div",
          { class: "event-body" },
          el("div", { class: "event-text" }, item.text),
          el("div", { class: "event-time" }, fmtTime(item.at)),
        ),
      ),
    );
  }
}

/* ---------- 聊天 ---------- */

export function renderChat() {
  const log = document.getElementById("chat-log")!;
  const { chat } = store.get();
  clear(log);

  if (chat.length === 0) {
    log.append(el("div", { class: "event-empty" }, "还没有消息，向全班说点什么吧"));
    return;
  }
  for (const msg of chat) {
    log.append(bubble(msg));
  }
  log.scrollTop = log.scrollHeight;
}

function bubble(msg: ChatMessage): HTMLElement {
  const mine = msg.sender_role === "teacher";
  const who = mine
    ? "我"
    : msg.is_whisper
      ? `${msg.sender_name} · 私信`
      : msg.sender_name;

  return el(
    "div",
    { class: `chat-bubble ${mine ? "teacher" : "student"}` },
    el("div", { class: "who" }, who),
    el("span", {}, msg.content),
  );
}

export async function sendChat(content: string, target: string) {
  await InteractionService.sendChat(content, target);
  // 教师自己的消息通过 ChatHistory 统一回流（订阅只推学生消息）
  const history = await InteractionService.chatHistory();
  store.set({ chat: history });
  renderChat();
}

/** Refresh chat target dropdown: 全班 / 各分组 / 在线设备。 */
export function refreshChatTargets() {
  const select = document.getElementById("chat-target") as HTMLSelectElement;
  const current = select.value;
  clear(select);
  select.append(el("option", { value: "" }, "全班"));
  const groupGroup = el("optgroup", { label: "分组" });
  for (const g of store.get().groups) {
    groupGroup.append(el("option", { value: "group:" + g.group_id }, g.name));
  }
  if (groupGroup.children.length > 0) select.append(groupGroup);
  const deviceGroup = el("optgroup", { label: "私聊" });
  for (const d of store.get().devices) {
    deviceGroup.append(el("option", { value: d.device_id }, d.device_id));
  }
  if (deviceGroup.children.length > 0) select.append(deviceGroup);
  if ([...select.options].some((o) => o.value === current)) select.value = current;
}

/* ---------- 举手队列 ---------- */

export function renderHands() {
  const panel = document.getElementById("panel-hands")!;
  const { hands } = store.get();
  clear(panel);

  if (hands.length === 0) {
    panel.append(el("div", { class: "event-empty" }, "暂无学生举手"));
    return;
  }

  for (const hand of hands) {
    panel.append(handCard(hand));
  }
}

function handCard(hand: HandRecord): HTMLElement {
  return el(
    "div",
    { class: "hand-card" },
    el("span", { class: "hand-emoji" }, "✋"),
    el(
      "div",
      { class: "hand-info" },
      el("div", { class: "hand-name" }, hand.student_name || hand.device_id),
      el("div", { class: "hand-reason" }, hand.reason || fmtTime(hand.raised_at) + " 举手"),
    ),
    el(
      "div",
      { class: "hand-actions" },
      el("button", {
        class: "btn-sm primary",
        onclick: async (e) => {
          (e.currentTarget as HTMLButtonElement).disabled = true;
          try {
            await InteractionService.ackHand(hand.device_id);
            await InteractionService.clearHand(hand.device_id);
            toast("🙋", `已响应 ${hand.student_name || hand.device_id}`);
          } catch (err) {
            toast("⚠️", String(err));
          }
        },
      }, "响应"),
      el("button", {
        class: "btn-sm",
        onclick: async () => {
          try {
            await InteractionService.clearHand(hand.device_id);
          } catch (err) {
            toast("⚠️", String(err));
          }
        },
      }, "忽略"),
    ),
  );
}

