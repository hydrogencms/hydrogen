package tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/features/review"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

func (n *revNode) hash(id ids.UUID) string { return n.review(id)["draftHash"].(string) }

func lastKind(rv map[string]any) string { k := kinds(rv); return k[len(k)-1] }

// approved leaves a draft of the article approved: alice edits and submits, bob approves.
func (n *revNode) approved(alice, bob who, id ids.UUID, title string) {
	n.t.Helper()
	n.edit(alice, id, `{"title":"`+title+`"}`)
	n.act(alice, id, "submit", nil, 200)
	n.act(bob, id, "approve", nil, 200)
	if got := n.state(id); got != "approved" {
		n.t.Fatalf("state = %s, want approved", got)
	}
}

func TestReviewMetaChange(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		c := n.app.Services().Content
		alice := n.person("alice", editorRights...)
		bob := n.person("bob", reviewerRights...)

		// A published item: key and availability go live at once, so the review gate applies.
		art := n.create("article", "", "en", `{"title":"Live"}`, true)
		n.approved(alice, bob, art, "Next")
		h0 := n.hash(art)
		k := "first-key"
		_, err := c.Update(alice.ctx, art, content.UpdateInput{Key: &k})
		if !errors.Is(err, content.ErrReviewRequired) {
			t.Fatalf("editor changes the key of a published review item: %v", err)
		}
		deq(t, "still approved", n.state(art), "approved")
		deq(t, "hash unchanged", n.hash(art), h0)

		// The administrator (review.bypass) may; it is audited, and the approval is reopened.
		yes := true
		if _, err := c.Update(ctx, art, content.UpdateInput{AlwaysAvailable: &yes}); err != nil {
			t.Fatal(err)
		}
		rv := n.review(art)
		deq(t, "reopened", rv["state"], "in_review")
		deq(t, "reopened event", lastKind(rv), "reopened")
		if rv["draftHash"] == h0 {
			t.Error("availability is not part of the draft hash")
		}
		// A system caller has no principal and is not checked; make the admin path audited by a principal.
		root2 := n.person("root2", "*")
		if _, err := c.Update(root2.ctx, art, content.UpdateInput{Key: &k}); err != nil {
			t.Fatal(err)
		}
		bypassed := 0
		for _, d := range n.auditData(art, "item.update") {
			if d["reviewBypassed"] == true {
				bypassed++
			}
		}
		deq(t, "bypass audited (the system's update and the principal's)", bypassed, 2)

		// An unpublished item has no live effect: no gate, but the key is part of the hash.
		draft := n.create("article", "", "en", `{"title":"Draft"}`, false)
		n.approved(alice, bob, draft, "Draft 2")
		h1 := n.hash(draft)
		k2 := "draft-key"
		if _, err := c.Update(alice.ctx, draft, content.UpdateInput{Key: &k2}); err != nil {
			t.Fatal(err)
		}
		rv = n.review(draft)
		deq(t, "key change reopens", rv["state"], "in_review")
		if rv["draftHash"] == h1 {
			t.Error("the key is not part of the draft hash")
		}
	})
}

func TestReviewSystemDraftChanges(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		c := n.app.Services().Content
		alice := n.person("alice", editorRights...)
		bob := n.person("bob", reviewerRights...)

		// Purging a referenced item removes the reference from the draft too.
		p := n.create("person", "", "en", `{"fullName":"P"}`, true)
		art := n.create("article", "", "en", `{"title":"A","author":"`+p.String()+`"}`, true)
		n.approved(alice, bob, art, "A2")
		if err := c.Trash(ctx, p); err != nil {
			t.Fatal(err)
		}
		if err := c.Purge(ctx, p); err != nil {
			t.Fatal(err)
		}
		rv := n.review(art)
		deq(t, "nullify reopens", rv["state"], "in_review")
		deq(t, "reopened event", lastKind(rv), "reopened")

		// Unpublishing keeps the published content as draft parts: the draft is not what was approved.
		live := n.create("article", "", "en", `{"title":"Live","body":"b"}`, true)
		n.approved(alice, bob, live, "Live 2")
		if _, err := c.Unpublish(ctx, live); err != nil {
			t.Fatal(err)
		}
		deq(t, "unpublish reopens", n.state(live), "in_review")
	})
}

