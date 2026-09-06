import { describe, it, expect } from "vitest";
import { matchesPinyin } from "./pinyin-match";

describe("matchesPinyin", () => {
  it("matches full pinyin", () => {
    expect(matchesPinyin("李云龙", "liyunlong")).toBe(true);
  });

  it("matches pinyin initials", () => {
    expect(matchesPinyin("李云龙", "lyl")).toBe(true);
  });

  it("matches partial pinyin prefix", () => {
    expect(matchesPinyin("李云龙", "liyu")).toBe(true);
  });

  it("matches hybrid pinyin (full + initials)", () => {
    expect(matchesPinyin("李云龙", "liyunl")).toBe(true);
  });

  it("does not match unrelated query", () => {
    expect(matchesPinyin("李云龙", "zhangsan")).toBe(false);
  });

  it("returns false for non-Chinese names", () => {
    expect(matchesPinyin("Alice", "ali")).toBe(false);
  });

  it("returns true for empty query", () => {
    expect(matchesPinyin("李云龙", "")).toBe(true);
  });

  it("matches single character pinyin", () => {
    expect(matchesPinyin("张大彪", "z")).toBe(true);
    expect(matchesPinyin("张大彪", "zdb")).toBe(true);
    expect(matchesPinyin("张大彪", "zhangdabiao")).toBe(true);
  });

  it("matches mixed Chinese/English names", () => {
    expect(matchesPinyin("魏和尚", "whs")).toBe(true);
    expect(matchesPinyin("魏和尚", "weiheshang")).toBe(true);
  });

  it("normalizes ü to v for names like 吕布", () => {
    expect(matchesPinyin("吕布", "lvbu")).toBe(true);
    expect(matchesPinyin("吕布", "lb")).toBe(true);
    expect(matchesPinyin("吕布", "lv")).toBe(true);
  });

  describe("wildcard match", () => {
    it("matches suffix wildcard *", () => {
      expect(matchesPinyin("wild-agent-one", "wild*")).toBe(true);
      expect(matchesPinyin("wild-agent-one", "wild-agent*")).toBe(true);
      expect(matchesPinyin("wild-agent-one", "wild-agent-two*")).toBe(false);
    });

    it("matches prefix wildcard *", () => {
      expect(matchesPinyin("wild-agent-one", "*one")).toBe(true);
      expect(matchesPinyin("wild-agent-one", "*agent*")).toBe(true);
    });

    it("matches single character wildcard ?", () => {
      expect(matchesPinyin("wild-agent-one", "wild-agent-on?")).toBe(true);
      expect(matchesPinyin("wild-agent-one", "wild-agent-o?e")).toBe(true);
      expect(matchesPinyin("wild-agent-one", "wild-agent-?ne")).toBe(true);
      expect(matchesPinyin("wild-agent-one", "wild-agent-o??")).toBe(true);
      expect(matchesPinyin("wild-agent-one", "wild-agent-?")).toBe(false);
    });

    it("matches open-ended queries (e.g. *clau for autocomplete suggestions)", () => {
      expect(matchesPinyin("trondethi-hptom-claude", "*clau")).toBe(true);
      expect(matchesPinyin("trondethi-hptom-claude", "*claude")).toBe(true);
      expect(matchesPinyin("trondethi-hptom-claude", "trondethi-hptom-claud?")).toBe(true);
      expect(matchesPinyin("trondethi-hptom-claude", "trondethi-hptom-clau?")).toBe(false);
      expect(matchesPinyin("trondethi-hptom-claude", "*claudes")).toBe(false);
    });
  });
});
