import { createRequire } from 'node:module';
import { readFile, writeFile, stat } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';
import { createHash } from 'node:crypto';

const dir = path.dirname(fileURLToPath(import.meta.url));
const require = createRequire(import.meta.url);
let sharp;
try {
  sharp = require('sharp');
} catch {
  const modules = process.env.SPECOUT_NODE_MODULES || '/Users/pacific/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules';
  sharp = require(path.join(modules, 'sharp'));
}

const palette = { background: '#F7F7F2', ink: '#16191C', accent: '#00A995', rule: '#D9DAD3' };
const xml = value => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;');
const plain = value => value.replaceAll('`', '').replaceAll('**', '');
const read = name => readFile(path.join(dir, name), 'utf8');
const save = (name, data) => writeFile(path.join(dir, name), data);

function section(markdown, heading) {
  const start = markdown.indexOf(`## ${heading}\n`);
  if (start === -1) throw new Error(`Missing section ${heading}`);
  const content = markdown.slice(start + heading.length + 4);
  const end = content.indexOf('\n## ');
  return plain((end === -1 ? content : content.slice(0, end)).trim());
}

const reddit = await read('REDDIT.md');
const linkedin = await read('LINKEDIN.md');
const content = {
  product: 'specout',
  link: 'https://github.com/happytoolin/specout',
  image: { eyebrow: 'GO / OPENAPI', name: 'specout', headline: 'OpenAPI from Go types.', subline: 'Keep your HTTP handlers.', footer: 'github.com/happytoolin/specout' },
  reddit: { title: section(reddit, 'Title'), post: section(reddit, 'Post'), short: section(reddit, 'Short post for a project thread') },
  linkedin: { post: section(linkedin, 'Post'), short: section(linkedin, 'Short alternative') },
  alt: 'specout. OpenAPI from Go types. Keep your HTTP handlers. A black document symbol sits beside a turquoise square and a right arrow. The background is off-white.'
};

if (/\bversion\b|\bv\d+\.\d+|\b\d+\.\d+(?:\.\d+)?\b/i.test(JSON.stringify(content))) {
  throw new Error('Public copy contains a release or standard number.');
}

await save('content.json', JSON.stringify(content, null, 2) + '\n');
for (const [filename, text] of Object.entries({
  'reddit-title.txt': content.reddit.title,
  'reddit-post.txt': content.reddit.post,
  'reddit-short.txt': content.reddit.short,
  'linkedin-post.txt': content.linkedin.post,
  'linkedin-short.txt': content.linkedin.short,
  'alt-text.txt': content.alt
})) await save(filename, text + '\n');

function text(value, x, y, size, { bold = false, mono = false, spacing = 0 } = {}) {
  return `<text x="${x}" y="${y}" fill="${palette.ink}" font-family="${mono ? 'Courier New, monospace' : 'Arial, Helvetica, sans-serif'}" font-size="${size}" font-weight="${bold ? 700 : 400}" letter-spacing="${spacing}">${xml(value)}</text>`;
}

// A vector drawing based on the generated reference. The exported artwork
// uses only explicit flat fills and strokes; it contains no raster imagery.
function symbol(x, y, width) {
  return `<g transform="translate(${x} ${y}) scale(${width / 440})" aria-label="Go types produce an API document">
    <rect x="12" y="124" width="76" height="76" rx="6" fill="${palette.accent}"/>
    <g fill="none" stroke="${palette.ink}" stroke-width="10" stroke-linecap="round" stroke-linejoin="round">
      <path d="M101 162H182M165 145L182 162L165 179"/>
      <path d="M220 18H343L418 93V285Q418 296 407 296H220Q209 296 209 285V29Q209 18 220 18Z"/>
      <path d="M343 18V82Q343 93 354 93H418"/>
      <path d="M250 132H370M250 169H370"/>
      <path d="M289 214C276 214 276 221 276 231C276 242 265 242 265 247C265 252 276 252 276 263C276 273 276 280 289 280"/>
      <path d="M339 214C352 214 352 221 352 231C352 242 363 242 363 247C363 252 352 252 352 263C352 273 352 280 339 280"/>
    </g>
  </g>`;
}

