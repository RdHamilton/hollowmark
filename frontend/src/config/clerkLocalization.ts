/**
 * Clerk localization override — COPPA/GDPR #884 hotfix.
 *
 * Overrides the built-in legal-consent checkbox label to add the required
 * COPPA age-gate affirmation: "I am 13 or older".
 *
 * The Clerk en-US default label for `signUp.legalConsent.checkbox
 * .label__termsOfServiceAndPrivacyPolicy` is:
 *   "I agree to the {{ termsOfServiceLink }} and {{ privacyPolicyLink }}"
 *
 * We prepend "I am 13 or older and " so the rendered text reads:
 *   "I am 13 or older and I agree to the <Terms of Service> and <Privacy Policy>"
 *
 * Template tokens {{ termsOfServiceLink }} and {{ privacyPolicyLink }} are
 * rendered by Clerk as clickable links to the URLs configured in the
 * Dashboard under Legal → Terms of Service URL and Privacy Policy URL.
 *
 * The localization prop accepts a DeepPartial — only the overridden keys need
 * to be present.  All other Clerk strings remain at their en-US defaults.
 *
 * Reference:
 *   node_modules/@clerk/localizations/dist/en-US.js
 *   signUp.legalConsent.checkbox.label__termsOfServiceAndPrivacyPolicy
 */

export const clerkLocalization = {
  signUp: {
    legalConsent: {
      checkbox: {
        label__termsOfServiceAndPrivacyPolicy:
          'I am 13 or older and I agree to the {{ termsOfServiceLink || link("Terms of Service") }} and {{ privacyPolicyLink || link("Privacy Policy") }}',
      },
    },
  },
} as const;
