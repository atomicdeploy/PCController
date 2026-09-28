## What changed

- replace the misleading disconnected-dashboard sentence with an explicit authentication requirement;
- link the disconnected hero directly to Settings, where the session access token is applied;
- keep English and Persian copy aligned;
- add a server-rendered UI contract test that requires the Settings link and rejects the old sentence;
- rebuild the embedded production Web assets.

## Why

The edge correctly requires authentication, but an unauthenticated browser still said the dashboard was already ready and linked back to itself. That made a healthy authenticated edge look like an older or broken deployment and gave the operator no next action.

## Validation

- `npm run typecheck`
- `npm test -- --run src/ui-contract.test.tsx` — 18/18 passed
- `npm run build` — 2,811 modules, embedded production bundle regenerated
- mobile runtime QA at 390×844 against the deployed PWA; navigation and preloader lifecycle passed and reproduced the stale copy fixed here
- `git diff --check`

Tracks #159 and completes the stale-copy deployment gate in #235. This PR contains no firmware or compatibility-parser change.
