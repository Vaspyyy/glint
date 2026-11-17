package agent

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Every glint -A -M of a folder saves a snapshot of its sizes, and the next
// one says what grew or shrank since. A snapshot keeps every top-level entry
// and, below them, every folder and file of snapMin or more, keyed by its
// path below the measured folder (folders end in "/").

var snapMin int64 = 32 << 20 // a variable so tests can use small files

const (
	snapKeep    = 30                  // snapshots kept per folder
	snapReplace = time.Hour           // a newer run replaces a snapshot younger than this
	snapExpire  = 90 * 24 * time.Hour // folders not measured for this long lose their snapshots
	diffLines   = 20
)

type snapshot struct {
	Version int              `json:"version"`
	Root    string           `json:"root"`
	Time    time.Time        `json:"time"`
	Total   int64            `json:"total"`
	Sizes   map[string]int64 `json:"sizes"`
}

func snapBase() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "glint", "du")
}

func snapDir(root string) string {
	h := sha256.Sum256([]byte(root))
	return filepath.Join(snapBase(), hex.EncodeToString(h[:8]))
}

// snapFiles lists a folder's snapshot files, oldest first.
func snapFiles(root string) []string {
	ents, _ := os.ReadDir(snapDir(root))
	var out []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".json.gz") {
			out = append(out, filepath.Join(snapDir(root), e.Name()))
		}
	}
	sort.Strings(out) // names are zero-padded unix nanoseconds
	return out
}

func snapTime(file string) time.Time {
	n, _ := strconv.ParseInt(strings.TrimSuffix(filepath.Base(file), ".json.gz"), 10, 64)
	return time.Unix(0, n)
}

