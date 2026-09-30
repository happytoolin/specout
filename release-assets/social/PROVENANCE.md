# specout social asset provenance

Prepared on 30 September 2026.

## Illustration reference

The built-in Codex ImageGen tool generated `sources/illustration-reference.png`. The exact prompt is saved in `sources/image-prompt.txt`. The reference shows a code symbol producing an API document.

## Final images

The reference was used to guide an editable SVG drawing. The final images contain the SVG drawing and deterministic text. They do not contain pixels from the generated reference.

All colors are fixed fills or strokes. The background is `#F7F7F2`. The text and document symbol use `#16191C`. The accent uses `#00A995`. The footer rule uses `#D9DAD3`.

There are no gradients, shadows, texture layers, or release numbers in the final images. Text uses Arial and Courier New. The PNG files were rendered from the SVG files with Sharp. Export sizes and file hashes are recorded in `manifest.json`.

The Open Graph export uses the wide image layout with a 1600 × 800 viewBox. It removes only empty space at the top and bottom. The resulting PNG is 1280 × 640 pixels. The text and document symbol retain their proportions.

## Copy sources

Product details were checked against the project README, `handler.go`, `register_std.go`, and `recorder/verify.go`. The repository and its default branch were also checked. The public repository is [happytoolin/specout](https://github.com/happytoolin/specout).

The Reddit project thread guidance uses a [moderator introduction to Small Projects](https://www.reddit.com/r/golang/comments/1vxc255/small_projects/). It is a format reference, not the current thread or approval for a new standalone post.

## Rebuild

Run `node release-assets/social/build-assets.mjs` from the repository root. The script uses Sharp from local dependencies or the bundled Codex runtime. Set `SPECOUT_NODE_MODULES` to a different dependency directory if needed.

Change the Markdown post files to edit the copy. Change `build-assets.mjs` to edit the image text or layout. Run the script again to update the exports, plain text copies, preview, and manifest. Recreate the ZIP after rebuilding.
