package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Subject struct {
	LineRef string   `json:"line_ref"`
	Text    string   `json:"text"`
	Blames  []string `json:"blames"`
}

type Metadata struct {
	Author string `json:"author"`
}

type Note struct {
	File     string   `json:"file"`
	Subject  Subject  `json:"subject"`
	NoteText string   `json:"note"`
	Metadata Metadata `json:"metadata"`
}

type EditItem struct {
	Orig       Note
	Curr       Note
	Action     string
	OrigBody   string
	EditedBody string
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "add":
		exitIf(addCmd(os.Args[2:]))
	case "review":
		exitIf(reviewCmd(os.Args[2:]))
	case "install-hook":
		exitIf(installHook())
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: notectl <add|review|install-hook>")
	os.Exit(2)
}

func addCmd(args []string) error {
	if len(args) < 3 {
		return errors.New("usage: notectl add <file> <Lstart[+n]> <note text>")
	}
	file := filepath.Clean(args[0])
	ref := args[1]
	noteText := strings.Join(args[2:], " ")
	start, cnt, err := parseLineRef(ref)
	if err != nil {
		return err
	}
	lines, err := readLines(file)
	if err != nil {
		return err
	}
	seq, err := sliceRange(lines, start, cnt)
	if err != nil {
		return err
	}
	blames, err := blameRange(file, start, cnt)
	if err != nil {
		return err
	}
	author := strings.TrimSpace(runOut("git", "config", "user.name"))
	n := Note{File: file, Subject: Subject{LineRef: normalizeLineRef(start, cnt), Text: strings.Join(seq, "\n"), Blames: blames}, NoteText: noteText, Metadata: Metadata{Author: author}}
	notes, _ := loadNotesForFile(file)
	notes = append(notes, n)
	if err := writeNotesForFile(file, notes); err != nil {
		return err
	}
	return gitAdd(notePath(file))
}

func reviewCmd(args []string) error {
	staged := len(args) == 1 && args[0] == "--staged"
	if !staged {
		return errors.New("usage: notectl review --staged")
	}
	changes, err := stagedChanges()
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	if err := handleRenames(changes); err != nil {
		return err
	}
	targetFiles := changedTargets(changes)
	var reviewItems []EditItem
	original := map[string][]Note{}
	updated := map[string][]Note{}
	for _, f := range targetFiles {
		notes, _ := loadNotesForFile(f)
		if len(notes) == 0 {
			continue
		}
		orig := cloneNotes(notes)
		if isDeleted(changes, f) {
			for _, n := range notes {
				reviewItems = append(reviewItems, EditItem{Orig: n, Curr: n, Action: "accept"})
			}
			original[f] = orig
			updated[f] = notes
			continue
		}
		lines, err := readLines(f)
		if err != nil {
			continue
		}
		newNotes := make([]Note, len(notes))
		copy(newNotes, notes)
		for i, n := range notes {
			curr, found, changed := relocateNote(n, f, lines)
			if !found {
				reviewItems = append(reviewItems, EditItem{Orig: n, Curr: n, Action: "accept"})
				continue
			}
			newNotes[i] = curr
			if changed {
				reviewItems = append(reviewItems, EditItem{Orig: n, Curr: curr, Action: "update"})
			}
		}
		original[f] = orig
		updated[f] = newNotes
	}
	if len(reviewItems) == 0 {
		for f, notes := range updated {
			_ = writeNotesForFile(f, notes)
			_ = gitAdd(notePath(f))
		}
		return nil
	}
	edited, err := editReviewLoop(reviewItems, original, updated)
	if err != nil {
		return err
	}
	if edited == "accept" || edited == "abort" {
		for f, notes := range updated {
			if err := writeNotesForFile(f, notes); err != nil {
				return err
			}
			_ = gitAdd(notePath(f))
		}
		if edited == "abort" {
			return errors.New("commit aborted by user")
		}
		return nil
	}
	for f, notes := range original {
		if err := writeNotesForFile(f, notes); err != nil {
			return err
		}
		_ = gitAdd(notePath(f))
	}
	return errors.New("commit aborted and notes reset")
}

