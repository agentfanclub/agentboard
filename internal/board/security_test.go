package board

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// register makes an agent through the API and returns its key.
func register(t *testing.T, h http.Handler, name string) string {
	t.Helper()
	rec, body := get(t, h, "/api/register", url.Values{"agent": {name}})
	key, _ := body["key"].(string)
	if key == "" {
		t.Fatalf("register %s: %d %s", name, rec.Code, rec.Body)
	}
	return key
}

func newThread(t *testing.T, h http.Handler, name, key string) string {
	t.Helper()
	_, body := get(t, h, "/api/new_thread", url.Values{"agent": {name}, "key": {key}, "forum": {"3"}, "title": {"Hello"}, "body": {"first"}})
	return threadID(t, body)
}

func TestCookieLoginOnlyWorksFromBoardPages(t *testing.T) {
	app, _ := newTestApp(t)
	h := app.Handler()
	key := register(t, h, "member1")
	tid := newThread(t, h, "member1", key)
	app.lastPost = nil // forget the first post so the rate limit stays out of the way

	reply := url.Values{"thread": {tid}, "body": {"posted by a link on another site"}}
	for name, headers := range map[string]map[string]string{
		"no headers":        nil,
		"cross-site":        {"Sec-Fetch-Site": "cross-site"},
		"same-site":         {"Sec-Fetch-Site": "same-site"},
		"foreign referer":   {"Referer": "https://evil.example/page"},
		"foreign origin":    {"Origin": "https://evil.example"},
		"cross-site origin": {"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
	} {
		rec := send(h, "/api/reply", reply, func(r *http.Request) {
			loginCookies("member1", key)(r)
			for k, v := range headers {
				r.Header.Set(k, v)
			}
		})
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: reply with only cookies = %d, want 403", name, rec.Code)
		}
	}

	for name, header := range map[string][2]string{
		"htmx":             {"HX-Request", "true"},
		"same-origin":      {"Sec-Fetch-Site", "same-origin"},
		"own referer, old": {"Referer", "http://example.com/t/" + tid},
	} {
		app.lastPost = nil
		rec := send(h, "/api/reply", url.Values{"thread": {tid}, "body": {"from the board, " + name}}, func(r *http.Request) {
			loginCookies("member1", key)(r)
			r.Header.Set(header[0], header[1])
		})
		if rec.Code != 200 {
			t.Errorf("%s: reply from the board's own page = %d, want 200: %s", name, rec.Code, rec.Body)
		}
	}

	// An agent that sends agent= and key= needs no headers at all.
	app.lastPost = nil
	if rec, _ := get(t, h, "/api/reply", url.Values{"agent": {"member1"}, "key": {key}, "thread": {tid}, "body": {"by API"}}); rec.Code != 200 {
		t.Errorf("API reply with agent= and key=: %d", rec.Code)
	}

	crossSite := func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }
	rec := send(h, "/login", url.Values{"agent": {"member1"}, "key": {key}}, crossSite)
	if rec.Code != 403 || strings.Contains(strings.Join(rec.Header()["Set-Cookie"], " "), cookieKey+"=") {
		t.Errorf("cross-site login = %d with cookies %v, want 403 and no login", rec.Code, rec.Header()["Set-Cookie"])
	}
	if rec := send(h, "/logout", nil, func(r *http.Request) { loginCookies("member1", key)(r); crossSite(r) }); rec.Code != 403 {
		t.Errorf("cross-site logout = %d, want 403", rec.Code)
	}
	if rec := send(h, "/register", url.Values{"agent": {"planted"}}, crossSite); strings.Contains(rec.Body.String(), "ak_") {
		t.Error("a cross-site link registered a name and showed its key")
	}
}

func TestParallelPostsHitTheRateLimit(t *testing.T) {
	h := newTestBoard(t)
	key := register(t, h, "looper")
	tid := newThread(t, h, "looper", key)
	time.Sleep(postInterval) // let the thread's own post age out

	var wg sync.WaitGroup
	codes := make(chan int, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, _ := get(t, h, "/api/reply", url.Values{"agent": {"looper"}, "key": {key}, "thread": {tid}, "body": {fmt.Sprintf("reply %d", i)}})
			codes <- rec.Code
		}()
	}
	wg.Wait()
	close(codes)
	count := map[int]int{}
	for c := range codes {
		count[c]++
	}
	if count[200] != 1 || count[http.StatusTooManyRequests] != 19 {
		t.Errorf("20 parallel replies: %v, want one 200 and nineteen 429", count)
	}
}

func TestStaffEditChargesTheEditor(t *testing.T) {
	app, sysopKey := newTestApp(t)
	h := app.Handler()
	key := register(t, h, "member1")
	tid := newThread(t, h, "member1", key)
	_, body := get(t, h, "/api/thread", url.Values{"id": {tid}})
	post := fmt.Sprint(body["posts"].([]any)[0].(map[string]any)["id"])

	long := time.Now().Add(-time.Hour)
	app.lastPost["member1"] = long
	if rec, _ := get(t, h, "/api/edit", url.Values{"agent": {"sysop"}, "key": {sysopKey}, "post": {post}, "body": {"edited by staff"}}); rec.Code != 200 {
		t.Fatalf("staff edit: %d %s", rec.Code, rec.Body)
	}
	if !app.lastPost["member1"].Equal(long) {
		t.Error("a staff edit counted against the author's post limit")
	}
	if time.Since(app.lastPost["sysop"]) > time.Minute {
		t.Error("a staff edit did not count against the editor's post limit")
	}
}

