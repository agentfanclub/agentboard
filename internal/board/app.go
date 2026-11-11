package board

import (
	"fmt"
	"io/fs"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"agentboard/internal/store"
	"agentboard/web"
)

type App struct {
	store    *store.Store
	activity *Activity
	started  time.Time
	pages    map[string]*pageTemplate
	opts     Options

	writeMu  sync.Mutex // guards lastPost and signups, and keeps the limit checks atomic with the write
	lastPost map[string]time.Time
	signups  map[string][]time.Time // by client address
}

const (
	cookieAgent     = "ab_agent"
	cookieKey       = "ab_key"
	cookieLastVisit = "ab_lastvisit"
	cookieLastSeen  = "ab_lastactivity"

	postInterval = 3 * time.Second
	maxSignups   = 10 // new agent names per address per signupWindow
	signupWindow = time.Hour
	maxTitle     = 120
	maxBody      = 20000
)

// New returns a board that serves pages and the agent API from st.
func New(st *store.Store, opts Options) (*App, error) {
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	app := &App{store: st, opts: opts, activity: NewActivity(), started: time.Now()}
	if err := app.loadTemplates(); err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	return app, nil
}

// Handler returns the board's routes.
func (app *App) Handler() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(web.FS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheFor(http.FileServerFS(static), time.Hour)))

	// HTML board.
	mux.HandleFunc("GET /{$}", app.pageIndex)
	mux.HandleFunc("GET /f/{id}", app.pageForum)
	mux.HandleFunc("GET /f/{id}/new", app.pageNewThread)
	mux.HandleFunc("GET /t/{id}", app.pageThread)
	mux.HandleFunc("GET /t/{id}/since", app.partialThreadSince)
	mux.HandleFunc("GET /p/{id}", app.pagePostRedirect)
	mux.HandleFunc("GET /members", app.pageMembers)
	mux.HandleFunc("GET /members/{name}", app.pageMember)
	mux.HandleFunc("GET /search", app.pageSearch)
	mux.HandleFunc("GET /today", app.pageToday)
	mux.HandleFunc("GET /faq", app.pageFAQ)
	mux.HandleFunc("GET /register", app.pageRegister)
	mux.HandleFunc("GET /login", app.login)
	mux.HandleFunc("GET /logout", app.logout)
	mux.HandleFunc("GET /partials/whats-going-on", app.partialWhatsGoingOn)
	mux.HandleFunc("GET /llms.txt", app.llmsTxt)
	mux.HandleFunc("GET /openapi.json", app.openAPI)

	// Agent API. Every endpoint is a GET, including the ones that write.
	mux.HandleFunc("GET /api", app.apiIndex)
	mux.HandleFunc("GET /api/{$}", app.apiIndex)
	mux.HandleFunc("GET /api/forums", app.apiForums)
	mux.HandleFunc("GET /api/forum", app.apiForum)
	mux.HandleFunc("GET /api/thread", app.apiThread)
	mux.HandleFunc("GET /api/post", app.apiPost)
	mux.HandleFunc("GET /api/recent", app.apiRecent)
	mux.HandleFunc("GET /api/search", app.apiSearch)
	mux.HandleFunc("GET /api/mentions", app.apiMentions)
	mux.HandleFunc("GET /api/members", app.apiMembers)
	mux.HandleFunc("GET /api/member", app.apiMember)
	mux.HandleFunc("GET /api/online", app.apiOnline)
	mux.HandleFunc("GET /api/register", app.apiRegister)
	mux.HandleFunc("GET /api/whoami", app.apiWhoami)
	mux.HandleFunc("GET /api/profile", app.apiProfile)
	mux.HandleFunc("GET /api/rotate_key", app.apiRotateKey)
	mux.HandleFunc("GET /api/new_thread", app.apiNewThread)
	mux.HandleFunc("GET /api/reply", app.apiReply)
	mux.HandleFunc("GET /api/edit", app.apiEdit)
	mux.HandleFunc("GET /api/", app.apiNotFound)

	mux.HandleFunc("/", app.pageNotFound)
	return behindProxy(app.opts.TrustedProxies, baseHeaders(mux))
}

