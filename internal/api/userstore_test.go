package api

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/snoozeweb/snooze/internal/auth"
	"github.com/snoozeweb/snooze/internal/condition"
	"github.com/snoozeweb/snooze/internal/db"
)

// userStoreDB is a minimal stateful db.Driver stub backing the `user`
// collection in memory. It exists so the OIDC JIT-provisioning path and the
// refresh enabled-recheck can be exercised without a real backend. Every
// non-"user" collection (e.g. "tenant", "role") falls through to fakeDB's
// empty behaviour, so checkTenantStatus reads "active" and role resolution
// yields no group-mapped roles.
type userStoreDB struct {
	fakeDB
	mu      sync.Mutex
	users   []db.Document
	nextUID int
}

// seedUser inserts a pre-existing user row (assigning a uid if absent).
func (s *userStoreDB) seedUser(doc db.Document) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := cloneDoc(doc)
	if _, ok := d["uid"].(string); !ok {
		s.nextUID++
		d["uid"] = fmt.Sprintf("u%d", s.nextUID)
	}
	s.users = append(s.users, d)
}

// find returns the stored doc matching (name, method), or nil. Caller holds mu.
func (s *userStoreDB) find(name, method string) db.Document {
	for _, u := range s.users {
		un, _ := u["name"].(string)
		um, _ := u["method"].(string)
		if un == name && um == method {
			return u
		}
	}
	return nil
}

func (s *userStoreDB) GetOne(_ context.Context, coll string, filter db.Document) (db.Document, error) {
	if coll != auth.LocalCollection {
		return nil, errors.New("not found")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name, _ := filter["name"].(string)
	method, _ := filter["method"].(string)
	if u := s.find(name, method); u != nil {
		return cloneDoc(u), nil
	}
	return nil, errors.New("not found")
}

func (s *userStoreDB) Write(ctx context.Context, coll string, docs []db.Document, _ db.WriteOptions) (db.WriteResult, error) {
	if coll != auth.LocalCollection {
		return db.WriteResult{}, nil
	}
	// Mirror the real drivers: stamp tenant_id from the context on a scoped write.
	tid, _ := auth.TenantFrom(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	var added []string
	for _, d := range docs {
		nd := cloneDoc(d)
		nd["tenant_id"] = tid
		s.nextUID++
		uid := fmt.Sprintf("u%d", s.nextUID)
		nd["uid"] = uid
		s.users = append(s.users, nd)
		added = append(added, uid)
	}
	return db.WriteResult{Added: added}, nil
}

func (s *userStoreDB) UpdateOne(_ context.Context, coll, uid string, patch db.Document, _ bool) error {
	if coll != auth.LocalCollection {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if id, _ := u["uid"].(string); id == uid {
			for k, v := range patch {
				u[k] = v
			}
			return nil
		}
	}
	return nil
}

// Search returns no rows: role resolution finds no group-mapped roles, which
// is fine for these tests (they assert provisioning + token issuance, not RBAC).
func (s *userStoreDB) Search(_ context.Context, _ string, _ condition.Cond, _ db.Page) ([]db.Document, int, error) {
	return nil, 0, nil
}

func cloneDoc(d db.Document) db.Document {
	out := make(db.Document, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}