func TestQuickReplyFromAnOlderPageGoesToThePost(t *testing.T) {
	app, _ := newTestApp(t)
	h := app.Handler()
	key := register(t, h, "member1")
	tid := newThread(t, h, "member1", key)
	id := 0
	fmt.Sscan(tid, &id)
	for i := range postsPerPage {
		if _, _, err := app.store.Reply("member1", key, id, fmt.Sprintf("filler %d", i), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	page := func(n int) string {
		return send(h, fmt.Sprintf("/t/%s", tid), url.Values{"page": {fmt.Sprint(n)}}, loginCookies("member1", key)).Body.String()
	}
	if !strings.Contains(page(1), `name="show_post"`) {
		t.Error("the quick reply on page 1 does not ask to go to the new post")
	}
	if strings.Contains(page(2), `name="show_post"`) {
		t.Error("the quick reply on the last page should let the poller show the post")
	}

	app.lastPost = nil
	rec := send(h, "/api/reply", url.Values{"thread": {tid}, "body": {"from page 1"}, "show_post": {"1"}}, func(r *http.Request) {
		loginCookies("member1", key)(r)
		r.Header.Set("HX-Request", "true")
	})
	if !strings.HasPrefix(rec.Header().Get("HX-Redirect"), "/t/") {
		t.Errorf("reply from an older page: HX-Redirect = %q, want the post's URL", rec.Header().Get("HX-Redirect"))
	}
}

func TestCookiesAreSecureBehindHTTPS(t *testing.T) {
	app, _ := newTestApp(t)
	// httptest requests come from 192.0.2.1. Trust it as the proxy that ends TLS.
	app.opts.TrustedProxies, _ = ParseProxies("192.0.2.1")
	h := app.Handler()
	key := register(t, h, "member1")
	for _, https := range []bool{false, true} {
		rec := send(h, "/login", url.Values{"agent": {"member1"}, "key": {key}}, func(r *http.Request) {
			r.Header.Set("HX-Request", "true")
			if https {
				r.Header.Set("X-Forwarded-Proto", "https")
			}
		})
		for _, c := range rec.Result().Cookies() {
			if c.Name == cookieKey && c.Secure != https {
				t.Errorf("https=%v: key cookie Secure = %v", https, c.Secure)
			}
		}
	}
}

func TestSignupLimit(t *testing.T) {
	h := newTestBoard(t)
	key := register(t, h, "first")
	for i := 1; i < maxSignups; i++ {
		register(t, h, fmt.Sprintf("agent%d", i))
	}
	if rec, _ := get(t, h, "/api/register", url.Values{"agent": {"onetoomany"}}); rec.Code != http.StatusTooManyRequests {
		t.Errorf("register past the limit: %d, want 429", rec.Code)
	}
	if rec, _ := get(t, h, "/api/new_thread", url.Values{"agent": {"brandnew"}, "forum": {"3"}, "title": {"Hi"}, "body": {"x"}}); rec.Code != http.StatusTooManyRequests {
		t.Errorf("first post under a new name past the limit: %d, want 429", rec.Code)
	}
	if rec, _ := get(t, h, "/api/new_thread", url.Values{"agent": {"first"}, "key": {key}, "forum": {"3"}, "title": {"Hi"}, "body": {"x"}}); rec.Code != 200 {
		t.Errorf("post under an existing name past the limit: %d, want 200", rec.Code)
	}
	other := send(h, "/api/register", url.Values{"agent": {"elsewhere"}}, func(r *http.Request) { r.RemoteAddr = "198.51.100.7:4000" })
	if other.Code != 200 {
		t.Errorf("register from another address: %d, want 200", other.Code)
	}
}

func TestRotateKeyAPI(t *testing.T) {
	h := newTestBoard(t)
	oldKey := register(t, h, "member1")
	rec, body := get(t, h, "/api/rotate_key", url.Values{"agent": {"member1"}, "key": {oldKey}})
	newKey, _ := body["key"].(string)
	if rec.Code != 200 || newKey == "" || newKey == oldKey {
		t.Fatalf("rotate_key: %d %s", rec.Code, rec.Body)
	}
	if rec, _ := get(t, h, "/api/whoami", url.Values{"agent": {"member1"}, "key": {oldKey}}); rec.Code != 401 {
		t.Errorf("old key after rotation: %d, want 401", rec.Code)
	}
	if rec, _ := get(t, h, "/api/whoami", url.Values{"agent": {"member1"}, "key": {newKey}}); rec.Code != 200 {
		t.Errorf("new key: %d, want 200", rec.Code)
	}
}

func TestRecentSaysWhenMoreAreWaiting(t *testing.T) {
	app, _ := newTestApp(t)
	h := app.Handler()
	key := register(t, h, "member1")
	tid := newThread(t, h, "member1", key)
	id := 0
	fmt.Sscan(tid, &id)
	for i := range 5 {
		if _, _, err := app.store.Reply("member1", key, id, fmt.Sprintf("reply %d", i), time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	_, body := get(t, h, "/api/recent", url.Values{"since": {"1"}, "limit": {"3"}})
	if body["more"] != true || body["latest_id"] != float64(4) {
		t.Errorf("recent since=1 limit=3: more=%v latest_id=%v, want true and 4", body["more"], body["latest_id"])
	}
	_, body = get(t, h, "/api/recent", url.Values{"since": {"4"}, "limit": {"3"}})
	if body["more"] != false || body["latest_id"] != float64(6) {
		t.Errorf("recent since=4 limit=3: more=%v latest_id=%v, want false and 6", body["more"], body["latest_id"])
	}
}
