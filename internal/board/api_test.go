package board

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"agentboard/internal/store"
)

func newTestBoard(t *testing.T) http.Handler {
	t.Helper()
	app, _ := newTestApp(t)
	return app.Handler()
}

// newTestApp returns a board on a fresh database and the sysop's key.
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	st, sysopKey, err := store.Open(filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	app, err := New(st, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return app, sysopKey
}

// send serves one request built by build, for tests that need headers or cookies.
func send(h http.Handler, path string, params url.Values, build func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path+"?"+params.Encode(), nil)
	if build != nil {
		build(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func loginCookies(name, key string) func(*http.Request) {
	return func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieAgent, Value: name})
		r.AddCookie(&http.Cookie{Name: cookieKey, Value: key})
	}
}

func get(t *testing.T, h http.Handler, path string, params url.Values) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	if params != nil {
		path += "?" + params.Encode()
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func TestPostingFlow(t *testing.T) {
	h := newTestBoard(t)

	rec, body := get(t, h, "/api/new_thread", url.Values{"agent": {"ledger7"}, "forum": {"3"}, "title": {"Hello"}, "body": {"first"}})
	if rec.Code != 200 || body["ok"] != true {
		t.Fatalf("new_thread: %d %s", rec.Code, rec.Body)
	}
	key, _ := body["key"].(string)
	if key == "" {
		t.Fatal("new_thread with a new name should return a key")
	}
	tid := threadID(t, body)

	reply := url.Values{"agent": {"ledger7"}, "key": {key}, "thread": {tid}, "body": {"second"}}
	if rec, _ := get(t, h, "/api/reply", reply); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("reply right after posting: %d, Retry-After %q; want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}

	_, reg := get(t, h, "/api/register", url.Values{"agent": {"other1"}})
	otherKey, _ := reg["key"].(string)
	if rec, _ := get(t, h, "/api/reply", url.Values{"agent": {"other1"}, "key": {"ak_wrong"}, "thread": {tid}, "body": {"x"}}); rec.Code != 401 {
		t.Errorf("reply with wrong key: %d, want 401", rec.Code)
	}
	if rec, _ := get(t, h, "/api/reply", url.Values{"agent": {"other1"}, "key": {otherKey}, "thread": {tid}, "body": {"hi"}}); rec.Code != 200 {
		t.Errorf("reply from another agent: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := get(t, h, "/api/new_thread", url.Values{"agent": {"third1"}, "forum": {"1"}, "title": {"x"}, "body": {"y"}}); rec.Code != 403 {
		t.Errorf("member posting in staff forum: %d, want 403", rec.Code)
	}
	if rec, body := get(t, h, "/api/thread", url.Values{"id": {tid}}); rec.Code != 200 || body["ok"] != true {
		t.Errorf("read thread: %d %s", rec.Code, rec.Body)
	}
}

func TestHTMXReplyAnswersWithHeaders(t *testing.T) {
	h := newTestBoard(t)
	_, body := get(t, h, "/api/new_thread", url.Values{"agent": {"ledger7"}, "forum": {"3"}, "title": {"Hello"}, "body": {"first"}})
	tid := threadID(t, body)

	req := httptest.NewRequest("GET", "/api/reply?"+url.Values{"agent": {"other1"}, "thread": {tid}, "body": {"hi"}}.Encode(), nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("HX-Trigger") != "ab-posted" {
		t.Errorf("htmx reply: HX-Trigger = %q, want ab-posted", rec.Header().Get("HX-Trigger"))
	}
	// other1 was a new name. The page cannot show a key, so the board logs the browser in.
	var gotKey string
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieKey {
			gotKey = c.Value
		}
	}
	if !strings.HasPrefix(gotKey, "ak_") {
		t.Errorf("htmx first post: no key cookie in %v", rec.Header()["Set-Cookie"])
	}
}

func TestPagesRender(t *testing.T) {
	h := newTestBoard(t)
	for path, want := range map[string]int{
		"/": 200, "/faq": 200, "/register": 200, "/members": 200, "/search": 200, "/today": 200,
		"/f/3": 200, "/f/999": 404, "/t/999": 404, "/nope": 404,
		"/api": 200, "/api/nope": 404, "/llms.txt": 200, "/openapi.json": 200, "/static/style.css": 200,
	} {
		rec, _ := get(t, h, path, nil)
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestOpenAPICoversEveryEndpoint(t *testing.T) {
	h := newTestBoard(t)
	_, spec := get(t, h, "/openapi.json", nil)
	if spec["openapi"] == nil {
		t.Fatal("/openapi.json has no openapi version")
	}
	paths, _ := spec["paths"].(map[string]any)
	for _, e := range apiDocs {
		if _, ok := paths[e.Path]; !ok {
			t.Errorf("spec is missing %s", e.Path)
		}
	}
}

// threadID reads the thread id from a write response.
func threadID(t *testing.T, body map[string]any) string {
	t.Helper()
	thread, _ := body["thread"].(map[string]any)
	id, ok := thread["id"].(float64)
	if !ok {
		t.Fatalf("response has no thread id: %v", body)
	}
	return strconv.Itoa(int(id))
}

func TestNewThreadPage(t *testing.T) {
	h := newTestBoard(t)
	_, reg := get(t, h, "/api/register", url.Values{"agent": {"member1"}})
	key, _ := reg["key"].(string)

	page := func(path string, loggedIn bool) int {
		req := httptest.NewRequest("GET", path, nil)
		if loggedIn {
			req.AddCookie(&http.Cookie{Name: cookieAgent, Value: "member1"})
			req.AddCookie(&http.Cookie{Name: cookieKey, Value: key})
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := page("/f/4/new", false); code != 403 {
		t.Errorf("logged out: %d, want 403 with a login form", code)
	}
	if code := page("/f/4/new", true); code != 200 {
		t.Errorf("member in an open forum: %d, want 200", code)
	}
	if code := page("/f/1/new", true); code != 403 {
		t.Errorf("member in the staff-only forum: %d, want 403", code)
	}
}

func TestRegisterPageKeepsKeyOutOfCaches(t *testing.T) {
	h := newTestBoard(t)
	rec := send(h, "/register", url.Values{"agent": {"member1"}}, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-origin") })
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("HX-Replace-Url") != "/register" {
		t.Errorf("headers = %v, want Cache-Control no-store and HX-Replace-Url /register", rec.Header())
	}
}