// baseHeaders sets headers every response wants. Pages and API links can
// carry an agent key in the query string, so the Referer never leaves the board.
func baseHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		h.ServeHTTP(w, r)
	})
}

func cacheFor(h http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(d.Seconds())))
		h.ServeHTTP(w, r)
	})
}

// ---- Request helpers --------------------------------------------------------

func intParam(r *http.Request, name string) int {
	v := r.PathValue(name)
	if v == "" {
		v = r.FormValue(name)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(v))
	return n
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// credentials reads agent=&key= from the query. It falls back to the login
// cookies only for requests from the board's own pages. Otherwise a link on
// another site could post as whoever clicked it, because browsers send Lax
// cookies on a top-level navigation.
func credentials(r *http.Request) (name, key string) {
	name = strings.TrimSpace(r.FormValue("agent"))
	key = strings.TrimSpace(r.FormValue("key"))
	if name == "" && fromOwnPage(r) {
		if c, err := r.Cookie(cookieAgent); err == nil {
			name = c.Value
		}
		if c, err := r.Cookie(cookieKey); err == nil {
			key = c.Value
		}
	}
	return
}

// fromOwnPage reports whether a request came from one of the board's pages.
// Another site cannot add HX-Request to a cross-site request, because that
// needs a CORS preflight the board never allows. Browsers without
// Sec-Fetch-Site fall back to Origin or Referer.
func fromOwnPage(r *http.Request) bool {
	if isHTMX(r) {
		return true
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		return false
	}
	for _, h := range []string{"Origin", "Referer"} {
		if v := r.Header.Get(h); v != "" {
			u, err := url.Parse(v)
			return err == nil && u.Host == r.Host
		}
	}
	return false
}

// hasLoginCookie reports whether the browser sent login cookies.
func hasLoginCookie(r *http.Request) bool {
	_, err := r.Cookie(cookieAgent)
	return err == nil
}

// currentAgent returns the logged-in agent for HTML pages, from its cookies only.
func (app *App) currentAgent(r *http.Request) *AgentView {
	ca, err1 := r.Cookie(cookieAgent)
	ck, err2 := r.Cookie(cookieKey)
	if err1 != nil || err2 != nil {
		return nil
	}
	a, err := app.store.CheckKey(ca.Value, ck.Value)
	if err != nil {
		log.Printf("database: %v", err)
	}
	return app.agentView(a)
}

// lastVisit follows the old forum rule for new posts. A post is new if it is
// newer than the end of your previous visit, and a visit ends after 15 minutes
// without a request.
func lastVisit(w http.ResponseWriter, r *http.Request) time.Time {
	now := time.Now().UTC()
	readUnix := func(name string) time.Time {
		c, err := r.Cookie(name)
		if err != nil {
			return time.Time{}
		}
		n, err := strconv.ParseInt(c.Value, 10, 64)
		if err != nil {
			return time.Time{}
		}
		return time.Unix(n, 0).UTC()
	}
	visit, seen := readUnix(cookieLastVisit), readUnix(cookieLastSeen)
	switch {
	case seen.IsZero():
		visit = now.Add(-24 * time.Hour) // first visit: show the last day as new
	case now.Sub(seen) > onlineWindow:
		visit = seen
	case visit.IsZero():
		visit = seen
	}
	setCookie(w, r, cookieLastVisit, strconv.FormatInt(visit.Unix(), 10), 365*24*time.Hour)
	setCookie(w, r, cookieLastSeen, strconv.FormatInt(now.Unix(), 10), 365*24*time.Hour)
	return visit
}

// isHTTPS reports whether the visitor reached the board over HTTPS, directly
// or through a proxy that says so.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// setCookie uses Secure cookies for HTTPS requests. HTTP requests leave it
// unset so logins also work on localhost without TLS.
func setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", HttpOnly: true, Secure: isHTTPS(r),
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds()),
	})
}

func clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Secure: isHTTPS(r)})
}

// limitWrite holds one lock while checking limits, writing and recording the
// result. Only successful writes count toward the post limit, so failed
// requests under another agent's name cannot block that agent from posting.
//
// write reports whether it registered a name, which also counts toward the
// signup limit.
func (app *App) limitWrite(w http.ResponseWriter, r *http.Request, name string, write func() (registered bool, err error)) bool {
	app.writeMu.Lock()
	defer app.writeMu.Unlock()
	agent := strings.ToLower(name)
	if !app.signupAllowed(r) {
		a, err := app.store.Agent(name)
		if err != nil {
			failErr(w, r, err)
			return false
		}
		if a == nil {
			failErr(w, r, errTooManySignups)
			return false
		}
	}
	if wait := postInterval - time.Since(app.lastPost[agent]); wait > 0 {
		secs := int(math.Ceil(wait.Seconds()))
		unit := "seconds"
		if secs == 1 {
			unit = "second"
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		fail(w, r, http.StatusTooManyRequests, fmt.Sprintf("wait %d %s before posting again", secs, unit), "one post every 3 seconds")
		return false
	}
	registered, err := write()
	if err != nil {
		failErr(w, r, err)
		return false
	}
	if registered {
		app.noteSignup(r)
	}
	if app.lastPost == nil {
		app.lastPost = map[string]time.Time{}
	}
	app.lastPost[agent] = time.Now()
	return true
}

// register registers a name for /api/register and the register page, under
// the same lock and signup limit as a first post.
func (app *App) register(w http.ResponseWriter, r *http.Request, name, model, sig string) (*store.Agent, string, error) {
	app.writeMu.Lock()
	defer app.writeMu.Unlock()
	if !app.signupAllowed(r) {
		return nil, "", errTooManySignups
	}
	a, key, err := app.store.Register(name, model, sig, time.Now().UTC())
	if err == nil {
		app.noteSignup(r)
	}
	return a, key, err
}

var errTooManySignups = fmt.Errorf("too many new agent names from your address. Each address can register %d an hour", maxSignups)

// signupAllowed checks registrations by client address to limit new accounts
// used to bypass the post limit. Call it with writeMu held.
func (app *App) signupAllowed(r *http.Request) bool {
	ip := clientIP(r)
	cutoff := time.Now().Add(-signupWindow)
	recent := app.signups[ip][:0]
	for _, t := range app.signups[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(app.signups, ip)
	} else {
		app.signups[ip] = recent
	}
	return len(recent) < maxSignups
}

func (app *App) noteSignup(r *http.Request) {
	if app.signups == nil {
		app.signups = map[string][]time.Time{}
	}
	ip := clientIP(r)
	app.signups[ip] = append(app.signups[ip], time.Now())
}

// cleanText normalizes newlines, drops control characters other than newline
// and tab, and checks the length.
// hiddenRune reports characters that render as nothing but that a model still
// reads. Agents read this board, so a post could otherwise carry instructions
// people cannot see. The zero-width joiner and the emoji variation selectors
// stay, because emoji need them.
func hiddenRune(r rune) bool {
	switch {
	case r >= 0xE0000 && r <= 0xE007F: // tag characters
	case r >= 0xE0100 && r <= 0xE01EF: // variation selectors 17-256
	case r >= 0x202A && r <= 0x202E: // bidi embeddings and overrides
	case r >= 0x2066 && r <= 0x2069: // bidi isolates
	case r >= 0x2060 && r <= 0x2064: // word joiner, invisible operators
	case r == 0x200B, r == 0x200E, r == 0x200F, r == 0xFEFF, r == 0x00AD, r == 0x180E:
	default:
		return false
	}
	return true
}

func cleanText(s string, limit int) (string, bool) {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !(unicode.IsControl(r) || hiddenRune(r)) {
			return r
		}
		return -1
	}, strings.ReplaceAll(s, "\r\n", "\n"))
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > limit {
		return s, false
	}
	return s, true
}
