package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

type authStore struct{ tx pgx.Tx }

func (t *tx) Auth() store.AuthStore { return authStore{t.tx} }

// wrapAuth passes store errors through (a unique violation is ErrDuplicateKey) and wraps anything else.
func wrapAuth(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return fmt.Errorf("postgres: %s: %w", op, mapErr(err))
}

func (a authStore) exec(ctx context.Context, op, query string, args ...any) (int64, error) {
	tag, err := a.tx.Exec(ctx, query, args...)
	if err != nil {
		return 0, wrapAuth(op, err)
	}
	return tag.RowsAffected(), nil
}

// one runs a statement that must change exactly one row.
func (a authStore) one(ctx context.Context, op, query string, args ...any) error {
	n, err := a.exec(ctx, op, query, args...)
	if err == nil && n == 0 {
		return store.ErrNotFound
	}
	return err
}

// insert adds a row; a row that violates any unique constraint is skipped by the statement itself (a unique
// violation would abort the transaction) and reported as ErrDuplicateKey.
func (a authStore) insert(ctx context.Context, op, query string, args ...any) error {
	n, err := a.exec(ctx, op, query+` ON CONFLICT DO NOTHING`, args...)
	if err == nil && n == 0 {
		return store.ErrDuplicateKey
	}
	return err
}

