import ts from 'typescript';
import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../src/', import.meta.url));
const keys = new Set(['语言', '应用分发控制台', '登录应用分发控制台，管理应用、下载入口与账户。', '请求失败', '操作失败，请重试']);
const chinese = /[\u3400-\u9fff]/;
for (const file of readdirSync(root).filter(name => /\.tsx?$/.test(name) && !name.includes('.test.'))) {
  const source = ts.createSourceFile(file, readFileSync(root + file, 'utf8'), ts.ScriptTarget.Latest, true, file.endsWith('tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  function visit(node) {
    if (ts.isCallExpression(node) && node.expression.getText(source) === 't' && node.arguments[0] && ts.isStringLiteral(node.arguments[0])) keys.add(node.arguments[0].text);
    if (file === 'api.ts' && ts.isStringLiteral(node) && chinese.test(node.text)) keys.add(node.text);
    ts.forEachChild(node, visit);
  }
  visit(source);
}
function collect(value) {
  if (typeof value === 'string' && chinese.test(value)) keys.add(value);
  else if (Array.isArray(value)) value.forEach(collect);
  else if (value && typeof value === 'object') Object.values(value).forEach(collect);
}
collect(JSON.parse(readFileSync(root + 'home-content.json', 'utf8')));
for (const row of JSON.parse(readFileSync(root + 'locales/reviewed.json', 'utf8')).rows) keys.add(row[0]);
const backend = fileURLToPath(new URL('../../internal/platform/', import.meta.url));
for (const file of readdirSync(backend).filter(name => name.endsWith('.go') && !name.endsWith('_test.go'))) {
  const source = readFileSync(backend + file, 'utf8');
  for (const match of source.matchAll(/(?:problem\(\d+,|errors\.New\(|http\.Error\(w,)\s*("(?:[^"\\]|\\.)*")/g)) {
    const value = JSON.parse(match[1]);
    if (chinese.test(value)) keys.add(value);
  }
}
writeFileSync(root + 'locales/source.json', JSON.stringify([...keys].sort(), null, 2) + '\n');
console.log(`Extracted ${keys.size} static messages.`);
