const vscode = require('vscode');
const cp = require('child_process');
const fs = require('fs');
const path = require('path');

const output = vscode.window.createOutputChannel('Text Semantic Highlight');
const DEFAULT_LANGUAGES = ['go'];
const SUPPORTED_EXTENSIONS = new Set(['.go']);

let serverProc;
const decorationStates = new Map();

function log(message, extra) {
  const suffix = extra === undefined ? '' : ` ${typeof extra === 'string' ? extra : JSON.stringify(extra)}`;
  output.appendLine(`[text-semantic-highlight] ${message}${suffix}`);
}

function cfg() {
  return vscode.workspace.getConfiguration('textSemanticHighlight');
}

function cubeValue(v) {
  return v === 0 ? 0 : 55 + v * 40;
}

function xterm256(idx) {
  const clamped = Math.max(0, Math.min(255, idx));
  if (clamped < 16) {
    return [
      '#000000', '#800000', '#008000', '#808000', '#000080', '#800080', '#008080', '#c0c0c0',
      '#808080', '#ff0000', '#00ff00', '#ffff00', '#0000ff', '#ff00ff', '#00ffff', '#ffffff'
    ][clamped];
  }
  if (clamped <= 231) {
    const n = clamped - 16;
    const r = Math.floor(n / 36);
    const g = Math.floor((n % 36) / 6);
    const b = n % 6;
    return `#${cubeValue(r).toString(16).padStart(2, '0')}${cubeValue(g).toString(16).padStart(2, '0')}${cubeValue(b).toString(16).padStart(2, '0')}`;
  }
  const v = 8 + (clamped - 232) * 10;
  return `#${v.toString(16).padStart(2, '0')}${v.toString(16).padStart(2, '0')}${v.toString(16).padStart(2, '0')}`;
}

function ansiBasicColor(i) {
  return ['#000000', '#b22222', '#2e8b57', '#b8860b', '#1d4ed8', '#8b5cf6', '#0891b2', '#d1d5db'][i];
}

function ansiBrightColor(i) {
  return ['#6b7280', '#ef4444', '#22c55e', '#fde047', '#60a5fa', '#c084fc', '#67e8f9', '#ffffff'][i];
}

function ansiColor(code) {
  if (!code) return undefined;
  if (code.startsWith('38;5;') || code.startsWith('48;5;')) {
    const idx = Number(code.split(';')[2]);
    return Number.isInteger(idx) ? xterm256(idx) : undefined;
  }

  const value = Number(code);
  if (!Number.isInteger(value)) return undefined;
  if (value >= 30 && value <= 37) return ansiBasicColor(value - 30);
  if (value >= 90 && value <= 97) return ansiBrightColor(value - 90);
  if (value >= 40 && value <= 47) return ansiBasicColor(value - 40);
  if (value >= 100 && value <= 107) return ansiBrightColor(value - 100);
  return undefined;
}

function parseAnsiStyle(style) {
  const parsed = { fg: undefined, bg: undefined, inverse: false, fontWeight: undefined, textDecoration: undefined };
  const textDecorations = new Set();
  const matches = typeof style === 'string' ? [...style.matchAll(/\x1b\[([0-9;]+)m/g)] : [];

  const updateTextDecoration = () => {
    parsed.textDecoration = textDecorations.size ? [...textDecorations].join(' ') : undefined;
  };

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
        textDecorations.clear();
        updateTextDecoration();
      } else if (code === 1) {
        parsed.fontWeight = '700';
      } else if (code === 4) {
        textDecorations.add('underline');
        updateTextDecoration();
      } else if (code === 7) {
        parsed.inverse = true;
      } else if (code === 9) {
        textDecorations.add('line-through');
        updateTextDecoration();
      } else if (code === 24) {
        textDecorations.delete('underline');
        updateTextDecoration();
      } else if (code === 29) {
        textDecorations.delete('line-through');
        updateTextDecoration();
      } else if (code === 38 && codes[i + 1] === 5 && Number.isInteger(codes[i + 2])) {
        parsed.fg = `38;5;${codes[i + 2]}`;
        i += 2;
      } else if (code === 48 && codes[i + 1] === 5 && Number.isInteger(codes[i + 2])) {
        parsed.bg = `48;5;${codes[i + 2]}`;
        i += 2;
      } else if ((code >= 30 && code <= 37) || (code >= 90 && code <= 97)) {
        parsed.fg = String(code);
      } else if ((code >= 40 && code <= 47) || (code >= 100 && code <= 107)) {
        parsed.bg = String(code);
      }
    }
  }

  return parsed;
}

