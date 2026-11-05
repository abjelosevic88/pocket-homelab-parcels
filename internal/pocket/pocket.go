// Package pocket talks to sibling Pocket apps: Pocket Mail (shipping
// emails), Pocket Money (the payment for an order, extra costs), Pocket
// Wishlist (the wish a parcel is), Pocket Inventory (what arrived goes on
// the shelves), Pocket Calendar (delivery days as events), Pocket Tasks
// (pick it up, send it back), Pocket Contacts (who gets a parcel) and
// Pocket Documents (invoices, customs papers). Same bearer-token JSON API
// everywhere.
package pocket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a bearer-token JSON client for one Pocket app.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New creates a client.
func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: timeout}}
}

// Configured reports whether URL and token are set.
func (c *Client) Configured() bool { return c != nil && c.BaseURL != "" && c.Token != "" }

// ErrNotFound is a 404 from the sibling app.
var ErrNotFound = errors.New("not found")

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any, v any) error {
	if !c.Configured() {
		return errors.New("not configured")
	}
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errors.New("token rejected (401)")
	}
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2000))
		_ = json.Unmarshal(b, &e)
		if e.Error != "" {
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, e.Error)
		}
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if v == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// Health is the connection test for any Pocket app: /healthz plus a token check.
func (c *Client) Health(ctx context.Context) (string, error) {
	var out struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if !c.Configured() {
		return "", errors.New("not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/healthz", nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	var me map[string]any
	if err := c.do(ctx, http.MethodGet, "/api/v1/auth/me", nil, nil, &me); err != nil {
		return out.Version, err
	}
	return out.Version, nil
}

// Do is a raw JSON call (for a connection test).
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body, v any) error {
	return c.do(ctx, method, path, q, body, v)
}

// list decodes a list that may be bare or wrapped in {key:[…]} / {results:[…]}.
func list[T any](raw json.RawMessage, keys ...string) ([]T, error) {
	var out []T
	if err := json.Unmarshal(raw, &out); err == nil {
		return out, nil
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	for _, k := range append(keys, "results", "items") {
		if b, ok := wrap[k]; ok {
			if err := json.Unmarshal(b, &out); err != nil {
				return nil, err
			}
			return out, nil
		}
	}
	return []T{}, nil
}

// ---------------- Pocket Money ----------------

// Account is a Money account the person can see.
type Account struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Currency string `json:"currency"`
	Everyday bool   `json:"everyday"`
	Archived bool   `json:"archived"`
	Role     string `json:"role"` // owner | editor | viewer
}

// Accounts lists the accounts the person can book on.
func (c *Client) Accounts(ctx context.Context) ([]Account, error) {
	var all []Account
	if err := c.do(ctx, http.MethodGet, "/api/v1/accounts", nil, nil, &all); err != nil {
		return nil, err
	}
	out := []Account{}
	for _, a := range all {
		if !a.Archived && a.Role != "viewer" {
			out = append(out, a)
		}
	}
	return out, nil
}

// Category is a Money category.
type Category struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Kind     string `json:"kind"`
	Archived bool   `json:"archived"`
}

// Categories lists the expense categories usable on an account.
func (c *Client) Categories(ctx context.Context, accountID int64) ([]Category, error) {
	var all []Category
	path := "/api/v1/categories"
	if accountID > 0 {
		path = fmt.Sprintf("/api/v1/accounts/%d/categories", accountID)
	}
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &all); err != nil {
		return nil, err
	}
	out := []Category{}
	for _, ct := range all {
		if !ct.Archived && ct.Kind != "income" {
			out = append(out, ct)
		}
	}
	return out, nil
}

// Tx is what Parcels books: always an expense, in the cost's own currency
// (Money converts to the account's).
type Tx struct {
	AccountID   int64
	Date        string
	Amount      int64 // minor units of Currency
	Currency    string
	Description string
	Payee       string
	CategoryID  int64
	Tags        []string
	Notes       string
	ExternalID  string
}

