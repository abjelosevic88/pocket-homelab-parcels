package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

const parcelCols = `id, user_id, title, tracking_number, carrier, direction, shop, order_number, ordered_on, price, extra_cost, currency,
	status, status_text, status_at, location, expected_on, expected_until, expected_manual, delivered_at, return_by, return_state, notes, archived,
	provider, provider_ref, checked_at, next_check_at, check_error, source, external_id, mail_message_id,
	wish_id, wish_title, money_tx_id, money_tx_label, extra_tx_id, inventory_item_id, contact_id, contact_name, task_id, return_task_id,
	cal_event_id, cal_synced, return_event_id, return_synced, notified_status, notified_on, created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanParcel(row scanner) (*Parcel, error) {
	var p Parcel
	var price, extra, mail, wish, tx, xtx, inv, contact, task, rtask, cal, rcal sql.NullInt64
	var manual, archived int
	err := row.Scan(&p.ID, &p.UserID, &p.Title, &p.TrackingNumber, &p.Carrier, &p.Direction, &p.Shop, &p.OrderNumber, &p.OrderedOn, &price, &extra, &p.Currency,
		&p.Status, &p.StatusText, &p.StatusAt, &p.Location, &p.ExpectedOn, &p.ExpectedUntil, &manual, &p.DeliveredAt, &p.ReturnBy, &p.ReturnState, &p.Notes, &archived,
		&p.Provider, &p.ProviderRef, &p.CheckedAt, &p.NextCheckAt, &p.CheckError, &p.Source, &p.ExternalID, &mail,
		&wish, &p.WishTitle, &tx, &p.MoneyTxLabel, &xtx, &inv, &contact, &p.ContactName, &task, &rtask,
		&cal, &p.CalSynced, &rcal, &p.ReturnSynced, &p.NotifiedStatus, &p.NotifiedOn, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	p.ExpectedManual, p.Archived = manual == 1, archived == 1
	ptr := func(n sql.NullInt64) *int64 {
		if !n.Valid {
			return nil
		}
		v := n.Int64
		return &v
	}
	p.Price, p.ExtraCost, p.MailMessageID, p.WishID, p.MoneyTxID, p.ExtraTxID = ptr(price), ptr(extra), ptr(mail), ptr(wish), ptr(tx), ptr(xtx)
	p.InventoryItemID, p.ContactID, p.TaskID, p.ReturnTaskID, p.CalEventID, p.ReturnEventID = ptr(inv), ptr(contact), ptr(task), ptr(rtask), ptr(cal), ptr(rcal)
	p.fill(time.Now())
	return &p, nil
}

// fill computes the view fields.
func (p *Parcel) fill(now time.Time) {
	start, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if p.OrderedOn != "" {
		if t, e := time.ParseInLocation("2006-01-02", p.OrderedOn, time.Local); e == nil {
			start, err = t, nil
		}
	}
	if err != nil {
		return
	}
	end := now
	if p.DeliveredAt != "" {
		if t, e := time.Parse(time.RFC3339, p.DeliveredAt); e == nil {
			end = t
		}
	}
	d := int(end.Sub(start).Hours() / 24)
	if d < 0 {
		d = 0
	}
	p.Days = &d
}

// nullable money: 0 is a real price, nil is "not known".
func nullP(i *int64) any {
	if i == nil {
		return nil
	}
	return *i
}

// ParcelFilter narrows a list of parcels.
type ParcelFilter struct {
	View      string // active (default) | delivered | archived | all | returns | problems
	Q         string
	Shop      string
	Carrier   string
	Direction string // in | out | ""
	Status    string
	Limit     int
	Offset    int
}

// ListParcels lists one person's parcels: active ones by what needs you
// first (out for delivery, pick-up, problems) then soonest expected;
// delivered ones newest first.
func (s *Store) ListParcels(ctx context.Context, uid int64, f ParcelFilter) ([]Parcel, int, error) {
	where := []string{"user_id = ?"}
	args := []any{uid}
	order := `CASE status WHEN 'out_for_delivery' THEN 0 WHEN 'pickup' THEN 1 WHEN 'exception' THEN 2 ELSE 3 END,
		CASE WHEN expected_on = '' THEN 1 ELSE 0 END, expected_on, COALESCE(NULLIF(status_at, ''), created_at) DESC, id DESC`
	switch f.View {
	case "", "active":
		where = append(where, "archived = 0", "status NOT IN ('delivered','returned','expired')")
	case "delivered":
		where = append(where, "status IN ('delivered','returned','expired')", "archived = 0")
		order = "COALESCE(NULLIF(delivered_at, ''), NULLIF(status_at, ''), updated_at) DESC, id DESC"
	case "archived":
		where = append(where, "archived = 1")
		order = "COALESCE(NULLIF(delivered_at, ''), NULLIF(status_at, ''), updated_at) DESC, id DESC"
	case "returns":
		where = append(where, "return_by != ''", "return_state != 'returned'", "return_state != 'keeping'")
		order = "return_by, id"
	case "problems":
		where = append(where, "archived = 0", "status IN ('exception','returned','expired')")
	case "all":
		order = "COALESCE(NULLIF(status_at, ''), created_at) DESC, id DESC"
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		like := "%" + strings.ToLower(q) + "%"
		where = append(where, `(lower(title) LIKE ? OR lower(tracking_number) LIKE ? OR lower(shop) LIKE ? OR lower(order_number) LIKE ? OR lower(notes) LIKE ? OR lower(contact_name) LIKE ? OR lower(status_text) LIKE ?)`)
		args = append(args, like, strings.ToLower(strings.ReplaceAll(like, " ", "")), like, like, like, like, like)
	}
	if f.Shop != "" {
		where = append(where, "lower(shop) = lower(?)")
		args = append(args, f.Shop)
	}
	if f.Carrier != "" {
		where = append(where, "carrier = ?")
		args = append(args, f.Carrier)
	}
	if f.Direction == "in" || f.Direction == "out" {
		where = append(where, "direction = ?")
		args = append(args, f.Direction)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM parcels WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	q := `SELECT ` + parcelCols + ` FROM parcels WHERE ` + w + ` ORDER BY ` + order
	if f.Limit > 0 {
		q += ` LIMIT ? OFFSET ?`
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Parcel{}
	for rows.Next() {
		p, err := scanParcel(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	if err := s.fillEventCounts(ctx, uid, out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *Store) fillEventCounts(ctx context.Context, uid int64, ps []Parcel) error {
	if len(ps) == 0 {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT parcel_id, COUNT(1) FROM parcel_events WHERE user_id = ? GROUP BY parcel_id`, uid)
	if err != nil {
		return err
	}
	defer rows.Close()
	n := map[int64]int{}
	for rows.Next() {
		var id int64
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			return err
		}
		n[id] = c
	}
	for i := range ps {
		ps[i].EventCount = n[ps[i].ID]
	}
	return rows.Err()
}

