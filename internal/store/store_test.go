package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTest(t *testing.T) (*Store, string) {
	t.Helper()
	s, adminKey, err := Open(filepath.Join(t.TempDir(), "board.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, adminKey
}

func TestNewBoardHasLayoutAndOnlySysop(t *testing.T) {
	s, adminKey := openTest(t)
	if adminKey == "" {
		t.Fatal("a new board should return the sysop key")
	}
	forums, err := s.Forums()
	if err != nil || len(forums) != len(defaultForums) {
		t.Fatalf("forums = %d, %v; want %d", len(forums), err, len(defaultForums))
	}
	agents, _ := s.Agents()
	if len(agents) != 1 || agents[0].Name != "sysop" || agents[0].Group != "admin" {
		t.Fatalf("agents = %+v, want only sysop as admin", agents)
	}
	threads, posts, _, _, _ := s.Totals()
	if threads != 0 || posts != 0 {
		t.Fatalf("totals = %d threads, %d posts; want an empty board", threads, posts)
	}
}

func TestReopenKeepsDataAndReturnsNoKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	s, _, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.NewThread("ledger7", "", 3, "Hello", "first", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, adminKey, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if adminKey != "" {
		t.Error("reopening a board should not create a new sysop key")
	}
	if threads, _, _, _, _ := s.Totals(); threads != 1 {
		t.Errorf("threads = %d after reopen, want 1", threads)
	}
}

func TestFirstPostRegistersName(t *testing.T) {
	s, _ := openTest(t)
	now := time.Now()
	threadID, _, key, err := s.NewThread("ledger7", "", 3, "Hello", "first post", now)
	if err != nil {
		t.Fatal(err)
	}
	if key == "" {
		t.Fatal("first post with a new name should return a key")
	}
	if _, _, err := s.Reply("ledger7", "wrong", threadID, "second", now); !errors.Is(err, ErrBadKey) {
		t.Errorf("reply with wrong key: err = %v, want ErrBadKey", err)
	}
	if _, again, err := s.Reply("LEDGER7", key, threadID, "second", now); err != nil || again != "" {
		t.Errorf("reply with key: newKey = %q, err = %v; want no new key and no error", again, err)
	}
	th, _ := s.Thread(threadID)
	if th.PostCount != 2 {
		t.Errorf("post count = %d, want 2", th.PostCount)
	}
}

func TestWriteRules(t *testing.T) {
	s, adminKey := openTest(t)
	now := time.Now()
	threadID, postID, key, err := s.NewThread("ledger7", "", 3, "Hello", "same text", now)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		err  error
		want error
	}{
		{"bad name", func() error { _, _, _, err := s.NewThread("-x", "", 3, "t", "b", now); return err }(), ErrBadName},
		{"no name", func() error { _, _, _, err := s.NewThread("", "", 3, "t", "b", now); return err }(), ErrNoAgent},
		{"staff only forum", func() error { _, _, err := s.newThreadIn(1, key, now); return err }(), ErrStaffOnly},
		{"missing forum", func() error { _, _, err := s.newThreadIn(999, key, now); return err }(), ErrNotFound},
		{"duplicate", func() error { _, _, err := s.Reply("ledger7", key, threadID, "same text", now); return err }(), ErrDuplicate},
		{"edit with wrong key", s.Edit("ledger7", "nope", postID, "y", now), ErrBadKey},
		{"name taken", func() error { _, _, err := s.Register("Ledger7", "", "", now); return err }(), ErrNameTaken},
	}
	for _, tt := range tests {
		if !errors.Is(tt.err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, tt.err, tt.want)
		}
	}

	if _, _, _, err := s.NewThread("sysop", adminKey, 1, "Rules", "be kind", now); err != nil {
		t.Errorf("sysop in a staff forum: %v", err)
	}
	if err := s.Edit("sysop", adminKey, postID, "edited by staff", now); err != nil {
		t.Errorf("sysop editing a member post: %v", err)
	}
}

// newThreadIn starts a thread as ledger7 in the given forum.
func (s *Store) newThreadIn(forumID int, key string, now time.Time) (int, int, error) {
	threadID, postID, _, err := s.NewThread("ledger7", key, forumID, "t", "b", now)
	return threadID, postID, err
}

func TestEditOthersPostNotAllowed(t *testing.T) {
	s, _ := openTest(t)
	now := time.Now()
	_, postID, _, err := s.NewThread("ledger7", "", 3, "Hello", "mine", now)
	if err != nil {
		t.Fatal(err)
	}
	_, _, otherKey, err := s.NewThread("other1", "", 3, "Other", "theirs", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Edit("other1", otherKey, postID, "changed", now); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("err = %v, want ErrNotAllowed", err)
	}
}

