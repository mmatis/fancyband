# Fancyband's fork of Limoni

The client uses a fork of Limoni (github.com/thebanri/limoni), under the Apache License 2.0. The fork lives at github.com/mmatis/limoni. `client/go.mod` uses it in place of the published module through a `replace` line, pinned to the tag `v0.16.0-image-diff.2`. The fork keeps Limoni's module path, so no import changes.

That tag sits on the branch `test/all-pictures`, which starts from the fork's `main`. It stacks `feat/kitty-placement-diff`, `fix/sixel-picture-diff`, and `feat/picture-motion`, then merges `feat/konsole-kitty`. Only the tag is on GitHub, not `test/all-pictures`, `fix/sixel-picture-diff`, or `feat/picture-motion`. The older branch `feat/image-diff` holds the first tag, `v0.16.0-image-diff`, and earlier versions of the same features. It no longer gets changes.

## What was changed

Upstream sends pictures again whenever any picture on the screen changes. That suits a few large pictures but not a map of a thousand tiles, where it cost 117 KB a move with Kitty's protocol and 4.4 MB with Sixel. The fork sends only what changed, which brought those to about 5 KB and 88 KB.

| File | Change |
|---|---|
| `graphics/clip.go` | Adds `AppendKittyPlaceID`, a placement with an id of its own, and `AppendKittyDeletePlacement`. |
| `core/terminal/kitty_images.go` | Keeps each Kitty placement under an id tied to its cells and changes only the placements that differ from the last frame's, with no "delete every placement" in between. That also stops the flash Konsole showed between the delete and the new placements. A full redraw still places everything again. |
| `core/terminal/terminal.go` | Passes the full-redraw flag to the Kitty code, and sends only the Sixel and iTerm2 pictures that differ from the last frame's on the same cells. |
| `core/terminal/capabilities.go` | Lists Konsole as showing Kitty's pictures, so Limoni picks Kitty's protocol when Konsole names itself, even over SSH. |
| `core/terminal/picture_composite.go`, `core/terminal/picture_groups.go`, `core/buffer/diff.go` | Keeps Sixel pictures in Konsole, which deletes a Sixel picture fully covered by newer ones whatever their transparency: pictures stacked on the same cells are drawn into one and sent together. |
| `graphics/graphics.go` | A transparent Sixel picture sets P2 to 1, so the terminal leaves the pixels it skips showing what is behind them instead of filling them with its background color. |
| `core/terminal/frame.go`, `core/terminal/kitty_images.go`, `core/terminal/terminal.go`, `graphics/clip.go` | An `ImageRegion` can be shifted by fractions of a cell (`ShiftX`, `ShiftY`), which Kitty places to the pixel with its `X` and `Y` keys and other protocols round to whole cells, and can name a `Track`, which keeps one Kitty placement for a moving picture. Used to slide sprites between grids. |
| the same files | An `ImageRegion` can name a `Clip`, the cells it must stay within. Kitty cuts a shifted picture at its edges: on whole cells with a source rectangle scaled to them, or within a cell by showing a copy sent at exactly its cells' size in pixels at its own size (`EncodeKittyTransmitSized`). Other protocols leave out a picture that does not fit. Used to scroll the map. |

The branch's commits and its `CHANGELOG.md` describe each change, and its tests cover them (`picture_diff_test.go`, `picture_shift_test.go`, `picture_sixel_test.go`, and `sixel_transparent_test.go`). The branches are written to be offered upstream, so their comments do not name Fancyband.

## Changing the fork

1. Commit the change on `test/all-pictures`, or on the branch it merges that the change belongs to, and run `go test ./core/terminal ./graphics` there.
2. To try it in the client before you push, point Go at your clone with a workspace file that stays out of git:

   ```
   go 1.25.0
   use ./client
   replace github.com/thebanri/limoni => /path/to/your/limoni
   ```

   Save it as `go.work` at the top of this repo, or anywhere and set `GOWORK` to its path.
3. Add a new tag such as `v0.16.0-image-diff.3` and push it. In `client/`, run `go mod edit -replace github.com/thebanri/limoni=github.com/mmatis/limoni@v0.16.0-image-diff.3` and then `go mod tidy`. Delete `go.work` if you made one, and run `scripts/check`.

Never move a tag that `client/go.mod` uses. The Go module proxy keeps the first code it saw under a tag, so a moved tag breaks the build for anyone whose `go.sum` holds the old one.

## Upgrading Limoni

Upgrade on purpose: merge the new release into the fork's `main`, rebase the fork's branches on it, run the fork's tests, then tag and update the client as above. If upstream gains its own way to send only changed pictures, drop the fork and remove the `replace` line.
