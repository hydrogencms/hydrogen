package meilisearch

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"
)

// fakeMeili implements the subset of the Meilisearch API the module uses, with the semantics the module relies
// on: asynchronous tasks (the first poll of a task says "processing"), filters on declared filterable
// attributes only (AND, OR, NOT, parentheses, = != < <= > >=, IN, EXISTS; an array matches when any element
// does), prefix word search over name and text, sorting, facets, highlight and crop, page and offset modes,
// index swaps.
type fakeMeili struct {
	t   *testing.T
	key string

	mu      sync.Mutex
	indexes map[string]*fakeIndex
	tasks   []*fakeTask
	// failWrite makes a write task fail with the message when it returns one.
	failWrite func(method, path string) string
	// stall keeps every task in processing.
	stall bool
	// hideSwap makes swap tasks unreadable, as for a key limited to some indexes.
	hideSwap bool
	requests []string
}

type fakeIndex struct {
	docs     map[string]map[string]any
	settings map[string]any
}

type fakeTask struct {
	hidden bool
	status string
	polls  int
	code   string
	msg    string
}

func newFake(t *testing.T, key string) (*fakeMeili, *httptest.Server) {
	f := &fakeMeili{t: t, key: key, indexes: map[string]*fakeIndex{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeMeili) reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeMeili) errorReply(w http.ResponseWriter, status int, code, msg string) {
	f.reply(w, status, map[string]string{"message": msg, "code": code, "type": "invalid_request"})
}

// enqueue records a finished task that is reported as processing once.
func (f *fakeMeili) enqueue(w http.ResponseWriter, code, msg string) {
	status := "succeeded"
	if code != "" {
		status = "failed"
	}
	f.tasks = append(f.tasks, &fakeTask{status: status, code: code, msg: msg})
	f.reply(w, http.StatusAccepted, map[string]any{"taskUid": len(f.tasks) - 1, "status": "enqueued"})
}

func (f *fakeMeili) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if f.key != "" && r.Header.Get("Authorization") != "Bearer "+f.key {
		f.errorReply(w, http.StatusUnauthorized, "invalid_api_key", "The provided API key is invalid.")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var body json.RawMessage
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	write := func() bool {
		if f.failWrite != nil {
			if msg := f.failWrite(r.Method, r.URL.Path); msg != "" {
				f.enqueue(w, "internal", msg)
				return false
			}
		}
		return true
	}
	switch {
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "tasks":
		n, _ := strconv.Atoi(parts[1])
		if n < 0 || n >= len(f.tasks) {
			f.errorReply(w, 404, "task_not_found", "no such task")
			return
		}
		tk := f.tasks[n]
		if tk.hidden {
			f.errorReply(w, 404, "task_not_found", "no such task")
			return
		}
		tk.polls++
		out := map[string]any{"uid": n, "status": tk.status}
		if tk.polls == 1 || f.stall {
			out["status"] = "processing"
		} else if tk.status == "failed" {
			out["error"] = map[string]string{"message": tk.msg, "code": tk.code}
		}
		f.reply(w, 200, out)
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "indexes":
		var in struct{ UID, PrimaryKey string }
		_ = json.Unmarshal(body, &in)
		if !write() {
			return
		}
		if f.indexes[in.UID] != nil {
			f.enqueue(w, "index_already_exists", "Index `"+in.UID+"` already exists.")
			return
		}
		f.indexes[in.UID] = &fakeIndex{docs: map[string]map[string]any{}, settings: map[string]any{}}
		f.enqueue(w, "", "")
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "swap-indexes":
		var in []struct{ Indexes []string }
		_ = json.Unmarshal(body, &in)
		if !write() {
			return
		}
		for _, s := range in {
			a, b := f.indexes[s.Indexes[0]], f.indexes[s.Indexes[1]]
			if a == nil || b == nil {
				f.enqueue(w, "index_not_found", "swap of a missing index")
				return
			}
			f.indexes[s.Indexes[0]], f.indexes[s.Indexes[1]] = b, a
		}
		f.enqueue(w, "", "")
		f.tasks[len(f.tasks)-1].hidden = f.hideSwap
	case len(parts) >= 2 && parts[0] == "indexes":
		f.indexRoute(w, r, parts, body, write)
	default:
		f.errorReply(w, 404, "not_found", "no route "+r.URL.Path)
	}
}

func (f *fakeMeili) indexRoute(w http.ResponseWriter, r *http.Request, parts []string, body json.RawMessage, write func() bool) {
	uid := parts[1]
	idx := f.indexes[uid]
	missing := func() { f.errorReply(w, 404, "index_not_found", "Index `"+uid+"` not found.") }
	rest := strings.Join(parts[2:], "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		if idx == nil {
			missing()
			return
		}
		f.reply(w, 200, map[string]string{"uid": uid, "primaryKey": "id"})
	case rest == "" && r.Method == http.MethodDelete:
		if !write() {
			return
		}
		if idx == nil {
			f.enqueue(w, "index_not_found", "Index `"+uid+"` not found.")
			return
		}
		delete(f.indexes, uid)
		f.enqueue(w, "", "")
	case rest == "settings" && r.Method == http.MethodPatch:
		if !write() {
			return
		}
		if idx == nil {
			f.indexes[uid] = &fakeIndex{docs: map[string]map[string]any{}}
			idx = f.indexes[uid]
		}
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		idx.settings = in
		f.enqueue(w, "", "")
	case rest == "documents" && r.Method == http.MethodPost:
		if !write() {
			return
		}
		var in []map[string]any
		if err := json.Unmarshal(body, &in); err != nil {
			f.errorReply(w, 400, "bad_request", "documents must be an array")
			return
		}
		if idx == nil {
			f.indexes[uid] = &fakeIndex{docs: map[string]map[string]any{}}
			idx = f.indexes[uid]
		}
		for _, d := range in {
			id, _ := d["id"].(string)
			if id == "" {
				f.enqueue(w, "missing_document_id", "no id")
				return
			}
			idx.docs[id] = d
		}
		f.enqueue(w, "", "")
	case rest == "documents/delete" && r.Method == http.MethodPost:
		if !write() {
			return
		}
		if idx == nil {
			f.enqueue(w, "index_not_found", "Index `"+uid+"` not found.")
			return
		}
		var in struct{ Filter string }
		_ = json.Unmarshal(body, &in)
		expr, err := parseFilter(in.Filter)
		if err == nil {
			err = checkFilterable(expr, idx)
		}
		if err != nil {
			f.errorReply(w, 400, "invalid_document_filter", err.Error())
			return
		}
		for id, d := range idx.docs {
			if expr.eval(d) {
				delete(idx.docs, id)
			}
		}
		f.enqueue(w, "", "")
	case strings.HasPrefix(rest, "documents/") && r.Method == http.MethodGet:
		if idx == nil {
			missing()
			return
		}
		d, ok := idx.docs[strings.TrimPrefix(rest, "documents/")]
		if !ok {
			f.errorReply(w, 404, "document_not_found", "Document not found.")
			return
		}
		f.reply(w, 200, d)
	case rest == "search" && r.Method == http.MethodPost:
		if idx == nil {
			missing()
			return
		}
		f.search(w, idx, body)
	default:
		f.errorReply(w, 404, "not_found", "no route")
	}
}

