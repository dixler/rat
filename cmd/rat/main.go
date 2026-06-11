package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime/pprof"
	"strings"

	"rat/internal/ansihtml"
	"rat/internal/file/scan/golang/goplsclient"
	"rat/internal/highlight"
)

type OutputMode string

const (
	ModeANSI OutputMode = "ansi"
	ModeHTML OutputMode = "html"
)

func ProcessPipeline(filepath string, mode OutputMode) (string, error) {
	program, err := highlight.Analyze(filepath)
	if err != nil {
		return "", err
	}

	ansi := highlight.RenderSource(program)
	if mode == ModeHTML {
		return ansihtml.Convert(ansi), nil
	}
	return ansi, nil
}

func main() {
	if err := run(); err != nil {
		die(err.Error())
	}
}

func run() (err error) {
	defer func() {
		err = errors.Join(err, goplsclient.CloseDefault())
	}()

	serve := flag.Bool("serve", false, "run HTTP server")
	addr := flag.String("addr", ":8081", "server listen addr")
	mode := flag.String("format", string(ModeANSI), "output format: ansi or html")
	cpuprofile := flag.String("cpuprofile", "", "write CPU profile to file")
	callgrind := flag.String("callgrind", "", "write callgrind output for kcachegrind")
	profile := flag.Bool("profile", false, "automatically generate valgrind/callgrind profile (no name needed)")
	flag.Parse()

	if *profile && *callgrind == "" {
		*callgrind = "rat.callgrind"
	}

	stopProfile, err := startCPUProfile(*cpuprofile, *callgrind)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, stopProfile())
	}()

	if *serve {
		runServer(*addr)
		return nil
	}

	if len(flag.Args()) != 1 {
		return fmt.Errorf("usage: rat [-format ansi|html] [-profile] [-cpuprofile file] [-callgrind file] <file>")
	}
	outputMode := OutputMode(*mode)
	if outputMode != ModeANSI && outputMode != ModeHTML {
		return fmt.Errorf("invalid format: expected ansi or html")
	}

	path := flag.Args()[0]
	out, err := ProcessPipeline(path, outputMode)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func startCPUProfile(cpuProfilePath, callgrindPath string) (func() error, error) {
	if cpuProfilePath == "" && callgrindPath == "" {
		return func() error { return nil }, nil
	}

	tempProfile := false
	if cpuProfilePath == "" {
		f, err := os.CreateTemp("", "rat-*.prof")
		if err != nil {
			return nil, fmt.Errorf("create temp CPU profile: %w", err)
		}
		cpuProfilePath = f.Name()
		if err := f.Close(); err != nil {
			_ = os.Remove(cpuProfilePath)
			return nil, fmt.Errorf("close temp CPU profile: %w", err)
		}
		tempProfile = true
	}

	f, err := os.Create(cpuProfilePath)
	if err != nil {
		return nil, fmt.Errorf("create CPU profile: %w", err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		if tempProfile {
			_ = os.Remove(cpuProfilePath)
		}
		return nil, fmt.Errorf("start CPU profile: %w", err)
	}

	return func() error {
		var stopErr error

		pprof.StopCPUProfile()
		if err := f.Close(); err != nil {
			stopErr = errors.Join(stopErr, fmt.Errorf("close CPU profile: %w", err))
		}
		if callgrindPath != "" {
			if err := writeCallgrind(callgrindPath, cpuProfilePath); err != nil {
				stopErr = errors.Join(stopErr, err)
			}
		}
		if tempProfile {
			if err := os.Remove(cpuProfilePath); err != nil {
				stopErr = errors.Join(stopErr, fmt.Errorf("remove temp CPU profile: %w", err))
			}
		}

		return stopErr
	}, nil
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

func die(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
