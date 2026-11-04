// 教学类弹层：电子画板 / 随堂测验 / 抢答竞赛 / 点名考勤 / 脚本终端 / 资产盘点

import type { QuizQuestion } from "../state";
import { store } from "../state";
import { clear, el, field, fmtClock, onModalClose, openModal, toast } from "../ui";
import { TeachingService, ToolsService } from "../mygo";
import { errMsg } from "./seating";

/* ---------- 电子画板（真实画布） ---------- */

export async function openWhiteboardModal() {
  const { body, close } = openModal("🎨", "电子画板", { wide: true });

  let tool = "pen";
  let color = "#e63946";
  let width = 3;

  const canvas = el("canvas", { id: "wb-canvas" }) as HTMLCanvasElement;
  const ctx = canvas.getContext("2d")!;
  const dpr = window.devicePixelRatio || 1;

  const resize = () => {
    const rect = canvas.getBoundingClientRect();
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    redraw();
  };

  let strokes: { tool: string; color: string; width: number; points: { x: number; y: number }[] }[] = [];

  async function redraw() {
    try {
      strokes = (await TeachingService.whiteboardStrokes()).map((s) => ({
        tool: s.tool,
        color: `rgba(${s.color.r},${s.color.g},${s.color.b},${(s.color.a / 255).toFixed(2)})`,
        width: s.width,
        points: s.points.map((p) => ({ x: p.x, y: p.y })),
      }));
    } catch {
      /* backend unavailable — keep local */
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    for (const s of strokes) drawStroke(ctx, s);
  }

  function drawStroke(c: CanvasRenderingContext2D, s: { tool: string; color: string; width: number; points: { x: number; y: number }[] }) {
    const first = s.points[0];
    if (!first) return;
    c.save();
    c.strokeStyle = s.color;
    c.lineWidth = s.tool === "highlighter" ? s.width * 4 : s.width;
    c.globalAlpha = s.tool === "highlighter" ? 0.35 : 1;
    c.lineCap = "round";
    c.lineJoin = "round";
    c.beginPath();
    c.moveTo(first.x * canvas.width / dpr, first.y * canvas.height / dpr);
    for (const p of s.points.slice(1)) c.lineTo(p.x * canvas.width / dpr, p.y * canvas.height / dpr);
    c.stroke();
    c.restore();
  }

  // 本地绘制（笔画实时反馈），抬笔后提交到后端
  let current: { tool: string; color: string; width: number; points: { x: number; y: number }[] } | null = null;

  const toNorm = (e: PointerEvent) => {
    const r = canvas.getBoundingClientRect();
    return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height };
  };

  canvas.addEventListener("pointerdown", (e) => {
    if (tool === "undo" || tool === "redo" || tool === "clear") return;
    current = { tool, color, width, points: [toNorm(e)] };
    canvas.setPointerCapture(e.pointerId);
  });
  canvas.addEventListener("pointermove", (e) => {
    if (!current) return;
    current.points.push(toNorm(e));
    redrawLocal();
  });
  canvas.addEventListener("pointerup", async () => {
    if (!current) return;
    const s = current;
    current = null;
    if (s.points.length < 2) return;
    redrawLocal();
    const [r, g, b] = hexToRGB(color);
    try {
      await TeachingService.addWhiteboardStroke({
        tool: tool as never,
        color: { r, g, b, a: tool === "highlighter" ? 90 : 255 },
        width,
        points: s.points,
      });
    } catch (err) {
      toast("⚠️", errMsg(err));
    }
  });

  function redrawLocal() {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    for (const s of strokes) drawStroke(ctx, s);
    if (current) drawStroke(ctx, current);
  }

  // 工具栏
  const tools: [string, string, string][] = [
    ["pen", "✏️", "画笔"],
    ["highlighter", "🖍️", "荧光笔"],
    ["undo", "↩️", "撤销"],
    ["redo", "↪️", "重做"],
    ["clear", "🗑️", "清空"],
  ];
  const toolbar = el("div", { class: "wb-toolbar" });
  for (const [value, icon, title] of tools) {
    toolbar.append(el("button", {
      class: `wb-tool${value === tool ? " active" : ""}`,
      title,
      "data-tool": value,
      onclick: async (e) => {
        const btn = e.currentTarget as HTMLElement;
        if (value === "undo") { await TeachingService.undoWhiteboard(); await redraw(); return; }
        if (value === "redo") { await TeachingService.redoWhiteboard(); await redraw(); return; }
        if (value === "clear") { await TeachingService.clearWhiteboard(); await redraw(); return; }
        toolbar.querySelectorAll(".wb-tool").forEach((t) => t.classList.remove("active"));
        btn.classList.add("active");
        tool = value;
      },
    }, icon));
  }
  for (const c of ["#e63946", "#f4a261", "#2a9d8f", "#457b9d", "#1d1d1f"]) {
    toolbar.append(el("button", {
      class: `wb-color${c === color ? " active" : ""}`,
      style: `background:${c}`,
      title: c,
      onclick: (e) => {
        color = c;
        toolbar.querySelectorAll(".wb-color").forEach((x) => x.classList.remove("active"));
        (e.currentTarget as HTMLElement).classList.add("active");
      },
    }));
  }

  body.append(toolbar, canvas);
  onModalClose(() => window.removeEventListener("resize", resize));
  window.addEventListener("resize", resize);
  requestAnimationFrame(resize);
  void close;
}

