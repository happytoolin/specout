# specout social launch kit

This kit contains posts for Reddit and LinkedIn, matching images, alt text, and replies to common questions. The main message is: **OpenAPI from Go types. Keep your HTTP handlers.**

## Posts

- [Reddit post](REDDIT.md) includes a title, full post, and short project thread comment.
- [LinkedIn post](LINKEDIN.md) includes the main post and a short alternative.
- [Common replies](REPLIES.md) covers request validation, router support, and documentation checks.

Plain text copies are also included for direct use in the post editors.

## Images

| File | Size | Use |
| --- | --- | --- |
| [specout-square.png](specout-square.png) | 1200 × 1200 | Main LinkedIn image or a square social post |
| [specout-wide.png](specout-wide.png) | 1600 × 900 | Reddit image attachment or a wide social post |
| [specout-portrait.png](specout-portrait.png) | 1200 × 1500 | Taller LinkedIn alternative |
| [specout-og.png](specout-og.png) | 1280 × 640 | GitHub social preview and README banner |

Each image uses an off-white background, black text, and a single turquoise accent. There are no gradients, shadows, or release numbers. Each image has an editable SVG copy.

## Before you post

1. Open [preview.html](preview.html) to review the images and copy.
2. Open the repository link to check that readers can access it.
3. Read the rules of the selected Reddit community.
4. Use the Reddit text post where project announcements are allowed.
5. Use the short comment in the current Small Projects thread if you choose that format.
6. Add the square image to the LinkedIn post.
7. Add the supplied alt text where the editor supports it.
8. Keep time available to answer technical questions after posting.

## Repository social preview

The README uses `specout-og.png`. This export preserves the wide image design in the size recommended for a GitHub social preview. The [GitHub image guidance](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/customizing-your-repositorys-social-media-preview) recommends 1280 × 640 pixels and a file under 1 MB.

The repository social preview is a separate setting. In [repository settings](https://github.com/happytoolin/specout/settings), use Social preview, then Edit, then Upload an image. Select `specout-og.png`.

r/golang has a weekly Small Projects thread with less strict posting criteria. Its moderators describe the format in this [Small Projects thread](https://www.reddit.com/r/golang/comments/1vxc255/small_projects/). Find the current thread in [r/golang](https://www.reddit.com/r/golang/). The rules page did not expose readable rule text during preparation, so this kit does not claim that a standalone post is approved.

## Product details checked

The copy follows the project README and the handler, router, and recorder code. It does not claim automatic request validation or response body validation. Route discovery checks are limited to chi and gorilla/mux.

## Files for future edits

- `build-assets.mjs` exports the PNG and SVG files, text copies, manifest, and local preview.
- `content.json` stores the exact public copy and image text.
- `PROVENANCE.md` records how the illustration and exports were made.
- `manifest.json` records dimensions and file hashes.
- `specout-social-kit.zip` contains the complete kit.
