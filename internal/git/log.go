package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Commit struct {
	Hash    string
	Author  string
	Subject string
	Time    time.Time
	Count   int // commits touching this entry within the scanned history (heat)
}

func gitCmd(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "core.quotePath=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd
}

// History walks `git log` for the files under dir and attributes every commit
// to the direct child of dir it touched. Without counts it stops as soon as
// each child has its most recent commit.
func (r *Repo) History(dir string, children []string, counts bool) map[string]*Commit {
	out := map[string]*Commit{}
	if len(children) == 0 {
		return out
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return out
	}
	rel, err := filepath.Rel(r.Root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return out
	}
	rel = filepath.ToSlash(rel)
	prefix := ""
	if rel != "." {
		prefix = rel + "/"
	}
	want := make(map[string]bool, len(children))
	for _, c := range children {
		want[c] = true
	}

	limit := "1500"
	if counts {
		limit = "5000"
	}
	cmd := gitCmd(r.Root, "log", "-n", limit, "--no-renames", "--name-only",
		"--format=\x1e%h\x1f%at\x1f%an\x1f%s", "--", abs)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return out
	}
	if err := cmd.Start(); err != nil {
		return out
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	sc := bufio.NewScanner(pipe)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var cur *Commit
	seen := map[string]bool{} // children already credited for the current commit
	found := 0
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "\x1e") {
			parts := strings.SplitN(line[1:], "\x1f", 4)
			if len(parts) < 4 {
				cur = nil
				continue
			}
			ts, _ := strconv.ParseInt(parts[1], 10, 64)
			cur = &Commit{Hash: parts[0], Time: time.Unix(ts, 0), Author: parts[2], Subject: parts[3]}
			clear(seen)
			continue
		}
		if cur == nil || line == "" || !strings.HasPrefix(line, prefix) {
			continue
		}
		child, _, _ := strings.Cut(line[len(prefix):], "/")
		if !want[child] || seen[child] {
			continue
		}
		seen[child] = true
		c, ok := out[child]
		if !ok {
			c = &Commit{Hash: cur.Hash, Time: cur.Time, Author: cur.Author, Subject: cur.Subject}
			out[child] = c
			found++
		}
		c.Count++
		if !counts && found == len(want) {
			break
		}
	}
	if counts {
		return out
	}
	// stragglers that weren't touched recently: ask git per path, in parallel
	var missing []string
	for c := range want {
		if out[c] == nil {
			missing = append(missing, c)
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, c := range missing {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if cm := r.lastCommitFor(filepath.Join(abs, c)); cm != nil {
				cm.Count = 1
				mu.Lock()
				out[c] = cm
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return out
}

func parseCommit(line string) *Commit {
	parts := strings.SplitN(strings.TrimSpace(line), "\x1f", 4)
	if len(parts) < 4 {
		return nil
	}
	ts, _ := strconv.ParseInt(parts[1], 10, 64)
	return &Commit{Hash: parts[0], Time: time.Unix(ts, 0), Author: parts[2], Subject: parts[3]}
}

func (r *Repo) lastCommitFor(path string) *Commit {
	b, err := gitCmd(r.Root, "log", "-1", "--format=%h\x1f%at\x1f%an\x1f%s", "--", path).Output()
	if err != nil {
		return nil
	}
	return parseCommit(string(b))
}

// LastCommit is HEAD's commit, or nil in an empty repo.
func (r *Repo) LastCommit() *Commit {
	b, err := gitCmd(r.Root, "log", "-1", "--format=%h\x1f%at\x1f%an\x1f%s").Output()
	if err != nil {
		return nil
	}
	return parseCommit(string(b))
}

// TrackedFiles lists files git knows about (tracked + untracked, not ignored) under dir.
func (r *Repo) TrackedFiles(dir string) []string {
	abs, _ := filepath.Abs(dir)
	b, err := gitCmd(r.Root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--full-name", "--", abs).Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(string(b), "\x00") {
		if f != "" {
			files = append(files, filepath.Join(r.Root, f))
		}
	}
	return files
}

func (r *Repo) Dirty() bool {
	s, m, u := r.Counts()
	return s+m+u > 0
}

// HeadFiles lists the paths HEAD touched (repo-relative), at most limit.
func (r *Repo) HeadFiles(limit int) (files []string, total int) {
	if r.NoCommits {
		return nil, 0
	}
	b, err := gitCmd(r.Root, "show", "--no-renames", "--name-only", "--format=", "HEAD").Output()
	if err != nil {
		return nil, 0
	}
	for _, f := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if f == "" {
			continue
		}
		total++
		if len(files) < limit {
			files = append(files, f)
		}
	}
	return files, total
}

// Weekly counts commits per week over the last n weeks (index n-1 = this week).
func (r *Repo) Weekly(n int) []int {
	out := make([]int, n)
	if r.NoCommits {
		return out
	}
	b, err := gitCmd(r.Root, "log", fmt.Sprintf("--since=%d.weeks", n), "--format=%at").Output()
	if err != nil {
		return out
	}
	now := time.Now()
	for _, line := range strings.Fields(string(b)) {
		ts, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		week := int(now.Sub(time.Unix(ts, 0)).Hours() / (24 * 7))
		if week >= 0 && week < n {
			out[n-1-week]++
		}
	}
	return out
}

// MergeBase returns the best common ancestor of two commits, or "" if
// there's none or git fails.
func MergeBase(root, a, b string) string {
	out, err := gitCmd(root, "merge-base", a, b).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Differs reports whether anything at path (repo-relative, literal; "."
// for everything) differs between two commits. When git fails it reports
// true, so a caller that skips work on false never skips by mistake.
func Differs(root, from, to, path string) bool {
	cmd := gitCmd(root, "diff", "--quiet", "--no-renames", "--no-ext-diff", from, to, "--", path)
	cmd.Env = append(cmd.Env, "GIT_LITERAL_PATHSPECS=1")
	return cmd.Run() != nil
}
