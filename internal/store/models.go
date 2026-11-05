package store

import "strings"

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Parcel is a delivery someone is waiting for (direction "in") or one they
// sent ("out").
type Parcel struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"-"`
	Title           string `json:"title"`
	TrackingNumber  string `json:"tracking_number"`
	Carrier         string `json:"carrier"`
	Direction       string `json:"direction"`
	Shop            string `json:"shop"`
	OrderNumber     string `json:"order_number"`
	OrderedOn       string `json:"ordered_on"`
	Price           *int64 `json:"price"`
	ExtraCost       *int64 `json:"extra_cost"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`
	StatusText      string `json:"status_text"`
	StatusAt        string `json:"status_at"`
	Location        string `json:"location"`
	ExpectedOn      string `json:"expected_on"`
	ExpectedUntil   string `json:"expected_until"`
	ExpectedManual  bool   `json:"expected_manual"`
	DeliveredAt     string `json:"delivered_at"`
	ReturnBy        string `json:"return_by"`
	ReturnState     string `json:"return_state"`
	Notes           string `json:"notes"`
	Archived        bool   `json:"archived"`
	Provider        string `json:"provider"`
	ProviderRef     string `json:"-"`
	CheckedAt       string `json:"checked_at"`
	NextCheckAt     string `json:"next_check_at"`
	CheckError      string `json:"check_error"`
	Source          string `json:"source"`
	ExternalID      string `json:"external_id"`
	MailMessageID   *int64 `json:"mail_message_id"`
	WishID          *int64 `json:"wish_id"`
	WishTitle       string `json:"wish_title"`
	MoneyTxID       *int64 `json:"money_tx_id"`
	MoneyTxLabel    string `json:"money_tx_label"`
	ExtraTxID       *int64 `json:"extra_tx_id"`
	InventoryItemID *int64 `json:"inventory_item_id"`
	ContactID       *int64 `json:"contact_id"`
	ContactName     string `json:"contact_name"`
	TaskID          *int64 `json:"task_id"`
	ReturnTaskID    *int64 `json:"return_task_id"`
	CalEventID      *int64 `json:"-"`
	CalSynced       string `json:"-"`
	ReturnEventID   *int64 `json:"-"`
	ReturnSynced    string `json:"-"`
	NotifiedStatus  string `json:"-"`
	NotifiedOn      string `json:"-"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`

	// Filled in for views, not stored.
	Days        *int         `json:"days,omitempty"`        // days on the way (delivered: how long it took)
	EventCount  int          `json:"event_count,omitempty"` // steps in the timeline
	Events      []Event      `json:"events,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Active reports whether the parcel is still on its way.
func (p Parcel) Active() bool {
	switch p.Status {
	case "delivered", "returned", "expired":
		return false
	}
	return !p.Archived
}

// Event is one step in a parcel's journey.
type Event struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"-"`
	ParcelID    int64  `json:"parcel_id"`
	At          string `json:"at"`
	Status      string `json:"status"`
	Description string `json:"description"`
	Location    string `json:"location"`
	Source      string `json:"source"`
	Fingerprint string `json:"-"`
	CreatedAt   string `json:"created_at"`
}

// Find is a shipping email found in Pocket Mail.
type Find struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"-"`
	MailMessageID  int64  `json:"mail_message_id"`
	Subject        string `json:"subject"`
	FromName       string `json:"from_name"`
	FromAddr       string `json:"from_addr"`
	ReceivedAt     string `json:"received_at"`
	TrackingNumber string `json:"tracking_number"`
	Carrier        string `json:"carrier"`
	Shop           string `json:"shop"`
	OrderNumber    string `json:"order_number"`
	Title          string `json:"title"`
	StatusHint     string `json:"status_hint"`
	State          string `json:"state"`
	ParcelID       *int64 `json:"parcel_id"`
	CreatedAt      string `json:"created_at"`
}

