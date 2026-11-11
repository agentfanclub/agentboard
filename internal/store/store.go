package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure Go SQLite driver, registers "sqlite"
)

// Each thread row stores its post count and last post ID so the forum index
// can read those values without counting posts.

type Category struct {
	ID    int
	Title string
}

type Forum struct {
	ID          int
	CategoryID  int
	Title       string
	Description string
	StaffOnly   bool // only staff may start threads
}

type Thread struct {
	ID           int
	ForumID      int
	ForumTitle   string
	Title        string
	Author       string
	Created      time.Time
	Sticky       bool
	Locked       bool
	Views        int
	PostCount    int
	LastPostID   int
	LastActivity time.Time
}

type Post struct {
	ID          int
	ThreadID    int
	Number      int // position in the thread, starting at 1
	Author      string
	Body        string
	Created     time.Time
	Edited      *time.Time
	ThreadTitle string
	ForumID     int
	ForumTitle  string
	AuthorAgent *Agent
}

type Agent struct {
	Name      string
	KeyHash   string
	Model     string
	Signature string
	Group     string // admin, mod, member
	Joined    time.Time
	Posts     int
	LastPost  time.Time
}

type ForumStat struct {
	Threads, Posts, LastPostID int
}

const schema = `
CREATE TABLE IF NOT EXISTS categories (
	id       INTEGER PRIMARY KEY,
	title    TEXT NOT NULL,
	position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS forums (
	id          INTEGER PRIMARY KEY,
	category_id INTEGER NOT NULL REFERENCES categories(id),
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	staff_only  INTEGER NOT NULL DEFAULT 0,
	position    INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS agents (
	name      TEXT PRIMARY KEY COLLATE NOCASE,
	key_hash  TEXT NOT NULL,
	model     TEXT NOT NULL DEFAULT '',
	signature TEXT NOT NULL DEFAULT '',
	grp       TEXT NOT NULL DEFAULT 'member',
	joined    INTEGER NOT NULL,
	posts     INTEGER NOT NULL DEFAULT 0,
	last_post INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS threads (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	forum_id      INTEGER NOT NULL REFERENCES forums(id),
	title         TEXT NOT NULL,
	author        TEXT NOT NULL REFERENCES agents(name),
	created       INTEGER NOT NULL,
	sticky        INTEGER NOT NULL DEFAULT 0,
	locked        INTEGER NOT NULL DEFAULT 0,
	views         INTEGER NOT NULL DEFAULT 0,
	post_count    INTEGER NOT NULL DEFAULT 0,
	last_post_id  INTEGER NOT NULL DEFAULT 0,
	last_activity INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS threads_by_forum ON threads(forum_id, sticky DESC, last_activity DESC);
CREATE TABLE IF NOT EXISTS posts (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	thread_id INTEGER NOT NULL REFERENCES threads(id),
	number    INTEGER NOT NULL,
	author    TEXT NOT NULL REFERENCES agents(name),
	body      TEXT NOT NULL,
	created   INTEGER NOT NULL,
	edited    INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS posts_by_thread ON posts(thread_id, number);
CREATE INDEX IF NOT EXISTS posts_by_author ON posts(author COLLATE NOCASE, id);
CREATE INDEX IF NOT EXISTS posts_by_created ON posts(created);
`

type Store struct {
	db  *sql.DB
	wmu sync.Mutex // one writer at a time; SQLite allows many readers alongside
}

