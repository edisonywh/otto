# otto

A terminal-based daily notes app with a vim editor, todo tracking, and markdown preview.

## Features

- **Vim editor** — full modal editing with normal/insert/visual modes
- **Todo pane** — track daily todos with priorities and status
- **Markdown preview** — toggle a rendered preview of your notes
- **Daily notes** — each day gets its own file, auto-created on launch
- **Pane navigation** — switch between editor, todos, and preview with keyboard

## Key Bindings

### Editor (Normal mode)

| Key | Action |
|-----|--------|
| `i / a / A / I` | Enter Insert mode |
| `o / O` | New line below / above |
| `h / j / k / l` | Move cursor |
| `w / b / e`, `W / B / E` | Word motions |
| `gg`, `G`, `{N}gg` | Go to top / bottom / line N |
| `d / y / c` + motion | Delete / yank / change |
| `dd`, `D`, `dG` | Delete line / to end / to EOF |
| `u`, `Ctrl+R` | Undo / redo |
| `p / P` | Paste below / above |
| `r` | Replace char |
| `~` | Toggle case |
| `J` | Join line below |
| `x` | Delete char |
| `>> / <<` | Indent / unindent |
| `Ctrl+P` | Toggle markdown preview |
| `Ctrl+S` | Save |
| `Esc` | Normal → NavPane mode |

### Pane Navigation (NavPane mode, orange border)

| Key | Action |
|-----|--------|
| `h / l / j / k` | Move between panes |
| `Enter` | Activate focused pane |
| `Tab / Shift+Tab` | Cycle panes |

### Todo Pane

| Key | Action |
|-----|--------|
| `j / k` | Navigate todos |
| `Enter` | Toggle done |
| `i` | Edit todo text |
| `1-4` | Set priority (1=urgent … 4=low) |
| `/` | Search |

## Install

```bash
git clone https://github.com/edisonywh/otto.git
cd otto
go build -o otto .
./otto
```

Requires Go 1.21+.

## Data Storage

Notes are saved to `~/.otto/notes/YYYY-MM-DD.md`. Each day's file is created automatically when otto launches.
