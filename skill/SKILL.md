---
name: glint
description: Explore directories and codebases token-efficiently with `glint -A` (budgeted repo map with project stack, git state, line counts) and `glint -A <file>` (symbol outline with line numbers). Use instead of ls, tree, find or git ls-files when orienting in a project, locating code, checking what changed, or finding what uses disk space.
---

# glint for agents

`glint -A` prints a plain-text map built for agents: no colors or padding, a predictable size, and the facts you'd otherwise need several commands for.

## Commands

| Goal | Command |
| --- | --- |
| Orient in a project (start here) | `glint -A` or `glint -A <dir>` |
| Open a collapsed folder from the map | `glint -A <dir>` |
| Bigger or smaller map | `glint -A --budget 400` / `--budget 80` (default 200 lines) |
| Only shallow levels | `glint -A -L2` |
| Symbols of one file, with line numbers | `glint -A path/to/file.rs` |
| Symbols inline in the map | `glint -A --outline <dir>` |
| Only uncommitted changes | `glint -A --changed` |
| Only recently modified files | `glint -A --since 2h` (30m, 3d, 1w, 6mo...) |
| Last commit per top-level entry | `glint -A -c` |
| What is using disk space | `glint -A -M <dir>` |
| Hide paths by name glob | `glint -A -I '*.snap' -I fixtures` |
| Exact listing like ls -l (mode, bytes, timestamp) plus line counts | `glint -A -l <dir>` |

Several paths can be passed at once: `glint -A src/ tests/ Cargo.toml`.

## Reading the output

```
/abs/path — Rust · my-crate · git main 2 uncommitted; HEAD a1b2c3d 3h ago: subject
412 files, 3.1M (what git sees: tracked + untracked, excluding ignored)   ← says what the numbers count
HEAD touched: src/parser/lexer.rs, src/lib.rs                            files of the last commit
changed: [M] src/lib.rs, [new] notes.md
gitignored (hidden): target/, .env, vendor/ (3 ignored inside)
largest source files (excl. vendored/generated): src/lib.rs 412L, src/parser/ast.rs 380L

src/ (120 files, 900K; newest file 3h ago) [2 changed]   expanded folder, with how many files inside changed
  parser/ (14 files, 80K; rs×14)               collapsed folder: counts and main file types
    ↳ ast.rs lexer.rs mod.rs tests/            glance: names of a collapsed folder's children
  lib.rs 412L [M]                              text file: line count, git status
  frame_{001..240}.png ×240 64x64 each, 2M total   numbered series folded into one line
  run1/ (6 files, 30K)                         first of a family of similar folders...
  run2/ … run9/ (8 more dirs like run1/; ...)  ...the rest folded
assets/ (300 files, 40M; png×290)              media folders are summarized, not listed
vendor/ (3 files, 4.4M; vendored/third-party)  not hand-written: ignore for "largest file" questions
old/ (empty)                                   empty folders are listed too
sub/ (no files listed: only .git)              exists, but nothing to list (with the reason)
big/ (…; PARTIAL: scan time limit hit, ...)    counts incomplete: run glint -A big/ for the truth
logo.png 512x512 24K                           image: dimensions and size
tool bin 2.1M exec                             binary, executable
CLAUDE.md -> AGENTS.md                         symlink
  … +51 files (json×40 md×11), +3 dirs: a/ b/ c/   cut from a long folder (folders are named)
… also: x/ y/ notes.txt                        top level only: every remaining name, nothing hidden
```

In a folder of projects (not itself a repo), each project is one line plus a glance:
```
hatrick/ (254 files, 53M; C · build.sh; own git repo: main, 2 uncommitted, last commit 38h ago; …)
  ↳ assets/ audio/ remake/ tas/ tools/ vendor/ audio.c boss.c build.sh …
```