function parseHexByte(v) {
  const n = Number.parseInt(v, 16);
  return Number.isNaN(n) ? undefined : n;
}

function contrastTextColor(hex) {
  if (typeof hex !== 'string' || hex.length !== 7 || !hex.startsWith('#')) return '#ffffff';
  const r = parseHexByte(hex.slice(1, 3));
  const g = parseHexByte(hex.slice(3, 5));
  const b = parseHexByte(hex.slice(5, 7));
  if (![r, g, b].every(Number.isInteger)) return '#ffffff';

  const luma = 0.299 * r + 0.587 * g + 0.114 * b;
  return luma >= 140 ? '#000000' : '#ffffff';
}

function decorationOptions(style) {
  const parsed = parseAnsiStyle(style);
  let foreground = ansiColor(parsed.fg);
  let backgroundColor = ansiColor(parsed.bg);

  if (parsed.inverse) {
    [foreground, backgroundColor] = [backgroundColor, foreground];
    if (backgroundColor && !foreground) foreground = contrastTextColor(backgroundColor);
  }

  const options = {
    rangeBehavior: vscode.DecorationRangeBehavior.ClosedClosed,
    fontStyle: 'normal'
  };
  if (foreground) options.color = foreground;
  if (backgroundColor) {
    options.backgroundColor = backgroundColor;
    options.borderRadius = '2px';
    if (!foreground) options.color = contrastTextColor(backgroundColor);
  }
  if (parsed.fontWeight) options.fontWeight = parsed.fontWeight;
  if (parsed.textDecoration) options.textDecoration = parsed.textDecoration;

  return options.color || options.backgroundColor || options.fontWeight || options.textDecoration ? options : undefined;
}

function resolveServerCwd() {
  const configured = cfg().get('serverCwd', '${workspaceFolder}');
  const workspaceFolder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || process.cwd();
  const cwd = configured.replace('${workspaceFolder}', workspaceFolder);
  if (fs.existsSync(cwd)) return cwd;

  log('configured server cwd does not exist', { cwd });
  return workspaceFolder;
}

function startServerIfNeeded() {
  if (serverProc || !cfg().get('autoStartServer', true)) return;

  const command = cfg().get('serverCommand', 'rat');
  const args = cfg().get('serverArgs', ['--serve', '--addr', ':8081']);
  const cwd = resolveServerCwd();
  log('starting server', { command, args, cwd });

  serverProc = cp.spawn(command, args, { cwd, stdio: ['ignore', 'pipe', 'pipe'] });
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

function documentKey(document) {
  return document.uri.toString();
}

function stateFor(document) {
  const key = documentKey(document);
  let state = decorationStates.get(key);
  if (!state) {
    state = { decorations: [], generation: 0, timer: undefined, signature: undefined, abort: undefined };
    decorationStates.set(key, state);
  }
  return state;
}

function visibleEditorsFor(document) {
  return vscode.window.visibleTextEditors.filter((editor) => editor.document === document);
}

function applyDecorations(document, decorations) {
  for (const editor of visibleEditorsFor(document)) {
    for (const decoration of decorations) {
      editor.setDecorations(decoration.type, decoration.ranges);
    }
  }
}

function clearDocument(document) {
  const key = documentKey(document);
  const state = decorationStates.get(key);
  if (!state) return;

  clearTimeout(state.timer);
  state.abort?.abort();
  for (const decoration of state.decorations) {
    decoration.type.dispose();
  }
  decorationStates.delete(key);
}

function clearAll() {
  for (const key of [...decorationStates.keys()]) {
    const state = decorationStates.get(key);
    clearTimeout(state?.timer);
    state?.abort?.abort();
    for (const decoration of state?.decorations || []) {
      decoration.type.dispose();
    }
    decorationStates.delete(key);
  }
}

function isSupportedDocument(document) {
  if (!new Set(cfg().get('languages', DEFAULT_LANGUAGES)).has(document.languageId)) return false;
  if (document.uri.scheme === 'untitled') return true;
  if (document.uri.scheme !== 'file') return false;
  if ([...SUPPORTED_EXTENSIONS].some((extension) => document.fileName.endsWith(extension))) return true;
  return document.languageId === 'go';
}

function normalizeSpans(payload) {
  const spans = payload?.spans;
  if (Array.isArray(spans)) return spans;
  if (!spans || typeof spans !== 'object') return [];

  const out = [];
  for (const [line, lineSpans] of Object.entries(spans)) {
    if (!Array.isArray(lineSpans)) continue;
    for (const span of lineSpans) {
      out.push({ ...span, line: Number(span.line || line) });
    }
  }
  return out;
}

function documentPath(document) {
  if (document.uri.scheme === 'file') return document.fileName;

  const workspaceFolder = vscode.workspace.workspaceFolders?.[0]?.uri.fsPath || resolveServerCwd();
  const base = path.basename(document.fileName || 'untitled.go');
  const name = path.extname(base) ? base : `${base}.go`;
  return path.join(workspaceFolder, name);
}

async function fetchSpans(document, signal) {
  const baseUrl = cfg().get('serverUrl', 'http://localhost:8081').replace(/\/$/, '');
  const response = await fetch(`${baseUrl}/spans`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    signal,
    body: JSON.stringify({ path: documentPath(document), content: document.getText() })
  });

  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `${response.status} ${response.statusText}`);
  return normalizeSpans(body);
}