// update changes one row whose unique columns must stay free: free is a NOT EXISTS condition on the statement,
// so a conflict is detected without a violation. Zero rows then mean missing or taken.
func (a authStore) update(ctx context.Context, op, table string, id ids.UUID, query string, args ...any) error {
	n, err := a.exec(ctx, op, query, args...)
	if err != nil || n > 0 {
		return err
	}
	var exists bool
	if err := a.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE id = $1)`, uid(id)).Scan(&exists); err != nil {
		return wrapAuth(op, err)
	}
	if exists {
		return store.ErrDuplicateKey
	}
	return store.ErrNotFound
}

func optTz(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := tz(*t)
	return &v
}

func optUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := utc(*t)
	return &v
}

func jsonOr(b []byte, empty string) string {
	if len(b) == 0 {
		return empty
	}
	return string(b)
}

// listPage returns one page of rows and the total count of the same table and filter. rowsSQL binds args as
// $1..$n.
func listPage[T any](ctx context.Context, a authStore, op, countSQL, rowsSQL string, p store.Page, scan func(pgx.CollectableRow) (T, error), args ...any) ([]T, int, error) {
	p = storeutil.NormalizePage(p)
	var total int
	if err := a.tx.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, wrapAuth(op, err)
	}
	n := len(args)
	rows, err := a.tx.Query(ctx, rowsSQL+` LIMIT $`+strconv.Itoa(n+1)+` OFFSET $`+strconv.Itoa(n+2), append(args, p.Limit, p.Offset)...)
	if err != nil {
		return nil, 0, wrapAuth(op, err)
	}
	out, err := pgx.CollectRows(rows, scan)
	if err != nil {
		return nil, 0, wrapAuth(op, err)
	}
	return out, total, nil
}

// Users

const userColumns = `id, login, email, name, password_hash, disabled, created_at, updated_at, last_login_at`

func scanUser(r pgx.CollectableRow) (store.User, error) {
	var u store.User
	var id [16]byte
	err := r.Scan(&id, &u.Login, &u.Email, &u.Name, &u.PasswordHash, &u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
	u.ID = id
	u.CreatedAt, u.UpdatedAt, u.LastLoginAt = utc(u.CreatedAt), utc(u.UpdatedAt), optUTC(u.LastLoginAt)
	return u, err
}

func (a authStore) CreateUser(ctx context.Context, u store.User) error {
	return a.insert(ctx, "create user", `INSERT INTO app_user (`+userColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uid(u.ID), u.Login, u.Email, u.Name, u.PasswordHash, u.Disabled, tz(u.CreatedAt), tz(u.UpdatedAt), optTz(u.LastLoginAt))
}

func (a authStore) UpdateUser(ctx context.Context, u store.User) error {
	return a.update(ctx, "update user", "app_user", u.ID, `UPDATE app_user SET login = $2, email = $3, name = $4,
			password_hash = $5, disabled = $6, updated_at = $7, last_login_at = $8
		WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM app_user o WHERE o.login = $2 AND o.id <> $1)`,
		uid(u.ID), u.Login, u.Email, u.Name, u.PasswordHash, u.Disabled, tz(u.UpdatedAt), optTz(u.LastLoginAt))
}

func (a authStore) DeleteUser(ctx context.Context, id ids.UUID) error {
	if _, err := a.exec(ctx, "delete user assignments", `DELETE FROM role_assignment WHERE principal_kind = 'user' AND principal_id = $1`, uid(id)); err != nil {
		return err
	}
	return a.one(ctx, "delete user", `DELETE FROM app_user WHERE id = $1`, uid(id))
}

func (a authStore) GetUser(ctx context.Context, id ids.UUID) (store.User, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+userColumns+` FROM app_user WHERE id = $1`, uid(id))
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	return u, wrapAuth("get user", err)
}

func (a authStore) UserByLogin(ctx context.Context, login string) (store.User, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+userColumns+` FROM app_user WHERE login = $1`, login)
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	return u, wrapAuth("user by login", err)
}

func (a authStore) ListUsers(ctx context.Context, p store.Page, search string) ([]store.User, int, error) {
	where, args := "", []any(nil)
	if search != "" {
		where = ` WHERE login ILIKE $1 OR name ILIKE $1 OR email ILIKE $1`
		args = []any{storeutil.LikeContains(search)}
	}
	return listPage(ctx, a, "list users", `SELECT count(*) FROM app_user`+where,
		`SELECT `+userColumns+` FROM app_user`+where+` ORDER BY login`, p, scanUser, args...)
}

// Groups

const groupColumns = `id, name, description, created_at, updated_at`

func scanGroup(r pgx.CollectableRow) (store.Group, error) {
	var g store.Group
	var id [16]byte
	err := r.Scan(&id, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt)
	g.ID = id
	g.CreatedAt, g.UpdatedAt = utc(g.CreatedAt), utc(g.UpdatedAt)
	return g, err
}

func (a authStore) CreateGroup(ctx context.Context, g store.Group) error {
	return a.insert(ctx, "create group", `INSERT INTO user_group (`+groupColumns+`) VALUES ($1, $2, $3, $4, $5)`,
		uid(g.ID), g.Name, g.Description, tz(g.CreatedAt), tz(g.UpdatedAt))
}

func (a authStore) UpdateGroup(ctx context.Context, g store.Group) error {
	return a.update(ctx, "update group", "user_group", g.ID, `UPDATE user_group SET name = $2, description = $3, updated_at = $4
		WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM user_group o WHERE o.name = $2 AND o.id <> $1)`,
		uid(g.ID), g.Name, g.Description, tz(g.UpdatedAt))
}

func (a authStore) DeleteGroup(ctx context.Context, id ids.UUID) error {
	if _, err := a.exec(ctx, "delete group assignments", `DELETE FROM role_assignment WHERE principal_kind = 'group' AND principal_id = $1`, uid(id)); err != nil {
		return err
	}
	return a.one(ctx, "delete group", `DELETE FROM user_group WHERE id = $1`, uid(id))
}

func (a authStore) GetGroup(ctx context.Context, id ids.UUID) (store.Group, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+groupColumns+` FROM user_group WHERE id = $1`, uid(id))
	g, err := pgx.CollectExactlyOneRow(rows, scanGroup)
	return g, wrapAuth("get group", err)
}

func (a authStore) ListGroups(ctx context.Context, p store.Page) ([]store.Group, int, error) {
	return listPage(ctx, a, "list groups", `SELECT count(*) FROM user_group`,
		`SELECT `+groupColumns+` FROM user_group ORDER BY name`, p, scanGroup)
}

func (a authStore) AddMember(ctx context.Context, group, user ids.UUID) error {
	_, err := a.exec(ctx, "add member", `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		uid(group), uid(user))
	return err
}

