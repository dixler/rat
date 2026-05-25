const vscode = require('vscode');
const cp = require('child_process');
const fs = require('fs');
const path = require('path');

const PALETTE = {
  hotMagenta: { foreground: 'terminal.ansiMagenta', background: 'rgba(255, 0, 255, 0.22)', fontWeight: '700' },
  orange: { foreground: 'charts.orange', background: 'rgba(255, 165, 0, 0.22)' },
  yellow: { foreground: 'charts.yellow', background: 'rgba(255, 215, 0, 0.22)' },
  green: { foreground: 'terminal.ansiGreen', background: 'rgba(35, 209, 139, 0.22)' },
  cyan: { foreground: 'terminal.ansiCyan', background: 'rgba(0, 255, 255, 0.2)' },
  blue: { foreground: 'charts.blue', background: 'rgba(88, 166, 255, 0.22)' },
  lavender: { foreground: 'terminal.ansiMagenta', background: 'rgba(255, 0, 255, 0.2)' },
  white: { foreground: 'editor.foreground', background: 'rgba(255, 255, 255, 0.18)' }
};

const ANSI_COLOR_CODES = {
  '38;5;198': 'hotMagenta',
  '38;5;215': 'orange',
  '38;5;226': 'yellow',
  '38;5;120': 'green',
  '96': 'cyan',
  '94': 'blue',
  '38;5;183': 'lavender',
  '32': 'green',
  '38;5;130': 'orange',
  '38;5;208': 'orange',
  '97': 'white',
  '48;5;198': 'hotMagenta',
  '48;5;215': 'orange',
  '48;5;226': 'yellow',
  '48;5;120': 'green',
  '48;5;183': 'lavender',
  '48;5;130': 'orange',
  '48;5;208': 'orange',
  '48;5;15': 'white'
};

const FOREGROUND_STYLE = new Map();
const BACKGROUND_STYLE = new Map();
const output = vscode.window.createOutputChannel('Text Semantic Highlight');
const seenLogs = new Set();

let serverProc;

function log(message, extra) {
  const suffix = extra === undefined ? '' : ` ${typeof extra === 'string' ? extra : JSON.stringify(extra)}`;
  output.appendLine(`[text-semantic-highlight] ${message}${suffix}`);
}

function logOnce(key, message, extra) {
  if (seenLogs.has(key)) return;
  seenLogs.add(key);
  log(message, extra);
}

function mk({ color, ...extra }) {
  return vscode.window.createTextEditorDecorationType({
    ...(color ? { color: new vscode.ThemeColor(color) } : {}),
    ...extra
  });
}

function mkDeclaration(backgroundColor) {
  return vscode.window.createTextEditorDecorationType({ backgroundColor, borderRadius: '2px' });
}

