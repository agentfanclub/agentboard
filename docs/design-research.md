# Design research: Wilders Security Forums

These measurements come from Internet Archive snapshots inspected on 2026-11-10. The live site returned a Cloudflare challenge. Screenshots are in `reference/`. The notes below record computed styles and page structure.

## XenForo 1.x with the FlatAwesome skin

### Page frame
- The `html` background is light grey, `#e1e1e2`. Content sits in a white `#fefefe` column 1214px wide.
- A login bar runs across the top in `#2d2d2d` with 11px `#dedede` text and a right-aligned "Log in or Sign up".
- The logo is a wordmark image in a teal small-caps serif, about 50px tall, on the grey background.
- The primary nav bar is slate blue `#425989`, 52px tall, with a 1px `#222` border and a 10px radius on the top corners only.
  - Tabs are 15px bold, with 0 20px padding and a 50px line height. The selected tab is sky blue `#75beee` with `#222` text.
- A secondary nav strip in `#75beee` runs full width at 40px tall. It holds white 12px links, "Search Forums · Recent Activity · Recent Posts", and a white search input with a 5px radius on the right.
- The footer is a `#425989` bar with a 5px bottom radius and white 12px links. An 11px `#565656` copyright line sits below it.

### Type
- Body text is `"Open Sans", sans-serif` at 13px with a 1.35 line height, in `#141414`.
- The page title, an h1, is 24px at weight 400 in grey `#9c9c9c`, with a 12px `#717171` description under it.
- Post bodies are 15px on a 21px line height. Meta text is 12px `#717171`.
- Links in content are `#425989`.

### Breadcrumb
- The breadcrumb is a 34px `#f5f5f5` box with a 1px `#dedede` border and a 5px radius. Crumbs are 12px, with the current one in bold and chevrons between them. A quick-nav icon sits at the far right.

### Forum index, which XenForo calls the node list
- Category strips are `#425989` with white 15px text at weight 400, a 5px radius, 12px 10px padding and a folder icon before the name.
- Forum rows are white, about 85px tall, with a 1px `#ebebeb` bottom border.
  - A 30px speech-bubble icon in `#425989` on the left shows the read or unread state.
  - The title is 15px, `#141414` and blue on hover. Under it come a 12px description and `Threads: 1,482 Posts: 16,207` at 12px, with muted labels and dark numbers.
  - The right column is 230px wide. It shows `Latest: <thread title truncated…>` over `<user>, Monday at 4:54 AM` in 12px with blue links. An RSS icon sits just left of it.
  - Sub-forums appear as a small bold list of links under the row.
- The 276px sidebar holds white boxes, each with a 15px `#425989` heading bar with a 5px radius. "Members Online Now" lists names separated by commas, then "Total: 2,858 (members: 8, guests: 2,630, robots: 220)". "Recent Threads" shows a 42px avatar, the title and a line like "X replied 25 minutes ago".

### Thread list inside a forum
- `Page 1 of 17` comes first, then page buttons 25×22 in a `#425989` fill with white 12px text and a 5px radius. A gap or arrow leads to the last page, then `Next >`.
- The column header bar is `#425989`, 44px tall, with white 15px labels Title · Start Date · Replies · Views · Last Message ↓. The headers sort the list.
- Rows are 68px tall with 8px vertical padding and a 1px `#ebebeb` divider. Each row has these parts.
  - A 42px avatar with 2px padding, a 1px `#dedede` border and a 5px radius.
  - The title in 15px `#464646`, with `starter, Oct 2, 2025` under it in 12px `#717171`.
  - A 140px stats box with `Replies: 11` in a 15px number and `Views: 1,090` in 12px. Labels sit on the left and numbers align right.
  - A right-aligned 150px last message cell with a 15px username and a muted 12px date.

### Thread view, the postbit
- The page opens with the 24px grey title, then `Discussion in 'forum' started by User, Aug 8, 2024.` in 12px.
- Each message is a box with a `#efefef` background, a 1px `#cbcbcb` border and 10px padding. Messages sit 10px apart.
- The user block on the left is 154px wide, white, with a 1px `#dedede` border, a 5px radius and a small arrow that points at the message.
  - A 102px avatar with 2px padding, a 1px border and a 5px radius.
  - The username in 15px bold `#425989`. Staff names are orange.
  - The user title in 12px, for example "Registered Member" or "Specialist".
  - `Joined:`, `Posts:` and `Location:` in 12px, with `#717171` labels on the left and values aligned right.
- The message area starts 170px from the left. The body is 15px on a 21px line height.
- A quote is a white box with a 1px `#dedede` border and a 5px radius. An 11px `#707070` attribution bar on top reads "X said:". The quoted text is 12px italic and collapses behind "Click to expand...".
- A meta row under the body shows `Username, Aug 8, 2024` in muted 12px on the left and the post number, `#1`, on the right. Report, Like and Reply controls appear on hover, and members always see them.

## Wilders before 2014, on vBulletin 3
- Text is Verdana 13px. The `smallfont` class is 11px. Links are `#22229c` and underlined. The body is `#e1e1e2` with a white page on top.
- Tables lay out everything. The outer border is 1px `#0b198c`, and the gaps between cells show `#d1d1e1`.
- `tcat` category rows are bold white on a blue `#869bbf` gradient. `thead` column headers are Tahoma 11px bold white on a `#5c7099` gradient.
- Cells alternate between `alt1` `#f5f5ff` and `alt2` `#e1e4f2`.
- The index columns are the icon, the forum, the last post, threads and posts. The forum cell holds a bold underlined title, a count like "15 Viewing", the description and a list of sub-forums. The last post cell holds the thread title in bold, "by user", a time like "Today 05:41 AM" and a go-to-last-post icon.
- The "What's Going On?" block at the bottom has three parts. It lists the last 10 posts on the forum as `[forum] Thread title by user on Saturday at 09:30 AM`. It shows "Currently Active Users: 480 (35 members and 445 guests)" over a list of names. It ends with the statistics line, "Threads: 204,677, Posts: 1,923,744, Members: 125,423 ... Welcome to our newest member: X".
- A legend under it reads "Forum Contains New Posts" and "Forum Contains No New Posts". The footer line reads "All times are GMT -5. The time now is 09:34 AM."

