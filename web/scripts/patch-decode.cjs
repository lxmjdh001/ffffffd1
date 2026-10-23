// React Admin uses query-string 7 (CommonJS). The CVE-2026-45822 fix in
// decode-uri-component 0.5 is ESM. Keep the upstream parser and unwrap its
// default export; fail closed if the upstream integration changes.
const fs = require('node:fs');
const target = require.resolve('query-string');
const source = fs.readFileSync(target, 'utf8');
const original = "const decodeComponent = require('decode-uri-component');";
const patched = "const decodeModule = require('decode-uri-component');\nconst decodeComponent = decodeModule.default || decodeModule;";
if (source.includes(patched)) process.exit(0);
if (!source.includes(original)) throw new Error('Review the query-string/decode-uri-component compatibility patch before upgrading.');
fs.writeFileSync(target, source.replace(original, patched));