type fakeSearch struct {
	Q                     string   `json:"q"`
	Filter                string   `json:"filter"`
	Sort                  []string `json:"sort"`
	Facets                []string `json:"facets"`
	Limit                 *int     `json:"limit"`
	Offset                int      `json:"offset"`
	Page                  int      `json:"page"`
	HitsPerPage           int      `json:"hitsPerPage"`
	Retrieve              []string `json:"attributesToRetrieve"`
	Highlight             []string `json:"attributesToHighlight"`
	Crop                  []string `json:"attributesToCrop"`
	CropLength            int      `json:"cropLength"`
	Pre                   string   `json:"highlightPreTag"`
	Post                  string   `json:"highlightPostTag"`
	ShowRankingScore      bool     `json:"showRankingScore"`
	ShowRankingScoreExtra bool     `json:"-"`
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func matchScore(d map[string]any, terms []string) (float64, bool) {
	name, _ := d["name"].(string)
	text, _ := d["text"].(string)
	score := 1.0
	for _, t := range terms {
		inName, inText := false, false
		for _, w := range words(name) {
			inName = inName || strings.HasPrefix(w, t)
		}
		for _, w := range words(text) {
			inText = inText || strings.HasPrefix(w, t)
		}
		if !inName && !inText {
			return 0, false
		}
		if !inName {
			score -= 0.5 / float64(len(terms))
		}
	}
	return score, true
}

func (f *fakeMeili) search(w http.ResponseWriter, idx *fakeIndex, body json.RawMessage) {
	var q fakeSearch
	_ = json.Unmarshal(body, &q)
	expr, err := parseFilter(q.Filter)
	if err == nil {
		err = checkFilterable(expr, idx)
	}
	if err != nil {
		f.errorReply(w, 400, "invalid_search_filter", err.Error())
		return
	}
	for _, s := range q.Sort {
		attr, _, _ := strings.Cut(s, ":")
		if !slices.Contains(toStrings(idx.settings["sortableAttributes"]), attr) {
			f.errorReply(w, 400, "invalid_search_sort", "attribute "+attr+" is not sortable")
			return
		}
	}
	terms := words(q.Q)
	type match struct {
		doc   map[string]any
		score float64
	}
	var ms []match
	for _, d := range idx.docs {
		if !expr.eval(d) {
			continue
		}
		if s, ok := matchScore(d, terms); ok {
			ms = append(ms, match{d, s})
		}
	}
	slices.SortFunc(ms, func(a, b match) int {
		for _, s := range q.Sort {
			attr, dir, _ := strings.Cut(s, ":")
			c := compareAny(a.doc[attr], b.doc[attr])
			if dir == "desc" {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		if len(q.Sort) == 0 && a.score != b.score {
			if a.score > b.score {
				return -1
			}
			return 1
		}
		return strings.Compare(a.doc["id"].(string), b.doc["id"].(string))
	})
	const maxTotal = 10000
	total := min(len(ms), maxTotal)
	out := map[string]any{"query": q.Q}
	var window []match
	if q.HitsPerPage > 0 {
		lo := min((q.Page-1)*q.HitsPerPage, total)
		window = ms[lo:min(lo+q.HitsPerPage, total)]
		out["totalHits"], out["page"], out["hitsPerPage"] = total, q.Page, q.HitsPerPage
		out["totalPages"] = int(math.Ceil(float64(total) / float64(q.HitsPerPage)))
	} else {
		limit := 20
		if q.Limit != nil {
			limit = *q.Limit
		}
		lo := min(q.Offset, total)
		window = ms[lo:min(lo+limit, total)]
		out["estimatedTotalHits"], out["offset"], out["limit"] = total, q.Offset, limit
	}
	hits := []map[string]any{}
	for _, m := range window {
		h := map[string]any{}
		for _, a := range q.Retrieve {
			h[a] = m.doc[a]
		}
		if q.ShowRankingScore {
			h["_rankingScore"] = m.score
		}
		fm := map[string]any{}
		for _, a := range q.Retrieve {
			if s, ok := m.doc[a].(string); ok {
				fm[a] = s
			}
		}
		for _, a := range q.Crop {
			if s, ok := m.doc[a].(string); ok {
				fm[a] = crop(s, terms, q.CropLength)
			}
		}
		for _, a := range q.Highlight {
			if s, ok := fm[a].(string); ok {
				fm[a] = markWords(s, terms, q.Pre, q.Post)
			}
		}
		if len(q.Highlight) > 0 || len(q.Crop) > 0 {
			h["_formatted"] = fm
		}
		hits = append(hits, h)
	}
	out["hits"] = hits
	if len(q.Facets) > 0 {
		dist := map[string]map[string]int{}
		for _, a := range q.Facets {
			if !slices.Contains(toStrings(idx.settings["filterableAttributes"]), a) {
				f.errorReply(w, 400, "invalid_search_facets", "attribute "+a+" is not filterable")
				return
			}
			dist[a] = map[string]int{}
			for _, m := range ms {
				switch v := m.doc[a].(type) {
				case []any:
					for _, e := range v {
						dist[a][fmt.Sprint(e)]++
					}
				case nil:
				default:
					dist[a][fmt.Sprint(v)]++
				}
			}
		}
		out["facetDistribution"] = dist
	}
	f.reply(w, 200, out)
}

func toStrings(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, e := range l {
			out = append(out, fmt.Sprint(e))
		}
	}
	return out
}

func compareAny(a, b any) int {
	if x, ok := a.(float64); ok {
		y, _ := b.(float64)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

// markWords wraps each word that starts with a term.
func markWords(s string, terms []string, pre, post string) string {
	var sb strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); {
		if !unicode.IsLetter(rs[i]) && !unicode.IsDigit(rs[i]) {
			sb.WriteRune(rs[i])
			i++
			continue
		}
		j := i
		for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j])) {
			j++
		}
		word := string(rs[i:j])
		hit := false
		for _, t := range terms {
			hit = hit || strings.HasPrefix(strings.ToLower(word), t)
		}
		if hit {
			sb.WriteString(pre + word + post)
		} else {
			sb.WriteString(word)
		}
		i = j
	}
	return sb.String()
}

