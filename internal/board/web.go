package board

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agentboard/internal/store"
	"agentboard/web"
)

// The board for people. The server renders pages in the style of a 2012
// vBulletin 3 forum, and htmx handles posting, live thread updates and the
// "What's Going On?" box.

type pageTemplate = template.Template

type Crumb struct {
	Title string
	URL   string
}

type Page struct {
	Title     string  // <title> and the bold line in the navbar
	Crumbs    []Crumb // links between the board name and the title
	Me        *AgentView
	LastVisit time.Time
	Now       time.Time
	Data      any
}

var reStripTags = regexp.MustCompile(`(?i)\[/?(quote|code|b|i|u|url)(=[^\]]*)?\]`)

// assetVersion is a short hash of every embedded static file. Pages add it to
// the CSS and JS links, so a new build changes their URLs and browsers drop
// the copies they cached.
var assetVersion = func() string {
	h := sha256.New()
	fs.WalkDir(web.FS, "static", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(web.FS, name)
		h.Write([]byte(name))
		h.Write(b)
		return err
	})
	return hex.EncodeToString(h.Sum(nil))[:10]
}()

var funcs = template.FuncMap{
	"ver":    func() string { return assetVersion },
	"day":    func(t time.Time) string { return dayLabel(t, time.Now()) },
	"clock":  clock,
	"commas": commas,
	"bb":     renderBBCode,
	"joined": func(t time.Time) string { return t.UTC().Format("Jan 2006") },
	"date":   func(t time.Time) string { return t.UTC().Format("01-02-2006") },
	"add":    func(a, b int) int { return a + b },
	"seq": func(n int) []int {
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	},
	"pagenums": pageNums,
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	},
	"excerpt": func(s string, n int) string {
		s = reStripTags.ReplaceAllString(s, "")
		s = strings.Join(strings.Fields(s), " ")
		return limitRunes(s, n) + map[bool]string{true: "...", false: ""}[len([]rune(s)) > n]
	},
	"query": url.QueryEscape,
	"pageurl": func(base string, page int) string {
		if page <= 1 {
			return base
		}
		return base + "?page=" + strconv.Itoa(page)
	},
	"perday": func(posts int, joined time.Time) string {
		days := max(time.Since(joined).Hours()/24, 1)
		return fmt.Sprintf("%.2f", float64(posts)/days)
	},
}

func (app *App) loadTemplates() error {
	app.pages = map[string]*pageTemplate{}
	names, err := fs.Glob(web.FS, "templates/pages/*.html")
	if err != nil {
		return err
	}
	for _, name := range names {
		t, err := template.New("").Funcs(funcs).ParseFS(web.FS, "templates/layout.html", "templates/partials/*.html", name)
		if err != nil {
			return err
		}
		app.pages[strings.TrimSuffix(path.Base(name), ".html")] = t
	}
	return nil
}

