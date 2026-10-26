import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import vm from 'node:vm';

const context = { window: {} };
vm.runInNewContext(readFileSync('public/docs-data.js', 'utf8'), context);
const pages = context.window.SSHDOCK_DOCS;
assert.equal(pages.length, 10);
assert.equal(new Set(pages.map((page) => page.id)).size, pages.length);
const documents = new Map([
  ['index.html', readFileSync('public/index.html', 'utf8')],
  ['docs.html', readFileSync('public/docs.html', 'utf8')],
]);
for (const page of pages) {
  assert(page.title && page.group && page.description && page.body);
  assert(page.source.startsWith('https://github.com/sshdock/sshdock/'));
  assert(page.body.includes('<h2 id="'));
  documents.set(page.id, page.body);
}
let links = 0;
for (const [name, html] of documents) {
  for (const [, href] of html.matchAll(/href="([^"]+)"/g)) {
    if (/^(https?:|mailto:)/.test(href)) continue;
    const url = new URL(href, 'https://example.com/' + (name.endsWith('.html') ? name : 'docs.html'));
    if (!url.pathname.endsWith('.html')) {
      assert(existsSync('public' + url.pathname), `${name}: missing asset ${href}`);
      links++;
      continue;
    }
    const target = url.pathname === '/docs.html' ? ((pages.find((page) => page.id === (url.searchParams.get('page') || 'quick-start'))?.body || '') + documents.get('docs.html')) : documents.get(url.pathname.slice(1));
    assert(target, `${name}: missing destination ${href}`);
    if (url.hash) assert(target.includes(`id="${decodeURIComponent(url.hash.slice(1))}"`), `${name}: missing anchor ${href}`);
    links++;
  }
}
for (const file of ['app.js', 'docs-data.js', 'styles.css', 'favicon.svg', 'tui-preview.svg']) assert(existsSync('public/' + file), `${file} missing`);
assert(!readFileSync('public/docs-data.js', 'utf8').includes('sshdock check .'));
console.log(`PASS: ${pages.length} guides, ${links} internal links, required assets, optional preflight wording.`);
