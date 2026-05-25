const vscode = require('vscode');
const cp = require('child_process');

const STYLE = {
  keyword: mk('charts.blue'),
  type: mk('terminal.ansiGreen'),
  variable: mk('charts.yellow'),
  parameter: mk('charts.orange'),
  project: mk('charts.blue'),
  samepkg: mk('terminal.ansiCyan'),
  external: mk('terminal.ansiMagenta'),
  indirect: mk('terminal.ansiMagenta', { fontWeight: '700' })
};

let serverProc;

function mk(color, extra = {}) { return vscode.window.createTextEditorDecorationType({ color: new vscode.ThemeColor(color), ...extra }); }

function cfg() { return vscode.workspace.getConfiguration('textSemanticHighlight'); }

function startServerIfNeeded() {
  if (serverProc || !cfg().get('autoStartServer', true)) return;
  const cmd = cfg().get('serverCommand', 'go');
  const args = cfg().get('serverArgs', ['run', './cmd/rat', '--serve', '--addr', ':8081']);
  const cwd = cfg().get('serverCwd', '${workspaceFolder}').replace('${workspaceFolder}', vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || process.cwd());
  serverProc = cp.spawn(cmd, args, { cwd, stdio: 'ignore' });
  serverProc.on('exit', () => { serverProc = undefined; });
}

function stopServer() {
  if (!serverProc) return;
  serverProc.kill();
  serverProc = undefined;
}

async function fetchSpans(doc, url) {
  const r = await fetch(`${url}/spans`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ path: doc.fileName }) });
  if (!r.ok) return [];
  const j = await r.json();
  return Array.isArray(j.spans) ? j.spans : [];
}

async function decorate(editor) {
  const c = cfg();
  if (!c.get('enabled', true)) return clear(editor);
  const langSet = new Set(c.get('languages', ['go']));
  if (!langSet.has(editor.document.languageId)) return clear(editor);
  const url = c.get('serverUrl', 'http://localhost:8081');

  const buckets = Object.fromEntries(Object.keys(STYLE).map((k) => [k, []]));
  for (const s of await fetchSpans(editor.document, url)) {
    if (!STYLE[s.kind]) continue;
    buckets[s.kind].push(new vscode.Range(new vscode.Position((s.line || 1) - 1, s.start || 0), new vscode.Position((s.line || 1) - 1, s.end || 0)));
  }
  for (const [k, ranges] of Object.entries(buckets)) editor.setDecorations(STYLE[k], ranges);
}

function clear(editor) { Object.values(STYLE).forEach((s) => editor.setDecorations(s, [])); }

function activate(context) {
  startServerIfNeeded();
  const refresh = () => { const e = vscode.window.activeTextEditor; if (e) void decorate(e); };
  context.subscriptions.push(
    vscode.commands.registerCommand('textSemanticHighlight.toggle', async () => {
      const c = cfg();
      await c.update('enabled', !c.get('enabled', true), vscode.ConfigurationTarget.Global);
      refresh();
    }),
    vscode.window.onDidChangeActiveTextEditor((e) => e && void decorate(e)),
    vscode.workspace.onDidSaveTextDocument((d) => { const e = vscode.window.activeTextEditor; if (e && e.document === d) void decorate(e); }),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('textSemanticHighlight.autoStartServer') || e.affectsConfiguration('textSemanticHighlight.serverCommand') || e.affectsConfiguration('textSemanticHighlight.serverArgs') || e.affectsConfiguration('textSemanticHighlight.serverCwd')) {
        stopServer();
        startServerIfNeeded();
      }
      if (e.affectsConfiguration('textSemanticHighlight')) refresh();
    }),
    { dispose: stopServer }
  );
  refresh();
}

function deactivate() { stopServer(); Object.values(STYLE).forEach((s) => s.dispose()); }
module.exports = { activate, deactivate };