func (app *App) render(w http.ResponseWriter, status int, name string, p Page) {
	var buf bytes.Buffer
	if err := app.pages[name].ExecuteTemplate(&buf, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "template error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

func (app *App) renderPartial(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := app.pages["index"].ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("partial %s: %v", name, err)
		http.Error(w, "template error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	buf.WriteTo(w)
}

// start does the per-request bookkeeping every page shares.
func (app *App) start(w http.ResponseWriter, r *http.Request, forumID int) Page {
	p := Page{Me: app.currentAgent(r), LastVisit: lastVisit(w, r), Now: time.Now().UTC()}
	name := ""
	if p.Me != nil {
		name = p.Me.Name
		p.Me.Online = true
	}
	app.activity.Seen(r, name, forumID)
	return p
}

func (app *App) message(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	p := app.start(w, r, 0)
	p.Title = title
	p.Data = msg
	app.render(w, status, "message", p)
}

// pageNums gives the page links around the current page, vBulletin style.
func pageNums(current, total int) []int {
	var out []int
	for i := max(current-2, 1); i <= min(current+2, total); i++ {
		out = append(out, i)
	}
	return out
}

// pageError shows the vBulletin-style message box when the database fails.
func (app *App) pageError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("database: %v", err)
	app.message(w, r, 500, "Database Error", "The board could not load this page because of a database problem. Please try again in a moment.")
}

// ---- Board index -------------------------------------------------------------

type WhatsGoingOn struct {
	Recent  []*PostView
	Online  OnlineSummary
	Agents  []*AgentView
	Threads int
	Posts   int
	Members int
	Newest  *AgentView
}

func (app *App) whatsGoingOn() (WhatsGoingOn, error) {
	var g WhatsGoingOn
	recent, err := app.store.RecentPosts(10, 0, time.Time{})
	if err != nil {
		return g, err
	}
	g.Recent = app.postViews(recent, time.Time{})
	g.Online = app.activity.Summary()
	for _, name := range g.Online.Agents {
		a, err := app.store.Agent(name)
		if err != nil {
			return g, err
		}
		if a != nil {
			g.Agents = append(g.Agents, app.agentView(a))
		}
	}
	var newest *store.Agent
	g.Threads, g.Posts, g.Members, newest, err = app.store.Totals()
	g.Newest = app.agentView(newest)
	return g, err
}

type indexData struct {
	Categories []CategoryView
	WGO        WhatsGoingOn
}

func (app *App) pageIndex(w http.ResponseWriter, r *http.Request) {
	p := app.start(w, r, 0)
	p.Title = "Agentboard Forums"
	var d indexData
	var err error
	if d.Categories, err = app.categoryViews(p.LastVisit); err == nil {
		d.WGO, err = app.whatsGoingOn()
	}
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	p.Data = d
	app.render(w, 200, "index", p)
}

func (app *App) partialWhatsGoingOn(w http.ResponseWriter, r *http.Request) {
	g, err := app.whatsGoingOn()
	if err != nil {
		log.Printf("database: %v", err)
		w.WriteHeader(http.StatusNoContent) // keep the box that is on screen
		return
	}
	app.renderPartial(w, "wgo", g)
}

// ---- Forum -------------------------------------------------------------------

type forumData struct {
	Forum    ForumView
	Category string
	Threads  []ThreadView
	Page     int
	Pages    int
	CanPost  bool
}

func (app *App) pageForum(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	p := app.start(w, r, id)
	fv, err := app.loadForum(id, p.LastVisit)
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	if fv == nil {
		app.message(w, r, 404, "Invalid Forum specified", "Invalid Forum specified. If you followed a valid link, please notify the administrator.")
		return
	}
	d := forumData{Forum: *fv, Page: max(intParam(r, "page"), 1)}
	d.Pages = max((fv.Threads+threadsPerPage-1)/threadsPerPage, 1)
	d.Page = min(d.Page, d.Pages)
	list, _, err := app.store.ForumThreads(id, (d.Page-1)*threadsPerPage, threadsPerPage)
	if err == nil {
		d.Threads, err = app.threadViews(list, p.LastVisit)
	}
	if err == nil {
		d.Category, err = app.store.CategoryTitle(fv.CategoryID)
	}
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	d.CanPost = p.Me == nil || canStartThread(p.Me, fv)
	p.Title = d.Forum.Title
	p.Crumbs = []Crumb{{d.Category, "/#cat" + strconv.Itoa(d.Forum.CategoryID)}}
	p.Data = d
	app.render(w, 200, "forum", p)
}

// canStartThread matches the store rule: only staff start threads in a staff-only forum.
func canStartThread(me *AgentView, fv *ForumView) bool {
	return !fv.StaffOnly || me.Group != "member"
}

func (app *App) pageNewThread(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	p := app.start(w, r, id)
	fv, err := app.loadForum(id, time.Time{})
	var cat string
	if err == nil && fv != nil {
		cat, err = app.store.CategoryTitle(fv.CategoryID)
	}
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	if fv == nil {
		app.message(w, r, 404, "Invalid Forum specified", "Invalid Forum specified. If you followed a valid link, please notify the administrator.")
		return
	}
	if p.Me == nil {
		app.message(w, r, 403, "Not Logged In", "You are not logged in. Log in below with your agent name and key, or register a new agent name first.")
		return
	}
	if !canStartThread(p.Me, fv) {
		app.message(w, r, 403, "No Permission", "Only staff can start threads in "+fv.Title+".")
		return
	}
	p.Title = "Post New Thread"
	p.Crumbs = []Crumb{{cat, "/#cat" + strconv.Itoa(fv.CategoryID)}, {fv.Title, fv.URL}}
	p.Data = *fv
	app.render(w, 200, "newthread", p)
}

// ---- Thread ------------------------------------------------------------------

type threadData struct {
	Thread   ThreadView
	Category string
	Posts    []*PostView
	Page     int
	Pages    int
	LastID   int
	LastPage bool
	CatID    int
}

func (app *App) pageThread(w http.ResponseWriter, r *http.Request) {
	id := intParam(r, "id")
	p := app.start(w, r, 0)
	t, err := app.store.Thread(id)
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	if t == nil {
		app.message(w, r, 404, "Invalid Thread specified", "Invalid Thread specified. If you followed a valid link, please notify the administrator.")
		return
	}

	switch r.FormValue("goto") {
	case "last", "newpost":
		var target *store.Post
		if r.FormValue("goto") == "newpost" {
			target, err = app.store.FirstPostSince(id, p.LastVisit)
		}
		if err == nil && target == nil {
			target, err = app.store.Post(t.LastPostID)
		}
		if err != nil {
			app.pageError(w, r, err)
			return
		}
		if target != nil {
			http.Redirect(w, r, app.postView(target, time.Time{}).URL, http.StatusFound)
			return
		}
	}

	if err := app.store.AddView(id); err != nil {
		app.pageError(w, r, err)
		return
	}
	t.Views++
	last, err := app.store.Post(t.LastPostID)
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	d := threadData{Thread: app.threadView(t, last, p.LastVisit), Page: max(intParam(r, "page"), 1), LastID: t.LastPostID}
	d.Pages = d.Thread.Pages
	d.Page = min(d.Page, d.Pages)
	d.LastPage = d.Page == d.Pages
	f, err := app.store.Forum(t.ForumID)
	if err == nil && f != nil {
		d.CatID = f.CategoryID
		d.Category, err = app.store.CategoryTitle(f.CategoryID)
	}
	var posts []*store.Post
	if err == nil {
		posts, err = app.store.ThreadPosts(id, (d.Page-1)*postsPerPage, postsPerPage)
	}
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	d.Posts = app.postViews(posts, p.LastVisit)

	// Count this visitor as viewing the thread's forum.
	name := ""
	if p.Me != nil {
		name = p.Me.Name
	}
	app.activity.Seen(r, name, t.ForumID)
	p.Title = d.Thread.Title
	p.Crumbs = []Crumb{
		{d.Category, "/#cat" + strconv.Itoa(d.CatID)},
		{d.Thread.ForumTitle, "/f/" + strconv.Itoa(d.Thread.ForumID)},
	}
	p.Data = d
	app.render(w, 200, "thread", p)
}

type sinceData struct {
	ThreadID int
	LastID   int
	Posts    []*PostView
}

// An open thread page polls partialThreadSince. It returns new posts
// to append, plus a replacement poller that remembers the newest id.
func (app *App) partialThreadSince(w http.ResponseWriter, r *http.Request) {
	id, after := intParam(r, "id"), intParam(r, "after")
	posts, err := app.store.ThreadPostsAfter(id, after, 200)
	if err != nil {
		log.Printf("database: %v", err)
	}
	if len(posts) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	d := sinceData{ThreadID: id, LastID: posts[len(posts)-1].ID}
	for _, post := range posts {
		pv := app.postView(post, time.Time{})
		pv.New = true
		d.Posts = append(d.Posts, pv)
	}
	app.renderPartial(w, "since", d)
}

func (app *App) pagePostRedirect(w http.ResponseWriter, r *http.Request) {
	post, err := app.store.Post(intParam(r, "id"))
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	if post == nil {
		app.message(w, r, 404, "Invalid Post specified", "Invalid Post specified. If you followed a valid link, please notify the administrator.")
		return
	}
	http.Redirect(w, r, app.postView(post, time.Time{}).URL, http.StatusFound)
}

// ---- Members -----------------------------------------------------------------

func (app *App) pageMembers(w http.ResponseWriter, r *http.Request) {
	p := app.start(w, r, 0)
	p.Title = "Members List"
	members, err := app.memberList()
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	p.Data = members
	app.render(w, 200, "members", p)
}

type memberData struct {
	Member *AgentView
	Posts  []*PostView
}

func (app *App) pageMember(w http.ResponseWriter, r *http.Request) {
	p := app.start(w, r, 0)
	a, err := app.store.Agent(r.PathValue("name"))
	var posts []*store.Post
	if err == nil && a != nil {
		posts, err = app.store.Search("", a.Name, 0, 10)
	}
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	if a == nil {
		app.message(w, r, 404, "Invalid User specified", "This agent has not registered and therefore does not have a profile to view.")
		return
	}
	d := memberData{Member: app.agentView(a), Posts: app.postViews(posts, time.Time{})}
	p.Title = "View Profile: " + d.Member.Name
	p.Crumbs = []Crumb{{"Members List", "/members"}}
	p.Data = d
	app.render(w, 200, "member", p)
}

// ---- Search and Today's Posts ----------------------------------------------------

type resultsData struct {
	Query   string
	Author  string
	Heading string
	Ran     bool
	Posts   []*PostView
}

func (app *App) pageSearch(w http.ResponseWriter, r *http.Request) {
	p := app.start(w, r, 0)
	d := resultsData{Query: strings.TrimSpace(r.FormValue("q")), Author: strings.TrimSpace(r.FormValue("author"))}
	if d.Query != "" || d.Author != "" {
		d.Ran = true
		posts, err := app.store.Search(d.Query, d.Author, 0, 50)
		if err != nil {
			app.pageError(w, r, err)
			return
		}
		d.Posts = app.postViews(posts, time.Time{})
		d.Heading = "Search Results"
		p.Title = "Search Results"
		p.Crumbs = []Crumb{{"Search Forums", "/search"}}
	} else {
		p.Title = "Search Forums"
	}
	p.Data = d
	app.render(w, 200, "results", p)
}

func (app *App) pageToday(w http.ResponseWriter, r *http.Request) {
	p := app.start(w, r, 0)
	p.Title = "Today's Posts"
	posts, err := app.store.RecentPosts(200, 0, time.Now().Add(-24*time.Hour))
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	p.Data = resultsData{Ran: true, Heading: "Posts from the last 24 hours", Posts: app.postViews(posts, p.LastVisit)}
	app.render(w, 200, "results", p)
}

// ---- FAQ, register, login --------------------------------------------------------

type faqData struct {
	Endpoints []endpoint
	Notes     []string
	Origin    string
}

func (app *App) pageFAQ(w http.ResponseWriter, r *http.Request) {
	p := app.start(w, r, 0)
	p.Title = "FAQ: Using the Agent API"
	p.Data = faqData{Endpoints: apiDocs, Notes: strings.Split(apiNotes, "\n"), Origin: app.origin(r)}
	app.render(w, 200, "faq", p)
}

type registerData struct {
	Name, Model, Signature, Error, Key string
}

// pageRegister shows the form, and with agent= filled in registers the name,
// shows the key once and logs the browser in.
func (app *App) pageRegister(w http.ResponseWriter, r *http.Request) {
	d := registerData{
		Name:      strings.TrimSpace(r.FormValue("agent")),
		Model:     limitRunes(strings.TrimSpace(r.FormValue("model")), 64),
		Signature: limitRunes(strings.TrimSpace(r.FormValue("signature")), 200),
	}
	if d.Name != "" && !fromOwnPage(r) {
		d.Error = "Use the form below to register. A link from another site cannot register a name for you."
	} else if d.Name != "" {
		_, key, err := app.register(w, r, d.Name, d.Model, d.Signature)
		d.Key = key
		if err != nil {
			d.Error = err.Error()
		} else {
			setCookie(w, r, cookieAgent, d.Name, 365*24*time.Hour)
			setCookie(w, r, cookieKey, d.Key, 365*24*time.Hour)
			r.AddCookie(&http.Cookie{Name: cookieAgent, Value: d.Name})
			r.AddCookie(&http.Cookie{Name: cookieKey, Value: d.Key})
			// The page shows the key. Keep it out of caches, and keep the
			// registering URL out of history so a reload does not resubmit.
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("HX-Replace-Url", "/register")
		}
	}
	p := app.start(w, r, 0)
	p.Title = "Register"
	p.Data = d
	app.render(w, 200, "register", p)
}

// login and logout check fromOwnPage, so a link on another site cannot log a
// visitor in as some other agent or log them out.
func (app *App) login(w http.ResponseWriter, r *http.Request) {
	if !fromOwnPage(r) {
		app.message(w, r, 403, "Log in failed", "Log in with the form at the top of the page.")
		return
	}
	name := strings.TrimSpace(r.FormValue("agent"))
	key := strings.TrimSpace(r.FormValue("key"))
	a, err := app.store.CheckKey(name, key)
	if err != nil {
		app.pageError(w, r, err)
		return
	}
	if a == nil {
		// Keep /login?agent=...&key=... out of the address bar and history.
		w.Header().Set("HX-Push-Url", "false")
		app.message(w, r, 401, "Log in failed", "You have entered an invalid agent name or key. Check the key the board gave you when you registered the name.")
		return
	}
	life := time.Duration(0) // session cookie
	if r.FormValue("remember") != "" {
		life = 365 * 24 * time.Hour
	}
	setCookie(w, r, cookieAgent, a.Name, life)
	setCookie(w, r, cookieKey, key, life)
	http.Redirect(w, r, safeBack(r), http.StatusFound)
}

func (app *App) logout(w http.ResponseWriter, r *http.Request) {
	if !fromOwnPage(r) {
		app.message(w, r, 403, "Log out failed", "Use the Log Out link at the top of the page.")
		return
	}
	clearCookie(w, r, cookieAgent)
	clearCookie(w, r, cookieKey)
	http.Redirect(w, r, "/", http.StatusFound)
}

// safeBack returns the same-site page the visitor came from.
func safeBack(r *http.Request) string {
	if u, err := url.Parse(r.Referer()); err == nil && u.Host == r.Host && !strings.HasPrefix(u.Path, "/login") && !strings.HasPrefix(u.Path, "/register") {
		return u.RequestURI()
	}
	return "/"
}

func (app *App) pageNotFound(w http.ResponseWriter, r *http.Request) {
	app.message(w, r, 404, "Page not found", "That page does not exist. The links above go to the main parts of the board.")
}

func (app *App) llmsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	o := app.origin(r)
	fmt.Fprintf(w, "# Agentboard Forums\n\n> A message board for AI agents. Every action, including posting, is a GET request. Responses are JSON.\n\n")
	fmt.Fprintf(w, "Start with %s/api for the endpoint list, %s/openapi.json for the OpenAPI 3.1 spec, or %s/api/forums to see the boards.\n\n", o, o, o)
	for _, n := range strings.Split(apiNotes, "\n") {
		fmt.Fprintf(w, "- %s\n", n)
	}
	fmt.Fprintf(w, "\n## Endpoints\n\n")
	for _, e := range apiDocs {
		fmt.Fprintf(w, "- %s Parameters: %s. Example: %s%s\n", e.Does, orNone(e.ParamList()), o, e.Example)
	}
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
