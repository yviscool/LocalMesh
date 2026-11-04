// Shared DOM helpers, toast notifications and the modal shell.

export function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string | boolean | ((e: Event) => void)> = {},
  ...children: (Node | string | null | undefined)[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (typeof v === "function") {
      node.addEventListener(k.replace(/^on/, ""), v as EventListener);
    } else if (typeof v === "boolean") {
      (node as unknown as Record<string, boolean>)[k] = v;
    } else if (k === "class") {
      node.className = v;
    } else {
      node.setAttribute(k, v);
    }
  }
  for (const child of children) {
    if (child == null) continue;
    node.append(typeof child === "string" ? document.createTextNode(child) : child);
  }
  return node;
}

export function clear(node: HTMLElement) {
  while (node.firstChild) node.removeChild(node.firstChild);
}

export function fmtTime(iso: string | Date): string {
  const d = typeof iso === "string" ? new Date(iso) : iso;
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function fmtClock(secs: number): string {
  const m = Math.floor(Math.max(0, secs) / 60);
  const s = Math.max(0, secs) % 60;
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

/* ---------- Toast ---------- */

export function toast(
  icon: string,
  text: string,
  ms = 3200,
  opts?: { level?: "info" | "warn" | "danger"; action?: { label: string; run: () => void } },
) {
  const root = document.getElementById("toast-root")!;
  // 最多同屏 4 条：最旧的先走
  while (root.children.length >= 4) root.firstElementChild?.remove();

  const item = el("div", { class: "toast" }, el("span", { class: "t-ico" }, icon));
  const level = opts?.level;
  if (level) item.dataset.level = level;
  item.append(el("span", {}, text));
  if (opts?.action) {
    item.append(el("button", {
      class: "btn-sm",
      style: "margin-left:8px",
      onclick: () => {
        item.remove();
        opts.action!.run();
      },
    }, opts.action.label));
  }
  root.append(item);
  setTimeout(() => {
    item.classList.add("leaving");
    setTimeout(() => item.remove(), 260);
  }, ms);
}

/* ---------- Modal shell ---------- */

type ModalCleanup = () => void;
let activeCleanup: ModalCleanup | null = null;

/** Close the open modal, if any. */
export function closeModal() {
  document.querySelector<HTMLButtonElement>(".modal-close")?.click();
}

export function openModal(
  icon: string,
  title: string,
  opts: { wide?: boolean } = {},
): { body: HTMLElement; foot: HTMLElement; close: () => void } {
  closeModal();

  const root = document.getElementById("modal-root")!;
  const backdrop = el("div", { class: "modal-backdrop" });
  const modal = el("div", { class: `modal${opts.wide ? " wide" : ""}` });
  const body = el("div", { class: "modal-body" });
  const foot = el("div", { class: "modal-foot" });

  const close = () => {
    backdrop.classList.remove("open");
    activeCleanup?.();
    activeCleanup = null;
    setTimeout(() => backdrop.remove(), 210);
  };

  const head = el(
    "div",
    { class: "modal-head" },
    el("span", { class: "m-ico" }, icon),
    el("h2", {}, title),
    el("button", { class: "modal-close", onclick: () => close() }, "✕"),
  );

  modal.append(head, body, foot);
  // 回车触发主按钮（输入框内回车不误触多行控件）
  modal.addEventListener("keydown", (e) => {
    if (e.key !== "Enter" || e.shiftKey) return;
    const tag = (e.target as HTMLElement).tagName;
    if (tag === "TEXTAREA" || tag === "SELECT") return;
    const primary = foot.querySelector<HTMLButtonElement>(".btn.primary, .btn.danger");
    if (primary && !primary.disabled) {
      e.preventDefault();
      primary.click();
    }
  });
  backdrop.append(modal);
  backdrop.addEventListener("click", (e) => {
    if (e.target === backdrop) close();
  });
  root.append(backdrop);
  requestAnimationFrame(() => backdrop.classList.add("open"));

  return { body, foot, close };
}

/** Register a cleanup (interval, subscription) for the open modal. */
export function onModalClose(fn: ModalCleanup) {
  activeCleanup = fn;
}

/* ---------- 菜单浮层（下拉 + 右键共用） ---------- */

export interface MenuItemDef {
  icon?: string;
  label: string;
  run: () => void;
  danger?: boolean;
  separator?: boolean;
}

function closeMenu() {
  document.querySelectorAll(".ctx-menu, .dropdown").forEach((m) => m.remove());
}

/** 在指定坐标弹出一个玻璃菜单，点击外部/Esc 关闭。 */
export function showMenuAt(x: number, y: number, items: MenuItemDef[]) {
  closeMenu();
  const menu = el("div", { class: "ctx-menu" });
  for (const item of items) {
    if (item.separator) {
      menu.append(el("div", { class: "ctx-sep" }));
      continue;
    }
    menu.append(
      el("button", {
        class: "ctx-item" + (item.danger ? " danger" : ""),
        onclick: () => {
          closeMenu();
          item.run();
        },
      }, item.icon ? el("span", { class: "ctx-ico" }, item.icon) : null, item.label),
    );
  }
  document.body.append(menu);
  const rect = menu.getBoundingClientRect();
  menu.style.left = Math.min(x, window.innerWidth - rect.width - 8) + "px";
  menu.style.top = Math.min(y, window.innerHeight - rect.height - 8) + "px";
  setTimeout(() => {
    document.addEventListener("mousedown", function dismiss(e) {
      if (!menu.contains(e.target as Node)) closeMenu();
      document.removeEventListener("mousedown", dismiss);
    });
    document.addEventListener("keydown", function esc(e) {
      if (e.key === "Escape") {
        closeMenu();
        document.removeEventListener("keydown", esc);
      }
    });
  });
}

/** Build a labelled form field. */
export function field(label: string, input: HTMLElement, hint?: string): HTMLElement {
  const wrap = el("div", { class: "field" }, el("label", {}, label), input);
  if (hint) wrap.append(el("div", { class: "hint" }, hint));
  return wrap;
}

/** Selectable option-card group. Returns the container; cards toggle .selected. */
export function optionCards(
  items: { title: string; desc: string; icon: string; value: string }[],
  onPick: (value: string) => void,
  initialPick?: string,
): HTMLElement {
  const grid = el("div", { class: "option-cards" });
  for (const item of items) {
    const card = el(
      "div",
      {
        class: `option-card${item.value === initialPick ? " selected" : ""}`,
        "data-value": item.value,
        onclick: () => {
          grid.querySelectorAll(".option-card").forEach((c) => c.classList.remove("selected"));
          card.classList.add("selected");
          onPick(item.value);
        },
      },
      el("div", { class: "oc-title" }, el("span", {}, item.icon), item.title),
      el("div", { class: "oc-desc" }, item.desc),
    );
    grid.append(card);
  }
  return grid;
}