// crop keeps n words around the first match, with an ellipsis where text was cut.
func crop(s string, terms []string, n int) string {
	ws := strings.Fields(s)
	if n <= 0 || len(ws) <= n {
		return s
	}
	first := 0
	for i, w := range ws {
		if slices.ContainsFunc(terms, func(t string) bool { return strings.HasPrefix(strings.ToLower(w), t) }) {
			first = i
			break
		}
	}
	lo := max(0, min(first-n/2, len(ws)-n))
	out := strings.Join(ws[lo:lo+n], " ")
	if lo > 0 {
		out = "…" + out
	}
	if lo+n < len(ws) {
		out += "…"
	}
	return out
}

// ---- filter expressions ----

type fexpr interface{ eval(doc map[string]any) bool }

type (
	fAnd  struct{ a, b fexpr }
	fOr   struct{ a, b fexpr }
	fNot  struct{ e fexpr }
	fCond struct {
		attr string
		op   string // = != < <= > >= IN EXISTS
		vals []any
		strs []string // vals printed once: a delete filter can list hundreds of ids
	}
)

func cond(attr, op string, vals []any) fCond {
	c := fCond{attr: attr, op: op, vals: vals, strs: make([]string, len(vals))}
	for i, v := range vals {
		c.strs[i] = fmt.Sprint(v)
	}
	return c
}

