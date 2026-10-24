import { readFileSync, readdirSync } from 'node:fs';
import ts from 'typescript';
const root = new URL('../src/', import.meta.url);
const keys = JSON.parse(readFileSync(new URL('locales/source.json', root)));
const messages = JSON.parse(readFileSync(new URL('locales/messages.json', root)));
const placeholders = value => JSON.stringify((value.match(/\{\d+\}/g) || []).sort());
for (const locale of ['en', 'pt', 'de', 'fr', 'ko', 'ja', 'zh-TW', 'ru']) {
  const catalog = messages[locale];
  if (!catalog || Object.keys(catalog).length !== keys.length) throw Error(`${locale}: catalog size does not match source`);
  for (const key of keys) {
    if (!catalog[key]?.trim()) throw Error(`${locale}: missing ${key}`);
    if (placeholders(key) !== placeholders(catalog[key])) throw Error(`${locale}: damaged placeholders in ${key}`);
  }
}
for (const file of readdirSync(root).filter(name => /\.tsx?$/.test(name) && !name.includes('.test.'))) {
  const source = ts.createSourceFile(file, readFileSync(new URL(file, root), 'utf8'), ts.ScriptTarget.Latest, true, file.endsWith('tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  function visit(node) {
    if (ts.isCallExpression(node) && node.expression.getText(source) === 't' && ts.isStringLiteral(node.arguments[0])) {
      if (!keys.includes(node.arguments[0].text)) throw Error(`${file}: unregistered translation ${node.arguments[0].text}`);
    }
    ts.forEachChild(node, visit);
  }
  visit(source);
}
console.log(`Locale check passed: ${keys.length} static messages in nine languages, with intact placeholders.`);
