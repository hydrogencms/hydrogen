var ni = Object.defineProperty;
var In = (e) => {
  throw TypeError(e);
};
var si = (e, t, r) => t in e ? ni(e, t, { enumerable: !0, configurable: !0, writable: !0, value: r }) : e[t] = r;
var M = (e, t, r) => si(e, typeof t != "symbol" ? t + "" : t, r), Vr = (e, t, r) => t.has(e) || In("Cannot " + r);
var o = (e, t, r) => (Vr(e, t, "read from private field"), r ? r.call(e) : t.get(e)), E = (e, t, r) => t.has(e) ? In("Cannot add the same private member more than once") : t instanceof WeakSet ? t.add(e) : t.set(e, r), w = (e, t, r, n) => (Vr(e, t, "write to private field"), n ? n.call(e, r) : t.set(e, r), r), $ = (e, t, r) => (Vr(e, t, "access private method"), r);
var Xn;
typeof window < "u" && ((Xn = window.__svelte ?? (window.__svelte = {})).v ?? (Xn.v = /* @__PURE__ */ new Set())).add("5");
const ii = 1, li = 2, ai = 16, oi = 1, fi = 4, ui = 8, ci = 16, hi = 1, di = 2, es = "[", gn = "[!", Ln = "[?", mn = "]", ut = {}, B = Symbol("uninitialized"), vi = "http://www.w3.org/1999/xhtml", ts = !1;
var rs = Array.isArray, pi = Array.prototype.indexOf, nr = Array.prototype.includes, Dr = Array.from, Tr = Object.keys, sr = Object.defineProperty, $t = Object.getOwnPropertyDescriptor, _i = Object.getOwnPropertyDescriptors, gi = Object.prototype, mi = Array.prototype, ns = Object.getPrototypeOf, Dn = Object.isExtensible;
const bi = () => {
};
function yi(e) {
  for (var t = 0; t < e.length; t++)
    e[t]();
}
function ss() {
  var e, t, r = new Promise((n, s) => {
    e = n, t = s;
  });
  return { promise: r, resolve: e, reject: t };
}
const Z = 2, Ft = 4, Fr = 8, is = 1 << 24, Oe = 16, Me = 32, tt = 64, Qr = 128, bn = 256, Ne = 512, H = 1024, L = 2048, ye = 4096, ee = 8192, G = 16384, kt = 32768, Ar = 1 << 25, zt = 65536, Or = 1 << 17, wi = 1 << 18, Tt = 1 << 19, $i = 1 << 20, Je = 1 << 25, Rr = 1 << 21, Rt = 1 << 22, ot = 1 << 23, $r = Symbol("$state"), ls = Symbol("component"), as = Symbol("legacy props"), xi = Symbol(""), xr = Symbol("attributes"), Ei = Symbol("class"), en = Symbol("style"), Gt = Symbol("text"), Qt = Symbol("form reset"), vr = new class extends Error {
  constructor() {
    super(...arguments);
    M(this, "name", "StaleReactionError");
    M(this, "message", "The reaction that called `getAbortSignal()` was re-run or destroyed");
  }
}();
var Zn;
const os = (
  // We gotta write it like this because after downleveling the pure comment may end up in the wrong location
  !!((Zn = globalThis.document) != null && Zn.contentType) && /* @__PURE__ */ globalThis.document.contentType.includes("xml")
), zr = 3, pr = 8;
function Si() {
  console.warn("https://svelte.dev/e/derived_inert");
}
function qr(e) {
  console.warn("https://svelte.dev/e/hydration_mismatch");
}
function ki() {
  console.warn("https://svelte.dev/e/svelte_boundary_reset_noop");
}
let R = !1;
function Xe(e) {
  R = e;
}
let A;
function ne(e) {
  if (e === null)
    throw qr(), ut;
  return A = e;
}
function Br() {
  return ne(/* @__PURE__ */ Ve(A));
}
function we(e) {
  if (R) {
    if (/* @__PURE__ */ Ve(A) !== null)
      throw qr(), ut;
    A = e;
  }
}
function yn(e = 1) {
  if (R) {
    for (var t = e, r = A; t--; )
      r = /** @type {TemplateNode} */
      /* @__PURE__ */ Ve(r);
    A = r;
  }
}
function Nr(e = !0) {
  for (var t = 0, r = A; ; ) {
    if (r.nodeType === pr) {
      var n = (
        /** @type {Comment} */
        r.data
      );
      if (n === mn) {
        if (t === 0) return r;
        t -= 1;
      } else (n === es || n === gn || // "[1", "[2", etc. for if blocks
      n[0] === "[" && !isNaN(Number(n.slice(1)))) && (t += 1);
    }
    var s = (
      /** @type {TemplateNode} */
      /* @__PURE__ */ Ve(r)
    );
    e && r.remove(), r = s;
  }
}
function wn(e) {
  if (!e || e.nodeType !== pr)
    throw qr(), ut;
  return (
    /** @type {Comment} */
    e.data
  );
}
function fs(e) {
  return e === this.v;
}
function Ti(e, t) {
  return e != e ? t == t : e !== t || e !== null && typeof e == "object" || typeof e == "function";
}
function us(e) {
  return !Ti(e, this.v);
}
function Ai() {
  throw new Error("https://svelte.dev/e/async_derived_orphan");
}
function Oi(e, t, r) {
  throw new Error("https://svelte.dev/e/each_key_duplicate");
}
function Ri(e) {
  throw new Error("https://svelte.dev/e/effect_in_teardown");
}
function Ni() {
  throw new Error("https://svelte.dev/e/effect_in_unowned_derived");
}
function Ci(e) {
  throw new Error("https://svelte.dev/e/effect_orphan");
}
function Mi() {
  throw new Error("https://svelte.dev/e/effect_update_depth_exceeded");
}
function Pi() {
  throw new Error("https://svelte.dev/e/hydration_failed");
}
function ji(e) {
  throw new Error("https://svelte.dev/e/props_invalid_value");
}
function Ii() {
  throw new Error("https://svelte.dev/e/state_descriptors_fixed");
}
function Li() {
  throw new Error("https://svelte.dev/e/state_prototype_fixed");
}
function Di() {
  throw new Error("https://svelte.dev/e/state_unsafe_mutation");
}
function Fi() {
  throw new Error("https://svelte.dev/e/svelte_boundary_reset_onerror");
}
let zi = !1, se = null;
function qt(e) {
  se = e;
}
function Ee(e, t = !1, r) {
  se = {
    p: se,
    i: !1,
    c: null,
    e: null,
    s: e,
    x: null,
    r: (
      /** @type {Effect} */
      T
    ),
    l: null
  };
}
function Se(e) {
  var t = (
    /** @type {ComponentContext} */
    se
  ), r = t.e;
  if (r !== null) {
    t.e = null;
    for (var n of r)
      Ps(n);
  }
  return e !== void 0 && (t.x = e), t.i = !0, se = t.p, cs(e);
}
function cs(e = {}) {
  return sr(e, ls, { value: !0 }), e;
}
function hs() {
  return !0;
}
let dt = [];
function ds() {
  var e = dt;
  dt = [], yi(e);
}
function Ze(e) {
  if (dt.length === 0 && !er) {
    var t = dt;
    queueMicrotask(() => {
      t === dt && ds();
    });
  }
  dt.push(e);
}
function qi() {
  for (; dt.length > 0; )
    ds();
}
const Bi = -7169;
function I(e, t) {
  e.f = e.f & Bi | t;
}
function $n(e) {
  (e.f & Ne) !== 0 || e.deps === null ? I(e, H) : I(e, ye);
}
function vs(e, t, r) {
  (e.f & L) !== 0 ? t.add(e) : (e.f & ye) !== 0 && r.add(e), I(e, H);
}
function Hi(e) {
  R && /* @__PURE__ */ rt(e) !== null && Tn(e);
}
let Fn = !1;
function ps() {
  Fn || (Fn = !0, document.addEventListener(
    "reset",
    (e) => {
      Promise.resolve().then(() => {
        var t;
        if (!e.defaultPrevented)
          for (
            const r of
            /**@type {HTMLFormElement} */
            e.target.elements
          )
            (t = r[Qt]) == null || t.call(r);
      });
    },
    // In the capture phase to guarantee we get noticed of it (no possibility of stopPropagation)
    { capture: !0 }
  ));
}
function Yt(e) {
  var t = S, r = T;
  xe(null), He(null);
  try {
    return e();
  } finally {
    xe(t), He(r);
  }
}
function Vi(e, t, r, n = r) {
  e.addEventListener(t, () => Yt(r));
  const s = (
    /** @type {any} */
    e[Qt]
  );
  s ? e[Qt] = () => {
    s(), n(!0);
  } : e[Qt] = () => n(!0), ps();
}
function Yi(e, t, r, n) {
  const s = ir;
  var i = e.filter((v) => !v.settled), l = t.map(s);
  if (r.length === 0 && i.length === 0) {
    n(l);
    return;
  }
  var a = (
    /** @type {Effect} */
    T
  ), f = Ui(), u = i.length === 1 ? i[0].promise : i.length > 1 ? Promise.all(i.map((v) => v.promise)) : null;
  function d(v) {
    if ((a.f & G) === 0) {
      f();
      try {
        n([...l, ...v]);
      } catch (c) {
        ze(c, a);
      }
      Cr();
    }
  }
  var h = _s();
  if (r.length === 0) {
    u.then(() => d([])).finally(h);
    return;
  }
  function p() {
    Promise.all(r.map((v) => /* @__PURE__ */ Gi(v))).then(d).catch((v) => ze(v, a)).finally(h);
  }
  u ? u.then(() => {
    if ((a.f & G) !== 0) {
      h();
      return;
    }
    f(), p(), Cr();
  }) : p();
}
function Ui() {
  var e = (
    /** @type {Effect} */
    T
  ), t = S, r = se, n = (
    /** @type {Batch} */
    y
  );
  return function(i = !0) {
    He(e), xe(t), qt(r), i && (e.f & G) === 0 && (n == null || n.activate(), n == null || n.apply());
  };
}
function Cr(e = !0) {
  He(null), xe(null), qt(null), e && (y == null || y.deactivate());
}
function _s() {
  var e = (
    /** @type {Effect} */
    T
  ), t = e.b, r = (
    /** @type {Batch} */
    y
  ), n = !!(t != null && t.is_rendered());
  return t == null || t.update_pending_count(1, r), r.increment(n, e), () => {
    t == null || t.update_pending_count(-1, r), r.decrement(n, e);
  };
}
// @__NO_SIDE_EFFECTS__
function ir(e) {
  var t = Z | L;
  return T !== null && (T.f |= Tt), {
    ctx: se,
    deps: null,
    effects: null,
    equals: fs,
    f: t,
    fn: e,
    reactions: null,
    rv: 0,
    v: (
      /** @type {V} */
      B
    ),
    wv: 0,
    parent: T,
    ac: null
  };
}
const Wt = Symbol("obsolete");
// @__NO_SIDE_EFFECTS__
function Gi(e, t, r) {
  let n = (
    /** @type {Effect | null} */
    T
  );
  n === null && Ai();
  var s = (
    /** @type {Promise<V>} */
    /** @type {unknown} */
    void 0
  ), i = St(
    /** @type {V} */
    B
  ), l = !S, a = /* @__PURE__ */ new Set();
  return al(() => {
    var v, c;
    var f = (
      /** @type {Effect} */
      T
    ), u = ss();
    s = u.promise;
    try {
      Promise.resolve(e()).then(u.resolve, (_) => {
        _ !== vr && u.reject(_);
      }).finally(Cr);
    } catch (_) {
      u.reject(_), Cr();
    }
    var d = (
      /** @type {Batch} */
      y
    );
    if (l) {
      if ((f.f & kt) !== 0)
        var h = _s();
      if (
        // boundary can be null if the async derived is inside an $effect.root not connected to the component render tree
        (v = n.b) != null && v.is_rendered()
      )
        (c = d.async_deriveds.get(f)) == null || c.reject(Wt);
      else
        for (const _ of a.values())
          _.reject(Wt);
      a.add(u), d.async_deriveds.set(f, u);
    }
    const p = (_, g = void 0) => {
      h == null || h(), a.delete(u), g !== Wt && (d.activate(), g ? (i.f |= ot, Bt(i, g)) : ((i.f & ot) !== 0 && (i.f ^= ot), Bt(i, _)), d.deactivate());
    };
    u.promise.then(p, (_) => p(null, _ || "unknown"));
  }), Cs(() => {
    for (const f of a)
      f.reject(Wt);
  }), new Promise((f) => {
    function u(d) {
      function h() {
        d === s ? f(i) : u(s);
      }
      d.then(h, h);
    }
    u(s);
  });
}
// @__NO_SIDE_EFFECTS__
function Qe(e) {
  const t = /* @__PURE__ */ ir(e);
  return Fs(t), t;
}
// @__NO_SIDE_EFFECTS__
function gs(e) {
  const t = /* @__PURE__ */ ir(e);
  return t.equals = us, t;
}
function Wi(e) {
  var t = e.effects;
  if (t !== null) {
    e.effects = null;
    for (var r = 0; r < t.length; r += 1)
      te(
        /** @type {Effect} */
        t[r]
      );
  }
}
function xn(e) {
  var t, r = T, n = e.parent;
  if (!nt && n !== null && e.v !== B && // if it was never evaluated before, it's guaranteed to fail downstream, so we try to execute instead
  (n.f & (G | ee)) !== 0)
    return Si(), e.v;
  He(n);
  try {
    Wi(e), t = Hs(e);
  } finally {
    He(r);
  }
  return t;
}
function ms(e) {
  var t = xn(e);
  if (!e.equals(t) && (e.wv = qs(), (!(y != null && y.is_fork) || e.deps === null) && (y !== null ? (y.capture(e, t, !0), ft == null || ft.capture(e, t, !0)) : e.v = t, e.deps === null))) {
    I(e, H);
    return;
  }
  nt || (U !== null ? (Rn() || y != null && y.is_fork) && U.set(e, t) : $n(e));
}
function Ki(e) {
  var t;
  if (e.effects !== null)
    for (const r of e.effects)
      (r.teardown || r.ac) && ((t = r.teardown) == null || t.call(r), r.ac !== null && Yt(() => {
        r.ac.abort(vr), r.ac = null;
      }), r.fn !== null && (r.teardown = bi), lr(r, 0), Mn(r));
}
function bs(e) {
  if (e.effects !== null)
    for (const t of e.effects)
      t.teardown && t.fn !== null && Ht(t);
}
let Yr = null, At = null, y = null, ft = null, U = null, tn = null, er = !1, Ur = !1, tr = null, Er = null;
var zn = 0;
let Ji = 1;
var Ct, lt, gt, Mt, Pt, jt, Ue, It, oe, or, Ge, Te, Le, Lt, mt, N, rn, nn, Kt, sn, ys, ws, Ot, Xi, Jt;
const jr = class jr {
  constructor() {
    E(this, N);
    M(this, "id", Ji++);
    /** True as soon as `#process` was called */
    E(this, Ct, !1);
    M(this, "linked", !0);
    /** @type {Batch | null} */
    E(this, lt, null);
    /** @type {Batch | null} */
    E(this, gt, null);
    /** @type {Map<Effect, ReturnType<typeof deferred<any>>>} */
    M(this, "async_deriveds", /* @__PURE__ */ new Map());
    /**
     * The current values of any signals that are updated in this batch.
     * Tuple format: [value, is_derived] (note: is_derived is false for deriveds, too, if they were overridden via assignment)
     * They keys of this map are identical to `this.#previous`
     * @type {Map<Value, [any, boolean]>}
     */
    M(this, "current", /* @__PURE__ */ new Map());
    /**
     * The values of any signals (sources and deriveds) that are updated in this batch _before_ those updates took place.
     * They keys of this map are identical to `this.#current`
     * @type {Map<Value, any>}
     */
    M(this, "previous", /* @__PURE__ */ new Map());
    /**
     * When the batch is committed (and the DOM is updated), we need to remove old branches
     * and append new ones by calling the functions added inside (if/each/key/etc) blocks
     * @type {Set<(batch: Batch) => void>}
     */
    E(this, Mt, /* @__PURE__ */ new Set());
    /**
     * If a fork is discarded, we need to destroy any effects that are no longer needed
     * @type {Set<(batch: Batch) => void>}
     */
    E(this, Pt, /* @__PURE__ */ new Set());
    /**
     * The number of async effects that are currently in flight
     */
    E(this, jt, 0);
    /**
     * Async effects that are currently in flight, _not_ inside a pending boundary
     * @type {Map<Effect, number>}
     */
    E(this, Ue, /* @__PURE__ */ new Map());
    /**
     * A deferred that resolves when the batch is committed, used with `settled()`
     * TODO replace with Promise.withResolvers once supported widely enough
     * @type {{ promise: Promise<void>, resolve: (value?: any) => void, reject: (reason: unknown) => void } | null}
     */
    E(this, It, null);
    /**
     * Effects that were scheduled in this batch but not yet 'resolved' into the
     * root effects that need to be flushed. Resolving — the upwards traversal that
     * marks the path to each effect on the shared effect tree (see #resolve) — is
     * deferred until the batch is processed, so that the markers are created and
     * consumed within a single traversal. Scheduling into other batches (which can
     * happen concurrently, e.g. while a batch is committed) can therefore never
     * observe (and be confused by) this batch's markers.
     * May contain duplicates — deduplication happens during resolving
     * @type {Effect[]}
     */
    E(this, oe, []);
    /**
     * Effects created while this batch was active.
     * @type {Effect[]}
     */
    E(this, or, []);
    /**
     * Deferred effects (which run after async work has completed) that are DIRTY
     * @type {Set<Effect>}
     */
    E(this, Ge, /* @__PURE__ */ new Set());
    /**
     * Deferred effects that are MAYBE_DIRTY
     * @type {Set<Effect>}
     */
    E(this, Te, /* @__PURE__ */ new Set());
    /**
     * A map of branches that still exist, but will be destroyed when this batch
     * is committed — we skip over these during `process`.
     * The value contains child effects that were dirty/maybe_dirty before being reset,
     * so they can be rescheduled if the branch survives.
     * @type {Map<Effect, { d: Effect[], m: Effect[] }>}
     */
    E(this, Le, /* @__PURE__ */ new Map());
    /**
     * Inverse of #skipped_branches which we need to tell prior batches to unskip them when committing
     * @type {Set<Effect>}
     */
    E(this, Lt, /* @__PURE__ */ new Set());
    M(this, "is_fork", !1);
    E(this, mt, !1);
    At === null ? Yr = At = this : (w(At, gt, this), w(this, lt, At)), At = this;
  }
  /**
   * Add an effect to the #skipped_branches map and reset its children
   * @param {Effect} effect
   */
  skip_effect(t) {
    o(this, Le).has(t) || o(this, Le).set(t, { d: [], m: [] }), o(this, Lt).delete(t);
  }
  /**
   * Remove an effect from the #skipped_branches map and reschedule
   * any tracked dirty/maybe_dirty child effects
   * @param {Effect} effect
   * @param {(e: Effect) => void} callback
   */
  unskip_effect(t, r = (n) => this.schedule(n)) {
    var n = o(this, Le).get(t);
    if (n) {
      o(this, Le).delete(t);
      for (var s of n.d)
        I(s, L), r(s);
      for (s of n.m)
        I(s, ye), r(s);
    }
    o(this, Lt).add(t);
  }
  /**
   * Associate a change to a given source with the current
   * batch, noting its previous and current values
   * @param {Value} source
   * @param {any} value
   * @param {boolean} [is_derived]
   */
  capture(t, r, n = !1) {
    t.v !== B && !this.previous.has(t) && this.previous.set(t, t.v), (t.f & ot) === 0 && (this.current.set(t, [r, n]), U == null || U.set(t, r)), this.is_fork || (t.v = r);
  }
  activate() {
    y = this;
  }
  deactivate() {
    y = null, U = null;
  }
  flush() {
    try {
      Ur = !0, y = this, $(this, N, Kt).call(this);
    } finally {
      zn = 0, tn = null, tr = null, Er = null, Ur = !1, y = null, U = null, qe.clear();
    }
  }
  discard() {
    var t;
    for (const r of o(this, Pt)) r(this);
    o(this, Pt).clear();
    for (const r of this.async_deriveds.values())
      r.reject(Wt);
    $(this, N, Jt).call(this), (t = o(this, It)) == null || t.resolve();
  }
  /**
   * @param {Effect} effect
   */
  register_created_effect(t) {
    o(this, or).push(t);
  }
  /**
   * @param {boolean} blocking
   * @param {Effect} effect
   */
  increment(t, r) {
    if (w(this, jt, o(this, jt) + 1), t) {
      let n = o(this, Ue).get(r) ?? 0;
      o(this, Ue).set(r, n + 1);
    }
  }
  /**
   * @param {boolean} blocking
   * @param {Effect} effect
   */
  decrement(t, r) {
    if (w(this, jt, o(this, jt) - 1), t) {
      let n = o(this, Ue).get(r) ?? 0;
      n === 1 ? o(this, Ue).delete(r) : o(this, Ue).set(r, n - 1);
    }
    o(this, mt) || (w(this, mt, !0), Ze(() => {
      w(this, mt, !1), this.linked && this.flush();
    }));
  }
  /**
   * @param {Set<Effect>} dirty_effects
   * @param {Set<Effect>} maybe_dirty_effects
   */
  transfer_effects(t, r) {
    for (const n of t)
      o(this, Ge).add(n);
    for (const n of r)
      o(this, Te).add(n);
    t.clear(), r.clear();
  }
  /** @param {(batch: Batch) => void} fn */
  oncommit(t) {
    o(this, Mt).add(t);
  }
  /** @param {(batch: Batch) => void} fn */
  ondiscard(t) {
    o(this, Pt).add(t);
  }
  settled() {
    return (o(this, It) ?? w(this, It, ss())).promise;
  }
  static ensure() {
    if (y === null) {
      const t = y = new jr();
      !Ur && !er && Ze(() => {
        o(t, Ct) || t.flush();
      });
    }
    return y;
  }
  apply() {
    {
      U = null;
      return;
    }
  }
  /**
   *
   * @param {Effect} effect
   */
  schedule(t) {
    var r;
    if (tn = t, (r = t.b) != null && r.is_pending && (t.f & (Ft | Fr | is)) !== 0 && (t.f & kt) === 0) {
      t.b.defer_effect(t);
      return;
    }
    o(this, oe).push(t);
  }
};
Ct = new WeakMap(), lt = new WeakMap(), gt = new WeakMap(), Mt = new WeakMap(), Pt = new WeakMap(), jt = new WeakMap(), Ue = new WeakMap(), It = new WeakMap(), oe = new WeakMap(), or = new WeakMap(), Ge = new WeakMap(), Te = new WeakMap(), Le = new WeakMap(), Lt = new WeakMap(), mt = new WeakMap(), N = new WeakSet(), rn = function() {
  if (this.is_fork) return !0;
  for (const n of o(this, Ue).keys()) {
    for (var t = n, r = !1; t.parent !== null; ) {
      if (o(this, Le).has(t)) {
        r = !0;
        break;
      }
      t = t.parent;
    }
    if (!r)
      return !0;
  }
  return !1;
}, /**
 * Convert the effects that were scheduled in this batch into the root effects
 * that need to be traversed, marking the path to each effect (by clearing the
 * `CLEAN` flag on ancestor branches) so that the traversal can find them.
 * This happens right before traversal rather than at scheduling time, so that
 * the markers left on the (shared) effect tree are created and consumed within
 * a single traversal — scheduling into other batches can never observe them
 * @returns {Effect[]}
 */