function parseAnsiStyle(style) {
  const parsed = { fg: undefined, bg: undefined, inverse: false, fontWeight: undefined, textDecoration: undefined };
  const matches = typeof style === 'string' ? [...style.matchAll(/\x1b\[([0-9;]+)m/g)] : [];

  for (const [, rawCodes] of matches) {
    const codes = rawCodes.split(';').map((part) => Number(part));
    for (let i = 0; i < codes.length; i++) {
      const code = codes[i];
      if (!Number.isInteger(code)) continue;
      if (code === 0) {
        parsed.fg = undefined;
        parsed.bg = undefined;
        parsed.inverse = false;
        parsed.fontWeight = undefined;
        parsed.textDecoration = undefined;
        continue;
      }
      if (code === 1) {
        parsed.fontWeight = '700';
        continue;
      }
      if (code === 4) {
        parsed.textDecoration = 'underline';
        continue;
      }
      if (code === 7) {
        parsed.inverse = true;
        continue;
      }
      if (code === 38 && codes[i + 1] === 5 && Number.isInteger(codes[i + 2])) {
        parsed.fg = `38;5;${codes[i + 2]}`;
        i += 2;
        continue;
      }
      if (code === 48 && codes[i + 1] === 5 && Number.isInteger(codes[i + 2])) {
        parsed.bg = `48;5;${codes[i + 2]}`;
        i += 2;
        continue;
      }
      if ((code >= 30 && code <= 37) || (code >= 90 && code <= 97)) {
        parsed.fg = String(code);
        continue;
      }
      if ((code >= 40 && code <= 47) || (code >= 100 && code <= 107)) {
        parsed.bg = String(code);
      }
    }
  }

  return parsed;
}

function paletteSpec(code) {
  const palette = ANSI_COLOR_CODES[code];
  return palette ? PALETTE[palette] : undefined;
}

function normalizeSpan(span) {
  if (typeof span?.style === 'string' && span.style) {
    return { parsed: parseAnsiStyle(span.style), source: 'style', raw: span.style, priority: Number.isInteger(span.priority) ? span.priority : 0 };
  }

  return { parsed: undefined, source: 'unknown', raw: JSON.stringify(span ?? {}), priority: 0 };
}

function foregroundSpecFromAnsi(parsed) {
  const spec = paletteSpec(parsed?.fg);
  if (!spec) return undefined;
  const { foreground, fontWeight, fontStyle, textDecoration } = spec;
  return {
    ...(foreground ? { color: foreground } : {}),
    ...(parsed?.fontWeight ? { fontWeight: parsed.fontWeight } : {}),
    ...(fontWeight ? { fontWeight } : {}),
    ...(fontStyle ? { fontStyle } : {}),
    ...(parsed?.textDecoration ? { textDecoration: parsed.textDecoration } : {}),
    ...(textDecoration ? { textDecoration } : {})
  };
}

function foregroundDecoration(specKey, spec) {
  const fgSpec = foregroundSpecFromAnsi(spec);
  if (!fgSpec) return undefined;
  if (!FOREGROUND_STYLE.has(specKey)) FOREGROUND_STYLE.set(specKey, mk(fgSpec));
  return FOREGROUND_STYLE.get(specKey);
}

function backgroundColorFromAnsi(parsed) {
  const bgSpec = paletteSpec(parsed?.bg);
  if (bgSpec?.background) return bgSpec.background;

  if (parsed?.inverse) {
    const fgSpec = paletteSpec(parsed.fg);
    return fgSpec?.background;
  }

  return undefined;
}

function spanUsesBackground(parsed) {
  return Boolean(parsed?.bg || parsed?.inverse);
}

function backgroundDecoration(specKey, spec) {
  const backgroundColor = backgroundColorFromAnsi(spec);

  if (!backgroundColor) return undefined;
  if (!BACKGROUND_STYLE.has(specKey)) BACKGROUND_STYLE.set(specKey, mkDeclaration(backgroundColor));
  return BACKGROUND_STYLE.get(specKey);
}

function cfg() { return vscode.workspace.getConfiguration('textSemanticHighlight'); }

function resolveServerCwd() {
  const configured = cfg().get('serverCwd', '${workspaceFolder}');
  const workspaceFolder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || process.cwd();
  const cwd = configured.replace('${workspaceFolder}', workspaceFolder);
  if (fs.existsSync(path.join(cwd, 'cmd', 'rat'))) return cwd;

  const parent = path.dirname(cwd);
  if (parent !== cwd && fs.existsSync(path.join(parent, 'cmd', 'rat'))) {
    log('server cwd fallback to parent', { configured: cwd, resolved: parent });
    return parent;
  }

  log('server cwd missing cmd/rat', { configured: cwd });
  return cwd;
}

function startServerIfNeeded() {
  if (serverProc) {
    log('server already running');
    return;
  }
  if (!cfg().get('autoStartServer', true)) {
    log('auto-start disabled');
    return;
  }
  const cmd = cfg().get('serverCommand', 'go');
  const args = cfg().get('serverArgs', ['run', './cmd/rat', '--serve', '--addr', ':8081']);
  const cwd = resolveServerCwd();
  log('starting server', { cmd, args, cwd });
  serverProc = cp.spawn(cmd, args, { cwd, stdio: ['ignore', 'pipe', 'pipe'] });
  serverProc.stdout?.on('data', (chunk) => log('server stdout', chunk.toString().trim()));
  serverProc.stderr?.on('data', (chunk) => log('server stderr', chunk.toString().trim()));
  serverProc.on('error', (err) => log('server process error', { message: err.message }));
  serverProc.on('exit', (code, signal) => {
    log('server exited', { code, signal });
    serverProc = undefined;
  });
}

function stopServer() {
  if (!serverProc) return;
  log('stopping server');
  serverProc.kill();
  serverProc = undefined;
}

async function fetchSpans(doc, url) {
  const target = `${url}/spans`;
  log('fetching spans', { path: doc.fileName, languageId: doc.languageId, url: target });
  try {
    const r = await fetch(target, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ path: doc.fileName }) });
    if (!r.ok) {
      log('span fetch failed', { status: r.status, statusText: r.statusText });
      return [];
    }
    const j = await r.json();
    const spans = Array.isArray(j.spans) ? j.spans : [];
    log('fetched spans', { count: spans.length });
    return spans;
  } catch (err) {
    log('span fetch error', { message: err instanceof Error ? err.message : String(err) });
    return [];
  }
}

