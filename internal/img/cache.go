package img

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
)

func cacheHome() string {
	if d := os.Getenv("XDG_CACHE_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache")
}

// freedesktop thumbnail buckets and their max edge length in pixels.
var buckets = []struct {
	dir  string
	size int
}{{"normal", 128}, {"large", 256}, {"x-large", 512}, {"xx-large", 1024}}

func decodePNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// Thumb returns a downscaled version of the image at path whose longest edge
// is at least minPx (when the source is that large). It reuses thumbnails
// file managers already generated (~/.cache/thumbnails), then glint's own
// cache, and only decodes the original as a last resort, caching the result.
func Thumb(path string, minPx int) (image.Image, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}

	uri := (&url.URL{Scheme: "file", Path: abs}).String()
	sum := md5.Sum([]byte(uri))
	name := hex.EncodeToString(sum[:]) + ".png"
	for _, b := range buckets {
		if b.size < minPx {
			continue
		}
		p := filepath.Join(cacheHome(), "thumbnails", b.dir, name)
		if ts, err := os.Stat(p); err == nil && !ts.ModTime().Before(st.ModTime()) {
			if m, err := decodePNG(p); err == nil {
				return m, nil
			}
		}
	}

	size := 256
	if minPx > 256 {
		size = 512
	}
	key := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%d", abs, st.Size(), st.ModTime().UnixNano(), size)))
	own := filepath.Join(cacheHome(), "glint", "thumbs", hex.EncodeToString(key[:])+".png")
	if m, err := decodePNG(own); err == nil {
		return m, nil
	}

	m, err := Load(abs)
	if err != nil {
		return nil, err
	}
	b := m.Bounds()
	if b.Dx() > size || b.Dy() > size {
		w, h := Fit(b.Dx(), b.Dy(), size, size)
		m = Resize(m, w, h)
	}
	if err := os.MkdirAll(filepath.Dir(own), 0o755); err == nil {
		if f, err := os.CreateTemp(filepath.Dir(own), ".thumb-*"); err == nil {
			if png.Encode(f, m) == nil && f.Close() == nil {
				os.Rename(f.Name(), own)
			} else {
				f.Close()
				os.Remove(f.Name())
			}
		}
	}
	return m, nil
}
