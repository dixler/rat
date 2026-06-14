package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime/pprof"
	"strings"
)

type profiler struct {
	callgrindPath string
	file          *os.File
}

func (p *profiler) Start() error {
	if os.Getenv("PROFILE") != "1" {
		return nil
	}

	f, err := os.CreateTemp("", "rat-*.prof")
	if err != nil {
		return fmt.Errorf("create temp CPU profile: %w", err)
	}
	cpuProfilePath := f.Name()
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		os.Remove(cpuProfilePath)
		return fmt.Errorf("start CPU profile: %w", err)
	}
	p.file = f
	return nil
}

func (p *profiler) Stop() error {
	if p.file == nil {
		return nil
	}

	pprof.StopCPUProfile()
	f, err := os.CreateTemp(".", "rat-*.callgrind")
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err := writeCallgrind(f.Name(), p.file.Name()); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\ngenerated profile. to view profile:\n")
	fmt.Fprintf(os.Stderr, "\tkcachegrind %s\n", f.Name())
	return nil
}

func writeCallgrind(callgrindPath, cpuProfilePath string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable for callgrind conversion: %w", err)
	}

	cmd := exec.Command("go", "tool", "pprof", "-callgrind", "-output", callgrindPath, executable, cpuProfilePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("convert CPU profile to callgrind: %w", err)
		}
		return fmt.Errorf("convert CPU profile to callgrind: %w: %s", err, message)
	}

	return nil
}