nn = function() {
  var t = [];
  for (const i of o(this, oe))
    if (!((i.f & G) !== 0 || (i.f & (L | ye)) === 0)) {
      for (var r = i, n = !1; r.parent !== null; ) {
        r = r.parent;
        var s = r.f;
        if ((s & (tt | Me)) !== 0) {
          if ((s & H) === 0) {
            n = !0;
            break;
          }
          r.f ^= H;
        }
      }
      n || t.push(r);
    }
  return w(this, oe, []), t;
}, Kt = function() {
  var a, f, u, d;
  w(this, Ct, !0);
  for (const h of o(this, Ge))
    o(this, Te).delete(h), I(h, L), this.schedule(h);
  for (const h of o(this, Te))
    I(h, ye), this.schedule(h);
  this.apply();
  for (var t = tr = [], r = [], n = Er = []; o(this, oe).length > 0; ) {
    zn++ > 1e3 && ($(this, N, Jt).call(this), Zi());
    for (const h of $(this, N, nn).call(this))
      try {
        $(this, N, sn).call(this, h, t, r);
      } catch (p) {
        throw Es(h), $(this, N, rn).call(this) || this.discard(), p;
      }
  }
  if (y = null, n.length > 0) {
    var s = jr.ensure();
    for (const h of n)
      s.schedule(h);
  }
  if (tr = null, Er = null, $(this, N, rn).call(this)) {
    $(this, N, Ot).call(this, r), $(this, N, Ot).call(this, t);
    for (const [h, p] of o(this, Le))
      xs(h, p);
    n.length > 0 && /** @type {unknown} */
    $(a = y, N, Kt).call(a);
    return;
  }
  const i = $(this, N, ys).call(this);
  if (i) {
    $(this, N, Ot).call(this, r), $(this, N, Ot).call(this, t), $(f = i, N, ws).call(f, this);
    return;
  }
  o(this, Ge).clear(), o(this, Te).clear();
  for (const h of o(this, Mt)) h(this);
  o(this, Mt).clear(), ft = this, qn(r), qn(t), ft = null, (u = o(this, It)) == null || u.resolve();
  var l = (
    /** @type {Batch | null} */
    /** @type {unknown} */
    y
  );
  if (o(this, jt) === 0 && (o(this, oe).length === 0 || l !== null) && $(this, N, Jt).call(this), o(this, oe).length > 0)
    if (l !== null) {
      for (const h of o(this, oe))
        o(l, oe).push(h);
      w(this, oe, []);
    } else
      l = this;
  l !== null && (qe.clear(), $(d = l, N, Kt).call(d));
}, /**
 * Traverse the effect tree, executing effects or stashing
 * them for later execution as appropriate
 * @param {Effect} root
 * @param {Effect[]} effects
 * @param {Effect[]} render_effects
 */
sn = function(t, r, n) {
  t.f ^= H;
  for (var s = t.first; s !== null; ) {
    var i = s.f, l = (i & (Me | tt)) !== 0, a = l && (i & H) !== 0, f = a || (i & ee) !== 0 || o(this, Le).has(s);
    if (!f && s.fn !== null) {
      l ? s.f ^= H : (i & Ft) !== 0 ? r.push(s) : _r(s) && ((i & Oe) !== 0 && o(this, Te).add(s), Ht(s));
      var u = s.first;
      if (u !== null) {
        s = u;
        continue;
      }
    }
    for (; s !== null; ) {
      var d = s.next;
      if (d !== null) {
        s = d;
        break;
      }
      s = s.parent;
    }
  }
}, ys = function() {
  for (var t = o(this, lt); t !== null; ) {
    if (!t.is_fork) {
      for (const [r, [, n]] of this.current)
        if (t.current.has(r) && !n)
          return t;
    }
    t = o(t, lt);
  }
  return null;
}, /**
 * @param {Batch} batch
 */
ws = function(t) {
  var n;
  for (const [s, i] of t.current)
    !this.previous.has(s) && t.previous.has(s) && this.previous.set(s, t.previous.get(s)), this.current.set(s, i);
  for (const [s, i] of t.async_deriveds) {
    const l = this.async_deriveds.get(s);
    l && i.promise.then(l.resolve).catch(l.reject);
  }
  t.async_deriveds.clear(), this.transfer_effects(o(t, Ge), o(t, Te));
  const r = (s) => {
    var i = s.reactions;
    if (i !== null && !((s.f & Z) !== 0 && (s.f & (L | ye)) === 0))
      for (const f of i) {
        var l = f.f;
        if ((l & Z) !== 0)
          r(
            /** @type {Derived} */
            f
          );
        else {
          var a = (
            /** @type {Effect} */
            f
          );
          l & (Rt | Oe) && !this.async_deriveds.has(a) && (o(this, Te).delete(a), I(a, L), this.schedule(a));
        }
      }
  };
  for (const s of this.current.keys())
    r(s);
  this.oncommit(() => t.discard()), $(n = t, N, Jt).call(n), y = this, $(this, N, Kt).call(this);
}, /**
 * @param {Effect[]} effects
 */
