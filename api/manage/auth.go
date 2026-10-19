package manage

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// authRoutes are the sign-in, account and access-management endpoints. Their policies are the coarse gate;
// access.Admin checks the caller again, including what it may grant.
var authRoutes = []route{
	{"POST", "/auth/login", Public, (*api).login},
	{"GET", "/auth/setup", Public, (*api).setupState},
	{"POST", "/auth/setup", Public, (*api).setup},
	{"POST", "/auth/logout", Authenticated, (*api).logout},
	{"GET", "/auth/session", Authenticated, (*api).session},
	{"POST", "/auth/password", Authenticated, (*api).changePassword},
	{"GET", "/users", access.PolicyUserManage, (*api).listUsers},
	{"POST", "/users", access.PolicyUserManage, creating("/users", createUser)},
	{"GET", "/users/{id}", access.PolicyUserManage, byID((*access.Admin).GetUser, same)},
	{"GET", "/users/{id}/groups", access.PolicyUserManage, (*api).userGroups},
	{"PATCH", "/users/{id}", access.PolicyUserManage, patching(updateUser, same)},
	{"DELETE", "/users/{id}", access.PolicyUserManage, deleting((*access.Admin).DeleteUser)},
	{"GET", "/groups", access.PolicyUserManage, listing((*access.Admin).ListGroups, groupView)},
	{"POST", "/groups", access.PolicyUserManage, creating("/groups", createGroup)},
	{"GET", "/groups/{id}", access.PolicyUserManage, byID((*access.Admin).GetGroup, groupView)},
	{"PATCH", "/groups/{id}", access.PolicyUserManage, patching(updateGroup, groupView)},
	{"DELETE", "/groups/{id}", access.PolicyUserManage, deleting((*access.Admin).DeleteGroup)},
	{"GET", "/groups/{id}/members", access.PolicyUserManage, (*api).listMembers},
	{"PUT", "/groups/{id}/members/{user}", access.PolicyUserManage, (*api).addMember},
	{"DELETE", "/groups/{id}/members/{user}", access.PolicyUserManage, (*api).removeMember},
	{"GET", "/roles", access.PolicyRoleManage, listing((*access.Admin).ListRoles, same)},
	{"POST", "/roles", access.PolicyRoleManage, creating("/roles", createRole)},
	{"GET", "/roles/{id}", access.PolicyRoleManage, byID((*access.Admin).GetRole, same)},
	{"PATCH", "/roles/{id}", access.PolicyRoleManage, patching(updateRole, same)},
	{"DELETE", "/roles/{id}", access.PolicyRoleManage, deleting((*access.Admin).DeleteRole)},
	{"GET", "/roles/{id}/assignments", access.PolicyRoleManage, byID((*access.Admin).RoleAssignments, listOf)},
	{"GET", "/assignments", access.PolicyRoleManage, (*api).listAssignments},
	{"POST", "/assignments", access.PolicyRoleManage, creating("", createAssignment)},
	{"DELETE", "/assignments/{id}", access.PolicyRoleManage, deleting((*access.Admin).DeleteAssignment)},
	{"GET", "/policies", access.PolicyRoleManage, (*api).policies},
	{"GET", "/tokens", access.PolicyTokenManage, listing((*access.Admin).ListTokens, same)},
	{"POST", "/tokens", access.PolicyTokenManage, creating("/tokens", createToken)},
	{"GET", "/tokens/{id}", access.PolicyTokenManage, byID((*access.Admin).GetToken, same)},
	{"DELETE", "/tokens/{id}", access.PolicyTokenManage, deleting((*access.Admin).RevokeToken)},
	{"GET", "/audit", access.PolicyAuditRead, (*api).auditLog},
}

func (a *api) adminOrNotFound() (*access.Admin, error) {
	if a.admin == nil {
		return nil, NotFound("access management is not enabled")
	}
	return a.admin, nil
}

