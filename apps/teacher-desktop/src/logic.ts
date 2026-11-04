// 纯逻辑层 — 无 DOM 依赖，vitest 全覆盖

export interface DeviceLike {
  device_id: string;
  endpoint: string;
  seen_at: string;
}

export interface GroupLike {
  group_id: string;
  name: string;
  members: string[];
}

export type SortMode = "name" | "online" | "group";

/** 设备排序：名称 / 在线时长（新→旧）/ 分组（未分组在最后）。 */
export function sortDevices<T extends DeviceLike>(
  devices: T[],
  mode: SortMode,
  groupOf: (deviceID: string) => string | null,
): T[] {
  const out = [...devices];
  switch (mode) {
    case "name":
      out.sort((a, b) => a.device_id.localeCompare(b.device_id));
      break;
    case "online":
      out.sort((a, b) => new Date(b.seen_at).getTime() - new Date(a.seen_at).getTime());
      break;
    case "group":
      out.sort((a, b) => {
        const ga = groupOf(a.device_id) ?? "\uffff";
        const gb = groupOf(b.device_id) ?? "\uffff";
        if (ga !== gb) return ga.localeCompare(gb);
        return a.device_id.localeCompare(b.device_id);
      });
      break;
  }
  return out;
}

/** 分组筛选：null = 全部。 */
export function filterByGroup<T extends { device_id: string }>(
  devices: T[],
  group: string | null,
  membersOf: (groupID: string) => string[],
): T[] {
  if (!group) return devices;
  const members = new Set(membersOf(group));
  return devices.filter((d) => members.has(d.device_id));
}

/** 组名 → 稳定色相（0-360），用于卡片色环。 */
export function groupHue(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) {
    h = (h * 31 + name.charCodeAt(i)) >>> 0;
  }
  return h % 360;
}

/** 组名 → CSS 颜色（固定饱和度/亮度保证可读性）。 */
export function groupColor(name: string): string {
  return `hsl(${groupHue(name)}, 70%, 60%)`;
}

/** device_id → 所在分组 id（多组归属取第一个）。 */
export function groupOfDevice(
  deviceID: string,
  groups: GroupLike[],
): string | null {
  for (const g of groups) {
    if (g.members.includes(deviceID)) return g.group_id;
  }
  return null;
}

export interface QuizQuestionLike {
  id: string;
  text: string;
  type: string;
  options: string[];
  correct_answers: number[];
  points: number;
}

/** 测验组装校验：题目完整才允许提交。 */
export function validateQuizDraft(
  questions: QuizQuestionLike[],
): { ok: boolean; reason: string } {
  if (questions.length === 0) return { ok: false, reason: "至少一道题" };
  for (const q of questions) {
    if (!q.text.trim()) return { ok: false, reason: "有题目未填写题干" };
    if (q.type !== "text") {
      if (q.options.some((o) => !o.trim())) {
        return { ok: false, reason: "有选项未填写内容" };
      }
      if (q.correct_answers.length === 0) {
        return { ok: false, reason: "有题目未设正确答案" };
      }
    }
  }
  return { ok: true, reason: "" };
}

/** 模糊匹配：子序列匹配（大小写不敏感），用于命令面板。 */
export function fuzzyMatch(query: string, target: string): boolean {
  const q = query.toLowerCase();
  const t = target.toLowerCase();
  let ti = 0;
  for (const ch of q) {
    ti = t.indexOf(ch, ti);
    if (ti === -1) return false;
    ti++;
  }
  return true;
}