func installHook() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	hook := filepath.Join(".git", "hooks", "pre-commit")
	body := "#!/usr/bin/env bash\n\"" + exe + "\" review --staged\n"
	if err := os.WriteFile(hook, []byte(body), 0o755); err != nil {
		return err
	}
	return nil
}

type change struct{ status, oldPath, newPath string }

func stagedChanges() ([]change, error) {
	out := runOut("git", "diff", "--cached", "--name-status", "-M")
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var c []change
	for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		s := f[0]
		if strings.HasPrefix(s, "R") && len(f) >= 3 {
			c = append(c, change{status: "R", oldPath: f[1], newPath: f[2]})
			continue
		}
		c = append(c, change{status: string(s[0]), oldPath: f[1], newPath: f[1]})
	}
	return c, nil
}

func changedTargets(c []change) []string {
	m := map[string]bool{}
	for _, ch := range c {
		if ch.status == "D" {
			m[ch.oldPath] = true
		} else {
			m[ch.newPath] = true
		}
	}
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isDeleted(c []change, path string) bool {
	for _, ch := range c {
		if ch.status == "D" && ch.oldPath == path {
			return true
		}
	}
	return false
}

func handleRenames(c []change) error {
	for _, ch := range c {
		if ch.status != "R" {
			continue
		}
		oldN, newN := notePath(ch.oldPath), notePath(ch.newPath)
		if _, err := os.Stat(oldN); err == nil {
			if err := os.MkdirAll(filepath.Dir(newN), 0o755); err != nil {
				return err
			}
			if err := os.Rename(oldN, newN); err != nil {
				return err
			}
			_ = gitAdd(newN)
			_ = gitRM(oldN)
		}
	}
	return nil
}

func notePath(file string) string {
	return filepath.Join(".notes", "shadow", filepath.Clean(file)+".note")
}

func loadNotesForFile(file string) ([]Note, error) {
	p := notePath(file)
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	var out []Note
	for s.Scan() {
		ln := strings.TrimSpace(s.Text())
		if ln == "" {
			continue
		}
		var n Note
		if err := json.Unmarshal([]byte(ln), &n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, s.Err()
}

func writeNotesForFile(file string, notes []Note) error {
	p := notePath(file)
	if len(notes) == 0 {
		_ = os.Remove(p)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, n := range notes {
		n.File = file
		if err := enc.Encode(n); err != nil {
			return err
		}
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

func gitAdd(path string) error { return exec.Command("git", "add", path).Run() }
func gitRM(path string) error  { return exec.Command("git", "rm", "-f", "--cached", path).Run() }

func parseLineRef(ref string) (start, count int, err error) {
	if !strings.HasPrefix(ref, "L") {
		return 0, 0, errors.New("line ref must start with L")
	}
	r := strings.TrimPrefix(ref, "L")
	parts := strings.Split(r, "+")
	start, err = strconv.Atoi(parts[0])
	if err != nil || start < 1 {
		return 0, 0, errors.New("invalid start line")
	}
	count = 1
	if len(parts) == 2 {
		n, e := strconv.Atoi(parts[1])
		if e != nil || n < 0 {
			return 0, 0, errors.New("invalid range suffix")
		}
		count = n + 1
	}
	return
}

func normalizeLineRef(start, count int) string {
	if count <= 1 {
		return fmt.Sprintf("L%d", start)
	}
	return fmt.Sprintf("L%d+%d", start, count-1)
}

func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := strings.ReplaceAll(string(b), "\r\n", "\n")
	if strings.HasSuffix(t, "\n") {
		t = t[:len(t)-1]
	}
	if t == "" {
		return []string{}, nil
	}
	return strings.Split(t, "\n"), nil
}

func sliceRange(lines []string, start, count int) ([]string, error) {
	if start < 1 || start+count-1 > len(lines) {
		return nil, errors.New("line range out of bounds")
	}
	return lines[start-1 : start-1+count], nil
}

func blameRange(file string, start, count int) ([]string, error) {
	end := start + count - 1
	out, err := exec.Command("git", "blame", "--line-porcelain", "-L", fmt.Sprintf("%d,%d", start, end), "--", file).Output()
	if err != nil {
		return nil, err
	}
	var b []string
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) >= 1 && len(f[0]) == 40 {
			b = append(b, f[0])
		}
	}
	return b, nil
}

type candidate struct{ start, count, dist, lineDelta int }

func bestMatch(n Note, lines []string) candidate {
	start, count, err := parseLineRef(n.Subject.LineRef)
	if err != nil {
		return candidate{}
	}
	if count < 1 || len(lines) < count {
		return candidate{}
	}
	best := candidate{dist: 1 << 30, lineDelta: 1 << 30}
	target := n.Subject.Text
	for i := 0; i+count <= len(lines); i++ {
		seg := strings.Join(lines[i:i+count], "\n")
		d := editDistance(target, seg)
		ld := abs((i + 1) - start)
		if d < best.dist || (d == best.dist && ld < best.lineDelta) {
			best = candidate{start: i + 1, count: count, dist: d, lineDelta: ld}
		}
	}
	return best
}

func relocateNote(n Note, file string, lines []string) (Note, bool, bool) {
	best := bestMatch(n, lines)
	if best.start == 0 {
		return n, false, false
	}
	updated := n
	updated.Subject.LineRef = normalizeLineRef(best.start, best.count)
	updated.Subject.Text = strings.Join(lines[best.start-1:best.start-1+best.count], "\n")
	blames, _ := blameRange(file, best.start, best.count)
	updated.Subject.Blames = blames
	return updated, true, updated.Subject.Text != n.Subject.Text
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	dp := make([]int, len(rb)+1)
	for j := range dp {
		dp[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		prev := dp[0]
		dp[0] = i
		for j := 1; j <= len(rb); j++ {
			cur := dp[j]
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			dp[j] = min(min(dp[j]+1, dp[j-1]+1), prev+cost)
			prev = cur
		}
	}
	return dp[len(rb)]
}

func editReviewLoop(items []EditItem, original map[string][]Note, updated map[string][]Note) (string, error) {
	for {
		edited, err := openEditor(items)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(stripCommentLines(edited)) == "" {
			return "abort", nil
		}
		if err := applyEdits(edited, items, updated); err != nil {
			fmt.Fprintln(os.Stderr, "edit parse error:", err)
			continue
		}
		d, _ := notesDiff(original, updated)
		fmt.Println("summary diff")
		fmt.Println(d)
		action, err := promptAction()
		if err != nil {
			return "", err
		}
		switch action {
		case "accept", "abort", "reset":
			return action, nil
		case "edit":
			continue
		}
	}
}

func openEditor(items []EditItem) (string, error) {
	var buf bytes.Buffer
	buf.WriteString("# Please review the following notes as their underlying code has been changed.\n")
	buf.WriteString("# Lines starting with '#' will be ignored, and an empty message aborts the commit.\n\n")
	for _, it := range items {
		buf.WriteString(fmt.Sprintf("file: %s\n", it.Curr.File))
		buf.WriteString(fmt.Sprintf("line: %s\n", it.Curr.Subject.LineRef))
		buf.WriteString("note:\n")
		for _, ln := range strings.Split(it.Orig.NoteText, "\n") {
			buf.WriteString("    " + ln + "\n")
		}
		buf.WriteString(fmt.Sprintf("action: %s\n", it.Action))
		buf.WriteString("---\n")
	}
	f, err := os.CreateTemp("", "notectl-review-*.txt")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(buf.Bytes()); err != nil {
		return "", err
	}
	_ = f.Close()
	ed := os.Getenv("EDITOR")
	if ed == "" {
		ed = "vi"
	}
	cmd := exec.Command(ed, f.Name())
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close()
		cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	} else {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	b, err := os.ReadFile(f.Name())
	return string(b), err
}

func applyEdits(text string, items []EditItem, updated map[string][]Note) error {
	clean := strings.TrimSpace(stripCommentLines(text))
	if clean == "" {
		return errors.New("empty review message")
	}
	blocks := strings.Split(clean, "\n---\n")
	if len(blocks) < len(items) {
		return errors.New("missing note blocks")
	}
	byFile := map[string][]Note{}
	for _, arr := range updated {
		for _, n := range arr {
			byFile[n.File] = append(byFile[n.File], n)
		}
	}
	for i, it := range items {
		file, line, note, action, err := parseBlock(blocks[i])
		if err != nil {
			return err
		}
		if file != it.Curr.File || line != it.Curr.Subject.LineRef {
			return errors.New("file/line fields are immutable")
		}
		if action == "accept" && note != it.Orig.NoteText {
			return errors.New("accept requires unchanged note text")
		}
		arr := byFile[file]
		idx := -1
		for j := range arr {
			if arr[j].Subject.LineRef == it.Curr.Subject.LineRef && arr[j].NoteText == it.Curr.NoteText {
				idx = j
				break
			}
		}
		if idx < 0 {
			continue
		}
		switch action {
		case "drop":
			arr = append(arr[:idx], arr[idx+1:]...)
		case "accept":
			n := it.Curr
			n.NoteText = it.Orig.NoteText
			arr[idx] = n
		case "update":
			n := it.Curr
			n.NoteText = note
			arr[idx] = n
		default:
			return errors.New("action must be accept/update/drop")
		}
		byFile[file] = arr
	}
	for f := range updated {
		updated[f] = byFile[f]
	}
	return nil
}

func parseBlock(b string) (file, line, note, action string, err error) {
	s := bufio.NewScanner(strings.NewReader(b))
	inNote := false
	var noteLines []string
	for s.Scan() {
		ln := s.Text()
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		switch {
		case strings.HasPrefix(ln, "file: "):
			file = strings.TrimSpace(strings.TrimPrefix(ln, "file: "))
		case strings.HasPrefix(ln, "line: "):
			line = strings.TrimSpace(strings.TrimPrefix(ln, "line: "))
		case ln == "note:":
			inNote = true
		case strings.HasPrefix(ln, "action: "):
			action = strings.TrimSpace(strings.TrimPrefix(ln, "action: "))
			inNote = false
		default:
			if inNote {
				noteLines = append(noteLines, strings.TrimPrefix(ln, "    "))
			}
		}
	}
	note = strings.TrimRight(strings.Join(noteLines, "\n"), "\n")
	if file == "" || line == "" || action == "" {
		return "", "", "", "", errors.New("invalid review block")
	}
	return
}

func stripCommentLines(s string) string {
	var out []string
	for _, ln := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

func promptAction() (string, error) {
	fmt.Println("save note changes?")
	fmt.Println("accept")
	fmt.Println("abort")
	fmt.Println("edit")
	fmt.Println("reset")
	fmt.Print("> ")
	in := io.Reader(os.Stdin)
	if tty, err := os.Open("/dev/tty"); err == nil {
		defer tty.Close()
		in = tty
	}
	r := bufio.NewReader(in)
	v, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	v = strings.TrimSpace(v)
	if v == "" {
		v = "accept"
	}
	return v, nil
}

func notesDiff(before, after map[string][]Note) (string, error) {
	type info struct{ file, line, note string }
	index := func(m map[string][]Note) map[string]info {
		out := map[string]info{}
		for _, notes := range m {
			for _, n := range notes {
				k := n.File + "\x00" + n.Subject.LineRef
				out[k] = info{file: n.File, line: n.Subject.LineRef, note: n.NoteText}
			}
		}
		return out
	}
	b, a := index(before), index(after)
	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range a {
		keys[k] = true
	}
	var sorted []string
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var out strings.Builder
	for _, k := range sorted {
		oldv, oldok := b[k]
		newv, newok := a[k]
		if oldok && newok && oldv.note == newv.note {
			continue
		}
		ref := oldv.file
		line := oldv.line
		if ref == "" {
			ref, line = newv.file, newv.line
		}
		out.WriteString(fmt.Sprintf("%s:%s\n", ref, strings.TrimPrefix(line, "L")))
		if oldok {
			out.WriteString(colorRed("-   " + oldv.note + "\n"))
		}
		if newok {
			out.WriteString(colorGreen("+   " + newv.note + "\n"))
		}
	}
	return out.String(), nil
}

func colorRed(s string) string   { return "\x1b[31m" + s + "\x1b[0m" }
func colorGreen(s string) string { return "\x1b[32m" + s + "\x1b[0m" }

func cloneNotes(a []Note) []Note {
	b := make([]Note, len(a))
	copy(b, a)
	return b
}

func runOut(cmd string, args ...string) string {
	out, _ := exec.Command(cmd, args...).Output()
	return string(out)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func exitIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