func list[T any](w http.ResponseWriter, items []T, total int) {
	if items == nil {
		items = []T{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func pageOf(r *http.Request) (store.Page, error) {
	q := r.URL.Query()
	limit, err := parseInt(q, "limit", defaultLimit, 1, maxLimit)
	if err != nil {
		return store.Page{}, err
	}
	offset, err := parseInt(q, "offset", 0, 0, maxOffset)
	return store.Page{Limit: limit, Offset: offset}, err
}

func pathUUID(r *http.Request, name string) (ids.UUID, error) {
	id, err := ids.Parse(r.PathValue(name))
	if err != nil {
		return ids.Nil, NotFound("%s not found", name)
	}
	return id, nil
}

type principalJSON struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// maxCredentialBody bounds login and password-change bodies, which anyone can send.
const maxCredentialBody = 4 << 10

// busy maps a full password-check gate to 503.
func busy(err error) error {
	if errors.Is(err, access.ErrBusy) {
		return &Error{Status: http.StatusServiceUnavailable, Code: "busy", Message: "too many sign-ins in progress; retry shortly"}
	}
	return err
}

func (a *api) login(w http.ResponseWriter, r *http.Request) error {
	var b struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialBody)
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	res, err := a.auth.Login(w, r, b.Login, b.Password)
	if err != nil {
		return busy(err)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": access.ViewUser(res.User), "csrf": res.CSRF, "expiresAt": res.ExpiresAt})
	return nil
}

// setupState tells the admin app whether the first administrator is still to be created.
func (a *api) setupState(w http.ResponseWriter, r *http.Request) error {
	need := false
	if a.admin != nil {
		var err error
		if need, err = a.admin.NeedsSetup(r.Context()); err != nil {
			return err
		}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"required": need})
	return nil
}

// setup creates the first administrator with the setup token the server printed, then signs them in.
func (a *api) setup(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	var b struct {
		Token    string `json:"token"`
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialBody)
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	switch _, err := adm.CompleteSetup(r.Context(), b.Token, b.Login, b.Password); {
	case errors.Is(err, access.ErrSetupToken):
		return &Error{Status: http.StatusForbidden, Code: "setup_link", Message: "the setup link is not valid; use the one the server printed at its last start"}
	case errors.Is(err, access.ErrSetupDone):
		return Conflict("setup_done", "an administrator already exists; sign in instead")
	case err != nil:
		return err
	}
	res, err := a.auth.Login(w, r, b.Login, b.Password)
	if err != nil {
		return busy(err)
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": access.ViewUser(res.User), "csrf": res.CSRF, "expiresAt": res.ExpiresAt})
	return nil
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) error {
	if err := a.auth.Logout(w, r); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *api) session(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p := access.PrincipalFrom(ctx)
	out := map[string]any{"principal": principalJSON{Kind: string(p.Kind), ID: idString(p.ID), Name: p.Name}}
	grants := []access.Grant{}
	switch {
	case p.Super:
		grants = append(grants, access.Grant{Policy: access.PolicyAll})
	case a.gate != nil:
		gs, err := a.gate.Grants(ctx, p)
		if err != nil {
			return err
		}
		grants = append(grants, gs...)
	}
	out["grants"] = grants
	if p.Kind == access.KindUser {
		groups := make([]string, len(p.Groups))
		for i, g := range p.Groups {
			groups[i] = g.String()
		}
		out["groups"] = groups
	}
	if an := authnFrom(ctx); an != nil && an.Session != nil {
		out["csrf"] = an.Session.CSRF
		out["expiresAt"] = an.Session.ExpiresAt
	}
	WriteJSON(w, http.StatusOK, out)
	return nil
}

func (a *api) listUsers(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	q := r.URL.Query()
	search, err := single(q, "q")
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	us, total, err := adm.SearchUsers(r.Context(), p, search)
	if err != nil {
		return err
	}
	list(w, us, total)
	return nil
}

func (a *api) userGroups(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	ctx := r.Context()
	if _, err := adm.GetUser(ctx, id); err != nil {
		return err
	}
	gids, err := adm.GroupsOf(ctx, id)
	if err != nil {
		return err
	}
	out := make([]groupJSON, 0, len(gids))
	for _, gid := range gids {
		g, err := adm.GetGroup(ctx, gid)
		if err != nil {
			return err
		}
		out = append(out, groupView(g))
	}
	list(w, out, len(out))
	return nil
}

func idString(id ids.UUID) string {
	if id.IsZero() {
		return ""
	}
	return id.String()
}

func (a *api) changePassword(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	var b struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCredentialBody)
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	var keep []byte
	if an := authnFrom(r.Context()); an != nil && an.Session != nil {
		keep = an.Session.IDHash
	}
	if err := adm.ChangeOwnPassword(r.Context(), b.Current, b.New, keep); err != nil {
		err = busy(err)
		if p := access.PrincipalFrom(r.Context()); p != nil && p.Kind == access.KindUser && isUnauthenticated(err) {
			return &Error{Status: http.StatusForbidden, Code: "wrong_password", Message: "the current password is wrong"}
		}
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Users.

type handler = func(*api, http.ResponseWriter, *http.Request) error

func same[T any](v T) T { return v }

func listOf[T any](items []T) map[string]any {
	if items == nil {
		items = []T{}
	}
	return map[string]any{"items": items, "total": len(items)}
}

func listing[T, V any](fn func(*access.Admin, context.Context, store.Page) ([]T, int, error), view func(T) V) handler {
	return func(a *api, w http.ResponseWriter, r *http.Request) error {
		adm, err := a.adminOrNotFound()
		if err != nil {
			return err
		}
		p, err := pageOf(r)
		if err != nil {
			return err
		}
		items, total, err := fn(adm, r.Context(), p)
		if err != nil {
			return err
		}
		out := make([]V, len(items))
		for i, it := range items {
			out[i] = view(it)
		}
		list(w, out, total)
		return nil
	}
}

func byID[T, V any](fn func(*access.Admin, context.Context, ids.UUID) (T, error), view func(T) V) handler {
	return func(a *api, w http.ResponseWriter, r *http.Request) error {
		adm, err := a.adminOrNotFound()
		if err != nil {
			return err
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		v, err := fn(adm, r.Context(), id)
		if err != nil {
			return err
		}
		WriteJSON(w, http.StatusOK, view(v))
		return nil
	}
}

// creating decodes B and answers 201 with fn's result; a non-empty path adds a Location of path/<id>.
func creating[B any](path string, fn func(*access.Admin, context.Context, B) (ids.UUID, any, error)) handler {
	return func(a *api, w http.ResponseWriter, r *http.Request) error {
		adm, err := a.adminOrNotFound()
		if err != nil {
			return err
		}
		var b B
		if err := decodeBody(w, r, &b, false); err != nil {
			return err
		}
		id, out, err := fn(adm, r.Context(), b)
		if err != nil {
			return err
		}
		if path != "" {
			w.Header().Set("Location", a.prefix+path+"/"+id.String())
		}
		WriteJSON(w, http.StatusCreated, out)
		return nil
	}
}

func patching[B, T, V any](fn func(*access.Admin, context.Context, ids.UUID, B) (T, error), view func(T) V) handler {
	return func(a *api, w http.ResponseWriter, r *http.Request) error {
		adm, err := a.adminOrNotFound()
		if err != nil {
			return err
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		var b B
		if err := decodeBody(w, r, &b, false); err != nil {
			return err
		}
		v, err := fn(adm, r.Context(), id, b)
		if err != nil {
			return err
		}
		WriteJSON(w, http.StatusOK, view(v))
		return nil
	}
}

func deleting(fn func(*access.Admin, context.Context, ids.UUID) error) handler {
	return func(a *api, w http.ResponseWriter, r *http.Request) error {
		adm, err := a.adminOrNotFound()
		if err != nil {
			return err
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		if err := fn(adm, r.Context(), id); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

type userBody struct {
	Login    string `json:"login"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func createUser(adm *access.Admin, ctx context.Context, b userBody) (ids.UUID, any, error) {
	u, err := adm.CreateUser(ctx, access.UserInput{Login: b.Login, Name: b.Name, Email: b.Email, Password: b.Password})
	return u.ID, u, err
}

type userPatchBody struct {
	Name     *string `json:"name"`
	Email    *string `json:"email"`
	Disabled *bool   `json:"disabled"`
	Password *string `json:"password"`
}

func updateUser(adm *access.Admin, ctx context.Context, id ids.UUID, b userPatchBody) (access.User, error) {
	if b.Password != nil {
		if err := adm.ResetPassword(ctx, id, *b.Password); err != nil {
			return access.User{}, err
		}
	}
	return adm.UpdateUser(ctx, id, access.UserPatch{Name: b.Name, Email: b.Email, Disabled: b.Disabled})
}

// Groups.

type groupJSON struct {
	ID          ids.UUID  `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func groupView(g store.Group) groupJSON {
	return groupJSON{ID: g.ID, Name: g.Name, Description: g.Description, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
}

type groupBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func createGroup(adm *access.Admin, ctx context.Context, b groupBody) (ids.UUID, any, error) {
	g, err := adm.CreateGroup(ctx, b.Name, b.Description)
	return g.ID, groupView(g), err
}

type groupPatchBody struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func updateGroup(adm *access.Admin, ctx context.Context, id ids.UUID, b groupPatchBody) (store.Group, error) {
	return adm.UpdateGroup(ctx, id, b.Name, b.Description)
}

func (a *api) listMembers(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	us, total, err := adm.Members(r.Context(), id, p)
	if err != nil {
		return err
	}
	list(w, us, total)
	return nil
}

func (a *api) addMember(w http.ResponseWriter, r *http.Request) error {
	return a.member(w, r, (*access.Admin).AddMember)
}

func (a *api) removeMember(w http.ResponseWriter, r *http.Request) error {
	return a.member(w, r, (*access.Admin).RemoveMember)
}

func (a *api) member(w http.ResponseWriter, r *http.Request, fn func(*access.Admin, context.Context, ids.UUID, ids.UUID) error) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	group, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	user, err := pathUUID(r, "user")
	if err != nil {
		return err
	}
	if err := fn(adm, r.Context(), group, user); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Roles and assignments.

type policiesBody = []access.RolePolicy

type roleBody struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Policies    policiesBody `json:"policies"`
}

func createRole(adm *access.Admin, ctx context.Context, b roleBody) (ids.UUID, any, error) {
	role, err := adm.CreateRole(ctx, access.RoleInput{Name: b.Name, Description: b.Description, Policies: b.Policies})
	return role.ID, role, err
}

type rolePatchBody struct {
	Name        *string       `json:"name"`
	Description *string       `json:"description"`
	Policies    *policiesBody `json:"policies"`
}

func updateRole(adm *access.Admin, ctx context.Context, id ids.UUID, b rolePatchBody) (access.Role, error) {
	return adm.UpdateRole(ctx, id, access.RolePatch{Name: b.Name, Description: b.Description, Policies: b.Policies})
}

// listAssignments lists the roles of one principal: ?principalKind=user|group|token&principalId=<id>.
func (a *api) listAssignments(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	q := r.URL.Query()
	for k := range q {
		if k != "principalKind" && k != "principalId" {
			return BadRequest("unknown query parameter %q", k)
		}
	}
	kind, err := single(q, "principalKind")
	if err != nil {
		return err
	}
	rawID, err := single(q, "principalId")
	if err != nil {
		return err
	}
	id, perr := ids.Parse(rawID)
	if !slices.Contains([]string{"user", "group", "token"}, kind) || perr != nil {
		return BadRequest("principalKind (user, group or token) and principalId are required")
	}
	as, err := adm.AssignmentsOf(r.Context(), store.PrincipalRef{Kind: store.PrincipalKind(kind), ID: id})
	if err != nil {
		return err
	}
	list(w, as, len(as))
	return nil
}

type assignmentBody struct {
	RoleID        string              `json:"roleId"`
	PrincipalKind string              `json:"principalKind"`
	PrincipalID   string              `json:"principalId"`
	Limitations   map[string][]string `json:"limitations"`
}

func createAssignment(adm *access.Admin, ctx context.Context, b assignmentBody) (ids.UUID, any, error) {
	role, rerr := ids.Parse(b.RoleID)
	principal, perr := ids.Parse(b.PrincipalID)
	if rerr != nil || perr != nil {
		return ids.Nil, nil, BadRequest("roleId and principalId must be ids")
	}
	as, err := adm.CreateAssignment(ctx, access.AssignmentInput{RoleID: role,
		Principal: store.PrincipalRef{Kind: store.PrincipalKind(b.PrincipalKind), ID: principal}, Limitations: b.Limitations})
	return ids.Nil, as, err
}

func (a *api) policies(w http.ResponseWriter, r *http.Request) error {
	if a.catalog == nil {
		return NotFound("access management is not enabled")
	}
	v, err := a.catalog.Describe(r.Context())
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, v)
	return nil
}

// Tokens.

type tokenBody struct {
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Roles     []string   `json:"roles"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

// createToken answers with the secret, the only time it is ever shown. Roles are role ids or names.
func createToken(adm *access.Admin, ctx context.Context, b tokenBody) (ids.UUID, any, error) {
	in := access.TokenInput{Name: b.Name, Kind: b.Kind, ExpiresAt: b.ExpiresAt}
	for _, ref := range b.Roles {
		id, err := ids.Parse(ref)
		if err != nil {
			role, rerr := adm.RoleByName(ctx, ref)
			if rerr != nil {
				return ids.Nil, nil, rerr
			}
			id = role.ID
		}
		in.Roles = append(in.Roles, id)
	}
	t, secret, err := adm.CreateToken(ctx, in)
	return t.ID, map[string]any{"token": t, "secret": secret}, err
}

// Audit.

func (a *api) auditLog(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	q := r.URL.Query()
	var f access.AuditFilter
	for k := range q {
		switch k {
		case "actor", "action", "subject", "since", "before", "limit", "offset":
		default:
			return BadRequest("unknown query parameter %q", k)
		}
	}
	for key, dst := range map[string]*string{"actor": &f.Actor, "action": &f.Action, "subject": &f.Subject} {
		if *dst, err = single(q, key); err != nil {
			return err
		}
	}
	since, err := single(q, "since")
	if err != nil {
		return err
	}
	if since != "" {
		if f.Since, err = time.Parse(time.RFC3339, since); err != nil {
			return BadRequest("since must be an RFC 3339 time")
		}
	}
	if f.Limit, err = parseInt(q, "limit", defaultLimit, 1, maxLimit); err != nil {
		return err
	}
	if f.Offset, err = parseInt(q, "offset", 0, 0, maxOffset); err != nil {
		return err
	}
	before, err := single(q, "before")
	if err != nil {
		return err
	}
	if before != "" {
		if f.Before, err = strconv.ParseInt(before, 10, 64); err != nil || f.Before < 1 {
			return BadRequest("before must be an audit entry id")
		}
	}
	if strings.ContainsAny(f.Actor, " \t\n") {
		return BadRequest("actor is a kind or kind:id")
	}
	entries, more, err := adm.AuditLog(r.Context(), f)
	if err != nil {
		return err
	}
	if entries == nil {
		entries = []access.AuditEntry{}
	}
	out := map[string]any{"items": entries, "hasMore": more}
	if more {
		out["next"] = entries[len(entries)-1].ID
	}
	WriteJSON(w, http.StatusOK, out)
	return nil
}

func isUnauthenticated(err error) bool { return errors.Is(err, access.ErrUnauthenticated) }
