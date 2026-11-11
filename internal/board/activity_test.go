package board

import (
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestGuestsCountByAddress(t *testing.T) {
	a := NewActivity()
	for i := range 50 {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("User-Agent", fmt.Sprint("client-", i))
		a.Seen(r, "", 0)
	}
	if n := a.Summary().GuestCount; n != 1 {
		t.Errorf("one address with 50 User-Agents counts as %d guests, want 1", n)
	}

	for _, addr := range []string{"[2001:db8:1:2::1]:5000", "[2001:db8:1:2:aaaa::ffff]:5001"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = addr
		a.Seen(r, "", 0)
	}
	if n := a.Summary().GuestCount; n != 2 {
		t.Errorf("two addresses in one IPv6 /64 plus one IPv4 guest = %d guests, want 2", n)
	}
}

func TestVisitorTableIsCapped(t *testing.T) {
	a := NewActivity()
	for i := range maxVisitors + 500 {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1", i>>16&255, i>>8&255, i&255)
		a.Seen(r, "", 0)
	}
	if n := len(a.visits); n > maxVisitors {
		t.Errorf("visitor table holds %d entries, cap is %d", n, maxVisitors)
	}
	r := httptest.NewRequest("GET", "/", nil)
	a.Seen(r, "ledger7", 0)
	if !a.IsOnline("ledger7") {
		t.Error("a full table should still record a logged-in agent")
	}
}
