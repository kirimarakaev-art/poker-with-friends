package server

import (
	"net/http"
	"os"
	"path"
	"strings"
)

// staticHandler serves the frontend build in dir. index.html is revalidated
// on every load so new releases show up at once; files under /assets/ have
// content hashes in their names and are cached for a year (FileServer drops
// Cache-Control from error responses, so a 404 is never cached). Directories
// without index.html are not listed.
func staticHandler(dir string) http.Handler {
	fileServer := http.FileServer(noDirListing{http.Dir(dir)})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cache := "no-cache"
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			cache = "public, max-age=31536000, immutable"
		}
		w.Header().Set("Cache-Control", cache)
		fileServer.ServeHTTP(w, r)
	})
}

type noDirListing struct {
	fs http.FileSystem
}

func (n noDirListing) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if st.IsDir() {
		index, err := n.fs.Open(path.Join(name, "index.html"))
		if err != nil {
			f.Close()
			return nil, os.ErrNotExist
		}
		index.Close()
	}
	return f, nil
}
