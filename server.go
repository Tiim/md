package main

import (
	"cmp"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

//go:embed page.html
var pageHTML string

var pageTmpl = template.Must(template.New("page").Parse(pageHTML))

type page struct {
	Title   string
	Content template.HTML
	Entries []entry // set for directory listings
	CSS     template.CSS
	Watch   bool // page reloads its content when the file changes
}

type entry struct {
	Name  string
	Href  string
	IsDir bool
}

type server struct {
	name        string
	root        *os.Root
	defaultFile string // rendered at "/" when set
	stdin       []byte // rendered at "/" when set
	css         template.CSS
}

func newServer(dir, defaultFile string, stdin []byte, name string) (*server, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	css, err := chromaCSS()
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return &server{
		name:        name,
		root:        root,
		defaultFile: defaultFile,
		stdin:       stdin,
		css:         template.CSS(css),
	}, nil
}

func (s *server) Close() error { return s.root.Close() }

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{path...}", s.handle)
	return mux
}

func isMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	p := r.PathValue("path")

	if p == "" {
		switch {
		case s.stdin != nil:
			s.renderSource(w, s.name, s.stdin)
			return
		case s.defaultFile != "":
			p = s.defaultFile
		}
	}

	name := cmp.Or(path.Clean(p), ".")
	info, err := s.root.Stat(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	switch {
	case info.IsDir():
		if p != "" && !strings.HasSuffix(p, "/") {
			http.Redirect(w, r, "/"+p+"/", http.StatusMovedPermanently)
			return
		}
		s.renderDir(w, name)
	case isMarkdown(name) && r.URL.Query().Has("watch"):
		s.watch(w, r, name)
	case isMarkdown(name) && r.URL.Query().Get("raw") == "":
		src, err := s.root.ReadFile(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.renderFile(w, name, src)
	default:
		// Images and other assets referenced by the markdown.
		http.ServeFileFS(w, r, s.root.FS(), name)
	}
}

func (s *server) renderSource(w http.ResponseWriter, title string, src []byte) {
	s.renderPage(w, page{Title: title}, src)
}

func (s *server) renderFile(w http.ResponseWriter, name string, src []byte) {
	s.renderPage(w, page{Title: name, Watch: true}, src)
}

func (s *server) renderPage(w http.ResponseWriter, p page, src []byte) {
	out, err := renderMarkdown(src)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.Content = template.HTML(out)
	s.writePage(w, p)
}

// watch streams a server-sent event whenever the file's size or
// modification time changes. Polling the name (rather than the open file)
// also catches editors that save by replacing the file.
func (s *server) watch(w http.ResponseWriter, r *http.Request, name string) {
	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := rc.Flush(); err != nil {
		return
	}

	var last fs.FileInfo
	if info, err := s.root.Stat(name); err == nil {
		last = info
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
		info, err := s.root.Stat(name)
		if err != nil {
			continue // possibly mid-save; keep the last known state
		}
		if last != nil && info.ModTime().Equal(last.ModTime()) && info.Size() == last.Size() {
			continue
		}
		last = info
		if _, err := fmt.Fprint(w, "data: change\n\n"); err != nil {
			return
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

func (s *server) renderDir(w http.ResponseWriter, dir string) {
	dirEntries, err := fs.ReadDir(s.root.FS(), dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var entries []entry
	if dir != "." {
		entries = append(entries, entry{Name: "..", Href: "../", IsDir: true})
	}
	for _, e := range dirEntries {
		n := e.Name()
		if strings.HasPrefix(n, ".") || (!e.IsDir() && !isMarkdown(n)) {
			continue
		}
		href := n
		if e.IsDir() {
			href += "/"
		}
		entries = append(entries, entry{Name: n, Href: href, IsDir: e.IsDir()})
	}
	// Directories first, then alphabetically.
	slices.SortStableFunc(entries, func(a, b entry) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})

	title := path.Join(s.name, dir)
	s.writePage(w, page{Title: title, Entries: entries})
}

func (s *server) writePage(w http.ResponseWriter, p page) {
	p.CSS = s.css
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := pageTmpl.Execute(w, p); err != nil {
		fmt.Fprintln(os.Stderr, "md: render page:", err)
	}
}