Ot = function(t) {
  for (var r = 0; r < t.length; r += 1)
    vs(t[r], o(this, Ge), o(this, Te));
}, Xi = function() {
  var h, p;
  for (let v = Yr; v !== null; v = o(v, gt)) {
    var t = v.id < this.id, r = [];
    for (const [c, [_, g]] of this.current) {
      if (v.current.has(c)) {
        var n = (
          /** @type {[any, boolean]} */
          v.current.get(c)[0]
        );
        if (t && _ !== n)
          v.current.set(c, [_, g]);
        else
          continue;
      }
      r.push(c);
    }
    if (t)
      for (const [c, _] of this.async_deriveds) {
        const g = v.async_deriveds.get(c);
        g && _.promise.then(g.resolve).catch(g.reject);
      }
    var s = [...v.current.keys()].filter(
      (c) => !/** @type {[any, boolean]} */
      v.current.get(c)[1]
    );
    if (!(!o(v, Ct) || s.length === 0)) {
      var i = s.filter((c) => !this.current.has(c));
      if (i.length === 0)
        t && v.discard();
      else if (r.length > 0) {
        if (t)
          for (const c of o(this, Lt))
            v.unskip_effect(c, (_) => {
              var g;
              (_.f & (Oe | Rt)) !== 0 ? v.schedule(_) : $(g = v, N, Ot).call(g, [_]);
            });
        v.activate();
        var l = /* @__PURE__ */ new Set(), a = /* @__PURE__ */ new Map();
        for (var f of r)
          $s(f, i, l, a);
        a = /* @__PURE__ */ new Map();
        var u = [...v.current].filter(([c, _]) => {
          const g = this.current.get(c);
          return g ? g[0] !== _[0] || g[1] !== _[1] : !0;
        }).map(([c]) => c);
        if (u.length > 0)
          for (const c of o(this, or))
            (c.f & (G | ee | Or)) === 0 && En(c, u, a) && ((c.f & (Rt | Oe)) !== 0 ? (I(c, L), v.schedule(c)) : o(v, Ge).add(c));
        if (o(v, oe).length > 0 && !o(v, mt)) {
          v.apply();
          for (var d of $(h = v, N, nn).call(h))
            $(p = v, N, sn).call(p, d, [], []);
        }
        v.deactivate();
      }
    }
  }
}, Jt = function() {
  if (this.linked) {
    var t = o(this, lt), r = o(this, gt);
    t === null ? Yr = r : w(t, gt, r), r === null ? At = t : w(r, lt, t), this.linked = !1;
  }
};
let ct = jr;
function P(e) {
  var t = er, r = ft;
  ft = null, er = !0;
  try {
    for (var n; ; ) {
      if (qi(), y === null)
        return (
          /** @type {T} */
          n
        );
      y.flush();
    }
  } finally {
    er = t, ft = r;
  }
}
function Zi() {
  try {
    Mi();
  } catch (e) {
    ze(e, tn);
  }
}
let ke = null;
function qn(e) {
  var t = e.length;
  if (t !== 0) {
    for (var r = 0; r < t; ) {
      var n = e[r++];
      if ((n.f & (G | ee)) === 0 && _r(n) && (ke = /* @__PURE__ */ new Set(), Ht(n), n.deps === null && n.first === null && n.nodes === null && n.teardown === null && n.ac === null && Is(n), (ke == null ? void 0 : ke.size) > 0)) {
        qe.clear();
        for (const s of ke) {
          if ((s.f & (G | ee)) !== 0) continue;
          const i = [s];
          let l = s.parent;
          for (; l !== null; )
            ke.has(l) && (ke.delete(l), i.push(l)), l = l.parent;
          for (let a = i.length - 1; a >= 0; a--) {
            const f = i[a];
            (f.f & (G | ee)) === 0 && Ht(f);
          }
        }
        ke.clear();
      }
    }
    ke = null;
  }
}
function $s(e, t, r, n) {
  if (!r.has(e) && (r.add(e), e.reactions !== null))
    for (const s of e.reactions) {
      const i = s.f;
      (i & Z) !== 0 ? $s(
        /** @type {Derived} */
        s,
        t,
        r,
        n
      ) : (i & (Rt | Oe)) !== 0 && (i & L) === 0 && En(s, t, n) && (I(s, L), Sn(
        /** @type {Effect} */
        s
      ));
    }
}
function En(e, t, r) {
  const n = r.get(e);
  if (n !== void 0) return n;
  if (e.deps !== null)
    for (const s of e.deps) {
      if (nr.call(t, s))
        return !0;
      if ((s.f & Z) !== 0 && En(
        /** @type {Derived} */
        s,
        t,
        r
      ))
        return r.set(
          /** @type {Derived} */
          s,
          !0
        ), !0;
    }
  return r.set(e, !1), !1;
}
function Sn(e) {
  y.schedule(e);
}
function xs(e, t) {
  if (!((e.f & Me) !== 0 && (e.f & H) !== 0)) {
    (e.f & L) !== 0 ? t.d.push(e) : (e.f & ye) !== 0 && t.m.push(e), I(e, H);
    for (var r = e.first; r !== null; )
      xs(r, t), r = r.next;
  }
}
function Es(e) {
  I(e, H);
  for (var t = e.first; t !== null; )
    Es(t), t = t.next;
}
let Mr = /* @__PURE__ */ new Set();
const qe = /* @__PURE__ */ new Map();
let Ss = !1;
function St(e, t) {
  var r = {
    f: 0,
    // TODO ideally we could skip this altogether, but it causes type errors
    v: e,
    reactions: null,
    equals: fs,
    rv: 0,
    wv: 0
  };
  return r;
}
// @__NO_SIDE_EFFECTS__
function J(e, t) {
  const r = St(e);
  return Fs(r), r;
}
// @__NO_SIDE_EFFECTS__
function ks(e, t = !1, r = !0) {
  const n = St(e);
  return t || (n.equals = us), n;
}
function D(e, t, r = !1) {
  S !== null && // since we are untracking the function inside `$inspect.with` we need to add this check
  // to ensure we error if state is set inside an inspect effect
  (!Re || (S.f & Or) !== 0) && hs() && (S.f & (Z | Oe | Rt | Or)) !== 0 && (Be === null || !Be.has(e)) && Di();
  let n = r ? vt(t) : t;
  return Bt(e, n, Er);
}
var ht = null, ln = 0;
function Bt(e, t, r = null) {
  if (!e.equals(t)) {
    nt ? qe.set(e, t) : qe.has(e) || qe.set(e, e.v);
    var n = ct.ensure();
    if (n.capture(e, t), (e.f & Z) !== 0) {
      const s = (
        /** @type {Derived} */
        e
      );
      (e.f & L) !== 0 && xn(s), U === null && $n(s);
    }
    e.wv = qs(), ht = null, ln = 0, Ts(e, L, r), ht = null, T !== null && (T.f & H) !== 0 && (T.f & (Me | tt)) === 0 && (_e === null ? ul([e]) : _e.push(e)), !n.is_fork && Mr.size > 0 && !Ss && Qi();
  }
  return t;
}
function Qi() {
  Ss = !1;
  for (const e of Mr) {
    (e.f & H) !== 0 && I(e, ye);
    let t;
    try {
      t = _r(e);
    } catch {
      t = !0;
    }
    t && Ht(e);
  }
  Mr.clear();
}
function el(e, t = 1) {
  var r = b(e), n = t === 1 ? r++ : r--;
  return D(e, r), n;
}
function rr(e) {
  D(e, e.v + 1);
}
function Ts(e, t, r) {
  var n = e.reactions;
  if (n !== null) {
    var s = n.length;
    if (ln += s, ln > 1e5 && ht === null && (ht = /* @__PURE__ */ new Set()), ht !== null) {
      if (ht.has(e)) return;
      ht.add(e);
    }
    for (var i = 0; i < s; i++) {
      var l = n[i], a = l.f, f = (a & L) === 0;
      if (f && I(l, t), (a & Or) !== 0)
        Mr.add(
          /** @type {Effect} */
          l
        );
      else if ((a & Z) !== 0) {
        var u = (
          /** @type {Derived} */
          l
        );
        U == null || U.delete(u), Ts(u, ye, r);
      } else if (f) {
        var d = (
          /** @type {Effect} */
          l
        );
        (a & Oe) !== 0 && ke !== null && ke.add(d), r !== null ? r.push(d) : Sn(d);
      }
    }
  }
}
function vt(e) {
  if (typeof e != "object" || e === null || $r in e || ls in e)
    return e;
  const t = ns(e);
  if (t !== gi && t !== mi)
    return e;
  var r = /* @__PURE__ */ new Map(), n = rs(e), s = /* @__PURE__ */ J(0), i = Et, l = (a) => {
    if (Et === i)
      return a();
    var f = S, u = Et;
    xe(null), Vn(i);
    var d = a();
    return xe(f), Vn(u), d;
  };
  return n && r.set("length", /* @__PURE__ */ J(
    /** @type {any[]} */
    e.length
  )), new Proxy(
    /** @type {any} */
    e,
    {
      defineProperty(a, f, u) {
        (!("value" in u) || u.configurable === !1 || u.enumerable === !1 || u.writable === !1) && Ii();
        var d = r.get(f);
        return d === void 0 ? l(() => {
          var h = /* @__PURE__ */ J(u.value);
          return r.set(f, h), h;
        }) : D(d, u.value, !0), !0;
      },
      deleteProperty(a, f) {
        var u = r.get(f);
        if (u === void 0) {
          if (f in a) {
            const d = l(() => /* @__PURE__ */ J(B));
            r.set(f, d), rr(s);
          }
        } else
          D(u, B), rr(s);
        return !0;
      },
      get(a, f, u) {
        var v;
        if (f === $r)
          return e;
        var d = r.get(f), h = f in a;
        if (d === void 0 && (!h || (v = $t(a, f)) != null && v.writable) && (d = l(() => {
          var c = vt(h ? a[f] : B), _ = /* @__PURE__ */ J(c);
          return _;
        }), r.set(f, d)), d !== void 0) {
          var p = b(d);
          return p === B ? void 0 : p;
        }
        return Reflect.get(a, f, u);
      },
      getOwnPropertyDescriptor(a, f) {
        var p;
        (p = this.has) == null || p.call(this, a, f);
        var u = Reflect.getOwnPropertyDescriptor(a, f), d = r.get(f);
        if (d !== void 0) {
          var h = b(d);
          if (h === B)
            return;
          if (u && "value" in u)
            u.value = h;
          else
            return {
              enumerable: !0,
              configurable: !0,
              value: h,
              writable: !0
            };
        }
        return u;
      },
      has(a, f) {
        var p;
        if (f === $r)
          return !0;
        var u = r.get(f), d = u !== void 0 && u.v !== B || Reflect.has(a, f);
        if (u !== void 0 || T !== null && (!d || (p = $t(a, f)) != null && p.writable)) {
          u === void 0 && (u = l(() => {
            var v = d ? vt(a[f]) : B, c = /* @__PURE__ */ J(v);
            return c;
          }), r.set(f, u));
          var h = b(u);
          if (h === B)
            return !1;
        }
        return d;
      },
      set(a, f, u, d) {
        var j;
        var h = r.get(f), p = f in a;
        if (n && f === "length")
          for (var v = u; v < /** @type {Source<number>} */
          h.v; v += 1) {
            var c = r.get(v + "");
            c !== void 0 ? D(c, B) : v in a && (c = l(() => /* @__PURE__ */ J(B)), r.set(v + "", c));
          }
        if (h === void 0)
          (!p || (j = $t(a, f)) != null && j.writable) && (h = l(() => /* @__PURE__ */ J(void 0)), D(h, vt(u)), r.set(f, h));
        else {
          p = h.v !== B;
          var _ = l(() => vt(u));
          D(h, _);
        }
        var g = Reflect.getOwnPropertyDescriptor(a, f);
        if (g != null && g.set && g.set.call(d, u), !p) {
          if (n && typeof f == "string") {
            var m = (
              /** @type {Source<number>} */
              r.get("length")
            ), x = Number(f);
            Number.isInteger(x) && x >= m.v && D(m, x + 1);
          }
          rr(s);
        }
        return !0;
      },
      ownKeys(a) {
        b(s);
        var f = Reflect.ownKeys(a).filter((h) => {
          var p = r.get(h);
          return p === void 0 || p.v !== B;
        });
        for (var [u, d] of r)
          d.v !== B && !(u in a) && f.push(u);
        return f;
      },
      setPrototypeOf() {
        Li();
      }
    }
  );
}
var Bn, As, Os, Rs;
function an() {
  if (Bn === void 0) {
    Bn = window, As = /Firefox/.test(navigator.userAgent);
    var e = Element.prototype, t = Node.prototype, r = Text.prototype;
    Os = $t(t, "firstChild").get, Rs = $t(t, "nextSibling").get, Dn(e) && (e[Ei] = void 0, e[xr] = null, e[en] = void 0, e.__e = void 0), Dn(r) && (r[Gt] = void 0);
  }
}
function $e(e = "") {
  return document.createTextNode(e);
}
// @__NO_SIDE_EFFECTS__
function rt(e) {
  return (
    /** @type {TemplateNode | null} */
    Os.call(e)
  );
}
// @__NO_SIDE_EFFECTS__
function Ve(e) {
  return (
    /** @type {TemplateNode | null} */
    Rs.call(e)
  );
}
function Ce(e, t) {
  if (!R)
    return /* @__PURE__ */ rt(e);
  var r = /* @__PURE__ */ rt(A);
  if (r === null)
    r = A.appendChild($e());
  else if (t && r.nodeType !== zr) {
    var n = $e();
    return r == null || r.before(n), ne(n), n;
  }
  return t && On(
    /** @type {Text} */
    r
  ), ne(r), r;
}
function kn(e, t = !1) {
  if (!R) {
    var r = /* @__PURE__ */ rt(e);
    return r instanceof Comment && r.data === "" ? /* @__PURE__ */ Ve(r) : r;
  }
  if (t) {
    if ((A == null ? void 0 : A.nodeType) !== zr) {
      var n = $e();
      return A == null || A.before(n), ne(n), n;
    }
    On(
      /** @type {Text} */
      A
    );
  }
  return A;
}
function ie(e, t = !1) {
  if (!R)
    return /* @__PURE__ */ rt(e);
  var r = Ce(e, t);
  return we(e), r;
}
function X(e, t = 1, r = !1) {
  let n = R ? A : e;
  for (var s; t--; )
    s = n, n = /** @type {TemplateNode} */
    /* @__PURE__ */ Ve(n);
  if (!R)
    return n;
  if (r) {
    if ((n == null ? void 0 : n.nodeType) !== zr) {
      var i = $e();
      return n === null ? s == null || s.after(i) : n.before(i), ne(i), i;
    }
    On(
      /** @type {Text} */
      n
    );
  }
  return ne(n), n;
}
function Tn(e) {
  e.textContent = "";
}
function Ns() {
  return !1;
}
function An(e, t, r) {
  return (
    /** @type {T extends keyof HTMLElementTagNameMap ? HTMLElementTagNameMap[T] : Element} */
    r ? document.createElement(e, { is: r }) : document.createElement(e)
  );
}
function On(e) {
  if (
    /** @type {string} */
    e.nodeValue.length < 65536
  )
    return;
  let t = e.nextSibling;
  for (; t !== null && t.nodeType === zr; )
    t.remove(), e.nodeValue += /** @type {string} */
    t.nodeValue, t = e.nextSibling;
}
function tl(e) {
  var t = T;
  if (t === null)
    return S.f |= ot, e;
  if ((t.f & kt) === 0 && (t.f & Ft) === 0)
    throw e;
  ze(e, t);
}
function ze(e, t) {
  if (e === ut)
    throw e;
  if (!(t !== null && (t.f & G) !== 0)) {
    for (; t !== null; ) {
      if ((t.f & Qr) !== 0 && (t.f & (G | Ar)) === 0) {
        if ((t.f & kt) === 0)
          throw e;
        try {
          t.b.error(e);
          return;
        } catch (r) {
          e = r;
        }
      }
      t = t.parent;
    }
    throw e;
  }
}
function rl(e) {
  T === null && (S === null && Ci(), Ni()), nt && Ri();
}
function nl(e, t) {
  var r = t.last;
  r === null ? t.last = t.first = e : (r.next = e, e.prev = r, t.last = e);
}
function Ye(e, t) {
  var r = T;
  r !== null && (r.f & ee) !== 0 && (e |= ee);
  var n = {
    ctx: se,
    deps: null,
    nodes: null,
    f: e | L | Ne,
    first: null,
    fn: t,
    last: null,
    next: null,
    parent: r,
    b: r && r.b,
    prev: null,
    teardown: null,
    wv: 0,
    ac: null
  };
  y == null || y.register_created_effect(n);
  var s = n;
  if ((e & Ft) !== 0)
    tr !== null ? tr.push(n) : ct.ensure().schedule(n);
  else if (t !== null) {
    try {
      Ht(n);
    } catch (l) {
      throw te(n), l;
    }
    s.deps === null && s.teardown === null && s.nodes === null && s.first === s.last && // either `null`, or a singular child
    (s.f & Tt) === 0 && (s = s.first, (e & Oe) !== 0 && (e & zt) !== 0 && s !== null && (s.f |= zt));
  }
  if (s !== null && (s.parent = r, r !== null && nl(s, r), S !== null && (S.f & Z) !== 0 && (e & tt) === 0)) {
    var i = (
      /** @type {Derived} */
      S
    );
    (i.effects ?? (i.effects = [])).push(s);
  }
  return n;
}
function Rn() {
  return S !== null && !Re;
}
function Cs(e) {
  const t = Ye(Fr, null);
  return I(t, H), t.teardown = e, t;
}
function Ms(e) {
  rl();
  var t = (
    /** @type {Effect} */
    T.f
  ), r = !S && (t & Me) !== 0 && se !== null && !se.i;
  if (r) {
    var n = (
      /** @type {ComponentContext} */
      se
    );
    (n.e ?? (n.e = [])).push(e);
  } else
    return Ps(e);
}
function Ps(e) {
  return Ye(Ft | $i, e);
}
function sl(e) {
  ct.ensure();
  const t = Ye(tt | Tt, e);
  return () => {
    te(t);
  };
}
function il(e) {
  ct.ensure();
  const t = Ye(tt | Tt, e);
  return (r = {}) => new Promise((n) => {
    r.outro ? xt(t, () => {
      te(t), n(void 0);
    }) : (te(t), n(void 0));
  });
}
function ll(e) {
  return Ye(Ft, e);
}
function al(e) {
  return Ye(Rt | Tt, e);
}
function Nn(e, t = 0) {
  return Ye(Fr | t, e);
}
function le(e, t = [], r = [], n = []) {
  Yi(n, t, r, (s) => {
    Ye(Fr, () => {
      e(...s.map(b));
    });
  });
}
function Cn(e, t = 0) {
  var r = Ye(Oe | t, e);
  return r;
}
function be(e) {
  return Ye(Me | Tt, e);
}
function js(e) {
  var t = e.teardown;
  if (t !== null) {
    const r = nt, n = S;
    Hn(!0), xe(null);
    try {
      t.call(null);
    } catch (s) {
      ze(s, e.parent);
    } finally {
      Hn(r), xe(n);
    }
  }
}
function Mn(e, t = !1) {
  var r = e.first;
  for (e.first = e.last = null; r !== null; ) {
    const s = r.ac;
    s !== null && Yt(() => {
      s.abort(vr);
    });
    var n = r.next;
    (r.f & tt) !== 0 ? r.parent = null : te(r, t), r = n;
  }
}
function ol(e) {
  for (var t = e.first; t !== null; ) {
    var r = t.next;
    (t.f & Me) === 0 && te(t), t = r;
  }
}
function te(e, t = !0) {
  var r = !1;
  (t || (e.f & wi) !== 0) && e.nodes !== null && e.nodes.end !== null && (fl(
    e.nodes.start,
    /** @type {TemplateNode} */
    e.nodes.end
  ), r = !0), e.f |= Ar, Mn(e, t && !r), lr(e, 0);
  var n = e.nodes && e.nodes.t;
  if (n !== null)
    for (const i of n)
      i.stop();
  js(e), e.f ^= Ar, e.f |= G;
  var s = e.parent;
  s !== null && s.first !== null && Is(e), e.next = e.prev = e.teardown = e.ctx = e.deps = e.fn = e.nodes = e.ac = e.b = null;
}
function fl(e, t) {
  for (; e !== null; ) {
    var r = e === t ? null : /* @__PURE__ */ Ve(e);
    e.remove(), e = r;
  }
}
function Is(e) {
  var t = e.parent, r = e.prev, n = e.next;
  r !== null && (r.next = n), n !== null && (n.prev = r), t !== null && (t.first === e && (t.first = n), t.last === e && (t.last = r));
}
function xt(e, t, r = !0) {
  var n = [];
  e.f |= bn, Ls(e, n, !0);
  var s = () => {
    r && te(e), t && t();
  }, i = n.length;
  if (i > 0) {
    var l = () => --i || s();
    for (var a of n)
      a.out(l);
  } else
    s();
}
function Ls(e, t, r) {
  if ((e.f & ee) === 0) {
    e.f ^= ee;
    var n = e.nodes && e.nodes.t;
    if (n !== null)
      for (const a of n)
        (a.is_global || r) && t.push(a);
    for (var s = e.first; s !== null; ) {
      var i = s.next;
      if ((s.f & tt) === 0) {
        var l = (s.f & zt) !== 0 || // If this is a branch effect without a block effect parent,
        // it means the parent block effect was pruned. In that case,
        // transparency information was transferred to the branch effect.
        (s.f & Me) !== 0 && (e.f & Oe) !== 0;
        Ls(s, t, l ? r : !1);
      }
      s = i;
    }
  }
}
function Pr(e) {
  e.f &= ~bn, Ds(e, !0);
}
function Ds(e, t) {
  if ((e.f & bn) === 0 && (e.f & ee) !== 0) {
    e.f ^= ee, (e.f & H) === 0 && (I(e, L), ct.ensure().schedule(e));
    for (var r = e.first; r !== null; ) {
      var n = r.next, s = (r.f & zt) !== 0 || (r.f & Me) !== 0;
      Ds(r, s ? t : !1), r = n;
    }
    var i = e.nodes && e.nodes.t;
    if (i !== null)
      for (const l of i)
        (l.is_global || t) && l.in();
  }
}
function Pn(e, t) {
  if (e.nodes)
    for (var r = e.nodes.start, n = e.nodes.end; r !== null; ) {
      var s = r === n ? null : /* @__PURE__ */ Ve(r);
      t.append(r), r = s;
    }
}
let nt = !1;
function Hn(e) {
  nt = e;
}
let S = null, Re = !1;
function xe(e) {
  S = e;
}
let T = null;
function He(e) {
  T = e;
}
let Be = null;
function Fs(e) {
  S !== null && ((S.f & Rr) !== 0 || (S.f & Z) !== 0) && (Be ?? (Be = /* @__PURE__ */ new Set())).add(e);
}
let re = null, de = 0, _e = null;
function ul(e) {
  _e = e;
}
let zs = 1, pt = 0, Et = pt;
function Vn(e) {
  Et = e;
}
function qs() {
  return ++zs;
}
function _r(e) {
  var t = e.f;
  if ((t & L) !== 0)
    return !0;
  if ((t & ye) !== 0) {
    for (var r = (
      /** @type {Value[]} */
      e.deps
    ), n = r.length, s = 0; s < n; s++) {
      var i = r[s];
      if (_r(
        /** @type {Derived} */
        i
      ) && ms(
        /** @type {Derived} */
        i
      ), i.wv > e.wv)
        return !0;
    }
    (t & Ne) !== 0 && // During time traveling we don't want to reset the status so that
    // traversal of the graph in the other batches still happens
    U === null && I(e, H);
  }
  return !1;
}
function Bs(e, t, r = !0) {
  var n = e.reactions;
  if (n !== null && !(Be !== null && Be.has(e)))
    for (var s = 0; s < n.length; s++) {
      var i = n[s];
      (i.f & Z) !== 0 ? Bs(
        /** @type {Derived} */
        i,
        t,
        !1
      ) : t === i && (r ? I(i, L) : (i.f & H) !== 0 && I(i, ye), Sn(
        /** @type {Effect} */
        i
      ));
    }
}
function Hs(e) {
  var t = re, r = de, n = _e, s = S, i = Be, l = se, a = Re, f = Et, u = e.f;
  re = /** @type {null | Value[]} */
  null, de = 0, _e = null, S = (u & (Me | tt)) === 0 ? e : null, Be = null, qt(e.ctx), Re = !1, Et = ++pt, e.ac !== null && (Yt(() => {
    e.ac.abort(vr);
  }), e.ac = null);
  try {
    e.f |= Rr;
    var d = (
      /** @type {Function} */
      e.fn
    ), h = d();
    e.f |= kt;
    var p = Yn(e);
    if (hs() && _e !== null && !Re && p !== null && (e.f & (Z | ye | L)) === 0)
      for (var v = 0; v < /** @type {Source[]} */
      _e.length; v++)
        Bs(
          _e[v],
          /** @type {Effect} */
          e
        );
    if (s !== null && s !== e) {
      if (pt++, s.deps !== null)
        for (let c = 0; c < r; c += 1)
          s.deps[c].rv = pt;
      if (t !== null)
        for (const c of t)
          c.rv = pt;
      _e !== null && (n === null ? n = _e : n.push(.../** @type {Source[]} */
      _e));
    }
    return (e.f & ot) !== 0 && (e.f ^= ot), h;
  } catch (c) {
    return Yn(e), tl(c);
  } finally {
    e.f ^= Rr, re = t, de = r, _e = n, S = s, Be = i, qt(l), Re = a, Et = f;
  }
}
function Yn(e) {
  var s;
  var t = e.deps, r = y == null ? void 0 : y.is_fork;
  if (re !== null) {
    var n;
    if (r || lr(e, de), t !== null && de > 0)
      for (t.length = de + re.length, n = 0; n < re.length; n++)
        t[de + n] = re[n];
    else
      e.deps = t = re;
    if (Rn() && (e.f & Ne) !== 0)
      for (n = de; n < t.length; n++)
        ((s = t[n]).reactions ?? (s.reactions = [])).push(e);
  } else !r && t !== null && de < t.length && (lr(e, de), t.length = de);
  return t;
}
function cl(e, t) {
  let r = t.reactions;
  if (r !== null) {
    var n = pi.call(r, e);
    if (n !== -1) {
      var s = r.length - 1;
      s === 0 ? r = t.reactions = null : (r[n] = r[s], r.pop());
    }
  }
  if (r === null && (t.f & Z) !== 0 && // Destroying a child effect while updating a parent effect can cause a dependency to appear
  // to be unused, when in fact it is used by the currently-updating parent. Checking `new_deps`
  // allows us to skip the expensive work of disconnecting and immediately reconnecting it
  (re === null || !nr.call(re, t))) {
    var i = (
      /** @type {Derived} */
      t
    );
    (i.f & Ne) !== 0 && (i.f ^= Ne), i.v !== B && $n(i), i.ac !== null && Yt(() => {
      i.ac.abort(vr), i.ac = null, I(i, L);
    }), Ki(i), lr(i, 0);
  }
}
function lr(e, t) {
  var r = e.deps;
  if (r !== null)
    for (var n = t; n < r.length; n++)
      cl(e, r[n]);
}
function Ht(e) {
  var t = e.f;
  if ((t & G) === 0) {
    I(e, H);
    var r = T;
    T = e;
    try {
      (t & (Oe | is)) !== 0 ? ol(e) : Mn(e), js(e);
      var n = Hs(e);
      e.teardown = typeof n == "function" ? n : null, e.wv = zs;
      var s;
      ts && zi && (e.f & L) !== 0 && e.deps;
    } finally {
      T = r;
    }
  }
}
async function hl() {
  await Promise.resolve(), P();
}
function b(e) {
  var t = e.f, r = (t & Z) !== 0;
  if (S !== null && !Re) {
    var n = T !== null && (T.f & G) !== 0;
    if (!n && (Be === null || !Be.has(e))) {
      var s = S.deps;
      if ((S.f & Rr) !== 0)
        e.rv < pt && (e.rv = pt, re === null && s !== null && s[de] === e ? de++ : re === null ? re = [e] : re.push(e));
      else {
        S.deps ?? (S.deps = []), nr.call(S.deps, e) || S.deps.push(e);
        var i = e.reactions;
        i === null ? e.reactions = [S] : nr.call(i, S) || i.push(S);
      }
    }
  }
  if (nt && qe.has(e))
    return qe.get(e);
  if (r) {
    var l = (
      /** @type {Derived} */
      e
    );
    if (nt) {
      var a = l.v;
      return ((l.f & H) === 0 && l.reactions !== null || Ys(l)) && (a = xn(l)), qe.set(l, a), a;
    }
    var f = (l.f & Ne) === 0 && !Re && S !== null && (S.f & Ne) !== 0, u = (l.f & kt) === 0;
    _r(l) && (f && (l.f |= Ne), ms(l)), f && !u && (bs(l), Vs(l));
  }
  if (U != null && U.has(e))
    return U.get(e);
  if ((e.f & ot) !== 0)
    throw e.v;
  return e.v;
}
function Vs(e) {
  if (e.f |= Ne, e.deps !== null)
    for (const r of e.deps) {
      var t = r.reactions;
      t === null ? r.reactions = [e] : nr.call(t, e) || t.push(e), (r.f & Z) !== 0 && (r.f & Ne) === 0 && (bs(
        /** @type {Derived} */
        r
      ), Vs(
        /** @type {Derived} */
        r
      ));
    }
}
function Ys(e) {
  if (e.v === B) return !0;
  if (e.deps === null) return !1;
  for (const t of e.deps)
    if (qe.has(t) || (t.f & Z) !== 0 && Ys(
      /** @type {Derived} */
      t
    ))
      return !0;
  return !1;
}
function jn(e) {
  var t = Re;
  try {
    return Re = !0, e();
  } finally {
    Re = t;
  }
}
const _t = Symbol("events"), Us = /* @__PURE__ */ new Set(), on = /* @__PURE__ */ new Set();
function dl(e, t, r, n = {}) {
  function s(i) {
    if (n.capture || fn.call(t, i), !i.cancelBubble)
      return Yt(() => r == null ? void 0 : r.call(this, i));
  }
  return e.startsWith("pointer") || e.startsWith("touch") || e === "wheel" ? (s.__removed = !1, Ze(() => {
    s.__removed || t.addEventListener(e, s, n);
  })) : t.addEventListener(e, s, n), s;
}
function vl(e, t, r, n, s) {
  var i = { capture: n, passive: s }, l = dl(e, t, r, i);
  (t === document.body || // @ts-ignore
  t === window || // @ts-ignore
  t === document || // Firefox has quirky behavior, it can happen that we still get "canplay" events when the element is already removed
  t instanceof HTMLMediaElement) && Cs(() => {
    l.__removed = !0, t.removeEventListener(e, l, i);
  });
}
function Vt(e, t, r) {
  (t[_t] ?? (t[_t] = {}))[e] = r;
}
function gr(e) {
  for (var t = 0; t < e.length; t++)
    Us.add(e[t]);
  for (var r of on)
    r(e);
}
let Gr = null, Wr = !1;
function fn(e) {
  var _, g;
  var t = this, r = (
    /** @type {Node} */
    t.ownerDocument
  ), n = e.type, s = ((_ = e.composedPath) == null ? void 0 : _.call(e)) || [], i = (
    /** @type {null | Element} */
    s[0] || e.target
  );
  Gr = e, Wr || (Wr = !0, setTimeout(() => {
    Wr = !1, Gr = null;
  }));
  var l = 0, a = Gr === e && e[_t];
  if (a) {
    var f = s.indexOf(a);
    if (f !== -1 && (t === document || t === /** @type {any} */
    window)) {
      e[_t] = t;
      return;
    }
    var u = s.indexOf(t);
    if (u === -1)
      return;
    f <= u && (l = f);
  }
  if (i = /** @type {Element} */
  s[l] || e.target, i !== t) {
    sr(e, "currentTarget", {
      configurable: !0,
      get() {
        return i || r;
      }
    });
    var d = S, h = T;
    xe(null), He(null);
    try {
      for (var p, v = []; i !== null && i !== t; ) {
        try {
          var c = (g = i[_t]) == null ? void 0 : g[n];
          c != null && (!/** @type {any} */
          i.disabled || // DOM could've been updated already by the time this is reached, so we check this as well
          // -> the target could not have been disabled because it emits the event in the first place
          e.target === i) && c.call(i, e);
        } catch (m) {
          p ? v.push(m) : p = m;
        }
        if (e.cancelBubble) break;
        l++, i = l < s.length ? (
          /** @type {Element} */
          s[l]
        ) : null;
      }
      if (p) {
        for (let m of v)
          queueMicrotask(() => {
            throw m;
          });
        throw p;
      }
    } finally {
      e[_t] = t, delete e.currentTarget, xe(d), He(h);
    }
  }
}
var Qn;
const Kr = (
  // We gotta write it like this because after downleveling the pure comment may end up in the wrong location
  ((Qn = globalThis == null ? void 0 : globalThis.window) == null ? void 0 : Qn.trustedTypes) && /* @__PURE__ */ globalThis.window.trustedTypes.createPolicy("svelte-trusted-html", {
    /** @param {string} html */
    createHTML: (e) => e
  })
);
function pl(e) {
  return (
    /** @type {string} */
    (Kr == null ? void 0 : Kr.createHTML(e)) ?? e
  );
}
function _l(e) {
  var t = An("template");
  return t.innerHTML = pl(e.replaceAll("<!>", "<!---->")), t.content;
}
function Nt(e, t) {
  var r = (
    /** @type {Effect} */
    T
  );
  r.nodes === null && (r.nodes = { start: e, end: t, a: null, t: null });
}
// @__NO_SIDE_EFFECTS__
function W(e, t) {
  var r = (t & hi) !== 0, n = (t & di) !== 0, s, i = !e.startsWith("<!>");
  return () => {
    if (R)
      return Nt(A, null), A;
    s === void 0 && (s = _l(i ? e : "<!>" + e), r || (s = /** @type {TemplateNode} */
    /* @__PURE__ */ rt(s)));
    var l = (
      /** @type {TemplateNode} */
      n || As ? document.importNode(s, !0) : s.cloneNode(!0)
    );
    if (r) {
      var a = (
        /** @type {TemplateNode} */
        /* @__PURE__ */ rt(l)
      ), f = (
        /** @type {TemplateNode} */
        l.lastChild
      );
      Nt(a, f);
    } else
      Nt(l, l);
    return l;
  };
}
function Gs() {
  if (R)
    return Nt(A, null), A;
  var e = document.createDocumentFragment(), t = document.createComment(""), r = $e();
  return e.append(t, r), Nt(t, r), e;
}
function z(e, t) {
  if (R) {
    var r = (
      /** @type {Effect & { nodes: EffectNodes }} */
      T
    );
    ((r.f & kt) === 0 || r.nodes.end === null) && (r.nodes.end = A), Br();
    return;
  }
  e !== null && e.before(
    /** @type {Node} */
    t
  );
}
const gl = ["touchstart", "touchmove"];
function ml(e) {
  return gl.includes(e);
}
function bl(e) {
  let t = 0, r = St(0), n;
  return () => {
    Rn() && (b(r), Nn(() => (t === 0 && (n = jn(() => e(() => rr(r)))), t += 1, () => {
      Ze(() => {
        t -= 1, t === 0 && (n == null || n(), n = void 0, rr(r));
      });
    })));
  };
}
var yl = zt | Tt;
function wl(e, t, r, n) {
  new $l(e, t, r, n);
}
var fe, fr, ge, bt, K, ue, Q, ce, De, yt, at, Dt, ur, cr, We, Ir, k, Ws, Ks, un, it, Js, cn, Xt, Sr, hn, dn;
class $l {
  /**
   * @param {TemplateNode} node
   * @param {BoundaryProps} props
   * @param {((anchor: Node) => void)} children
   * @param {((error: unknown) => unknown) | undefined} [transform_error]
   */
  constructor(t, r, n, s) {
    E(this, k);
    /** @type {Boundary | null} */
    M(this, "parent");
    M(this, "is_pending", !1);
    /**
     * API-level transformError transform function. Transforms errors before they reach the `failed` snippet.
     * Inherited from parent boundary, or defaults to identity.
     * @type {(error: unknown) => unknown}
     */
    M(this, "transform_error");
    /** @type {TemplateNode} */
    E(this, fe);
    /** @type {TemplateNode | null} */
    E(this, fr, R ? A : null);
    /** @type {BoundaryProps} */
    E(this, ge);
    /** @type {((anchor: Node) => void)} */
    E(this, bt);
    /** @type {Effect} */
    E(this, K);
    /** @type {Effect | null} */
    E(this, ue, null);
    /** @type {Effect | null} */
    E(this, Q, null);
    /** @type {Effect | null} */
    E(this, ce, null);
    /** @type {DocumentFragment | null} */
    E(this, De, null);
    E(this, yt, 0);
    E(this, at, 0);
    E(this, Dt, !1);
    /** @type {Set<Effect>} */
    E(this, ur, /* @__PURE__ */ new Set());
    /** @type {Set<Effect>} */
    E(this, cr, /* @__PURE__ */ new Set());
    /**
     * A source containing the number of pending async deriveds/expressions.
     * Only created if `$effect.pending()` is used inside the boundary,
     * otherwise updating the source results in needless `Batch.ensure()`
     * calls followed by no-op flushes
     * @type {Source<number> | null}
     */
    E(this, We, null);
    E(this, Ir, bl(() => (w(this, We, St(o(this, yt))), () => {
      w(this, We, null);
    })));
    var i;
    w(this, fe, t), w(this, ge, r), w(this, bt, (l) => {
      var a = (
        /** @type {Effect} */
        T
      );
      a.b = this, a.f |= Qr, n(l);
    }), this.parent = /** @type {Effect} */
    T.b, this.transform_error = s ?? ((i = this.parent) == null ? void 0 : i.transform_error) ?? ((l) => l), w(this, K, Cn(() => {
      if (R) {
        const l = wn(
          /** @type {TemplateNode} */
          o(this, fr)
        );
        Br();
        const a = l === gn;
        if (l.startsWith(Ln)) {
          const u = JSON.parse(l.slice(Ln.length));
          $(this, k, Ks).call(this, u);
        } else a ? $(this, k, Js).call(this) : $(this, k, Ws).call(this);
      } else
        $(this, k, cn).call(this);
    }, yl)), R && w(this, fe, A);
  }
  /**
   * Defer an effect inside a pending boundary until the boundary resolves
   * @param {Effect} effect
   */
  defer_effect(t) {
    vs(t, o(this, ur), o(this, cr));
  }
  /**
   * Returns `false` if the effect exists inside a boundary whose pending snippet is shown
   * @returns {boolean}
   */
  is_rendered() {
    return !this.is_pending && (!this.parent || this.parent.is_rendered());
  }
  has_pending_snippet() {
    return !!o(this, ge).pending;
  }
  /**
   * Update the source that powers `$effect.pending()` inside this boundary,
   * and controls when the current `pending` snippet (if any) is removed.
   * Do not call from inside the class
   * @param {1 | -1} d
   * @param {Batch} batch
   */
  update_pending_count(t, r) {
    $(this, k, hn).call(this, t, r), w(this, yt, o(this, yt) + t), !(!o(this, We) || o(this, Dt)) && (w(this, Dt, !0), Ze(() => {
      w(this, Dt, !1), o(this, We) && Bt(o(this, We), o(this, yt));
    }));
  }
  get_effect_pending() {
    return o(this, Ir).call(this), b(
      /** @type {Source<number>} */
      o(this, We)
    );
  }
  /** @param {unknown} error */
  error(t) {
    if (t === ut || !o(this, ge).onerror && !o(this, ge).failed)
      throw t;
    y != null && y.is_fork ? (o(this, ue) && y.skip_effect(o(this, ue)), o(this, Q) && y.skip_effect(o(this, Q)), o(this, ce) && y.skip_effect(o(this, ce)), y.oncommit(() => {
      $(this, k, it).call(this) || $(this, k, dn).call(this, t);
    })) : $(this, k, dn).call(this, t);
  }
}
fe = new WeakMap(), fr = new WeakMap(), ge = new WeakMap(), bt = new WeakMap(), K = new WeakMap(), ue = new WeakMap(), Q = new WeakMap(), ce = new WeakMap(), De = new WeakMap(), yt = new WeakMap(), at = new WeakMap(), Dt = new WeakMap(), ur = new WeakMap(), cr = new WeakMap(), We = new WeakMap(), Ir = new WeakMap(), k = new WeakSet(), Ws = function() {
  try {
    w(this, ue, be(() => o(this, bt).call(this, o(this, fe))));
  } catch (t) {
    this.error(t);
  }
}, /**
 * @param {unknown} error The deserialized error from the server's hydration comment
 */
