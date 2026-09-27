## Secure-session onboarding fix

PR #245 replaces the misleading disconnected-dashboard sentence with an explicit authentication requirement, links the hero directly to Settings, aligns English/Persian copy, and adds a contract test that rejects the old sentence. It also rebuilds the embedded production assets.

Validated locally on the reconciled PR head:

- TypeScript typecheck: PASS
- focused UI contract: 18/18 PASS
- production Vite build: 2,811 modules PASS
- 390×844 deployed mobile reproduction: responsive navigation/preloader lifecycle PASS; old copy reproduced before the fix

This closes only the stale unauthenticated-copy/onboarding slice of #159. Live authenticated multi-client state, physical event propagation, and the remaining dashboard acceptance stay open.