// AllParcels is every parcel of a person, archived included (export, sync).
func (s *Store) AllParcels(ctx context.Context, uid int64) ([]Parcel, error) {
	ps, _, err := s.ListParcels(ctx, uid, ParcelFilter{View: "all"})
	return ps, err
}

// GetParcel reads one of a person's parcels.
func (s *Store) GetParcel(ctx context.Context, uid, id int64) (*Parcel, error) {
	p, err := scanParcel(s.DB.QueryRowContext(ctx, `SELECT `+parcelCols+` FROM parcels WHERE id = ? AND user_id = ?`, id, uid))
	if err != nil {
		return nil, err
	}
	n := 0
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM parcel_events WHERE parcel_id = ? AND user_id = ?`, id, uid).Scan(&n)
	p.EventCount = n
	return p, nil
}

// ParcelByNumber finds a person's parcel by its tracking number.
func (s *Store) ParcelByNumber(ctx context.Context, uid int64, number string) (*Parcel, error) {
	return scanParcel(s.DB.QueryRowContext(ctx, `SELECT `+parcelCols+` FROM parcels WHERE user_id = ? AND tracking_number = ? AND tracking_number != ''`, uid, number))
}

// ParcelByExternal finds a parcel another app (or an import) created with this id.
func (s *Store) ParcelByExternal(ctx context.Context, uid int64, source, ext string) (*Parcel, error) {
	return scanParcel(s.DB.QueryRowContext(ctx, `SELECT `+parcelCols+` FROM parcels WHERE user_id = ? AND source = ? AND external_id = ? AND external_id != ''`, uid, source, ext))
}

// CreateParcel adds a parcel for a person.
func (s *Store) CreateParcel(ctx context.Context, uid int64, p *Parcel) error {
	now := Now()
	if p.Status == "" {
		p.Status = "pending"
	}
	if p.Direction == "" {
		p.Direction = "in"
	}
	if p.Source == "" {
		p.Source = "manual"
	}
	if p.Currency == "" {
		p.Currency = "EUR"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO parcels (user_id, title, tracking_number, carrier, direction, shop, order_number, ordered_on, price, extra_cost, currency,
		status, status_text, status_at, location, expected_on, expected_until, expected_manual, delivered_at, return_by, return_state, notes, archived,
		provider, provider_ref, checked_at, next_check_at, check_error, source, external_id, mail_message_id,
		wish_id, wish_title, money_tx_id, money_tx_label, extra_tx_id, inventory_item_id, contact_id, contact_name, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?,?, ?,?,?,?,?,?,?,?,?,?)`,
		uid, p.Title, p.TrackingNumber, p.Carrier, p.Direction, p.Shop, p.OrderNumber, p.OrderedOn, nullP(p.Price), nullP(p.ExtraCost), p.Currency,
		p.Status, p.StatusText, p.StatusAt, p.Location, p.ExpectedOn, p.ExpectedUntil, boolInt(p.ExpectedManual), p.DeliveredAt, p.ReturnBy, p.ReturnState, p.Notes, boolInt(p.Archived),
		p.Provider, p.ProviderRef, p.CheckedAt, p.NextCheckAt, p.CheckError, p.Source, p.ExternalID, nullI(p.MailMessageID),
		nullI(p.WishID), p.WishTitle, nullI(p.MoneyTxID), p.MoneyTxLabel, nullI(p.ExtraTxID), nullI(p.InventoryItemID), nullI(p.ContactID), p.ContactName, now, now)
	if err != nil {
		return conflict(err)
	}
	p.ID, _ = res.LastInsertId()
	p.UserID, p.CreatedAt, p.UpdatedAt = uid, now, now
	p.fill(time.Now())
	return nil
}

