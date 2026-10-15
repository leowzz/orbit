import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mergeShortcuts, moveShortcut, applyWebsiteInfo, isShortcutUrl, canFetchWebsiteInfo } from '../src/shortcut-model.ts';
const entry = id => ({ id, title: id, kind: 'app', target: `/${id}.app`, icon: '' });
test('recommendation/import deduplicates targets without losing custom title or order', () => {
 const original = [entry('a'), entry('b')];
 const next = mergeShortcuts(original, [{ ...entry('a'), id: 'different', title: 'overwritten' }, entry('c')]);
 assert.deepEqual(next.map(i => i.id), ['a', 'b', 'c']);
 assert.equal(next[0].title, 'a');
 assert.equal(original.length, 2);
 assert.throws(() => mergeShortcuts(Array.from({length: 24}, (_, i) => entry(String(i))), [entry('extra')]));
});
test('dragging works in both directions and missing or removed targets do nothing', () => {
 const original = ['a','b','c','d'].map(entry);
 assert.deepEqual(moveShortcut(original,'a','c').map(i=>i.id), ['b','c','a','d']);
 assert.deepEqual(moveShortcut(original,'d','b').map(i=>i.id), ['a','d','b','c']);
 assert.equal(moveShortcut(original,'x','b'), original);
 assert.equal(moveShortcut(original,'a','x'), original);
 assert.equal(moveShortcut(original,'b','b'), original);
 assert.deepEqual(original.map(i=>i.id), ['a','b','c','d']);
});

test('website metadata fills only an empty name and preserves edits during retrieval', () => {
 const requested = { ...entry('web'), kind: 'url', target: 'https://example.com', title: '' };
 const info = { icon: 'png', title: 'Example', warning: null };
 assert.equal(applyWebsiteInfo(requested, requested, info).title, 'Example');
 assert.equal(applyWebsiteInfo({ ...requested, title: 'My name' }, requested, info).title, 'My name');
 const named = { ...requested, title: 'Custom' };
 assert.equal(applyWebsiteInfo(named, named, info).title, 'Custom');
 const changed = { ...requested, target: 'http://other.test' };
 assert.equal(applyWebsiteInfo(changed, requested, info), changed);
 assert.equal(applyWebsiteInfo({ ...requested, icon: 'manual' }, requested, info).icon, 'manual');
 const partial = applyWebsiteInfo({ ...requested, icon: 'old' }, { ...requested, icon: 'old' }, { ...info, icon: '', warning: 'unavailable' });
 assert.equal(partial.icon, 'old');
 assert.equal(partial.title, 'Example');
});

test('custom protocols are configurable while metadata retrieval remains HTTP only', () => {
 for (const url of ['ftp://192.168.1.1', 'codex://threads/01a13dc3-58a7-7861-95ed-dee3470de4fd', 'mailto:hello@example.com', 'file:///tmp/example.txt', 'custom+app:open']) {
   assert.equal(isShortcutUrl(url), true, url);
   assert.equal(canFetchWebsiteInfo(url), false, url);
 }
 for (const url of ['', 'example.com', '--help', 'https://', 'https://example.com\n']) assert.equal(isShortcutUrl(url), false, url);
 assert.equal(isShortcutUrl('http://example.com'), true);
 assert.equal(canFetchWebsiteInfo('http://example.com'), true);
 assert.equal(canFetchWebsiteInfo('https://example.com'), true);
});
