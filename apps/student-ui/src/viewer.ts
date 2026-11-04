// 全屏广播接收窗：<img> 直吃 agent 本地 MJPEG，Esc/双击退出

const img = document.getElementById("broadcast-img") as HTMLImageElement;
const tip = document.getElementById("viewer-tip")!;

// 学生 UI 与 agent 同机，本地端口直连
img.src = "http://127.0.0.1:7450/broadcast.mjpg";
img.style.display = "block";
tip.classList.add("hidden");

img.addEventListener("error", () => {
  img.classList.remove("live");
  tip.classList.remove("hidden");
  tip.textContent = "广播信号中断，等待恢复…";
});
img.addEventListener("load", () => {
  img.classList.add("live");
  tip.classList.add("hidden");
});

const exit = () => (window as unknown as { mygo?: { window: { close(): void } } }).mygo?.window.close();
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") exit();
});
img.addEventListener("dblclick", exit);
