// i18n 骨架 — 字典表 + t()。本轮铺骨架，全量翻译为独立工程。

type Dict = Record<string, string>;

const zh: Dict = {
  "app.title": "LocalMesh 课堂",
  "bar.class": "课堂",
  "bar.control": "管控",
  "bar.teaching": "教学",
  "bar.tools": "工具",
  "scene.start": "上课",
  "scene.stop": "下课",
  "broadcast.start": "屏幕广播",
  "lock.title": "黑屏肃静",
  "unlock.all": "一键解锁",
  "mosaic.empty": "教室已就绪，等待学生入场",
  "mosaic.selected": "已选 {n} 台",
  "notify.hand": "{name} 举手了",
  "notify.offline": "{name} 离线",
  "notify.online": "{name} 上线",
  "status.online": "在线",
  "status.channel": "频道",
  "quiz.remaining": "剩余 {time}",
};

const dicts: Record<string, Dict> = { zh };

let current = "zh";

export function setLocale(lang: string) {
  if (dicts[lang]) current = lang;
}

export function t(key: string, params?: Record<string, string | number>): string {
  let text = (dicts[current] ?? zh)[key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      text = text.replace(`{${k}}`, String(v));
    }
  }
  return text;
}
