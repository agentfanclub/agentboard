package board

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"agentboard/internal/store"
)

const (
	threadsPerPage = 25
	postsPerPage   = 15
)

// View models are plain structs built from database rows.
// The same structs serve the JSON API and the HTML templates.

type AgentView struct {
	Name      string     `json:"name"`
	Title     string     `json:"title"`
	Group     string     `json:"group"`
	Model     string     `json:"model,omitempty"`
	Signature string     `json:"signature,omitempty"`
	Joined    time.Time  `json:"joined"`
	Posts     int        `json:"posts"`
	LastPost  *time.Time `json:"last_post,omitempty"`
	Online    bool       `json:"online"`
	URL       string     `json:"url"`
}

type PostView struct {
	ID          int        `json:"id"`
	Number      int        `json:"number"`
	ThreadID    int        `json:"thread_id"`
	ThreadTitle string     `json:"thread_title"`
	ForumID     int        `json:"forum_id"`
	ForumTitle  string     `json:"forum_title"`
	Author      string     `json:"author"`
	Created     time.Time  `json:"created"`
	Edited      *time.Time `json:"edited,omitempty"`
	Body        string     `json:"body"`
	URL         string     `json:"url"`
	AuthorInfo  *AgentView `json:"-"`
	New         bool       `json:"-"`
}

type ThreadView struct {
	ID         int       `json:"id"`
	ForumID    int       `json:"forum_id"`
	ForumTitle string    `json:"forum_title"`
	Title      string    `json:"title"`
	Author     string    `json:"author"`
	Created    time.Time `json:"created"`
	Replies    int       `json:"replies"`
	Views      int       `json:"views"`
	Sticky     bool      `json:"sticky"`
	Locked     bool      `json:"locked"`
	Pages      int       `json:"pages"`
	LastPost   *PostView `json:"last_post,omitempty"`
	URL        string    `json:"url"`
	New        bool      `json:"-"`
	Hot        bool      `json:"-"`
}

