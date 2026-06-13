import { describe, it, expect } from "vitest";
import { humanizeFormatSlug, isRawArenaSlug } from "./formatLabels";

describe("formatLabels", () => {
  describe("humanizeFormatSlug", () => {
    it("returns empty string for null", () => {
      expect(humanizeFormatSlug(null)).toBe("");
    });

    it("returns empty string for undefined", () => {
      expect(humanizeFormatSlug(undefined)).toBe("");
    });

    it("returns empty string for empty string", () => {
      expect(humanizeFormatSlug("")).toBe("");
    });

    it("maps QuickDraft_SOS_20260526 to Quick Draft with set name", () => {
      const result = humanizeFormatSlug("QuickDraft_SOS_20260526");
      expect(result).toContain("Quick Draft");
      expect(result).toContain("Shadows over Innistrad Remastered");
    });

    it("maps PremierDraft_BLB_20240730 to Premier Draft with set name", () => {
      const result = humanizeFormatSlug("PremierDraft_BLB_20240730");
      expect(result).toContain("Premier Draft");
      expect(result).toContain("Bloomburrow");
    });

    it("maps QuickDraft_DSK_20240924 to Quick Draft with Duskmourn", () => {
      const result = humanizeFormatSlug("QuickDraft_DSK_20240924");
      expect(result).toContain("Quick Draft");
      expect(result).toContain("Duskmourn");
    });

    it("maps TradDraft_MKM to Traditional Draft", () => {
      const result = humanizeFormatSlug("TradDraft_MKM_20240206");
      expect(result).toContain("Traditional Draft");
    });

    it("maps SealedDeck_BLB to Sealed", () => {
      const result = humanizeFormatSlug("SealedDeck_BLB_20240730");
      expect(result).toContain("Sealed");
      expect(result).toContain("Bloomburrow");
    });

    it("falls back to draft type label when set code is unknown", () => {
      expect(humanizeFormatSlug("QuickDraft_XYZ_20260101")).toBe("Quick Draft");
    });

    it("maps HISTORICBRAWLWITHALLOWLIST_20260126 to Historic Brawl", () => {
      expect(humanizeFormatSlug("HISTORICBRAWLWITHALLOWLIST_20260126")).toBe("Historic Brawl");
    });

    it("maps HistoricBrawlWithAllowlist to Historic Brawl", () => {
      expect(humanizeFormatSlug("HistoricBrawlWithAllowlist")).toBe("Historic Brawl");
    });

    it("maps HistoricBrawl to Historic Brawl", () => {
      expect(humanizeFormatSlug("HistoricBrawl")).toBe("Historic Brawl");
    });

    it("maps Ladder to Ranked", () => {
      expect(humanizeFormatSlug("Ladder")).toBe("Ranked");
    });

    it("maps Play to Play Queue", () => {
      expect(humanizeFormatSlug("Play")).toBe("Play Queue");
    });

    it("maps Standard to Standard", () => {
      expect(humanizeFormatSlug("Standard")).toBe("Standard");
    });

    it("maps Historic to Historic", () => {
      expect(humanizeFormatSlug("Historic")).toBe("Historic");
    });

    // ── Emblem draft types (#1418 Defect A) ─────────────────────────────────
    it("maps QuickDraftEmblem_STX_20260601 to Quick Draft (Cascade Emblem) with set name", () => {
      const result = humanizeFormatSlug("QuickDraftEmblem_STX_20260601");
      expect(result).toContain("Quick Draft (Cascade Emblem)");
      expect(result).toContain("Strixhaven");
    });

    it("maps PremierDraftEmblem_STX_20260601 to Premier Draft (Cascade Emblem) with set name", () => {
      const result = humanizeFormatSlug("PremierDraftEmblem_STX_20260601");
      expect(result).toContain("Premier Draft (Cascade Emblem)");
      expect(result).toContain("Strixhaven");
    });

    it("falls back to label-only when set code is unknown for QuickDraftEmblem", () => {
      expect(humanizeFormatSlug("QuickDraftEmblem_XYZ_20260101")).toBe("Quick Draft (Cascade Emblem)");
    });

    it("never returns a raw slug with an 8-digit date", () => {
      const inputs = [
        "QuickDraft_SOS_20260526",
        "PremierDraft_BLB_20240730",
        "TradDraft_DSK_20240924",
        "HISTORICBRAWLWITHALLOWLIST_20260126",
        "SealedDeck_OTJ_20240416",
        "QuickDraftEmblem_STX_20260601",
        "PremierDraftEmblem_STX_20260601",
      ];
      for (const input of inputs) {
        const result = humanizeFormatSlug(input);
        expect(result, `${input} should not contain a raw date`).not.toMatch(/_\d{8}/);
      }
    });

    it("never returns an all-uppercase raw identifier", () => {
      const inputs = [
        "HISTORICBRAWLWITHALLOWLIST_20260126",
        "HISTORICBRAWL",
      ];
      for (const input of inputs) {
        const result = humanizeFormatSlug(input);
        expect(result, `${input} should not be all-caps`).not.toMatch(/^[A-Z]{5,}$/);
      }
    });
  });

  describe("isRawArenaSlug", () => {
    it("identifies slugs with 8-digit date segments as raw", () => {
      expect(isRawArenaSlug("QuickDraft_SOS_20260526")).toBe(true);
    });

    it("identifies all-uppercase identifiers longer than 4 chars as raw", () => {
      expect(isRawArenaSlug("HISTORICBRAWL")).toBe(true);
    });

    it("does not flag human-readable labels as raw", () => {
      expect(isRawArenaSlug("Quick Draft")).toBe(false);
      expect(isRawArenaSlug("Historic Brawl")).toBe(false);
      expect(isRawArenaSlug("Ranked")).toBe(false);
    });

    it("does not flag normal short set codes as raw", () => {
      expect(isRawArenaSlug("DSK")).toBe(false);
      expect(isRawArenaSlug("BLB")).toBe(false);
    });
  });
});
