Fancyband's default tiles, filters, and symbols.

Fancyband reads these after each tile set's own files, and before the
player's own changes:

  <set>.prf     which tile each monster, object, and so on uses
  <set>.json    the virtual tiles: a tile on the sheet seen through a filter
  symbols.prf   the letter and color each one has when the map draws letters

<set> is the name the Tile Editor shows, such as adam-bolt or
shockbolt-dark.

Change them with the Tile Editor (Defaults), which the start menu offers
when fancyband-tui runs with -dev. It saves here, in the source tree.