function refreshSignature(document) {
  const configuration = cfg();
  return JSON.stringify({
    enabled: configuration.get('enabled', true),
    languages: configuration.get('languages', DEFAULT_LANGUAGES),
    serverUrl: configuration.get('serverUrl', 'http://localhost:8081'),
    fileName: documentPath(document),
    version: document.version,
    text: document.getText()
  });
}

function spanRange(document, span) {
  const lineIndex = Number(span.line || 1) - 1;
  if (lineIndex < 0 || lineIndex >= document.lineCount) return undefined;

  const line = document.lineAt(lineIndex);
  const start = Math.max(0, Math.min(line.range.end.character, Number(span.start || 0)));
  const end = Math.max(start, Math.min(line.range.end.character, Number(span.end || 0)));
  if (start === end) return undefined;

  return new vscode.Range(new vscode.Position(lineIndex, start), new vscode.Position(lineIndex, end));
}

function uncoveredRanges(document, decoratedRanges) {
  const coveredByLine = Array.from({ length: document.lineCount }, () => []);
  for (const range of decoratedRanges) {
    const lineIndex = range.start.line;
    if (lineIndex < 0 || lineIndex >= document.lineCount) continue;

    const lineEnd = document.lineAt(lineIndex).range.end.character;
    const start = Math.max(0, Math.min(lineEnd, range.start.character));
    const end = Math.max(start, Math.min(lineEnd, range.end.character));
    if (start < end) coveredByLine[lineIndex].push([start, end]);
  }

  const out = [];
  for (let lineIndex = 0; lineIndex < document.lineCount; lineIndex++) {
    const lineEnd = document.lineAt(lineIndex).range.end.character;
    if (lineEnd === 0) continue;

    const ranges = coveredByLine[lineIndex].sort((a, b) => a[0] - b[0] || a[1] - b[1]);
    let coveredUntil = 0;
    for (const [start, end] of ranges) {
      if (start > coveredUntil) out.push(new vscode.Range(new vscode.Position(lineIndex, coveredUntil), new vscode.Position(lineIndex, start)));
      coveredUntil = Math.max(coveredUntil, end);
      if (coveredUntil >= lineEnd) break;
    }

    if (coveredUntil < lineEnd) out.push(new vscode.Range(new vscode.Position(lineIndex, coveredUntil), new vscode.Position(lineIndex, lineEnd)));
  }

  return out;
}

function buildDecorationSpecs(document, spans) {
  const rangesByStyle = new Map();
  const decoratedRanges = [];
  for (const span of spans) {
    const range = spanRange(document, span);
    if (!range) continue;
    if (typeof span.style !== 'string' || !span.style) continue;

    const options = decorationOptions(span.style);
    if (!options) continue;

    decoratedRanges.push(range);
    if (!rangesByStyle.has(span.style)) rangesByStyle.set(span.style, { options, ranges: [] });
    rangesByStyle.get(span.style).ranges.push(range);
  }

  const specs = [];
  const uncovered = uncoveredRanges(document, decoratedRanges);
  if (uncovered.length > 0) {
    specs.push({
      options: {
        rangeBehavior: vscode.DecorationRangeBehavior.ClosedClosed,
        color: '#ffffff',
        fontStyle: 'normal'
      },
      ranges: uncovered
    });
  }

  for (const { options, ranges } of rangesByStyle.values()) {
    specs.push({ options, ranges });
  }

  return specs;
}