func TestReviewNothingGetsStuck(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		alice := n.person("alice", editorRights...)
		bob := n.person("bob", reviewerRights...)
		viewer := n.person("viewer", "content.read")

		// An approval whose stored hash no longer matches the draft can be submitted again.
		a := n.create("article", "", "en", `{"title":"A"}`, false)
		n.approved(alice, bob, a, "A2")
		err := n.app.Services().DB.Tx(ctx, func(tx store.Tx) error {
			r, err := tx.Reviews().Get(ctx, a)
			if err != nil {
				return err
			}
			r.DraftHash = "stale"
			return tx.Reviews().Put(ctx, r)
		})
		if err != nil {
			t.Fatal(err)
		}
		deq(t, "canSubmit on a stale approval", n.call(alice, "GET", "/items/"+a.String()+"/review", nil, 200).json()["canSubmit"], true)
		rv := n.act(alice, a, "submit", nil, 200)
		deq(t, "state", rv["state"], "in_review")
		deq(t, "event", lastKind(rv), "resubmitted")
		deq(t, "a fresh approval cannot be submitted", errCode(n.act(alice, a, "submit", nil, 409)), "review_state")

		// Withdraw from approved (the submitter) and from changes_requested (an approver); a bystander cannot.
		n.act(bob, a, "approve", nil, 200)
		n.act(viewer, a, "withdraw", nil, 403)
		deq(t, "withdraw approved", n.act(alice, a, "withdraw", nil, 200)["state"], "none")

		b := n.create("article", "", "en", `{"title":"B"}`, false)
		n.act(alice, b, "submit", nil, 200)
		n.act(bob, b, "request-changes", map[string]any{"comment": "no"}, 200)
		n.act(viewer, b, "withdraw", nil, 403)
		deq(t, "canWithdraw", n.call(alice, "GET", "/items/"+b.String()+"/review", nil, 200).json()["canWithdraw"], true)
		deq(t, "approver withdraws changes_requested", n.act(bob, b, "withdraw", nil, 200)["state"], "none")
	})
}

func TestReviewFourEyesEditorsAndTokens(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		s := n.app.Services()
		alice := n.person("alice", append(editorRights, "review.approve")...)
		bob := n.person("bob", append(reviewerRights, "content.edit")...)
		carol := n.person("carol", editorRights...)
		dave := n.person("dave", reviewerRights...)

		// Every editor of any part is out, not just the newest.
		a := n.create("article", "", "en", `{"title":"A"}`, false)
		n.edit(alice, a, `{"body":"shared part"}`)
		n.edit(bob, a, `{"title":"en part"}`)
		n.act(carol, a, "submit", nil, 200)
		deq(t, "alice (older part) blocked", errCode(n.act(alice, a, "approve", nil, 403)), "four_eyes")
		deq(t, "bob blocked", errCode(n.act(bob, a, "approve", nil, 403)), "four_eyes")
		deq(t, "canApprove alice", n.call(alice, "GET", "/items/"+a.String()+"/review", nil, 200).json()["canApprove"], false)
		deq(t, "dave approves", n.act(dave, a, "approve", nil, 200)["state"], "approved")

		// A token is its creator: a user and the token they made are one person.
		ursula, err := s.Admin.CreateUser(ctx, access.UserInput{Login: "ursula", Name: "Ursula"})
		if err != nil {
			t.Fatal(err)
		}
		role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: "ur", Policies: []access.RolePolicy{
			rolePolicy("content.read", nil), rolePolicy("content.edit", nil), rolePolicy("review.submit", nil), rolePolicy("review.approve", nil), rolePolicy("token.manage", nil), rolePolicy("role.manage", nil)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Admin.CreateAssignment(ctx, access.AssignmentInput{RoleID: role.ID, Principal: store.PrincipalRef{Kind: "user", ID: ursula.ID}}); err != nil {
			t.Fatal(err)
		}
		up, err := s.Auth.PrincipalOf(ctx, "user:"+ursula.ID.String())
		if err != nil || up == nil {
			t.Fatal(err)
		}
		uc := access.WithPrincipal(ctx, up)
		_, secret, err := s.Admin.CreateToken(uc, access.TokenInput{Name: "ursula-ci", Roles: []ids.UUID{role.ID}})
		if err != nil {
			t.Fatal(err)
		}
		tw := who{name: "ursula-ci", secret: secret}

		b := n.create("article", "", "en", `{"title":"B"}`, false)
		n.edit(carol, b, `{"title":"B2"}`)
		if _, err := s.Review.Submit(uc, b, review.SubmitInput{}); err != nil {
			t.Fatal(err)
		}
		deq(t, "the user's token cannot approve", errCode(n.act(tw, b, "approve", nil, 403)), "four_eyes")

		c := n.create("article", "", "en", `{"title":"C"}`, false)
		n.edit(carol, c, `{"title":"C2"}`)
		n.act(tw, c, "submit", nil, 200)
		_, err = s.Review.Approve(uc, c, review.ApproveInput{})
		var me *manage.Error
		if !errors.As(err, &me) || me.Code != "four_eyes" {
			t.Fatalf("the token's creator approves: %v", err)
		}
		// Somebody else is fine.
		deq(t, "dave approves", n.act(dave, c, "approve", nil, 200)["state"], "approved")
	})
}

