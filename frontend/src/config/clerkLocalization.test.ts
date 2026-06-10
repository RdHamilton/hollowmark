/**
 * Unit tests for clerkLocalization config — COPPA #884 hotfix.
 *
 * AC1: the legal-consent checkbox in the Clerk signup modal must read
 *   "I am 13 or older and I agree to the Terms of Service and Privacy Policy"
 *
 * These tests assert the localization override that is injected into
 * ClerkProvider via the `localization` prop.  They run under Vitest (jsdom)
 * and do NOT require a real Clerk publishable key.
 */

import { describe, it, expect } from 'vitest';
import { clerkLocalization } from './clerkLocalization';

describe('clerkLocalization — AC1: 13+ age affirmation in signup consent label', () => {
  it('exports a clerkLocalization object', () => {
    expect(clerkLocalization).toBeDefined();
    expect(typeof clerkLocalization).toBe('object');
  });

  it('overrides signUp.legalConsent.checkbox.label__termsOfServiceAndPrivacyPolicy', () => {
    const label =
      clerkLocalization?.signUp?.legalConsent?.checkbox
        ?.label__termsOfServiceAndPrivacyPolicy;
    expect(label).toBeDefined();
  });

  it('label contains "13 or older" age affirmation', () => {
    const label =
      clerkLocalization?.signUp?.legalConsent?.checkbox
        ?.label__termsOfServiceAndPrivacyPolicy as string;
    expect(label).toMatch(/13\s+or\s+older/i);
  });

  it('label contains Terms of Service reference', () => {
    const label =
      clerkLocalization?.signUp?.legalConsent?.checkbox
        ?.label__termsOfServiceAndPrivacyPolicy as string;
    expect(label).toMatch(/terms of service/i);
  });

  it('label contains Privacy Policy reference', () => {
    const label =
      clerkLocalization?.signUp?.legalConsent?.checkbox
        ?.label__termsOfServiceAndPrivacyPolicy as string;
    expect(label).toMatch(/privacy policy/i);
  });

  it('full label text starts with required AC1 wording', () => {
    const label =
      clerkLocalization?.signUp?.legalConsent?.checkbox
        ?.label__termsOfServiceAndPrivacyPolicy as string;
    // Required wording: "I am 13 or older and I agree to the Terms of Service and Privacy Policy"
    // Clerk template tokens ({{ termsOfServiceLink }}, {{ privacyPolicyLink }}) are acceptable
    // in the interpolated string; assert on the human-readable prefix only.
    expect(label).toMatch(/^I am 13 or older and I agree to/i);
  });
});