Ks = function(t) {
  const r = o(this, ge).failed, { reset: n, invoke_onerror: s } = $(this, k, un).call(this, t);
  Ze(s), r && w(this, ce, be(() => {
    r(
      o(this, fe),
      () => t,
      () => n
    );
  }));
}, /**
 * Creates the `reset` function for a failed boundary, along with a function
 * that invokes `onerror` with it (if provided)
 * @param {unknown} error
 * @returns {{ reset: () => void, invoke_onerror: () => void }}
 */
un = function(t) {
  var r = !1, n = !1;
  const s = () => {
    if (!$(this, k, it).call(this)) {
      if (r) {
        ki();
        return;
      }
      r = !0, n && Fi(), o(this, ce) !== null && xt(o(this, ce), () => {
        w(this, ce, null);
      }), $(this, k, Sr).call(this, () => {
        $(this, k, cn).call(this);
      });
    }
  };
  return { reset: s, invoke_onerror: () => {
    var l, a;
    if (!$(this, k, it).call(this))
      try {
        n = !0, (a = (l = o(this, ge)).onerror) == null || a.call(l, t, s), n = !1;
      } catch (f) {
        ze(f, o(this, K) && o(this, K).parent);
      }
  } };
}, it = function() {
  return (o(this, K).f & (G | Ar)) !== 0;
}, Js = function() {
  const t = o(this, ge).pending;
  t && (this.is_pending = !0, w(this, Q, be(() => t(o(this, fe)))), Ze(() => {
    if (!$(this, k, it).call(this)) {
      var r = w(this, De, document.createDocumentFragment()), n = $e(), s = !1;
      if (r.append(n), w(this, ue, $(this, k, Sr).call(this, () => {
        try {
          return be(() => o(this, bt).call(this, n));
        } catch (i) {
          try {
            this.error(i), s = !0;
          } catch (l) {
            ze(l, o(this, K).parent);
          }
          return null;
        }
      })), o(this, ue) === null) {
        w(this, De, null), s && $(this, k, Xt).call(
          this,
          /** @type {Batch} */
          y
        );
        return;
      }
      o(this, at) === 0 && (o(this, fe).before(r), w(this, De, null), xt(
        /** @type {Effect} */
        o(this, Q),
        () => {
          w(this, Q, null);
        }
      ), $(this, k, Xt).call(
        this,
        /** @type {Batch} */
        y
      ));
    }
  }));
}, cn = function() {
  try {
    if (this.is_pending = this.has_pending_snippet(), w(this, at, 0), w(this, yt, 0), w(this, ue, be(() => {
      o(this, bt).call(this, o(this, fe));
    })), o(this, at) > 0) {
      var t = w(this, De, document.createDocumentFragment());
      Pn(o(this, ue), t);
      const r = (
        /** @type {(anchor: Node) => void} */
        o(this, ge).pending
      );
      w(this, Q, be(() => r(o(this, fe))));
    } else
      $(this, k, Xt).call(
        this,
        /** @type {Batch} */
        y
      );
  } catch (r) {
    this.error(r);
  }
}, /**
 * @param {Batch} batch
 */
