package main

import (
	"crypto/rand"
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	maxSessions = 2000
	maxEvents   = 10000
	sessionTTL  = 24 * time.Hour
)

type Selection struct {
	ProductID string `json:"product_id"`
	Color     string `json:"color"`
}

type CartItem struct {
	ProductID string `json:"product_id"`
	Color     string `json:"color"`
	Qty       int    `json:"qty"`
}

type Order struct {
	ID        string    `json:"id"`
	Total     float64   `json:"total"`
	Currency  string    `json:"currency"`
	ItemCount int       `json:"item_count"`
	PlacedAt  time.Time `json:"placed_at"`
	Simulated bool      `json:"simulated"`
}

type Session struct {
	ID          string      `json:"id"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	AdID        string      `json:"ad_id"`
	Selected    []Selection `json:"selected"`
	Cart        []CartItem  `json:"cart"`
	Order       *Order      `json:"order,omitempty"`
	PhoneOpened bool        `json:"phone_opened"`
	CartID      string      `json:"ucp_cart_id,omitempty"`     // the UCP cart behind Cart
	CheckoutID  string      `json:"ucp_checkout_id,omitempty"` // set once the order is placed
}

type Event struct {
	ID      int               `json:"id"`
	T       time.Time         `json:"t"`
	Session string            `json:"session"`
	Name    string            `json:"name"`
	Props   map[string]string `json:"props,omitempty"`
}

// Funnel stages in display order. alternative_selected and chat_message are
// reported separately because they are side branches, not steps.
var funnelStages = []string{"ad_view", "panel_opened", "product_opened", "add_to_cart", "demo_purchase_completed"}

// branchEvents are side paths, not steps: a shopper may buy on the TV and never touch the phone.
var branchEvents = []string{"sent_to_phone", "qr_handoff_opened", "alternative_selected", "chat_message"}

var knownEvents = map[string]bool{
	"ad_view": true, "panel_opened": true, "product_opened": true, "alternative_selected": true,
	"chat_message": true, "sent_to_phone": true, "qr_handoff_opened": true, "add_to_cart": true,
	"demo_purchase_completed": true,
}

type Store struct {
	mu       sync.Mutex
	sessions map[string]*Session
	events   []Event
	nextEvt  int
	now      func() time.Time
}

func NewStore() *Store {
	return &Store{sessions: map[string]*Session{}, now: time.Now}
}

var errNotFound = errors.New("not found")

const idAlphabet = "abcdefghjkmnpqrstuvwxyz23456789" // no look-alike characters

func newID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return string(b)
}

func (s *Store) gcLocked() {
	cut := s.now().Add(-sessionTTL)
	for id, ss := range s.sessions {
		if ss.UpdatedAt.Before(cut) {
			delete(s.sessions, id)
		}
	}
	if len(s.sessions) >= maxSessions {
		// Drop the oldest quarter rather than refusing new viewers.
		type kv struct {
			id string
			t  time.Time
		}
		var all []kv
		for id, ss := range s.sessions {
			all = append(all, kv{id, ss.UpdatedAt})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
		for _, e := range all[:len(all)/4] {
			delete(s.sessions, e.id)
		}
	}
}

func (s *Store) NewSession(adID string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	now := s.now()
	ss := &Session{ID: newID(8), CreatedAt: now, UpdatedAt: now, AdID: adID, Selected: []Selection{}, Cart: []CartItem{}}
	s.sessions[ss.ID] = ss
	return ss
}

func (s *Store) Get(id string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.sessions[id]
	if !ok {
		return Session{}, errNotFound
	}
	return cloneSession(ss), nil
}

func cloneSession(ss *Session) Session {
	c := *ss
	c.Selected = append([]Selection{}, ss.Selected...)
	c.Cart = append([]CartItem{}, ss.Cart...)
	if ss.Order != nil {
		o := *ss.Order
		c.Order = &o
	}
	return c
}

// Mutate applies fn to the live session under the lock and returns a copy.
func (s *Store) Mutate(id string, fn func(*Session) error) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, ok := s.sessions[id]
	if !ok {
		return Session{}, errNotFound
	}
	if err := fn(ss); err != nil {
		return Session{}, err
	}
	ss.UpdatedAt = s.now()
	return cloneSession(ss), nil
}

func (s *Store) Select(id string, sel Selection) (Session, error) {
	return s.Mutate(id, func(ss *Session) error {
		for i := range ss.Selected {
			if ss.Selected[i].ProductID == sel.ProductID {
				ss.Selected[i].Color = sel.Color
				return nil
			}
		}
		if len(ss.Selected) >= 12 {
			return errors.New("too many selected items")
		}
		ss.Selected = append(ss.Selected, sel)
		return nil
	})
}

func (s *Store) Unselect(id, productID string) (Session, error) {
	return s.Mutate(id, func(ss *Session) error {
		out := ss.Selected[:0]
		for _, it := range ss.Selected {
			if it.ProductID != productID {
				out = append(out, it)
			}
		}
		ss.Selected = out
		return nil
	})
}

// CartChange adds (delta>0) or removes (delta<0) quantity of a product variant.
// SetCart mirrors the UCP cart into the session so the TV and phone read one source.
func (s *Store) SetCart(id, cartID string, items []CartItem) (Session, error) {
	return s.Mutate(id, func(ss *Session) error {
		if ss.Order != nil {
			return errOrdered
		}
		ss.CartID = cartID
		ss.Cart = append([]CartItem(nil), items...)
		return nil
	})
}

var errOrdered = errors.New("order already placed for this session")

func (s *Store) MarkPhoneOpened(id string) (first bool, err error) {
	_, err = s.Mutate(id, func(ss *Session) error {
		first = !ss.PhoneOpened
		ss.PhoneOpened = true
		return nil
	})
	return first, err
}

// SetOrder records the order a completed UCP checkout produced.
func (s *Store) SetOrder(id, checkoutID string, o Order) (Session, error) {
	return s.Mutate(id, func(ss *Session) error {
		if ss.Order != nil {
			return errOrdered
		}
		o.PlacedAt = s.now()
		ss.Order = &o
		ss.CheckoutID = checkoutID
		return nil
	})
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func (s *Store) AddEvent(session, name string, props map[string]string) Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextEvt++
	e := Event{ID: s.nextEvt, T: s.now(), Session: session, Name: name, Props: props}
	s.events = append(s.events, e)
	if len(s.events) > maxEvents {
		s.events = append([]Event(nil), s.events[len(s.events)-maxEvents:]...)
	}
	return e
}

func (s *Store) Events(since int, limit int) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Event
	for _, e := range s.events {
		if e.ID > since {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

type FunnelStage struct {
	Event    string  `json:"event"`
	Sessions int     `json:"sessions"`
	Rate     float64 `json:"rate_from_first"` // share of sessions that reached the first stage
}

type Analytics struct {
	Live struct {
		Sessions            int            `json:"sessions"`
		Funnel              []FunnelStage  `json:"funnel"`
		Totals              map[string]int `json:"event_totals"`
		AlternativeSessions int            `json:"alternative_selected_sessions"`
		Branches            []FunnelStage  `json:"branches"` // unique sessions per side path
		Note                string         `json:"note"`
	} `json:"live"`
	Simulated struct {
		Simulated bool          `json:"simulated"`
		Label     string        `json:"label"`
		Funnel    []FunnelStage `json:"funnel"`
		Branches  []FunnelStage `json:"branches"`
	} `json:"simulated_sample"`
}

// simulatedFunnel is illustrative seed data only. It is not measured anywhere
// and the API and dashboard label it as such.
var simulatedFunnel = []int{1000, 184, 131, 39, 21}
var simulatedBranches = []int{88, 62, 34, 57}

func (s *Store) Analytics() Analytics {
	s.mu.Lock()
	defer s.mu.Unlock()
	reached := map[string]map[string]bool{}
	totals := map[string]int{}
	seen := map[string]bool{}
	for _, e := range s.events {
		if reached[e.Name] == nil {
			reached[e.Name] = map[string]bool{}
		}
		reached[e.Name][e.Session] = true
		totals[e.Name]++
		seen[e.Session] = true
	}
	var a Analytics
	a.Live.Sessions = len(seen)
	a.Live.Totals = totals
	a.Live.AlternativeSessions = len(reached["alternative_selected"])
	a.Live.Note = "Counts come from real interactions with this running deployment only (demo/test sessions)."
	first := len(reached[funnelStages[0]])
	for _, st := range funnelStages {
		n := len(reached[st])
		rate := 0.0
		if first > 0 {
			rate = roundTo(float64(n)/float64(first), 3)
		}
		a.Live.Funnel = append(a.Live.Funnel, FunnelStage{Event: st, Sessions: n, Rate: rate})
	}
	for _, b := range branchEvents {
		n := len(reached[b])
		rate := 0.0
		if first > 0 {
			rate = roundTo(float64(n)/float64(first), 3)
		}
		a.Live.Branches = append(a.Live.Branches, FunnelStage{Event: b, Sessions: n, Rate: rate})
	}
	a.Simulated.Simulated = true
	for i, b := range branchEvents {
		a.Simulated.Branches = append(a.Simulated.Branches, FunnelStage{Event: b, Sessions: simulatedBranches[i], Rate: roundTo(float64(simulatedBranches[i])/float64(simulatedFunnel[0]), 3)})
	}
	a.Simulated.Label = "SIMULATED sample data for layout only. Not measured, not a Google campaign result."
	for i, st := range funnelStages {
		a.Simulated.Funnel = append(a.Simulated.Funnel, FunnelStage{Event: st, Sessions: simulatedFunnel[i], Rate: roundTo(float64(simulatedFunnel[i])/float64(simulatedFunnel[0]), 3)})
	}
	return a
}
