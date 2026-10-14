package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
)

// Most staff-facing reads are shaped in SQL (to_jsonb) so the column names in the schema are the
// field names in the API. The contract test checks each response against openapi.yaml.

// stripNulls drops null members at every depth. The contract never requires a nullable field, and an
// absent member reads the same as null to a client, so responses stay small and uniform.
func stripNulls(raw json.RawMessage) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return raw
	}
	out, err := json.Marshal(strip(v))
	if err != nil {
		return raw
	}
	return out
}

func strip(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if e == nil {
				delete(x, k)
			} else {
				x[k] = strip(e)
			}
		}
	case []any:
		for i, e := range x {
			x[i] = strip(e)
		}
	}
	return v
}

// compact removes nil values from a map built in Go, one level deep, plus the maps in any []map child.
func compact(m map[string]any) map[string]any {
	for k, v := range m {
		switch x := v.(type) {
		case nil:
			delete(m, k)
		case []map[string]any:
			for _, e := range x {
				compact(e)
			}
		case map[string]any:
			compact(x)
		}
	}
	return m
}

// rawOne runs a query that returns one jsonb column. No row is a 404.
func rawOne(ctx context.Context, q db, sql string, args ...any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := q.QueryRow(ctx, sql, args...).Scan(&raw); err != nil {
		return nil, mapErr(err)
	}
	return stripNulls(raw), nil
}

// rawAll runs a query that returns one jsonb column per row.
func rawAll(ctx context.Context, q db, sql string, args ...any) ([]json.RawMessage, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, stripNulls(raw))
	}
	return out, rows.Err()
}

// qb builds optional WHERE conditions. Every "?" in a condition becomes the same new $n.
type qb struct {
	conds []string
	args  []any
}

func (b *qb) add(cond string, v any) {
	b.args = append(b.args, v)
	b.conds = append(b.conds, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(b.args))))
}

func (b *qb) raw(cond string) { b.conds = append(b.conds, cond) }

func (b *qb) where() string {
	if len(b.conds) == 0 {
		return "TRUE"
	}
	return strings.Join(b.conds, " AND ")
}

// pagedRaw returns a newest-first page of jsonb rows. inner must select the output columns plus
// _t (timestamptz) and _id (uuid), which order the page and form the cursor.
func pagedRaw(ctx context.Context, q db, inner string, args []any, cur *cursor, limit int) (page[json.RawMessage], error) {
	return pagedRawDir(ctx, q, inner, args, cur, limit, false)
}

// pagedRawDir is pagedRaw with a choice of order: asc pages oldest first (work queues, expiry lists).
func pagedRawDir(ctx context.Context, q db, inner string, args []any, cur *cursor, limit int, asc bool) (page[json.RawMessage], error) {
	n := len(args)
	op, dir := "<", "DESC"
	if asc {
		op, dir = ">", "ASC"
	}
	var ct *time.Time
	var cid *uuid.UUID
	if cur != nil {
		ct, cid = &cur.T, &cur.ID
	}
	sql := fmt.Sprintf(`SELECT to_jsonb(x) - '_t' - '_id', x._t, x._id FROM (%s) x
		WHERE ($%d::timestamptz IS NULL OR (x._t, x._id) %s ($%d::timestamptz, $%d::uuid))
		ORDER BY x._t %s, x._id %s LIMIT %d`, inner, n+1, op, n+1, n+2, dir, dir, limit+1)
	rows, err := q.Query(ctx, sql, append(append([]any{}, args...), ct, cid)...)
	if err != nil {
		return page[json.RawMessage]{}, err
	}
	defer rows.Close()
	type item struct {
		raw json.RawMessage
		t   time.Time
		id  uuid.UUID
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.raw, &it.t, &it.id); err != nil {
			return page[json.RawMessage]{}, err
		}
		it.raw = stripNulls(it.raw)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return page[json.RawMessage]{}, err
	}
	p := page[json.RawMessage]{Data: []json.RawMessage{}, Meta: pageMeta{Limit: limit}}
	for i, it := range items {
		if i == limit {
			last := items[limit-1]
			c := encodeCursor(last.t, last.id)
			p.Meta.NextCursor = &c
			break
		}
		p.Data = append(p.Data, it.raw)
	}
	return p, nil
}

// bigUUID turns a BIGSERIAL id into a uuid so tables keyed that way can use the same cursor.
const bigUUID = "lpad(to_hex(%s), 32, '0')::uuid"

// notify records an in-app notification for one user, in the caller's transaction.
func notify(ctx context.Context, q db, userID uuid.UUID, eventKey, title, body string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	b, _ := json.Marshal(data)
	_, err := q.Exec(ctx, `
		INSERT INTO notifications (user_id, event_key, channel, status, title, body, data, sent_at)
		VALUES ($1,$2,'in_app','delivered',$3,$4,$5, now())`, userID, eventKey, title, body, b)
	return err
}

// actorID is the signed-in user, or nil for system work such as webhooks.
func actorID(ctx context.Context) *uuid.UUID {
	if p := auth.FromContext(ctx); p != nil {
		return &p.UserID
	}
	return nil
}