func TestReviewStaleApprovalAndEdits(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		alice := n.person("alice", editorRights...)
		bob := n.person("bob", append(editorRights, "review.approve")...)
		dave := n.person("dave", reviewerRights...)

		a := n.create("article", "", "en", `{"title":"A"}`, false)
		n.act(alice, a, "submit", nil, 200)
		seen := n.hash(a)
		if seen == "" {
			t.Fatal("no draftHash in the view")
		}
		n.edit(alice, a, `{"title":"A2"}`)
		n.edit(alice, a, `{"title":"A3"}`) // coalesced with the previous edit
		deq(t, "edit notes", strings.Join(kinds(n.review(a)), ","), "submitted,edited")
		n.edit(bob, a, `{"title":"A4"}`)
		deq(t, "another editor", strings.Join(kinds(n.review(a)), ","), "submitted,edited,edited")

		deq(t, "stale", errCode(n.act(dave, a, "approve", map[string]any{"draftHash": seen}, 409)), "review_stale")
		deq(t, "still in review", n.state(a), "in_review")
		deq(t, "current hash", n.act(dave, a, "approve", map[string]any{"draftHash": n.hash(a), "comment": "ok"}, 200)["state"], "approved")
	})
}

func TestReviewQueueEarlyStop(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		s := n.app.Services()
		alice := n.person("alice", editorRights...)
		grp, err := s.Admin.CreateGroup(ctx, "Desk", "")
		if err != nil {
			t.Fatal(err)
		}
		var first ids.UUID
		for i := range 5 {
			id := n.create("article", "", "en", `{"title":"T`+string(rune('a'+i))+`"}`, false)
			n.tick()
			var body any
			if i == 0 {
				first, body = id, map[string]any{"reviewers": []string{"group:" + grp.ID.String()}}
			}
			n.act(alice, id, "submit", body, 200)
		}
		page := func(q string) map[string]any { return n.call(root, "GET", "/reviews"+q, nil, 200).json() }
		r := page("?limit=2")
		deq(t, "hasMore", r["hasMore"], true)
		deq(t, "total is a lower bound", r["total"], float64(3))
		r = page("?limit=2&offset=3")
		deq(t, "last page", r["hasMore"], false)
		deq(t, "exact total", r["total"], float64(5))
		deq(t, "rows", len(r["items"].([]any)), 2)
		r = page("?limit=5")
		deq(t, "full page has no more", r["hasMore"], false)
		for _, x := range r["items"].([]any) {
			row := x.(map[string]any)
			if row["item"] == first.String() {
				deq(t, "reviewerNames", asJSON(row["reviewerNames"]), `{"group:`+grp.ID.String()+`":"Desk"}`)
			} else {
				deq(t, "no reviewers", asJSON(row["reviewerNames"]), `{}`)
			}
		}

		// The type scope reaches the query: nothing to scan for a principal that reads no review type.
		notes := n.person("notes", "content.read")
		role, _ := s.Admin.RoleByName(ctx, "notes")
		keep := []access.RolePolicy{rolePolicy("content.read", map[string][]string{"collection": {"note"}})}
		if _, err := s.Admin.UpdateRole(ctx, role.ID, access.RolePatch{Policies: &keep}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Content.ListableTypes(notes.ctx, s.Content.ReviewTypes())
		if err != nil || len(got) != 0 {
			t.Fatalf("listable types = %v, %v", got, err)
		}
		deq(t, "no rows", len(page("")["items"].([]any)), 5)
		deq(t, "notes sees none", len(n.call(notes, "GET", "/reviews", nil, 200).json()["items"].([]any)), 0)
	})
}