- In a folder of projects, identical glances become `↳ same layout as aero-run/`. The up-to-3 projects changed in the last 7 days are opened further and marked `opened: recently active`, since they're likely what you'll work on next.
- Type summaries: `rs×12 toml×3` lists the most common types. `+5 more types` means one-off types were cut. `36 different file types` means no type repeats.
- Ages: folders always show their newest file. Top-level files show an age only if it's under 7 days. Empty folders show their own modification time.
- **Numbers are consistent:** a project shows the same file count and size from its parent folder as from inside. Both are "what git sees" (ignored files excluded). Build output outside git is skipped. For real disk usage use `glint -A -M`.
- **The top level is always complete.** Every entry appears, as a full line or in the final `… also:` line. Empty and seemingly empty folders are included and labelled. If a name isn't there, it doesn't exist (or it's gitignored, and then it's in the header).
- Deeper levels can be cut to fit the budget. Those lines always say what was left out.
- `NL` means N lines. Read big files (over about 800 lines) with an offset, after `glint -A <file>` has told you where to look.
- File git marks: `[M]` modified, `[A]` staged, `[D]` deleted, `[new]` untracked. Folders show `[N changed]`, or `[all new]` if everything inside is untracked. Repos show their own count (deletions included) in their line.
- In a subfolder of a repo the header says `N uncommitted here, M in the whole repo`. A gitignored folder says so and lists what's on disk.
- Folders are expanded by value: source and docs come before media, data dumps and vendored code, and breadth comes before depth. Open a collapsed one with `glint -A <dir>` rather than raising the budget a lot.

### Single file: `glint -A path/to/file`
Prints a header (the path relative to the repo root, lines, size, last commit, modified time, `VENDORED` if applicable), then one line per symbol: line number and signature (`1863 fn toast_label(kind: FrameType) -> Rgba`). When the signature doesn't show the symbol's name as is, the name comes first: `187 globSet.match  func (g globSet) match(p string) bool` (a method), `2 outer.inner  def inner():` (nested), `5 init#2  func init()` (second of two). C `#define`s go on one compact line at the end. Very large outlines are capped at 150 symbols. Works for Go, Rust, Python, JS/TS, C/C++, Java/Kotlin/C#, GDScript, shell, Lua, Ruby and Markdown headings. It's regex-based, so treat it as an index, not a parser.

### Refs
Header path + `:` + a symbol line's name is a ref that seek, recap and crux accept as is: `internal/files/files.go:globSet.match`. Forms: `path`, `path:Name` (qualified: `Repo.Open`, `outer.inner`), `path:12` (a line; `12:5`, `12-40`), `path:Name@rev` (as in a commit), `path:Name#2` (second of two definitions). `--outline` in the map uses the same names (`Name:line`).

### Disk: `glint -A -M <dir>`
Matches `du`: allocated blocks, hardlinks counted once, ignored files included. Gives the biggest entries and their biggest children, with build output marked `[regenerable]`. The header totals how much is regenerable (target/, node_modules/, .godot/, build/, …).

### Filters
- `--changed`: only uncommitted files. In a folder of repos it lists only the dirty repos, each followed by its changed file paths. Outside any git repo it refuses rather than guessing.
- `--since 2h`: only files modified in that window. Folders without matches are dropped.

## Tips

- One `glint -A` usually replaces `ls`, `tree`, `find`, `git status`, `git show --stat HEAD` and a `wc -l` sweep at the start of a task.
- After editing, `glint -A --changed` is a quick check of what you touched.
- For an exact listing of one folder use `glint -A -l <dir>`: mode, exact bytes and timestamps, like `ls -l`, plus line counts. glint reads everything below the folder, so it takes 10–200 ms instead of `ls`'s 3 ms.
- A bare `glint [paths]` with piped stdout (as in your tool calls) switches to agent mode by itself, so `-A` is optional there. Any other flag disables that, so keep `-A` when combining flags. `GLINT_PLAIN=1` gives the plain `ls`-style name list.
