import { readFileSync, existsSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import ts from 'typescript';
import type { Plugin } from 'vite';

// Follow only static imports: the console and its full translation catalog are
// loaded after navigation, rather than on every public homepage visit.
export function initialMessageKeys(root: string): Set<string> {
  const keys = new Set<string>();
  const visited = new Set<string>();
  const collectJSON = (value: unknown) => {
    if (typeof value === 'string') keys.add(value);
    else if (value && typeof value === 'object') Object.values(value).forEach(collectJSON);
  };
  const visit = (filename: string) => {
    if (visited.has(filename) || filename.endsWith('/locales/messages.json')) return;
    visited.add(filename);
    const source = readFileSync(filename, 'utf8');
    if (filename.endsWith('.json')) { collectJSON(JSON.parse(source)); return; }
    const ast = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, true);
    const walk = (node: ts.Node) => {
      if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) keys.add(node.text);
      if ((ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) && node.moduleSpecifier && ts.isStringLiteral(node.moduleSpecifier) && !('importClause' in node && node.importClause?.isTypeOnly)) {
        const name = node.moduleSpecifier.text;
        if (name.startsWith('.')) {
          const base = resolve(dirname(filename), name);
          const target = [base, base + '.tsx', base + '.ts', base + '/index.tsx', base + '/index.ts'].find(file => /\.(tsx?|json)$/.test(file) && existsSync(file));
          if (target) visit(target);
        }
      }
      ts.forEachChild(node, walk);
    };
    walk(ast);
  };
  visit(resolve(root, 'src/main.tsx'));
  return keys;
}

export function homeMessages(): Plugin {
  const id = 'virtual:d1-home-messages';
  let root = '';
  return {
    name: 'd1-public-home-messages', apply: 'build', enforce: 'pre',
    configResolved(config) { root = config.root; },
    resolveId(source) { if (source === id) return '\0' + id; },
    load(source) {
      if (source !== '\0' + id) return;
      const keys = initialMessageKeys(root);
      const catalogs = JSON.parse(readFileSync(resolve(root, 'src/locales/messages.json'), 'utf8')) as Record<string, Record<string, string>>;
      const initial = Object.fromEntries(Object.entries(catalogs).map(([locale, messages]) => [locale, Object.fromEntries(Object.entries(messages).filter(([key]) => keys.has(key)))]));
      return 'export default ' + JSON.stringify(initial) + ';';
    },
    transform(source, filename) {
      if (!filename.replaceAll('\\', '/').endsWith('/src/i18n.ts')) return;
      return source.replace("import messages from './locales/messages.json' with { type: 'json' };", `import messages from '${id}';`);
    },
  };
}