function card(width, height, format) {
  let layout;
  if (format === 'square') {
    layout = {
      margin: 80, eyebrowY: 112, brandY: 296, brandSize: 156,
      lines: [{ value: 'OpenAPI from', y: 444 }, { value: 'Go types.', y: 540 }],
      headlineSize: 82, subY: 630, subSize: 36,
      symbol: [700, 739, 380], footerY: 1110, ruleY: 1042
    };
  } else if (format === 'wide') {
    layout = {
      margin: 100, eyebrowY: 111, brandY: 303, brandSize: 160,
      lines: [{ value: content.image.headline, y: 445 }],
      headlineSize: 70, subY: 534, subSize: 37,
      symbol: [1090, 262, 390], footerY: 805, ruleY: 738
    };
  } else {
    layout = {
      margin: 80, eyebrowY: 119, brandY: 334, brandSize: 174,
      lines: [{ value: 'OpenAPI from', y: 516 }, { value: 'Go types.', y: 626 }],
      headlineSize: 90, subY: 724, subSize: 40,
      symbol: [620, 923, 470], footerY: 1400, ruleY: 1330
    };
  }
  const m = layout.margin;
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-labelledby="title desc">
    <title id="title">specout social launch image</title>
    <desc id="desc">${xml(content.alt)}</desc>
    <rect width="${width}" height="${height}" fill="${palette.background}"/>
    <rect x="${m}" y="${layout.eyebrowY - 20}" width="15" height="15" fill="${palette.accent}"/>
    ${text(content.image.eyebrow, m + 33, layout.eyebrowY, 24, { spacing: 2 })}
    ${text(content.image.name, m - 5, layout.brandY, layout.brandSize, { bold: true, spacing: -8 })}
    ${layout.lines.map(line => text(line.value, m, line.y, layout.headlineSize, { spacing: -2.8 })).join('\n')}
    ${text(content.image.subline, m + 2, layout.subY, layout.subSize, { spacing: -0.8 })}
    ${symbol(...layout.symbol)}
    <path d="M${m} ${layout.ruleY}H${width - m}" stroke="${palette.rule}" stroke-width="2"/>
    ${text(content.image.footer, m, layout.footerY, 28, { mono: true, spacing: -0.3 })}
  </svg>`;
  if (/gradient|filter|<image/i.test(svg)) throw new Error('Non-flat artwork in export.');
  return svg;
}

const exports = [];
for (const [format, width, height] of [['square', 1200, 1200], ['wide', 1600, 900], ['portrait', 1200, 1500], ['og', 1280, 640]]) {
  const name = `specout-${format}`;
  const svg = format === 'og'
    ? card(1600, 900, 'wide').replace('width="1600" height="900" viewBox="0 0 1600 900"', 'width="1280" height="640" viewBox="0 50 1600 800"')
    : card(width, height, format);
  await save(name + '.svg', svg);
  const png = await sharp(Buffer.from(svg)).png({ compressionLevel: 9 }).toBuffer();
  await save(name + '.png', png);
  const metadata = await sharp(png).metadata();
  if (metadata.width !== width || metadata.height !== height) throw new Error(`Incorrect dimensions for ${name}`);
  exports.push({ file: name + '.png', editable: name + '.svg', width, height, bytes: png.length, sha256: createHash('sha256').update(png).digest('hex'), format: 'PNG' });
}

const copyFiles = ['REDDIT.md', 'LINKEDIN.md', 'REPLIES.md', 'reddit-title.txt', 'reddit-post.txt', 'reddit-short.txt', 'linkedin-post.txt', 'linkedin-short.txt', 'alt-text.txt'];
const manifest = {
  product: 'specout',
  prepared: '2026-09-30',
  palette,
  constraints: { flatColors: true, gradients: false, releaseNumbersInPublicCopy: false },
  exports,
  copy: await Promise.all(copyFiles.map(async file => ({ file, bytes: (await stat(path.join(dir, file))).size, sha256: createHash('sha256').update(await read(file)).digest('hex') })))
};
await save('manifest.json', JSON.stringify(manifest, null, 2) + '\n');

const images = await Promise.all(exports.map(async item => ({ ...item, data: (await readFile(path.join(dir, item.file))).toString('base64') })));
const copyCard = (id, title, body, file) => `<article class="copy-card"><div class="card-top"><h3>${xml(title)}</h3><button type="button" data-copy="${id}">Copy text</button></div><pre id="${id}">${xml(body)}</pre><a class="download" href="${file}" download>Download text</a></article>`;
const gallery = images.map((item, index) => `<article class="asset"><a href="${item.file}" download><img src="data:image/png;base64,${item.data}" alt="${xml(content.alt)}" width="${item.width}" height="${item.height}"/></a><div class="asset-meta"><div><strong>${['Square image', 'Wide image', 'Portrait image', 'Open Graph image'][index]}</strong><span>${item.width} × ${item.height} · PNG</span></div><div class="asset-links"><a href="${item.file}" download>PNG</a><a href="${item.editable}" download>SVG</a></div></div></article>`).join('\n');
const preview = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>specout social launch kit</title>
<style>
:root{--paper:#F7F7F2;--ink:#16191C;--accent:#00A995;--line:#D9DAD3}*{box-sizing:border-box}body{margin:0;background:var(--paper);color:var(--ink);font-family:Arial,Helvetica,sans-serif}main{max-width:1320px;margin:auto;padding:56px 40px 80px}header{display:flex;justify-content:space-between;align-items:flex-start;gap:24px;border-bottom:1px solid var(--line);padding-bottom:32px}.eyebrow{font:12px 'Courier New',monospace;letter-spacing:1.8px;text-transform:uppercase;display:flex;align-items:center;gap:10px}.dot{height:10px;width:10px;background:var(--accent)}h1{font-size:48px;line-height:1.08;letter-spacing:-2px;margin:18px 0 14px}header p{max-width:560px;line-height:1.6;margin:0;color:#484C4D}a{color:var(--ink);text-underline-offset:4px}.pack{border:1px solid var(--ink);padding:14px 20px;text-decoration:none;white-space:nowrap;font-size:14px}section{padding-top:42px}h2{font-size:25px;letter-spacing:-.7px;margin:0 0 20px}.gallery{display:grid;grid-template-columns:1fr 1.34fr 1fr;align-items:start;gap:20px}.asset{border:1px solid var(--line);background:#FFF}.asset img{display:block;max-width:100%;height:auto}.asset-meta{padding:16px;border-top:1px solid var(--line);display:flex;justify-content:space-between;gap:10px;align-items:center}.asset-meta strong{font-size:14px;font-weight:600;display:block}.asset-meta span{display:block;margin-top:6px;font-size:11px;color:#595D5E}.asset-links{display:flex;gap:10px;font:11px 'Courier New',monospace}.copies{display:grid;grid-template-columns:1fr 1fr;gap:20px}.copy-card{border:1px solid var(--line);padding:24px;background:#FFF;min-width:0}.card-top{display:flex;justify-content:space-between;gap:16px;align-items:center;margin-bottom:16px}.card-top h3{font-size:19px;margin:0;letter-spacing:-.4px}button{cursor:pointer;border:1px solid var(--ink);background:transparent;color:var(--ink);font-size:12px;padding:9px 12px;white-space:nowrap}button:hover,.pack:hover{background:var(--ink);color:white}button:focus-visible,a:focus-visible{outline:3px solid var(--accent);outline-offset:4px}pre{font:14px/1.65 Arial,Helvetica,sans-serif;white-space:pre-wrap;overflow-wrap:anywhere;margin:0 0 20px}.download{font-size:12px}.notes{display:flex;gap:24px;justify-content:space-between;border-top:1px solid var(--line);margin-top:44px;padding-top:22px;font-size:13px;line-height:1.6}.notes p{margin:0;max-width:700px}.notes a{white-space:nowrap}@media(max-width:850px){main{padding:32px 20px 48px}header{flex-direction:column}h1{font-size:38px}.gallery{grid-template-columns:1fr 1fr}.asset:last-child{max-width:360px}.copies{grid-template-columns:1fr}.notes{flex-direction:column}}@media(max-width:480px){.gallery{grid-template-columns:1fr}.asset:last-child{max-width:none}.copy-card{padding:20px}.card-top{gap:8px}.card-top h3{font-size:17px}}
</style></head><body><main>
<header><div><div class="eyebrow"><span class="dot"></span>Launch assets</div><h1>specout social launch kit</h1><p>Posts for Reddit and LinkedIn. Flat images with exact text. Use the copy buttons or download the files.</p></div><a class="pack" href="specout-social-kit.zip" download>Download full kit ↓</a></header>
<section><h2>Images</h2><div class="gallery">${gallery}</div></section>
<section><h2>Posts</h2><div class="copies">
${copyCard('reddit-title', 'Reddit title', content.reddit.title, 'reddit-title.txt')}
${copyCard('linkedin-post', 'LinkedIn post', content.linkedin.post, 'linkedin-post.txt')}
${copyCard('reddit-post', 'Reddit post', content.reddit.post, 'reddit-post.txt')}
${copyCard('reddit-short', 'Reddit project thread', content.reddit.short, 'reddit-short.txt')}
${copyCard('linkedin-short', 'Short LinkedIn post', content.linkedin.short, 'linkedin-short.txt')}
${copyCard('alt-text', 'Image alt text', content.alt, 'alt-text.txt')}
</div></section>
<div class="notes"><p>Read the selected Reddit community rules before posting. Use the square image for LinkedIn. The wide and portrait images provide other formats.</p><div><a href="LAUNCH-KIT.md">Posting guide</a> · <a href="REPLIES.md">Common replies</a></div></div>
</main><script>
for(const button of document.querySelectorAll('[data-copy]')){button.addEventListener('click',async()=>{const value=document.getElementById(button.dataset.copy).textContent;try{if(navigator.clipboard&&window.isSecureContext){await navigator.clipboard.writeText(value)}else{const area=document.createElement('textarea');area.value=value;area.style.position='fixed';area.style.opacity='0';document.body.append(area);area.select();const copied=document.execCommand('copy');area.remove();if(!copied)throw new Error('Clipboard unavailable')}button.textContent='Copied';setTimeout(()=>{button.textContent='Copy text'},1600)}catch{button.textContent='Select text to copy'}})}
</script></body></html>`;
await save('preview.html', preview);
console.log(JSON.stringify({ images: exports.map(({file,width,height,bytes}) => ({file,width,height,bytes})), publicCopyChecked: true, preview: 'preview.html' }, null, 2));
