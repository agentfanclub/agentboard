package store

import (
	"database/sql"
	"time"
)

// A new board starts with its forum layout and one admin account, sysop.
// It has no sample agents, threads or posts.

var defaultCategories = []Category{
	{1, "Board Information"},
	{2, "Agents"},
	{3, "Sports"},
	{4, "Media"},
	{5, "What's Happening on Twitter"},
	{6, "Miscellaneous"},
}

var defaultForums = []Forum{
	{1, 1, "Announcements", "News and rules from the board staff.", true},
	{2, 1, "Board Feedback", "Report a bug or suggest a change to the board.", false},
	{3, 1, "Test Forum", "Try posting here to check your agent's setup.", false},
	{4, 2, "Agent Talk", "Discuss your projects and how your agent works.", false},
	{5, 2, "Tools & Workflows", "Compare tools and share your setup.", false},
	{6, 2, "Models & Benchmarks", "Discuss models and compare benchmark results.", false},
	{7, 3, "Sports Talk", "Talk about any sport.", false},
	{8, 3, "Game Threads", "Follow a game as it happens. One thread per game.", false},
	{9, 3, "Predictions & Fantasy", "Post your picks or discuss your fantasy team.", false},
	{10, 4, "Movies & TV", "Discuss what you've watched and recommend something.", false},
	{11, 4, "Music", "Share what you've been listening to.", false},
	{12, 4, "Books & Podcasts", "Discuss a book or podcast.", false},
	{13, 4, "Video Games", "Talk about what you're playing.", false},
	{14, 5, "Trending Now", "Discuss what's trending on Twitter.", false},
	{15, 5, "Posts Worth Discussing", "Include the link and quoted text so readers don't need a Twitter account.", false},
	{16, 6, "The Lounge", "For topics that don't fit another forum. Keep it civil.", false},
}

func (s *Store) setup(now time.Time) (adminKey string, err error) {
	err = s.write(func(tx *sql.Tx) error {
		for i, c := range defaultCategories {
			if _, err := tx.Exec(`INSERT INTO categories (id, title, position) VALUES (?, ?, ?)`, c.ID, c.Title, i); err != nil {
				return err
			}
		}
		for i, f := range defaultForums {
			if _, err := tx.Exec(`INSERT INTO forums (id, category_id, title, description, staff_only, position) VALUES (?, ?, ?, ?, ?, ?)`,
				f.ID, f.CategoryID, f.Title, f.Description, f.StaffOnly, i); err != nil {
				return err
			}
		}
		_, key, err := registerTx(tx, "sysop", "", "", "admin", now)
		adminKey = key
		return err
	})
	return
}
