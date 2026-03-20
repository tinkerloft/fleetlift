# ADS Fix Rules

Non-negotiable rules for all Auto Debt Slayer execute steps. Violations block merge.

## Code Quality

- Match the style and conventions of the surrounding code.
- Do not introduce new abstractions unless the fix genuinely requires them.
- Prefer the simplest correct change over a clever one.
- Do not reformat unrelated code.

## Investigation Discipline

- Read the failing test or error before touching any code.
- Understand the root cause before writing a fix. A guess that passes tests is not a fix.
- If the scope of the fix extends beyond the ticket, stop and report it in the PR description.

## Safety

- **No auth or encryption changes.** If the fix touches authentication, authorisation, session handling, or cryptographic code, abandon the attempt and report it.
- **No data deletion.** Do not add or modify any code that deletes, truncates, or migrates production data.
- **No dependency upgrades** unless the ticket explicitly requests it.

## Testing

- Every behaviour change must be covered by at least one new or updated test.
- Tests must be deterministic — no time.Sleep, no reliance on external services, no random data without a fixed seed.
- Do not delete existing tests. If a test is wrong, fix it; do not remove it.

## Verification

- The build must pass: `go build ./...` (or the project-appropriate equivalent).
- All tests must pass: `go test ./...`.
- No orphaned exports: every new exported symbol must be used or tested.
- No type-safety regressions: do not introduce `any`, `interface{}`, or unchecked type assertions where the existing code used concrete types.

## Git

- **Do not commit or push.** The Fleetlift platform creates the commit and PR.
- Do not modify `.gitignore`, CI config, or any file outside the scope of the fix without explicit justification in the PR description.

## Review Readiness

- The PR description must explain what changed and why.
- If the fix is non-obvious, add an inline comment at the relevant line.
- Keep the diff small. If the fix requires more than ~200 lines of change, break it into logical parts and describe the split in the PR description.
