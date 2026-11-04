// 危险操作二次确认弹层

import { el } from "../ui";

let active = false;

export function openConfirm(
  icon: string,
  title: string,
  message: string,
  onConfirm: () => void | Promise<void>,
) {
  if (active) return;
  active = true;

  const backdrop = el("div", { class: "modal-backdrop" });
  const modal = el("div", { class: "modal", style: "width:min(420px,88vw)" });
  const close = () => {
    active = false;
    backdrop.classList.remove("open");
    setTimeout(() => backdrop.remove(), 200);
  };

  modal.append(
    el("div", { class: "modal-head" },
      el("span", { class: "m-ico" }, icon),
      el("h2", {}, title),
      el("button", { class: "modal-close", onclick: () => close() }, "✕"),
    ),
    el("div", { class: "modal-body" },
      el("div", { class: "note danger" }, message),
    ),
    el("div", { class: "modal-foot" },
      el("button", { class: "btn", onclick: () => close() }, "取消"),
      el("button", {
        class: "btn danger",
        onclick: async () => {
          close();
          await onConfirm();
        },
      }, "确认执行"),
    ),
  );

  backdrop.append(modal);
  backdrop.addEventListener("mousedown", (e) => {
    if (e.target === backdrop) close();
  });
  document.getElementById("modal-root")!.append(backdrop);
  requestAnimationFrame(() => backdrop.classList.add("open"));
}
