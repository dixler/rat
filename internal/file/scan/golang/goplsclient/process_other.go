//go:build !linux

package goplsclient

import "os/exec"

func configureChildProcess(cmd *exec.Cmd) {}
