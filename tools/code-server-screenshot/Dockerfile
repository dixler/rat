FROM codercom/code-server:latest

USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl golang-go \
    && rm -rf /var/lib/apt/lists/*

USER coder
WORKDIR /home/coder/rat
COPY --chown=coder:coder . /home/coder/rat
COPY --chown=coder:coder rat /home/coder/bin/rat
COPY --chown=coder:coder text-semantic-highlight-0.0.4.vsix /home/coder/text-semantic-highlight-0.0.4.vsix

RUN mkdir -p /home/coder/bin \
    && chmod +x /home/coder/bin/rat \
    && code-server --install-extension /home/coder/text-semantic-highlight-0.0.4.vsix

RUN mkdir -p /home/coder/.local/share/code-server/User \
    && printf '%s\n' \
      '{' \
      '  "workbench.colorTheme": "Default Dark Modern",' \
      '  "workbench.startupEditor": "none",' \
      '  "workbench.editor.showTabs": "single",' \
      '  "workbench.activityBar.location": "hidden",' \
      '  "workbench.sideBar.location": "left",' \
      '  "workbench.panel.defaultLocation": "bottom",' \
      '  "workbench.statusBar.visible": false,' \
      '  "window.commandCenter": false,' \
      '  "window.menuBarVisibility": "hidden",' \
      '  "editor.fontFamily": "JetBrains Mono, Fira Code, SF Mono, Menlo, Consolas, DejaVu Sans Mono, monospace",' \
      '  "editor.fontSize": 14,' \
      '  "editor.lineHeight": 20,' \
      '  "editor.minimap.enabled": false,' \
      '  "editor.scrollBeyondLastLine": false,' \
      '  "editor.renderWhitespace": "none",' \
      '  "editor.guides.indentation": false,' \
      '  "editor.stickyScroll.enabled": false,' \
      '  "editor.semanticHighlighting.enabled": false,' \
      '  "breadcrumbs.enabled": false,' \
      '  "outline.showFiles": false,' \
      '  "explorer.compactFolders": false,' \
      '  "remote.autoForwardPorts": false,' \
      '  "textSemanticHighlight.autoStartServer": true,' \
      '  "textSemanticHighlight.serverCommand": "/home/coder/bin/rat",' \
      '  "textSemanticHighlight.serverArgs": ["--serve", "--addr", ":8081"],' \
      '  "textSemanticHighlight.serverCwd": "/home/coder/rat"' \
      '}' > /home/coder/.local/share/code-server/User/settings.json

COPY --chown=coder:coder tools/code-server-screenshot/start.sh /home/coder/start-rat-code-server.sh
RUN chmod +x /home/coder/start-rat-code-server.sh

EXPOSE 8080
ENTRYPOINT ["/home/coder/start-rat-code-server.sh"]