// UpdateParcel saves what a person (or the tracker) can change. Calendar
// sync state is cleared so the next sync moves the events.
func (s *Store) UpdateParcel(ctx context.Context, uid int64, p *Parcel) error {
	now := Now()
	res, err := s.DB.ExecContext(ctx, `UPDATE parcels SET title=?, tracking_number=?, carrier=?, direction=?, shop=?, order_number=?, ordered_on=?, price=?, extra_cost=?, currency=?,
		status=?, status_text=?, status_at=?, location=?, expected_on=?, expected_until=?, expected_manual=?, delivered_at=?, return_by=?, return_state=?, notes=?, archived=?,
		provider=?, provider_ref=?, checked_at=?, next_check_at=?, check_error=?, mail_message_id=?,
		wish_id=?, wish_title=?, money_tx_id=?, money_tx_label=?, extra_tx_id=?, inventory_item_id=?, contact_id=?, contact_name=?, task_id=?, return_task_id=?,
		cal_synced='', return_synced='', updated_at=?
		WHERE id=? AND user_id=?`,
		p.Title, p.TrackingNumber, p.Carrier, p.Direction, p.Shop, p.OrderNumber, p.OrderedOn, nullP(p.Price), nullP(p.ExtraCost), p.Currency,
		p.Status, p.StatusText, p.StatusAt, p.Location, p.ExpectedOn, p.ExpectedUntil, boolInt(p.ExpectedManual), p.DeliveredAt, p.ReturnBy, p.ReturnState, p.Notes, boolInt(p.Archived),
		p.Provider, p.ProviderRef, p.CheckedAt, p.NextCheckAt, p.CheckError, nullI(p.MailMessageID),
		nullI(p.WishID), p.WishTitle, nullI(p.MoneyTxID), p.MoneyTxLabel, nullI(p.ExtraTxID), nullI(p.InventoryItemID), nullI(p.ContactID), p.ContactName, nullI(p.TaskID), nullI(p.ReturnTaskID),
		now, p.ID, uid)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	p.UpdatedAt = now
	return nil
}

// SetChecked records a tracking check without touching anything else.
func (s *Store) SetChecked(ctx context.Context, uid, id int64, next, errText, providerRef string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE parcels SET checked_at = ?, next_check_at = ?, check_error = ?, provider_ref = CASE WHEN ? != '' THEN ? ELSE provider_ref END WHERE id = ? AND user_id = ?`,
		Now(), next, errText, providerRef, providerRef, id, uid)
	return err
}

// SetNotified remembers the last status a push went out for.
func (s *Store) SetNotified(ctx context.Context, uid, id int64, status, on string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE parcels SET notified_status = CASE WHEN ? != '' THEN ? ELSE notified_status END, notified_on = CASE WHEN ? != '' THEN ? ELSE notified_on END WHERE id = ? AND user_id = ?`,
		status, status, on, on, id, uid)
	return err
}

