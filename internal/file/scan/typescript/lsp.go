package typescript

import (
	"fmt"
	"path/filepath"
	"strings"

	"rat/internal/file/scan"
	"rat/internal/lspclient"
	"rat/internal/tsgobin"

	treesitter "github.com/tree-sitter/go-tree-sitter"
)

func defaultLSPClient(file string) *lspclient.Client {
	command, err := tsgobin.Path()
	if err != nil {
		return nil
	}
	languageID := "typescript"
	switch strings.ToLower(filepath.Ext(file)) {
	case ".js", ".jsx":
		languageID = "javascript"
	}
	client, err := lspclient.Start(lspclient.Config{
		Name:       "typescript",
		Command:    command,
		Args:       []string{"--lsp", "--stdio"},
		LanguageID: languageID,
	})
	if err != nil {
		return nil
	}
	return client
}

func (b *typescriptBuilder) definitionFor(line, column int) (definitionLocation, bool) {
	key := fmt.Sprintf("%d:%d", line, column)
	if cached, ok := b.defsByPos[key]; ok {
		return cached, scan.HasLocation(cached)
	}
	if b.client == nil {
		b.defsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	target, ok, err := b.client.DefinitionInSyncedDocument(b.file, line, column)
	if err != nil || !ok {
		b.defsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	loc := scan.Location{File: target.File, Line: target.Line, Column: target.Column}
	b.defsByPos[key] = loc
	return loc, scan.HasLocation(loc)
}

func (b *typescriptBuilder) definitionForNode(node *treesitter.Node) (definitionLocation, bool) {
	if node == nil {
		return definitionLocation{}, false
	}
	start := node.StartPosition()
	end := node.EndPosition()
	line := int(start.Row) + 1
	startCol := int(start.Column) + 1
	if loc, ok := b.definitionFor(line, startCol); ok {
		return loc, true
	}
	if start.Row != end.Row {
		return definitionLocation{}, false
	}
	for col := startCol + 1; col <= int(end.Column); col++ {
		if loc, ok := b.definitionFor(line, col); ok {
			return loc, true
		}
	}
	return definitionLocation{}, false
}
