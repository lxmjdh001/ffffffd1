const assert = require('node:assert/strict');
const query = require('query-string');
assert.equal(query.parse('q=%E4%BD%A0%E5%A5%BD')['q'], '你好');
assert.equal(query.parse('q=a+b')['q'], 'a b');
assert.equal(query.parse('q=%F0%80%80%80')['q'], '%F0%80%80%80');
assert.equal(query.stringify({q:'a b'}),'q=a%20b');
// Previously pathological malformed input must complete in ordinary test time.
assert.ok(typeof query.parse('q=' + '%FF'.repeat(10000)).q === 'string');
console.log('Query parser security compatibility: passed');
