// Central observable store shared by every view.

export interface DeviceInfo {
  device_id: string;
  endpoint: string;
  method: string;
  seen_at: string;
}

export interface ThumbnailInfo {
  device_id: string;
  width: number;
  height: number;
  data: string; // base64 JPEG (Go []byte → base64 string)
  timestamp: string;
  sequence: number;
}

export interface ChatMessage {
  id: string;
  classroom_id: string;
  sender_id: string;
  sender_name: string;
  sender_role: string;
  target_id?: string;
  content: string;
  timestamp: string;
  is_whisper: boolean;
}

export interface HandRecord {
  id: string;
  device_id: string;
  student_name: string;
  reason?: string;
  status: string;
  raised_at: string;
}

export interface Seat {
  row: number;
  col: number;
  device_id?: string;
  student_name?: string;
  occupied: boolean;
}

export interface BroadcastStatus {
  active: boolean;
  mode: string;
  active_sender: string;
}

export interface AudioStatus {
  streaming: boolean;
  target: string;
}

export interface LockStatus {
  state: string;
  is_trapping: boolean;
}

export interface ChannelInfo {
  channel_id: number;
  classroom_id: string;
  screen_multicast: string;
  audio_multicast: string;
  fingerprint: string;
}

export interface QuizStatus {
  active: boolean;
  state: string;
  quiz?: Quiz | null;
  time_remain: number;
  submissions: number;
}

export interface Quiz {
  id: string;
  title: string;
  description?: string;
  questions: QuizQuestion[];
  time_limit_seconds: number;
}

export interface QuizQuestion {
  id: string;
  text: string;
  type: string;
  options: string[];
  correct_answers: number[];
  points: number;
}

export interface AttendanceStatus {
  active: boolean;
  state: string;
  session_id: string;
  title: string;
  stats: {
    total_expected: number;
    total_checked_in: number;
    attendance_rate: number;
    unchecked_students: string[];
    late_students: string[];
  };
  remain_secs: number;
}

export interface BuzzerStatus {
  state: string;
  question: string;
  rankings: { rank: number; device_id: string; student_name: string; delta_ms: number }[];
}

export interface ActivityItem {
  icon: string;
  text: string;
  at: Date;
}

export interface GroupInfo {
  group_id: string;
  name: string;
  members: string[];
}

export interface NotificationItem {
  icon: string;
  text: string;
  at: Date;
  level: "info" | "warn" | "danger";
}

export type CardSize = "S" | "M" | "L";

export interface AppState {
  devices: DeviceInfo[];
  thumbs: Map<string, ThumbnailInfo>;
  selected: Set<string>;
  chat: ChatMessage[];
  hands: HandRecord[];
  seating: Seat[][];
  broadcast: BroadcastStatus;
  audio: AudioStatus;
  lock: LockStatus;
  channel: ChannelInfo;
  quiz: QuizStatus;
  attendance: AttendanceStatus;
  buzzer: BuzzerStatus;
  view: "mosaic" | "seating";
  activity: ActivityItem[];
  unreadHands: number;
  groups: GroupInfo[];
  filterGroup: string | null;
  sortMode: "name" | "online" | "group";
  cardSize: CardSize;
  notifications: NotificationItem[];
  unreadNotifications: number;
  quizLive: { active: boolean; time_remain: number; submissions: number } | null;
}

const initial: AppState = {
  devices: [],
  thumbs: new Map(),
  selected: new Set(),
  chat: [],
  hands: [],
  seating: [],
  broadcast: { active: false, mode: "idle", active_sender: "" },
  audio: { streaming: false, target: "" },
  lock: { state: "unlocked", is_trapping: false },
  channel: {
    channel_id: 1,
    classroom_id: "CLASS-DEFAULT",
    screen_multicast: "",
    audio_multicast: "",
    fingerprint: "",
  },
  quiz: { active: false, state: "pending", quiz: null, time_remain: 0, submissions: 0 },
  attendance: {
    active: false,
    state: "Pending",
    session_id: "",
    title: "",
    stats: { total_expected: 0, total_checked_in: 0, attendance_rate: 0, unchecked_students: [], late_students: [] },
    remain_secs: 0,
  },
  buzzer: { state: "idle", question: "", rankings: [] },  view: "mosaic",
  activity: [],
  unreadHands: 0,
  groups: [],
  filterGroup: null,
  sortMode: (localStorage.getItem("lm.sort") as "name" | "online" | "group") || "name",
  cardSize: (localStorage.getItem("lm.card") as CardSize) || "M",
  notifications: [],
  unreadNotifications: 0,
  quizLive: null,
};

type Listener = (state: AppState) => void;

let state: AppState = initial;
const listeners = new Set<Listener>();

export const store = {
  get(): AppState {
    return state;
  },

  /** Merge a patch and notify listeners. */
  set(patch: Partial<AppState>) {
    state = { ...state, ...patch };
    for (const fn of listeners) fn(state);
  },

  on(fn: Listener): () => void {
    listeners.add(fn);
    return () => listeners.delete(fn);
  },

  /** Append an activity timeline entry. */
  log(icon: string, text: string) {
    const activity = [{ icon, text, at: new Date() }, ...state.activity].slice(0, 60);
    store.set({ activity });
  },

  /** Push a notification-center entry (toast 也是它的影子)。 */
  notify(icon: string, text: string, level: "info" | "warn" | "danger" = "info") {
    const notifications = [{ icon, text, at: new Date(), level }, ...state.notifications].slice(0, 50);
    store.set({ notifications, unreadNotifications: state.unreadNotifications + 1 });
  },

  groupOf(deviceID: string): string | null {
    for (const g of state.groups) {
      if (g.members.includes(deviceID)) return g.group_id;
    }
    return null;
  },
};

export function thumbURL(t: ThumbnailInfo | undefined): string | null {
  return t ? `data:image/jpeg;base64,${t.data}` : null;
}

/** Replace null cells (typed as nullable by the bridge) with empty seats, keeping grid shape. */
export function normalizeGrid(grid: (Seat | null)[][]): Seat[][] {
  return grid.map((row, r) =>
    row.map((s, c) => s ?? { row: r, col: c, occupied: false }),
  );
}
