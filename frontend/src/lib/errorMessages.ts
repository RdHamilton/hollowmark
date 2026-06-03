/**
 * Friendly error message mapping for known BFF error codes and messages.
 */

const ERROR_MAP: Array<{ pattern: RegExp | string; friendly: string }> = [
  {
    pattern: "upstream_unavailable",
    friendly: "We couldn't reach VaultMTG right now — make sure it's running and try again.",
  },
  {
    pattern: /service unavailable/i,
    friendly: "We couldn't reach VaultMTG right now — make sure it's running and try again.",
  },
  {
    pattern: /upstream.*unavailable/i,
    friendly: "We couldn't reach VaultMTG right now — make sure it's running and try again.",
  },
  {
    pattern: /no auth token/i,
    friendly: "You need to be signed in to view this. Please sign in and try again.",
  },
  {
    pattern: /unauthorized/i,
    friendly: "Your session has expired. Please sign in again.",
  },
  {
    pattern: /request timeout/i,
    friendly: "The request took too long. Check your connection and try again.",
  },
  {
    pattern: /network/i,
    friendly: "A network error occurred. Check your connection and try again.",
  },
  {
    pattern: /failed to fetch/i,
    friendly: "Could not connect to the server. Check your connection and try again.",
  },
  {
    pattern: /internal server error/i,
    friendly: "Something went wrong on our end. Please try again in a moment.",
  },
];

/**
 * Map a raw error message to a player-friendly string.
 */
export function friendlyErrorMessage(rawMessage: string | null | undefined): string {
  if (!rawMessage) return "Something went wrong. Please try again.";

  for (const entry of ERROR_MAP) {
    if (typeof entry.pattern === "string") {
      if (rawMessage.toLowerCase().includes(entry.pattern.toLowerCase())) {
        return entry.friendly;
      }
    } else {
      if (entry.pattern.test(rawMessage)) {
        return entry.friendly;
      }
    }
  }

  return rawMessage;
}
