---
name: run-stage
description: The orchestrating (main) session's procedure for executing one stage of the plan, from reading the stage to reporting it; use it when asked to run, start or continue a plan stage. Only for the main session — a subagent following a brief must not use it (it implements directly and never spawns further agents).
---

# Running one stage of the plan

The plan is `C:\Users\Eric\.gemini\antigravity\brain\de77fa0c-5fcc-4f70-b8fd-8c17e7a03ef2\plan.md`. This skill is for the
session that talks to the user. **A subagent never uses it:** it does the brief it was given, implements directly, and
spawns no agents.

## Rules of the session

- **The main session plans, briefs, merges and reports. Every implementation task goes to a subagent.**
- **Model:** `sonnet` by default. `opus` only for design-heavy or subtle work: the scheduled front (plan 1.2a), porting
  the checker's ownership and range analysis, or debugging a failure whose cause is not obvious.
- **Parallel subagents that edit files get their own git worktree only when the state they need is committed.** A
  worktree starts from HEAD, so it lacks every uncommitted or untracked file (`self/`, `cmd/`, earlier stages' work), and
  commits happen only when the user asks. Otherwise run the tasks sequentially in the main tree, or give each parallel
  subagent disjoint paths in the main tree. Tightly coupled sequential edits go to one subagent in the main tree.
- **Workflow scripts** (multi-agent) only when the user asked for a workflow; otherwise the Agent tool.
- **Context control.** The main session reads no large file and no dump: maxon's `Parser*.maxon` files are ~600 KB and
  `docs/LANGUAGE_REFERENCE.md` ~420 KB; search them with Grep and read line ranges. Each subagent gets one narrow task,
  file paths (never pasted contents), reads big files only by search and line ranges, passes large intermediate results
  through scratch files, and returns a short summary. A fresh subagent per task; do not reuse one across tasks.
- **A repetitive multi-file change is made by a tool first**: plain Go in `cmd/<name>/` (as `cmd/convert` is), run
  once, reviewed by its diff. Hand edits across many files are a sign the tool is missing.
- **Open decisions go to the user**, batched in one message, the recommended option first, before delegating any work
  that depends on them.

## Decisions that hold (do not re-argue)

- The Go compiler implements only what `self/` needs (`C:\Users\Eric\Dev\minimaxon\self`). Once `self/` self-hosts,
  compiler work in Go ends. `cmd/convert` and `internal/grammargen` stay Go tools until the cutover.
- Compatible with current maxon (`C:\Users\Eric\Dev\maxon`: `maxon-bin/`, `stdlib/`, `specs/`, `docs/`) except deliberate
  minimaxon changes, recorded in README "Where it differs from Maxon". Where maxon code is not converted it is still
  the reference for behaviour, edge cases and names.
- Maxon code and tests are reused through `cmd/convert`; converted files are regenerated, never hand-edited.
- Byte-identical output is never a goal. Gates are functional: the suites pass with 0 leaks.
- Testing is minimal (see "Before a commit"). A test is added only when a bug would otherwise come back or nothing in
  the corpus covers a feature. No test-first rule.

## Procedure

1. **Read the stage.** Find its heading in the plan (`Grep "^### Stage"`) and read only that section, plus the sections
   it names and the decisions it depends on (plan section 1 "Core Principle", the generator table). List what is
   undecided and ask the user, batched, before any delegation that depends on it.
2. **Scout cheaply.** Build the work list with Grep and Glob over the paths the stage names, or one Explore subagent
   that writes its findings to a scratch file. Do not read big files.
3. **Split into narrow tasks; decide order and parallelism.** A task is one goal with a path list a subagent can finish
   alone. Independent tasks that touch different files run in parallel (worktrees per the rule above). Tasks that edit the
   same files, or where one needs the other's output, are sequential or one subagent. Put a generator or conversion tool
   task before the tasks that use its output. Choose the model per task (rules above).
4. **Brief each task** with the template below. Name the skills the brief should use: `language-change` (exists) for any
   grammar, syntax or diagnostic change and `convert-from-maxon` (exists) for pulling code or tests from maxon;
   `port-to-self` is planned (no plan stage names it), so give its steps in the brief itself.
5. **Run, monitor and merge.** Start subagents, wait for their short reports, and monitor them (next section) after
   each round, before merging. Subagents in a worktree leave their changes
   uncommitted; transfer each in source order with `git add -N .` then `git diff | git apply` run from the worktree
   into the main tree (`-N` makes new files, such as `self/` files and `cmd/<name>/` tools, appear in the diff), resolve
   conflicts, then `git worktree remove --force`. Then run the checks once, on the merged tree. A failure goes to a fresh subagent with
   the failing output saved to a scratch file; `opus` when the cause is not obvious.
6. **Update the docs.** `README.md` for what the stage changed (subset, commands and flags, packages, pipeline, IR,
   "Where it differs from Maxon"), and the stage's status in the plan, in its heading as the existing ones do
   (for example `— implemented, uncommitted`).
7. **Report to the user and stop.** Say what landed, what the suites showed, what is left or was decided on the way.
   Do not commit unless asked; when asked, commit directly on `main`.

## Monitoring subagents

A standing rule, applied after every task or workflow round and before merging.

- **Check the cheap signals.** Tokens, tool calls and duration (task notification or workflow journal) against what the
  task should take. Read each short report for retries, failed builds and "could not".
- **Counting tool calls from a transcript** (the session's `subagents/` directory: `agent-*.jsonl`, with a `.meta.json`
  naming the task): every `tool_use` appears twice in the JSONL (a streamed row and a final row), so count distinct
  `tool_use` ids.
- **Every subagent starts at about 44k tokens of injected context.** A task that is too small is mostly overhead; do
  not split below a meaningful unit of work.
- **For an outlier, spawn a Sonnet auditor.** It reads the transcript by Grep and line ranges only (never whole:
  transcripts are 400-800 KB) and returns the causes ranked by tokens wasted, each with a fix, in under ~250 words. The
  main session never reads transcripts itself.
- **Fold each fix in before the next brief.** A repo fact (a command, a path, a pattern minimaxon refuses) goes into
  the skill that covers it (`port-to-self`, `convert-from-maxon`, `language-change`); a briefing lesson goes into the
  brief template or the pitfalls below.

## Brief pitfalls (from the first audit)

- Give exact line ranges for every plan or README section the task needs, or paste a short digest of what matters;
  never "read section X" alone.
- Name the specific maxon files that are reuse candidates, never "check maxon-bin", and cap exploration (how many
  files, which repos).
- State the working directory, the shell (PowerShell is primary; Bash is Git Bash) and that Go is at
  `C:\Program Files\Go\bin` if not on `PATH`, so agents do not repeat `cd` or rediscover.
- State the standard facts: the `self/` packages that exist (`ast`, `check`, `diag`, `project`, `resolve`, `syntax`,
  `table`, plus `self/main.maxon`; re-list with Glob before briefing), the gates (`.\check.ps1`, and `.\minimaxon.exe
  check self`, which compiles `self` without writing an executable and exits 0 silently when clean), and that the Write
  tool creates missing directories.
- Ask agents to prefer Read with offset/limit and Grep over `cat`/`sed`/`grep` through the shell. An auto-mode reminder
  tells them otherwise; following it works but costs more output.

## Before a commit

Only these, from the repository root (Go is at `C:\Program Files\Go\bin` if not on `PATH`):

```powershell
go build -o minimaxon.exe . ; go vet ./... ; gofmt -l . ; go fix -diff ./... ; go test ./... ; .\minimaxon.exe test
```

`gofmt -l .` and `go fix -diff ./...` must print nothing (AGENTS.md requires both). `.\check.ps1` is the shortcut: it
runs `gofmt -w` and `go fix ./...`, which modify files, then vet, unit tests, build and the corpus; use it to save a
round. Both suites green with 0 leaks. AGENTS.md's other rules still apply.

## Brief template

Copy, fill every field, keep it under ~40 lines. Paths, not contents; apply "Brief pitfalls" above.

```text
Goal: <one sentence>. Plan stage: <number and title in plan.md>; read only that section.
Files to change: <paths or globs>.
Read (by Grep and line ranges; the large ones are never read whole):
  - minimaxon: <paths>
  - maxon reference: <paths under C:\Users\Eric\Dev\maxon>
Decisions that apply: <the relevant ones, quoted short>. Do not re-argue them.
Skills to use: <language-change | port-to-self | convert-from-maxon | none>.
Rules: AGENTS.md applies (no workarounds, no silent unhandled cases, no magic values, comments say why). Converted
  files are regenerated, never hand-edited. Repetitive changes across many files: write a tool in cmd/<name>/ first.
Checks when done: go build -o minimaxon.exe . ; go vet ./... ; gofmt -l . ; go fix -diff ./... ; go test ./... ;
  .\minimaxon.exe test (or .\check.ps1). Do not commit.
Context: implement directly; do not spawn agents and do not use the run-stage skill. Read big files only by search
  and line ranges. Keep large intermediate output in <scratch path>, not in your reply.
Worktree: <path, changes left uncommitted; or "main tree" and the disjoint paths you own>.
Return: at most 8 lines: what changed (paths), check results, anything unverified or left undone. No file contents.
```

## Worktrees on this machine

The Agent tool's `isolation: "worktree"` is refused here (it creates `.claude/worktrees/agent-<id>` and then rejects
it over the drive letter's case). Make the worktree yourself and brief the agent with its path instead:

```powershell
git worktree add --detach .claude/worktrees/<task> HEAD
```

- The brief names the worktree's absolute path as the agent's tree, says every command starts with `cd` into it and
  every Read/Edit/Write path lies under it, and that the main tree must not be touched.
- `.claude/worktrees/` is in `.git/info/exclude`, so the main tree's `git status` and Grep do not see it.
- After merging: `git worktree remove --force .claude/worktrees/<task>` (`git worktree unlock` first for one the
  harness created).