async function refreshEditor(editor, force = false) {
  const document = editor.document;
  const signature = refreshSignature(document);
  const state = stateFor(document);
  if (!force && state.signature === signature) {
    applyDecorations(document, state.decorations);
    return;
  }

  const generation = ++state.generation;

  if (!cfg().get('enabled', true) || !isSupportedDocument(document)) {
    clearDocument(document);
    return;
  }

  try {
    state.abort?.abort();
    const abort = new AbortController();
    state.abort = abort;
    const spans = await fetchSpans(document, abort.signal);
    if (state.abort === abort) state.abort = undefined;
    if (generation !== state.generation) return;
    if (document.isClosed || decorationStates.get(documentKey(document)) !== state) return;

    const specs = buildDecorationSpecs(document, spans);
    const newDecorations = specs.map((spec) => ({
      type: vscode.window.createTextEditorDecorationType(spec.options),
      ranges: spec.ranges
    }));

    applyDecorations(document, newDecorations);

    for (const decoration of state.decorations) {
      decoration.type.dispose();
    }
    state.decorations = newDecorations;
    state.signature = signature;

    log('applied decorations', { file: document.fileName, spans: spans.length, decorations: newDecorations.length });
  } catch (err) {
    if (err?.name === 'AbortError') return;
    log('refresh failed', { file: document.fileName, message: err instanceof Error ? err.message : String(err) });
  }
}

function scheduleRefresh(editor = vscode.window.activeTextEditor, delay = 100, force = false) {
  if (!editor) return;
  const state = stateFor(editor.document);
  clearTimeout(state.timer);
  state.abort?.abort();
  state.abort = undefined;
  state.timer = setTimeout(() => void refreshEditor(editor, force), delay);
}

function scheduleVisibleRefreshes(delay = 100, force = false) {
  for (const editor of vscode.window.visibleTextEditors) {
    scheduleRefresh(editor, delay, force);
  }
}

function activate(context) {
  startServerIfNeeded();

  context.subscriptions.push(
    vscode.commands.registerCommand('textSemanticHighlight.toggle', async () => {
      const configuration = cfg();
      await configuration.update('enabled', !configuration.get('enabled', true), vscode.ConfigurationTarget.Global);
      scheduleVisibleRefreshes(0, true);
    }),
    vscode.window.onDidChangeActiveTextEditor((editor) => scheduleRefresh(editor)),
    vscode.workspace.onDidChangeTextDocument((event) => {
      for (const editor of visibleEditorsFor(event.document)) {
        scheduleRefresh(editor, 250, true);
      }
    }),
    vscode.workspace.onDidSaveTextDocument((document) => {
      const editor = vscode.window.visibleTextEditors.find((candidate) => candidate.document === document);
      if (!editor) return;
      scheduleRefresh(editor, 100, true);
      setTimeout(() => {
        if (!editor.document.isClosed) void refreshEditor(editor, true);
      }, 750);
    }),
    vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration('textSemanticHighlight.serverCommand') ||
          event.affectsConfiguration('textSemanticHighlight.serverArgs') ||
          event.affectsConfiguration('textSemanticHighlight.serverCwd') ||
          event.affectsConfiguration('textSemanticHighlight.autoStartServer')) {
        stopServer();
        startServerIfNeeded();
      }
      if (event.affectsConfiguration('textSemanticHighlight')) scheduleVisibleRefreshes(0, true);
    }),
    vscode.workspace.onDidCloseTextDocument(clearDocument),
    { dispose: clearAll },
    { dispose: stopServer },
    output
  );

  scheduleVisibleRefreshes();
}

function deactivate() {
  clearAll();
  stopServer();
}

module.exports = {
  activate,
  deactivate,
  _test: {
    ansiColor,
    buildDecorationSpecs,
    decorationOptions,
    documentPath,
    isSupportedDocument,
    normalizeSpans,
    parseAnsiStyle,
    spanRange,
    uncoveredRanges
  }
};