func (a authStore) RemoveMember(ctx context.Context, group, user ids.UUID) error {
	_, err := a.exec(ctx, "remove member", `DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, uid(group), uid(user))
	return err
}

func (a authStore) GroupsOf(ctx context.Context, user ids.UUID) ([]ids.UUID, error) {
	rows, err := a.tx.Query(ctx, `SELECT group_id FROM group_member WHERE user_id = $1 ORDER BY group_id`, uid(user))
	if err != nil {
		return nil, wrapAuth("groups of", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ids.UUID, error) {
		var id [16]byte
		err := r.Scan(&id)
		return id, err
	})
	return out, wrapAuth("groups of", err)
}

func (a authStore) Members(ctx context.Context, group ids.UUID, p store.Page) ([]store.User, int, error) {
	cols := make([]string, 0, 9)
	for _, c := range strings.Split(userColumns, ", ") {
		cols = append(cols, "u."+c)
	}
	return listPage(ctx, a, "members", `SELECT count(*) FROM group_member WHERE group_id = $1`,
		`SELECT `+strings.Join(cols, ", ")+` FROM group_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.group_id = $1 ORDER BY u.login`, p, scanUser, uid(group))
}

// Roles

const roleColumns = `id, name, description, policies, template, created_at, updated_at`

func scanRole(r pgx.CollectableRow) (store.Role, error) {
	var ro store.Role
	var id [16]byte
	var policies string
	err := r.Scan(&id, &ro.Name, &ro.Description, &policies, &ro.Template, &ro.CreatedAt, &ro.UpdatedAt)
	ro.ID = id
	ro.Policies = []byte(policies)
	ro.CreatedAt, ro.UpdatedAt = utc(ro.CreatedAt), utc(ro.UpdatedAt)
	return ro, err
}

func (a authStore) CreateRole(ctx context.Context, r store.Role) error {
	return a.insert(ctx, "create role", `INSERT INTO role (`+roleColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uid(r.ID), r.Name, r.Description, jsonOr(r.Policies, "[]"), r.Template, tz(r.CreatedAt), tz(r.UpdatedAt))
}

func (a authStore) UpdateRole(ctx context.Context, r store.Role) error {
	return a.update(ctx, "update role", "role", r.ID, `UPDATE role SET name = $2, description = $3, policies = $4,
			template = $5, updated_at = $6
		WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM role o WHERE o.name = $2 AND o.id <> $1)`,
		uid(r.ID), r.Name, r.Description, jsonOr(r.Policies, "[]"), r.Template, tz(r.UpdatedAt))
}

func (a authStore) DeleteRole(ctx context.Context, id ids.UUID) error {
	return a.one(ctx, "delete role", `DELETE FROM role WHERE id = $1`, uid(id))
}

func (a authStore) GetRole(ctx context.Context, id ids.UUID) (store.Role, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+roleColumns+` FROM role WHERE id = $1`, uid(id))
	r, err := pgx.CollectExactlyOneRow(rows, scanRole)
	return r, wrapAuth("get role", err)
}

func (a authStore) RoleByName(ctx context.Context, name string) (store.Role, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+roleColumns+` FROM role WHERE name = $1`, name)
	r, err := pgx.CollectExactlyOneRow(rows, scanRole)
	return r, wrapAuth("role by name", err)
}

func (a authStore) ListRoles(ctx context.Context, p store.Page) ([]store.Role, int, error) {
	return listPage(ctx, a, "list roles", `SELECT count(*) FROM role`,
		`SELECT `+roleColumns+` FROM role ORDER BY name`, p, scanRole)
}

// Assignments

const assignmentColumns = `id, role_id, principal_kind, principal_id, limitations, created_at`

func fillAssignment(as *store.Assignment, id, role, pid [16]byte, kind, lim string) {
	as.ID, as.RoleID = id, role
	as.Principal = store.PrincipalRef{Kind: store.PrincipalKind(kind), ID: pid}
	as.Limitations = []byte(lim)
	as.CreatedAt = utc(as.CreatedAt)
}

func scanAssignment(r pgx.CollectableRow) (store.Assignment, error) {
	var as store.Assignment
	var id, role, pid [16]byte
	var kind, lim string
	err := r.Scan(&id, &role, &kind, &pid, &lim, &as.CreatedAt)
	fillAssignment(&as, id, role, pid, kind, lim)
	return as, err
}

func (a authStore) CreateAssignment(ctx context.Context, as store.Assignment) error {
	return a.insert(ctx, "create assignment", `INSERT INTO role_assignment (`+assignmentColumns+`) VALUES ($1, $2, $3, $4, $5, $6)`,
		uid(as.ID), uid(as.RoleID), string(as.Principal.Kind), uid(as.Principal.ID), jsonOr(as.Limitations, "{}"), tz(as.CreatedAt))
}

func (a authStore) DeleteAssignment(ctx context.Context, id ids.UUID) error {
	return a.one(ctx, "delete assignment", `DELETE FROM role_assignment WHERE id = $1`, uid(id))
}

func (a authStore) GetAssignment(ctx context.Context, id ids.UUID) (store.Assignment, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+assignmentColumns+` FROM role_assignment WHERE id = $1`, uid(id))
	as, err := pgx.CollectExactlyOneRow(rows, scanAssignment)
	return as, wrapAuth("get assignment", err)
}

func (a authStore) RoleAssignments(ctx context.Context, role ids.UUID) ([]store.Assignment, error) {
	rows, err := a.tx.Query(ctx, `SELECT `+assignmentColumns+` FROM role_assignment WHERE role_id = $1 ORDER BY id`, uid(role))
	if err != nil {
		return nil, wrapAuth("role assignments", err)
	}
	out, err := pgx.CollectRows(rows, scanAssignment)
	return out, wrapAuth("role assignments", err)
}

func (a authStore) AssignmentsFor(ctx context.Context, principals []store.PrincipalRef) ([]store.RoleGrant, error) {
	if len(principals) == 0 {
		return nil, nil
	}
	kinds := make([]string, len(principals))
	pids := make([][16]byte, len(principals))
	for i, p := range principals {
		kinds[i], pids[i] = string(p.Kind), uid(p.ID)
	}
	rows, err := a.tx.Query(ctx, `SELECT a.id, a.role_id, a.principal_kind, a.principal_id, a.limitations, a.created_at,
			r.name, r.description, r.policies, r.template, r.created_at, r.updated_at
		FROM role_assignment a JOIN role r ON r.id = a.role_id
		WHERE (a.principal_kind, a.principal_id) IN (SELECT * FROM unnest($1::text[], $2::uuid[]))
		ORDER BY r.name, a.id`, kinds, pids)
	if err != nil {
		return nil, wrapAuth("assignments for", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.RoleGrant, error) {
		var g store.RoleGrant
		var id, role, pid [16]byte
		var kind, lim, policies string
		err := r.Scan(&id, &role, &kind, &pid, &lim, &g.Assignment.CreatedAt,
			&g.Role.Name, &g.Role.Description, &policies, &g.Role.Template, &g.Role.CreatedAt, &g.Role.UpdatedAt)
		fillAssignment(&g.Assignment, id, role, pid, kind, lim)
		g.Role.ID, g.Role.Policies = role, []byte(policies)
		g.Role.CreatedAt, g.Role.UpdatedAt = utc(g.Role.CreatedAt), utc(g.Role.UpdatedAt)
		return g, err
	})
	return out, wrapAuth("assignments for", err)
}

// Tokens

const tokenColumns = `id, name, kind, secret_hash, prefix, created_by, created_at, expires_at, last_used_at, revoked_at`

func scanToken(r pgx.CollectableRow) (store.Token, error) {
	var t store.Token
	var id [16]byte
	err := r.Scan(&id, &t.Name, &t.Kind, &t.SecretHash, &t.Prefix, &t.CreatedBy, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt)
	t.ID = id
	t.CreatedAt, t.ExpiresAt, t.LastUsedAt, t.RevokedAt = utc(t.CreatedAt), optUTC(t.ExpiresAt), optUTC(t.LastUsedAt), optUTC(t.RevokedAt)
	return t, err
}

func (a authStore) CreateToken(ctx context.Context, t store.Token) error {
	return a.insert(ctx, "create token", `INSERT INTO api_token (`+tokenColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		uid(t.ID), t.Name, t.Kind, t.SecretHash, t.Prefix, t.CreatedBy, tz(t.CreatedAt), optTz(t.ExpiresAt), optTz(t.LastUsedAt), optTz(t.RevokedAt))
}

func (a authStore) DeleteToken(ctx context.Context, id ids.UUID) error {
	if _, err := a.exec(ctx, "delete token assignments", `DELETE FROM role_assignment WHERE principal_kind = 'token' AND principal_id = $1`, uid(id)); err != nil {
		return err
	}
	return a.one(ctx, "delete token", `DELETE FROM api_token WHERE id = $1`, uid(id))
}

func (a authStore) RevokeToken(ctx context.Context, id ids.UUID, at time.Time) error {
	return a.one(ctx, "revoke token", `UPDATE api_token SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, uid(id), tz(at))
}

func (a authStore) GetToken(ctx context.Context, id ids.UUID) (store.Token, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+tokenColumns+` FROM api_token WHERE id = $1`, uid(id))
	t, err := pgx.CollectExactlyOneRow(rows, scanToken)
	return t, wrapAuth("get token", err)
}