// CreateTx books a new expense and returns its id.
func (c *Client) CreateTx(ctx context.Context, t Tx) (int64, error) {
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	body := map[string]any{"kind": "expense", "account_id": t.AccountID, "date": t.Date, "amount": t.Amount, "currency": t.Currency,
		"description": t.Description, "payee": t.Payee, "tags": tags, "notes": t.Notes, "source": "parcels", "external_id": t.ExternalID}
	if t.CategoryID > 0 {
		body["category_id"] = t.CategoryID
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/transactions", nil, body, &out); err != nil {
		return 0, err
	}
	return out.ID, nil
}

// UpdateTx changes what Parcels owns on the transaction: date, amount,
// description and payee. Account, category and tags stay as the person left them.
func (c *Client) UpdateTx(ctx context.Context, id int64, t Tx) error {
	body := map[string]any{"date": t.Date, "amount": t.Amount, "currency": t.Currency, "description": t.Description, "payee": t.Payee}
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/transactions/%d", id), nil, body, nil)
}

// DeleteTx removes a transaction; one already gone counts as deleted.
func (c *Client) DeleteTx(ctx context.Context, id int64) error {
	err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/transactions/%d", id), nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// ---------------- Pocket Calendar ----------------

// Calendar is one of the person's calendars.
type Calendar struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Writable bool   `json:"writable"`
	Default  bool   `json:"is_default"`
}

// Calendars lists the calendars the person can write to.
func (c *Client) Calendars(ctx context.Context) ([]Calendar, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/calendars", nil, nil, &raw); err != nil {
		return nil, err
	}
	all, err := list[Calendar](raw, "calendars")
	if err != nil {
		return nil, err
	}
	out := []Calendar{}
	for _, cal := range all {
		if cal.Writable {
			out = append(out, cal)
		}
	}
	return out, nil
}

// Event is an all-day event Parcels keeps in the calendar for a parcel.
type Event struct {
	CalendarID  int64
	Title       string
	Description string
	URL         string
	Date        string
}

func (e Event) body() map[string]any {
	b := map[string]any{"title": e.Title, "description": e.Description, "url": e.URL, "all_day": true, "start": e.Date, "busy": false, "categories": []string{"Parcels"}}
	if e.CalendarID > 0 {
		b["calendar_id"] = e.CalendarID
	}
	return b
}

