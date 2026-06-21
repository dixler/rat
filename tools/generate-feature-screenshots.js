#!/usr/bin/env node

const cp = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..');
const sourcePath = path.join(repoRoot, 'testdata', 'go', 'default', 'features_showcase.go');
const showcaseLineCount = fs.readFileSync(sourcePath, 'utf8').split('\n').length;
const imageDir = path.join(repoRoot, '.images');
const cliPng = path.join(imageDir, 'cli.png');
const vscodePng = path.join(imageDir, 'vscode.png');
const codeServerImage = 'rat-code-server-screenshot:local';

main().catch((err) => {
  console.error(err?.stack || err);
  process.exit(1);
});

async function main() {
  fs.mkdirSync(imageDir, { recursive: true });

  renderCliScreenshot(cliPng);
  await renderVSCodeScreenshot(vscodePng);
  console.log(`wrote ${path.relative(repoRoot, cliPng)}`);
  console.log(`wrote ${path.relative(repoRoot, vscodePng)}`);
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", `'\\''`)}'`;
}

function renderCliScreenshot(pngPath) {
  const asciinema = findCommand('asciinema');
  const agg = findCommand('agg');
  const convert = findCommand('magick') || findCommand('convert');
  if (!asciinema) throw new Error('asciinema is required to generate .images/cli.png');
  if (!agg) throw new Error('agg is required to render asciinema recordings. Install it with: cargo install --git https://github.com/asciinema/agg');
  if (!convert) throw new Error('ImageMagick convert or magick is required to convert agg GIF output to PNG');

  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'rat-asciinema-'));
  const castPath = path.join(tmp, 'cli.cast');
  const gifPath = path.join(tmp, 'cli.gif');
  try {
    const command = ['go', 'run', './cmd/rat', sourcePath].map(shellQuote).join(' ');
    cp.execFileSync(asciinema, [
      'record',
      '--quiet',
      '--headless',
      '--overwrite',
      '--return',
      '--window-size', '140x70',
      '--command', command,
      castPath
    ], { cwd: repoRoot, env: { ...process.env, TERM: 'xterm-256color', NO_COLOR: undefined }, stdio: 'pipe' });
    cp.execFileSync(agg, [
      '--theme', 'asciinema',
      '--font-size', '15',
      '--line-height', '1.45',
      '--idle-time-limit', '0.1',
      '--last-frame-duration', '0.1',
      '--select', '100%',
      castPath,
      gifPath
    ], { stdio: 'pipe' });
    if (path.basename(convert) === 'magick') cp.execFileSync(convert, [gifPath + '[0]', pngPath], { stdio: 'pipe' });
    else cp.execFileSync(convert, [gifPath + '[0]', pngPath], { stdio: 'pipe' });
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

async function renderVSCodeScreenshot(pngPath) {
  const docker = findCommand('docker');
  if (!docker) throw new Error('docker is required to generate .images/vscode.png with code-server');

  buildLocalArtifacts();

  cp.execFileSync(docker, [
    'build',
    '-t', codeServerImage,
    '-f', path.join('tools', 'code-server-screenshot', 'Dockerfile'),
    '.'
  ], { cwd: repoRoot, stdio: 'inherit' });

  const port = await freePort();
  const name = `rat-code-server-screenshot-${process.pid}`;
  try {
    cp.execFileSync(docker, [
      'run',
      '--rm',
      '--detach',
      '--name', name,
      '-p', `127.0.0.1:${port}:8080`,
      codeServerImage
    ], { stdio: 'pipe' });
    await waitForCodeServer(port, docker, name);
    await screenshotCodeServer(findBrowser(), `http://127.0.0.1:${port}/?folder=/home/coder/rat`, pngPath, docker, name);
  } finally {
    cp.spawnSync(docker, ['rm', '-f', name], { stdio: 'ignore' });
  }
}

function buildLocalArtifacts() {
  cp.execFileSync('go', ['build', './cmd/rat'], { cwd: repoRoot, stdio: 'inherit' });
  cp.execFileSync('npm', ['ci', '--no-audit', '--no-fund', '--loglevel=error'], { cwd: path.join(repoRoot, 'vscode-text-semantic'), stdio: 'inherit' });
  cp.execFileSync('npm', ['run', 'build'], { cwd: path.join(repoRoot, 'vscode-text-semantic'), stdio: 'inherit' });
  fs.copyFileSync(
    path.join(repoRoot, 'vscode-text-semantic', 'text-semantic-highlight-0.0.4.vsix'),
    path.join(repoRoot, 'text-semantic-highlight-0.0.4.vsix')
  );
}

function freePort() {
  return new Promise((resolve, reject) => {
    const server = require('node:net').createServer();
    server.listen(0, '127.0.0.1', () => {
      const port = server.address().port;
      server.close(() => resolve(port));
    });
    server.on('error', reject);
  });
}

async function waitForCodeServer(port, docker, name) {
  const started = Date.now();
  while (Date.now() - started < 60000) {
    const running = cp.spawnSync(docker, ['inspect', '-f', '{{.State.Running}}', name], { encoding: 'utf8' });
    if (running.status !== 0 || running.stdout.trim() !== 'true') {
      const logs = cp.spawnSync(docker, ['logs', name], { encoding: 'utf8' });
      throw new Error(`code-server container exited:\n${logs.stdout}${logs.stderr}`);
    }
    try {
      await httpGet(`http://127.0.0.1:${port}/healthz`);
      return;
    } catch (_err) {
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
  }
  const logs = cp.spawnSync(docker, ['logs', name], { encoding: 'utf8' });
  throw new Error(`timed out waiting for code-server:\n${logs.stdout}${logs.stderr}`);
}

function httpGet(url) {
  return new Promise((resolve, reject) => {
    const http = require('node:http');
    const req = http.get(url, (res) => {
      let data = '';
      res.setEncoding('utf8');
      res.on('data', (chunk) => { data += chunk; });
      res.on('end', () => {
        if (res.statusCode < 200 || res.statusCode >= 300) {
          reject(new Error(`${url} returned ${res.statusCode}: ${data}`));
          return;
        }
        resolve(data);
      });
    });
    req.on('error', reject);
  });
}

function findBrowser() {
  for (const command of ['chromium', 'chromium-browser', 'google-chrome', 'google-chrome-stable', 'microsoft-edge']) {
    const found = findCommand(command);
    if (found) return found;
  }
  throw new Error('could not find chromium, chrome, or edge for screenshots');
}

function findCommand(command) {
  const result = cp.spawnSync('which', [command], { encoding: 'utf8' });
  if (result.status !== 0) return undefined;
  return result.stdout.trim();
}

async function screenshotCodeServer(browser, url, pngPath, docker, name) {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'rat-code-server-browser-'));
  const debugPort = await freePort();
  const chrome = cp.spawn(browser, [
    '--headless=new',
    '--disable-gpu',
    '--no-sandbox',
    '--disable-dev-shm-usage',
    `--user-data-dir=${path.join(tmp, 'profile')}`,
    `--remote-debugging-port=${debugPort}`,
    '--window-size=1440,1600',
    url
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  let stderr = '';
  chrome.stderr.on('data', (chunk) => { stderr += chunk; });
  try {
    const client = await connectChrome(debugPort, chrome, () => stderr);
    try {
      await client.send('Runtime.enable');
      await client.send('Page.enable');
      await waitForWorkbench(client);
      await closeWorkbenchChrome(client);
      await openShowcaseFile(client);
      await waitForExtensionApplied(docker, name);
      await forceEditorRepaint(client);
      await closeWorkbenchChrome(client);
      await client.send('Runtime.evaluate', { expression: 'document.body.style.background = "#1e1e1e"' });
      const clip = await editorClip(client);
      const result = await client.send('Page.captureScreenshot', { format: 'png', fromSurface: true, captureBeyondViewport: false, clip });
      fs.writeFileSync(pngPath, Buffer.from(result.data, 'base64'));
    } finally {
      client.close();
    }
  } finally {
    chrome.kill('SIGTERM');
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

async function waitForExtensionApplied(docker, name) {
  const started = Date.now();
  while (Date.now() - started < 60000) {
    const result = cp.spawnSync(docker, ['exec', name, 'bash', '-lc', `grep -R "applied decorations.*features_showcase.go" /home/coder/.local/share/code-server/logs/*/exthost*/output_logging_*/*Text\\ Semantic\\ Highlight.log >/dev/null 2>&1`]);
    if (result.status === 0) {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  const logs = cp.spawnSync(docker, ['exec', name, 'bash', '-lc', `cat /home/coder/.local/share/code-server/logs/*/exthost*/output_logging_*/*Text\\ Semantic\\ Highlight.log 2>/dev/null || true`], { encoding: 'utf8' });
  throw new Error(`timed out waiting for VS Code extension decorations:\n${logs.stdout}${logs.stderr}`);
}

async function forceEditorRepaint(client) {
  await client.send('Runtime.evaluate', {
    expression: `(() => {
      const editor = document.querySelector('.monaco-editor');
      editor?.focus();
      const scrollable = document.querySelector('.monaco-scrollable-element .scrollbar.vertical') || document.querySelector('.monaco-editor .overflow-guard');
      if (scrollable) scrollable.dispatchEvent(new WheelEvent('wheel', { deltaY: 1, bubbles: true }));
      window.dispatchEvent(new Event('resize'));
      return true;
    })()`,
    returnByValue: true
  });
  await new Promise((resolve) => setTimeout(resolve, 1000));
}

async function editorClip(client) {
  const result = await client.send('Runtime.evaluate', {
    expression: `(() => {
      const group = document.querySelector('.editor-group-container') || document.querySelector('.part.editor') || document.body;
      const rect = group.getBoundingClientRect();
      return { x: rect.left, y: rect.top, width: rect.width, height: rect.height };
    })()`,
    returnByValue: true
  });
  const rect = result.result?.value || { x: 0, y: 0, width: 1440, height: 1457 };
  return {
    x: Math.max(0, Math.floor(rect.x)),
    y: Math.max(0, Math.floor(rect.y)),
    width: Math.max(1, Math.ceil(rect.width)),
    height: Math.max(1, Math.ceil(rect.height)),
    scale: 1
  };
}

async function closeWorkbenchChrome(client) {
  await client.send('Runtime.evaluate', {
    expression: `(() => {
      const run = (id) => window.require?.('vs/platform/commands/common/commands')?.CommandsRegistry?.getCommand(id)?.handler?.({ get() { return undefined; } });
      try { run('workbench.action.closeSidebar'); } catch (_) {}
      try { run('workbench.action.closeAuxiliaryBar'); } catch (_) {}
      try { run('workbench.action.closePanel'); } catch (_) {}
      document.body.classList.add('rat-screenshot-clean');
      const style = document.createElement('style');
      style.textContent = '.part.sidebar, .part.auxiliarybar, .part.panel, .activitybar { display: none !important; } .part.editor { left: 0 !important; right: 0 !important; bottom: 0 !important; }';
      document.head.appendChild(style);
      return true;
    })()`,
    returnByValue: true
  });
  await new Promise((resolve) => setTimeout(resolve, 500));
}

async function connectChrome(port, child, stderr) {
  const started = Date.now();
  while (Date.now() - started < 30000) {
    if (child.exitCode !== null) throw new Error(`chromium exited before DevTools was ready:\n${stderr()}`);
    try {
      const tabs = JSON.parse(await httpGet(`http://127.0.0.1:${port}/json`));
      const tab = tabs.find((candidate) => candidate.type === 'page' && candidate.webSocketDebuggerUrl);
      if (tab) return new CDPClient(tab.webSocketDebuggerUrl);
    } catch (_err) {
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
  throw new Error(`timed out waiting for Chromium DevTools:\n${stderr()}`);
}

async function waitForWorkbench(client) {
  const started = Date.now();
  while (Date.now() - started < 90000) {
    const result = await client.send('Runtime.evaluate', {
      expression: `Boolean(document.querySelector('.monaco-workbench') && document.querySelector('.monaco-editor .view-lines'))`,
      returnByValue: true
    });
    if (result.result?.value === true) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error('timed out waiting for VS Code workbench to render');
}

async function openShowcaseFile(client) {
  await client.send('Input.dispatchKeyEvent', { type: 'keyDown', windowsVirtualKeyCode: 17, nativeVirtualKeyCode: 17, key: 'Control', code: 'ControlLeft', modifiers: 2 });
  await client.send('Input.dispatchKeyEvent', { type: 'keyDown', windowsVirtualKeyCode: 80, nativeVirtualKeyCode: 80, key: 'p', code: 'KeyP', modifiers: 2 });
  await client.send('Input.dispatchKeyEvent', { type: 'keyUp', windowsVirtualKeyCode: 80, nativeVirtualKeyCode: 80, key: 'p', code: 'KeyP', modifiers: 2 });
  await client.send('Input.dispatchKeyEvent', { type: 'keyUp', windowsVirtualKeyCode: 17, nativeVirtualKeyCode: 17, key: 'Control', code: 'ControlLeft' });
  await new Promise((resolve) => setTimeout(resolve, 800));
  await client.send('Runtime.evaluate', {
    expression: `(() => {
      const input = document.querySelector('.quick-input-widget input');
      if (!input) return false;
      input.focus();
      input.value = 'testdata/go/default/features_showcase.go';
      input.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText', data: input.value }));
      return true;
    })()`,
    returnByValue: true
  });
  await new Promise((resolve) => setTimeout(resolve, 1200));
  await client.send('Input.dispatchKeyEvent', { type: 'keyDown', windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13, key: 'Enter', code: 'Enter' });
  await client.send('Input.dispatchKeyEvent', { type: 'keyUp', windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13, key: 'Enter', code: 'Enter' });
  await waitForShowcaseEditor(client);
  await moveCursorToLastLine(client);
  await new Promise((resolve) => setTimeout(resolve, 3000));
  await waitForRatDecorations(client);
}

async function moveCursorToLastLine(client) {
  await client.send('Runtime.evaluate', {
    expression: `(() => {
      document.querySelector('.monaco-editor textarea')?.focus();
      return true;
    })()`,
    returnByValue: true
  });
  await client.send('Input.dispatchKeyEvent', { type: 'keyDown', windowsVirtualKeyCode: 17, nativeVirtualKeyCode: 17, key: 'Control', code: 'ControlLeft', modifiers: 2 });
  await client.send('Input.dispatchKeyEvent', { type: 'keyDown', windowsVirtualKeyCode: 35, nativeVirtualKeyCode: 35, key: 'End', code: 'End', modifiers: 2 });
  await client.send('Input.dispatchKeyEvent', { type: 'keyUp', windowsVirtualKeyCode: 35, nativeVirtualKeyCode: 35, key: 'End', code: 'End', modifiers: 2 });
  await client.send('Input.dispatchKeyEvent', { type: 'keyUp', windowsVirtualKeyCode: 17, nativeVirtualKeyCode: 17, key: 'Control', code: 'ControlLeft' });
  await waitForLastLineVisible(client);
}

async function waitForLastLineVisible(client) {
  const started = Date.now();
  while (Date.now() - started < 10000) {
    const result = await client.send('Runtime.evaluate', {
      expression: `(() => {
        const lineNumbers = [...document.querySelectorAll('.monaco-editor .line-numbers')]
          .map((node) => Number((node.textContent || '').trim()))
          .filter(Number.isFinite);
        return { visibleMax: Math.max(0, ...lineNumbers), count: lineNumbers.length };
      })()`,
      returnByValue: true
    });
    const value = result.result?.value || {};
    if (value.visibleMax >= showcaseLineCount) {
      await new Promise((resolve) => setTimeout(resolve, 1000));
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error('failed to reveal the last line before screenshot');
}

async function waitForShowcaseEditor(client) {
  const started = Date.now();
  while (Date.now() - started < 20000) {
    const result = await client.send('Runtime.evaluate', {
      expression: `(() => {
        const active = document.querySelector('.tabs-container .tab.active, .editor-group-container .title .monaco-icon-label');
        const labels = [...document.querySelectorAll('.monaco-icon-label, .tab-label')].map((node) => node.textContent || '');
        return labels.some((label) => label.includes('features_showcase.go')) || Boolean(active && active.textContent.includes('features_showcase.go'));
      })()`,
      returnByValue: true
    });
    if (result.result?.value === true) return;
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
}

async function waitForRatDecorations(client) {
  const started = Date.now();
  while (Date.now() - started < 20000) {
    const result = await client.send('Runtime.evaluate', {
      expression: `(() => {
        const title = document.querySelector('.monaco-icon-label.file-icon.go-name-file-icon.features_showcase-name-file-icon')
          || [...document.querySelectorAll('.monaco-icon-label')].find((node) => node.textContent.includes('features_showcase.go'));
        const tokens = [...document.querySelectorAll('.monaco-editor .view-line span')];
        const computed = tokens.map((node) => getComputedStyle(node));
        const colors = new Set(computed.map((style) => style.color).filter(Boolean));
        const backgrounds = computed.filter((style) => style.backgroundColor && style.backgroundColor !== 'rgba(0, 0, 0, 0)').length;
        const decorations = computed.filter((style) => style.textDecorationLine && style.textDecorationLine !== 'none').length;
        const weights = computed.filter((style) => Number(style.fontWeight) >= 700).length;
        return { title: Boolean(title), colors: colors.size, backgrounds, decorations, weights };
      })()`,
      returnByValue: true
    });
    const value = result.result?.value || {};
    if (value.title && (value.colors > 8 || value.backgrounds > 0 || value.decorations > 0 || value.weights > 0)) {
      await new Promise((resolve) => setTimeout(resolve, 2000));
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
  throw new Error('timed out waiting for rat VS Code decorations');
}

class CDPClient {
  constructor(url) {
    this.nextID = 1;
    this.pending = new Map();
    this.socket = new WebSocket(url);
    this.ready = new Promise((resolve, reject) => {
      this.socket.addEventListener('open', resolve, { once: true });
      this.socket.addEventListener('error', reject, { once: true });
    });
    this.socket.addEventListener('message', (event) => {
      const message = JSON.parse(event.data);
      if (!message.id) return;
      const pending = this.pending.get(message.id);
      if (!pending) return;
      this.pending.delete(message.id);
      if (message.error) pending.reject(new Error(message.error.message));
      else pending.resolve(message.result || {});
    });
  }

  async send(method, params = {}) {
    await this.ready;
    const id = this.nextID++;
    this.socket.send(JSON.stringify({ id, method, params }));
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
    });
  }

  close() {
    this.socket.close();
  }
}