function hexToRGB(hex: string): [number, number, number] {
  const v = hex.replace("#", "");
  return [parseInt(v.slice(0, 2), 16), parseInt(v.slice(2, 4), 16), parseInt(v.slice(4, 6), 16)];
}

/* ---------- 随堂测验 ---------- */

export function openQuizModal() {
  const { body, foot, close } = openModal("📝", "随堂测验", { wide: true });

  const titleInput = el("input", { type: "text", value: "课堂小测", placeholder: "测验标题" }) as HTMLInputElement;
  const timeInput = el("input", { type: "number", value: "120", min: "10", max: "3600" }) as HTMLInputElement;
  const timeChips = el("div", { class: "quiz-time-chips" });
  for (const s of [30, 60, 120, 300]) {
    timeChips.append(el("button", {
      class: "chip",
      onclick: () => { timeInput.value = String(s); },
    }, s < 60 ? s + "s" : (s / 60) + "m"));
  }

  const questions: QuizQuestion[] = [];
  const builder = el("div");

  function questionEditor(q: QuizQuestion): HTMLElement {
    const text = el("input", { type: "text", value: q.text, placeholder: "题目内容" }) as HTMLInputElement;
    text.addEventListener("input", () => (q.text = text.value));

    const typeSel = el("select", {},
      el("option", { value: "single_choice" }, "单选题"),
      el("option", { value: "true_false" }, "判断题"),
    ) as HTMLSelectElement;
    typeSel.value = q.type;
    typeSel.addEventListener("change", () => {
      q.type = typeSel.value;
      if (q.type === "true_false") q.options = ["正确", "错误"];
      else q.options = ["", "", "", ""];
      renderOptions();
    });

    const optionsWrap = el("div", { style: "display:flex; flex-direction:column; gap:6px; margin-top:8px" });
    function renderOptions() {
      clear(optionsWrap);
      q.options.forEach((opt, i) => {
        const input = el("input", { type: "text", value: opt, placeholder: `选项 ${"ABCD"[i]}` }) as HTMLInputElement;
        input.addEventListener("input", () => (q.options[i] = input.value));
        const correct = el("input", { type: "radio", name: `correct-${q.id}` }) as HTMLInputElement;
        correct.checked = q.correct_answers.includes(i);
        correct.addEventListener("change", () => (q.correct_answers = [i]));
        optionsWrap.append(el("label", { class: "check-row", style: "gap:10px" }, correct, input));
      });
    }
    if (q.options.length === 0) q.options = ["", "", "", ""];
    renderOptions();

    return el(
      "div",
      { class: "stat-card", style: "margin-bottom:10px" },
      el("div", { style: "display:flex; gap:8px; margin-bottom:8px" }, text, el("div", { style: "width:110px" }, typeSel)),
      optionsWrap,
    );
  }

  function addQuestion() {
    const q: QuizQuestion = {
      id: `q${Date.now()}-${questions.length + 1}`,
      text: "",
      type: "single_choice",
      options: ["", "", "", ""],
      correct_answers: [0],
      points: 10,
    };
    questions.push(q);
    builder.append(questionCard(q));
  }

  /** 单题卡片：编辑 + 上移/下移/复制/删除 */
  function questionCard(q: QuizQuestion): HTMLElement {
    const card = questionEditor(q);
    const tools = el("div", { style: "display:flex; gap:5px; margin-top:8px" });
    const move = (delta: number) => {
      const i = questions.indexOf(q);
      const j = i + delta;
      if (j < 0 || j >= questions.length) return;
      const a = questions[i];
      const b = questions[j];
      if (!a || !b) return;
      [questions[i], questions[j]] = [b, a];
      rebuildBuilder();
    };
    tools.append(
      el("button", { class: "btn-sm", onclick: () => move(-1) }, "↑ 上移"),
      el("button", { class: "btn-sm", onclick: () => move(1) }, "↓ 下移"),
      el("button", { class: "btn-sm", onclick: () => {
          const copy: QuizQuestion = { ...q, id: `q${Date.now()}` };
          questions.splice(questions.indexOf(q) + 1, 0, copy);
          rebuildBuilder();
        } }, "⧉ 复制"),
      el("button", { class: "btn-sm danger", onclick: () => {
          const i = questions.indexOf(q);
          if (questions.length <= 1) { toast("💡", "至少保留一道题"); return; }
          questions.splice(i, 1);
          rebuildBuilder();
        } }, "🗑 删除"),
    );
    card.append(tools);
    return card;
  }

  function rebuildBuilder() {
    clear(builder);
    for (const q of questions) builder.append(questionCard(q));
  }

  const addBtn = el("button", { class: "btn", onclick: () => addQuestion() }, "＋ 添加题目");
  addQuestion();

  // 实时状态区
  const live = el("div");
  const timer = setInterval(async () => {
    try {
      const status = await TeachingService.activeQuiz();
      clear(live);
      if (!status.quiz || status.state === "pending") {
        live.append(el("div", { class: "note info" }, "填写内容后点击「开始测验」"));
        return;
      }
      const stats = status.state === "ended" ? await TeachingService.quizStats().catch(() => null) : null;
      live.append(
        el("div", { class: "note info" },
          `「${status.quiz.title}」${status.state === "active" ? `进行中 · 剩余 ${fmtClock(status.time_remain)}` : "已结束"} · 已收到 ${status.submissions} 份答卷`),
        stats ? accuracyList(stats) : el("div"),
      );
    } catch { /* ignore */ }
  }, 1000);
  onModalClose(() => clearInterval(timer));

  body.append(
    el("div", { style: "display:flex; gap:10px" },
      field("测验标题", titleInput),
      field("限时（秒）", timeInput),
    ),
    timeChips,
    builder,
    addBtn,
    live,
  );

  foot.append(
    el("button", { class: "btn", onclick: () => close() }, "关闭"),
    el("button", {
      class: "btn danger",
      onclick: async () => {
        try { await TeachingService.endQuiz(); toast("📝", "测验已结束"); } catch (err) { toast("⚠️", errMsg(err)); }
      },
    }, "结束测验"),
    el("button", {
      class: "btn primary",
      onclick: async () => {
        if (!questions.every((q) => q.text.trim() && q.options.every((o) => o.trim()))) {
          toast("💡", "请补全题目和选项内容");
          return;
        }
        try {
          await TeachingService.startQuiz({
            id: `QUIZ-${Date.now()}`,
            title: titleInput.value || "课堂小测",
            description: "",
            questions,
            time_limit_seconds: Number(timeInput.value) || 120,
          });
          store.log("📝", `随堂测验「${titleInput.value}」已开始`);
          toast("📝", "测验已开始");
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "🚀 开始测验"),
  );
}

function accuracyList(stats: { total_submissions: number; questions_analytics: { question_id: string; total_submissions: number; correct_submissions: number; accuracy_percentage: number }[] }): HTMLElement {
  const wrap = el("div");
  for (const q of stats.questions_analytics ?? []) {
    wrap.append(
      el(
        "div",
        { class: "acc-row" },
        el("div", { class: "acc-head" },
          el("span", {}, `题目 ${q.question_id} · 答对 ${q.correct_submissions}/${q.total_submissions}`),
          el("span", { class: "acc-pct" }, `${q.accuracy_percentage.toFixed(0)}%`),
        ),
        el("div", { class: "meter" }, el("i", { style: `width:${q.accuracy_percentage}%` })),
      ),
    );
  }
  return wrap;
}

/* ---------- 抢答竞赛 ---------- */

export function openBuzzerModal() {
  const { body, foot, close } = openModal("⏱️", "抢答竞赛");

  const promptInput = el("input", { type: "text", placeholder: "本轮抢答的问题" }) as HTMLInputElement;
  const limitInput = el("input", { type: "number", value: "3", min: "1", max: "50" }) as HTMLInputElement;

  const rankings = el("div");
  const timer = setInterval(async () => {
    try {
      const s = await TeachingService.buzzerStatus();
      store.set({ buzzer: s });
      clear(rankings);
      if (s.state === "idle") {
        rankings.append(el("div", { class: "event-empty" }, "设置问题后点击「开始抢答」"));
        return;
      }
      rankings.append(el("div", { class: "note info" }, `状态：${s.state === "active" ? "抢答进行中" : "已关闭"} · ${s.question || "—"}`));
      if (s.rankings.length === 0) {
        rankings.append(el("div", { class: "event-empty" }, "等待第一位抢答者…"));
        return;
      }
      s.rankings.forEach((r, i) => {
        rankings.append(el("div", { class: `rank-row${i < 3 ? ` top${i + 1}` : ""}` },
          el("span", { class: "rank-num" }, String(r.rank)),
          el("span", { class: "rank-name" }, r.student_name || r.device_id),
          el("span", { class: "rank-delta" }, r.delta_ms > 0 ? `+${r.delta_ms} ms` : "首发"),
        ));
      });
    } catch { /* ignore */ }
  }, 800);
  onModalClose(() => clearInterval(timer));

  body.append(
    field("抢答问题", promptInput),
    field("获奖名额", limitInput),
    rankings,
  );

  foot.append(
    el("button", { class: "btn", onclick: () => close() }, "关闭"),
    el("button", {
      class: "btn",
      onclick: async () => { await TeachingService.resetBuzzer(); toast("⏱️", "已重置"); },
    }, "重置"),
    el("button", {
      class: "btn primary",
      onclick: async () => {
        try {
          await TeachingService.startBuzzer(promptInput.value, Number(limitInput.value) || 3);
          store.log("⏱️", "抢答竞赛已开始");
          toast("⏱️", "抢答开始！");
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "🚀 开始抢答"),
  );
}

/* ---------- 点名考勤 ---------- */

export function openAttendanceModal() {
  const { body, foot, close } = openModal("🙋", "点名考勤");

  const titleInput = el("input", { type: "text", value: "课堂考勤点名" }) as HTMLInputElement;
  const limitInput = el("input", { type: "number", value: "60", min: "10", max: "3600" }) as HTMLInputElement;

  const live = el("div");
  const timer = setInterval(async () => {
    try {
      const s = await TeachingService.attendanceStatus();
      store.set({ attendance: s });
      clear(live);
      if (!s.session_id) {
        live.append(el("div", { class: "event-empty" }, "发起一次点名，学生机将自动签到"));
        return;
      }
      const rate = Math.round(s.stats.attendance_rate);
      live.append(
        el("div", { class: "note info" },
          `「${s.title}」${s.state === "Active" ? `进行中 · 剩余 ${fmtClock(s.remain_secs)}` : "已结束"}`),
        el("div", { class: "stat-grid" },
          statCard("已签到", `${s.stats.total_checked_in}`, s.stats.total_expected ? `期望 ${s.stats.total_expected} 人` : "自由签到"),
          statCard("出勤率", `${rate}%`, ""),
        ),
        el("div", { class: "meter", style: "margin-top:10px" }, el("i", { style: `width:${rate}%` })),
        s.stats.late_students.length
          ? el("div", { class: "note", style: "margin-top:10px" }, `迟到：${s.stats.late_students.join("、")}`)
          : el("div"),
      );
    } catch { /* ignore */ }
  }, 1000);
  onModalClose(() => clearInterval(timer));

  body.append(
    el("div", { style: "display:flex; gap:10px" },
      field("考勤标题", titleInput),
      field("限时（秒）", limitInput),
    ),
    live,
  );

  foot.append(
    el("button", { class: "btn", onclick: () => close() }, "关闭"),
    el("button", {
      class: "btn",
      onclick: async () => {
        try {
          const path = await TeachingService.exportAttendanceCSV();
          toast("📥", path ? `已保存：${path}` : "已取消保存");
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "📥 导出报表"),
    el("button", {
      class: "btn primary",
      onclick: async () => {
        try {
          await TeachingService.startAttendance(titleInput.value, Number(limitInput.value) || 60, []);
          store.log("🙋", "考勤点名已发起");
          toast("🙋", "点名已发起");
        } catch (err) {
          toast("⚠️", errMsg(err));
        }
      },
    }, "🚀 发起点名"),
  );
}

function statCard(label: string, value: string, sub: string): HTMLElement {
  return el("div", { class: "stat-card" },
    el("div", { class: "s-label" }, label),
    el("div", { class: "s-value" }, value, sub ? el("small", {}, ` ${sub}`) : null),
  );
}

/* ---------- 脚本终端 ---------- */

export function openScriptModal() {
  const { body, foot } = openModal("💻", "脚本终端", { wide: true });

  const typeSel = el("select", {},
    el("option", { value: "powershell" }, "PowerShell"),
    el("option", { value: "batch" }, "批处理 (CMD)"),
  ) as HTMLSelectElement;
  const content = el("textarea", { placeholder: "Get-Process | Select-Object -First 5" }) as HTMLTextAreaElement;
  const output = el("div", { class: "output-console" }, "输出会显示在这里");
  const historyWrap = el("div");
  try {
    const hist: { t: string; s: string }[] = JSON.parse(localStorage.getItem("lm.scriptHistory") || "[]");
    if (hist.length) {
      historyWrap.append(el("div", { style: "font-size:11px;color:var(--faint);margin:10px 0 5px" }, "最近执行"));
      for (const h of hist) {
        historyWrap.append(el("div", { class: "kv", style: "padding:5px 10px" },
          el("span", { class: "k" }, h.s), el("span", { class: "v" }, h.t)));
      }
    }
  } catch { /* ignore */ }

  body.append(
    field("脚本类型", typeSel),
    field("脚本内容", content, "Phase 0 演示：脚本在教师机本机执行，输出实时回显"),
    output,
    historyWrap,
  );

  foot.append(
    el("button", {
      class: "btn primary",
      onclick: async (e) => {
        const btn = e.currentTarget as HTMLButtonElement;
        if (!content.value.trim()) { toast("💡", "请输入脚本内容"); return; }
        btn.disabled = true;
        output.textContent = "⏳ 执行中…";
        try {
          const result = await ToolsService.executeScript({
            script_type: typeSel.value as never,
            content: content.value,
            timeout_seconds: 15,
          });
          if (!result) throw new Error("empty result");
          clear(output);
          output.append(
            el("div", { class: result.exit_code === 0 ? "exit-ok" : "exit-err" },
              `exit_code=${result.exit_code} · ${result.duration_ms}ms${result.truncated ? " · 输出已截断" : ""}`),
            el("div", {}, result.stdout || ""),
            ...(result.stderr ? [el("div", { class: "exit-err" }, result.stderr)] : []),
          );
          try {
            const hist: { t: string; s: string }[] = JSON.parse(localStorage.getItem("lm.scriptHistory") || "[]");
            hist.unshift({ t: new Date().toLocaleTimeString("zh-CN"), s: `${typeSel.value}: exit ${result.exit_code}` });
            localStorage.setItem("lm.scriptHistory", JSON.stringify(hist.slice(0, 5)));
          } catch { /* ignore */ }
          store.log("💻", `脚本执行完成（exit ${result.exit_code}）`);
        } catch (err) {
          output.textContent = `执行失败：${errMsg(err)}`;
        } finally {
          btn.disabled = false;
        }
      },
    }, "▶ 运行"),
  );
}

/* ---------- 资产盘点 ---------- */

export async function openInventoryModal() {
  const { body, foot } = openModal("📋", "资产盘点", { wide: true });

  const dash = el("div", {}, el("div", { class: "event-empty" }, "采集中，大约需要几秒…"));
  body.append(dash);

  async function load() {
    try {
      const snap = await ToolsService.inventory();
      clear(dash);
      dash.append(
        el("div", { class: "stat-grid" },
          statCard("处理器", snap.hardware.cpu.model.split(" ").slice(0, 3).join(" "), `${snap.hardware.cpu.cores} 核 · ${snap.hardware.cpu.usage_percent.toFixed(0)}%`),
          statCard("内存", `${snap.hardware.memory.used_percent.toFixed(0)}%`, formatBytes(snap.hardware.memory.available_bytes) + " 可用"),
          statCard("操作系统", `${snap.system.os.name}`, `${snap.system.os.version} (${snap.system.os.arch})`),
          statCard("主机", snap.system.hostname, snap.system.current_user),
        ),
        el("div", { style: "margin-top:14px" },
          el("div", { class: "s-label", style: "font-size:11px; color:var(--muted); margin-bottom:7px" }, `磁盘（${snap.hardware.disks.length} 个分区）`),
          el("div", { class: "kv-list" },
            ...snap.hardware.disks.map((d) =>
              el("div", { class: "kv" },
                el("span", { class: "k" }, `${d.mount_point} (${d.file_system})`),
                el("span", { class: "v" }, `${formatBytes(d.free_bytes)} 可用 / 共 ${formatBytes(d.total_bytes)}`),
              ))),
        ),
        el("div", { style: "margin-top:14px" },
          el("div", { class: "s-label", style: "font-size:11px; color:var(--muted); margin-bottom:7px" }, `已安装软件 ${snap.installed_software.length} 款`),
          el("div", { class: "output-console", style: "max-height:180px" },
            snap.installed_software.slice(0, 50).map((s) => `${s.display_name}${s.display_version ? ` — ${s.display_version}` : ""}`).join("\n") || "无记录"),
        ),
        el("div", { class: "event-time", style: "margin-top:10px" }, `采集时间：${new Date(snap.captured_at).toLocaleString("zh-CN")}`),
      );
      store.log("📋", "资产盘点完成");
    } catch (err) {
      clear(dash);
      dash.append(el("div", { class: "event-empty" }, `采集失败：${errMsg(err)}`));
    }
  }

  foot.append(
    el("button", { class: "btn", onclick: () => load() }, "🔄 重新采集"),
  );
  await load();
}

function formatBytes(n: number): string {
  if (!n) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}
