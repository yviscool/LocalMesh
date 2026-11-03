// 排座视图 — click an empty seat to assign the selected device.

import { store, normalizeGrid } from "../state";
import { clear, el, toast } from "../ui";
import { InteractionService } from "../mygo";

export function renderSeating() {
  const grid = document.getElementById("seating")!;
  const { seating, selected } = store.get();
  clear(grid);

  if (seating.length === 0) {
    grid.append(el("div", { class: "event-empty" }, "座位表加载中…"));
    return;
  }

  for (const row of seating) {
    for (const seat of row) {
      grid.append(seatCell(seat, selected));
    }
  }
}

function seatCell(seat: Seat, selected: Set<string>): HTMLElement {
  if (seat.occupied) {
    return el(
      "div",
      {
        class: "seat occupied",
        title: `${seat.student_name || seat.device_id} · 点击移出座位`,
        onclick: async () => {
          try {
            await InteractionService.unassignSeat(seat.row, seat.col);
            await refreshSeating();
            toast("🪑", `已将 ${seat.student_name || seat.device_id} 移出座位`);
          } catch (err) {
            toast("⚠️", `移出失败：${errMsg(err)}`);
          }
        },
      },
      el("span", { class: "seat-pos" }, `${seat.row + 1} 排 ${seat.col + 1} 列`),
      el("span", { class: "seat-name" }, seat.student_name || seat.device_id),
    );
  }

  const pending = selected.size > 0;
  return el(
    "div",
    {
      class: "seat",
      title: pending ? `将选中的 ${selected.size} 台设备分配到此座位` : "先在画面墙选中一台设备，再点击空座位",
      onclick: async () => {
        if (selected.size === 0) {
          toast("💡", "请先在画面墙点击选中一台学生机");
          return;
        }
        const deviceID = [...selected][0];
        if (!deviceID) return;
        try {
          await InteractionService.assignSeat(seat.row, seat.col, deviceID, deviceID);
          store.set({ selected: new Set() });
          await refreshSeating();
          toast("🪑", `${deviceID} 已安排到 ${seat.row + 1} 排 ${seat.col + 1} 列`);
        } catch (err) {
          toast("⚠️", `分配失败：${errMsg(err)}`);
        }
      },
    },
    el("span", { class: "seat-pos" }, `${seat.row + 1} 排 ${seat.col + 1} 列`),
    el("span", {}, pending ? "＋ 分配选中" : "空位"),
  );
}

export async function refreshSeating() {
  const grid = await InteractionService.seatingGrid();
  const flat = grid.flat();
  // ToGrid 总是返回完整矩阵，空位也有对象
  store.set({ seating: flat.length ? normalizeGrid(grid) : [] });
  renderSeating();
}

export function errMsg(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

type Seat = import("../state").Seat;
