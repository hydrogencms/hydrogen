package store

import (
	"context"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
)

// AuthStore holds users, groups, roles and their assignments, API tokens and sessions. Unique violations (login,
// group name, role name, token hash) return ErrDuplicateKey; lookups that match nothing return ErrNotFound.
// Contract: planning/design/auth-h5a.md.
type AuthStore interface {
	CreateUser(ctx context.Context, u User) error
	UpdateUser(ctx context.Context, u User) error // every mutable column
	// DeleteUser removes the user, their memberships, sessions and role assignments.
	DeleteUser(ctx context.Context, id ids.UUID) error
	GetUser(ctx context.Context, id ids.UUID) (User, error)
	UserByLogin(ctx context.Context, login string) (User, error)
	// ListUsers lists by login; a non-empty search keeps users whose login, name or email contains it
	// (case-insensitive).
	ListUsers(ctx context.Context, p Page, search string) ([]User, int, error)

	CreateGroup(ctx context.Context, g Group) error
	UpdateGroup(ctx context.Context, g Group) error
	// DeleteGroup removes the group, its memberships and role assignments.
	DeleteGroup(ctx context.Context, id ids.UUID) error
	GetGroup(ctx context.Context, id ids.UUID) (Group, error)
	ListGroups(ctx context.Context, p Page) ([]Group, int, error) // by name
	AddMember(ctx context.Context, group, user ids.UUID) error    // idempotent
	RemoveMember(ctx context.Context, group, user ids.UUID) error // idempotent
	GroupsOf(ctx context.Context, user ids.UUID) ([]ids.UUID, error)
	Members(ctx context.Context, group ids.UUID, p Page) ([]User, int, error)

	CreateRole(ctx context.Context, r Role) error
	UpdateRole(ctx context.Context, r Role) error
	// DeleteRole removes the role and its assignments.
	DeleteRole(ctx context.Context, id ids.UUID) error
	GetRole(ctx context.Context, id ids.UUID) (Role, error)
	RoleByName(ctx context.Context, name string) (Role, error)
	ListRoles(ctx context.Context, p Page) ([]Role, int, error) // by name

	CreateAssignment(ctx context.Context, a Assignment) error
	DeleteAssignment(ctx context.Context, id ids.UUID) error
	GetAssignment(ctx context.Context, id ids.UUID) (Assignment, error)
	RoleAssignments(ctx context.Context, role ids.UUID) ([]Assignment, error)
	// AssignmentsFor returns every assignment of these principals with its role, ordered by role name then id.
	AssignmentsFor(ctx context.Context, principals []PrincipalRef) ([]RoleGrant, error)

	CreateToken(ctx context.Context, t Token) error
	// DeleteToken removes the token and its role assignments.
	DeleteToken(ctx context.Context, id ids.UUID) error
	RevokeToken(ctx context.Context, id ids.UUID, at time.Time) error
	GetToken(ctx context.Context, id ids.UUID) (Token, error)
	TokenByHash(ctx context.Context, hash []byte) (Token, error)
	ListTokens(ctx context.Context, p Page) ([]Token, int, error) // newest first
	TouchToken(ctx context.Context, id ids.UUID, at time.Time) error

	CreateSession(ctx context.Context, s Session) error
	SessionByHash(ctx context.Context, hash []byte) (Session, error)
	// SessionUserByHash returns the session with its user and the user's group ids (by id) in one query, the
	// per-request lookup. ErrNotFound when the session does not exist.
	SessionUserByHash(ctx context.Context, hash []byte) (SessionUser, error)
	// TouchSession sets last_seen_at and expires_at.
	TouchSession(ctx context.Context, hash []byte, seen, expires time.Time) error
	DeleteSession(ctx context.Context, hash []byte) error
	DeleteUserSessions(ctx context.Context, user ids.UUID) error
	// DeleteOtherSessions ends every session of the user except the one with this hash.
	DeleteOtherSessions(ctx context.Context, user ids.UUID, keep []byte) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error)

	// Meta is a small key/value table for auth bookkeeping (e.g. "templates_seeded"); ErrNotFound when unset.
	Meta(ctx context.Context, key string) (string, error)
	SetMeta(ctx context.Context, key, value string) error
}

// Page is offset paging for admin lists; Limit 1..500 (0 = 50).
type Page struct {
	Limit  int
	Offset int
}

type User struct {
	ID           ids.UUID
	Login        string // lower-case, unique
	Email        string
	Name         string
	PasswordHash string // "" = cannot log in with a password
	Disabled     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

type Group struct {
	ID          ids.UUID
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Role struct {
	ID          ids.UUID
	Name        string
	Description string
	Policies    []byte // JSON array of {policy, limitations}; decoded by the access package
	Template    string // the template it was seeded from, or ""
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// PrincipalKind is who a role is assigned to.
type PrincipalKind string

const (
	PrincipalUser  PrincipalKind = "user"
	PrincipalGroup PrincipalKind = "group"
	PrincipalToken PrincipalKind = "token"
)

type PrincipalRef struct {
	Kind PrincipalKind
	ID   ids.UUID
}

type Assignment struct {
	ID          ids.UUID
	RoleID      ids.UUID
	Principal   PrincipalRef
	Limitations []byte // JSON object of limitation name → values; "{}" = none
	CreatedAt   time.Time
}

// RoleGrant is an assignment joined with its role.
type RoleGrant struct {
	Assignment Assignment
	Role       Role
}

type Token struct {
	ID         ids.UUID
	Name       string
	Kind       string // "token" or "agent"
	SecretHash []byte // SHA-256 of the secret
	Prefix     string // first 8 characters of the secret
	CreatedBy  string // actor
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

type Session struct {
	IDHash     []byte // SHA-256 of the cookie value
	UserID     ids.UUID
	CSRF       string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IP         string
	UserAgent  string
}

// SessionUser is a session with the user it belongs to.
type SessionUser struct {
	Session Session
	User    User
	Groups  []ids.UUID
}
