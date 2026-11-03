<div align="center">

# glint

A modern `ls` for people and for AI coding agents.

[![Go](https://img.shields.io/github/go-mod/go-version/Vaspyyy/glint?style=flat-square&labelColor=1e1e2e&color=89b4fa&logo=go&logoColor=89b4fa)](go.mod)
[![Release](https://img.shields.io/github/v/tag/Vaspyyy/glint?style=flat-square&labelColor=1e1e2e&color=cba6f7&label=release)](https://github.com/Vaspyyy/glint/tags)
[![License](https://img.shields.io/github/license/Vaspyyy/glint?style=flat-square&labelColor=1e1e2e&color=a6e3a1)](LICENSE)

<img src="assets/hero.gif" alt="glint showing a grid, a long listing and a tree" width="900">

</div>

glint lists directories with icons, colors and git status, and goes a bit further than `ls` where that's useful: it can preview files, search their contents, show how a folder of projects is doing, and draw a map of what's using your disk. It also has a plain-text mode for coding agents that describes a repository in one call, using far fewer tokens than `tree`.

It's a single Go binary and starts in about 4 ms, so it's fine to alias over `ls`.

## Install

```sh
go install github.com/Vaspyyy/glint@latest
```

You'll need Go 1.26 or newer (older versions fetch the right toolchain on their own). Icons need a [Nerd Font](https://www.nerdfonts.com/); without one, use `--icons never`. Content search in the browser uses [ripgrep](https://github.com/BurntSushi/ripgrep), moving files to the trash uses `gio`, and media durations in `--peek` come from `ffprobe` if it's installed.

glint is developed on Linux. It also builds on macOS and the BSDs, but gets less testing there. Windows isn't supported yet.

For fish, this adds completions, a `gi` command that browses and then `cd`s to wherever you quit, and an optional listing after every `cd`:

```fish
glint --init fish --auto-ls | source    # in ~/.config/fish/config.fish
```

## Listing files

Plain `glint` prints a grid. The other views:

| Command | View |
| --- | --- |
| `glint -l` | Long listing with permissions, sizes, relative times and git status |
| `glint -T`, `glint -lT` | Tree, or a tree with the long columns (`-L 2` limits the depth) |
| `glint -p` | Adds a short description of each entry: the project type of a folder, the line count of a text file, image dimensions, media duration, archive contents |
| `glint -lcH` | Adds the last commit touching each entry and how often it changes |
| `glint -C` | A summary card for a project: stack, branch, languages, lines of code, TODOs |
| `glint --thumbs` | Image thumbnails, using the kitty graphics protocol or half-block characters |
| `glint -W` | Keeps the listing open and updates it as files change |
| `glint --json` | Machine-readable output |

The usual filters work with every view: `-a` for hidden files, `-D` and `-f` for only folders or only files, `-I '*.tmp'` to hide by pattern, `--gitignore`, `--changed` for files with uncommitted changes, and `--since 2h` for recently modified ones. Sorting is natural (`file2` before `file10`) and accent-aware; `-t`, `-S` and `-X` sort by time, size and extension.

<div align="center">
<img src="assets/thumbs.png" alt="glint --thumbs showing image thumbnails" width="820">
</div>

## Interactive browser

`glint -i` opens a full-screen browser with a preview pane. Code is syntax highlighted, markdown is styled, and images, archives and binary files get previews of their own.

<div align="center">
<img src="assets/browser.gif" alt="Browsing, searching file contents, jumping to another project and opening the disk map" width="900">
</div>

From the browser you can search inside files (ripgrep runs as you type, and Enter opens your editor at the matching line), jump to recently visited folders and to other projects, open a disk map of the current folder, and do the usual file operations. Deleting moves things to the trash, and asks before deleting permanently on drives that don't have one.

| Key | Action |
| --- | --- |
| arrows, `hjkl`, mouse | Move, open folders, go up |
| `/` | Filter names |
| `f`, `ctrl+f` | Search inside files |
| `P`, `ctrl+p` | Jump to a recent folder or project |
| `M` | Disk map of this folder |
| `r`, `d` | Rename, move to trash |
| `c`, `x`, `p` | Copy, cut, paste |
| `n`, `N` | New file, new folder |
| `space`, `a`, `y` | Select, select all, copy paths to the clipboard |
| `e`, `o` | Open in `$EDITOR`, open with the default app |
| `.`, `s`, `R` | Hidden files, sort order, reverse |
| `?` | Show all keys |

## Project dashboard

If you keep your projects in one folder, `glint -P` shows them side by side: the stack, the branch, uncommitted changes, the last commit, commit activity over the last twelve weeks, and the size of the source. Projects are sorted by recent activity; `-S` sorts by size and `-s name` by name.

<div align="center">
<img src="assets/dashboard.png" alt="glint -P showing a dashboard of projects" width="900">
</div>

## Disk map

`glint -M` draws a treemap of disk usage, measured the same way `du` measures it. Build output and caches that can be regenerated, like `target/`, `node_modules/`, `.godot/` or `.venv/`, are hatched, and a short summary below the map says how much space they take and how to get them back.

<div align="center">
<img src="assets/diskmap.png" alt="glint -M showing a disk usage treemap" width="820">
</div>

`glint -M -i` opens the same map interactively. Select a block with the arrow keys or the mouse, press Enter to look inside it, Backspace to go up, and `o` to open that folder in the browser.

## For coding agents

A ready-made Claude Code skill is in [`skill/SKILL.md`](skill/SKILL.md). Install it with `mkdir -p ~/.claude/skills/glint && cp skill/SKILL.md ~/.claude/skills/glint/`.

`glint -A` prints a plain-text description of a directory that's meant to be read by an AI agent. The first lines say what the project is, what state git is in, what changed and what's ignored. After that comes a tree that fits a line budget (200 lines by default, set with `--budget`).

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

Folders with source code and documentation are expanded before media, generated data and vendored code, and shallow folders before deep ones. Runs of similar files are folded into one line, and the top level is always listed completely, so an agent can tell when something doesn't exist. In a folder that contains several repositories, each one gets a line with its stack and git state.

Given a file, `glint -A` lists its functions and types with line numbers, so an agent can read just the part it needs. This works for Go, Rust, Python, JavaScript and TypeScript, C and C++, Java, Kotlin, C#, GDScript, shell, Lua, Ruby and Markdown headings.

```console
$ glint -A crates/aurora-render/src/gpu.rs
…/gpu.rs — 103L (last commit 2mo ago: Initial path tracer) [M], modified 1m ago
  3 pub struct Context
  8 pub fn create_device(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
  20 pub fn upload_scene(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
  32 pub fn dispatch(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
  44 pub fn readback(ctx: &mut Context, input: &[f32]) -> Result<f32, Error>
```

On a Rust workspace with about 4,000 files, `glint -A` came to roughly 2,900 tokens. For comparison, `tree -L 2` was about 13,000 and `git ls-files` about 50,000, and neither includes git state or line counts. The format was refined over several rounds of comparisons in which different models used glint alongside `ls`, `tree`, `git` and `du` on real projects.

To have your agent use it, add something like this to its instructions (for example `AGENTS.md`):

```md
To explore directories, prefer `glint -A [dir]` over `ls`, `tree`, `find` or `git ls-files`.
`glint -A <file>` lists a file's symbols with line numbers.
```

## Configuration

glint reads `~/.config/glint/config.toml` if it exists; `glint --init config` writes a commented starting point. Each key is the long name of a flag, and `[themes.NAME]` sections define color schemes:

```toml
theme = "midnight"
classify = true
ignore = ["__pycache__", "*.pyc"]

[themes.midnight]
extends = "tokyonight"
blue = "#7aa2f7"
purple = "#bb9af7"
```

The built-in themes are `catppuccin` (the default), `tokyonight`, `gruvbox`, `nord`, and `wal`, which follows [pywal](https://github.com/dylanaraps/pywal). Default flags can also be set in the `GLINT_OPTS` environment variable. `glint --help` lists every option.

## How it works

Most of glint's speed comes from doing little at startup. It avoids dependencies with expensive initialization, which is why the terminal UI, the color handling and the syntax highlighter are written by hand rather than using Bubble Tea, Lip Gloss or Chroma. Each listing runs `git status` once and derives folder status from it, file metadata is read in parallel, and thumbnails reuse the cache your file manager has already built.

```
internal/
  agent/    glint -A
  cli/      flags, help, watch mode, shell integration
  config/   config file and custom themes
  dash/     project dashboard
  fsx/      reading, sorting and measuring files
  git/      status and history
  icons/    file icons and types
  img/      image decoding and thumbnails
  peek/     short descriptions of file contents
  project/  project type detection
  render/   the different views
  textw/    text width handling
  theme/    colors
  tui/      interactive browser
```

## License

GPL-3.0-or-later. See [LICENSE](LICENSE).
