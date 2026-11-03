# ✦ glint

`ls`, but make it pretty. Nerd Font icons, git superpowers, previews, treemaps, thumbnails, and it's still instant.

```sh
go install github.com/Vaspyyy/glint@latest      # installs `glint` into ~/go/bin (needs Go 1.26+)
echo 'glint --init fish | source' >> ~/.config/fish/config.fish   # optional: completions, `gi`, auto-list on cd
glint --help
```

Icons need a [Nerd Font](https://www.nerdfonts.com/) (or run with `--icons never`). Content search in the browser uses [ripgrep](https://github.com/BurntSushi/ripgrep), the trash uses `gio` (GNOME/KDE desktops ship it).

## Views

| command | what you get |
| --- | --- |
| `glint` | column grid with icons and colors |
| `glint -l` | detailed table: colored permissions, size heat, relative times, git status |
| `glint -T` / `-lT` | tree, optionally as a table |
| `glint -i` | **interactive browser**: fuzzy filter, previews (code, markdown, images, archives, hex), mouse, `$EDITOR`, clipboard, file actions, content search, project jumping, disk map (see below) |
| `gi` | same, but your shell `cd`s to wherever you quit (fish integration) |
| `glint -p` | **peek**: what's inside — project stack, line counts, image size, media duration, archive file count, PDF pages, ELF info |
| `glint -C` | **project card**: stack, branch, last commit, language bar, lines, TODOs |
| `glint -P` | **project dashboard** for a folder of projects: stack, branch, uncommitted changes, last commit, 12-week activity sparkline, source size |
| `glint -M` | **disk map**: treemap measured like `du`, regenerable build output hatched, plus "you could free 56 GB" with how (`cargo clean`, …) |
| `glint -M -i` | the disk map, interactive: arrows/click to select, enter to drill in, ⌫ up, `o` to jump into the browser |
| `glint --thumbs` | **image thumbnails** (kitty graphics protocol or Unicode half blocks) |
| `glint -W` | **live** listing that updates as files change |
| `glint --json` | everything above, machine-readable |

## For AI agents: `glint -A`

A plain-text, token-efficient view designed for coding agents: one call answers *what is this project, where is the code, what changed, what's worth reading*.

- **Budgeted tree:** expands folders by value (code and docs before media and vendored code, breadth before depth) until about `--budget` lines (default 200). Collapsed folders still show file counts, size and their main file types.
- **Nothing silently missing:** the top level is always complete (empty and `.git`-only folders included and labelled), and every cut says what it left out, naming the folders.
- **No budget wasted on repetition:** numbered files fold into one line (`frame_{001..240}.png ×240`), and families of similar folders (`run1/` … `run9/`) show the first one and summarise the rest.
- **Workspaces of projects:** every project gets a one-line glance at its contents before any single one is expanded.
- **Useful facts per entry:** line counts for text files (`main.go 120L`), size for binaries, image dimensions, `exec`, symlink targets, git status (`[M]`, `[new]`).
- **Header:** absolute path, detected stack, branch, uncommitted count, HEAD commit, the changed files and what's gitignored.
- **Git-aware:** inside a repo it lists exactly what git sees (tracked plus untracked, minus ignored); outside, it skips `node_modules`, `target`, caches and so on, and says so.
- `glint -A file.go` prints a symbol outline with line numbers, so an agent can read only the part it needs. Add `--outline` for one-line symbol lists in the tree.
- `glint -A -M` shows disk usage as text: the biggest entries and what's inside them.
- Works with `--changed`, `--since 2h`, `-c` (last commit per entry), `-l` (mode and age everywhere), `-L`, `-I`.

On a 4,000-file Rust workspace it uses about **2.9k tokens**, against 13k for `tree -L 2` and 50k for `git ls-files`, while also carrying stack, git state and line counts.

## Interactive browser keys

| key | does |
| --- | --- |
| `f` / `ctrl+f` | search inside files (ripgrep): live results grouped by file, preview around the hit, enter opens `$EDITOR` at that line |
| `P` / `ctrl+p` | jump to a recent folder or any project next to the ones you've used (fuzzy) |
| `M` | disk map of the current folder |
| `r` / `F2` · `d` / `del` | rename · move to trash (via `gio`, falls back to a confirmed permanent delete where there's no trash) |
| `c` · `x` · `p` | copy · cut · paste into the current folder (name clashes become `name (copy).ext`) |
| `n` · `N` | new file · new folder (nested paths allowed) |
| `space` · `a` · `y` | select · select all · copy paths to the clipboard |
| `/` · `.` · `s` · `R` | filter names · hidden files · sort · reverse |

## Git

- per-file status, with directories summarising their contents, and branch/ahead/behind/dirty in the header
- `-c` last commit per entry, `-H` churn heat (how often it changes)
- `--changed` only uncommitted changes (great with `-T`), `--gitignore` hides ignored files

## Filters, sorting, details

`--since 2h` · `-I '*.tmp'` · `-D`/`-f` dirs/files only · natural, accent-aware sort (`file2 < file10`, `Öffentlich` next to `Musik`) · `-t -S -X -r` · `--du` folder sizes with bars · `--octal --inode --links --group` · `--hyperlink` clickable names · `-F` classify

## Config

`~/.config/glint/config.toml` (create one with `glint --init config`). Every key is a long flag name; `[themes.NAME]` tables define palettes:

```toml
theme = "midnight"
peek = true
ignore = ["__pycache__"]

[themes.midnight]
extends = "tokyonight"
blue = "#7aa2f7"
```

Built-in themes: `catppuccin`, `tokyonight`, `gruvbox`, `nord`, and `wal` (follows pywal). `GLINT_OPTS` adds flags too.

## Performance notes

- startup ≈ 4 ms: no dependency with a heavy `init()` (that's why there's no Bubble Tea / lipgloss / chroma: the TUI, styling and highlighter are hand-rolled)
- entries are `lstat`ed in parallel; git is one `git status` call per listing
- thumbnails reuse `~/.cache/thumbnails` (Dolphin/Nautilus) and cache their own in `~/.cache/glint`

## Layout

```
internal/
  cli/      flags, orchestration, watch mode, shell integration, help
  config/   config.toml + custom themes
  fsx/      entries (parallel lstat), sorting, trees, dir sizes
  git/      status, history (last commit / churn), tracked files
  icons/    Nerd Font glyphs + file kinds
  img/      decoding, resizing, half blocks, kitty protocol, thumbnail cache
  peek/     content descriptions
  project/  stack detection + card stats
  render/   grid, long, tree, json, card, treemap, thumbnails
  textw/    display width / truncation of styled text
  theme/    palettes, gradients, a tiny precomputed ANSI styler
  tui/      interactive browser, previews, syntax highlighting
```

## License

GPL-3.0-or-later. See [LICENSE](LICENSE).
