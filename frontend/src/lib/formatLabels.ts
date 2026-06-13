/**
 * Human-readable label mapping for MTG Arena internal format slugs.
 *
 * Arena event IDs follow a few structural patterns:
 *   - <DraftType>_<SETCODE>_<YYYYMMDD>  e.g. QuickDraft_SOS_20260526
 *   - <FORMATKEYWORD>_<YYYYMMDD>        e.g. HISTORICBRAWLWITHALLOWLIST_20260126
 *   - Plain keywords                    e.g. Ladder, Play
 *
 * This util centralises all slug -> human-name mapping so components never
 * display a raw slug to a player.
 */

import { ARENA_SET_RELEASES } from "@/constants/arenaSetReleases";

const EXTRA_SET_NAMES: Record<string, string> = {
  SOS: "Shadows over Innistrad Remastered",
  TLA: "Avatar: The Last Airbender",
  LCI: "Lost Caverns of Ixalan",
  WOE: "Wilds of Eldraine",
  MAT: "March of the Machine: Aftermath",
  MOM: "March of the Machine",
  ONE: "Phyrexia: All Will Be One",
  GRN: "Guilds of Ravnica",
  RNA: "Ravnica Allegiance",
  WAR: "War of the Spark",
  ELD: "Throne of Eldraine",
  THB: "Theros Beyond Death",
  IKO: "Ikoria: Lair of Behemoths",
  ZNR: "Zendikar Rising",
  KHM: "Kaldheim",
  STX: "Strixhaven: School of Mages",
  AFR: "Adventures in the Forgotten Realms",
  MID: "Innistrad: Midnight Hunt",
  VOW: "Innistrad: Crimson Vow",
  NEO: "Kamigawa: Neon Dynasty",
  SNC: "Streets of New Capenna",
  DMU: "Dominaria United",
  BRO: "The Brothers' War",
};

function buildSetCodeMap(): Record<string, string> {
  const map: Record<string, string> = { ...EXTRA_SET_NAMES };
  for (const entry of ARENA_SET_RELEASES) {
    map[entry.code.toUpperCase()] = entry.name;
  }
  return map;
}

const SET_CODE_MAP = buildSetCodeMap();

const DRAFT_TYPE_LABELS: Record<string, string> = {
  QuickDraft: "Quick Draft",
  PremierDraft: "Premier Draft",
  TradDraft: "Traditional Draft",
  SealedDeck: "Sealed",
  // Emblem variants: Arena applies a Cascade Emblem game effect to the draft
  // while keeping the same pick-from-packs structure.  Label them distinctly
  // so players recognise the event, but the advisor logic treats them identically
  // to the base format (#1418 Defect A).
  QuickDraftEmblem: "Quick Draft (Cascade Emblem)",
  PremierDraftEmblem: "Premier Draft (Cascade Emblem)",
};

const FORMAT_KEYWORD_LABELS: Record<string, string> = {
  HISTORICBRAWLWITHALLOWLIST: "Historic Brawl",
  HistoricBrawlWithAllowlist: "Historic Brawl",
  historicbrawlwithallowlist: "Historic Brawl",
  HISTORICBRAWL: "Historic Brawl",
  HistoricBrawl: "Historic Brawl",
  historicbrawl: "Historic Brawl",
  HistoricBrawl_Play: "Play Queue",
  BRAWL: "Brawl",
  Brawl: "Brawl",
  STANDARD: "Standard",
  Standard: "Standard",
  HISTORIC: "Historic",
  Historic: "Historic",
  EXPLORER: "Explorer",
  Explorer: "Explorer",
  ALCHEMY: "Alchemy",
  Alchemy: "Alchemy",
  TIMELESS: "Timeless",
  Timeless: "Timeless",
  PIONEER: "Pioneer",
  Pioneer: "Pioneer",
  MODERN: "Modern",
  Modern: "Modern",
  LEGACY: "Legacy",
  Legacy: "Legacy",
  VINTAGE: "Vintage",
  Vintage: "Vintage",
  PAUPER: "Pauper",
  Pauper: "Pauper",
  GLADIATOR: "Gladiator",
  Gladiator: "Gladiator",
  Ladder: "Ranked",
  LADDER: "Ranked",
  Play: "Play Queue",
  PLAY: "Play Queue",
  Traditional_Ladder: "Traditional Ranked",
  Traditional_Play: "Traditional Play",
};

function stripDateSuffix(slug: string): string {
  return slug.replace(/_\d{8}$/, "");
}

/**
 * Convert a raw Arena format slug to a human-readable label.
 * Never returns a raw slug containing an 8-digit date or all-caps identifiers.
 */
export function humanizeFormatSlug(slug: string | null | undefined): string {
  if (!slug) return "";

  if (FORMAT_KEYWORD_LABELS[slug]) {
    return FORMAT_KEYWORD_LABELS[slug];
  }

  const draftTypeKeys = Object.keys(DRAFT_TYPE_LABELS);
  for (const prefix of draftTypeKeys) {
    if (slug.startsWith(prefix + "_")) {
      const rest = slug.slice(prefix.length + 1);
      const withoutDate = stripDateSuffix(rest);
      const setCode = withoutDate.split("_")[0].toUpperCase();
      const setName = SET_CODE_MAP[setCode];
      const draftLabel = DRAFT_TYPE_LABELS[prefix];
      if (setName) {
        return `${draftLabel} — ${setName}`;
      }
      return draftLabel;
    }
  }

  const withoutDate = stripDateSuffix(slug);
  if (withoutDate !== slug) {
    const keyword = withoutDate.split("_")[0];
    if (FORMAT_KEYWORD_LABELS[keyword]) {
      return FORMAT_KEYWORD_LABELS[keyword];
    }
    const keywordLower = keyword.toLowerCase();
    const ciMatch = Object.entries(FORMAT_KEYWORD_LABELS).find(
      ([k]) => k.toLowerCase() === keywordLower
    );
    if (ciMatch) {
      return ciMatch[1];
    }
  }

  const base = stripDateSuffix(slug);
  if (base.includes("_")) {
    const parts = base
      .split("_")
      .filter((p) => !/^\d+$/.test(p))
      .map((p) => p.charAt(0).toUpperCase() + p.slice(1).toLowerCase());
    return parts.join(" ");
  }

  return slug.charAt(0).toUpperCase() + slug.slice(1);
}

/**
 * Returns true when the given string looks like a raw Arena internal slug.
 */
export function isRawArenaSlug(value: string): boolean {
  if (/_\d{8}/.test(value)) return true;
  if (/^[A-Z]{5,}$/.test(value)) return true;
  return false;
}