// CreateEvent adds the event and returns its id.
func (c *Client) CreateEvent(ctx context.Context, e Event) (int64, error) {
	var out struct {
		EventID int64 `json:"event_id"`
		ID      any   `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/events", nil, e.body(), &out); err != nil {
		return 0, err
	}
	if out.EventID > 0 {
		return out.EventID, nil
	}
	// a bare {"id": 12} or an occurrence id like "12:20261104"
	var id int64
	_, _ = fmt.Sscan(strings.SplitN(fmt.Sprint(out.ID), ":", 2)[0], &id)
	return id, nil
}

// UpdateEvent moves or renames the event.
func (c *Client) UpdateEvent(ctx context.Context, id int64, e Event) error {
	b := e.body()
	delete(b, "calendar_id")
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/events/%d", id), nil, b, nil)
}

// DeleteEvent removes the event; one already gone counts as deleted.
func (c *Client) DeleteEvent(ctx context.Context, id int64) error {
	err := c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/events/%d", id), nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// ---------------- Pocket Tasks ----------------

// Task is a task in Pocket Tasks.
type Task struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// CreateTask adds a task with a link back to the parcel.
func (c *Client) CreateTask(ctx context.Context, title, notes, due, link string) (*Task, error) {
	body := map[string]any{"title": title, "notes": notes, "url": link}
	if due != "" {
		body["due"] = due
	}
	var out Task
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------- Pocket Contacts ----------------

// Contact is a card in Pocket Contacts.
type Contact struct {
	ID     int64    `json:"id"`
	FN     string   `json:"fn"`
	Org    string   `json:"org"`
	Title  string   `json:"title"`
	Emails []string `json:"emails"`
	Phones []string `json:"phones"`
	URLs   []string `json:"urls"`
	Note   string   `json:"note"`
}

// SearchContacts finds the person's contacts.
func (c *Client) SearchContacts(ctx context.Context, q string, limit int) ([]Contact, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/search", url.Values{"q": {q}, "limit": {fmt.Sprint(limit)}}, nil, &raw); err != nil {
		return nil, err
	}
	out, err := list[Contact](raw, "contacts")
	if len(out) > limit {
		out = out[:limit]
	}
	return out, err
}

// GetContact reads one contact.
func (c *Client) GetContact(ctx context.Context, id int64) (*Contact, error) {
	var out Contact
	return &out, c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/contacts/%d", id), nil, nil, &out)
}

// ---------------- Pocket Documents ----------------

// Document is a document in Pocket Documents.
type Document struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	OriginalName  string `json:"original_name"`
	Created       string `json:"created"`
	AddedAt       string `json:"added_at"`
	Correspondent string `json:"correspondent_name"`
	DocumentType  string `json:"document_type_name"`
}

// SearchDocuments finds the person's documents (newest first without a query).
func (c *Client) SearchDocuments(ctx context.Context, q string, limit int) ([]Document, error) {
	v := url.Values{"limit": {fmt.Sprint(limit)}, "no_facets": {"1"}}
	if q != "" {
		v.Set("q", q)
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/documents", v, nil, &raw); err != nil {
		return nil, err
	}
	return list[Document](raw, "documents")
}

// ---------------- Pocket Inventory ----------------

// Item is a Pocket Inventory item.
type Item struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	Location string  `json:"location_name"`
	Category string  `json:"category_name"`
	Brand    string  `json:"brand"`
	Model    string  `json:"model"`
	Serial   string  `json:"serial"`
}

// SearchItems looks up inventory items by name.
func (c *Client) SearchItems(ctx context.Context, q string, limit int) ([]Item, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/items", url.Values{"q": {q}, "limit": {fmt.Sprint(limit)}}, nil, &raw); err != nil {
		return nil, err
	}
	return list[Item](raw, "items")
}

// CreateItem puts something on the shelves and returns it.
func (c *Client) CreateItem(ctx context.Context, in ItemInput) (*Item, error) {
	var it Item
	if err := c.do(ctx, http.MethodPost, "/api/v1/items", nil, in, &it); err != nil {
		return nil, err
	}
	return &it, nil
}

// ItemInput is a new inventory item.
type ItemInput struct {
	Name         string   `json:"name"`
	Notes        string   `json:"notes,omitempty"`
	Location     string   `json:"location,omitempty"`
	Category     string   `json:"category,omitempty"`
	Quantity     *float64 `json:"quantity,omitempty"`
	Brand        string   `json:"brand,omitempty"`
	Tags         string   `json:"tags,omitempty"`
	Price        *float64 `json:"price,omitempty"` // major units
	PurchaseDate string   `json:"purchase_date,omitempty"`
	URL          string   `json:"url,omitempty"`
}

// ---------------- Pocket Mail ----------------

// Address is a mail address with its display name.
type Address struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// Message is a message in a mail list.
type Message struct {
	ID       int64   `json:"id"`
	Subject  string  `json:"subject"`
	From     Address `json:"from"`
	Date     string  `json:"date"`
	Snippet  string  `json:"snippet"`
	Category string  `json:"category"`
}

// MessageFull is one message with its text.
type MessageFull struct {
	Message
	Text string `json:"text"`
	HTML string `json:"html"`
}

// Messages lists the person's messages: category (receipts, updates…),
// a search, newest first.
func (c *Client) Messages(ctx context.Context, category, q string, limit, offset int) ([]Message, bool, error) {
	v := url.Values{"limit": {fmt.Sprint(limit)}, "conversations": {"0"}}
	if offset > 0 {
		v.Set("offset", fmt.Sprint(offset))
	}
	if category != "" {
		v.Set("category", category)
	}
	if q != "" {
		v.Set("q", q)
	}
	var out struct {
		Messages []Message `json:"messages"`
		More     bool      `json:"more"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/messages", v, nil, &out); err != nil {
		return nil, false, err
	}
	return out.Messages, out.More, nil
}