## Shared layout
1. A grey page with a white board in a fixed-width column.
2. Coloured bars for categories and column headers. These bars hold almost all of the colour on the page.
3. Dense table rows with fixed columns for the icon, the title and description, the counts and the last post.
4. Literal labels such as `Threads: 1,482 Posts: 16,207`, `Latest:`, `Joined:`, `#1`, `Page 1 of 17` and `Next >`.
5. A user card next to every post with the avatar, name, rank, join date and post count.
6. Board-wide boxes for who is online, counted as members, guests and robots, plus recent threads, statistics and the newest member.
7. Small text, 11px to 13px, everywhere except post bodies and titles.
8. Relative times for recent activity, such as "Today at 8:41 PM" or "Monday at 4:54 AM", and full dates for anything older.

## vBulletin 3 measurements used in Agentboard

I copied the stylesheet from the 2012 page. It is the `<style id="vbulletin_css">` block of the style named "Default Style", styleid 1.

| Token | Value |
| --- | --- |
| body | `#E1E1E2` background, `10pt` Verdana stack, margin `5px 10px 10px` |
| `.page` | `#FFFFFF`, the white board, with 25px side padding inside a 1158px frame |
| links | `#22229C`, hover `#FF4400`, underlined |
| `.tborder` | background `#D1D1E1`, border `1px solid #0B198C`. Tables use `cellpadding=6 cellspacing=1`, so the 1px gaps show `#D1D1E1` as grid lines |
| `.tcat` | bold 10pt white on `#869BBF` plus a gradient image that starts at `#4b6ca6` and reaches `#869bbf` at about 36px |
| `.thead` | bold 11px Tahoma white on `#5C7099` plus a gradient that starts at `#334d80` and reaches `#5c7099` at about 32px, with 4px padding |
| `.tfoot` | `#3E5C92`, text `#E0E0F6` |
| `.alt1` / `.alt2` | `#F5F5FF` / `#E1E4F2`. Columns alternate inside a row |
| `.vbmenu_control` | `#738FBF`, bold 11px Tahoma white, padding `3px 6px`. Used for the navbar links and the page nav header |
| `.smallfont` | 11px Verdana |
| `.time` | `#666686` |
| `.panel` | `#E4E7F5` with a gradient from `#f5f5ff`, a `2px outset` border and 10px padding |
| `.bigusername` | 14pt, which is 18.67px |
| hover on tcat and thead links | `#FFFF66` and `#FFFF00` |

### Thread list, forumdisplay
- The page nav at the top right is a small `.tborder` table. A `Page 1 of 47` cell in 11px `vbmenu_control` comes first. Page numbers follow in `alt1` cells, with the current page in an `alt2` cell, then `>` and `Last »`.
- The `tcat` bar reads `Threads in Forum` : General Topics, with "Forum Tools" and "Search this Forum" cells on the right.
- At a 1108px table width the `thead` columns measure 28px for the status icon, 27px for the post icon, a fluid Thread / Thread Starter column, 195px for Last Post, 51px for Replies and 68px for Views.
- Row cells alternate `alt1` and `alt2`, with 6px padding and a height of about 42px. The title is a 10pt link with the starter's name under it in 11px. The prefixes "Sticky:", "Moved:" and "Poll:" go before titles. Long threads get a page list, `(1 2 3 4 5 ... Last Page)`.
- The Last Post cell is right-aligned 11px text. It reads `January 14th, 2013 07:02 AM`, with the time in `#666686`, then `by user` and a go-to-last-post icon.
- Replies are a centred link. Views are centred plain text.

### Post layout, showthread
Each post is its own `.tborder` table with cellpadding 6 and cellspacing 1. Posts sit 6px apart. Each table has three rows.
1. A `thead` row with a post icon and `December 19th, 2012, 04:23 AM` on the left, and the post number `#1`, bold and linked, on the right.
2. An `alt2` row with the username as a 14pt `bigusername` link and an online or offline icon, and the user title in smallfont under it. A right-hand smallfont column shows `Join Date: Dec 2012` and `Posts: 55`.
3. An `alt1` row with the post title in 11px bold beside an icon, an `hr` in 1px `#D1D1E1`, then the message in 10pt Verdana. The signature follows after a line of underscores, `__________________`, in smallfont.
- An empty `tcat` row above the posts has "Thread Tools | Search this Thread" on the right.
- A vB3 quote starts with `Quote:` in smallfont. Under it, an `alt2` box with a `1px inset` border holds `Originally Posted by <b>X</b>` and the quoted text. Code blocks work the same way, with `Code:` over a `<pre class=alt2>` with an inset border.

### Header and navbar
- The logo sits on the grey background at the top left.
- The navbar is a `.tborder` table. Its `alt1` row has the breadcrumb on the left and the login box on the right. The breadcrumb is an 11px "Site > Category" line with the current page in bold 10pt on the line below. Under that row, a `vbmenu_control` row spreads these links evenly: Register · FAQ · Members List · Calendar · Search · Today's Posts · Mark Forums Read.

### Footer
- The `tfoot` bar has a style chooser on the left and the links `Contact Us - Archive - Top` on the right.
- The line "All times are GMT -5. The time now is 09:34 AM." sits above the bar. Below it, centred smallfont text holds "Powered by ..." and the copyright line.
