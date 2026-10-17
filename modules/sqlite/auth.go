package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

type authStore struct{ tx *sql.Tx }

func (t *tx) Auth() store.AuthStore { return authStore{t.tx} }

func (a authStore) exec(ctx context.Context, op, query string, args ...any) (int64, error) {
	res, err := a.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, wrapAuth(op, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// one runs a statement that must change exactly one row.
func (a authStore) one(ctx context.Context, op, query string, args ...any) error {
	n, err := a.exec(ctx, op, query, args...)
	if err == nil && n == 0 {
		return store.ErrNotFound
	}
	return err
}

// wrapAuth passes store errors through (a unique violation is ErrDuplicateKey) and wraps anything else.
func wrapAuth(op string, err error) error {
	if err == nil {
		return nil
	}
	err = mapErr(mapNoRows(err))
	if errors.Is(err, store.ErrNotFound) {
		return err
	}
	return fmt.Errorf("sqlite: %s: %w", op, err)
}

func optMs(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ms(*t)
}

func fromOptMs(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMs(v.Int64)
	return &t
}

func jsonOr(b []byte, empty string) string {
	if len(b) == 0 {
		return empty
	}
	return string(b)
}

// list runs a page of rows plus the total count of the same table and filter.
func (a authStore) list(ctx context.Context, op, countSQL, rowsSQL string, p store.Page, args []any, each func(scanner) error) (int, error) {
	p = storeutil.NormalizePage(p)
	var total int
	if err := a.tx.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return 0, wrapAuth(op, err)
	}
	rows, err := a.tx.QueryContext(ctx, rowsSQL+` LIMIT ? OFFSET ?`, append(slices.Clip(args), p.Limit, p.Offset)...)
	if err != nil {
		return 0, wrapAuth(op, err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := each(rows); err != nil {
			return 0, wrapAuth(op, err)
		}
	}
	return total, wrapAuth(op, rows.Err())
}

// Users

const userColumns = `id, login, email, name, password_hash, disabled, created_at, updated_at, last_login_at`

func scanUser(r scanner) (store.User, error) {
	var (
		u            store.User
		id           []byte
		created, upd int64
		lastLogin    sql.NullInt64
	)
	if err := r.Scan(&id, &u.Login, &u.Email, &u.Name, &u.PasswordHash, &u.Disabled, &created, &upd, &lastLogin); err != nil {
		return u, err
	}
	var err error
	if u.ID, err = toUUID(id); err != nil {
		return u, err
	}
	u.CreatedAt, u.UpdatedAt, u.LastLoginAt = fromMs(created), fromMs(upd), fromOptMs(lastLogin)
	return u, nil
}

func (a authStore) CreateUser(ctx context.Context, u store.User) error {
	_, err := a.exec(ctx, "create user", `INSERT INTO app_user (`+userColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid(u.ID), u.Login, u.Email, u.Name, u.PasswordHash, u.Disabled, ms(u.CreatedAt), ms(u.UpdatedAt), optMs(u.LastLoginAt))
	return err
}

func (a authStore) UpdateUser(ctx context.Context, u store.User) error {
	return a.one(ctx, "update user", `UPDATE app_user SET login = ?, email = ?, name = ?, password_hash = ?, disabled = ?,
			updated_at = ?, last_login_at = ? WHERE id = ?`,
		u.Login, u.Email, u.Name, u.PasswordHash, u.Disabled, ms(u.UpdatedAt), optMs(u.LastLoginAt), uid(u.ID))
}

func (a authStore) DeleteUser(ctx context.Context, id ids.UUID) error {
	if _, err := a.exec(ctx, "delete user assignments", `DELETE FROM role_assignment WHERE principal_kind = 'user' AND principal_id = ?`, uid(id)); err != nil {
		return err
	}
	return a.one(ctx, "delete user", `DELETE FROM app_user WHERE id = ?`, uid(id))
}

func (a authStore) GetUser(ctx context.Context, id ids.UUID) (store.User, error) {
	u, err := scanUser(a.tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM app_user WHERE id = ?`, uid(id)))
	if err != nil {
		return u, wrapAuth("get user", err)
	}
	return u, nil
}

func (a authStore) UserByLogin(ctx context.Context, login string) (store.User, error) {
	u, err := scanUser(a.tx.QueryRowContext(ctx, `SELECT `+userColumns+` FROM app_user WHERE login = ?`, login))
	if err != nil {
		return u, wrapAuth("user by login", err)
	}
	return u, nil
}

func (a authStore) ListUsers(ctx context.Context, p store.Page, search string) ([]store.User, int, error) {
	var out []store.User
	where, args := "", []any(nil)
	if search != "" {
		pat := storeutil.LikeContains(search)
		where = ` WHERE login LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR email LIKE ? ESCAPE '\'`
		args = []any{pat, pat, pat}
	}
	total, err := a.list(ctx, "list users", `SELECT count(*) FROM app_user`+where,
		`SELECT `+userColumns+` FROM app_user`+where+` ORDER BY login`, p, args, func(r scanner) error {
			u, err := scanUser(r)
			out = append(out, u)
			return err
		})
	return out, total, err
}

// Groups

const groupColumns = `id, name, description, created_at, updated_at`

func scanGroup(r scanner) (store.Group, error) {
	var (
		g            store.Group
		id           []byte
		created, upd int64
	)
	if err := r.Scan(&id, &g.Name, &g.Description, &created, &upd); err != nil {
		return g, err
	}
	var err error
	if g.ID, err = toUUID(id); err != nil {
		return g, err
	}
	g.CreatedAt, g.UpdatedAt = fromMs(created), fromMs(upd)
	return g, nil
}

func (a authStore) CreateGroup(ctx context.Context, g store.Group) error {
	_, err := a.exec(ctx, "create group", `INSERT INTO user_group (`+groupColumns+`) VALUES (?, ?, ?, ?, ?)`,
		uid(g.ID), g.Name, g.Description, ms(g.CreatedAt), ms(g.UpdatedAt))
	return err
}

func (a authStore) UpdateGroup(ctx context.Context, g store.Group) error {
	return a.one(ctx, "update group", `UPDATE user_group SET name = ?, description = ?, updated_at = ? WHERE id = ?`,
		g.Name, g.Description, ms(g.UpdatedAt), uid(g.ID))
}

func (a authStore) DeleteGroup(ctx context.Context, id ids.UUID) error {
	if _, err := a.exec(ctx, "delete group assignments", `DELETE FROM role_assignment WHERE principal_kind = 'group' AND principal_id = ?`, uid(id)); err != nil {
		return err
	}
	return a.one(ctx, "delete group", `DELETE FROM user_group WHERE id = ?`, uid(id))
}

func (a authStore) GetGroup(ctx context.Context, id ids.UUID) (store.Group, error) {
	g, err := scanGroup(a.tx.QueryRowContext(ctx, `SELECT `+groupColumns+` FROM user_group WHERE id = ?`, uid(id)))
	if err != nil {
		return g, wrapAuth("get group", err)
	}
	return g, nil
}

func (a authStore) ListGroups(ctx context.Context, p store.Page) ([]store.Group, int, error) {
	var out []store.Group
	total, err := a.list(ctx, "list groups", `SELECT count(*) FROM user_group`,
		`SELECT `+groupColumns+` FROM user_group ORDER BY name`, p, nil, func(r scanner) error {
			g, err := scanGroup(r)
			out = append(out, g)
			return err
		})
	return out, total, err
}

func (a authStore) AddMember(ctx context.Context, group, user ids.UUID) error {
	_, err := a.exec(ctx, "add member", `INSERT INTO group_member (group_id, user_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
		uid(group), uid(user))
	return err
}

func (a authStore) RemoveMember(ctx context.Context, group, user ids.UUID) error {
	_, err := a.exec(ctx, "remove member", `DELETE FROM group_member WHERE group_id = ? AND user_id = ?`, uid(group), uid(user))
	return err
}

func (a authStore) GroupsOf(ctx context.Context, user ids.UUID) ([]ids.UUID, error) {
	rows, err := a.tx.QueryContext(ctx, `SELECT group_id FROM group_member WHERE user_id = ? ORDER BY group_id`, uid(user))
	if err != nil {
		return nil, wrapAuth("groups of", err)
	}
	defer rows.Close()
	var out []ids.UUID
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, wrapAuth("groups of", err)
		}
		g, err := toUUID(b)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, wrapAuth("groups of", rows.Err())
}

func (a authStore) Members(ctx context.Context, group ids.UUID, p store.Page) ([]store.User, int, error) {
	var out []store.User
	cols := make([]string, 0, 9)
	for _, c := range strings.Split(userColumns, ", ") {
		cols = append(cols, "u."+c)
	}
	total, err := a.list(ctx, "members", `SELECT count(*) FROM group_member WHERE group_id = ?`,
		`SELECT `+strings.Join(cols, ", ")+` FROM group_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.group_id = ? ORDER BY u.login`, p, []any{uid(group)}, func(r scanner) error {
			u, err := scanUser(r)
			out = append(out, u)
			return err
		})
	return out, total, err
}

// Roles

const roleColumns = `id, name, description, policies, template, created_at, updated_at`

func scanRole(r scanner) (store.Role, error) {
	var (
		ro           store.Role
		id           []byte
		policies     string
		created, upd int64
	)
	if err := r.Scan(&id, &ro.Name, &ro.Description, &policies, &ro.Template, &created, &upd); err != nil {
		return ro, err
	}
	var err error
	if ro.ID, err = toUUID(id); err != nil {
		return ro, err
	}
	ro.Policies = []byte(policies)
	ro.CreatedAt, ro.UpdatedAt = fromMs(created), fromMs(upd)
	return ro, nil
}

func (a authStore) CreateRole(ctx context.Context, r store.Role) error {
	_, err := a.exec(ctx, "create role", `INSERT INTO role (`+roleColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uid(r.ID), r.Name, r.Description, jsonOr(r.Policies, "[]"), r.Template, ms(r.CreatedAt), ms(r.UpdatedAt))
	return err
}

func (a authStore) UpdateRole(ctx context.Context, r store.Role) error {
	return a.one(ctx, "update role", `UPDATE role SET name = ?, description = ?, policies = ?, template = ?, updated_at = ?
		WHERE id = ?`, r.Name, r.Description, jsonOr(r.Policies, "[]"), r.Template, ms(r.UpdatedAt), uid(r.ID))
}

func (a authStore) DeleteRole(ctx context.Context, id ids.UUID) error {
	return a.one(ctx, "delete role", `DELETE FROM role WHERE id = ?`, uid(id))
}

func (a authStore) GetRole(ctx context.Context, id ids.UUID) (store.Role, error) {
	r, err := scanRole(a.tx.QueryRowContext(ctx, `SELECT `+roleColumns+` FROM role WHERE id = ?`, uid(id)))
	if err != nil {
		return r, wrapAuth("get role", err)
	}
	return r, nil
}

func (a authStore) RoleByName(ctx context.Context, name string) (store.Role, error) {
	r, err := scanRole(a.tx.QueryRowContext(ctx, `SELECT `+roleColumns+` FROM role WHERE name = ?`, name))
	if err != nil {
		return r, wrapAuth("role by name", err)
	}
	return r, nil
}

func (a authStore) ListRoles(ctx context.Context, p store.Page) ([]store.Role, int, error) {
	var out []store.Role
	total, err := a.list(ctx, "list roles", `SELECT count(*) FROM role`,
		`SELECT `+roleColumns+` FROM role ORDER BY name`, p, nil, func(r scanner) error {
			ro, err := scanRole(r)
			out = append(out, ro)
			return err
		})
	return out, total, err
}

// Assignments

const assignmentColumns = `id, role_id, principal_kind, principal_id, limitations, created_at`

func scanAssignment(r scanner, extra ...any) (store.Assignment, error) {
	var (
		as            store.Assignment
		id, role, pid []byte
		kind, lim     string
		created       int64
	)
	if err := r.Scan(append([]any{&id, &role, &kind, &pid, &lim, &created}, extra...)...); err != nil {
		return as, err
	}
	var err error
	if as.ID, err = toUUID(id); err != nil {
		return as, err
	}
	if as.RoleID, err = toUUID(role); err != nil {
		return as, err
	}
	if as.Principal.ID, err = toUUID(pid); err != nil {
		return as, err
	}
	as.Principal.Kind = store.PrincipalKind(kind)
	as.Limitations = []byte(lim)
	as.CreatedAt = fromMs(created)
	return as, nil
}

func (a authStore) CreateAssignment(ctx context.Context, as store.Assignment) error {
	_, err := a.exec(ctx, "create assignment", `INSERT INTO role_assignment (`+assignmentColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
		uid(as.ID), uid(as.RoleID), string(as.Principal.Kind), uid(as.Principal.ID), jsonText(as.Limitations), ms(as.CreatedAt))
	return err
}

func (a authStore) DeleteAssignment(ctx context.Context, id ids.UUID) error {
	return a.one(ctx, "delete assignment", `DELETE FROM role_assignment WHERE id = ?`, uid(id))
}

func (a authStore) GetAssignment(ctx context.Context, id ids.UUID) (store.Assignment, error) {
	as, err := scanAssignment(a.tx.QueryRowContext(ctx, `SELECT `+assignmentColumns+` FROM role_assignment WHERE id = ?`, uid(id)))
	if err != nil {
		return as, wrapAuth("get assignment", err)
	}
	return as, nil
}

func (a authStore) RoleAssignments(ctx context.Context, role ids.UUID) ([]store.Assignment, error) {
	rows, err := a.tx.QueryContext(ctx, `SELECT `+assignmentColumns+` FROM role_assignment WHERE role_id = ? ORDER BY id`, uid(role))
	if err != nil {
		return nil, wrapAuth("role assignments", err)
	}
	defer rows.Close()
	var out []store.Assignment
	for rows.Next() {
		as, err := scanAssignment(rows)
		if err != nil {
			return nil, wrapAuth("role assignments", err)
		}
		out = append(out, as)
	}
	return out, wrapAuth("role assignments", rows.Err())
}

func (a authStore) AssignmentsFor(ctx context.Context, principals []store.PrincipalRef) ([]store.RoleGrant, error) {
	if len(principals) == 0 {
		return nil, nil
	}
	byKind := map[store.PrincipalKind][]any{}
	for _, p := range principals {
		byKind[p.Kind] = append(byKind[p.Kind], uid(p.ID))
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, string(k))
	}
	slices.Sort(kinds)
	var (
		conds []string
		args  []any
	)
	for _, k := range kinds {
		list := byKind[store.PrincipalKind(k)]
		conds = append(conds, `(a.principal_kind = ? AND a.principal_id IN (`+placeholders(len(list))+`))`)
		args = append(append(args, k), list...)
	}
	cols := make([]string, 0, 6)
	for _, c := range strings.Split(assignmentColumns, ", ") {
		cols = append(cols, "a."+c)
	}
	rcols := make([]string, 0, 7)
	for _, c := range strings.Split(roleColumns, ", ") {
		rcols = append(rcols, "r."+c)
	}
	rows, err := a.tx.QueryContext(ctx, `SELECT `+strings.Join(cols, ", ")+`, `+strings.Join(rcols, ", ")+`
		FROM role_assignment a JOIN role r ON r.id = a.role_id
		WHERE `+strings.Join(conds, " OR ")+` ORDER BY r.name, a.id`, args...)
	if err != nil {
		return nil, wrapAuth("assignments for", err)
	}
	defer rows.Close()
	var out []store.RoleGrant
	for rows.Next() {
		var (
			g            store.RoleGrant
			rid          []byte
			policies     string
			created, upd int64
		)
		g.Assignment, err = scanAssignment(rows, &rid, &g.Role.Name, &g.Role.Description, &policies, &g.Role.Template, &created, &upd)
		if err != nil {
			return nil, wrapAuth("assignments for", err)
		}
		g.Role.ID = g.Assignment.RoleID
		g.Role.Policies = []byte(policies)
		g.Role.CreatedAt, g.Role.UpdatedAt = fromMs(created), fromMs(upd)
		out = append(out, g)
	}
	return out, wrapAuth("assignments for", rows.Err())
}

// Tokens

const tokenColumns = `id, name, kind, secret_hash, prefix, created_by, created_at, expires_at, last_used_at, revoked_at`

func scanToken(r scanner) (store.Token, error) {
	var (
		t                 store.Token
		id                []byte
		created           int64
		expires, used, rv sql.NullInt64
	)
	if err := r.Scan(&id, &t.Name, &t.Kind, &t.SecretHash, &t.Prefix, &t.CreatedBy, &created, &expires, &used, &rv); err != nil {
		return t, err
	}
	var err error
	if t.ID, err = toUUID(id); err != nil {
		return t, err
	}
	t.SecretHash = slices.Clone(t.SecretHash)
	t.CreatedAt, t.ExpiresAt, t.LastUsedAt, t.RevokedAt = fromMs(created), fromOptMs(expires), fromOptMs(used), fromOptMs(rv)
	return t, nil
}

func (a authStore) CreateToken(ctx context.Context, t store.Token) error {
	_, err := a.exec(ctx, "create token", `INSERT INTO api_token (`+tokenColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid(t.ID), t.Name, t.Kind, t.SecretHash, t.Prefix, t.CreatedBy, ms(t.CreatedAt), optMs(t.ExpiresAt), optMs(t.LastUsedAt), optMs(t.RevokedAt))
	return err
}

func (a authStore) DeleteToken(ctx context.Context, id ids.UUID) error {
	if _, err := a.exec(ctx, "delete token assignments", `DELETE FROM role_assignment WHERE principal_kind = 'token' AND principal_id = ?`, uid(id)); err != nil {
		return err
	}
	return a.one(ctx, "delete token", `DELETE FROM api_token WHERE id = ?`, uid(id))
}

func (a authStore) RevokeToken(ctx context.Context, id ids.UUID, at time.Time) error {
	return a.one(ctx, "revoke token", `UPDATE api_token SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, ms(at), uid(id))
}

func (a authStore) GetToken(ctx context.Context, id ids.UUID) (store.Token, error) {
	t, err := scanToken(a.tx.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM api_token WHERE id = ?`, uid(id)))
	if err != nil {
		return t, wrapAuth("get token", err)
	}
	return t, nil
}

func (a authStore) TokenByHash(ctx context.Context, hash []byte) (store.Token, error) {
	t, err := scanToken(a.tx.QueryRowContext(ctx, `SELECT `+tokenColumns+` FROM api_token WHERE secret_hash = ?`, hash))
	if err != nil {
		return t, wrapAuth("token by hash", err)
	}
	return t, nil
}

func (a authStore) ListTokens(ctx context.Context, p store.Page) ([]store.Token, int, error) {
	var out []store.Token
	total, err := a.list(ctx, "list tokens", `SELECT count(*) FROM api_token`,
		`SELECT `+tokenColumns+` FROM api_token ORDER BY created_at DESC, id DESC`, p, nil, func(r scanner) error {
			t, err := scanToken(r)
			out = append(out, t)
			return err
		})
	return out, total, err
}

func (a authStore) TouchToken(ctx context.Context, id ids.UUID, at time.Time) error {
	return a.one(ctx, "touch token", `UPDATE api_token SET last_used_at = ? WHERE id = ?`, ms(at), uid(id))
}

// Sessions

func (a authStore) CreateSession(ctx context.Context, s store.Session) error {
	_, err := a.exec(ctx, "create session", `INSERT INTO session (id_hash, user_id, csrf, created_at, last_seen_at, expires_at, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.IDHash, uid(s.UserID), s.CSRF, ms(s.CreatedAt), ms(s.LastSeenAt), ms(s.ExpiresAt), s.IP, s.UserAgent)
	return err
}

func (a authStore) SessionByHash(ctx context.Context, hash []byte) (store.Session, error) {
	var (
		s                   store.Session
		user                []byte
		created, seen, expr int64
	)
	err := a.tx.QueryRowContext(ctx, `SELECT id_hash, user_id, csrf, created_at, last_seen_at, expires_at, ip, user_agent
		FROM session WHERE id_hash = ?`, hash).Scan(&s.IDHash, &user, &s.CSRF, &created, &seen, &expr, &s.IP, &s.UserAgent)
	if err != nil {
		return s, wrapAuth("session by hash", err)
	}
	if s.UserID, err = toUUID(user); err != nil {
		return s, err
	}
	s.IDHash = slices.Clone(s.IDHash)
	s.CreatedAt, s.LastSeenAt, s.ExpiresAt = fromMs(created), fromMs(seen), fromMs(expr)
	return s, nil
}

// SessionUserByHash runs three primary-key lookups: measured on SQLite (in process, no round trips), a join
// costs more to prepare than they cost to run.
func (a authStore) SessionUserByHash(ctx context.Context, hash []byte) (store.SessionUser, error) {
	var su store.SessionUser
	var err error
	if su.Session, err = a.SessionByHash(ctx, hash); err != nil {
		return su, err
	}
	if su.User, err = a.GetUser(ctx, su.Session.UserID); err != nil {
		return su, err
	}
	su.Groups, err = a.GroupsOf(ctx, su.Session.UserID)
	return su, err
}

func (a authStore) TouchSession(ctx context.Context, hash []byte, seen, expires time.Time) error {
	return a.one(ctx, "touch session", `UPDATE session SET last_seen_at = ?, expires_at = ? WHERE id_hash = ?`, ms(seen), ms(expires), hash)
}

func (a authStore) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := a.exec(ctx, "delete session", `DELETE FROM session WHERE id_hash = ?`, hash)
	return err
}

func (a authStore) DeleteUserSessions(ctx context.Context, user ids.UUID) error {
	_, err := a.exec(ctx, "delete user sessions", `DELETE FROM session WHERE user_id = ?`, uid(user))
	return err
}

func (a authStore) DeleteOtherSessions(ctx context.Context, user ids.UUID, keep []byte) error {
	_, err := a.exec(ctx, "delete other sessions", `DELETE FROM session WHERE user_id = ? AND id_hash <> ?`, uid(user), keep)
	return err
}

func (a authStore) DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	n, err := a.exec(ctx, "delete expired sessions", `DELETE FROM session WHERE expires_at <= ?`, ms(now))
	return int(n), err
}

// Meta

func (a authStore) Meta(ctx context.Context, key string) (string, error) {
	var v string
	if err := a.tx.QueryRowContext(ctx, `SELECT value FROM auth_meta WHERE key = ?`, key).Scan(&v); err != nil {
		return "", wrapAuth("meta", err)
	}
	return v, nil
}

func (a authStore) SetMeta(ctx context.Context, key, value string) error {
	_, err := a.exec(ctx, "set meta", `INSERT INTO auth_meta (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
