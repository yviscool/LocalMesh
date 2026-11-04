import { describe, expect, it } from "vitest";
import {
  filterByGroup,
  fuzzyMatch,
  groupColor,
  groupHue,
  groupOfDevice,
  sortDevices,
  validateQuizDraft,
} from "./logic";

const devices = [
  { device_id: "PC-03", endpoint: "1.1.1.3:1", seen_at: "2026-01-01T00:00:03Z" },
  { device_id: "PC-01", endpoint: "1.1.1.1:1", seen_at: "2026-01-01T00:00:01Z" },
  { device_id: "PC-02", endpoint: "1.1.1.2:1", seen_at: "2026-01-01T00:00:02Z" },
];

describe("sortDevices", () => {
  it("sorts by name", () => {
    const out = sortDevices(devices, "name", () => null);
    expect(out.map((d) => d.device_id)).toEqual(["PC-01", "PC-02", "PC-03"]);
  });
  it("sorts by online (newest first)", () => {
    const out = sortDevices(devices, "online", () => null);
    expect(out.map((d) => d.device_id)).toEqual(["PC-03", "PC-02", "PC-01"]);
  });
  it("groups together with ungrouped last", () => {
    const out = sortDevices(
      [...devices, { ...devices[0], device_id: "ZZ-9" }],
      "group",
      (id) => (id === "PC-02" ? "g1" : null),
    );
    // 分组成员排最前，未分组（￿ 键）按名称排最后
    expect(out[0].device_id).toBe("PC-02");
    expect(out[out.length - 1].device_id).toBe("ZZ-9");
  });
  it("does not mutate the input", () => {
    const before = devices.map((d) => d.device_id);
    sortDevices(devices, "name", () => null);
    expect(devices.map((d) => d.device_id)).toEqual(before);
  });
});

describe("filterByGroup", () => {
  const membersOf = (gid: string) => (gid === "g1" ? ["PC-01", "PC-02"] : []);
  it("returns all when group is null", () => {
    expect(filterByGroup(devices, null, membersOf)).toHaveLength(3);
  });
  it("filters by membership", () => {
    const out = filterByGroup(devices, "g1", membersOf);
    expect(out.map((d) => d.device_id)).toEqual(["PC-01", "PC-02"]);
  });
});

describe("groupColor", () => {
  it("is stable per name", () => {
    expect(groupColor("一组")).toBe(groupColor("一组"));
  });
  it("differs across likely names", () => {
    const hues = ["一组", "二组", "三组", "四组", "五组"].map(groupHue);
    expect(new Set(hues).size).toBeGreaterThan(1);
  });
});

describe("groupOfDevice", () => {
  const groups = [
    { group_id: "g1", name: "一组", members: ["PC-01"] },
    { group_id: "g2", name: "二组", members: ["PC-02", "PC-03"] },
  ];
  it("finds the owning group", () => {
    expect(groupOfDevice("PC-02", groups)).toBe("g2");
  });
  it("returns null for ungrouped", () => {
    expect(groupOfDevice("PC-99", groups)).toBeNull();
  });
});

describe("fuzzyMatch", () => {
  it("matches subsequences case-insensitively", () => {
    expect(fuzzyMatch("lk", "锁定 Lock")).toBe(true);
    expect(fuzzyMatch("lz", "锁定 Lock")).toBe(false);
  });
});

describe("validateQuizDraft", () => {
  it("rejects empty", () => {
    expect(validateQuizDraft([]).ok).toBe(false);
  });
  it("rejects blank question text", () => {
    expect(validateQuizDraft([{ id: "q1", text: " ", type: "single_choice", options: ["a", "b"], correct_answers: [0], points: 1 }]).ok).toBe(false);
  });
  it("rejects missing correct answer", () => {
    expect(validateQuizDraft([{ id: "q1", text: "t", type: "single_choice", options: ["a", "b"], correct_answers: [], points: 1 }]).ok).toBe(false);
  });
  it("accepts valid text question without options", () => {
    expect(validateQuizDraft([{ id: "q1", text: "简答", type: "text", options: [], correct_answers: [], points: 5 }]).ok).toBe(true);
  });
});
