// Package web serves the browser UI from the local machine.
//
// net/http plus go:embed. No framework, no bundler. Every mutating endpoint
// returns the whole workspace state, so the client never merges a partial
// update and cannot drift out of sync.
package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/tanisha327/whetstone/internal/lens"
	"github.com/tanisha327/whetstone/internal/provider"
	"github.com/tanisha327/whetstone/internal/workspace"
)

//go:embed assets
var assetsFS embed.FS

// Server serves one workspace to one local browser.
//
// It holds the workspace under a mutex because a browser will happily fire
// overlapping requests: an autosave and a provocation can land at once.
type Server struct {
	mu sync.Mutex
	ws *workspace.Workspace

	prov  provider.Provider
	token string
	mux   *http.ServeMux
}

// NewServer returns a Server bound to a workspace. The token it generates must
// appear in the opening URL; see Server.URL.
func NewServer(ws *workspace.Workspace, prov provider.Provider) (*Server, error) {
	tok, err := newToken()
	if err != nil {
		return nil, err
	}
	// A workspace loaded from an older file, or freshly created, has no lens
	// selected. Default it here rather than making every handler cope with the
	// empty case.
	if ws.ActiveLens == "" && len(lens.Builtin) > 0 {
		ws.ActiveLens = lens.Builtin[0].ID
	}
	s := &Server{ws: ws, prov: prov, token: tok, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	assets, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		panic("web: embedded assets missing: " + err.Error())
	}

	s.mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(assets)))

	s.mux.HandleFunc("GET /api/state", s.guard(s.handleState))
	s.mux.HandleFunc("POST /api/question", s.guard(s.handleQuestion))
	s.mux.HandleFunc("POST /api/lens", s.guard(s.handleSetLens))
	s.mux.HandleFunc("POST /api/documents", s.guard(s.handleAddDocument))
	s.mux.HandleFunc("POST /api/documents/delete", s.guard(s.handleDeleteDocument))
	s.mux.HandleFunc("POST /api/sections/read", s.guard(s.handleMarkRead))
	s.mux.HandleFunc("POST /api/sections/lens", s.guard(s.handleApplyLens))
	s.mux.HandleFunc("POST /api/sections/update", s.guard(s.handleUpdateSection))
	s.mux.HandleFunc("POST /api/sections/delete", s.guard(s.handleDeleteSection))
	s.mux.HandleFunc("POST /api/provoke", s.guard(s.handleProvoke))
	s.mux.HandleFunc("POST /api/provocations/resolve", s.guard(s.handleResolve))
	s.mux.HandleFunc("POST /api/outline/add", s.guard(s.handleAddNode))
	s.mux.HandleFunc("POST /api/outline/update", s.guard(s.handleUpdateNode))
	s.mux.HandleFunc("POST /api/outline/delete", s.guard(s.handleDeleteNode))
	s.mux.HandleFunc("POST /api/outline/cite", s.guard(s.handleCite))
	s.mux.HandleFunc("POST /api/outline/draft", s.guard(s.handleDraft))
	s.mux.HandleFunc("POST /api/outline/compose", s.guard(s.handleCompose))
	s.mux.HandleFunc("POST /api/rewrite", s.guard(s.handleRewrite))
	// Exports are GETs with the token in the query, so a plain link downloads
	// them; a fetch could not trigger the browser's save dialog.
	s.mux.HandleFunc("GET /export.docx", s.guardQuery(s.handleExportDOCX))
	s.mux.HandleFunc("GET /export.txt", s.guardQuery(s.handleExportText))
}

// guard rejects requests without the session token.
//
// Loopback is not a security boundary: any local process, and any page the user
// has open, can reach this server. The token is a custom header, which a
// cross-origin page cannot set without a preflight we never approve.
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Whetstone-Token") != s.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// guardQuery authenticates a download link. The token travels in the URL
// because a navigation cannot set a custom header.
func (s *Server) guardQuery(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != s.token {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// handleIndex serves the app shell. The token arrives in the query and is baked
// into the page, so the browser never stores it.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("t") != s.token {
		http.Error(w, "open the URL printed by whetstone", http.StatusForbidden)
		return
	}
	page, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "missing index", http.StatusInternalServerError)
		return
	}
	// Placeholder substitution rather than a format string: the page contains
	// CSS with literal % signs, which Fprintf would mangle. The token is our
	// own hex, so there is nothing to escape.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(page), "__TOKEN__", s.token)))
}

// Listen binds a loopback listener on the requested port, or any free port when
// port is 0.
func (s *Server) Listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("web: listening: %w", err)
	}
	return ln, nil
}

// URL is the address to open, including the session token.
func (s *Server) URL(ln net.Listener) string {
	return fmt.Sprintf("http://%s/?t=%s", ln.Addr().String(), s.token)
}

// Token returns the session token, for tests.
func (s *Server) Token() string { return s.token }

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("web: generating session token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// OpenBrowser asks the OS to open url. A failure is not fatal: the caller has
// already printed the URL.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// --- helpers shared by the handlers ---

// readJSON decodes a request body into v, rejecting unknown fields so a typo in
// the client shows up as an error rather than a silently ignored setting.
func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The response is already partly written; nothing useful is left to do.
		return
	}
}

// fail reports an error to the client. Errors are surfaced, never swallowed: a
// provider outage that silently yields no provocations is indistinguishable
// from a passage with nothing wrong in it.
func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// save persists the workspace. Callers hold s.mu.
func (s *Server) save() error { return s.ws.Save() }