// SetCalendar stores a parcel's calendar events and what they show.
func (s *Store) SetCalendar(ctx context.Context, uid, id int64, which string, event *int64, synced string) error {
	col, key := "cal_event_id", "cal_synced"
	if which == "return" {
		col, key = "return_event_id", "return_synced"
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE parcels SET `+col+` = ?, `+key+` = ? WHERE id = ? AND user_id = ?`, nullI(event), synced, id, uid)
	return err
}

// SetLink stores the id of something made in another app (task, item,
// transaction) on a parcel.
func (s *Store) SetLink(ctx context.Context, uid, id int64, col string, v *int64) error {
	switch col {
	case "task_id", "return_task_id", "inventory_item_id", "money_tx_id", "extra_tx_id", "wish_id":
	default:
		return ErrNotFound
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE parcels SET `+col+` = ?, updated_at = ? WHERE id = ? AND user_id = ?`, nullI(v), Now(), id, uid)
	return err
}

// DeleteParcel removes a parcel with its timeline and papers.
func (s *Store) DeleteParcel(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM parcels WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DueChecks lists active parcels whose next tracking check is due, across
// everybody (the tracker works per person after that).
func (s *Store) DueChecks(ctx context.Context, now string, limit int) ([]Parcel, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+parcelCols+` FROM parcels
		WHERE provider != '' AND archived = 0 AND tracking_number != '' AND status NOT IN ('delivered','returned','expired')
		AND (next_check_at = '' OR next_check_at <= ?) ORDER BY next_check_at, id LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Parcel
	for rows.Next() {
		p, err := scanParcel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// ArchiveDelivered moves a person's parcels delivered more than days ago
// out of the list.
func (s *Store) ArchiveDelivered(ctx context.Context, uid int64, days int) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	cut := time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339)
	res, err := s.DB.ExecContext(ctx, `UPDATE parcels SET archived = 1, updated_at = ? WHERE user_id = ? AND archived = 0
		AND status IN ('delivered','returned','expired') AND COALESCE(NULLIF(delivered_at, ''), NULLIF(status_at, ''), updated_at) < ?
		AND NOT (return_by != '' AND return_state = 'returning')`, Now(), uid, cut)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Shops lists the shops a person ordered from, most used first (suggestions).
func (s *Store) Shops(ctx context.Context, uid int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT shop FROM parcels WHERE user_id = ? AND shop != '' GROUP BY lower(shop) ORDER BY COUNT(1) DESC, MAX(created_at) DESC LIMIT 50`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------------- events ----------------

// AddEvent keeps a step once per source (the fingerprint); it reports
// whether the step is new.
func (s *Store) AddEvent(ctx context.Context, uid int64, e *Event) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO parcel_events (user_id, parcel_id, at, status, description, location, source, fingerprint)
		SELECT ?, id, ?, ?, ?, ?, ?, ? FROM parcels WHERE id = ? AND user_id = ?
		ON CONFLICT(parcel_id, fingerprint) DO NOTHING`, uid, e.At, e.Status, e.Description, e.Location, e.Source, e.Fingerprint, e.ParcelID, uid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	e.ID, _ = res.LastInsertId()
	e.UserID = uid
	return true, nil
}

// ListEvents is a parcel's timeline, newest first.
func (s *Store) ListEvents(ctx context.Context, uid, parcelID int64) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, parcel_id, at, status, description, location, source, fingerprint, created_at
		FROM parcel_events WHERE user_id = ? AND parcel_id = ? ORDER BY at DESC, id DESC`, uid, parcelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ParcelID, &e.At, &e.Status, &e.Description, &e.Location, &e.Source, &e.Fingerprint, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.UserID = uid
		out = append(out, e)
	}
	return out, rows.Err()
}

// AllEvents is every step of every parcel of a person (export).
func (s *Store) AllEvents(ctx context.Context, uid int64) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, parcel_id, at, status, description, location, source, fingerprint, created_at
		FROM parcel_events WHERE user_id = ? ORDER BY parcel_id, at, id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ParcelID, &e.At, &e.Status, &e.Description, &e.Location, &e.Source, &e.Fingerprint, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventsBetween lists steps of a person's parcels between two instants
