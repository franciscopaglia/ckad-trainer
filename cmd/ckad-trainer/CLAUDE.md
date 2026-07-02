# cmd/ckad-trainer

Cobra CLI, single `main.go`. Conventions to preserve:

- **`errSilent`**: a command that already printed its failure in full (the
  FAIL summary of `check`, `doctor`'s closing line) returns `errSilent`, so
  `main` exits 1 without adding a duplicate `error:` line. Don't return plain
  errors for failures the command has already reported.
- **Completion is instant and cluster-free.** ValidArgsFunctions read only
  local state / the embedded catalog: `completeStartedIDs` (ids with live
  state — check/solution/solve/cleanup/reset/status) vs `completeStartableIDs`
  (non-flashcard, not-yet-started — start). Wire one of these on any new
  id-taking command; add `--domain` flags via `addDomainFlag` and validate
  values with `checkDomain` (lists the valid slugs on error).
- **Prompt AND hints are templates.** Always pair `engine.RenderPrompt` with
  `engine.RenderHints`; never print `s.Hints` raw — they contain `{{.ns}}`
  and param references.
- Unknown scenario ids get did-you-mean suggestions from `scenario.Find`;
  don't bypass it with manual catalog loops.
- Color only via the `green/red/yellow/bold/dim` helpers (auto-disabled when
  stdout is piped or `NO_COLOR`/`TERM=dumb` is set).
- `status` (list form) reconciles against the cluster for stale detection but
  degrades to local state when unreachable; `status <id>` must stay
  cluster-free (it re-shows the task instantly).
- Any command/flag change must be mirrored in `USAGE.md` §8 (command
  reference) in the same change.
