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

export function toast(icon: string, text: string, ms = 3200) {
  const root = document.getElementById("toast-root")!;
  const item = el(
    "div",
    { class: "toast" },
    el("span", { class: "t-ico" }, icon),
    el("span", {}, text),
  );
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
