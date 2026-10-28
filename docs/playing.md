# Playing Fancyband

Fancyband is a variant of Angband 4.2.6 that you play in a terminal. It is two programs: `fancyband`, the game, and `fancyband-tui`, which draws the game in your terminal and passes it your keys. You start `fancyband-tui`, and it starts the game.

## Start playing

Unpack the release and run the client from its folder:

```
tar xzf fancyband-*-linux-x86_64.tar.gz
cd fancyband-*-linux-x86_64
./fancyband-tui
```

The game loads the character named after your login, or helps you make a new one if there is none. To pick a character, or start a new one, put game options after `--`:

| Command | What it does |
|---|---|
| `./fancyband-tui -- -n` | Make a new character. |
| `./fancyband-tui -- -uGandalf` | Play the character called Gandalf. |

Press **Ctrl+C** at any time to save and quit. The game's own key for that, **Ctrl+X**, works too. If the terminal closes or the connection drops, the game saves before it exits.

## What your terminal needs

The window must be at least 80 columns by 24 rows. With more room, the client adds a panel: a side panel with the message log and the list of monsters you can see when the window is wider than about 105 columns, or a bottom panel with the message log when it is taller than about 31 rows. Add `-subwindows=false` to turn the panels off.

The client works in any modern terminal, over SSH too. Kitty, Konsole, and macOS Terminal have been tried.

## Draw the map with pictures

The map can show tiles, small pictures for each monster, item, and kind of ground, in place of letters. Pick a tile set with `-tiles`:

| Option | Tile set |
|---|---|
| `-tiles 1` | The original 8×8 tiles |
| `-tiles 2` | Adam Bolt's 16×16 tiles |
| `-tiles 3` | David Gervais' 32×32 tiles |
| `-tiles 4` | Nomad's 8×16 tiles |

Tiles need a terminal that can show pictures. Kitty does, with its own image protocol. Konsole does, with an older one called Sixel. The client picks the right one in both. If pictures do not show in another terminal, tell the client which to try:

| Terminal | Option |
|---|---|
| Kitty | `-graphics kitty` |
| Konsole | `-graphics sixel` |

If the terminal cannot show pictures, or you run inside tmux, which drops them, the client draws letters instead.

Tiles cost more to send than letters: about 3 KB a move with Kitty's protocol and about 90 KB with Sixel, against a few hundred bytes for letters. Over a slow SSH link, prefer letters, or Kitty.

Lava ripples between red and yellow. Add `-animate=false` to keep it still.

## Keys

Fancyband uses Angband's keys. These are the ones you need first:

| Key | What it does |
|---|---|
| Arrow keys or number pad | Move, or attack a monster in the way |
| `.` then a direction | Run |
| `<` and `>` | Go up or down stairs |
| `g` | Pick up |
| `i`, `e` | Show your pack, show what you wear |
| `w`, `t` | Wear or wield, take off |
| `q`, `r`, `E` | Drink a potion, read a scroll, eat |
| `f`, `v` | Fire a missile, throw |
| `a`, `u`, `z` | Aim a wand, use a staff, zap a rod |
| `b`, `m` | Browse a book, cast a spell |
| `R` | Rest |
| `o`, `c`, `D`, `T` | Open, close, disarm, tunnel |
| `l` | Look around |
| `C` | Show your character |
| `Ctrl+P` | Show past messages |
| `Enter` | A menu of every command |
| `=` | Options, including the roguelike keys that move with `h`, `j`, `k`, `l` |
| `?` | The game's help, with every command |
| `Ctrl+X` or `Ctrl+C` | Save and quit |

Clicking the map with the mouse walks there.

## Where Fancyband keeps your files

Everything the game writes goes in `~/.angband/Fancyband`:

| Folder or file | What it holds |
|---|---|
| `save/` | Your characters |
| `scores/` | The high score list |
| `panic/` | Emergency saves, if the game ever crashes |
| `fancyband-tui.log`, `fancyband-engine.log` | What the client and the game logged in the last session |

Because of this, you can unpack a newer release next to an old one, or in its place, and keep your characters. Add `-log-dir FOLDER` to write the logs somewhere else.

If something goes wrong, the two log files usually say why.

## Licenses

The `licenses/` folder holds every license that applies:

| File | Covers |
|---|---|
| `angband-copying.rst` | The game, under the GNU GPL version 2 or the Angband licence, and the tile sets' own terms |
| `GPL-2.0.txt` | The text of the GNU GPL version 2 |
| `limoni-LICENSE`, `limoni-FORK.md` | Limoni, the terminal library in `fancyband-tui`, under Apache 2.0, and what Fancyband changed in it |
| `golang.org-x-sys-LICENSE`, `go-LICENSE` | Go libraries built into `fancyband-tui` |

The source code for this release is in the matching `fancyband-*-src.tar.gz`.

Shockbolt's tile set, which Angband ships, is not included: its licence allows it only with Angband itself and forbids changes to it.
