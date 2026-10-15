import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mergeShortcuts, moveShortcut } from '../src/shortcut-model.ts';
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