Xt = function(t) {
  this.is_pending = !1, t.transfer_effects(o(this, ur), o(this, cr));
}, /**
 * @template T
 * @param {() => T} fn
 */
Sr = function(t) {
  var r = T, n = S, s = se;
  He(o(this, K)), xe(o(this, K)), qt(o(this, K).ctx);
  try {
    return ct.ensure(), t();
  } finally {
    He(r), xe(n), qt(s);
  }
}, /**
 * Updates the pending count associated with the currently visible pending snippet,
 * if any, such that we can replace the snippet with content once work is done
 * @param {1 | -1} d
 * @param {Batch} batch
 */
hn = function(t, r) {
  var n;
  if (!this.has_pending_snippet()) {
    this.parent && $(n = this.parent, k, hn).call(n, t, r);
    return;
  }
  w(this, at, o(this, at) + t), o(this, at) === 0 && ($(this, k, Xt).call(this, r), o(this, Q) && xt(o(this, Q), () => {
    w(this, Q, null);
  }), o(this, De) && (o(this, fe).before(o(this, De)), w(this, De, null)));
}, /**
 * @param {unknown} error
 */
dn = function(t) {
  o(this, ue) && (te(o(this, ue)), w(this, ue, null)), o(this, Q) && (te(o(this, Q)), w(this, Q, null)), o(this, ce) && (te(o(this, ce)), w(this, ce, null)), R && (ne(
    /** @type {TemplateNode} */
    o(this, fr)
  ), yn(), ne(Nr()));
  let r = o(this, ge).failed;
  const n = (s) => {
    if ($(this, k, it).call(this)) return;
    const { reset: i, invoke_onerror: l } = $(this, k, un).call(this, s);
    l(), r && !$(this, k, it).call(this) && w(this, ce, $(this, k, Sr).call(this, () => {
      try {
        return be(() => {
          var a = (
            /** @type {Effect} */
            T
          );
          a.b = this, a.f |= Qr, r(
            o(this, fe),
            () => s,
            () => i
          );
        });
      } catch (a) {
        return ze(
          a,
          /** @type {Effect} */
          o(this, K).parent
        ), null;
      }
    }));
  };
  Ze(() => {
    if (!$(this, k, it).call(this)) {
      var s;
      try {
        s = this.transform_error(t);
      } catch (i) {
        ze(i, o(this, K) && o(this, K).parent);
        return;
      }
      s !== null && typeof s == "object" && typeof /** @type {any} */
      s.then == "function" ? s.then(
        n,
        /** @param {unknown} e */
        (i) => ze(i, o(this, K) && o(this, K).parent)
      ) : n(s);
    }
  });
};
function ae(e, t) {
  var r = t == null ? "" : typeof t == "object" ? `${t}` : t;
  r !== /** @type {any} */
  (e[Gt] ?? (e[Gt] = e.nodeValue)) && (e[Gt] = r, e.nodeValue = `${r}`);
}
function Xs(e, t) {
  return Zs(e, t);
}
function xl(e, t) {
  an(), t.intro = t.intro ?? !1;
  const r = t.target, n = R, s = A;
  try {
    for (var i = /* @__PURE__ */ rt(r); i && (i.nodeType !== pr || /** @type {Comment} */
    i.data !== es); )
      i = /* @__PURE__ */ Ve(i);
    if (!i)
      throw ut;
    Xe(!0), ne(
      /** @type {Comment} */
      i
    );
    const l = Zs(e, { ...t, anchor: i });
    return Xe(!1), /**  @type {Exports} */
    l;
  } catch (l) {
    if (l instanceof Error && l.message.split(`
`).some((a) => a.startsWith("https://svelte.dev/e/")))
      throw l;
    return l !== ut && console.warn("Failed to hydrate: ", l), t.recover === !1 && Pi(), an(), Tn(r), Xe(!1), Xs(e, t);
  } finally {
    Xe(n), ne(s);
  }
}
const yr = /* @__PURE__ */ new Map();
function Zs(e, { target: t, anchor: r, props: n = {}, events: s, context: i, intro: l = !0, transformError: a }) {
  an();
  var f = void 0, u = il(() => {
    var d = r ?? t.appendChild($e());
    wl(
      /** @type {TemplateNode} */
      d,
      {
        pending: () => {
        }
      },
      (v) => {
        Ee({});
        var c = (
          /** @type {ComponentContext} */
          se
        );
        if (i && (c.c = i), s && (n.$$events = s), R && Nt(
          /** @type {TemplateNode} */
          v,
          null
        ), f = e(v, n) || cs(), R && (T.nodes.end = A, A === null || A.nodeType !== pr || /** @type {Comment} */
        A.data !== mn))
          throw qr(), ut;
        Se();
      },
      a
    );
    var h = /* @__PURE__ */ new Set(), p = (v) => {
      for (var c = 0; c < v.length; c++) {
        var _ = v[c];
        if (!h.has(_)) {
          h.add(_);
          var g = ml(_);
          for (const j of [t, document]) {
            var m = yr.get(j);
            m === void 0 && (m = /* @__PURE__ */ new Map(), yr.set(j, m));
            var x = m.get(_);
            x === void 0 ? (j.addEventListener(_, fn, { passive: g }), m.set(_, 1)) : m.set(_, x + 1);
          }
        }
      }
    };
    return p(Dr(Us)), on.add(p), () => {
      var g;
      for (var v of h)
        for (const m of [t, document]) {
          var c = (
            /** @type {Map<string, number>} */
            yr.get(m)
          ), _ = (
            /** @type {number} */
            c.get(v)
          );
          --_ == 0 ? (m.removeEventListener(v, fn), c.delete(v), c.size === 0 && yr.delete(m)) : c.set(v, _);
        }
      on.delete(p), d !== r && ((g = d.parentNode) == null || g.removeChild(d));
    };
  });
  return vn.set(f, u), f;
}
let vn = /* @__PURE__ */ new WeakMap();
function El(e, t) {
  const r = vn.get(e);
  return r ? (vn.delete(e), r(t)) : Promise.resolve();
}
var Ae, Fe, he, wt, hr, dr, Lr;
class Sl {
  /**
   * @param {TemplateNode} anchor
   * @param {boolean} transition
   */
  constructor(t, r = !0) {
    /** @type {TemplateNode} */
    M(this, "anchor");
    /** @type {Map<Batch, Key>} */
    E(this, Ae, /* @__PURE__ */ new Map());
    /**
     * Map of keys to effects that are currently rendered in the DOM.
     * These effects are visible and actively part of the document tree.
     * Example:
     * ```
     * {#if condition}
     * 	foo
     * {:else}
     * 	bar
     * {/if}
     * ```
     * Can result in the entries `true->Effect` and `false->Effect`
     * @type {Map<Key, Effect>}
     */
    E(this, Fe, /* @__PURE__ */ new Map());
    /**
     * Similar to #onscreen with respect to the keys, but contains branches that are not yet
     * in the DOM, because their insertion is deferred.
     * @type {Map<Key, Branch>}
     */
    E(this, he, /* @__PURE__ */ new Map());
    /**
     * Keys of effects that are currently outroing
     * @type {Set<Key>}
     */
    E(this, wt, /* @__PURE__ */ new Set());
    /**
     * Whether to pause (i.e. outro) on change, or destroy immediately.
     * This is necessary for `<svelte:element>`
     */
    E(this, hr, !0);
    /**
     * @param {Batch} batch
     */
    E(this, dr, (t) => {
      if (o(this, Ae).has(t)) {
        var r = (
          /** @type {Key} */
          o(this, Ae).get(t)
        ), n = o(this, Fe).get(r);
        if (n)
          Pr(n), o(this, wt).delete(r);
        else {
          var s = o(this, he).get(r);
          s && (Pr(s.effect), o(this, Fe).set(r, s.effect), o(this, he).delete(r), s.fragment.lastChild.remove(), this.anchor.before(s.fragment), n = s.effect);
        }
        for (const [i, l] of o(this, Ae)) {
          if (o(this, Ae).delete(i), i === t)
            break;
          const a = o(this, he).get(l);
          a && (te(a.effect), o(this, he).delete(l));
        }
        for (const [i, l] of o(this, Fe)) {
          if (i === r || o(this, wt).has(i)) continue;
          const a = () => {
            if (Array.from(o(this, Ae).values()).includes(i)) {
              var u = document.createDocumentFragment();
              Pn(l, u), u.append($e()), o(this, he).set(i, { effect: l, fragment: u });
            } else
              te(l);
            o(this, wt).delete(i), o(this, Fe).delete(i);
          };
          o(this, hr) || !n ? (o(this, wt).add(i), xt(l, a, !1)) : a();
        }
      }
    });
    /**
     * @param {Batch} batch
     */
    E(this, Lr, (t) => {
      o(this, Ae).delete(t);
      const r = Array.from(o(this, Ae).values());
      for (const [n, s] of o(this, he))
        r.includes(n) || (te(s.effect), o(this, he).delete(n));
    });
    this.anchor = t, w(this, hr, r);
  }
  /**
   *
   * @param {any} key
   * @param {null | ((target: TemplateNode) => void)} fn
   */
  ensure(t, r) {
    var n = (
      /** @type {Batch} */
      y
    ), s = Ns();
    if (r && !o(this, Fe).has(t) && !o(this, he).has(t))
      if (s) {
        var i = document.createDocumentFragment(), l = $e();
        i.append(l), o(this, he).set(t, {
          effect: be(() => r(l)),
          fragment: i
        });
      } else
        o(this, Fe).set(
          t,
          be(() => r(this.anchor))
        );
    if (o(this, Ae).set(n, t), s) {
      for (const [a, f] of o(this, Fe))
        a === t ? n.unskip_effect(f) : n.skip_effect(f);
      for (const [a, f] of o(this, he))
        a === t ? n.unskip_effect(f.effect) : n.skip_effect(f.effect);
      n.oncommit(o(this, dr)), n.ondiscard(o(this, Lr));
    } else
      R && (this.anchor = A), o(this, dr).call(this, n);
  }
}
Ae = new WeakMap(), Fe = new WeakMap(), he = new WeakMap(), wt = new WeakMap(), hr = new WeakMap(), dr = new WeakMap(), Lr = new WeakMap();
function ar(e, t, r = !1) {
  var n;
  R && (n = A, Br());
  var s = new Sl(e), i = r ? zt : 0;
  function l(a, f) {
    if (R) {
      var u = wn(
        /** @type {TemplateNode} */
        n
      );
      if (a !== parseInt(u.substring(1))) {
        var d = Nr();
        ne(d), s.anchor = d, Xe(!1), s.ensure(a, f), Xe(!0);
        return;
      }
    }
    s.ensure(a, f);
  }
  Cn(() => {
    var a = !1;
    t((f, u = 0) => {
      a = !0, l(u, f);
    }), a || l(-1, null);
  }, i);
}
function kl(e, t, r) {
  for (var n = [], s = t.length, i, l = t.length, a = 0; a < s; a++) {
    let h = t[a];
    xt(
      h,
      () => {
        if (i) {
          if (i.pending.delete(h), i.done.add(h), i.pending.size === 0) {
            var p = (
              /** @type {Set<EachOutroGroup>} */
              e.outrogroups
            );
            pn(e, Dr(i.done)), p.delete(i), p.size === 0 && (e.outrogroups = null);
          }
        } else
          l -= 1;
      },
      !1
    );
  }
  if (l === 0) {
    var f = n.length === 0 && r !== null && e.pending.size === 0;
    if (f) {
      var u = (
        /** @type {Element} */
        r
      ), d = (
        /** @type {Element} */
        u.parentNode
      );
      Tn(d), d.append(u), e.items.clear();
    }
    pn(e, t, !f);
  } else
    i = {
      pending: new Set(t),
      done: /* @__PURE__ */ new Set()
    }, (e.outrogroups ?? (e.outrogroups = /* @__PURE__ */ new Set())).add(i);
}
function pn(e, t, r = !0) {
  var n;
  if (e.pending.size > 0) {
    n = /* @__PURE__ */ new Set();
    for (const l of e.pending.values())
      for (const a of l)
        n.add(
          /** @type {EachItem} */
          e.items.get(a).e
        );
  }
  for (var s = 0; s < t.length; s++) {
    var i = t[s];
    if (n != null && n.has(i)) {
      i.f |= Je;
      const l = document.createDocumentFragment();
      Pn(i, l);
    } else
      te(t[s], r);
  }
}
var Un;
function Tl(e, t, r, n, s, i = null) {
  var l = e, a = /* @__PURE__ */ new Map();
  {
    var f = (
      /** @type {Element} */
      e
    );
    l = R ? ne(/* @__PURE__ */ rt(f)) : f.appendChild($e());
  }
  R && Br();
  var u = null, d = /* @__PURE__ */ gs(() => {
    var m = r();
    return (
      /** @type {V[]} */
      rs(m) ? m : m == null ? [] : Dr(m)
    );
  }), h = /* @__PURE__ */ new Map(), p = !0;
  function v(m) {
    if ((g.effect.f & G) === 0) {
      g.pending.delete(m);
      var x = b(d);
      g.fallback = u, Al(g, x, l, t, n), u !== null && (x.length === 0 ? (u.f & Je) === 0 ? Pr(u) : (u.f ^= Je, Zt(u, null, l)) : xt(u, () => {
        u = null;
      }));
    }
  }
  function c(m) {
    g.pending.delete(m);
  }
  var _ = Cn(() => {
    var m = (
      /** @type {V[]} */
      b(d)
    ), x = m.length;
    let j = !1;
    if (R) {
      var ve = wn(l) === gn;
      ve !== (x === 0) && (l = Nr(), ne(l), Xe(!1), j = !0);
    }
    for (var C = /* @__PURE__ */ new Set(), Y = (
      /** @type {Batch} */
      y
    ), V = Ns(), O = 0; O < x; O += 1) {
      R && A.nodeType === pr && /** @type {Comment} */
      A.data === mn && (l = /** @type {Comment} */
      A, j = !0, Xe(!1));
      var q = m[O], Ie = n(q, O), pe = p ? null : a.get(Ie);
      pe ? (pe.v && Bt(pe.v, q), pe.i && Bt(pe.i, O), V && Y.unskip_effect(pe.e)) : (pe = Ol(
        a,
        p ? l : Un ?? (Un = $e()),
        q,
        Ie,
        O,
        s,
        t,
        r
      ), p || (pe.e.f |= Je), a.set(Ie, pe)), C.add(Ie);
    }
    if (x === 0 && i && !u && (p ? u = be(() => i(l)) : (u = be(() => i(Un ?? (Un = $e()))), u.f |= Je)), x > C.size && Oi(), R && x > 0 && ne(Nr()), !p)
      if (h.set(Y, C), V) {
        for (const [mr, br] of a)
          C.has(mr) || Y.skip_effect(br.e);
        Y.oncommit(v), Y.ondiscard(c);
      } else
        v(Y);
    j && Xe(!0), b(d);
  }), g = { effect: _, items: a, pending: h, outrogroups: null, fallback: u };
  p = !1, R && (l = A);
}
function Ut(e) {
  for (; e !== null && (e.f & Me) === 0; )
    e = e.next;
  return e;
}
function Al(e, t, r, n, s) {
  var O;
  var i = t.length, l = e.items, a = Ut(e.effect.first), f, u = null, d = [], h = [], p, v, c, _;
  for (_ = 0; _ < i; _ += 1) {
    if (p = t[_], v = s(p, _), c = /** @type {EachItem} */
    l.get(v).e, e.outrogroups !== null)
      for (const q of e.outrogroups)
        q.pending.delete(c), q.done.delete(c);
    if ((c.f & ee) !== 0 && Pr(c), (c.f & Je) !== 0)
      if (c.f ^= Je, c === a)
        Zt(c, null, r);
      else {
        var g = u ? u.next : a;
        c === e.effect.last && (e.effect.last = c.prev), c.prev && (c.prev.next = c.next), c.next && (c.next.prev = c.prev), st(e, u, c), st(e, c, g), Zt(c, g, r), u = c, d = [], h = [], a = Ut(u.next);
        continue;
      }
    if (c !== a) {
      if (f !== void 0 && f.has(c)) {
        if (d.length < h.length) {
          var m = h[0], x;
          u = m.prev;
          var j = d[0], ve = d[d.length - 1];
          for (x = 0; x < d.length; x += 1)
            Zt(d[x], m, r);
          for (x = 0; x < h.length; x += 1)
            f.delete(h[x]);
          st(e, j.prev, ve.next), st(e, u, j), st(e, ve, m), a = m, u = ve, _ -= 1, d = [], h = [];
        } else
          f.delete(c), Zt(c, a, r), st(e, c.prev, c.next), st(e, c, u === null ? e.effect.first : u.next), st(e, u, c), u = c;
        continue;
      }
      for (d = [], h = []; a !== null && a !== c; )
        (f ?? (f = /* @__PURE__ */ new Set())).add(a), h.push(a), a = Ut(a.next);
      if (a === null)
        continue;
    }
    (c.f & Je) === 0 && d.push(c), u = c, a = Ut(c.next);
  }
  if (e.outrogroups !== null) {
    for (const q of e.outrogroups)
      q.pending.size === 0 && (pn(e, Dr(q.done)), (O = e.outrogroups) == null || O.delete(q));
    e.outrogroups.size === 0 && (e.outrogroups = null);
  }
  if (a !== null || f !== void 0) {
    var C = [];
    if (f !== void 0)
      for (c of f)
        (c.f & ee) === 0 && C.push(c);
    for (; a !== null; )
      (a.f & ee) === 0 && a !== e.fallback && C.push(a), a = Ut(a.next);
    var Y = C.length;
    if (Y > 0) {
      var V = i === 0 ? r : null;
      kl(e, C, V);
    }
  }
}
function Ol(e, t, r, n, s, i, l, a) {
  var f = (l & ii) !== 0 ? (l & ai) === 0 ? /* @__PURE__ */ ks(r, !1, !1) : St(r) : null, u = (l & li) !== 0 ? St(s) : null;
  return {
    v: f,
    i: u,
    e: be(() => (i(t, f ?? r, u ?? s, a), () => {
      e.delete(n);
    }))
  };
}
function Zt(e, t, r) {
  if (e.nodes)
    for (var n = e.nodes.start, s = e.nodes.end, i = t && (t.f & Je) === 0 ? (
      /** @type {EffectNodes} */
      t.nodes.start
    ) : r; n !== null; ) {
      var l = (
        /** @type {TemplateNode} */
        /* @__PURE__ */ Ve(n)
      );
      if (i.before(n), n === s)
        return;
      n = l;
    }
}
function st(e, t, r) {
  t === null ? e.effect.first = r : t.next = r, r === null ? e.effect.last = t : r.prev = t;
}
function Pe(e, t) {
  ll(() => {
    var s, i;
    e = ((i = (s = T == null ? void 0 : T.parent) == null ? void 0 : s.nodes) == null ? void 0 : i.start) ?? e;
    var r = e.getRootNode(), n = (
      /** @type {ShadowRoot} */
      r.host ? (
        /** @type {ShadowRoot} */
        r
      ) : (
        /** @type {Document} */
        r.head ?? /** @type {Document} */
        r.ownerDocument.head
      )
    );
    if (!n.querySelector("#" + t.hash)) {
      const l = An("style");
      l.id = t.hash, l.textContent = t.code, n.appendChild(l);
    }
  });
}
function Gn(e, t = !1) {
  var r = t ? " !important;" : ";", n = "";
  for (var s of Object.keys(e)) {
    var i = e[s];
    i != null && i !== "" && (n += " " + s + ": " + i + r);
  }
  return n;
}
function Rl(e, t) {
  if (t) {
    var r = "", n, s;
    return Array.isArray(t) ? (n = t[0], s = t[1]) : n = t, n && (r += Gn(n)), s && (r += Gn(s, !0)), r = r.trim(), r === "" ? null : r;
  }
  return String(e);
}
function Jr(e, t = {}, r, n) {
  for (var s in r) {
    var i = r[s];
    t[s] !== i && (r[s] == null ? e.style.removeProperty(s) : e.style.setProperty(s, i, n));
  }
}
function Qs(e, t, r, n) {
  var s = (
    /** @type {any} */
    e[en]
  );
  if (R || s !== t) {
    var i = Rl(t, n);
    (!R || i !== e.getAttribute("style")) && (i == null ? e.removeAttribute("style") : e.style.cssText = i), e[en] = t;
  } else n && (Array.isArray(n) ? (Jr(e, r == null ? void 0 : r[0], n[0]), Jr(e, r == null ? void 0 : r[1], n[1], "important")) : Jr(e, r, n));
  return n;
}
const Nl = Symbol("is custom element"), Cl = Symbol("is html"), Ml = os ? "link" : "LINK", Pl = os ? "progress" : "PROGRESS";
function _n(e) {
  if (R) {
    var t = !1, r = () => {
      if (!t) {
        if (t = !0, e.hasAttribute("value")) {
          var n = e.value;
          et(e, "value", null), e.value = n;
        }
        if (e.hasAttribute("checked")) {
          var s = e.checked;
          et(e, "checked", null), e.checked = s;
        }
      }
    };
    e[Qt] = r, Ze(r), ps();
  }
}
function Wn(e, t) {
  var r = ei(e);
  r.value === (r.value = // treat null and undefined the same for the initial value
  t ?? void 0) || // @ts-expect-error
  // `progress` elements always need their value set when it's `0`
  e.value === t && (t !== 0 || e.nodeName !== Pl) || (e.value = t ?? "");
}
function et(e, t, r, n) {
  var s = ei(e);
  R && (s[t] = e.getAttribute(t), t === "src" || t === "srcset" || t === "href" && e.nodeName === Ml) || s[t] !== (s[t] = r) && (t === "loading" && (e[xi] = r), r == null ? e.removeAttribute(t) : typeof r != "string" && jl(e).has(t) ? e[t] = r : e.setAttribute(t, r));
}
function ei(e) {
  return (
    /** @type {Record<string | symbol, unknown>} **/
    /** @type {any} */
    e[xr] ?? (e[xr] = {
      [Nl]: e.nodeName.includes("-"),
      [Cl]: e.namespaceURI === vi
    })
  );
}
var Kn = /* @__PURE__ */ new Map();
function jl(e) {
  var t = e.getAttribute("is") || e.nodeName, r = Kn.get(t);
  if (r) return r;
  Kn.set(t, r = /* @__PURE__ */ new Set());
  for (var n, s = e, i = Element.prototype; i !== s; ) {
    n = _i(s);
    for (var l in n)
      n[l].set && // better safe than sorry, we don't want spread attributes to mess with HTML content
      l !== "innerHTML" && l !== "textContent" && l !== "innerText" && r.add(l);
    s = ns(s);
  }
  return r;
}
function Jn(e, t, r = t) {
  var n = /* @__PURE__ */ new WeakSet();
  Vi(e, "input", async (s) => {
    var i = s ? e.defaultValue : e.value;
    if (i = Xr(e) ? Zr(i) : i, r(i), y !== null && n.add(y), await hl(), i !== (i = t())) {
      var l = e.selectionStart, a = e.selectionEnd, f = e.value.length;
      if (e.value = i ?? "", a !== null) {
        var u = e.value.length;
        l === a && a === f && u > f ? (e.selectionStart = u, e.selectionEnd = u) : (e.selectionStart = l, e.selectionEnd = Math.min(a, u));
      }
    }
  }), // If we are hydrating and the value has since changed,
  // then use the updated value from the input instead.
  (R && e.defaultValue !== e.value || // If defaultValue is set, then value == defaultValue
  // TODO Svelte 6: remove input.value check and set to empty string?
  jn(t) == null && e.value) && (r(Xr(e) ? Zr(e.value) : e.value), y !== null && n.add(y)), Nn(() => {
    var s = t();
    if (e === document.activeElement) {
      var i = (
        /** @type {Batch} */
        y
      );
      if (n.has(i))
        return;
    }
    Xr(e) && s === Zr(e.value) || e.type === "date" && !s && !e.value || s !== e.value && (e.value = s ?? "");
  });
}
function Xr(e) {
  var t = e.type;
  return t === "number" || t === "range";
}
function Zr(e) {
  return e === "" ? null : +e;
}
let wr = !1;
function Il(e) {
  var t = wr;
  try {
    return wr = !1, [e(), wr];
  } finally {
    wr = t;
  }
}
function F(e, t, r, n) {
  var ve;
  var s = !0, i = (r & ui) !== 0, l = (r & ci) !== 0, a = (
    /** @type {V} */
    n
  ), f = !0, u = (
    /** @type {Derived<V> | undefined} */
    void 0
  ), d = () => l && s ? (u ?? (u = /* @__PURE__ */ ir(
    /** @type {() => V} */
    n
  )), b(u)) : (f && (f = !1, a = l ? jn(
    /** @type {() => V} */
    n
  ) : (
    /** @type {V} */
    n
  )), a);
  let h;
  if (i) {
    var p = $r in e || as in e;
    h = ((ve = $t(e, t)) == null ? void 0 : ve.set) ?? (p && t in e ? (C) => e[t] = C : void 0);
  }
  var v, c = !1;
  i ? [v, c] = Il(() => (
    /** @type {V} */
    e[t]
  )) : v = /** @type {V} */
  e[t], v === void 0 && n !== void 0 && (v = d(), h && (ji(), h(v)));
  var _;
  if (_ = () => {
    var C = (
      /** @type {V} */
      e[t]
    );
    return C === void 0 ? d() : (f = !0, C);
  }, (r & fi) === 0)
    return _;
  if (h) {
    var g = e.$$legacy;
    return (
      /** @type {() => V} */
      (function(C, Y) {
        return arguments.length > 0 ? ((!Y || g || c) && h(Y ? _() : C), C) : _();
      })
    );
  }
  var m = !1, x = ((r & oi) !== 0 ? ir : gs)(() => (m = !1, _()));
  i && b(x);
  var j = (
    /** @type {Effect} */
    T
  );
  return (
    /** @type {() => V} */
    (function(C, Y) {
      if (arguments.length > 0) {
        const V = Y ? b(x) : i ? vt(C) : C;
        return D(x, V), m = !0, a !== void 0 && (a = V), C;
      }
      return nt && m || (j.f & G) !== 0 ? x.v : b(x);
    })
  );
}
function Ll(e) {
  return new Dl(e);
}
var Ke, me;
class Dl {
  /**
   * @param {ComponentConstructorOptions & {
   *  component: any;
   * }} options
   */
  constructor(t) {
    /** @type {any} */
    E(this, Ke);
    /** @type {Record<string, any>} */
    E(this, me);
    var i;
    var r = /* @__PURE__ */ new Map(), n = (l, a) => {
      var f = /* @__PURE__ */ ks(a, !1, !1);
      return r.set(l, f), f;
    };
    const s = new Proxy(
      { ...t.props || {}, $$events: {} },
      {
        get(l, a) {
          return b(r.get(a) ?? n(a, Reflect.get(l, a)));
        },
        has(l, a) {
          return a === as ? !0 : (b(r.get(a) ?? n(a, Reflect.get(l, a))), Reflect.has(l, a));
        },
        set(l, a, f) {
          return D(r.get(a) ?? n(a, f), f), Reflect.set(l, a, f);
        }
      }
    );
    w(this, me, (t.hydrate ? xl : Xs)(t.component, {
      target: t.target,
      anchor: t.anchor,
      props: s,
      context: t.context,
      intro: t.intro ?? !1,
      recover: t.recover,
      transformError: t.transformError
    })), (!((i = t == null ? void 0 : t.props) != null && i.$$host) || t.sync === !1) && P(), w(this, Ke, s.$$events);
    for (const l of Object.keys(o(this, me)))
      l === "$set" || l === "$destroy" || l === "$on" || sr(this, l, {
        get() {
          return o(this, me)[l];
        },
        /** @param {any} value */
        set(a) {
          o(this, me)[l] = a;
        },
        enumerable: !0
      });
    o(this, me).$set = /** @param {Record<string, any>} next */
    (l) => {
      Object.assign(s, l);
    }, o(this, me).$destroy = () => {
      El(o(this, me));
    };
  }
  /** @param {Record<string, any>} props */
  $set(t) {
    o(this, me).$set(t);
  }
  /**
   * @param {string} event
   * @param {(...args: any[]) => any} callback
   * @returns {any}
   */
  $on(t, r) {
    o(this, Ke)[t] = o(this, Ke)[t] || [];
    const n = (...s) => r.call(this, ...s);
    return o(this, Ke)[t].push(n), () => {
      o(this, Ke)[t] = o(this, Ke)[t].filter(
        /** @param {any} fn */
        (s) => s !== n
      );
    };
  }
  $destroy() {
    o(this, me).$destroy();
  }
}
Ke = new WeakMap(), me = new WeakMap();
let ti;
typeof HTMLElement == "function" && (ti = class extends HTMLElement {
  /**
   * @param {*} $$componentCtor
   * @param {*} $$slots
   * @param {ShadowRootInit | undefined} shadow_root_init
   */
  constructor(t, r, n) {
    super();
    /** The Svelte component constructor */
    M(this, "$$ctor");
    /** Slots */
    M(this, "$$s");
    /** @type {any} The Svelte component instance */
    M(this, "$$c");
    /** Whether or not the custom element is connected */
    M(this, "$$cn", !1);
    /** @type {Record<string, any>} Component props data */
    M(this, "$$d", {});
    /** `true` if currently in the process of reflecting component props back to attributes */
    M(this, "$$r", !1);
    /** @type {Record<string, CustomElementPropDefinition>} Props definition (name, reflected, type etc) */
    M(this, "$$p_d", {});
    /** @type {Record<string, EventListenerOrEventListenerObject[]>} Event listeners */
    M(this, "$$l", {});
    /** @type {Map<EventListenerOrEventListenerObject, Function>} Event listener unsubscribe functions */
    M(this, "$$l_u", /* @__PURE__ */ new Map());
    /** @type {any} The managed render effect for reflecting attributes */
    M(this, "$$me");
    /** @type {ShadowRoot | null} The ShadowRoot of the custom element */
    M(this, "$$shadowRoot", null);
    this.$$ctor = t, this.$$s = r, n && (this.$$shadowRoot = this.attachShadow(n));
  }
  /**
   * @param {string} type
   * @param {EventListenerOrEventListenerObject} listener
   * @param {boolean | AddEventListenerOptions} [options]
   */
  addEventListener(t, r, n) {
    if (this.$$l[t] = this.$$l[t] || [], this.$$l[t].push(r), this.$$c) {
      const s = this.$$c.$on(t, r);
      this.$$l_u.set(r, s);
    }
    super.addEventListener(t, r, n);
  }
  /**
   * @param {string} type
   * @param {EventListenerOrEventListenerObject} listener
   * @param {boolean | AddEventListenerOptions} [options]
   */
  removeEventListener(t, r, n) {
    if (super.removeEventListener(t, r, n), this.$$c) {
      const s = this.$$l_u.get(r);
      s && (s(), this.$$l_u.delete(r));
    }
  }
  async connectedCallback() {
    if (this.$$cn = !0, !this.$$c) {
      let r = function(i) {
        return (l) => {
          const a = An("slot");
          i !== "default" && (a.name = i), z(l, a);
        };
      };
      var t = r;
      if (await Promise.resolve(), !this.$$cn || this.$$c)
        return;
      const n = {}, s = Fl(this);
      for (const i of this.$$s)
        i in s && (i === "default" && !this.$$d.children ? (this.$$d.children = r(i), n.default = !0) : n[i] = r(i));
      for (const i of this.attributes) {
        const l = this.$$g_p(i.name);
        l in this.$$d || (this.$$d[l] = kr(l, i.value, this.$$p_d, "toProp"));
      }
      for (const i in this.$$p_d)
        !(i in this.$$d) && this[i] !== void 0 && (this.$$d[i] = this[i], delete this[i]);
      this.$$c = Ll({
        component: this.$$ctor,
        target: this.$$shadowRoot || this,
        props: {
          ...this.$$d,
          $$slots: n,
          $$host: this
        }
      }), this.$$me = sl(() => {
        Nn(() => {
          var i;
          this.$$r = !0;
          for (const l of Tr(this.$$c)) {
            if (!((i = this.$$p_d[l]) != null && i.reflect)) continue;
            this.$$d[l] = this.$$c[l];
            const a = kr(
              l,
              this.$$d[l],
              this.$$p_d,
              "toAttribute"
            );
            a == null ? this.removeAttribute(this.$$p_d[l].attribute || l) : this.setAttribute(this.$$p_d[l].attribute || l, a);
          }
          this.$$r = !1;
        });
      });
      for (const i in this.$$l)
        for (const l of this.$$l[i]) {
          const a = this.$$c.$on(i, l);
          this.$$l_u.set(l, a);
        }
      this.$$l = {};
    }
  }
  // We don't need this when working within Svelte code, but for compatibility of people using this outside of Svelte
  // and setting attributes through setAttribute etc, this is helpful
  /**
   * @param {string} attr
   * @param {string} _oldValue
   * @param {string} newValue
   */
  attributeChangedCallback(t, r, n) {
    var s;
    this.$$r || (t = this.$$g_p(t), this.$$d[t] = kr(t, n, this.$$p_d, "toProp"), (s = this.$$c) == null || s.$set({ [t]: this.$$d[t] }));
  }
  disconnectedCallback() {
    this.$$cn = !1, Promise.resolve().then(() => {
      !this.$$cn && this.$$c && (this.$$c.$destroy(), this.$$me(), this.$$c = void 0);
    });
  }
  /**
   * @param {string} attribute_name
   */
  $$g_p(t) {
    return Tr(this.$$p_d).find(
      (r) => this.$$p_d[r].attribute === t || !this.$$p_d[r].attribute && r.toLowerCase() === t
    ) || t;
  }
});
function kr(e, t, r, n) {
  var i;
  const s = (i = r[e]) == null ? void 0 : i.type;
  if (t = s === "Boolean" && typeof t != "boolean" ? t != null : t, !n || !r[e])
    return t;
  if (n === "toAttribute")
    switch (s) {
      case "Object":
      case "Array":
        return t == null ? null : JSON.stringify(t);
      case "Boolean":
        return t ? "" : null;
      case "Number":
        return t ?? null;
      default:
        return t;
    }
  else
    switch (s) {
      case "Object":
      case "Array":
        return t && JSON.parse(t);
      case "Boolean":
        return t;
      // conversion already handled above
      case "Number":
        return t != null ? +t : t;
      default:
        return t;
    }
}
function Fl(e) {
  const t = {};
  return e.childNodes.forEach((r) => {
    t[
      /** @type {Element} node */
      r.slot || "default"
    ] = !0;
  }), t;
}
function je(e, t, r, n, s, i) {
  let l = class extends ti {
    constructor() {
      super(e, r, s), this.$$p_d = t;
    }
    static get observedAttributes() {
      return Tr(t).map(
        (a) => (t[a].attribute || a).toLowerCase()
      );
    }
  };
  return Tr(t).forEach((a) => {
    sr(l.prototype, a, {
      get() {
        return this.$$c && a in this.$$c ? this.$$c[a] : this.$$d[a];
      },
      set(f) {
        var h;
        f = kr(a, f, t), this.$$d[a] = f;
        var u = this.$$c;
        if (u) {
          var d = (h = $t(u, a)) == null ? void 0 : h.get;
          d ? u[a] = f : u.$set({ [a]: f });
        }
      }
    });
  }), n.forEach((a) => {
    sr(l.prototype, a, {
      get() {
        var f;
        return (f = this.$$c) == null ? void 0 : f[a];
      }
    });
  }), e.element = /** @type {any} */
  l, l;
}
function Hr(e, t, r) {
  return e.dispatchEvent(new CustomEvent(t, { detail: r, bubbles: !0, composed: !0 }));
}
var zl = /* @__PURE__ */ W('<p class="error svelte-1rq3t8b" role="alert"> </p>'), ql = /* @__PURE__ */ W('<div class="row svelte-1rq3t8b"><input type="color" class="picker svelte-1rq3t8b"/> <input type="text" class="hex svelte-1rq3t8b" placeholder="#rrggbb"/></div> <!>', 1);
const Bl = {
  hash: "svelte-1rq3t8b",
  code: ".row.svelte-1rq3t8b {display:flex;gap:var(--h-space-2);align-items:center;}.picker.svelte-1rq3t8b {inline-size:2.5rem;block-size:2.25rem;padding:0;}.hex.svelte-1rq3t8b {max-inline-size:10rem;font-family:var(--h-mono);}.hex[aria-invalid='true'].svelte-1rq3t8b {border-color:var(--h-danger);}.error.svelte-1rq3t8b {margin:var(--h-space-1) 0 0;color:var(--h-danger);font-size:var(--h-text-sm);}input.svelte-1rq3t8b {font:inherit;padding:var(--h-space-2) var(--h-space-3);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);}"
};
function Hl(e, t) {
  Ee(t, !0), Pe(e, Bl);
  let r = F(t, "ctx", 7), n = F(t, "value", 7, ""), s = F(t, "readonly", 7, !1), i = F(t, "error", 7, "");
  const l = t.$$host, a = /* @__PURE__ */ Qe(() => /^#[0-9a-fA-F]{6}$/.test(n() ?? "") ? n() : "#000000"), f = (g) => Hr(l, "change", { value: g });
  var u = {
    get ctx() {
      return r();
    },
    set ctx(g) {
      r(g), P();
    },
    get value() {
      return n();
    },
    set value(g = "") {
      n(g), P();
    },
    get readonly() {
      return s();
    },
    set readonly(g = !1) {
      s(g), P();
    },
    get error() {
      return i();
    },
    set error(g = "") {
      i(g), P();
    }
  }, d = ql(), h = kn(d), p = Ce(h);
  _n(p);
  var v = X(p, 2);
  _n(v), we(h);
  var c = X(h, 2);
  {
    var _ = (g) => {
      var m = zl(), x = ie(m, !0);
      le(() => ae(x, i())), z(g, m);
    };
    ar(c, (g) => {
      i() && g(_);
    });
  }
  return le(
    (g, m) => {
      et(p, "aria-label", g), Wn(p, b(a)), p.disabled = s(), et(v, "aria-label", m), Wn(v, n() ?? ""), v.readOnly = s(), et(v, "aria-invalid", i() ? "true" : void 0);
    },
    [() => r().t("brand.color"), () => r().t("brand.hex")]
  ), Vt("input", p, (g) => f(g.currentTarget.value)), Vt("input", v, (g) => f(g.currentTarget.value)), z(e, d), Se(u);
}
gr(["input"]);
customElements.define("x-brand-color", je(
  Hl,
  {
    ctx: { type: "Object" },
    value: { type: "String" },
    field: { type: "Object" },
    readonly: { type: "Boolean" },
    error: { type: "String" }
  },
  [],
  [],
  { mode: "open" }
));
var Vl = /* @__PURE__ */ W('<span class="swatch svelte-134mtg6"><i class="chip svelte-134mtg6"></i> <code> </code></span>');
const Yl = {
  hash: "svelte-134mtg6",
  code: ".swatch.svelte-134mtg6 {display:inline-flex;gap:var(--h-space-2);align-items:center;}.chip.svelte-134mtg6 {display:inline-block;inline-size:1rem;block-size:1rem;border-radius:50%;border:1px solid var(--h-border-strong);}"
};
function Ul(e, t) {
  Ee(t, !0), Pe(e, Yl);
  let r = F(t, "value", 7, "");
  var n = {
    get value() {
      return r();
    },
    set value(a = "") {
      r(a), P();
    }
  }, s = Gs(), i = kn(s);
  {
    var l = (a) => {
      var f = Vl(), u = Ce(f);
      let d;
      var h = X(u, 2), p = ie(h, !0);
      we(f), le(() => {
        et(f, "data-swatch", r()), d = Qs(u, "", d, { "background-color": r() }), ae(p, r());
      }), z(a, f);
    };
    ar(i, (a) => {
      r() && a(l);
    });
  }
  return z(e, s), Se(n);
}
customElements.define("x-color-swatch", je(Ul, { ctx: { type: "Object" }, value: { type: "String" } }, [], [], { mode: "open" }));
var Gl = /* @__PURE__ */ W('<section class="svelte-1cnsmp2"><p class="svelte-1cnsmp2">SEO score: <strong data-testid="seo-score"> </strong> / 100</p> <p class="svelte-1cnsmp2"> </p> <p data-testid="seo-state" class="svelte-1cnsmp2"> </p> <button type="button" class="svelte-1cnsmp2">Refresh</button></section>');
const Wl = {
  hash: "svelte-1cnsmp2",
  code: "section.svelte-1cnsmp2 {display:grid;gap:var(--h-space-2);font-size:var(--h-text-sm);}p.svelte-1cnsmp2 {margin:0;}button.svelte-1cnsmp2 {font:inherit;padding:var(--h-space-2) var(--h-space-4);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);cursor:pointer;}button.svelte-1cnsmp2:hover {background:var(--h-surface-hover);}"
};
function Kl(e, t) {
  Ee(t, !0), Pe(e, Wl);
  let r = F(t, "ctx", 7), n = F(t, "item", 7), s = F(t, "locale", 7, "en");
  const i = t.$$host, l = (O) => typeof O == "string" ? O : "", a = /* @__PURE__ */ Qe(() => {
    var O;
    return l((O = n()) == null ? void 0 : O.fields.title);
  }), f = /* @__PURE__ */ Qe(() => {
    var O, q;
    return l(((O = n()) == null ? void 0 : O.fields.summary) ?? ((q = n()) == null ? void 0 : q.fields.intro));
  }), u = /* @__PURE__ */ Qe(() => (b(a).length >= 30 && b(a).length <= 60 ? 50 : b(a).length > 0 ? 25 : 0) + (b(f).length >= 80 && b(f).length <= 160 ? 50 : b(f).length > 0 ? 25 : 0));
  let d = /* @__PURE__ */ J("loading"), h = /* @__PURE__ */ J(0);
  Ms(() => {
    var pe, mr, br;
    b(h), (pe = n()) == null || pe.updatedAt;
    const O = (mr = n()) == null ? void 0 : mr.id;
    if (!O) return;
    const q = JSON.stringify(((br = n()) == null ? void 0 : br.fields) ?? {});
    let Ie = !0;
    return D(d, "loading"), r().api.get("/items/" + O + "?locale=" + encodeURIComponent(s())).then((ri) => Ie && D(d, JSON.stringify(ri.fields) === q ? "same" : "differs", !0)).catch(() => Ie && D(d, "unpublished")), () => Ie = !1;
  });
  const p = {
    loading: "…",
    same: "published",
    differs: "draft differs",
    unpublished: "not published"
  }, v = /* @__PURE__ */ Qe(() => p[b(d)]);
  var c = {
    get ctx() {
      return r();
    },
    set ctx(O) {
      r(O), P();
    },
    get item() {
      return n();
    },
    set item(O) {
      n(O), P();
    },
    get locale() {
      return s();
    },
    set locale(O = "en") {
      s(O), P();
    }
  }, _ = Gl(), g = Ce(_), m = X(Ce(g)), x = ie(m, !0);
  yn(), we(g);
  var j = X(g, 2), ve = ie(j), C = X(j, 2), Y = ie(C, !0), V = X(C, 2);
  return we(_), le(() => {
    ae(x, b(u)), ae(ve, `Title ${b(a).length ?? ""} characters (30 to 60), summary ${b(f).length ?? ""} (80 to 160).`), ae(Y, b(v));
  }), Vt("click", V, () => {
    el(h), Hr(i, "refresh");
  }), z(e, _), Se(c);
}
gr(["click"]);
customElements.define("x-seo-panel", je(
  Kl,
  {
    ctx: { type: "Object" },
    item: { type: "Object" },
    type: { type: "String" },
    locale: { type: "String" },
    draft: { type: "Boolean" }
  },
  [],
  [],
  { mode: "open" }
));
var Jl = /* @__PURE__ */ W('<button type="button" class="svelte-10702mj">Send to translation</button>');
const Xl = {
  hash: "svelte-10702mj",
  code: "button.svelte-10702mj {font:inherit;padding:var(--h-space-2) var(--h-space-4);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);cursor:pointer;}button.svelte-10702mj:hover {background:var(--h-surface-hover);}"
};
function Zl(e, t) {
  Ee(t, !0), Pe(e, Xl);
  let r = F(t, "ctx", 7), n = F(t, "item", 7);
  const s = t.$$host;
  function i() {
    var f;
    r().toast("success", `Sent "${((f = n()) == null ? void 0 : f.name) ?? "item"}" to translation`), Hr(s, "refresh");
  }
  var l = {
    get ctx() {
      return r();
    },
    set ctx(f) {
      r(f), P();
    },
    get item() {
      return n();
    },
    set item(f) {
      n(f), P();
    }
  }, a = Jl();
  return Vt("click", a, i), z(e, a), Se(l);
}
gr(["click"]);
customElements.define("x-translate-action", je(
  Zl,
  {
    ctx: { type: "Object" },
    item: { type: "Object" },
    type: { type: "String" },
    locale: { type: "String" }
  },
  [],
  [],
  { mode: "open" }
));
var Ql = /* @__PURE__ */ W("<p>The audit log is not available to your account.</p>"), ea = /* @__PURE__ */ W('<a class="svelte-mtmgy0"> </a>'), ta = /* @__PURE__ */ W("<span> </span>"), ra = /* @__PURE__ */ W('<li><!> <time class="svelte-mtmgy0"> </time></li>'), na = /* @__PURE__ */ W('<ul class="svelte-mtmgy0"></ul>');
const sa = {
  hash: "svelte-mtmgy0",
  code: "ul.svelte-mtmgy0 {margin:0;padding:0;list-style:none;display:grid;gap:var(--h-space-2);}a.svelte-mtmgy0 {color:var(--h-accent-strong);}time.svelte-mtmgy0 {margin-inline-start:var(--h-space-2);color:var(--h-text-muted);font-size:var(--h-text-xs);}"
};
function ia(e, t) {
  Ee(t, !0), Pe(e, sa);
  let r = F(t, "ctx", 7), n = /* @__PURE__ */ J(vt([])), s = /* @__PURE__ */ J(!1);
  Ms(() => {
    let p = !0;
    return r().api.get("/audit?limit=5").then((v) => p && D(n, v.items, !0)).catch(() => p && D(s, !0)), () => p = !1;
  });
  const i = (p) => !!p && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(p);
  function l(p, v) {
    p.preventDefault(), r().navigate("/items/" + v);
  }
  var a = {
    get ctx() {
      return r();
    },
    set ctx(p) {
      r(p), P();
    }
  }, f = Gs(), u = kn(f);
  {
    var d = (p) => {
      var v = Ql();
      z(p, v);
    }, h = (p) => {
      var v = na();
      Tl(v, 21, () => b(n), (c) => c.id, (c, _) => {
        var g = ra(), m = Ce(g);
        {
          var x = (V) => {
            var O = ea(), q = ie(O, !0);
            le(() => {
              et(O, "href", r().prefix + "/items/" + b(_).subject), ae(q, b(_).action);
            }), Vt("click", O, (Ie) => l(Ie, b(_).subject)), z(V, O);
          }, j = /* @__PURE__ */ Qe(() => i(b(_).subject)), ve = (V) => {
            var O = ta(), q = ie(O, !0);
            le(() => ae(q, b(_).action)), z(V, O);
          };
          ar(m, (V) => {
            b(j) ? V(x) : V(ve, -1);
          });
        }
        var C = X(m, 2), Y = ie(C, !0);
        we(g), le(
          (V) => {
            et(C, "datetime", b(_).at), ae(Y, V);
          },
          [() => new Date(b(_).at).toLocaleString()]
        ), z(c, g);
      }), we(v), z(p, v);
    };
    ar(u, (p) => {
      b(s) ? p(d) : p(h, -1);
    });
  }
  return z(e, f), Se(a);
}
gr(["click"]);
customElements.define("x-recent-widget", je(ia, { ctx: { type: "Object" } }, [], [], { mode: "open" }));
var la = /* @__PURE__ */ W('<p class="error svelte-rm2e3w" role="alert"> </p>'), aa = /* @__PURE__ */ W('<form class="svelte-rm2e3w"><h2 class="svelte-rm2e3w">Import an article</h2> <label class="svelte-rm2e3w">Title <input name="title" required="" class="svelte-rm2e3w"/></label> <label class="svelte-rm2e3w">Summary <textarea name="summary" class="svelte-rm2e3w"></textarea></label> <!> <button type="submit" class="primary svelte-rm2e3w">Import</button></form>');
const oa = {
  hash: "svelte-rm2e3w",
  code: "form.svelte-rm2e3w {display:grid;gap:var(--h-space-3);max-inline-size:32rem;}label.svelte-rm2e3w {display:grid;gap:var(--h-space-1);font-weight:500;}h2.svelte-rm2e3w {margin:0;font-size:var(--h-text-lg);}.error.svelte-rm2e3w {margin:0;color:var(--h-danger);}button.svelte-rm2e3w {font:inherit;padding:var(--h-space-2) var(--h-space-4);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);cursor:pointer;}button.svelte-rm2e3w:hover {background:var(--h-surface-hover);}button.primary.svelte-rm2e3w {background:var(--h-accent);border-color:var(--h-accent);color:var(--h-accent-contrast);}input.svelte-rm2e3w, textarea.svelte-rm2e3w {font:inherit;padding:var(--h-space-2) var(--h-space-3);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);}"
};
function fa(e, t) {
  Ee(t, !0), Pe(e, oa);
  let r = F(t, "ctx", 7), n = /* @__PURE__ */ J(""), s = /* @__PURE__ */ J(""), i = /* @__PURE__ */ J(!1), l = /* @__PURE__ */ J("");
  async function a(m) {
    m.preventDefault(), D(i, !0), D(l, "");
    try {
      const x = await r().api.post("/collections/article/items", {
        locale: "en",
        fields: { title: b(n), summary: b(s) }
      });
      r().toast("success", "Article imported"), r().navigate("/items/" + x.id);
    } catch (x) {
      D(l, x.message ?? "Import failed", !0);
    } finally {
      D(i, !1);
    }
  }
  var f = {
    get ctx() {
      return r();
    },
    set ctx(m) {
      r(m), P();
    }
  }, u = aa(), d = X(Ce(u), 2), h = X(Ce(d));
  _n(h), we(d);
  var p = X(d, 2), v = X(Ce(p));
  Hi(v), we(p);
  var c = X(p, 2);
  {
    var _ = (m) => {
      var x = la(), j = ie(x, !0);
      le(() => ae(j, b(l))), z(m, x);
    };
    ar(c, (m) => {
      b(l) && m(_);
    });
  }
  var g = X(c, 2);
  return we(u), le(() => g.disabled = b(i)), vl("submit", u, a), Jn(h, () => b(n), (m) => D(n, m)), Jn(v, () => b(s), (m) => D(s, m)), z(e, u), Se(f);
}
customElements.define("x-import-tool", je(fa, { ctx: { type: "Object" } }, [], [], { mode: "open" }));
var ua = /* @__PURE__ */ W('<p class="svelte-s1xavp"> </p>');
const ca = {
  hash: "svelte-s1xavp",
  code: "p.svelte-s1xavp {margin:0 0 var(--h-space-3);padding:var(--h-space-2) var(--h-space-3);border-radius:var(--h-radius);background:var(--h-accent-soft);color:var(--h-text);}"
};
function ha(e, t) {
  Ee(t, !0), Pe(e, ca);
  let r = F(t, "ctx", 7), n = F(t, "type", 7, "");
  var s = {
    get ctx() {
      return r();
    },
    set ctx(a) {
      r(a), P();
    },
    get type() {
      return n();
    },
    set type(a = "") {
      n(a), P();
    }
  }, i = ua(), l = ie(i);
  return le(() => ae(l, `Editing ${n() ?? ""} as ${r().principal.name ?? ""}`)), z(e, i), Se(s);
}
customElements.define("x-banner", je(
  ha,
  {
    ctx: { type: "Object" },
    type: { type: "String" },
    at: { type: "String" }
  },
  [],
  [],
  { mode: "open" }
));
var da = /* @__PURE__ */ W('<span class="hero svelte-1iw7j0f"><span class="swatch svelte-1iw7j0f" data-testid="hero-swatch"></span> <strong data-testid="hero-heading" class="svelte-1iw7j0f"> </strong></span>');
const va = {
  hash: "svelte-1iw7j0f",
  code: ".hero.svelte-1iw7j0f {display:inline-flex;align-items:center;gap:var(--h-space-2);max-inline-size:100%;overflow:hidden;}.hero[data-context='palette'].svelte-1iw7j0f {flex-direction:column;justify-content:center;inline-size:100%;block-size:100%;}.swatch.svelte-1iw7j0f {inline-size:1rem;block-size:1rem;border-radius:50%;border:1px solid var(--h-border-strong);flex:none;}.hero[data-context='palette'].svelte-1iw7j0f .swatch:where(.svelte-1iw7j0f) {inline-size:2rem;block-size:2rem;}strong.svelte-1iw7j0f {overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}"
};
function pa(e, t) {
  Ee(t, !0), Pe(e, va);
  let r = F(t, "props", 23, () => ({})), n = F(t, "context", 7, "card");
  const s = /* @__PURE__ */ Qe(() => {
    var h;
    return /^#[0-9a-fA-F]{6}$/.test(((h = r()) == null ? void 0 : h.color) ?? "") ? r().color : "#cccccc";
  });
  var i = {
    get props() {
      return r();
    },
    set props(h = {}) {
      r(h), P();
    },
    get context() {
      return n();
    },
    set context(h = "card") {
      n(h), P();
    }
  }, l = da(), a = Ce(l);
  let f;
  var u = X(a, 2), d = ie(u, !0);
  return we(l), le(() => {
    var h;
    et(l, "data-context", n()), f = Qs(a, "", f, { background: b(s) }), ae(d, ((h = r()) == null ? void 0 : h.heading) || "Hero");
  }), z(e, l), Se(i);
}
customElements.define("x-hero-preview", je(
  pa,
  {
    ctx: { type: "Object" },
    type: { type: "String" },
    props: { type: "Object" },
    context: { type: "String" }
  },
  [],
  [],
  { mode: "open" }
));
var _a = /* @__PURE__ */ W('<button type="button" class="svelte-xralxl">Export CSV (<span data-testid="export-count"> </span> selected)</button>');
const ga = {
  hash: "svelte-xralxl",
  code: "button.svelte-xralxl {font:inherit;padding:var(--h-space-2) var(--h-space-4);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);cursor:pointer;}button.svelte-xralxl:hover {background:var(--h-surface-hover);}"
};
function ma(e, t) {
  Ee(t, !0), Pe(e, ga);
  let r = F(t, "ctx", 7), n = F(t, "type", 7, ""), s = F(t, "selected", 23, () => []);
  const i = t.$$host, l = /* @__PURE__ */ Qe(() => {
    var p;
    return ((p = s()) == null ? void 0 : p.length) ?? 0;
  });
  function a() {
    r().toast("success", `Exported ${b(l)} ${n()} items`), Hr(i, "refresh");
  }
  var f = {
    get ctx() {
      return r();
    },
    set ctx(p) {
      r(p), P();
    },
    get type() {
      return n();
    },
    set type(p = "") {
      n(p), P();
    },
    get selected() {
      return s();
    },
    set selected(p = []) {
      s(p), P();
    }
  }, u = _a(), d = X(Ce(u)), h = ie(d, !0);
  return yn(), we(u), le(() => ae(h, b(l))), Vt("click", u, a), z(e, u), Se(f);
}
gr(["click"]);
customElements.define("x-export-csv", je(
  ma,
  {
    ctx: { type: "Object" },
    type: { type: "String" },
    selected: { type: "Array" },
    query: { type: "Object" }
  },
  [],
  [],
  { mode: "open" }
));
var ba = /* @__PURE__ */ W('<p data-testid="alt-state" class="svelte-er2bou"> </p>');
const ya = {
  hash: "svelte-er2bou",
  code: "p.svelte-er2bou {margin:0;font-size:var(--h-text-sm);}"
};
function wa(e, t) {
  Ee(t, !0), Pe(e, ya);
  let r = F(t, "item", 7);
  const n = /* @__PURE__ */ Qe(() => {
    var a, f;
    return typeof ((f = (a = r()) == null ? void 0 : a.fields) == null ? void 0 : f.alt) == "string" ? r().fields.alt.trim() : "";
  });
  var s = {
    get item() {
      return r();
    },
    set item(a) {
      r(a), P();
    }
  }, i = ba(), l = ie(i, !0);
  return le(() => ae(l, b(n) ? "Alt text is set." : "Alt text is missing.")), z(e, i), Se(s);
}
customElements.define("x-alt-check", je(
  wa,
  {
    ctx: { type: "Object" },
    item: { type: "Object" },
    type: { type: "String" },
    locale: { type: "String" },
    draft: { type: "Boolean" }
  },
  [],
  [],
  { mode: "open" }
));
var $a = /* @__PURE__ */ W('<p class="svelte-1c9olp0"> </p>');
const xa = {
  hash: "svelte-1c9olp0",
  code: "p.svelte-1c9olp0 {margin:0 0 var(--h-space-3);padding:var(--h-space-2) var(--h-space-3);border-radius:var(--h-radius);background:var(--h-accent-soft);color:var(--h-text);}"
};
function Ea(e, t) {
  Ee(t, !0), Pe(e, xa);
  let r = F(t, "at", 7, "");
  var n = {
    get at() {
      return r();
    },
    set at(l = "") {
      r(l), P();
    }
  }, s = $a(), i = ie(s);
  return le(() => ae(i, `Media guidelines apply (${r() ?? ""})`)), z(e, s), Se(n);
}
customElements.define("x-assets-banner", je(Ea, { ctx: { type: "Object" }, at: { type: "String" } }, [], [], { mode: "open" }));