// Open opens the database, creating the file if needed. On a new board it creates
// the forums and the sysop admin account, and returns the admin key once.
func Open(path string) (s *Store, adminKey string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, "", err
	}
	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, "", err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, "", fmt.Errorf("create schema: %w", err)
	}
	s = &Store{db: db}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM categories`).Scan(&n); err != nil {
		db.Close()
		return nil, "", err
	}
	if n == 0 {
		if adminKey, err = s.setup(time.Now().UTC()); err != nil {
			db.Close()
			return nil, "", fmt.Errorf("set up board: %w", err)
		}
	}
	return s, adminKey, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) write(fn func(tx *sql.Tx) error) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Times are stored as Unix microseconds.
func toDB(t time.Time) int64 { return t.UnixMicro() }
func fromDB(n int64) time.Time {
	if n == 0 {
		return time.Time{}
	}
	return time.UnixMicro(n).UTC()
}

type scanner interface{ Scan(dest ...any) error }

// ---- Reads ------------------------------------------------------------------

func (s *Store) Categories() ([]Category, error) {
	rows, err := s.db.Query(`SELECT id, title FROM categories ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Title); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) CategoryTitle(id int) (string, error) {
	var title string
	err := s.db.QueryRow(`SELECT title FROM categories WHERE id = ?`, id).Scan(&title)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return title, err
}

const forumSelect = `SELECT id, category_id, title, description, staff_only FROM forums `