func (a authStore) TokenByHash(ctx context.Context, hash []byte) (store.Token, error) {
	rows, _ := a.tx.Query(ctx, `SELECT `+tokenColumns+` FROM api_token WHERE secret_hash = $1`, hash)
	t, err := pgx.CollectExactlyOneRow(rows, scanToken)
	return t, wrapAuth("token by hash", err)
}

func (a authStore) ListTokens(ctx context.Context, p store.Page) ([]store.Token, int, error) {
	return listPage(ctx, a, "list tokens", `SELECT count(*) FROM api_token`,
		`SELECT `+tokenColumns+` FROM api_token ORDER BY created_at DESC, id DESC`, p, scanToken)
}

func (a authStore) TouchToken(ctx context.Context, id ids.UUID, at time.Time) error {
	return a.one(ctx, "touch token", `UPDATE api_token SET last_used_at = $2 WHERE id = $1`, uid(id), tz(at))
}

// Sessions

func (a authStore) CreateSession(ctx context.Context, s store.Session) error {
	return a.insert(ctx, "create session", `INSERT INTO session (id_hash, user_id, csrf, created_at, last_seen_at, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.IDHash, uid(s.UserID), s.CSRF, tz(s.CreatedAt), tz(s.LastSeenAt), tz(s.ExpiresAt), s.IP, s.UserAgent)
}

func (a authStore) SessionByHash(ctx context.Context, hash []byte) (store.Session, error) {
	rows, _ := a.tx.Query(ctx, `SELECT id_hash, user_id, csrf, created_at, last_seen_at, expires_at, ip, user_agent
		FROM session WHERE id_hash = $1`, hash)
	s, err := pgx.CollectExactlyOneRow(rows, func(r pgx.CollectableRow) (store.Session, error) {
		var s store.Session
		var user [16]byte
		err := r.Scan(&s.IDHash, &user, &s.CSRF, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.IP, &s.UserAgent)
		s.UserID = user
		s.CreatedAt, s.LastSeenAt, s.ExpiresAt = utc(s.CreatedAt), utc(s.LastSeenAt), utc(s.ExpiresAt)
		return s, err
	})
	return s, wrapAuth("session by hash", err)
}

func (a authStore) SessionUserByHash(ctx context.Context, hash []byte) (store.SessionUser, error) {
	rows, _ := a.tx.Query(ctx, `SELECT s.id_hash, s.user_id, s.csrf, s.created_at, s.last_seen_at, s.expires_at, s.ip, s.user_agent,
			u.id, u.login, u.email, u.name, u.password_hash, u.disabled, u.created_at, u.updated_at, u.last_login_at,
			ARRAY(SELECT group_id FROM group_member WHERE user_id = u.id ORDER BY group_id)
		FROM session s JOIN app_user u ON u.id = s.user_id WHERE s.id_hash = $1`, hash)
	su, err := pgx.CollectExactlyOneRow(rows, func(r pgx.CollectableRow) (store.SessionUser, error) {
		var su store.SessionUser
		var sUser, uID [16]byte
		var groups [][16]byte
		s, u := &su.Session, &su.User
		err := r.Scan(&s.IDHash, &sUser, &s.CSRF, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.IP, &s.UserAgent,
			&uID, &u.Login, &u.Email, &u.Name, &u.PasswordHash, &u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &groups)
		s.UserID, u.ID = sUser, uID
		s.CreatedAt, s.LastSeenAt, s.ExpiresAt = utc(s.CreatedAt), utc(s.LastSeenAt), utc(s.ExpiresAt)
		u.CreatedAt, u.UpdatedAt, u.LastLoginAt = utc(u.CreatedAt), utc(u.UpdatedAt), optUTC(u.LastLoginAt)
		for _, g := range groups {
			su.Groups = append(su.Groups, g)
		}
		return su, err
	})
	return su, wrapAuth("session user by hash", err)
}

func (a authStore) TouchSession(ctx context.Context, hash []byte, seen, expires time.Time) error {
	return a.one(ctx, "touch session", `UPDATE session SET last_seen_at = $2, expires_at = $3 WHERE id_hash = $1`, hash, tz(seen), tz(expires))
}

func (a authStore) DeleteSession(ctx context.Context, hash []byte) error {
	_, err := a.exec(ctx, "delete session", `DELETE FROM session WHERE id_hash = $1`, hash)
	return err
}

func (a authStore) DeleteUserSessions(ctx context.Context, user ids.UUID) error {
	_, err := a.exec(ctx, "delete user sessions", `DELETE FROM session WHERE user_id = $1`, uid(user))
	return err
}

func (a authStore) DeleteOtherSessions(ctx context.Context, user ids.UUID, keep []byte) error {
	_, err := a.exec(ctx, "delete other sessions", `DELETE FROM session WHERE user_id = $1 AND id_hash <> $2`, uid(user), keep)
	return err
}

func (a authStore) DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	n, err := a.exec(ctx, "delete expired sessions", `DELETE FROM session WHERE expires_at <= $1`, tz(now))
	return int(n), err
}

// Meta

func (a authStore) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := a.tx.QueryRow(ctx, `SELECT value FROM auth_meta WHERE key = $1`, key).Scan(&v)
	return v, wrapAuth("meta", err)
}

func (a authStore) SetMeta(ctx context.Context, key, value string) error {
	_, err := a.exec(ctx, "set meta", `INSERT INTO auth_meta (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
