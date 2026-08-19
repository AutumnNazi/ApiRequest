# Contributing Guide

Thank you for contributing to ApiRequest.

This repository uses `dev` as the default integration branch, while stable releases are published from tags (`vX.Y.Z`). **Do not push directly to `dev`** — all changes land through pull requests.

[简体中文](CONTRIBUTING.zh-CN.md)

---

## Branch Model

- `dev`: default branch and day-to-day integration branch
- `release/*`: release preparation branches for maintainers
- `main`: stable release branch (cut from `release/*`)
- Recommended branch names for contributors:
  - `fix/*`: bug fixes
  - `feature/*`: new features or enhancements
  - `docs/*`: documentation-only changes

Maintainer release flow:

```text
feature/* / fix/* -> dev -> release/* -> main -> tag(vX.Y.Z)
```

---

## How to Open Pull Requests

Whether your branch is `fix/*` or `feature/*`, open pull requests **against `dev`**.

Reasons:

- `dev` is the active integration branch, so changes are reviewed in the same lane as ongoing work
- pushes to `dev` trigger the Dev Build workflow, so merged changes are validated continuously
- maintainers can cut `release/*` branches from `dev` without re-syncing external changes first

Recommended flow:

1. Fork this repository
2. Sync your fork with `dev` and create a branch from `dev` (`fix/*` or `feature/*`)
3. Make your changes and run self-checks (matching what CI runs):
   ```bash
   go vet ./backend/... ./cmd/...     # Go static analysis
   go test ./backend/... ./cmd/...    # Go tests
   node --test scripts                 # packaging script tests
   node scripts/check-docs.mjs        # doc links and bilingual parity
   npm --prefix frontend test -- --run  # frontend tests
   npm --prefix frontend run build      # type-check + bundle
   ```
4. Push the branch to your fork
5. Open a pull request against the `dev` branch of this repository

---

## Pull Request Requirements

Keep each pull request focused, reviewable, and easy to validate:

- one pull request should address one logical change
- use a clear title that explains the purpose
- include in the description:
  - background and problem statement
  - key changes
  - impact scope
  - validation method
- include screenshots or recordings for UI changes when helpful
- new backend behavior should come with tests; existing tests must stay green
- if you touch the Wails binding surface (exported methods on `backend/binding`), run `wails generate module` and commit the regenerated `frontend/wailsjs/` files

Commit messages follow the project convention: `emoji type(scope): 中文描述` (see `git log` for examples).

---

## Merge Strategy for Maintainers

Pull requests merged into `dev` should generally use **Squash and merge**:

- keeps `dev` history readable during active iteration
- maps each PR to a single integration commit on `dev`
- reduces cherry-pick and conflict cost before creating `release/*`

---

## Release Flow for Maintainers

1. Create `release/x.y.z` from `dev`; stabilize (version bumps, docs, fixes only)
2. Merge `release/x.y.z` into `main`
3. Tag `vX.Y.Z` on `main` — the Release workflow builds Windows / macOS / Linux artifacts and publishes a GitHub Release automatically
4. Merge `main` back into `dev` if release-only fixes were made

---

## Reporting Issues

Use GitHub Issues for bugs, feature requests, and docs. For technical reports, include version, OS, and reproduction steps when possible.
