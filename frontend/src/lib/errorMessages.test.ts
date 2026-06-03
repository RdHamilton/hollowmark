import { describe, it, expect } from "vitest";
import { friendlyErrorMessage } from "./errorMessages";

describe("friendlyErrorMessage", () => {
  it("returns default message for null input", () => {
    expect(friendlyErrorMessage(null)).toBe("Something went wrong. Please try again.");
  });

  it("returns default message for undefined input", () => {
    expect(friendlyErrorMessage(undefined)).toBe("Something went wrong. Please try again.");
  });

  it("returns default message for empty string", () => {
    expect(friendlyErrorMessage("")).toBe("Something went wrong. Please try again.");
  });

  describe("upstream_unavailable", () => {
    it('maps exact "upstream_unavailable" to friendly copy', () => {
      const result = friendlyErrorMessage("upstream_unavailable");
      expect(result).toContain("couldn't reach VaultMTG");
      expect(result).not.toBe("upstream_unavailable");
    });

    it('maps "upstream unavailable" case-insensitive', () => {
      const result = friendlyErrorMessage("upstream unavailable");
      expect(result).toContain("couldn't reach VaultMTG");
    });
  });

  describe("service unavailable", () => {
    it('maps "service unavailable" to friendly copy', () => {
      const result = friendlyErrorMessage("service unavailable");
      expect(result).toContain("couldn't reach VaultMTG");
      expect(result).not.toBe("service unavailable");
    });

    it("maps BFF response with service unavailable prefix", () => {
      const result = friendlyErrorMessage(
        "service unavailable: daemon registration not configured"
      );
      expect(result).toContain("couldn't reach VaultMTG");
    });

    it('maps "Service Unavailable" title-case to friendly copy', () => {
      const result = friendlyErrorMessage("Service Unavailable");
      expect(result).toContain("couldn't reach VaultMTG");
    });
  });

  describe("auth errors", () => {
    it('maps "No auth token" to sign-in prompt', () => {
      const result = friendlyErrorMessage("No auth token");
      expect(result).toContain("signed in");
    });

    it('maps "unauthorized" to session-expired message', () => {
      const result = friendlyErrorMessage("unauthorized");
      expect(result).toContain("session");
    });
  });

  describe("network errors", () => {
    it('maps "Request timeout" to friendly copy', () => {
      const result = friendlyErrorMessage("Request timeout");
      expect(result).toContain("too long");
    });

    it('maps "Failed to fetch" to friendly copy', () => {
      const result = friendlyErrorMessage("Failed to fetch");
      expect(result).toContain("connect");
    });

    it('maps "Network error" to friendly copy', () => {
      const result = friendlyErrorMessage("Network error");
      expect(result).toContain("network");
    });
  });

  describe("passthrough for unknown messages", () => {
    it("returns the original message when no pattern matches", () => {
      const msg = "Deck must contain at least 60 cards";
      expect(friendlyErrorMessage(msg)).toBe(msg);
    });

    it("returns the original message for validation errors", () => {
      const msg = "Invalid deck ID format";
      expect(friendlyErrorMessage(msg)).toBe(msg);
    });
  });
});