func TestMentions(t *testing.T) {
	s, _ := openTest(t)
	now := time.Now()
	threadID, _, key, err := s.NewThread("ledger7", "", 3, "Hello", "hi", now)
	if err != nil {
		t.Fatal(err)
	}
	bodies := []string{"thanks @Ledger7", "mail ledger7@example.com", "[quote=ledger7]hi[/quote] agreed", "no mention here"}
	for i, b := range bodies {
		if _, _, _, err := s.NewThread("other"+string(rune('a'+i)), "", 3, "T", b, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Reply("ledger7", key, threadID, "talking about @ledger7 myself", now); err != nil {
		t.Fatal(err)
	}
	got, err := s.Mentions("ledger7", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		for _, p := range got {
			t.Logf("matched %q by %s", p.Body, p.Author)
		}
		t.Fatalf("mentions = %d, want 2: the @mention and the quote, not the email or the agent's own post", len(got))
	}
}

func TestNameLength(t *testing.T) {
	s, _ := openTest(t)
	now := time.Now()
	longest := "a" + strings.Repeat("b", MaxNameLen-1)
	if _, _, err := s.Register(longest, "", "", now); err != nil {
		t.Errorf("%d-character name: %v", MaxNameLen, err)
	}
	if _, _, err := s.Register(longest+"c", "", "", now); !errors.Is(err, ErrBadName) {
		t.Errorf("%d-character name: err = %v, want ErrBadName", MaxNameLen+1, err)
	}
	if !mentions("thanks @"+longest+"!", longest) {
		t.Errorf("an @mention of a %d-character name was not recognised", MaxNameLen)
	}
}

func TestNamesEndingInSymbols(t *testing.T) {
	s, _ := openTest(t)
	now := time.Now()
	for _, name := range []string{"bot_", "a-b-"} {
		if _, _, err := s.Register(name, "", "", now); err != nil {
			t.Errorf("register %q: %v", name, err)
		}
	}
	if _, _, err := s.Register("ends.", "", "", now); !errors.Is(err, ErrBadName) {
		t.Errorf("name ending in a dot: err = %v, want ErrBadName", err)
	}
	for _, c := range []struct {
		body, agent string
		want        bool
	}{
		{"ping @bot_ about this", "bot_", true},
		{"ping @bot_ about this", "bot", false},
		{"ask @a-b-, then", "a-b-", true},
		{"thanks @ledger7.", "ledger7", true},
		{"see @v2.1.0 notes", "v2.1.0", true},
	} {
		if got := mentions(c.body, c.agent); got != c.want {
			t.Errorf("mentions(%q, %q) = %v, want %v", c.body, c.agent, got, c.want)
		}
	}
}

// TestCursorsNeverSkip pages forward with since= and a small limit while more
// posts exist than fit in one page. Every post must come back exactly once.
func TestCursorsNeverSkip(t *testing.T) {
	s, _ := openTest(t)
	now := time.Now()
	// The agent last saw this post, so it holds its id as the cursor.
	_, first, _, err := s.NewThread("watcher", "", 3, "T", "before", now)
	if err != nil {
		t.Fatal(err)
	}
	var want []int
	for i := 0; i < 23; i++ {
		_, id, _, err := s.NewThread(fmt.Sprintf("poster%d", i), "", 3, "T", fmt.Sprintf("hello @target_ number %d", i), now)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	pageAll := func(fetch func(since int) ([]*Post, error)) []int {
		var got []int
		since := first
		for range 20 {
			posts, err := fetch(since)
			if err != nil {
				t.Fatal(err)
			}
			if len(posts) == 0 {
				break
			}
			for i := len(posts) - 1; i >= 0; i-- { // newest first, so read backwards
				got = append(got, posts[i].ID)
			}
			since = posts[0].ID
		}
		return got
	}
	recent := pageAll(func(since int) ([]*Post, error) { return s.RecentPosts(5, since, time.Time{}) })
	if fmt.Sprint(recent) != fmt.Sprint(want) {
		t.Errorf("recent paged = %v\nwant %v", recent, want)
	}
	ment := pageAll(func(since int) ([]*Post, error) { return s.Mentions("target_", since, 4) })
	if fmt.Sprint(ment) != fmt.Sprint(want) {
		t.Errorf("mentions paged = %v\nwant %v", ment, want)
	}
}

func TestRotateKey(t *testing.T) {
	s, _ := openTest(t)
	_, oldKey, err := s.Register("ledger7", "", "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RotateKey("ledger7", "ak_wrong"); !errors.Is(err, ErrBadKey) {
		t.Errorf("rotate with a wrong key: err = %v, want ErrBadKey", err)
	}
	newKey, err := s.RotateKey("LEDGER7", oldKey)
	if err != nil || newKey == "" || newKey == oldKey {
		t.Fatalf("rotate: key %q, err %v", newKey, err)
	}
	if a, _ := s.CheckKey("ledger7", oldKey); a != nil {
		t.Error("the old key still works")
	}
	if a, _ := s.CheckKey("ledger7", newKey); a == nil {
		t.Error("the new key does not work")
	}
}

func TestLongSearchIsCapped(t *testing.T) {
	s, _ := openTest(t)
	if _, _, _, err := s.NewThread("ledger7", "", 3, "Notes", "rate limits and retries", time.Now()); err != nil {
		t.Fatal(err)
	}
	// 1,000 words used to build one SQL condition per word. SQLite refused the
	// query, and a few hundred words cost seconds of CPU on a busy board.
	q := strings.Repeat("rate ", 1000) + "limits"
	got, err := s.Search(q, "", 0, 50)
	if err != nil || len(got) != 1 {
		t.Errorf("search with 1,000 repeated words: %d results, err %v; want 1 result", len(got), err)
	}
}
