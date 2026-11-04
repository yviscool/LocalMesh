// 悬浮条：举手 / 消息徽章 / 随堂测验答题

import { StudentService, events } from "./mygo";

const $ = <T extends HTMLElement>(sel: string) => document.querySelector<T>(sel)!;

let quizPicked: Record<string, number[]> = {};
let quizText: Record<string, string> = {};

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

/* 举手 */
$("#raise-btn").addEventListener("click", async () => {
  const btn = $<HTMLButtonElement>("#raise-btn");
  btn.disabled = true;
  try {
    await StudentService.raiseHand("学生举手");
    $("#bar-status").textContent = "已举手，等待老师…";
    setTimeout(() => ($("#bar-status").textContent = ""), 4000);
  } catch (err) {
    $("#bar-status").textContent = "举手失败";
  } finally {
    setTimeout(() => (btn.disabled = false), 2000);
  }
});

/* 消息 */
function toggleChat() {
  $("#chat-pop").classList.toggle("open");
  $("#quiz-pop").classList.remove("open");
  if ($("#chat-pop").classList.contains("open")) void refreshChat();
  $("#chat-badge").classList.add("hidden");
}
$("#chat-btn").addEventListener("click", () => {
  toggleChat();
  void StudentService.focusBar();
});

async function refreshChat() {
  try {
    const msgs = await StudentService.recentChat(20);
    const list = $("#chat-list");
    list.replaceChildren(
      ...msgs.map((m) => {
        const div = document.createElement("div");
        div.className = "chat-msg" + (m.sender_role === "teacher" ? " teacher-msg" : "");
        const who = document.createElement("span");
        who.className = "who";
        who.textContent = `${m.sender_name} ${fmtTime(m.timestamp)}`;
        div.append(who, document.createTextNode(" " + m.content));
        return div;
      }),
    );
  } catch { /* teacher offline */ }
}

/* 发送消息 */
$("#chat-send").addEventListener("submit", async (e) => {
  e.preventDefault();
  const input = $<HTMLInputElement>("#chat-input");
  const text = input.value.trim();
  if (!text) return;
  try {
    await StudentService.sendChat(text);
    input.value = "";
    void refreshChat();
  } catch {
    $("#bar-status").textContent = "发送失败";
  }
});

/* 随堂测验 */
$("#quiz-btn").addEventListener("click", () => {
  $("#quiz-pop").classList.toggle("open");
  $("#chat-pop").classList.remove("open");
  if ($("#quiz-pop").classList.contains("open")) {
    void refreshQuiz();
    void StudentService.focusBar();
  }
});

async function refreshQuiz() {
  const body = $("#quiz-body");
  try {
    const status = await StudentService.activeQuiz();
    if (!status.active || !status.quiz) {
      body.textContent = "暂无进行中的测验";
      return;
    }
    // 已交卷状态持久（本机标记）
    if (localStorage.getItem("lm.quiz.done." + status.quiz.id) === "1") {
      body.textContent = "✅ 已交卷，等待老师收卷";
      return;
    }
    quizPicked = {};
    quizText = {};
    const quizId = status.quiz.id;
    const quizRemain = status.time_remain;
    const remainEl = document.createElement("div");
    remainEl.style.cssText = "font-size:11px;color:var(--muted);margin-bottom:8px";
    const tick = () => {
      const r = Math.max(0, quizRemain - Math.floor((Date.now() - openedAt) / 1000));
      remainEl.textContent = "⏱ 剩余 " + String(Math.floor(r / 60)).padStart(2, "0") + ":" + String(r % 60).padStart(2, "0");
    };
    const openedAt = Date.now();
    tick();
    const remainTimer = setInterval(tick, 1000);
    setTimeout(() => clearInterval(remainTimer), quizRemain * 1000 + 2000);
    body.replaceChildren(
      ...status.quiz.questions.map((q) => {
        const wrap = document.createElement("div");
        wrap.className = "quiz-q";
        const text = document.createElement("div");
        text.className = "q-text";
        text.textContent = q.text;
        wrap.append(text);
        if (q.type === "text") {
          const input = document.createElement("input");
          input.className = "quiz-text";
          input.placeholder = "在这里输入答案…";
          input.addEventListener("input", () => {
            quizText[q.id] = input.value;
          });
          wrap.append(input);
          return wrap;
        }
        q.options.forEach((opt, idx) => {
          const btn = document.createElement("button");
          btn.className = "quiz-opt";
          btn.textContent = opt;
          btn.addEventListener("click", () => {
            wrap.querySelectorAll(".quiz-opt").forEach((b) => b.classList.remove("picked"));
            btn.classList.add("picked");
            quizPicked[q.id] = [idx];
          });
          wrap.append(btn);
        });
        return wrap;
      }),
    );
    const submit = document.createElement("button");
    submit.className = "quiz-submit";
    submit.textContent = "提交答卷";
    submit.addEventListener("click", async () => {
      if (Object.keys(quizPicked).length === 0) return;
      try {
        localStorage.setItem("lm.quiz.done." + quizId, "1");
        const answers = { ...quizPicked };
        // 文字题合并进提交（教师端批阅）
        if (Object.keys(quizText).length) {
          (answers as Record<string, unknown>).text_answers = quizText;
        }
        await StudentService.submitQuiz(status.quiz!.id, answers);
        body.textContent = "✅ 答卷已提交";
        $("#quiz-pop").classList.add("open");
      } catch {
        body.textContent = "提交失败，请重试";
      }
    });
    body.append(submit);
  } catch {
    body.textContent = "暂时无法连接教师端";
  }
}

/* 测验键盘 A/B/C/D */
document.addEventListener("keydown", (e) => {
  if (!$("#quiz-pop").classList.contains("open")) return;
  const key = e.key.toUpperCase();
  const idx = { A: 0, B: 1, C: 2, D: 3 }[key];
  if (idx === undefined) return;
  const picked = document.querySelectorAll(".quiz-opt.picked");
  void idx;
  const opts = document.querySelectorAll(".quiz-opt");
  const perQ = Math.max(1, Math.floor(opts.length / Math.max(1, Object.keys(quizPicked).length || 1)));
  void perQ;
  const first = document.querySelector<HTMLElement>(".quiz-q .quiz-opt:nth-child(" + (idx + 1) + ")");
  first?.click();
});

/* 学生端音效（收到文件/测验开始） */
function studentCue(freq: number) {
  try {
    const ctx = new AudioContext();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.frequency.value = freq;
    gain.gain.setValueAtTime(0.1, ctx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.18);
    osc.connect(gain).connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + 0.18);
    setTimeout(() => ctx.close(), 400);
  } catch { /* no audio */ }
}

/* 页面事件 */
events.uiNotify.on((n) => {
  if (n.type === "file") {
    $("#bar-status").textContent = n.title || "收到新文件";
    studentCue(660);
  }
  if (n.type === "file_progress") {
    $("#bar-status").textContent = n.title || "";
  }
  if (n.type === "quiz") {
    studentCue(880);
  }
});

void refreshChat;
setInterval(() => {
  if ($("#chat-pop").classList.contains("open")) void refreshChat();
}, 5000);