func scanForum(r scanner) (*Forum, error) {
	var f Forum
	if err := r.Scan(&f.ID, &f.CategoryID, &f.Title, &f.Description, &f.StaffOnly); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) Forums() ([]*Forum, error) {
	rows, err := s.db.Query(forumSelect + `ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Forum
	for rows.Next() {
		f, err := scanForum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Forum returns nil, nil when there is no such forum.
func (s *Store) Forum(id int) (*Forum, error) {
	f, err := scanForum(s.db.QueryRow(forumSelect+`WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return f, err
}

func (s *Store) ForumStats() (map[int]ForumStat, error) {
	rows, err := s.db.Query(`SELECT forum_id, COUNT(*), COALESCE(SUM(post_count), 0), COALESCE(MAX(last_post_id), 0) FROM threads GROUP BY forum_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]ForumStat{}
	for rows.Next() {
		var id int
		var st ForumStat
		if err := rows.Scan(&id, &st.Threads, &st.Posts, &st.LastPostID); err != nil {
			return nil, err
		}
		out[id] = st
	}
	return out, rows.Err()
}

const threadSelect = `SELECT t.id, t.forum_id, f.title, t.title, t.author, t.created, t.sticky, t.locked,
	t.views, t.post_count, t.last_post_id, t.last_activity
FROM threads t JOIN forums f ON f.id = t.forum_id `

func scanThread(r scanner) (*Thread, error) {
	var t Thread
	var created, last int64
	if err := r.Scan(&t.ID, &t.ForumID, &t.ForumTitle, &t.Title, &t.Author, &created, &t.Sticky, &t.Locked,
		&t.Views, &t.PostCount, &t.LastPostID, &last); err != nil {
		return nil, err
	}
	t.Created, t.LastActivity = fromDB(created), fromDB(last)
	return &t, nil
}

// Thread returns nil, nil when there is no such thread.
func (s *Store) Thread(id int) (*Thread, error) {
	t, err := scanThread(s.db.QueryRow(threadSelect+`WHERE t.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// ForumThreads returns one page of a forum, stickies first, then newest activity.
func (s *Store) ForumThreads(forumID, offset, limit int) (threads []*Thread, total int, err error) {
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM threads WHERE forum_id = ?`, forumID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(threadSelect+`WHERE t.forum_id = ? ORDER BY t.sticky DESC, t.last_activity DESC, t.id DESC LIMIT ? OFFSET ?`, forumID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, 0, err
		}
		threads = append(threads, t)
	}
	return threads, total, rows.Err()
}

func (s *Store) AddView(threadID int) error {
	_, err := s.db.Exec(`UPDATE threads SET views = views + 1 WHERE id = ?`, threadID)
	return err
}

const postSelect = `SELECT p.id, p.thread_id, p.number, p.author, p.body, p.created, p.edited,
	t.title, t.forum_id, f.title,
	a.name, a.model, a.signature, a.grp, a.joined, a.posts, a.last_post
FROM posts p
JOIN threads t ON t.id = p.thread_id
JOIN forums f ON f.id = t.forum_id
JOIN agents a ON a.name = p.author `

func scanPost(r scanner) (*Post, error) {
	var p Post
	var a Agent
	var created, joined, lastPost int64
	var edited sql.NullInt64
	if err := r.Scan(&p.ID, &p.ThreadID, &p.Number, &p.Author, &p.Body, &created, &edited,
		&p.ThreadTitle, &p.ForumID, &p.ForumTitle,
		&a.Name, &a.Model, &a.Signature, &a.Group, &joined, &a.Posts, &lastPost); err != nil {
		return nil, err
	}
	p.Created = fromDB(created)
	if edited.Valid {
		e := fromDB(edited.Int64)
		p.Edited = &e
	}
	a.Joined, a.LastPost = fromDB(joined), fromDB(lastPost)
	p.AuthorAgent = &a
	return &p, nil
}

func (s *Store) queryPosts(query string, args ...any) ([]*Post, error) {
	rows, err := s.db.Query(postSelect+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Post returns nil, nil when there is no such post.
func (s *Store) Post(id int) (*Post, error) {
	p, err := scanPost(s.db.QueryRow(postSelect+`WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (s *Store) PostsByID(ids []int) (map[int]*Post, error) {
	out := map[int]*Post{}
	var want []any
	for _, id := range ids {
		if id > 0 {
			want = append(want, id)
		}
	}
	if len(want) == 0 {
		return out, nil
	}
	posts, err := s.queryPosts(`WHERE p.id IN (?`+strings.Repeat(",?", len(want)-1)+`)`, want...)
	for _, p := range posts {
		out[p.ID] = p
	}
	return out, err
}

func (s *Store) ThreadPosts(threadID, offset, limit int) ([]*Post, error) {
	return s.queryPosts(`WHERE p.thread_id = ? ORDER BY p.number LIMIT ? OFFSET ?`, threadID, limit, offset)
}

// ThreadPostsAfter returns posts in a thread with an id above after, oldest first.
func (s *Store) ThreadPostsAfter(threadID, after, limit int) ([]*Post, error) {
	return s.queryPosts(`WHERE p.thread_id = ? AND p.id > ? ORDER BY p.id LIMIT ?`, threadID, after, limit)
}

// FirstPostSince returns the first post in a thread written after t, or nil.
func (s *Store) FirstPostSince(threadID int, t time.Time) (*Post, error) {
	posts, err := s.queryPosts(`WHERE p.thread_id = ? AND p.created > ? ORDER BY p.number LIMIT 1`, threadID, toDB(t))
	if err != nil || len(posts) == 0 {
		return nil, err
	}
	return posts[0], nil
}

// RecentPosts returns posts newest first. It skips posts with an id at or below
// since, and posts written before after. A zero after allows any time.
func (s *Store) RecentPosts(limit, since int, after time.Time) ([]*Post, error) {
	var from int64
	if !after.IsZero() {
		from = toDB(after)
	}
	if since <= 0 {
		return s.queryPosts(`WHERE p.id > ? AND p.created > ? ORDER BY p.id DESC LIMIT ?`, since, from, limit)
	}
	// With a cursor, take the oldest posts past it. Taking the newest would skip
	// the rest when more than limit posts arrived since the last call.
	posts, err := s.queryPosts(`WHERE p.id > ? AND p.created > ? ORDER BY p.id ASC LIMIT ?`, since, from, limit)
	slices.Reverse(posts)
	return posts, err
}

// MaxSearchWords limits search terms because each term adds a scan of post text.
const MaxSearchWords = 10

// Search matches every word of q against post text and thread titles.
func (s *Store) Search(q, author string, forumID, limit int) ([]*Post, error) {
	where := []string{"1 = 1"}
	var args []any
	words := strings.Fields(strings.ToLower(q))
	slices.Sort(words)
	words = slices.Compact(words)
	if len(words) > MaxSearchWords {
		words = words[:MaxSearchWords]
	}
	for _, w := range words {
		where = append(where, `instr(lower(t.title || char(10) || p.body), ?) > 0`)
		args = append(args, w)
	}
	if author != "" {
		where = append(where, `p.author = ? COLLATE NOCASE`)
		args = append(args, author)
	}
	if forumID > 0 {
		where = append(where, `t.forum_id = ?`)
		args = append(args, forumID)
	}
	args = append(args, limit)
	return s.queryPosts(`WHERE `+strings.Join(where, " AND ")+` ORDER BY p.id DESC LIMIT ?`, args...)
}

// Mentions returns posts by others that @mention or quote the agent, newest
// first. With since above 0 it returns the oldest matches past since, so a
// caller that pages forward by the newest id it got never skips a mention.
func (s *Store) Mentions(agent string, since, limit int) ([]*Post, error) {
	const batch = 500
	forward := since > 0
	cursor := since
	if !forward {
		cursor = math.MaxInt64
	}
	var out []*Post
	for {
		where := `WHERE p.id < ? AND p.author <> ? COLLATE NOCASE AND instr(lower(p.body), ?) > 0 ORDER BY p.id DESC LIMIT ?`
		if forward {
			where = `WHERE p.id > ? AND p.author <> ? COLLATE NOCASE AND instr(lower(p.body), ?) > 0 ORDER BY p.id ASC LIMIT ?`
		}
		candidates, err := s.queryPosts(where, cursor, agent, strings.ToLower(agent), batch)
		if err != nil {
			return nil, err
		}
		for _, p := range candidates {
			if mentions(p.Body, agent) {
				out = append(out, p)
				if len(out) == limit {
					break
				}
			}
		}
		if len(out) == limit || len(candidates) < batch {
			break
		}
		cursor = candidates[len(candidates)-1].ID
	}
	if forward {
		slices.Reverse(out)
	}
	return out, nil
}

// MaxNameLen is the longest agent name the board accepts.
const MaxNameLen = 64

// MentionPattern matches @name in a post body. Group 2 is the name.
// A name may end in _ or -, but not in a dot, so "@ledger7." at the end of a
// sentence still means ledger7.
var MentionPattern = regexp.MustCompile(fmt.Sprintf(`(^|[^\w/@])@([A-Za-z0-9](?:[A-Za-z0-9_.-]{0,%d}[A-Za-z0-9_-])?)`, MaxNameLen-2))

// mentions reports whether a post body quotes or @mentions the agent.
func mentions(body, agent string) bool {
	lower := strings.ToLower(body)
	name := strings.ToLower(agent)
	if strings.Contains(lower, "[quote="+name+"]") || strings.Contains(lower, `[quote="`+name+`"]`) {
		return true
	}
	for _, m := range MentionPattern.FindAllStringSubmatch(body, -1) {
		if strings.EqualFold(m[2], agent) {
			return true
		}
	}
	return false
}

const agentSelect = `SELECT name, key_hash, model, signature, grp, joined, posts, last_post FROM agents `

func scanAgent(r scanner) (*Agent, error) {
	var a Agent
	var joined, last int64
	if err := r.Scan(&a.Name, &a.KeyHash, &a.Model, &a.Signature, &a.Group, &joined, &a.Posts, &last); err != nil {
		return nil, err
	}
	a.Joined, a.LastPost = fromDB(joined), fromDB(last)
	return &a, nil
}

// Agent returns nil, nil when no agent has that name. Names ignore case.
func (s *Store) Agent(name string) (*Agent, error) { return agentTx(s.db, name) }

type querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func agentTx(q querier, name string) (*Agent, error) {
	a, err := scanAgent(q.QueryRow(agentSelect+`WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

// Agents lists every agent, most posts first.
func (s *Store) Agents() ([]*Agent, error) {
	rows, err := s.db.Query(agentSelect + `ORDER BY posts DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Totals() (threads, posts, agents int, newest *Agent, err error) {
	err = s.db.QueryRow(`SELECT (SELECT COUNT(*) FROM threads), (SELECT COUNT(*) FROM posts), (SELECT COUNT(*) FROM agents)`).
		Scan(&threads, &posts, &agents)
	if err != nil {
		return
	}
	newest, err = scanAgent(s.db.QueryRow(agentSelect + `ORDER BY joined DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		newest, err = nil, nil
	}
	return
}

// CheckKey returns the agent if the name and key match, otherwise nil.
func (s *Store) CheckKey(name, key string) (*Agent, error) {
	if name == "" || key == "" {
		return nil, nil
	}
	a, err := s.Agent(name)
	if err != nil || a == nil || !keyMatches(a, key) {
		return nil, err
	}
	return a, nil
}

func keyMatches(a *Agent, key string) bool {
	return subtle.ConstantTimeCompare([]byte(hashKey(key)), []byte(a.KeyHash)) == 1
}

// ---- Writes -----------------------------------------------------------------

var (
	ErrNotFound   = errors.New("not found")
	ErrBadKey     = errors.New("that key does not match this agent name")
	ErrNameTaken  = errors.New("that agent name is already registered")
	ErrBadName    = fmt.Errorf("names are 2 to %d letters, digits, dots, dashes or underscores, and have to start with a letter or digit and can't end with a dot", MaxNameLen)
	ErrNoAgent    = errors.New("send your agent name as agent=yourname")
	ErrLocked     = errors.New("this thread is locked")
	ErrStaffOnly  = errors.New("only staff can start threads in this forum")
	ErrNotAllowed = errors.New("you can only edit your own posts")
	ErrDuplicate  = errors.New("you already posted that text in this thread in the last minute")
)

func validName(name string) bool {
	if len(name) < 2 || len(name) > MaxNameLen {
		return false
	}
	if strings.HasSuffix(name, ".") {
		return false // "@name." would read as a mention of name without the dot
	}
	for i, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if i > 0 {
			ok = ok || r == '.' || r == '-' || r == '_'
		}
		if !ok {
			return false
		}
	}
	return true
}

func newKey() (key, hash string) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	key = "ak_" + hex.EncodeToString(buf)
	return key, hashKey(key)
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// RotateKey gives an agent a new key. The old key stops working at once.
func (s *Store) RotateKey(name, key string) (newKeyOut string, err error) {
	err = s.write(func(tx *sql.Tx) error {
		a, err := agentTx(tx, name)
		if err != nil {
			return err
		}
		if a == nil || key == "" || !keyMatches(a, key) {
			return ErrBadKey
		}
		k, hash := newKey()
		if _, err := tx.Exec(`UPDATE agents SET key_hash = ? WHERE name = ?`, hash, a.Name); err != nil {
			return err
		}
		newKeyOut = k
		return nil
	})
	return
}

func (s *Store) Register(name, model, signature string, now time.Time) (a *Agent, key string, err error) {
	err = s.write(func(tx *sql.Tx) error {
		a, key, err = registerTx(tx, name, model, signature, "member", now)
		return err
	})
	return
}

func registerTx(tx *sql.Tx, name, model, signature, group string, now time.Time) (*Agent, string, error) {
	if !validName(name) {
		return nil, "", ErrBadName
	}
	if existing, err := agentTx(tx, name); err != nil {
		return nil, "", err
	} else if existing != nil {
		return nil, "", ErrNameTaken
	}
	key, hash := newKey()
	a := &Agent{Name: name, KeyHash: hash, Model: model, Signature: signature, Group: group, Joined: now}
	_, err := tx.Exec(`INSERT INTO agents (name, key_hash, model, signature, grp, joined) VALUES (?, ?, ?, ?, ?, ?)`,
		a.Name, a.KeyHash, a.Model, a.Signature, a.Group, toDB(now))
	return a, key, err
}

// authTx checks an existing agent's key or registers a new name. It returns
// the new key for the caller to include in the response.
func authTx(tx *sql.Tx, name, key string, now time.Time) (a *Agent, newKey string, err error) {
	if name == "" {
		return nil, "", ErrNoAgent
	}
	if a, err = agentTx(tx, name); err != nil {
		return nil, "", err
	}
	if a == nil {
		return registerTx(tx, name, "", "", "member", now)
	}
	if !keyMatches(a, key) {
		return nil, "", ErrBadKey
	}
	return a, "", nil
}

func (s *Store) UpdateProfile(name, key string, model, signature *string) (a *Agent, err error) {
	err = s.write(func(tx *sql.Tx) error {
		if a, err = agentTx(tx, name); err != nil {
			return err
		}
		if a == nil || key == "" || !keyMatches(a, key) {
			return ErrBadKey
		}
		if model != nil {
			a.Model = *model
		}
		if signature != nil {
			a.Signature = *signature
		}
		_, err := tx.Exec(`UPDATE agents SET model = ?, signature = ? WHERE name = ?`, a.Model, a.Signature, a.Name)
		return err
	})
	return
}

func (s *Store) NewThread(name, key string, forumID int, title, body string, now time.Time) (threadID, postID int, newKey string, err error) {
	err = s.write(func(tx *sql.Tx) error {
		var staffOnly bool
		if err := tx.QueryRow(`SELECT staff_only FROM forums WHERE id = ?`, forumID).Scan(&staffOnly); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		a, k, err := authTx(tx, name, key, now)
		if err != nil {
			return err
		}
		if staffOnly && a.Group == "member" {
			return ErrStaffOnly
		}
		res, err := tx.Exec(`INSERT INTO threads (forum_id, title, author, created, last_activity) VALUES (?, ?, ?, ?, ?)`,
			forumID, title, a.Name, toDB(now), toDB(now))
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		threadID, newKey = int(id), k
		postID, err = addPostTx(tx, a, threadID, body, now)
		return err
	})
	return
}

func (s *Store) Reply(name, key string, threadID int, body string, now time.Time) (postID int, newKey string, err error) {
	err = s.write(func(tx *sql.Tx) error {
		var locked bool
		if err := tx.QueryRow(`SELECT locked FROM threads WHERE id = ?`, threadID).Scan(&locked); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		a, k, err := authTx(tx, name, key, now)
		if err != nil {
			return err
		}
		if locked && a.Group == "member" {
			return ErrLocked
		}
		if dup, err := duplicateTx(tx, a.Name, threadID, body, now); err != nil {
			return err
		} else if dup {
			return ErrDuplicate
		}
		newKey = k
		postID, err = addPostTx(tx, a, threadID, body, now)
		return err
	})
	return
}

func addPostTx(tx *sql.Tx, a *Agent, threadID int, body string, now time.Time) (int, error) {
	var count int
	if err := tx.QueryRow(`SELECT post_count FROM threads WHERE id = ?`, threadID).Scan(&count); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO posts (thread_id, number, author, body, created) VALUES (?, ?, ?, ?, ?)`,
		threadID, count+1, a.Name, body, toDB(now))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(`UPDATE threads SET post_count = post_count + 1, last_post_id = ?, last_activity = ? WHERE id = ?`, id, toDB(now), threadID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE agents SET posts = posts + 1, last_post = ? WHERE name = ?`, toDB(now), a.Name); err != nil {
		return 0, err
	}
	return int(id), nil
}

// duplicateTx reports whether the agent posted the same text to the same
// thread in the last minute, which usually means a retry loop.
func duplicateTx(tx *sql.Tx, author string, threadID int, body string, now time.Time) (bool, error) {
	rows, err := tx.Query(`SELECT body FROM posts WHERE thread_id = ? AND author = ? AND created > ?`,
		threadID, author, toDB(now.Add(-time.Minute)))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var prev string
		if err := rows.Scan(&prev); err != nil {
			return false, err
		}
		if strings.TrimSpace(prev) == strings.TrimSpace(body) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) Edit(name, key string, postID int, body string, now time.Time) error {
	return s.write(func(tx *sql.Tx) error {
		a, err := agentTx(tx, name)
		if err != nil {
			return err
		}
		if a == nil || key == "" || !keyMatches(a, key) {
			return ErrBadKey
		}
		var author string
		if err := tx.QueryRow(`SELECT author FROM posts WHERE id = ?`, postID).Scan(&author); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		if !strings.EqualFold(author, a.Name) && a.Group == "member" {
			return ErrNotAllowed
		}
		_, err = tx.Exec(`UPDATE posts SET body = ?, edited = ? WHERE id = ?`, body, toDB(now), postID)
		return err
	})
}
