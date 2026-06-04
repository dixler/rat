package typescript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rat/internal/file/scan"
	"rat/internal/lspclient"
)

func defaultLSPClient(file string) *lspclient.Client {
	command := strings.TrimSpace(os.Getenv("RAT_TYPESCRIPT_LSP"))
	if command == "" {
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
		Args:       strings.Fields(os.Getenv("RAT_TYPESCRIPT_LSP_ARGS")),
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
		return cached, cached.OK
	}
	if b.client == nil {
		b.defsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	target, ok, err := b.client.Definition(b.file, line, column)
	if err != nil || !ok {
		b.defsByPos[key] = definitionLocation{}
		return definitionLocation{}, false
	}
	loc := scan.NewDefinitionLocation(target.File, target.Line, target.Column)
	b.defsByPos[key] = loc
	return loc, loc.OK
}