func (e fAnd) eval(d map[string]any) bool { return e.a.eval(d) && e.b.eval(d) }
func (e fOr) eval(d map[string]any) bool  { return e.a.eval(d) || e.b.eval(d) }
func (e fNot) eval(d map[string]any) bool { return !e.e.eval(d) }

func (c fCond) eval(d map[string]any) bool {
	v, present := d[c.attr]
	if c.op == "EXISTS" {
		return present && v != nil
	}
	var elems []any
	if l, ok := v.([]any); ok {
		elems = l
	} else if present && v != nil {
		elems = []any{v}
	}
	for _, e := range elems {
		es := fmt.Sprint(e)
		for i, want := range c.vals {
			cmp, comparable := 0, true
			switch x := e.(type) {
			case float64:
				y, ok := want.(float64)
				comparable = ok
				if ok {
					cmp = compareAny(x, y)
				}
			default:
				cmp = strings.Compare(es, c.strs[i])
			}
			if !comparable {
				continue
			}
			ok := false
			switch c.op {
			case "=", "IN":
				ok = cmp == 0
			case "!=":
				ok = cmp != 0
			case "<":
				ok = cmp < 0
			case "<=":
				ok = cmp <= 0
			case ">":
				ok = cmp > 0
			case ">=":
				ok = cmp >= 0
			}
			if ok {
				return true
			}
		}
	}
	// `!=` holds when the field is absent.
	return c.op == "!=" && len(elems) == 0
}

func checkFilterable(e fexpr, idx *fakeIndex) error {
	allowed := toStrings(idx.settings["filterableAttributes"])
	var walk func(e fexpr) error
	walk = func(e fexpr) error {
		switch x := e.(type) {
		case fAnd:
			return errJoin(walk(x.a), walk(x.b))
		case fOr:
			return errJoin(walk(x.a), walk(x.b))
		case fNot:
			return walk(x.e)
		case fCond:
			if !slices.Contains(allowed, x.attr) {
				return fmt.Errorf("Attribute `%s` is not filterable.", x.attr)
			}
		}
		return nil
	}
	return walk(e)
}