func TestReviewApprovers(t *testing.T) {
	runReview(t, "", func(t *testing.T, n *revNode) {
		s := n.app.Services()
		mkUser := func(login, name string) access.User {
			u, err := s.Admin.CreateUser(ctx, access.UserInput{Login: login, Name: name})
			if err != nil {
				t.Fatal(err)
			}
			return u
		}
		assign := func(r access.Role, kind store.PrincipalKind, id ids.UUID) {
			if _, err := s.Admin.CreateAssignment(ctx, access.AssignmentInput{RoleID: r.ID, Principal: store.PrincipalRef{Kind: kind, ID: id}}); err != nil {
				t.Fatal(err)
			}
		}
		mkRole := func(name string, ps ...access.RolePolicy) access.Role {
			r, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: name, Policies: ps})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		approvers := mkRole("approvers", rolePolicy("content.read", nil), rolePolicy("review.approve", nil))
		notesOnly := mkRole("notes-approvers", rolePolicy("content.read", nil), rolePolicy("review.approve", map[string][]string{"collection": {"note"}}))
		readers := mkRole("readers", rolePolicy("content.read", nil))
		submitters := mkRole("submitters", rolePolicy("content.read", nil), rolePolicy("review.submit", nil), rolePolicy("review.approve", nil))

		uma, vic, wes, yan, carl, zed := mkUser("uma", "Uma"), mkUser("vic", "Vic"), mkUser("wes", "Wes"), mkUser("yan", "Yan"), mkUser("carl", "Carl"), mkUser("zed", "Zed")
		grp, err := s.Admin.CreateGroup(ctx, "Desk", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Admin.AddMember(ctx, grp.ID, vic.ID); err != nil {
			t.Fatal(err)
		}
		assign(approvers, store.PrincipalUser, uma.ID)
		assign(approvers, store.PrincipalGroup, grp.ID)
		assign(notesOnly, store.PrincipalUser, wes.ID)
		assign(readers, store.PrincipalUser, yan.ID)
		assign(submitters, store.PrincipalUser, carl.ID)
		assign(approvers, store.PrincipalUser, zed.ID)
		if _, err := s.Admin.UpdateUser(ctx, zed.ID, access.UserPatch{Disabled: ptr(true)}); err != nil {
			t.Fatal(err)
		}

		art := n.create("article", "", "en", `{"title":"A"}`, false)
		note := n.create("note", "", "en", `{"title":"N"}`, false)
		submitter := n.person("sub", "content.read", "review.submit")
		viewer := n.person("viewer", "content.read")

		type row struct{ Ref, Name, Kind string }
		list := func(w who, item string, want int) []row {
			t.Helper()
			r := n.call(w, "GET", "/reviews/approvers?item="+item, nil, want)
			if want != 200 {
				return nil
			}
			var out struct{ Items []row }
			if err := jsonDecode(r.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			return out.Items
		}
		join := func(rs []row) string {
			var out []string
			for _, r := range rs {
				out = append(out, r.Kind+":"+r.Name+":"+r.Ref)
			}
			return strings.Join(out, ",")
		}
		// Carl holds approve too, but he is the caller below; Uma, the Desk group (and Vic through it, and Carl) can.
		want := func(excludeCarl bool) string {
			rows := []string{"user:Carl:user:" + carl.ID.String(), "group:Desk:group:" + grp.ID.String(), "user:Uma:user:" + uma.ID.String(), "user:Vic:user:" + vic.ID.String()}
			if excludeCarl {
				rows = rows[1:]
			}
			return strings.Join(rows, ",")
		}
		deq(t, "approvers of an article", join(list(submitter, art.String(), 200)), want(false))
		list(viewer, art.String(), 403)
		list(submitter, "nope", 400)
		list(submitter, ids.UUID{9}.String(), 404)
		list(root, note.String(), 400) // a type without reviews

		cp, err := s.Auth.PrincipalOf(ctx, "user:"+carl.ID.String())
		if err != nil || cp == nil {
			t.Fatal(err)
		}
		got, err := s.Review.Approvers(access.WithPrincipal(ctx, cp), art)
		if err != nil {
			t.Fatal(err)
		}
		var rows []row
		for _, a := range got {
			rows = append(rows, row{a.Ref, a.Name, a.Kind})
		}
		deq(t, "the caller is left out", join(rows), want(true))
	})
}

func ptr[T any](v T) *T { return &v }