func readSnap(file string) (*snapshot, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var s snapshot
	if err := json.NewDecoder(z).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// pickSnap returns the snapshot to compare with: the latest, or with since
// set, the newest one at least that old. tooYoung says that none was that old
// and the oldest one was taken instead.
func pickSnap(root string, since time.Duration, now time.Time) (s *snapshot, tooYoung bool) {
	files := snapFiles(root)
	for i := len(files) - 1; i >= 0; i-- {
		if since > 0 && now.Sub(snapTime(files[i])) < since {
			continue
		}
		if s, err := readSnap(files[i]); err == nil && s.Root == root {
			return s, false
		}
	}
	if since > 0 {
		for _, f := range files {
			if s, err := readSnap(f); err == nil && s.Root == root {
				return s, true
			}
		}
	}
	return nil, false
}

// saveSnap stores s and trims the folder's history. Errors are ignored: the
// snapshot is a convenience, and the listing doesn't depend on it.
func saveSnap(s *snapshot) {
	dir := snapDir(s.Root)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	old := snapFiles(s.Root)
	file := filepath.Join(dir, fmt.Sprintf("%020d.json.gz", s.Time.UnixNano()))
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return
	}
	z := gzip.NewWriter(tmp)
	err = json.NewEncoder(z).Encode(s)
	if cerr := z.Close(); err == nil {
		err = cerr
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil || os.Rename(tmp.Name(), file) != nil {
		os.Remove(tmp.Name())
		return
	}
	if n := len(old); n > 0 && s.Time.Sub(snapTime(old[n-1])) < snapReplace {
		os.Remove(old[n-1])
		old = old[:n-1]
	}
	for len(old) >= snapKeep {
		os.Remove(old[0])
		old = old[1:]
	}
	// other folders' snapshots nobody has refreshed in months
	if ents, err := os.ReadDir(snapBase()); err == nil {
		for _, e := range ents {
			p := filepath.Join(snapBase(), e.Name())
			if info, err := e.Info(); err == nil && e.IsDir() && p != dir && s.Time.Sub(info.ModTime()) > snapExpire {
				os.RemoveAll(p)
			}
		}
	}
}

// ------------------------------------------------------------ the diff

type change struct {
	key   string
	delta int64
	isNew bool // not in the old snapshot (or under snapMin then)
	gone  bool // not in the new one (or under snapMin now)
	kids  []*change
}

func snapParent(key string) string {
	p := path.Dir(strings.TrimSuffix(key, "/"))
	if p == "." {
		return ""
	}
	return p + "/"
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// diffSnaps finds where the space moved. An entry is shown where its change
// happened: a folder whose growth is mostly one child's is skipped in favour
// of that child, so the lines name the folder that actually grew, nested
// under the nearest shown folder above it. Changes under min are left out.
func diffSnaps(old, cur map[string]int64, floor int64) []*change {
	all := map[string]*change{}
	for k, v := range cur {
		o, had := old[k]
		all[k] = &change{key: k, delta: v - o, isNew: !had}
	}
	for k, o := range old {
		if _, ok := cur[k]; !ok {
			all[k] = &change{key: k, delta: -o, gone: true}
		}
	}
	kids := map[string][]string{}
	for k := range all {
		kids[snapParent(k)] = append(kids[snapParent(k)], k)
	}
	// shown: big enough, and no child explains three quarters of it in the
	// same direction
	shown := func(c *change) bool {
		if abs64(c.delta) < floor {
			return false
		}
		for _, k := range kids[c.key] {
			d := all[k].delta
			if (d > 0) == (c.delta > 0) && abs64(d)*4 >= abs64(c.delta)*3 {
				return false
			}
		}
		return true
	}
	var collect func(key string) []*change
	collect = func(key string) []*change {
		var out []*change
		for _, k := range kids[key] {
			c := all[k]
			if shown(c) {
				c.kids = collect(k)
				out = append(out, c)
			} else {
				out = append(out, collect(k)...)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if a, b := abs64(out[i].delta), abs64(out[j].delta); a != b {
				return a > b
			}
			return out[i].key < out[j].key
		})
		return out
	}
	return collect("")
}

func signed(n int64) string {
	if n < 0 {
		return "-" + human(-n)
	}
	return "+" + human(n)
}

// printDiff writes the comparison with prev (nil: none yet) under the
// listing's header.
func printDiff(w io.Writer, prev *snapshot, tooYoung bool, cur *snapshot, sinceRaw string) {
	if prev == nil {
		fmt.Fprintf(w, "disk snapshot saved: the next glint -A -M of this folder says what grew since %s\n", cur.Time.Format("2006-01-02 15:04"))
		return
	}
	var moved int64
	for k, v := range cur.Sizes {
		if snapParent(k) == "" {
			moved += abs64(v - prev.Sizes[k])
		}
	}
	for k, v := range prev.Sizes {
		if _, ok := cur.Sizes[k]; !ok && snapParent(k) == "" {
			moved += v
		}
	}
	floor := max(8*snapMin, moved/100)
	when := fmt.Sprintf("%s (%s ago)", prev.Time.Format("2006-01-02 15:04"), age(prev.Time))
	if tooYoung {
		when += fmt.Sprintf("; no snapshot is %s old yet, this is the oldest", sinceRaw)
	}
	changes := diffSnaps(prev.Sizes, cur.Sizes, floor)
	if len(changes) == 0 {
		fmt.Fprintf(w, "since %s: no change of %s or more (total %s → %s)\n", when, human(floor), human(prev.Total), human(cur.Total))
		return
	}
	fmt.Fprintf(w, "since %s: %s in total (%s → %s); changes of %s or more, where they happened (\"new\" can also mean under %s before)\n",
		when, signed(cur.Total-prev.Total), human(prev.Total), human(cur.Total), human(floor), human(snapMin))
	lines := 0
	var hidden []*change
	var walk func(cs []*change, parent string, depth int)
	walk = func(cs []*change, parent string, depth int) {
		for _, c := range cs {
			if lines == diffLines {
				hidden = append(hidden, c)
				continue
			}
			lines++
			line := strings.Repeat("  ", depth) + signed(c.delta) + " " + quote(strings.TrimPrefix(c.key, parent))
			switch {
			case c.isNew:
				line += " (new)"
			case c.gone:
				line += " (gone)"
			}
			fmt.Fprintln(w, line)
			walk(c.kids, c.key, depth+1)
		}
	}
	walk(changes, "", 0)
	if len(hidden) > 0 {
		var up, down int64
		for _, c := range hidden {
			if c.delta > 0 {
				up += c.delta
			} else {
				down += c.delta
			}
		}
		fmt.Fprintf(w, "… %d more changes (%s, %s)\n", len(hidden), signed(up), signed(down))
	}
}
