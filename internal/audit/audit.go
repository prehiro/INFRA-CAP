// Package audit provides a cross-module activity log helper.
// Any module calls audit.Log(...) at the handler layer to record create/update/delete/login/export.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// Entry is the data needed to write one audit row.
type Entry struct {
	ActorID   int
	ActorName string
	Action    string // create|update|delete|login|logout|export
	Entity    string // licenses|users|auth|...
	EntityID  string
	Changes   any // map or struct; JSON-encoded
	IP        string
}

// DB is the minimal interface the helper needs.
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Log writes an audit row. It never returns an error to the caller — audit failures
// must not break the primary operation, only log to stderr.
func Log(ctx context.Context, db DB, e Entry) {
	changes := ""
	if e.Changes != nil {
		if b, err := json.Marshal(e.Changes); err == nil {
			changes = string(b)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO audit_log
		(actor_id, actor_name, action, entity, entity_id, changes, ip, created_at)
		VALUES (@p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8)`,
		nullInt(e.ActorID), nullStr(e.ActorName), e.Action, e.Entity, nullStr(e.EntityID),
		nullStr(changes), nullStr(e.IP), time.Now()); err != nil {
		// audit must be best-effort
		println("audit.Log error:", err.Error())
	}
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

// ClientIP extracts the request IP, honoring X-Forwarded-For when present.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	return r.RemoteAddr
}
