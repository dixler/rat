const assert = require('node:assert/strict');
const cp = require('node:child_process');
const fs = require('node:fs');
const http = require('node:http');
const Module = require('node:module');
const net = require('node:net');
const path = require('node:path');
const test = require('node:test');

const repoRoot = path.resolve(__dirname, '..', '..');
const fixturesRoot = path.join(repoRoot, 'testdata', 'rat');

const originalLoad = Module._load;
Module._load = function load(request, parent, isMain) {
  if (request === 'vscode') return vscodeMock;
  return originalLoad.call(this, request, parent, isMain);
};

const vscodeMock = {
  DecorationRangeBehavior: { ClosedClosed: 'ClosedClosed' },
  Position: class Position {
    constructor(line, character) {
      this.line = line;
      this.character = character;
    }
  },
  Range: class Range {
    constructor(start, end) {
      this.start = start;
      this.end = end;
    }
  },
  window: {
    createOutputChannel() {
      return { appendLine() {}, dispose() {} };
    },
    createTextEditorDecorationType(options) {
      return { options, dispose() {} };
    },
    visibleTextEditors: []
  },
  workspace: {
    getConfiguration() {
      return { get(_key, fallback) { return fallback; } };
    }
  }
};

const extension = require('../extension')._test;

function walkGoFiles(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const entryPath = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walkGoFiles(entryPath));
    if (entry.isFile() && entry.name.endsWith('.go')) out.push(entryPath);
  }
  return out.sort();
}

function makeDocument(filePath, source) {
  const lines = source.split('\n');
  return {
    fileName: filePath,
    lineCount: lines.length,
    lineAt(lineIndex) {
      return { text: lines[lineIndex], range: { end: { character: lines[lineIndex].length } } };
    }
  };
}

function parseSnapshot(snapshotPath, source) {
  const sourceLines = source.split('\n');
  const expected = [];
  const snapshotLines = fs.readFileSync(snapshotPath, 'utf8').split('\n').filter((line) => line !== '');
  const linePrefix = /^ \x1b\[0m\s*(\d+) /;

  for (const line of snapshotLines) {
    const match = linePrefix.exec(line);
    if (!match) continue;

    const lineIndex = Number(match[1]) - 1;
    const sourceLine = sourceLines[lineIndex] || '';
    const content = line.slice(match[0].length);
    expected.push(...parseAnsiContent(lineIndex, sourceLine, content));
  }

  return expected;
}

function parseAnsiContent(lineIndex, sourceLine, content) {
  const out = [];
  let currentStyle = '';
  let sourceOffset = 0;
  let cursor = 0;
  const sgr = /\x1b\[([0-9;]+)m/g;

  for (const match of content.matchAll(sgr)) {
    const text = content.slice(cursor, match.index);
    if (text) {
      const consumed = consumeDisplayedSource(sourceLine, sourceOffset, text);
      if (currentStyle) {
        out.push({
          line: lineIndex,
          start: sourceOffset,
          end: sourceOffset + consumed,
          options: extension.decorationOptions(currentStyle)
        });
      }
      sourceOffset += consumed;
    }

    currentStyle = match[1] === '0' ? '' : `${currentStyle}\x1b[${match[1]}m`;
    cursor = match.index + match[0].length;
  }

  const text = content.slice(cursor);
  if (text) {
    const consumed = consumeDisplayedSource(sourceLine, sourceOffset, text);
    if (currentStyle) {
      out.push({
        line: lineIndex,
        start: sourceOffset,
        end: sourceOffset + consumed,
        options: extension.decorationOptions(currentStyle)
      });
    }
  }

  return out.filter((segment) => segment.options);
}

function consumeDisplayedSource(sourceLine, sourceOffset, displayed) {
  let rendered = '';
  let consumed = 0;
  while (rendered.length < displayed.length && sourceOffset + consumed < sourceLine.length) {
    const ch = sourceLine[sourceOffset + consumed];
    rendered += ch === '\t' ? '    ' : ch;
    consumed++;
  }
  assert.equal(rendered, displayed);
  return consumed;
}

function canonicalSegments(segments) {
  return segments
    .map((segment) => ({
      line: segment.line,
      start: segment.start,
      end: segment.end,
      options: normalizeOptions(segment.options)
    }))
    .sort((a, b) => a.line - b.line || a.start - b.start || a.end - b.end || JSON.stringify(a.options).localeCompare(JSON.stringify(b.options)));
}

function actualSegments(document, spans) {
  return extension.buildDecorationSpecs(document, extension.normalizeSpans({ spans })).flatMap((spec) =>
    spec.ranges.map((range) => ({
      line: range.start.line,
      start: range.start.character,
      end: range.end.character,
      options: spec.options
    }))
  );
}

function normalizeOptions(options) {
  const normalized = { ...options };
  delete normalized.rangeBehavior;
  return normalized;
}

function fetchSpans(port, filePath) {
  return new Promise((resolve, reject) => {
    const body = JSON.stringify({ path: filePath });
    const req = http.request({
      hostname: '127.0.0.1',
      port,
      path: '/spans',
      method: 'POST',
      headers: { 'content-type': 'application/json', 'content-length': Buffer.byteLength(body) }
    }, (res) => {
      let data = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => {
        const parsed = JSON.parse(data || '{}');
        if (res.statusCode !== 200) reject(new Error(parsed.error || `HTTP ${res.statusCode}`));
        else resolve(parsed.spans || {});
      });
    });
    req.on('error', reject);
    req.end(body);
  });
}

async function waitForServer(port) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    try {
      await fetchSpans(port, path.join(fixturesRoot, 'default', 'sample.go'));
      return;
    } catch (_err) {
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
  throw new Error('rat server did not become ready');
}

function freePort() {
  return new Promise((resolve, reject) => {
    const server = net.createServer();
    server.listen(0, '127.0.0.1', () => {
      const { port } = server.address();
      server.close(() => resolve(port));
    });
    server.on('error', reject);
  });
}

test('VS Code decorations match CLI highlight snapshots for rat fixtures', async (t) => {
  const port = await freePort();
  const server = cp.spawn('go', ['run', './cmd/rat', '--serve', '--addr', `127.0.0.1:${port}`], {
    cwd: repoRoot,
    detached: true,
    stdio: ['ignore', 'ignore', 'pipe']
  });
  t.after(() => {
    try {
      process.kill(-server.pid, 'SIGKILL');
    } catch (_err) {
      server.kill('SIGKILL');
    }
  });

  let stderr = '';
  server.stderr.on('data', (chunk) => { stderr += chunk.toString(); });
  server.on('exit', (code) => {
    if (code && code !== null) stderr += `rat server exited with ${code}\n`;
  });

  await waitForServer(port);

  for (const sourcePath of walkGoFiles(fixturesRoot)) {
    const rel = path.relative(fixturesRoot, sourcePath);
    await t.test(rel, async () => {
      const source = fs.readFileSync(sourcePath, 'utf8');
      const document = makeDocument(sourcePath, source);
      const spans = await fetchSpans(port, sourcePath);
      const expected = parseSnapshot(`${sourcePath}.out`, source);
      const actual = actualSegments(document, spans);

      assert.deepEqual(canonicalSegments(actual), canonicalSegments(expected), stderr);
    });
  }
});
