package goplsclient

import (
	"fmt"
	"os/exec"
	"sync"

	"rat/internal/goplsbin"
	"rat/internal/lspclient"
)

type Location = lspclient.Location

type Client struct {
	*lspclient.Client
}

var (
	defaultOnce sync.Once
	defaultInst *Client
	defaultErr  error
)

func Default() (*Client, error) {
	defaultOnce.Do(func() {
		defaultInst, defaultErr = start()
	})
	return defaultInst, defaultErr
}

func start() (*Client, error) {
	goplsBin, err := resolveGoplsBinary()
	if err != nil {
		return nil, err
	}
	client, err := lspclient.Start(lspclient.Config{Name: "gopls", Command: goplsBin, Args: []string{"serve"}, LanguageID: "go"})
	if err != nil {
		return nil, err
	}
	return &Client{Client: client}, nil
}

func resolveGoplsBinary() (string, error) {
	if path, err := goplsbin.Path(); err == nil {
		return path, nil
	}
	path, err := exec.LookPath("gopls")
	if err != nil {
		return "", fmt.Errorf("gopls not found; set GOPLS_BIN, build the embedded gopls artifact, or include gopls in PATH")
	}
	return path, nil
}
