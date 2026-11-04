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
$("#chat-btn").addEventListener("click", toggleChat);

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
  if ($("#quiz-pop").classList.contains("open")) void refreshQuiz();
});

async function refreshQuiz() {
  const body = $("#quiz-body");
  try {
    const status = await StudentService.activeQuiz();
    if (!status.active || !status.quiz) {
      body.textContent = "暂无进行中的测验";
      return;
    }
    quizPicked = {};
    quizText = {};
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

/* 页面事件 */
events.uiNotify.on((n) => {
  if (n.type === "file") {
    $("#bar-status").textContent = n.title || "收到新文件";
  }
  if (n.type === "file_progress") {
    $("#bar-status").textContent = n.title || "";
  }
});

void refreshChat;
setInterval(() => {
  if ($("#chat-pop").classList.contains("open")) void refreshChat();
}, 5000);
