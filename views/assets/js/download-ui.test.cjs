const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
function batch() {
    const context = { window: {}, document: { addEventListener() {} }, URL };
    vm.runInNewContext(fs.readFileSync(__dirname + '/download-ui.js', 'utf8'), context);
    return Object.assign(context.window.magnetBatch(), { $nextTick(fn) { fn(); }, $refs: { draft: { focus() {} } } });
}
test('paste and Add creates a removable named entry without exposing HTML', () => {
    const b = batch();
    b.draft = 'magnet:?xt=urn:btih:abc&dn=My%20file'; b.add();
    assert.equal(b.links.length, 1); assert.equal(b.draft, ''); assert.equal(b.label(b.links[0]), 'My file');
    b.links.splice(0, 1); assert.equal(b.links.length, 0);
});
test('validation rejects empty, malformed, duplicate, multiline and excessive entries', () => {
    const b = batch();
    for (const value of ['', 'https://example.com', 'magnet:?dn=test', 'magnet:?xt=a\nmagnet:?xt=b']) {
        b.draft = value; b.add(); assert.equal(b.links.length, 0); assert.ok(b.error);
    }
    b.draft = 'magnet:?xt=abc'; b.add(); b.draft = 'magnet:?xt=abc'; b.add();
    assert.equal(b.links.length, 1); assert.match(b.error, /already/);
    b.links = Array.from({ length: 50 }, (_, i) => 'magnet:?xt=' + i);
    b.draft = 'magnet:?xt=extra'; b.add(); assert.equal(b.links.length, 50);
});
test('partial imports remove only accepted entries and retain failures for retry', () => {
    const b = batch(); b.links = ['magnet:?xt=a', 'magnet:?xt=b'];
    b.imported(['magnet:?xt=a']); assert.deepEqual(b.links, ['magnet:?xt=b']);
});