async function decorate(editor) {
  const c = cfg();
  if (!c.get('enabled', true)) {
    log('highlighting disabled');
    return clear(editor);
  }
  const langSet = new Set(c.get('languages', ['go']));
  if (!langSet.has(editor.document.languageId)) {
    log('skipping unsupported language', { languageId: editor.document.languageId, configured: [...langSet] });
    return clear(editor);
  }
  const url = c.get('serverUrl', 'http://localhost:8081');

  const buckets = new Map();
  const declarationBuckets = new Map();
  const spans = await fetchSpans(editor.document, url);
  for (const s of spans) {
    const range = new vscode.Range(new vscode.Position((s.line || 1) - 1, s.start || 0), new vscode.Position((s.line || 1) - 1, s.end || 0));
    const normalized = normalizeSpan(s);
    const spec = normalized.parsed;
    const useBackground = spanUsesBackground(spec);
    const specKey = `${normalized.source}:${normalized.raw}:${normalized.priority}:${useBackground ? 'background' : 'foreground'}`;
    const decoration = useBackground ? backgroundDecoration(specKey, spec) : foregroundDecoration(specKey, spec);
    if (!decoration) {
      logOnce(`unrecognized:${JSON.stringify(Object.keys(s || {}).sort())}:${normalized.raw}`, 'unrecognized span payload', { keys: Object.keys(s || {}), span: s });
      continue;
    }
    if (useBackground) {
      if (!declarationBuckets.has(decoration)) declarationBuckets.set(decoration, []);
      declarationBuckets.get(decoration).push(range);
      continue;
    }
    if (!buckets.has(decoration)) buckets.set(decoration, []);
    buckets.get(decoration).push(range);
  }
  FOREGROUND_STYLE.forEach((decoration) => editor.setDecorations(decoration, []));
  BACKGROUND_STYLE.forEach((decoration) => editor.setDecorations(decoration, []));
  for (const [decoration, ranges] of buckets.entries()) editor.setDecorations(decoration, ranges);
  for (const [decoration, ranges] of declarationBuckets.entries()) editor.setDecorations(decoration, ranges);
  log('applied decorations', { spans: spans.length, foregroundBuckets: buckets.size, backgroundBuckets: declarationBuckets.size });
}

function clear(editor) {
  FOREGROUND_STYLE.forEach((decoration) => editor.setDecorations(decoration, []));
  BACKGROUND_STYLE.forEach((decoration) => editor.setDecorations(decoration, []));
}

function activate(context) {
  log('activating extension');
  startServerIfNeeded();
  const refresh = () => {
    const e = vscode.window.activeTextEditor;
    if (!e) {
      log('no active editor to decorate');
      return;
    }
    log('refreshing editor', { path: e.document.fileName, languageId: e.document.languageId });
    void decorate(e);
  };
  context.subscriptions.push(
    vscode.commands.registerCommand('textSemanticHighlight.toggle', async () => {
      const c = cfg();
      await c.update('enabled', !c.get('enabled', true), vscode.ConfigurationTarget.Global);
      log('toggled highlighting', { enabled: c.get('enabled', true) });
      refresh();
    }),
    vscode.window.onDidChangeActiveTextEditor((e) => {
      if (!e) return;
      log('active editor changed', { path: e.document.fileName, languageId: e.document.languageId });
      void decorate(e);
    }),
    vscode.workspace.onDidSaveTextDocument((d) => {
      const e = vscode.window.activeTextEditor;
      if (!e || e.document !== d) return;
      log('document saved', { path: d.fileName });
      void decorate(e);
    }),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('textSemanticHighlight.autoStartServer') || e.affectsConfiguration('textSemanticHighlight.serverCommand') || e.affectsConfiguration('textSemanticHighlight.serverArgs') || e.affectsConfiguration('textSemanticHighlight.serverCwd')) {
        log('server configuration changed');
        stopServer();
        startServerIfNeeded();
      }
      if (e.affectsConfiguration('textSemanticHighlight')) {
        log('highlight configuration changed');
        refresh();
      }
    }),
    { dispose: stopServer }
  );
  refresh();
}

function deactivate() {
  log('deactivating extension');
  stopServer();
  FOREGROUND_STYLE.forEach((decoration) => decoration.dispose());
  BACKGROUND_STYLE.forEach((decoration) => decoration.dispose());
  output.dispose();
}
module.exports = { activate, deactivate };
