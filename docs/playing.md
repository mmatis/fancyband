# Playing Fancyband

Fancyband is a variant of Angband 4.2.6 that you play in a terminal. It is two programs: `fancyband`, the game, and `fancyband-tui`, which draws the game in your terminal and passes it your keys. You start `fancyband-tui`, and it starts the game.

## Start playing

Unpack the release and run the client from its folder:

```
tar xzf fancyband-*-linux-x86_64.tar.gz
cd fancyband-*-linux-x86_64
./fancyband-tui
```

A start menu opens over the title screen:

- **Continue** loads the character you played last.
- **New character** makes a new one. Its name names its savefile, so it never replaces another character. If a character with that name already has a save, the game asks first.
- **Load a character** lists your other characters.
- **Quit** leaves.

Choose with the arrow keys and Enter, or click an entry. A long list also scrolls with the mouse wheel and **Page Up** and **Page Down**. In the list of characters, which shows each one's tile when the map uses tiles, one click selects a character and a double click loads it; the **Load** and **Cancel** buttons do the same as Enter and Escape, and `/` searches.

You can also pick a character, or start a new one, by putting game options after `--`:

| Command | What it does |
|---|---|
| `./fancyband-tui -- -n` | Open the start menu with New character selected. |
| `./fancyband-tui -- -uGandalf` | Open the start menu with Continue set to the character called Gandalf. |

## Make a character

New character opens Fancyband's own birth screen. It goes a step at a time, and the steps along the top show where you are; click one you have done to go back to it, or press **Escape** to go back a step.

1. **Race** and **Class.** Move through the list; the panel on the right shows what each is good at: stat changes, skills, and abilities. Press **Enter** or double-click to choose.
2. **Stats.** The table shows each stat, the race's and class's changes, and the result. The game suggests a set of point-based stats. Select a stat with the up and down arrows, and press the right arrow or `+` to spend a point on it, or the left arrow or `-` to take one back; the **+** and **-** buttons do the same. Press `r` to roll random stats instead, `p` for the previous roll, and `b` to go back to the suggested points.
3. **Name.** Type a name. The first key replaces the suggested one. If a savefile already has that name, the game asks before it replaces it.
4. **History.** The game writes a short history for your character. Press `e` to change it.
5. **Begin.** A last look at the character. Press **Enter** to start playing, or `s` to start over.

Press `=` at any step for the birth options. After a character dies, the game may first offer a new one just like it: `y` uses it as is, `c` changes the name first, and `n` makes a different one.

## Shops

Walk into a shop's entrance, a number on the town map, to open the store screen. It shows what the store sells on the left, with prices, and what you carry on the right, with what the store would pay. The panel under them describes the selected item, and the game's messages, such as what you just bought, show above the buttons.

- **Tab**, or the left and right arrows, moves between the two lists.
- `p`, or **Enter** in the store's list, buys the selected item. For more than one, a dialog asks how many, then the game asks you to confirm the price. `o` buys just one.
- `d`, or **Enter** in your list, sells the selected item. Items the store does not buy are dimmed, and trying says why. With the birth option that makes stores pay nothing, the buttons say **Give**.
- In your home, the same keys take items out and drop them in, with no prices.
- **Escape** leaves the shop.

The lists scroll with the arrows, **Page Up** and **Page Down**, and the mouse wheel; the description scrolls with **Shift** and the arrows or page keys. The birth screen and the store screen can each go back to the game's own version on the Display page of the options.

## The game menu

Press **Escape** or **F10** while the game waits for your next command to open the game menu:

