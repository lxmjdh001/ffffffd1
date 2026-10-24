// One-time, syntax-aware migration of developer-owned UI literals. This is not
// a runtime DOM translator: API values and customer content are never touched.
import ts from 'typescript';
import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const root = fileURLToPath(new URL('../src/', import.meta.url));
const chinese = /[\u3400-\u9fff]/;
const catalog = new Set(['语言']);
const quote = value => JSON.stringify(value);
const call = value => { catalog.add(value); return `t(${quote(value)})`; };
const jsxText = text => text.split(/\r?\n/).map((line, index, lines) => {
  let part = line.replace(/\t/g, ' ');
  if (index) part = part.replace(/^ +/, '');
  if (index !== lines.length - 1) part = part.replace(/ +$/, '');
  return part + (part && index < lines.length - 1 ? ' ' : '');
}).join('').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&apos;/g, "'");
for (const file of readdirSync(root).filter(file => file.endsWith('.tsx') && !file.includes('.test.') && file !== 'language-selector.tsx')) {
  let source = readFileSync(root + file, 'utf8');
  if (source.includes('// i18n: static UI literals')) continue;
  if (file === 'landing.tsx') {
    source = source.replace("import homeContent from './home-content.json';", "import originalHomeContent from './home-content.json';\nimport { translateContent } from './i18n';\nimport { LanguageSelector } from './language-selector';");
    source = source.replace('export function LandingPage() {', 'export function LandingPage() {\n  const homeContent = translateContent(originalHomeContent);');
    source = source.replace('<div className="home-nav-actions">', '<div className="home-nav-actions"><LanguageSelector />');
  }
  if (file === 'layout.tsx') {
    source = source.replace("import './workspace-navigation.css';", "import './workspace-navigation.css';\nimport { LanguageSelector } from './language-selector';");
    source = source.replace('<div className="topbar-right">', '<div className="topbar-right"><LanguageSelector />');
    source = source.replace('{current}</strong>', '{t(current)}</strong>');
  }
  if (file === 'auth.tsx') {
    source = "import { LanguageSelector } from './language-selector';\n" + source;
    source = source.replace('<div className="auth-top">', '<div className="auth-top"><LanguageSelector />');
  }
  // Module-level arrays of plain strings must be evaluated per render. Object
  // properties below become getters for the same reason.
  if (file === 'referrals.tsx') {
    source = source.replace("const levelNames = ['一级', '二级', '三级'];", "const levelNames = () => ['一级', '二级', '三级'];").replaceAll('levelNames[', 'levelNames()[');
  }
  const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const edits = [];
  let usedT = false, subscribed = false;
  const edit = (node, text) => { edits.push({ start: node.getStart(ast), end: node.end, text }); usedT = true; };
  function withinFunction(node) {
    for (let parent = node.parent; parent; parent = parent.parent) if (ts.isFunctionLike(parent)) return true;
    return false;
  }
  function skipLiteral(node) {
    const p = node.parent;
    return (p.name === node && !ts.isJsxAttribute(p)) || ts.isLiteralTypeNode(p) || ts.isImportDeclaration(p) || ts.isExportDeclaration(p) ||
      (ts.isCallExpression(p) && p.expression.getText(ast) === 't');
  }
  function visit(node) {
    if (ts.isFunctionDeclaration(node) && node.name && /^[A-Z]/.test(node.name.text) && node.body) {
      edits.push({ start: node.body.getStart(ast) + 1, end: node.body.getStart(ast) + 1, text: '\n  useLocale();' });
      subscribed = true;
    }
    if (ts.isTemplateExpression(node)) {
      let key = node.head.text;
      node.templateSpans.forEach((span, i) => { key += `{${i}}` + span.literal.text; });
      if (chinese.test(key)) {
        catalog.add(key);
        edit(node, `t(${quote(key)}, { ${node.templateSpans.map((span, i) => `${i}: ${source.slice(span.expression.getStart(ast), span.expression.end)}`).join(', ')} })`);
        // Nested expressions can themselves contain translatable literals.
        // They are migrated in the additional pass below to avoid overlaps.
        return;
      }
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      if (chinese.test(node.text) && !skipLiteral(node)) {
        const p = node.parent;
        if (ts.isJsxAttribute(p)) edit(node, `{${call(node.text)}}`);
        else if (ts.isPropertyAssignment(p) && p.initializer === node && !withinFunction(node)) {
          edit(p, `get ${p.name.getText(ast)}() { return ${call(node.text)}; }`);
        } else edit(node, call(node.text));
      }
      return;
    }
    if (ts.isJsxText(node) && chinese.test(node.text)) {
      edit(node, `{${call(jsxText(node.text))}}`);
      return;
    }
    ts.forEachChild(node, visit);
  }
  visit(ast);
  edits.sort((a, b) => b.start - a.start || b.end - a.end);
  for (let i = 0; i < edits.length; i++) {
    const { start, end, text } = edits[i];
    if (i && end > edits[i - 1].start) throw new Error(`Overlapping migration in ${file}`);
    source = source.slice(0, start) + text + source.slice(end);
  }
  if (usedT || subscribed) {
    source = `// i18n: static UI literals\nimport { ${[usedT && 't', subscribed && 'useLocale'].filter(Boolean).join(', ')} } from './i18n';\n` + source;
    writeFileSync(root + file, source);
  }
}
function collect(value) {
  if (typeof value === 'string' && chinese.test(value)) catalog.add(value);
  else if (Array.isArray(value)) value.forEach(collect);
  else if (value && typeof value === 'object') Object.values(value).forEach(collect);
}
collect(JSON.parse(readFileSync(root + 'home-content.json', 'utf8')));
writeFileSync(root + 'locales/source.json', JSON.stringify([...catalog].sort(), null, 2) + '\n');
console.log(`Migrated UI with ${catalog.size} translation keys.`);
