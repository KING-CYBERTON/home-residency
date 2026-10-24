package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// Photo is a responsive photograph under static/img/photos/. Each size is published in three
// formats as <name>-<width>x<height>.{avif,webp,jpg}, e.g. hero-1920x1080.avif, so pages get
// srcset and intrinsic dimensions (no layout shift) without decoding the files. A photo named
// <name>.narrow is the crop shown in place of <name> on phones.
//
// static/img/photos/credits.json records where each photo came from: its alt text,
// photographer, source and licence. The credits page is built from it.
type Photo struct {
	Src           string // the largest JPEG, cache-busted
	AVIF, WebP    string // srcsets
	JPEG          string
	Width, Height int
	Abs           string // absolute URL of the largest JPEG, for og:image
	Alt           string
	Narrow        *Photo
}

// Credit is one entry of credits.json.
type Credit struct {
	Name         string `json:"name"`
	UsedFor      string `json:"used_for"`
	Alt          string `json:"alt"`
	Photographer string `json:"photographer"`
	Profile      string `json:"profile"`
	Source       string `json:"source"`
	License      string `json:"license"`
	LicenseURL   string `json:"license_url"`
}

// unsplashRef is the referral Unsplash asks for on links back to it.
const unsplashRef = "?utm_source=homend_residence&utm_medium=referral"

// ProfileURL links to the photographer's Unsplash profile.
func (c Credit) ProfileURL() string { return c.Profile + unsplashRef }

// UnsplashURL links to Unsplash itself.
func (Credit) UnsplashURL() string { return "https://unsplash.com/" + unsplashRef }

func (s *Site) indexPhotos() error {
	type size struct {
		path string
		w, h int
	}
	by := map[string]map[string][]size{} // name -> extension -> sizes
	for p := range s.hashes {
		rest, ok := strings.CutPrefix(p, "img/photos/")
		if !ok {
			continue
		}
		i := strings.LastIndex(rest, ".")
		j := strings.LastIndex(rest, "-")
		if i < 0 || j < 0 || j > i {
			continue
		}
		ext := rest[i+1:]
		if ext != "avif" && ext != "webp" && ext != "jpg" {
			continue
		}
		ws, hs, ok := strings.Cut(rest[j+1:i], "x")
		w, err1 := strconv.Atoi(ws)
		h, err2 := strconv.Atoi(hs)
		if !ok || err1 != nil || err2 != nil {
			continue
		}
		name := rest[:j]
		if by[name] == nil {
			by[name] = map[string][]size{}
		}
		by[name][ext] = append(by[name][ext], size{p, w, h})
	}
	s.photos = map[string]*Photo{}
	for name, exts := range by {
		jpgs := exts["jpg"]
		if len(jpgs) == 0 {
			continue // the fallback format is required
		}
		set := func(sizes []size) string {
			sort.Slice(sizes, func(i, j int) bool { return sizes[i].w < sizes[j].w })
			out := make([]string, len(sizes))
			for i, z := range sizes {
				out[i] = fmt.Sprintf("%s %dw", s.Asset(z.path), z.w)
			}
			return strings.Join(out, ", ")
		}
		ph := &Photo{AVIF: set(exts["avif"]), WebP: set(exts["webp"]), JPEG: set(jpgs)}
		big := jpgs[len(jpgs)-1]
		ph.Src, ph.Width, ph.Height = s.Asset(big.path), big.w, big.h
		ph.Abs = s.AbsURL(ph.Src)
		s.photos[name] = ph
	}
	for name, ph := range s.photos {
		if base, ok := strings.CutSuffix(name, ".narrow"); ok && s.photos[base] != nil {
			s.photos[base].Narrow = ph
		}
	}

	b, err := fs.ReadFile(s.static, "img/photos/credits.json")
	if err != nil {
		return nil // no photos yet
	}
	if err := json.Unmarshal(b, &s.credits); err != nil {
		return fmt.Errorf("img/photos/credits.json: %w", err)
	}
	for _, c := range s.credits {
		if ph := s.photos[c.Name]; ph != nil {
			ph.Alt = c.Alt
			if ph.Narrow != nil {
				ph.Narrow.Alt = c.Alt
			}
		}
	}
	return nil
}

// Photo returns the named photograph, or nil when there is none, so templates can fall back.
func (s *Site) Photo(name string) *Photo { return s.photos[name] }

// PhotoCredits lists where the site's photos came from, in credits.json order.
func (s *Site) PhotoCredits() []Credit { return s.credits }

// Img renders the photo as a responsive <picture>: AVIF and WebP sources with a JPEG <img>
// fallback, and the narrow crop on phones when there is one. sizes is the CSS width it is shown
// at (for srcset); class goes on the <img>; eager marks the page's main image (the hero), which
// loads first instead of lazily.
func (p *Photo) Img(sizes, class string, eager bool) template.HTML {
	e := template.HTMLEscapeString
	var b strings.Builder
	b.WriteString("<picture>")
	source := func(ph *Photo, media string) {
		for _, f := range []struct{ typ, set string }{{"image/avif", ph.AVIF}, {"image/webp", ph.WebP}, {"image/jpeg", ph.JPEG}} {
			if f.set == "" {
				continue
			}
			fmt.Fprintf(&b, `<source type="%s" srcset="%s" sizes="%s" width="%d" height="%d"`, f.typ, e(f.set), e(sizes), ph.Width, ph.Height)
			if media != "" {
				fmt.Fprintf(&b, ` media="%s"`, media)
			}
			b.WriteString(">")
		}
	}
	if p.Narrow != nil {
		source(p.Narrow, "(max-width: 767px)")
	}
	if p.AVIF != "" {
		fmt.Fprintf(&b, `<source type="image/avif" srcset="%s" sizes="%s">`, e(p.AVIF), e(sizes))
	}
	if p.WebP != "" {
		fmt.Fprintf(&b, `<source type="image/webp" srcset="%s" sizes="%s">`, e(p.WebP), e(sizes))
	}
	load := `loading="lazy" decoding="async"`
	if eager {
		load = `fetchpriority="high" decoding="async"`
	}
	fmt.Fprintf(&b, `<img src="%s" srcset="%s" sizes="%s" width="%d" height="%d" alt="%s" class="%s" %s></picture>`,
		e(p.Src), e(p.JPEG), e(sizes), p.Width, p.Height, e(p.Alt), e(class), load)
	return template.HTML(b.String())
}