// (RFC 3339, from inclusive, to exclusive), oldest first.
func (s *Store) EventsBetween(ctx context.Context, uid int64, from, to string) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, parcel_id, at, status, description, location, source, fingerprint, created_at
		FROM parcel_events WHERE user_id = ? AND at >= ? AND at < ? ORDER BY at, id`, uid, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.ParcelID, &e.At, &e.Status, &e.Description, &e.Location, &e.Source, &e.Fingerprint, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteEvent removes one step the person added by hand.
func (s *Store) DeleteEvent(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM parcel_events WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// GetEvent reads one step.
func (s *Store) GetEvent(ctx context.Context, uid, id int64) (*Event, error) {
	var e Event
	err := s.DB.QueryRowContext(ctx, `SELECT id, parcel_id, at, status, description, location, source, fingerprint, created_at FROM parcel_events WHERE id = ? AND user_id = ?`, id, uid).
		Scan(&e.ID, &e.ParcelID, &e.At, &e.Status, &e.Description, &e.Location, &e.Source, &e.Fingerprint, &e.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	return &e, nil
}

// ---------------- email finds ----------------

const findCols = `id, user_id, mail_message_id, subject, from_name, from_addr, received_at, tracking_number, carrier, shop, order_number, title, status_hint, state, parcel_id, created_at`

func scanFind(row scanner) (*Find, error) {
	var f Find
	var pid sql.NullInt64
	if err := row.Scan(&f.ID, &f.UserID, &f.MailMessageID, &f.Subject, &f.FromName, &f.FromAddr, &f.ReceivedAt, &f.TrackingNumber, &f.Carrier, &f.Shop, &f.OrderNumber, &f.Title, &f.StatusHint, &f.State, &pid, &f.CreatedAt); err != nil {
		return nil, notFound(err)
	}
	if pid.Valid {
		v := pid.Int64
		f.ParcelID = &v
	}
	return &f, nil
}

// SaveFind keeps a find once per message and number; it reports whether it is new.
func (s *Store) SaveFind(ctx context.Context, uid int64, f *Find) (bool, error) {
	if f.State == "" {
		f.State = "new"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO mail_finds (user_id, mail_message_id, subject, from_name, from_addr, received_at, tracking_number, carrier, shop, order_number, title, status_hint, state, parcel_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id, mail_message_id, tracking_number) DO NOTHING`,
		uid, f.MailMessageID, f.Subject, f.FromName, f.FromAddr, f.ReceivedAt, f.TrackingNumber, f.Carrier, f.Shop, f.OrderNumber, f.Title, f.StatusHint, f.State, nullI(f.ParcelID))
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	f.ID, _ = res.LastInsertId()
	f.UserID = uid
	return true, nil
}

// ListFinds lists a person's finds; state "" = the ones waiting for an answer.
func (s *Store) ListFinds(ctx context.Context, uid int64, state string, limit int) ([]Find, error) {
	if state == "" {
		state = "new"
	}
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT ` + findCols + ` FROM mail_finds WHERE user_id = ?`
	args := []any{uid}
	if state != "all" {
		q += ` AND state = ?`
		args = append(args, state)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY received_at DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Find{}
	for rows.Next() {
		f, err := scanFind(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetFind reads one find.
func (s *Store) GetFind(ctx context.Context, uid, id int64) (*Find, error) {
	return scanFind(s.DB.QueryRowContext(ctx, `SELECT `+findCols+` FROM mail_finds WHERE id = ? AND user_id = ?`, id, uid))
}

// SetFindState answers a find.
func (s *Store) SetFindState(ctx context.Context, uid, id int64, state string, parcel *int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE mail_finds SET state = ?, parcel_id = COALESCE(?, parcel_id) WHERE id = ? AND user_id = ?`, state, nullI(parcel), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountFinds is how many finds wait for an answer.
func (s *Store) CountFinds(ctx context.Context, uid int64) int {
	n := 0
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM mail_finds WHERE user_id = ? AND state = 'new'`, uid).Scan(&n)
	return n
}

// ---------------- attachments ----------------

// ListAttachments lists a parcel's papers and links.
func (s *Store) ListAttachments(ctx context.Context, uid, parcelID int64) ([]Attachment, error) {
	q := `SELECT id, parcel_id, kind, ref, url, title, created_at FROM attachments WHERE user_id = ?`
	args := []any{uid}
	if parcelID > 0 {
		q += ` AND parcel_id = ?`
		args = append(args, parcelID)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		var a Attachment
		var ref sql.NullInt64
		if err := rows.Scan(&a.ID, &a.ParcelID, &a.Kind, &ref, &a.URL, &a.Title, &a.CreatedAt); err != nil {
			return nil, err
		}
		if ref.Valid {
			v := ref.Int64
			a.Ref = &v
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CreateAttachment keeps a paper or link with one of the person's parcels.
func (s *Store) CreateAttachment(ctx context.Context, uid int64, a *Attachment) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO attachments (user_id, parcel_id, kind, ref, url, title)
		SELECT ?, id, ?, ?, ?, ? FROM parcels WHERE id = ? AND user_id = ?`, uid, a.Kind, nullI(a.Ref), a.URL, a.Title, a.ParcelID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	a.ID, _ = res.LastInsertId()
	a.UserID, a.CreatedAt = uid, Now()
	return nil
}

// DeleteAttachment removes one.
func (s *Store) DeleteAttachment(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM attachments WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