func errJoin(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

type ftok struct {
	kind string // str num word punct
	text string
	num  float64
}

func lexFilter(s string) ([]ftok, error) {
	var out []ftok
	rs := []rune(s)
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '"' || r == '\'':
			q := r
			var sb strings.Builder
			i++
			closed := false
			for ; i < len(rs); i++ {
				if rs[i] == '\\' && i+1 < len(rs) {
					i++
					sb.WriteRune(rs[i])
					continue
				}
				if rs[i] == q {
					closed = true
					i++
					break
				}
				sb.WriteRune(rs[i])
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string in filter")
			}
			out = append(out, ftok{kind: "str", text: sb.String()})
		case strings.ContainsRune("()[],", r):
			out = append(out, ftok{kind: "punct", text: string(r)})
			i++
		case strings.ContainsRune("=!<>", r):
			j := i + 1
			if j < len(rs) && rs[j] == '=' {
				j++
			}
			out = append(out, ftok{kind: "punct", text: string(rs[i:j])})
			i = j
		default:
			j := i
			for j < len(rs) && !unicode.IsSpace(rs[j]) && !strings.ContainsRune("()[],=!<>\"'", rs[j]) {
				j++
			}
			if j == i {
				return nil, fmt.Errorf("unexpected %q in filter", r)
			}
			w := string(rs[i:j])
			if n, err := strconv.ParseFloat(w, 64); err == nil {
				out = append(out, ftok{kind: "num", text: w, num: n})
			} else {
				out = append(out, ftok{kind: "word", text: w})
			}
			i = j
		}
	}
	return out, nil
}

type fparser struct {
	toks []ftok
	pos  int
}

func parseFilter(s string) (fexpr, error) {
	if strings.TrimSpace(s) == "" {
		return fAnd{fCond{op: "EXISTS", attr: "id"}, fCond{op: "EXISTS", attr: "id"}}, nil
	}
	toks, err := lexFilter(s)
	if err != nil {
		return nil, err
	}
	p := &fparser{toks: toks}
	e, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.pos != len(toks) {
		return nil, fmt.Errorf("unexpected %q in filter", toks[p.pos].text)
	}
	return e, nil
}

func (p *fparser) peek() ftok {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	return ftok{}
}

func (p *fparser) isWord(w string) bool {
	t := p.peek()
	return t.kind == "word" && strings.EqualFold(t.text, w)
}

func (p *fparser) or() (fexpr, error) {
	e, err := p.and()
	for err == nil && p.isWord("OR") {
		p.pos++
		var r fexpr
		if r, err = p.and(); err == nil {
			e = fOr{e, r}
		}
	}
	return e, err
}

func (p *fparser) and() (fexpr, error) {
	e, err := p.not()
	for err == nil && p.isWord("AND") {
		p.pos++
		var r fexpr
		if r, err = p.not(); err == nil {
			e = fAnd{e, r}
		}
	}
	return e, err
}

func (p *fparser) not() (fexpr, error) {
	if p.isWord("NOT") {
		p.pos++
		e, err := p.not()
		return fNot{e}, err
	}
	return p.primary()
}

func (p *fparser) value() (any, error) {
	t := p.peek()
	p.pos++
	switch t.kind {
	case "str":
		return t.text, nil
	case "num":
		return t.num, nil
	case "word":
		switch strings.ToLower(t.text) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return t.text, nil
	}
	return nil, fmt.Errorf("expected a value in filter")
}

func (p *fparser) primary() (fexpr, error) {
	t := p.peek()
	if t.kind == "punct" && t.text == "(" {
		p.pos++
		e, err := p.or()
		if err != nil {
			return nil, err
		}
		if c := p.peek(); c.text != ")" {
			return nil, fmt.Errorf("expected ) in filter")
		}
		p.pos++
		return e, nil
	}
	if t.kind != "word" {
		return nil, fmt.Errorf("expected an attribute in filter, got %q", t.text)
	}
	p.pos++
	attr := t.text
	switch o := p.peek(); {
	case p.isWord("EXISTS"):
		p.pos++
		return fCond{attr: attr, op: "EXISTS"}, nil
	case p.isWord("IN"):
		p.pos++
		if c := p.peek(); c.text != "[" {
			return nil, fmt.Errorf("expected [ after IN")
		}
		p.pos++
		var vals []any
		for p.peek().text != "]" {
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
			if p.peek().text == "," {
				p.pos++
			}
			if p.pos >= len(p.toks) {
				return nil, fmt.Errorf("unterminated IN list")
			}
		}
		p.pos++
		return cond(attr, "IN", vals), nil
	case o.kind == "punct" && slices.Contains([]string{"=", "!=", "<", "<=", ">", ">="}, o.text):
		p.pos++
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		return cond(attr, o.text, []any{v}), nil
	}
	return nil, fmt.Errorf("expected an operator after %q in filter", attr)
}
