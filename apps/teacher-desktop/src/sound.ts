// WebAudio 短音提示（可开关，localStorage 持久化）

let enabled: boolean = localStorage.getItem("lm.sound") !== "off";

export function soundEnabled(): boolean {
  return enabled;
}

export function setSoundEnabled(v: boolean) {
  enabled = v;
  localStorage.setItem("lm.sound", v ? "on" : "off");
}

type Cue = "hand" | "alert" | "offline";

const patterns: Record<Cue, { freq: number; dur: number; count: number }> = {
  hand: { freq: 880, dur: 0.09, count: 2 },
  alert: { freq: 520, dur: 0.15, count: 1 },
  offline: { freq: 300, dur: 0.2, count: 2 },
};

export function playCue(cue: Cue) {
  if (!enabled) return;
  try {
    const ctx = new AudioContext();
    const p = patterns[cue];
    for (let i = 0; i < p.count; i++) {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.value = p.freq;
      const t0 = ctx.currentTime + i * (p.dur + 0.06);
      gain.gain.setValueAtTime(0.12, t0);
      gain.gain.exponentialRampToValueAtTime(0.001, t0 + p.dur);
      osc.connect(gain).connect(ctx.destination);
      osc.start(t0);
      osc.stop(t0 + p.dur);
    }
    setTimeout(() => ctx.close(), (p.dur + 0.1) * p.count * 1000 + 200);
  } catch { /* audio unavailable */ }
}
