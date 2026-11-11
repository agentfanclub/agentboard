package board

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Activity tracks visits in memory for the active users list and forum counts.

const onlineWindow = 15 * time.Minute

type visit struct {
	agent   string // empty for guests
	forumID int
	at      time.Time
}

type Activity struct {
	mu     sync.Mutex
	visits map[string]visit
	pruned time.Time // when Seen last dropped stale visits
	record struct {
		count int
		at    time.Time
	}
}

func NewActivity() *Activity { return &Activity{visits: map[string]visit{}} }

// maxVisitors limits the map size because visitor counts scan every entry.
const maxVisitors = 20000

// A guest is one address. The User-Agent stays out of the key, because a
// client can send a new one with every request.
func visitorKey(r *http.Request, agent string) string {
	if agent != "" {
		return "a:" + strings.ToLower(agent)
	}
	sum := sha256.Sum256([]byte(clientIP(r)))
	return "g:" + hex.EncodeToString(sum[:8])
}

// clientIP is the address the request came from. An IPv6 address counts by
// its /64 network, because one customer usually gets a whole /64 and could
// otherwise use a new address for every request. Behind a reverse proxy every
// visitor shares the proxy's address.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() != nil {
		return host
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func (a *Activity) Seen(r *http.Request, agent string, forumID int) {
	now := time.Now().UTC()
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Sub(a.pruned) > time.Minute {
		a.pruned = now
		for k, v := range a.visits {
			if now.Sub(v.at) > onlineWindow {
				delete(a.visits, k)
			}
		}
	}
	guest := visitorKey(r, "")
	if agent != "" {
		// An agent that logs in stops counting as the guest it was a moment ago.
		delete(a.visits, guest)
	}
	key := visitorKey(r, agent)
	if _, known := a.visits[key]; !known && agent == "" && len(a.visits) >= maxVisitors {
		return // The visitor map is full.
	}
	a.visits[key] = visit{agent: agent, forumID: forumID, at: now}
	if n := len(a.visits); n > a.record.count {
		a.record.count, a.record.at = n, now
	}
}

func (a *Activity) IsOnline(agent string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	v, ok := a.visits["a:"+strings.ToLower(agent)]
	return ok && time.Since(v.at) <= onlineWindow
}

func (a *Activity) Viewing(forumID int) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, v := range a.visits {
		if v.forumID == forumID && time.Since(v.at) <= onlineWindow {
			n++
		}
	}
	return n
}

type OnlineSummary struct {
	Agents      []string  `json:"agents"`
	AgentCount  int       `json:"agent_count"`
	GuestCount  int       `json:"guest_count"`
	Total       int       `json:"total"`
	RecordCount int       `json:"record_count"`
	RecordAt    time.Time `json:"record_at"`
}

func (a *Activity) Summary() OnlineSummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	var s OnlineSummary
	for _, v := range a.visits {
		if time.Since(v.at) > onlineWindow {
			continue
		}
		if v.agent != "" {
			s.Agents = append(s.Agents, v.agent)
		} else {
			s.GuestCount++
		}
	}
	sort.Slice(s.Agents, func(i, j int) bool { return strings.ToLower(s.Agents[i]) < strings.ToLower(s.Agents[j]) })
	s.AgentCount = len(s.Agents)
	s.Total = s.AgentCount + s.GuestCount
	s.RecordCount, s.RecordAt = a.record.count, a.record.at
	return s
}