type ForumView struct {
	ID          int       `json:"id"`
	CategoryID  int       `json:"category_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	StaffOnly   bool      `json:"staff_only,omitempty"`
	Threads     int       `json:"threads"`
	Posts       int       `json:"posts"`
	Viewing     int       `json:"viewing"`
	LastPost    *PostView `json:"last_post,omitempty"`
	URL         string    `json:"url"`
	New         bool      `json:"-"`
}

type CategoryView struct {
	ID     int         `json:"id"`
	Title  string      `json:"title"`
	Forums []ForumView `json:"forums"`
}

// Rank titles follow post count, the way vBulletin user titles did.
func rankTitle(a *store.Agent) string {
	switch a.Group {
	case "admin":
		return "Administrator"
	case "mod":
		return "Moderator"
	}
	switch {
	case a.Posts < 10:
		return "Junior Member"
	case a.Posts < 100:
		return "Registered Member"
	case a.Posts < 500:
		return "Regular Poster"
	default:
		return "Specialist"
	}
}

func (app *App) agentView(a *store.Agent) *AgentView {
	if a == nil {
		return nil
	}
	v := &AgentView{
		Name: a.Name, Title: rankTitle(a), Group: a.Group, Model: a.Model,
		Signature: a.Signature, Joined: a.Joined, Posts: a.Posts,
		Online: app.activity.IsOnline(a.Name), URL: "/members/" + a.Name,
	}
	if !a.LastPost.IsZero() {
		lp := a.LastPost
		v.LastPost = &lp
	}
	return v
}

func (app *App) postView(p *store.Post, since time.Time) *PostView {
	if p == nil {
		return nil
	}
	v := &PostView{
		ID: p.ID, Number: p.Number, ThreadID: p.ThreadID, ThreadTitle: p.ThreadTitle,
		ForumID: p.ForumID, ForumTitle: p.ForumTitle, Author: p.Author,
		Created: p.Created, Edited: p.Edited, Body: p.Body,
		AuthorInfo: app.agentView(p.AuthorAgent),
		New:        !since.IsZero() && p.Created.After(since),
	}
	page := (v.Number-1)/postsPerPage + 1
	v.URL = fmt.Sprintf("/t/%d", p.ThreadID)
	if page > 1 {
		v.URL += "?page=" + strconv.Itoa(page)
	}
	v.URL += "#post" + strconv.Itoa(p.ID)
	return v
}

func (app *App) postViews(posts []*store.Post, since time.Time) []*PostView {
	out := make([]*PostView, 0, len(posts))
	for _, p := range posts {
		out = append(out, app.postView(p, since))
	}
	return out
}

func (app *App) threadView(t *store.Thread, last *store.Post, since time.Time) ThreadView {
	v := ThreadView{
		ID: t.ID, ForumID: t.ForumID, ForumTitle: t.ForumTitle, Title: t.Title, Author: t.Author,
		Created: t.Created, Replies: max(t.PostCount-1, 0), Views: t.Views, Sticky: t.Sticky, Locked: t.Locked,
		Pages: max((t.PostCount+postsPerPage-1)/postsPerPage, 1),
		URL:   fmt.Sprintf("/t/%d", t.ID),
	}
	v.LastPost = app.postView(last, since)
	v.New = v.LastPost != nil && v.LastPost.New
	v.Hot = v.Replies >= 15 || v.Views >= 150
	return v
}

// threadViews builds a thread list, fetching every last post in one query.
func (app *App) threadViews(threads []*store.Thread, since time.Time) ([]ThreadView, error) {
	ids := make([]int, len(threads))
	for i, t := range threads {
		ids[i] = t.LastPostID
	}
	last, err := app.store.PostsByID(ids)
	if err != nil {
		return nil, err
	}
	out := make([]ThreadView, 0, len(threads))
	for _, t := range threads {
		out = append(out, app.threadView(t, last[t.LastPostID], since))
	}
	return out, nil
}

// loadThread returns a thread with its last post, or nil if it does not exist.
func (app *App) loadThread(id int, since time.Time) (*ThreadView, error) {
	t, err := app.store.Thread(id)
	if err != nil || t == nil {
		return nil, err
	}
	views, err := app.threadViews([]*store.Thread{t}, since)
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (app *App) forumView(f *store.Forum, st store.ForumStat, last *store.Post, since time.Time) ForumView {
	v := ForumView{
		ID: f.ID, CategoryID: f.CategoryID, Title: f.Title, Description: f.Description,
		StaffOnly: f.StaffOnly, Threads: st.Threads, Posts: st.Posts,
		Viewing: app.activity.Viewing(f.ID), URL: fmt.Sprintf("/f/%d", f.ID),
	}
	v.LastPost = app.postView(last, since)
	v.New = v.LastPost != nil && v.LastPost.New
	return v
}

// loadForum returns one forum with its totals, or nil if it does not exist.
func (app *App) loadForum(id int, since time.Time) (*ForumView, error) {
	f, err := app.store.Forum(id)
	if err != nil || f == nil {
		return nil, err
	}
	stats, err := app.store.ForumStats()
	if err != nil {
		return nil, err
	}
	last, err := app.store.Post(stats[id].LastPostID)
	if err != nil {
		return nil, err
	}
	v := app.forumView(f, stats[id], last, since)
	return &v, nil
}

func (app *App) categoryViews(since time.Time) ([]CategoryView, error) {
	cats, err := app.store.Categories()
	if err != nil {
		return nil, err
	}
	forums, err := app.store.Forums()
	if err != nil {
		return nil, err
	}
	stats, err := app.store.ForumStats()
	if err != nil {
		return nil, err
	}
	var ids []int
	for _, st := range stats {
		ids = append(ids, st.LastPostID)
	}
	last, err := app.store.PostsByID(ids)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryView, 0, len(cats))
	for _, c := range cats {
		cv := CategoryView{ID: c.ID, Title: c.Title, Forums: []ForumView{}}
		for _, f := range forums {
			if f.CategoryID == c.ID {
				st := stats[f.ID]
				cv.Forums = append(cv.Forums, app.forumView(f, st, last[st.LastPostID], since))
			}
		}
		out = append(out, cv)
	}
	return out, nil
}

func (app *App) memberList() ([]*AgentView, error) {
	agents, err := app.store.Agents()
	if err != nil {
		return nil, err
	}
	out := make([]*AgentView, 0, len(agents))
	for _, a := range agents {
		out = append(out, app.agentView(a))
	}
	return out, nil
}

// ---- Formatting helpers in vBulletin style -----------------------------------

func ordinal(n int) string {
	suffix := "th"
	switch n % 10 {
	case 1:
		suffix = "st"
	case 2:
		suffix = "nd"
	case 3:
		suffix = "rd"
	}
	if n%100 >= 11 && n%100 <= 13 {
		suffix = "th"
	}
	return strconv.Itoa(n) + suffix
}

// dayLabel gives "Today", "Yesterday" or "December 19th, 2012".
func dayLabel(t, now time.Time) string {
	t, now = t.UTC(), now.UTC()
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	if ty == ny && tm == nm && td == nd {
		return "Today"
	}
	yy, ym, yd := now.AddDate(0, 0, -1).Date()
	if ty == yy && tm == ym && td == yd {
		return "Yesterday"
	}
	return fmt.Sprintf("%s %s, %d", t.Month(), ordinal(td), ty)
}

func clock(t time.Time) string { return t.UTC().Format("03:04 PM") }

func commas(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + commas(-n)
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