// MessageBody reads one message with its text and HTML.
func (c *Client) MessageBody(ctx context.Context, id int64) (*MessageFull, error) {
	var out MessageFull
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/messages/%d", id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------- Pocket Money: reading ----------------

// Transaction is a Money transaction as Parcels shows it.
type Transaction struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Date        string `json:"date"`
	AccountID   int64  `json:"account_id"`
	Amount      int64  `json:"amount"`
	AmountHome  int64  `json:"amount_home"`
	Currency    string `json:"currency"`
	Description string `json:"description"`
	Payee       string `json:"payee"`
}

// Transactions searches the person's expenses (q matches description and
// payee) between two dates.
func (c *Client) Transactions(ctx context.Context, q, from, to string, limit int) ([]Transaction, error) {
	v := url.Values{"kind": {"expense"}, "limit": {fmt.Sprint(limit)}}
	if q != "" {
		v.Set("q", q)
	}
	if from != "" {
		v.Set("from", from)
	}
	if to != "" {
		v.Set("to", to)
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/transactions", v, nil, &raw); err != nil {
		return nil, err
	}
	return list[Transaction](raw, "transactions")
}

// Transaction reads one transaction.
func (c *Client) Transaction(ctx context.Context, id int64) (*Transaction, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/transactions/%d", id), nil, nil, &raw); err != nil {
		return nil, err
	}
	var wrap struct {
		Transaction *Transaction `json:"transaction"`
	}
	if json.Unmarshal(raw, &wrap) == nil && wrap.Transaction != nil && wrap.Transaction.ID > 0 {
		return wrap.Transaction, nil
	}
	var t Transaction
	return &t, json.Unmarshal(raw, &t)
}

// ---------------- Pocket Wishlist ----------------

// Wish is a wish in Pocket Wishlist.
type Wish struct {
	ID       int64  `json:"id"`
	ListID   int64  `json:"list_id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Store    string `json:"store"`
	Price    int64  `json:"price"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
	BoughtAt string `json:"bought_at"`
}

// Wishes lists the person's wishes (q filters by name).
func (c *Client) Wishes(ctx context.Context, q string, limit int) ([]Wish, error) {
	v := url.Values{"limit": {fmt.Sprint(limit)}}
	if q != "" {
		v.Set("q", q)
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v1/wishes", v, nil, &raw); err != nil {
		return nil, err
	}
	all, err := list[Wish](raw, "wishes")
	if err != nil {
		return nil, err
	}
	if q == "" {
		return all, nil
	}
	// older Wishlist versions ignore q
	out := []Wish{}
	for _, w := range all {
		if strings.Contains(strings.ToLower(w.Title+" "+w.Store), strings.ToLower(q)) {
			out = append(out, w)
		}
	}
	return out, nil
}

// GetWish reads one wish.
func (c *Client) GetWish(ctx context.Context, id int64) (*Wish, error) {
	var w Wish
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/wishes/%d", id), nil, nil, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// WishBought is what Parcels tells Wishlist when a wish was ordered.
type WishBought struct {
	Date      string `json:"date,omitempty"`
	Price     *int64 `json:"price,omitempty"`
	Currency  string `json:"currency,omitempty"`
	Store     string `json:"store,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Inventory *bool  `json:"inventory,omitempty"`
}

// MarkWishBought marks a wish bought (Wishlist then does its own follow-up:
// Money, Inventory, Split, as the person set it up there).
func (c *Client) MarkWishBought(ctx context.Context, id int64, in WishBought) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/wishes/%d/bought", id), nil, in, nil)
}
