package manage

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// authRoutes are the sign-in, account and access-management endpoints. Their policies are the coarse gate;
// access.Admin checks the caller again, including what it may grant.
var authRoutes = []route{
	{"POST", "/auth/login", Public, (*api).login},
	{"POST", "/auth/logout", Authenticated, (*api).logout},
	{"GET", "/auth/session", Authenticated, (*api).session},
	{"POST", "/auth/password", Authenticated, (*api).changePassword},
	{"GET", "/users", access.PolicyUserManage, (*api).listUsers},
	{"POST", "/users", access.PolicyUserManage, (*api).createUser},
	{"GET", "/users/{id}", access.PolicyUserManage, (*api).getUser},
	{"PATCH", "/users/{id}", access.PolicyUserManage, (*api).updateUser},
	{"DELETE", "/users/{id}", access.PolicyUserManage, (*api).deleteUser},
	{"GET", "/groups", access.PolicyUserManage, (*api).listGroups},
	{"POST", "/groups", access.PolicyUserManage, (*api).createGroup},
	{"GET", "/groups/{id}", access.PolicyUserManage, (*api).getGroup},
	{"PATCH", "/groups/{id}", access.PolicyUserManage, (*api).updateGroup},
	{"DELETE", "/groups/{id}", access.PolicyUserManage, (*api).deleteGroup},
	{"GET", "/groups/{id}/members", access.PolicyUserManage, (*api).listMembers},
	{"PUT", "/groups/{id}/members/{user}", access.PolicyUserManage, (*api).addMember},
	{"DELETE", "/groups/{id}/members/{user}", access.PolicyUserManage, (*api).removeMember},
	{"GET", "/roles", access.PolicyRoleManage, (*api).listRoles},
	{"POST", "/roles", access.PolicyRoleManage, (*api).createRole},
	{"GET", "/roles/{id}", access.PolicyRoleManage, (*api).getRole},
	{"PATCH", "/roles/{id}", access.PolicyRoleManage, (*api).updateRole},
	{"DELETE", "/roles/{id}", access.PolicyRoleManage, (*api).deleteRole},
	{"GET", "/roles/{id}/assignments", access.PolicyRoleManage, (*api).roleAssignments},
	{"GET", "/assignments", access.PolicyRoleManage, (*api).listAssignments},
	{"POST", "/assignments", access.PolicyRoleManage, (*api).createAssignment},
	{"DELETE", "/assignments/{id}", access.PolicyRoleManage, (*api).deleteAssignment},
	{"GET", "/policies", access.PolicyRoleManage, (*api).policies},
	{"GET", "/tokens", access.PolicyTokenManage, (*api).listTokens},
	{"POST", "/tokens", access.PolicyTokenManage, (*api).createToken},
	{"GET", "/tokens/{id}", access.PolicyTokenManage, (*api).getToken},
	{"DELETE", "/tokens/{id}", access.PolicyTokenManage, (*api).revokeToken},
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
	if an := authnFrom(ctx); an != nil && an.Session != nil {
		out["csrf"] = an.Session.CSRF
		out["expiresAt"] = an.Session.ExpiresAt
	}
	WriteJSON(w, http.StatusOK, out)
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
	if err := adm.ChangeOwnPassword(r.Context(), b.Current, b.New); err != nil {
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

func (a *api) listUsers(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	us, total, err := adm.ListUsers(r.Context(), p)
	if err != nil {
		return err
	}
	list(w, us, total)
	return nil
}

func (a *api) createUser(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	var b struct {
		Login    string `json:"login"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	u, err := adm.CreateUser(r.Context(), access.UserInput{Login: b.Login, Name: b.Name, Email: b.Email, Password: b.Password})
	if err != nil {
		return err
	}
	w.Header().Set("Location", a.prefix+"/users/"+u.ID.String())
	WriteJSON(w, http.StatusCreated, u)
	return nil
}

func (a *api) getUser(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	u, err := adm.GetUser(r.Context(), id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, u)
	return nil
}

func (a *api) updateUser(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var b struct {
		Name     *string `json:"name"`
		Email    *string `json:"email"`
		Disabled *bool   `json:"disabled"`
		Password *string `json:"password"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	ctx := r.Context()
	if b.Password != nil {
		if err := adm.ResetPassword(ctx, id, *b.Password); err != nil {
			return err
		}
	}
	u, err := adm.UpdateUser(ctx, id, access.UserPatch{Name: b.Name, Email: b.Email, Disabled: b.Disabled})
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, u)
	return nil
}

func (a *api) deleteUser(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := adm.DeleteUser(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
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

func (a *api) listGroups(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	gs, total, err := adm.ListGroups(r.Context(), p)
	if err != nil {
		return err
	}
	out := make([]groupJSON, len(gs))
	for i, g := range gs {
		out[i] = groupView(g)
	}
	list(w, out, total)
	return nil
}

func (a *api) createGroup(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	var b struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	g, err := adm.CreateGroup(r.Context(), b.Name, b.Description)
	if err != nil {
		return err
	}
	w.Header().Set("Location", a.prefix+"/groups/"+g.ID.String())
	WriteJSON(w, http.StatusCreated, groupView(g))
	return nil
}

func (a *api) getGroup(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	g, err := adm.GetGroup(r.Context(), id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, groupView(g))
	return nil
}

func (a *api) updateGroup(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var b struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	g, err := adm.UpdateGroup(r.Context(), id, b.Name, b.Description)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, groupView(g))
	return nil
}

func (a *api) deleteGroup(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := adm.DeleteGroup(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
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

func (a *api) listRoles(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	rs, total, err := adm.ListRoles(r.Context(), p)
	if err != nil {
		return err
	}
	list(w, rs, total)
	return nil
}

type policiesBody = []access.RolePolicy

func (a *api) createRole(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	var b struct {
		Name        string       `json:"name"`
		Description string       `json:"description"`
		Policies    policiesBody `json:"policies"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	role, err := adm.CreateRole(r.Context(), access.RoleInput{Name: b.Name, Description: b.Description, Policies: b.Policies})
	if err != nil {
		return err
	}
	w.Header().Set("Location", a.prefix+"/roles/"+role.ID.String())
	WriteJSON(w, http.StatusCreated, role)
	return nil
}

func (a *api) getRole(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	role, err := adm.GetRole(r.Context(), id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, role)
	return nil
}

func (a *api) updateRole(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var b struct {
		Name        *string       `json:"name"`
		Description *string       `json:"description"`
		Policies    *policiesBody `json:"policies"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	role, err := adm.UpdateRole(r.Context(), id, access.RolePatch{Name: b.Name, Description: b.Description, Policies: b.Policies})
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, role)
	return nil
}

func (a *api) deleteRole(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := adm.DeleteRole(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *api) roleAssignments(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	as, err := adm.RoleAssignments(r.Context(), id)
	if err != nil {
		return err
	}
	list(w, as, len(as))
	return nil
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

func (a *api) createAssignment(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	var b struct {
		RoleID        string              `json:"roleId"`
		PrincipalKind string              `json:"principalKind"`
		PrincipalID   string              `json:"principalId"`
		Limitations   map[string][]string `json:"limitations"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	role, rerr := ids.Parse(b.RoleID)
	principal, perr := ids.Parse(b.PrincipalID)
	if rerr != nil || perr != nil {
		return BadRequest("roleId and principalId must be ids")
	}
	as, err := adm.CreateAssignment(r.Context(), access.AssignmentInput{RoleID: role,
		Principal: store.PrincipalRef{Kind: store.PrincipalKind(b.PrincipalKind), ID: principal}, Limitations: b.Limitations})
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusCreated, as)
	return nil
}

func (a *api) deleteAssignment(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := adm.DeleteAssignment(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
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

func (a *api) listTokens(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	p, err := pageOf(r)
	if err != nil {
		return err
	}
	ts, total, err := adm.ListTokens(r.Context(), p)
	if err != nil {
		return err
	}
	list(w, ts, total)
	return nil
}

// createToken answers with the secret, the only time it is ever shown. Roles are role ids or names.
func (a *api) createToken(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	var b struct {
		Name      string     `json:"name"`
		Kind      string     `json:"kind"`
		Roles     []string   `json:"roles"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	ctx := r.Context()
	in := access.TokenInput{Name: b.Name, Kind: b.Kind, ExpiresAt: b.ExpiresAt}
	for _, ref := range b.Roles {
		id, err := ids.Parse(ref)
		if err != nil {
			role, rerr := adm.RoleByName(ctx, ref)
			if rerr != nil {
				return rerr
			}
			id = role.ID
		}
		in.Roles = append(in.Roles, id)
	}
	t, secret, err := adm.CreateToken(ctx, in)
	if err != nil {
		return err
	}
	w.Header().Set("Location", a.prefix+"/tokens/"+t.ID.String())
	WriteJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": secret})
	return nil
}

func (a *api) getToken(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	t, err := adm.GetToken(r.Context(), id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, t)
	return nil
}

func (a *api) revokeToken(w http.ResponseWriter, r *http.Request) error {
	adm, err := a.adminOrNotFound()
	if err != nil {
		return err
	}
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := adm.RevokeToken(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
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
		case "actor", "action", "subject", "since", "limit", "offset":
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
	WriteJSON(w, http.StatusOK, map[string]any{"items": entries, "hasMore": more})
	return nil
}

func isUnauthenticated(err error) bool { return errors.Is(err, access.ErrUnauthenticated) }