- **Resume** goes back to the game.
- **Save** saves the game.
- **Load a character** saves this character and loads another, or makes a new one.
- **Options** opens the options screen. See [options](#options).
- **Tile Editor** gives things other tiles. See [change the tiles](#change-the-tiles).
- **Borg** turns on Angband's automatic player, the borg. It plays the character until you press **Escape** or **F10**, which stops it and opens this menu. The first time for each character, a warning explains that using the borg keeps the character off the high score list. The map keeps its tiles while the borg plays. When the borg stops, your options are as you left them.
- **Save & Quit** saves and quits.

Escape still cancels prompts and menus, as in Angband. Just after a run or a rest ends, Escape goes to the game for half a second, so pressing it to stop the run does not open the menu by surprise. F10 always opens the menu.

When a character dies, a menu offers a new character, another character, a look at the dead one's items, a look back at the character through the game's own screens, or Quit.

Press **Ctrl+C** at any time to save and quit. The game's own key for that, **Ctrl+X**, works too. If the terminal closes or the connection drops, the game saves before it exits.

## Items and spells

Fancyband draws the inventory and the game's questions about items and spells itself.

**The inventory.** Press `i` for the pack, `e` for the equipment, or `|` for the quiver. They are three tabs on one screen: press **Tab** to move between them, `/` to switch between the pack and the equipment, or `|` for the quiver. Each item shows its tile or letter, its name, and its weight, and the panel on the right describes the selected one. The bottom of the screen shows how much you carry and how much more you can carry before it slows you down.

To use an item, press its letter, or select it and press **Enter**. A menu lists what you can do with it, such as Quaff, Equip, Take off, Drop, Throw, or Inscribe, each on the game's own key for it. So `i`, then `b`, then `q` drinks the potion in slot b, as in Angband. Choices you cannot make now are dimmed. **Browse** shows a book's spells over the inventory. Inscriptions such as `!d` still ask before you drop the item.

**Which item?** When a command needs an item, such as `q` to quaff, a list shows only the items that fit, with a tab for each place they are in: the pack, the equipment, the quiver, or the floor. Press an item's letter to choose it, or a digit for an item inscribed with it, such as `@q1`. The `/`, `|`, and `-` keys go to the pack or equipment, the quiver, and the floor. If nothing fits, the game says so and asks nothing.

**Spells.** Casting, studying, and browsing a book show its spells, each with its level, mana, chance to fail, and the game's note on it, such as "untried" or its damage. The panel on the right describes the selected spell. Spells you cannot choose now are dimmed. Press a spell's letter to choose it.

**Small questions** come as small dialogs:

- **How many?** Type a number, or change it with the arrow keys, **Page Up** and **Page Down** (by 10), or the mouse wheel. **End** or **All** takes all of them.
- **Yes or no?** Press `y` or `n`. **Escape** means no, and No is selected at first.
- **Which direction?** Press a digit, an arrow key, or a roguelike key such as `h`, or click a direction.

**Keymaps still work.** When a keymap types the answer to a question, as a keymap for `q1` does, the game's own prompt takes it, as it would without Fancyband.

The description panel scrolls with the mouse wheel, or with **Shift** and the arrow keys or **Page Up** and **Page Down**. Each of these screens can go back to the game's own version on the Display page of the options; the change takes effect when the game next starts.

## Look and target

Fancyband draws look mode, target mode, and the question of where to aim itself. Each is a list of what you can see, nearest first, with a description of the selected thing beside it. As you move through the list, a marker shows the thing on the map: a highlighted letter, or a yellow frame round the tile. The list moves to the top or the bottom of the window to keep out of the marker's way.

**Look around.** Press `l` (`x` with the roguelike keys), or choose Look in the game's mouse menu. The list shows you, the monsters, objects, traps, and interesting places, such as doors and shop entrances, that the map shows now. Move with the arrow keys, **Page Up** and **Page Down**, the mouse wheel, or **Space** and `-`, as in Angband. Click something on the map to select it, or click any other grid to look at it.

- `t` or **Enter** targets the selected monster, or the selected place.
- `g` travels there.
- **Escape** or `q` goes back to the game.

**Pick a target.** Press `*`. The list shows only the monsters you can target, and the one you targeted last is selected. Press **Enter** or `t` to target the selected one.

**Aim.** When a spell, wand, or missile needs a direction, the list shows what you can target:

- **Enter**, `t`, or `5` fires at the selected monster.
- `'` fires at the closest.
- A digit, or a roguelike key such as `h`, fires in that direction.
- **Escape** cancels.

With nothing to target, the direction dialog asks instead. The game's `use_old_target` option still fires at your target without asking.

**Read about a monster in the side panel.** Click a monster in the list of monsters you can see, in the side panel, to read its description. The description scrolls with the arrow keys, **Page Up** and **Page Down**, and the mouse wheel. **Escape** closes it.

These screens can go back to the game's own versions on the Display page of the options.

## Options

Press `=`, or choose **Options** in the game menu or the start menu. The screen has five pages; press **Tab** or a number from 1 to 5 to move between them:

| Page | What it sets |
|---|---|
| Interface | The game's interface options, such as `solid_walls`, each with a checkbox |
| Numbers | The delay for animations, the hit point warning, and the wait for a second key on diagonal moves |
| Birth | The birth options. They show here, but change only while you make a character |
| Cheat | The cheat options. Turning one on asks first, because it marks the character as a cheater for good |
| Display | Fancyband's own settings: the tile set, the tile size, animations, the side panels, highlighting under the mouse, and which screens are Fancyband's own |

Move with the arrow keys, or a page at a time with **Page Up** and **Page Down**, and press **Space** or **Enter** to change a checkbox or **Left** and **Right** to change a number. The mouse works too, and its wheel scrolls the list. Changes take effect at once.

**Save this page as the defaults for new characters**, on the Interface and Birth pages, does what it says. **More options**, on the Interface page, opens the game's own options menu, for keymaps, colors, what to ignore, and the rest. The Display page saves itself in `fancyband-tui.json`.

**Highlight under the mouse**, on the Display page, lights up the button, row, or tab under the mouse pointer in Fancyband's own screens, such as the menus, the options, and the Tile Editor. It is off by default, because the terminal then reports every move of the mouse, which adds a little traffic over SSH.

Before a character loads, the options screen opens on the Display page; the game's options show the defaults for new characters.

## What your terminal needs

The window must be at least 80 columns by 24 rows. With more room, the client adds a panel: a side panel with the message log and the list of monsters you can see when the window is wider than about 105 columns, or a bottom panel with the message log when it is taller than about 31 rows. Add `-subwindows=false` to turn the panels off.

The client works in any modern terminal, over SSH too. Kitty, Konsole, and macOS Terminal have been tried.

## Draw the map with pictures

The map can show tiles, small pictures for each monster, item, and kind of ground, in place of letters. Pick a tile set and a size on the Display page of the [options](#options), while you play; the client remembers your choice. Sizes that would leave too little of the map in the window are not used: the map drops to a smaller size and goes back up when the window grows. You can also pick a set with `-tiles`, which wins over the options for that session:

| Option | Tile set |
|---|---|
| `-tiles 1` | The original 8×8 tiles |
| `-tiles 2` | Adam Bolt's 16×16 tiles |
| `-tiles 3` | David Gervais' 32×32 tiles |
| `-tiles 4` | Nomad's 8×16 tiles |
| `-tiles 5`, `-tiles 6` | Shockbolt's 64×64 tiles, dark and light |

A few of Shockbolt's monsters are twice as tall as a grid and reach into the grid above. The client does not draw those right yet.

Tiles need a terminal that can show pictures. Kitty and Konsole both do, with Kitty's image protocol, and the client picks it in both. Konsole can also show an older protocol called Sixel, but with Sixel creatures and the map do not move smoothly. If pictures do not show in another terminal, tell the client which to try:

| Terminal | Option |
|---|---|
| Kitty | `-graphics kitty` |
| Konsole | `-graphics kitty` |

If the terminal cannot show pictures, or you run inside tmux, which drops them, the client draws letters instead.

Tiles cost more to send than letters: about 3 KB a move with Kitty's protocol and about 90 KB with Sixel, against a few hundred bytes for letters. Over a slow SSH link, prefer letters, or Kitty.

With tiles, you can animate lava, or anything else, with a filter in the [Tile Editor](#give-a-tile-a-filter). Turn off **Animations** on the Display page, or add `-animate=false`, to keep everything still.

## Change the tiles

The Tile Editor lets you give anything another tile, or give a tile to something that shows as a letter because its set has none for it. Press F2 while the game waits for a command, or choose **Tile Editor** in the [game menu](#the-game-menu). It needs a tile set.

The editor fills the window, in three columns:

1. **The list** of everything that can take a tile, with a tab for each kind of thing: monsters, objects, flavors, terrain, traps, and effects, which are the bolts, balls, and explosions of spells. Each row shows the letter, the tile, and the name.
2. **The preview** of the selected thing's tile, three times the size it is on the map, with its filter, on the tile set's floor. Its details are under it.
3. **The tile sheet, the filter, and the symbol,** as three tabs. While you move through the list, this column follows: the sheet shows where the selected thing's tile is, the filter tab shows its filter, and the symbol tab shows its letter.

The column with a gold border has the keys. Click a column to give it the keys, or press **Esc** in the third column to go back to the list. In a narrow window, the third column takes the preview's place while you use it.

- **Tab** moves to the next tab of the list, and **/** searches by name.
- **m**, or the **Missing only** checkbox, shows only the things that lack a tile.
- **Enter** gives the tile sheet the keys. The sheet shows each tile at your tile size, on the tile set's floor. Click a tile to use it, or move the cursor with the arrow keys and press **Enter**. The panel beside or under the sheet lists what already uses the tile under the cursor.
- Terrain and traps can have a different tile in each kind of light, and an effect has a tile for each bolt direction and one for balls and explosions. **Tab** in the sheet picks which one changes. For an effect, **f** sets all four bolt directions at once from the tile under the cursor and the three after it.
- **f** opens the filter for the thing, described [below](#give-a-tile-a-filter).
- **u** puts back the set's own tile, and removes its filter.
- **s** saves your changes.

The buttons under the list do the same with the mouse: **Undo**, **Save**, and **Close**, which leaves the editor as **Esc** does. The tabs of the third column open the sheet, the filter, and the symbol.

Every list in the editor, and the sheet, scrolls with the mouse wheel, the arrow keys, and **Page Up** and **Page Down**. The wheel scrolls whatever is under the pointer, even when another column has the keys.

The map changes as soon as you pick a tile. If you leave without saving, the editor asks whether to save or to put back what you saved before.

Your changes are kept for each tile set, in `tiles/` in your [Fancyband folder](#where-fancyband-keeps-your-files), for example `tiles/adam-bolt.prf`, with your filters beside it in `tiles/adam-bolt.json`. Other Angband front ends do not read them. Changing to another tile set drops the changes you have not saved.

### Change the letter for text mode

When the map draws letters instead of tiles, each thing shows as a letter in a color, such as a white `@` for you. The **Symbol** tab changes them. Select a row, click **Symbol**, and type the new letter, number, or sign. Use **Left** and **Right** on **Color**, or click one of the colors under the list, which show the letter in each. The preview shows the letter beside **As a letter**, and **Put back the game's symbol** undoes the change.

In text mode an effect draws `*` for a ball or a line along its path, so an effect takes only a color.

**s** saves your symbols with your tiles. They are kept in `symbols.prf` in your [Fancyband folder](#where-fancyband-keeps-your-files), for every tile set, and the map uses them whenever it draws letters.

### Give a tile a filter

A filter changes how one thing's tile looks, without changing the tile set. Select a row in the Tile Editor and press **f**, or click the **Filter** tab. The third column lists the settings, and the preview shows the tile through the filter. The map changes as you go.

| Setting | What it does |
|---|---|
| Hue | Turns colors around the color wheel: 120° makes red green, 240° makes it blue |
| Saturation | Makes colors richer, or grayer |
| Lightness | Makes the tile brighter or darker |
| Opacity | How much the tile covers what is under it. A ghost at 50% shows the floor through it. |
| Outline | Draws a colored edge on the tile's own shape, not around the grid |
| Inner glow | Like the outline, but soft, fading inward |
| Blur | Softens a monster or object |
| Waver | Moves pixels along a wave, like heat haze |
| Sparkle | Lights a few random pixels |
| Color range | Limits hue, saturation, and lightness to some colors |
| Animation | Moves between the settings and a second set over time. **Curve** says how it moves, **Period** how long one cycle takes, and **Spread** whether every grid moves together, in a ripple, or at random. |

Use the up and down arrows to pick a setting and the left and right arrows to change it. **Remove the filter** puts back the plain tile. **Esc** goes back to the list, where **s** saves.

A filter changes only the thing you chose, even when other things share its tile: a blue cave orc leaves the other orcs as they were. To do that, the editor gives the thing a tile of its own, in the spare rows past the end of the tile sheet.

With Sixel, every animated tile on screen is sent again at every step, about eight times a second. The preview warns when many are on screen.

## Keys

Fancyband uses Angband's keys. These are the ones you need first:

| Key | What it does |
|---|---|
| Arrow keys or number pad | Move, or attack a monster in the way |
| `.` then a direction | Run |
| `<` and `>` | Go up or down stairs |
| `g` | Pick up |
| `i`, `e`, `\|` | Show your pack, what you wear, your quiver; see [items and spells](#items-and-spells) |
| `w`, `t` | Wear or wield, take off |
| `q`, `r`, `E` | Drink a potion, read a scroll, eat |
| `f`, `v` | Fire a missile, throw |
| `a`, `u`, `z` | Aim a wand, use a staff, zap a rod |
| `b`, `m` | Browse a book, cast a spell |
| `R` | Rest |
| `o`, `c`, `D`, `T` | Open, close, disarm, tunnel |
| `l` | Look around; see [look and target](#look-and-target) |
| `*` | Pick a target |
| `C` | Show your character |
| `Ctrl+P` | Show past messages |
| `Enter` | A menu of every command |
| `=` | Options, including the roguelike keys that move with `h`, `j`, `k`, `l` |
| `?` | The game's help, with every command |
| `Escape` or `F10` | The game menu |
| `Ctrl+X` or `Ctrl+C` | Save and quit |

Clicking the map with the mouse walks there.

## Where Fancyband keeps your files

Everything the game writes goes in `~/.angband/Fancyband`:

| Folder or file | What it holds |
|---|---|
| `save/` | Your characters |
| `scores/` | The high score list |
| `panic/` | Emergency saves, if the game ever crashes |
| `fancyband-tui.json` | The client's settings, such as the character you played last |
| `tiles/` | Your changes to each tile set, from the Tile Editor |
| `symbols.prf` | Your letters and colors for text mode, from the Tile Editor |
| `borg.txt` | The borg's settings, copied there the first time you turn the borg on |
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
