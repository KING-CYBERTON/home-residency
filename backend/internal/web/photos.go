package web

import (
	"fmt"
	"html/template"
	"sort"
	"strconv"
	"strings"
)

// Photo is a responsive photograph under static/img/photos/. Each size is published as
// <name>-<width>x<height>.webp, e.g. hero-1920x1080.webp, so pages get srcset and intrinsic
// dimensions (no layout shift) without decoding the files.
type Photo struct {
	Src           string // the largest size, cache-busted
	SrcSet        string
	Width, Height int
	Abs           string // absolute URL of the largest size, for og:image
}

func (s *Site) indexPhotos() {
	type size struct {
		path string
		w, h int
	}
	by := map[string][]size{}
	for p := range s.hashes {
		rest, ok := strings.CutPrefix(p, "img/photos/")
		if !ok || !strings.HasSuffix(rest, ".webp") {
			continue
		}
		rest = strings.TrimSuffix(rest, ".webp")
		i := strings.LastIndex(rest, "-")
		if i < 0 {
			continue
		}
		ws, hs, ok := strings.Cut(rest[i+1:], "x")
		w, err1 := strconv.Atoi(ws)
		h, err2 := strconv.Atoi(hs)
		if !ok || err1 != nil || err2 != nil {
			continue
		}
		by[rest[:i]] = append(by[rest[:i]], size{p, w, h})
	}
	s.photos = map[string]*Photo{}
	for name, sizes := range by {
		sort.Slice(sizes, func(i, j int) bool { return sizes[i].w < sizes[j].w })
		set := make([]string, len(sizes))
		for i, z := range sizes {
			set[i] = fmt.Sprintf("%s %dw", s.Asset(z.path), z.w)
		}
		big := sizes[len(sizes)-1]
		s.photos[name] = &Photo{Src: s.Asset(big.path), SrcSet: strings.Join(set, ", "), Width: big.w, Height: big.h, Abs: s.AbsURL(s.Asset(big.path))}
	}
}

// Photo returns the named photograph, or nil when there is none, so templates can fall back.
func (s *Site) Photo(name string) *Photo { return s.photos[name] }

// Img renders the photo as a responsive <img>. sizes is the CSS width it is shown at (for
// srcset); eager marks the page's main image (the hero), which loads first instead of lazily.
func (p *Photo) Img(alt, sizes, class string, eager bool) template.HTML {
	load := `loading="lazy" decoding="async"`
	if eager {
		load = `fetchpriority="high" decoding="async"`
	}
	return template.HTML(fmt.Sprintf(`<img src="%s" srcset="%s" sizes="%s" width="%d" height="%d" alt="%s" class="%s" %s>`,
		template.HTMLEscapeString(p.Src), template.HTMLEscapeString(p.SrcSet), template.HTMLEscapeString(sizes),
		p.Width, p.Height, template.HTMLEscapeString(alt), template.HTMLEscapeString(class), load))
}
