# Fancyband's fork of Limoni

This folder is Limoni v0.16.0 (github.com/thebanri/limoni), Apache License 2.0; see `LICENSE`. `client/go.mod` uses it in place of the published module through a `replace` line.

## What was left out

The folders `examples` (except `examples/ascii3d/duck.glb`, which a test reads), `benchmarks`, `benchmark-results`, `docs`, `platform-results`, `scripts`, and `cmd`, and the files `README_TR.md` and `AWESOME.md`. Nothing the client builds uses them.

## What was changed

Upstream sends pictures again whenever any picture on the screen changes. That suits a few large pictures but not a map of a thousand tiles, where it cost 117 KB a move with Kitty's protocol and 4.4 MB with Sixel. The fork sends only what changed, which brought those to about 5 KB and 88 KB.

| File | Change |
|---|---|
| `graphics/clip.go` | Adds `AppendKittyPlaceID`, a placement with an id of its own, and `AppendKittyDeletePlacement`. |
| `core/terminal/kitty_images.go` | Keeps each Kitty placement under an id tied to its cells and changes only the placements that differ from the last frame's, with no "delete every placement" in between. That also stops the flash Konsole showed between the delete and the new placements. A full redraw still places everything again. |
| `core/terminal/terminal.go` | Passes the full-redraw flag to the Kitty code, and sends only the Sixel and iTerm2 pictures that differ from the last frame's on the same cells. |
| `core/terminal/capabilities.go` | Lists Konsole as showing Sixel pictures, so Limoni picks Sixel when Konsole names itself, even over SSH. |
| `graphics/graphics.go` | A transparent Sixel picture sets P2 to 1, so the terminal leaves the pixels it skips showing what is behind them instead of filling them with its background color. |
| `core/terminal/frame.go`, `core/terminal/kitty_images.go`, `core/terminal/terminal.go`, `graphics/clip.go` | An `ImageRegion` can be shifted by fractions of a cell (`ShiftX`, `ShiftY`), which Kitty places to the pixel with its `X` and `Y` keys and other protocols round to whole cells, and can name a `Track`, which keeps one Kitty placement for a moving picture. Used to slide sprites between grids. |
| the same files | An `ImageRegion` can name a `Clip`, the cells it must stay within. Kitty cuts a shifted picture at its edges: on whole cells with a source rectangle scaled to them, or within a cell by showing a copy sent at exactly its cells' size in pixels at its own size (`EncodeKittyTransmitSized`). Other protocols leave out a picture that does not fit. Used to scroll the map. |
| `core/terminal/fork_pictures_test.go`, `core/terminal/fork_slide_test.go`, `graphics/fork_sixel_test.go` | New tests for the above. `graphics/graphics_test.go` expects the new transparent Sixel header. |

Each change is marked "Fancyband fork" in a comment.

## Upgrading

Upgrade on purpose: copy the new release over this folder, leaving out the same folders, apply the changes above again, and run `go test ./core/terminal ./graphics` here and `scripts/check` at the top. If upstream gains its own way to send only changed pictures, drop the fork and remove the `replace` line.
