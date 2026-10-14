package api

import (
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

// rawOne runs a query that returns one jsonb column. No row is a 404.
func rawOne(ctx context.Context, q db, sql string, args ...any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := q.QueryRow(ctx, sql, args...).Scan(&raw); err != nil {
		return nil, mapErr(err)
	}
	return raw, nil
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
		out = append(out, raw)
	}
	return out, rows.Err()
}

// qb builds optional WHERE conditions. Each condition holds one "?" that becomes the next $n.
type qb struct {
	conds []string
	args  []any
}

func (b *qb) add(cond string, v any) {
	b.args = append(b.args, v)
	b.conds = append(b.conds, strings.Replace(cond, "?", "$"+strconv.Itoa(len(b.args)), 1))
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
	n := len(args)
	var ct *time.Time
	var cid *uuid.UUID
	if cur != nil {
		ct, cid = &cur.T, &cur.ID
	}
	sql := fmt.Sprintf(`SELECT to_jsonb(x) - '_t' - '_id', x._t, x._id FROM (%s) x
		WHERE ($%d::timestamptz IS NULL OR (x._t, x._id) < ($%d::timestamptz, $%d::uuid))
		ORDER BY x._t DESC, x._id DESC LIMIT %d`, inner, n+1, n+1, n+2, limit+1)
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