// Attachment is a paper or link kept with a parcel.
type Attachment struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	ParcelID  int64  `json:"parcel_id"`
	Kind      string `json:"kind"` // document | url
	Ref       *int64 `json:"ref"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

// Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday
	Currency   string `json:"currency"`   // for new parcels

	// Tracking.
	TrackCainiao    bool   `json:"track_cainiao"`     // free tracking of AliExpress / Cainiao parcels
	Track17Key      string `json:"track17_key"`       // 17TRACK API key: every other carrier
	AutoArchiveDays int    `json:"auto_archive_days"` // delivered parcels leave the list after N days (0 = never)
	StaleDays       int    `json:"stale_days"`        // no news for N days = "stuck?"

	// Notifications.
	NotifyMoves     bool   `json:"notify_moves"`     // every new step
	NotifyOut       bool   `json:"notify_out"`       // out for delivery
	NotifyPickup    bool   `json:"notify_pickup"`    // waiting at a pick-up point
	NotifyDelivered bool   `json:"notify_delivered"` // delivered
	NotifyProblems  bool   `json:"notify_problems"`  // a problem, sent back, tracking expired
	NotifyReturns   bool   `json:"notify_returns"`   // the last day to send something back is near
	NotifyStale     bool   `json:"notify_stale"`     // no news for a while
	NotifyHour      int    `json:"notify_hour"`      // daily reminders (returns, stuck parcels)
	NotifyURL       string `json:"notify_url"`
	NotifyFormat    string `json:"notify_format"` // apprise | ntfy | json

	// Pocket Mail: shipping emails become parcels and updates.
	MailMode      string `json:"mail_mode"`
	MailURL       string `json:"mail_url"`
	MailToken     string `json:"mail_token"`
	MailPublicURL string `json:"mail_public_url"`
	MailScan      bool   `json:"mail_scan"`     // look through new email
	MailAutoAdd   bool   `json:"mail_auto_add"` // add found parcels at once instead of asking
	MailDays      int    `json:"mail_days"`     // how far back the first look goes

	// Pocket Money: the payment for an order, and extra costs (customs, postage).
	MoneyMode       string `json:"money_mode"`
	MoneyURL        string `json:"money_url"`
	MoneyToken      string `json:"money_token"`
	MoneyPublicURL  string `json:"money_public_url"`
	MoneyAccountID  int64  `json:"money_account_id"`
	MoneyCategoryID int64  `json:"money_category_id"`

	// Pocket Wishlist: a parcel is the wish you ordered.
	WishlistMode      string `json:"wishlist_mode"`
	WishlistURL       string `json:"wishlist_url"`
	WishlistToken     string `json:"wishlist_token"`
	WishlistPublicURL string `json:"wishlist_public_url"`
	WishlistBought    bool   `json:"wishlist_bought"` // mark the wish bought when a parcel is linked to it

	// Pocket Inventory: a delivered parcel goes on your shelves.
	InventoryMode      string `json:"inventory_mode"`
	InventoryURL       string `json:"inventory_url"`
	InventoryToken     string `json:"inventory_token"`
	InventoryPublicURL string `json:"inventory_public_url"`
	InventoryAuto      bool   `json:"inventory_auto"` // add every delivered incoming parcel
	InventoryLocation  string `json:"inventory_location"`

	// Pocket Calendar: expected delivery days and return deadlines as all-day events.
	CalendarMode      string `json:"calendar_mode"`
	CalendarURL       string `json:"calendar_url"`
	CalendarToken     string `json:"calendar_token"`
	CalendarPublicURL string `json:"calendar_public_url"`
	CalendarSync      bool   `json:"calendar_sync"`
	CalendarReturns   bool   `json:"calendar_returns"`
	CalendarID        int64  `json:"calendar_id"` // 0 = the person's default calendar

	// Pocket Tasks: pick a parcel up, send one back.
	TasksMode      string `json:"tasks_mode"`
	TasksURL       string `json:"tasks_url"`
	TasksToken     string `json:"tasks_token"`
	TasksPublicURL string `json:"tasks_public_url"`
	TasksPickup    bool   `json:"tasks_pickup"`  // a task when a parcel waits at a pick-up point
	TasksReturns   bool   `json:"tasks_returns"` // a task when you decide to send one back

	// Pocket Contacts: who you send a parcel to.
	ContactsMode      string `json:"contacts_mode"`
	ContactsURL       string `json:"contacts_url"`
	ContactsToken     string `json:"contacts_token"`
	ContactsPublicURL string `json:"contacts_public_url"`
	// Pocket Documents: invoices, customs papers, return labels.
	DocumentsMode      string `json:"documents_mode"`
	DocumentsURL       string `json:"documents_url"`
	DocumentsToken     string `json:"documents_token"`
	DocumentsPublicURL string `json:"documents_public_url"`

	// AI reads shipping emails the patterns miss, and answers questions:
	// Pocket AI through Core, or an OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
