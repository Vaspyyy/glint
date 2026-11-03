<div align="center">

# ✦ glint

**`ls`, reimagined for humans *and* AI agents.**

Icons, git, previews, disk maps and project dashboards for you, plus a token-efficient repo map for your coding agent.<br>
One static binary, starts in ~4 ms.

[![Go](https://img.shields.io/github/go-mod/go-version/Vaspyyy/glint?style=flat-square&labelColor=1e1e2e&color=89b4fa&logo=go&logoColor=89b4fa)](go.mod)
[![Release](https://img.shields.io/github/v/tag/Vaspyyy/glint?style=flat-square&labelColor=1e1e2e&color=cba6f7&label=release)](https://github.com/Vaspyyy/glint/tags)
[![License](https://img.shields.io/github/license/Vaspyyy/glint?style=flat-square&labelColor=1e1e2e&color=a6e3a1)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-linux%20%C2%B7%20macOS-f9e2af?style=flat-square&labelColor=1e1e2e)](#install)

<img src="assets/hero.gif" alt="glint: grid, long and tree views" width="900">

[Install](#install) · [Views](#views) · [Interactive browser](#interactive-browser) · [Dashboard](#project-dashboard) · [Disk map](#disk-map) · [For AI agents](#for-ai-agents) · [Config](#configuration)

</div>

---

## Why glint?

`ls` tells you *names*. glint tells you **what you're looking at**:

- **Beautiful by default.** Nerd Font icons for 190+ file types and names, colored permissions, sizes that heat up as they grow, relative times that fade with age. Five built-in themes, or [your own](#configuration).
- **Git-aware everywhere.** Per-file status, folder rollups, branch / ahead / behind in the header, last commit and churn per file.
- **An interactive browser** with syntax-highlighted previews, **content search**, file actions, a project jumper and a mouse-driven **disk map**.
- **A dashboard for your projects folder**: every repo's stack, branch, uncommitted work, last commit and 12 weeks of activity on one screen.
- **Built for coding agents too.** `glint -A` answers *"what is this project, where's the code, what changed?"* in one call, at a fraction of the tokens of `tree`.
- **Fast.** Parallel `lstat`, one `git status` per listing, no heavy dependencies: the TUI, styling and highlighter are hand-rolled to keep startup at ~4 ms.

## Install

```sh
go install github.com/Vaspyyy/glint@latest
```

<details>
<summary><b>Requirements & optional extras</b></summary>

<br>

| | |
| --- | --- |
| **Go 1.26+** | to build. Older Go versions download the right toolchain automatically. |
| **A [Nerd Font](https://www.nerdfonts.com/)** | for icons. No Nerd Font? Run with `--icons never` or put `icons = "never"` in your config. |
| **[ripgrep](https://github.com/BurntSushi/ripgrep)** | for content search in the browser. |
| **`gio`** | for "move to trash" (ships with GNOME and KDE desktops). |
| **`ffprobe`** | optional: media durations in `--peek`. |

Developed on Linux. It builds for macOS and the BSDs too, but those are less tested (and "move to trash" needs `gio`). Windows isn't supported yet.

</details>

**Shell integration (fish):** completions, `gi` (browse, then `cd` to where you quit) and optional auto-listing on `cd`:

```fish
# ~/.config/fish/config.fish
glint --init fish --auto-ls | source
```

## Views

| Command | What you get |
| --- | --- |
| `glint` | Column grid with icons and colors |
| `glint -l` | Detailed table: permissions, size, relative time, git status |
| `glint -T` · `glint -lT` | Tree, or tree as a table (`-L 2` limits depth) |
| `glint -p` | **Peek**: what's inside each entry: project stack, line count, image size, media duration, archive contents, PDF pages… |
| `glint -lcH` | Last commit and git churn heat per entry |
| `glint -C` | Project card: stack, branch, language bar, lines of code, TODOs |
| `glint --thumbs` | Image thumbnails (kitty graphics protocol, or Unicode half-blocks anywhere) |
| `glint -W` | Live listing that updates as files change |
| `glint --json` | Everything, machine-readable |

<div align="center">
<img src="assets/thumbs.png" alt="glint --thumbs: an image thumbnail grid" width="820">
</div>

Filters work with every view: `-a` hidden files · `-D`/`-f` dirs/files only · `-I '*.tmp'` · `--gitignore` · `--changed` (only uncommitted files, great with `-T`) · `--since 2h`.

## Interactive browser

`glint -i` (or `gi` with the fish integration) opens a full-screen browser with live previews: syntax-highlighted code, rendered markdown, images, archive listings and hex dumps.

<div align="center">
<img src="assets/browser.gif" alt="glint -i: browsing, content search, project jumping and the disk map" width="900">
</div>

| Key | Action |
| --- | --- |
| `↑↓←→` / `hjkl` / mouse | Move, open folders, go up |
| `/` | Fuzzy-filter names |
| `f` · `ctrl+f` | **Search inside files** (ripgrep): live, grouped by file, preview around the hit; `enter` opens `$EDITOR` **at that line** |
| `P` · `ctrl+p` | **Jump** to a recent folder or any project next to the ones you've visited |
| `M` | **Disk map** of the current folder |
| `r` · `d` | Rename · move to trash (asks before deleting permanently where there's no trash) |
| `c` · `x` · `p` | Copy · cut · paste (clashes become `name (copy).ext`) |
| `n` · `N` | New file · new folder |
| `space` · `a` · `y` | Select · select all · copy paths to the clipboard (OSC 52) |
| `e` · `o` | Open in `$EDITOR` · open with the default app |
| `.` · `s` · `R` | Hidden files · cycle sort · reverse |
| `?` | All keys |

## Project dashboard

`glint -P ~/projects` gives you one row per project: stack, branch, uncommitted changes, last commit, a 12-week activity sparkline and source size, sorted by recent activity (`-S` for size, `-s name`).

<div align="center">
<img src="assets/dashboard.png" alt="glint -P: a dashboard of every project's git state and activity" width="900">
</div>

## Disk map

`glint -M` draws a treemap measured like `du` (allocated blocks, hardlinks counted once). Regenerable build output (`target/`, `node_modules/`, `.godot/`, `.venv/`, …) is **hatched**, and glint tells you how much you could free and how to get it back.

<div align="center">
<img src="assets/diskmap.png" alt="glint -M: a disk usage treemap with regenerable build output hatched" width="820">
</div>

`glint -M -i` makes it interactive: arrow keys or the mouse select a block, `enter` drills in, `⌫` goes up, `o` jumps to that spot in the browser.

## For AI agents

`glint -A` is a plain-text view designed for coding agents: **no colors, a predictable line budget, and the facts an agent would otherwise need four or five commands for.**

```console
$ glint -A aurora --budget 40
/home/ada/projects/aurora — Rust · workspace · git main 2 uncommitted; HEAD eb6f4a3 3h ago: Shader hot reload
24 files, 76K (what git sees: tracked + untracked, excluding ignored)
changed: [M] crates/aurora-render/src/gpu.rs, [new] crates/aurora-render/src/upscale.rs
gitignored (hidden): target/
largest source files (excl. vendored/generated): crates/aurora-core/src/math.rs 125L, crates/aurora-render/src/gpu.rs 103L, …

assets/ (4 files, 37K)
  textures/ (3 files, 14K; png×3)
    ↳ checker.png marble.png sunset.png
  logo.png 96x96 24K
crates/ (13 files, 19K; newest file 1m ago) [2 changed]
  aurora-core/ (5 files, 9.6K; Rust · aurora-core)
    src/ (4 files, 9.5K)
      camera.rs 53L
      lib.rs 101L
      math.rs 125L
…
```

```console
$ glint -A crates/aurora-render/src/gpu.rs
…/gpu.rs — 103L (last commit 2mo ago: Initial path tracer) [M], modified 1m ago
  3 pub struct Context
  8 pub fn create_device(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
  20 pub fn upload_scene(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
  32 pub fn dispatch(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
  44 pub fn readback(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
```

- **Budgeted tree.** Folders open by value until `--budget` lines (default 200): source and docs before media and vendored code, breadth before depth, recently active projects first.
- **Nothing silently missing.** The top level is always complete, empty folders are listed, and every cut says what it left out.
- **Folds the noise.** `frame_{001..240}.png ×240` becomes one line, and sibling folders with the same layout collapse.
- **Workspace-aware.** In a folder of repos every project gets a line with its stack and git state (`own git repo: main, 2 uncommitted, last commit 3h ago`).
- **Outlines.** `glint -A <file>` lists symbols with line numbers for Go, Rust, Python, JS/TS, C/C++, Java/Kotlin/C#, GDScript, shell, Lua, Ruby and Markdown.
- **More views.** `--changed`, `--since 2h`, `-c` (last commit per entry), `-l` (exact `ls -l`-style metadata) and `-M` (disk usage as text).

**Token cost** on a 4,000-file Rust workspace:

| Command | Tokens | Also tells you |
| --- | ---: | --- |
| `glint -A` | **~2,900** | stack, git state, changed files, line counts, what's ignored |
| `tree -L 2` | ~13,000 | – |
| `git ls-files` | ~50,000 | – |

<details>
<summary><b>Using glint with OpenCode, Claude Code & co.</b></summary>

<br>

Add a line to your agent's global instructions (e.g. `AGENTS.md`):

```md
- To explore directories, prefer `glint -A [dir]` over `ls`, `tree`, `find` or `git ls-files`.
  `glint -A <file>` lists a file's symbols with line numbers; `glint -A -l <dir>` gives an exact ls -l style listing.
```

</details>

## Configuration

Everything is optional. Create a starter config with `glint --init config`. Every key is a long flag name:

```toml
# ~/.config/glint/config.toml
theme = "midnight"
classify = true
hyperlink = true
ignore = ["__pycache__", "*.pyc"]

[themes.midnight]          # your own palette, based on a built-in one
extends = "tokyonight"
blue = "#7aa2f7"
purple = "#bb9af7"
```

Built-in themes: `catppuccin` (default) · `tokyonight` · `gruvbox` · `nord` · `wal` (follows [pywal](https://github.com/dylanaraps/pywal)). Extra default flags can also come from `GLINT_OPTS`.

<details>
<summary><b>All flags</b></summary>

<br>

Run `glint --help` for the full, colorful list. Highlights:

| Flag | |
| --- | --- |
| `-l` `-1` `-T` `-L n` | long, one per line, tree, depth |
| `-i` · `-P` · `-M` · `-A` | browser · dashboard · disk map · agent mode |
| `-a` `-D` `-f` `-d` `-I glob` | hidden, dirs only, files only, list dirs themselves, ignore |
| `-s name\|size\|time\|ext` `-t` `-S` `-X` `-r` | sorting |
| `-p` `-g` `-c` `-H` `--du` | peek, git, last commit, churn heat, folder sizes |
| `--since 2h` `--changed` `--gitignore` | filters |
| `--theme` `--icons` `--color` `-F` `--hyperlink` | appearance |
| `--json` `-W` `--thumbs` `-C` | JSON, watch, thumbnails, project card |

</details>

## How it works

<details>
<summary><b>Design notes</b></summary>

<br>

- **Startup is a feature.** `ls` runs constantly, so glint avoids dependencies with expensive `init()` work. That's why there's no Bubble Tea, Lip Gloss or Chroma: the TUI, the ANSI styler and the syntax highlighter are small and hand-written.
- **Git once per listing.** One `git status --porcelain` call, status rolled up into folders; history views stream a single `git log`.
- **Parallel I/O.** `lstat`, line counting, previews and disk measurement run on worker pools.
- **Thumbnails** reuse the freedesktop thumbnail cache your file manager already filled, and cache their own in `~/.cache/glint`.
- **The agent view was tuned by agents.** Its layout was reworked over several rounds of side-by-side evaluations with different models, each run against `ls`, `tree`, `git` and `du` on real workspaces.

```
internal/
  agent/    glint -A: budgeted map, outlines, disk usage as text
  cli/      flags, orchestration, watch mode, shell integration, help
  config/   config.toml and custom themes
  dash/     project dashboard scanner
  fsx/      entries, parallel lstat, sorting, trees, disk usage
  git/      status, history, tracked files
  icons/    Nerd Font glyphs and file kinds
  img/      decoding, half-blocks, kitty protocol, thumbnail cache
  peek/     content descriptions
  project/  stack detection and card stats
  render/   grid, long, tree, json, card, dashboard, treemap, thumbnails
  textw/    display width of styled text
  theme/    palettes, gradients, a tiny precomputed ANSI styler
  tui/      browser, search, file actions, jumper, disk map
```

</details>

## License

[GPL-3.0-or-later](LICENSE) © Vaspyyy
