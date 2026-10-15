var Qs = Object.defineProperty;
var Mn = (e) => {
  throw TypeError(e);
};
var ei = (e, t, r) => t in e ? Qs(e, t, { enumerable: !0, configurable: !0, writable: !0, value: r }) : e[t] = r;
var C = (e, t, r) => ei(e, typeof t != "symbol" ? t + "" : t, r), zr = (e, t, r) => t.has(e) || Mn("Cannot " + r);
var f = (e, t, r) => (zr(e, t, "read from private field"), r ? r.call(e) : t.get(e)), E = (e, t, r) => t.has(e) ? Mn("Cannot add the same private member more than once") : t instanceof WeakSet ? t.add(e) : t.set(e, r), w = (e, t, r, n) => (zr(e, t, "write to private field"), n ? n.call(e, r) : t.set(e, r), r), y = (e, t, r) => (zr(e, t, "access private method"), r);
var Wn;
typeof window < "u" && ((Wn = window.__svelte ?? (window.__svelte = {})).v ?? (Wn.v = /* @__PURE__ */ new Set())).add("5");
const ti = 1, ri = 2, ni = 16, si = 1, ii = 2, Xn = "[", pn = "[!", Ln = "[?", _n = "]", it = {}, F = Symbol("uninitialized"), li = "http://www.w3.org/1999/xhtml", Zn = !1;
var Qn = Array.isArray, ai = Array.prototype.indexOf, rr = Array.prototype.includes, Lr = Array.from, Er = Object.keys, nr = Object.defineProperty, Ot = Object.getOwnPropertyDescriptor, fi = Object.getOwnPropertyDescriptors, oi = Object.prototype, ui = Array.prototype, es = Object.getPrototypeOf, Pn = Object.isExtensible;
const ci = () => {
};
function hi(e) {
  for (var t = 0; t < e.length; t++)
    e[t]();
}
function ts() {
  var e, t, r = new Promise((n, s) => {
    e = n, t = s;
  });
  return { promise: r, resolve: e, reject: t };
}
const U = 2, jt = 4, Pr = 8, rs = 1 << 24, $e = 16, Se = 32, Ke = 64, Xr = 128, gn = 256, xe = 512, j = 1024, L = 2048, _e = 4096, J = 8192, z = 16384, yt = 32768, xr = 1 << 25, qt = 65536, Sr = 1 << 17, di = 1 << 18, $t = 1 << 19, vi = 1 << 20, Ye = 1 << 25, kr = 1 << 21, Rt = 1 << 22, rt = 1 << 23, Hr = Symbol("$state"), ns = Symbol("component"), pi = Symbol("legacy props"), _i = Symbol(""), br = Symbol("attributes"), gi = Symbol("class"), Zr = Symbol("style"), Ut = Symbol("text"), Zt = Symbol("form reset"), dr = new class extends Error {
  constructor() {
    super(...arguments);
    C(this, "name", "StaleReactionError");
    C(this, "message", "The reaction that called `getAbortSignal()` was re-run or destroyed");
  }
}();
var Kn;
const ss = (
  // We gotta write it like this because after downleveling the pure comment may end up in the wrong location
  !!((Kn = globalThis.document) != null && Kn.contentType) && /* @__PURE__ */ globalThis.document.contentType.includes("xml")
), Dr = 3, vr = 8;
function mi() {
  console.warn("https://svelte.dev/e/derived_inert");
}
function Ir(e) {
  console.warn("https://svelte.dev/e/hydration_mismatch");
}
function bi() {
  console.warn("https://svelte.dev/e/svelte_boundary_reset_noop");
}
let R = !1;
function Ue(e) {
  R = e;
}
let A;
function te(e) {
  if (e === null)
    throw Ir(), it;
  return A = e;
}
function Fr() {
  return te(/* @__PURE__ */ Fe(A));
}
function Ne(e) {
  if (R) {
    if (/* @__PURE__ */ Fe(A) !== null)
      throw Ir(), it;
    A = e;
  }
}
function is(e = 1) {
  if (R) {
    for (var t = e, r = A; t--; )
      r = /** @type {TemplateNode} */
      /* @__PURE__ */ Fe(r);
    A = r;
  }
}
function Tr(e = !0) {
  for (var t = 0, r = A; ; ) {
    if (r.nodeType === vr) {
      var n = (
        /** @type {Comment} */
        r.data
      );
      if (n === _n) {
        if (t === 0) return r;
        t -= 1;
      } else (n === Xn || n === pn || // "[1", "[2", etc. for if blocks
      n[0] === "[" && !isNaN(Number(n.slice(1)))) && (t += 1);
    }
    var s = (
      /** @type {TemplateNode} */
      /* @__PURE__ */ Fe(r)
    );
    e && r.remove(), r = s;
  }
}
function mn(e) {
  if (!e || e.nodeType !== vr)
    throw Ir(), it;
  return (
    /** @type {Comment} */
    e.data
  );
}
function ls(e) {
  return e === this.v;
}
function wi(e, t) {
  return e != e ? t == t : e !== t || e !== null && typeof e == "object" || typeof e == "function";
}
function as(e) {
  return !wi(e, this.v);
}
function yi() {
  throw new Error("https://svelte.dev/e/async_derived_orphan");
}
function $i(e, t, r) {
  throw new Error("https://svelte.dev/e/each_key_duplicate");
}
function Ei(e) {
  throw new Error("https://svelte.dev/e/effect_in_teardown");
}
function xi() {
  throw new Error("https://svelte.dev/e/effect_in_unowned_derived");
}
function Si(e) {
  throw new Error("https://svelte.dev/e/effect_orphan");
}
function ki() {
  throw new Error("https://svelte.dev/e/effect_update_depth_exceeded");
}
function Ti() {
  throw new Error("https://svelte.dev/e/hydration_failed");
}
function Ai() {
  throw new Error("https://svelte.dev/e/state_descriptors_fixed");
}
function Oi() {
  throw new Error("https://svelte.dev/e/state_prototype_fixed");
}
function Ri() {
  throw new Error("https://svelte.dev/e/state_unsafe_mutation");
}
function Ni() {
  throw new Error("https://svelte.dev/e/svelte_boundary_reset_onerror");
}
let Ci = !1, re = null;
function zt(e) {
  re = e;
}
function at(e, t = !1, r) {
  re = {
    p: re,
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
function ft(e) {
  var t = (
    /** @type {ComponentContext} */
    re
  ), r = t.e;
  if (r !== null) {
    t.e = null;
    for (var n of r)
      Rs(n);
  }
  return e !== void 0 && (t.x = e), t.i = !0, re = t.p, fs(e);
}
function fs(e = {}) {
  return nr(e, ns, { value: !0 }), e;
}
function os() {
  return !0;
}
let ut = [];
function us() {
  var e = ut;
  ut = [], hi(e);
}
function Ge(e) {
  if (ut.length === 0 && !Qt) {
    var t = ut;
    queueMicrotask(() => {
      t === ut && us();
    });
  }
  ut.push(e);
}
function Mi() {
  for (; ut.length > 0; )
    us();
}
const Li = -7169;
function M(e, t) {
  e.f = e.f & Li | t;
}
function bn(e) {
  (e.f & xe) !== 0 || e.deps === null ? M(e, j) : M(e, _e);
}
function cs(e, t, r) {
  (e.f & L) !== 0 ? t.add(e) : (e.f & _e) !== 0 && r.add(e), M(e, j);
}
function Pi(e) {
  R && /* @__PURE__ */ Je(e) !== null && xn(e);
}
let Dn = !1;
function hs() {
  Dn || (Dn = !0, document.addEventListener(
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
            (t = r[Zt]) == null || t.call(r);
      });
    },
    // In the capture phase to guarantee we get noticed of it (no possibility of stopPropagation)
    { capture: !0 }
  ));
}
function Vt(e) {
  var t = S, r = T;
  me(null), Ie(null);
  try {
    return e();
  } finally {
    me(t), Ie(r);
  }
}
function Di(e, t, r, n = r) {
  e.addEventListener(t, () => Vt(r));
  const s = (
    /** @type {any} */
    e[Zt]
  );
  s ? e[Zt] = () => {
    s(), n(!0);
  } : e[Zt] = () => n(!0), hs();
}
function Ii(e, t, r, n) {
  const s = jr;
  var i = e.filter((d) => !d.settled), l = t.map(s);
  if (r.length === 0 && i.length === 0) {
    n(l);
    return;
  }
  var a = (
    /** @type {Effect} */
    T
  ), o = Fi(), u = i.length === 1 ? i[0].promise : i.length > 1 ? Promise.all(i.map((d) => d.promise)) : null;
  function h(d) {
    if ((a.f & z) === 0) {
      o();
      try {
        n([...l, ...d]);
      } catch (c) {
        Re(c, a);
      }
      Ar();
    }
  }
  var v = ds();
  if (r.length === 0) {
    u.then(() => h([])).finally(v);
    return;
  }
  function p() {
    Promise.all(r.map((d) => /* @__PURE__ */ ji(d))).then(h).catch((d) => Re(d, a)).finally(v);
  }
  u ? u.then(() => {
    if ((a.f & z) !== 0) {
      v();
      return;
    }
    o(), p(), Ar();
  }) : p();
}
function Fi() {
  var e = (
    /** @type {Effect} */
    T
  ), t = S, r = re, n = (
    /** @type {Batch} */
    b
  );
  return function(i = !0) {
    Ie(e), me(t), zt(r), i && (e.f & z) === 0 && (n == null || n.activate(), n == null || n.apply());
  };
}
function Ar(e = !0) {
  Ie(null), me(null), zt(null), e && (b == null || b.deactivate());
}
function ds() {
  var e = (
    /** @type {Effect} */
    T
  ), t = e.b, r = (
    /** @type {Batch} */
    b
  ), n = !!(t != null && t.is_rendered());
  return t == null || t.update_pending_count(1, r), r.increment(n, e), () => {
    t == null || t.update_pending_count(-1, r), r.decrement(n, e);
  };
}
// @__NO_SIDE_EFFECTS__
function jr(e) {
  var t = U | L;
  return T !== null && (T.f |= $t), {
    ctx: re,
    deps: null,
    effects: null,
    equals: ls,
    f: t,
    fn: e,
    reactions: null,
    rv: 0,
    v: (
      /** @type {V} */
      F
    ),
    wv: 0,
    parent: T,
    ac: null
  };
}
const Gt = Symbol("obsolete");
// @__NO_SIDE_EFFECTS__
function ji(e, t, r) {
  let n = (
    /** @type {Effect | null} */
    T
  );
  n === null && yi();
  var s = (
    /** @type {Promise<V>} */
    /** @type {unknown} */
    void 0
  ), i = wt(
    /** @type {V} */
    F
  ), l = !S, a = /* @__PURE__ */ new Set();
  return el(() => {
    var d, c;
    var o = (
      /** @type {Effect} */
      T
    ), u = ts();
    s = u.promise;
    try {
      Promise.resolve(e()).then(u.resolve, (g) => {
        g !== dr && u.reject(g);
      }).finally(Ar);
    } catch (g) {
      u.reject(g), Ar();
    }
    var h = (
      /** @type {Batch} */
      b
    );
    if (l) {
      if ((o.f & yt) !== 0)
        var v = ds();
      if (
        // boundary can be null if the async derived is inside an $effect.root not connected to the component render tree
        (d = n.b) != null && d.is_rendered()
      )
        (c = h.async_deriveds.get(o)) == null || c.reject(Gt);
      else
        for (const g of a.values())
          g.reject(Gt);
      a.add(u), h.async_deriveds.set(o, u);
    }
    const p = (g, _ = void 0) => {
      v == null || v(), a.delete(u), _ !== Gt && (h.activate(), _ ? (i.f |= rt, Ht(i, _)) : ((i.f & rt) !== 0 && (i.f ^= rt), Ht(i, g)), h.deactivate());
    };
    u.promise.then(p, (g) => p(null, g || "unknown"));
  }), As(() => {
    for (const o of a)
      o.reject(Gt);
  }), new Promise((o) => {
    function u(h) {
      function v() {
        h === s ? o(i) : u(s);
      }
      h.then(v, v);
    }
    u(s);
  });
}
// @__NO_SIDE_EFFECTS__
function Tt(e) {
  const t = /* @__PURE__ */ jr(e);
  return Ps(t), t;
}
// @__NO_SIDE_EFFECTS__
function qi(e) {
  const t = /* @__PURE__ */ jr(e);
  return t.equals = as, t;
}
function zi(e) {
  var t = e.effects;
  if (t !== null) {
    e.effects = null;
    for (var r = 0; r < t.length; r += 1)
      X(
        /** @type {Effect} */
        t[r]
      );
  }
}
function wn(e) {
  var t, r = T, n = e.parent;
  if (!Xe && n !== null && e.v !== F && // if it was never evaluated before, it's guaranteed to fail downstream, so we try to execute instead
  (n.f & (z | J)) !== 0)
    return mi(), e.v;
  Ie(n);
  try {
    zi(e), t = js(e);
  } finally {
    Ie(r);
  }
  return t;
}
function vs(e) {
  var t = wn(e);
  if (!e.equals(t) && (e.wv = Is(), (!(b != null && b.is_fork) || e.deps === null) && (b !== null ? (b.capture(e, t, !0), nt == null || nt.capture(e, t, !0)) : e.v = t, e.deps === null))) {
    M(e, j);
    return;
  }
  Xe || (q !== null ? (Tn() || b != null && b.is_fork) && q.set(e, t) : bn(e));
}
function Hi(e) {
  var t;
  if (e.effects !== null)
    for (const r of e.effects)
      (r.teardown || r.ac) && ((t = r.teardown) == null || t.call(r), r.ac !== null && Vt(() => {
        r.ac.abort(dr), r.ac = null;
      }), r.fn !== null && (r.teardown = ci), sr(r, 0), Rn(r));
}
function ps(e) {
  if (e.effects !== null)
    for (const t of e.effects)
      t.teardown && t.fn !== null && Bt(t);
}
let Br = null, St = null, b = null, nt = null, q = null, Qr = null, Qt = !1, Vr = !1, er = null, wr = null;
var In = 0;
let Bi = 1;
var Ct, et, dt, Mt, Lt, Pt, ze, Dt, ne, ar, He, we, Te, It, vt, N, en, tn, Wt, rn, _s, gs, kt, Vi, Kt;
const Nr = class Nr {
  constructor() {
    E(this, N);
    C(this, "id", Bi++);
    /** True as soon as `#process` was called */
    E(this, Ct, !1);
    C(this, "linked", !0);
    /** @type {Batch | null} */
    E(this, et, null);
    /** @type {Batch | null} */
    E(this, dt, null);
    /** @type {Map<Effect, ReturnType<typeof deferred<any>>>} */
    C(this, "async_deriveds", /* @__PURE__ */ new Map());
    /**
     * The current values of any signals that are updated in this batch.
     * Tuple format: [value, is_derived] (note: is_derived is false for deriveds, too, if they were overridden via assignment)
     * They keys of this map are identical to `this.#previous`
     * @type {Map<Value, [any, boolean]>}
     */
    C(this, "current", /* @__PURE__ */ new Map());
    /**
     * The values of any signals (sources and deriveds) that are updated in this batch _before_ those updates took place.
     * They keys of this map are identical to `this.#current`
     * @type {Map<Value, any>}
     */
    C(this, "previous", /* @__PURE__ */ new Map());
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
    E(this, Lt, /* @__PURE__ */ new Set());
    /**
     * The number of async effects that are currently in flight
     */
    E(this, Pt, 0);
    /**
     * Async effects that are currently in flight, _not_ inside a pending boundary
     * @type {Map<Effect, number>}
     */
    E(this, ze, /* @__PURE__ */ new Map());
    /**
     * A deferred that resolves when the batch is committed, used with `settled()`
     * TODO replace with Promise.withResolvers once supported widely enough
     * @type {{ promise: Promise<void>, resolve: (value?: any) => void, reject: (reason: unknown) => void } | null}
     */
    E(this, Dt, null);
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
    E(this, ne, []);
    /**
     * Effects created while this batch was active.
     * @type {Effect[]}
     */
    E(this, ar, []);
    /**
     * Deferred effects (which run after async work has completed) that are DIRTY
     * @type {Set<Effect>}
     */
    E(this, He, /* @__PURE__ */ new Set());
    /**
     * Deferred effects that are MAYBE_DIRTY
     * @type {Set<Effect>}
     */
    E(this, we, /* @__PURE__ */ new Set());
    /**
     * A map of branches that still exist, but will be destroyed when this batch
     * is committed — we skip over these during `process`.
     * The value contains child effects that were dirty/maybe_dirty before being reset,
     * so they can be rescheduled if the branch survives.
     * @type {Map<Effect, { d: Effect[], m: Effect[] }>}
     */
    E(this, Te, /* @__PURE__ */ new Map());
    /**
     * Inverse of #skipped_branches which we need to tell prior batches to unskip them when committing
     * @type {Set<Effect>}
     */
    E(this, It, /* @__PURE__ */ new Set());
    C(this, "is_fork", !1);
    E(this, vt, !1);
    St === null ? Br = St = this : (w(St, dt, this), w(this, et, St)), St = this;
  }
  /**
   * Add an effect to the #skipped_branches map and reset its children
   * @param {Effect} effect
   */
  skip_effect(t) {
    f(this, Te).has(t) || f(this, Te).set(t, { d: [], m: [] }), f(this, It).delete(t);
  }
  /**
   * Remove an effect from the #skipped_branches map and reschedule
   * any tracked dirty/maybe_dirty child effects
   * @param {Effect} effect
   * @param {(e: Effect) => void} callback
   */
  unskip_effect(t, r = (n) => this.schedule(n)) {
    var n = f(this, Te).get(t);
    if (n) {
      f(this, Te).delete(t);
      for (var s of n.d)
        M(s, L), r(s);
      for (s of n.m)
        M(s, _e), r(s);
    }
    f(this, It).add(t);
  }
  /**
   * Associate a change to a given source with the current
   * batch, noting its previous and current values
   * @param {Value} source
   * @param {any} value
   * @param {boolean} [is_derived]
   */
  capture(t, r, n = !1) {
    t.v !== F && !this.previous.has(t) && this.previous.set(t, t.v), (t.f & rt) === 0 && (this.current.set(t, [r, n]), q == null || q.set(t, r)), this.is_fork || (t.v = r);
  }
  activate() {
    b = this;
  }
  deactivate() {
    b = null, q = null;
  }
  flush() {
    try {
      Vr = !0, b = this, y(this, N, Wt).call(this);
    } finally {
      In = 0, Qr = null, er = null, wr = null, Vr = !1, b = null, q = null, Ce.clear();
    }
  }
  discard() {
    var t;
    for (const r of f(this, Lt)) r(this);
    f(this, Lt).clear();
    for (const r of this.async_deriveds.values())
      r.reject(Gt);
    y(this, N, Kt).call(this), (t = f(this, Dt)) == null || t.resolve();
  }
  /**
   * @param {Effect} effect
   */
  register_created_effect(t) {
    f(this, ar).push(t);
  }
  /**
   * @param {boolean} blocking
   * @param {Effect} effect
   */
  increment(t, r) {
    if (w(this, Pt, f(this, Pt) + 1), t) {
      let n = f(this, ze).get(r) ?? 0;
      f(this, ze).set(r, n + 1);
    }
  }
  /**
   * @param {boolean} blocking
   * @param {Effect} effect
   */
  decrement(t, r) {
    if (w(this, Pt, f(this, Pt) - 1), t) {
      let n = f(this, ze).get(r) ?? 0;
      n === 1 ? f(this, ze).delete(r) : f(this, ze).set(r, n - 1);
    }
    f(this, vt) || (w(this, vt, !0), Ge(() => {
      w(this, vt, !1), this.linked && this.flush();
    }));
  }
  /**
   * @param {Set<Effect>} dirty_effects
   * @param {Set<Effect>} maybe_dirty_effects
   */
  transfer_effects(t, r) {
    for (const n of t)
      f(this, He).add(n);
    for (const n of r)
      f(this, we).add(n);
    t.clear(), r.clear();
  }
  /** @param {(batch: Batch) => void} fn */
  oncommit(t) {
    f(this, Mt).add(t);
  }
  /** @param {(batch: Batch) => void} fn */
  ondiscard(t) {
    f(this, Lt).add(t);
  }
  settled() {
    return (f(this, Dt) ?? w(this, Dt, ts())).promise;
  }
  static ensure() {
    if (b === null) {
      const t = b = new Nr();
      !Vr && !Qt && Ge(() => {
        f(t, Ct) || t.flush();
      });
    }
    return b;
  }
  apply() {
    {
      q = null;
      return;
    }
  }
  /**
   *
   * @param {Effect} effect
   */
  schedule(t) {
    var r;
    if (Qr = t, (r = t.b) != null && r.is_pending && (t.f & (jt | Pr | rs)) !== 0 && (t.f & yt) === 0) {
      t.b.defer_effect(t);
      return;
    }
    f(this, ne).push(t);
  }
};
Ct = new WeakMap(), et = new WeakMap(), dt = new WeakMap(), Mt = new WeakMap(), Lt = new WeakMap(), Pt = new WeakMap(), ze = new WeakMap(), Dt = new WeakMap(), ne = new WeakMap(), ar = new WeakMap(), He = new WeakMap(), we = new WeakMap(), Te = new WeakMap(), It = new WeakMap(), vt = new WeakMap(), N = new WeakSet(), en = function() {
  if (this.is_fork) return !0;
  for (const n of f(this, ze).keys()) {
    for (var t = n, r = !1; t.parent !== null; ) {
      if (f(this, Te).has(t)) {
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
tn = function() {
  var t = [];
  for (const i of f(this, ne))
    if (!((i.f & z) !== 0 || (i.f & (L | _e)) === 0)) {
      for (var r = i, n = !1; r.parent !== null; ) {
        r = r.parent;
        var s = r.f;
        if ((s & (Ke | Se)) !== 0) {
          if ((s & j) === 0) {
            n = !0;
            break;
          }
          r.f ^= j;
        }
      }
      n || t.push(r);
    }
  return w(this, ne, []), t;
}, Wt = function() {
  var a, o, u, h;
  w(this, Ct, !0);
  for (const v of f(this, He))
    f(this, we).delete(v), M(v, L), this.schedule(v);
  for (const v of f(this, we))
    M(v, _e), this.schedule(v);
  this.apply();
  for (var t = er = [], r = [], n = wr = []; f(this, ne).length > 0; ) {
    In++ > 1e3 && (y(this, N, Kt).call(this), Yi());
    for (const v of y(this, N, tn).call(this))
      try {
        y(this, N, rn).call(this, v, t, r);
      } catch (p) {
        throw ws(v), y(this, N, en).call(this) || this.discard(), p;
      }
  }
  if (b = null, n.length > 0) {
    var s = Nr.ensure();
    for (const v of n)
      s.schedule(v);
  }
  if (er = null, wr = null, y(this, N, en).call(this)) {
    y(this, N, kt).call(this, r), y(this, N, kt).call(this, t);
    for (const [v, p] of f(this, Te))
      bs(v, p);
    n.length > 0 && /** @type {unknown} */
    y(a = b, N, Wt).call(a);
    return;
  }
  const i = y(this, N, _s).call(this);
  if (i) {
    y(this, N, kt).call(this, r), y(this, N, kt).call(this, t), y(o = i, N, gs).call(o, this);
    return;
  }
  f(this, He).clear(), f(this, we).clear();
  for (const v of f(this, Mt)) v(this);
  f(this, Mt).clear(), nt = this, Fn(r), Fn(t), nt = null, (u = f(this, Dt)) == null || u.resolve();
  var l = (
    /** @type {Batch | null} */
    /** @type {unknown} */
    b
  );
  if (f(this, Pt) === 0 && (f(this, ne).length === 0 || l !== null) && y(this, N, Kt).call(this), f(this, ne).length > 0)
    if (l !== null) {
      for (const v of f(this, ne))
        f(l, ne).push(v);
      w(this, ne, []);
    } else
      l = this;
  l !== null && (Ce.clear(), y(h = l, N, Wt).call(h));
}, /**
 * Traverse the effect tree, executing effects or stashing
 * them for later execution as appropriate
 * @param {Effect} root
 * @param {Effect[]} effects
 * @param {Effect[]} render_effects
 */
rn = function(t, r, n) {
  t.f ^= j;
  for (var s = t.first; s !== null; ) {
    var i = s.f, l = (i & (Se | Ke)) !== 0, a = l && (i & j) !== 0, o = a || (i & J) !== 0 || f(this, Te).has(s);
    if (!o && s.fn !== null) {
      l ? s.f ^= j : (i & jt) !== 0 ? r.push(s) : pr(s) && ((i & $e) !== 0 && f(this, we).add(s), Bt(s));
      var u = s.first;
      if (u !== null) {
        s = u;
        continue;
      }
    }
    for (; s !== null; ) {
      var h = s.next;
      if (h !== null) {
        s = h;
        break;
      }
      s = s.parent;
    }
  }
}, _s = function() {
  for (var t = f(this, et); t !== null; ) {
    if (!t.is_fork) {
      for (const [r, [, n]] of this.current)
        if (t.current.has(r) && !n)
          return t;
    }
    t = f(t, et);
  }
  return null;
}, /**
 * @param {Batch} batch
 */
gs = function(t) {
  var n;
  for (const [s, i] of t.current)
    !this.previous.has(s) && t.previous.has(s) && this.previous.set(s, t.previous.get(s)), this.current.set(s, i);
  for (const [s, i] of t.async_deriveds) {
    const l = this.async_deriveds.get(s);
    l && i.promise.then(l.resolve).catch(l.reject);
  }
  t.async_deriveds.clear(), this.transfer_effects(f(t, He), f(t, we));
  const r = (s) => {
    var i = s.reactions;
    if (i !== null && !((s.f & U) !== 0 && (s.f & (L | _e)) === 0))
      for (const o of i) {
        var l = o.f;
        if ((l & U) !== 0)
          r(
            /** @type {Derived} */
            o
          );
        else {
          var a = (
            /** @type {Effect} */
            o
          );
          l & (Rt | $e) && !this.async_deriveds.has(a) && (f(this, we).delete(a), M(a, L), this.schedule(a));
        }
      }
  };
  for (const s of this.current.keys())
    r(s);
  this.oncommit(() => t.discard()), y(n = t, N, Kt).call(n), b = this, y(this, N, Wt).call(this);
}, /**
 * @param {Effect[]} effects
 */
kt = function(t) {
  for (var r = 0; r < t.length; r += 1)
    cs(t[r], f(this, He), f(this, we));
}, Vi = function() {
  var v, p;
  for (let d = Br; d !== null; d = f(d, dt)) {
    var t = d.id < this.id, r = [];
    for (const [c, [g, _]] of this.current) {
      if (d.current.has(c)) {
        var n = (
          /** @type {[any, boolean]} */
          d.current.get(c)[0]
        );
        if (t && g !== n)
          d.current.set(c, [g, _]);
        else
          continue;
      }
      r.push(c);
    }
    if (t)
      for (const [c, g] of this.async_deriveds) {
        const _ = d.async_deriveds.get(c);
        _ && g.promise.then(_.resolve).catch(_.reject);
      }
    var s = [...d.current.keys()].filter(
      (c) => !/** @type {[any, boolean]} */
      d.current.get(c)[1]
    );
    if (!(!f(d, Ct) || s.length === 0)) {
      var i = s.filter((c) => !this.current.has(c));
      if (i.length === 0)
        t && d.discard();
      else if (r.length > 0) {
        if (t)
          for (const c of f(this, It))
            d.unskip_effect(c, (g) => {
              var _;
              (g.f & ($e | Rt)) !== 0 ? d.schedule(g) : y(_ = d, N, kt).call(_, [g]);
            });
        d.activate();
        var l = /* @__PURE__ */ new Set(), a = /* @__PURE__ */ new Map();
        for (var o of r)
          ms(o, i, l, a);
        a = /* @__PURE__ */ new Map();
        var u = [...d.current].filter(([c, g]) => {
          const _ = this.current.get(c);
          return _ ? _[0] !== g[0] || _[1] !== g[1] : !0;
        }).map(([c]) => c);
        if (u.length > 0)
          for (const c of f(this, ar))
            (c.f & (z | J | Sr)) === 0 && yn(c, u, a) && ((c.f & (Rt | $e)) !== 0 ? (M(c, L), d.schedule(c)) : f(d, He).add(c));
        if (f(d, ne).length > 0 && !f(d, vt)) {
          d.apply();
          for (var h of y(v = d, N, tn).call(v))
            y(p = d, N, rn).call(p, h, [], []);
        }
        d.deactivate();
      }
    }
  }
}, Kt = function() {
  if (this.linked) {
    var t = f(this, et), r = f(this, dt);
    t === null ? Br = r : w(t, dt, r), r === null ? St = t : w(r, et, t), this.linked = !1;
  }
};
let lt = Nr;
function Y(e) {
  var t = Qt, r = nt;
  nt = null, Qt = !0;
  try {
    for (var n; ; ) {
      if (Mi(), b === null)
        return (
          /** @type {T} */
          n
        );
      b.flush();
    }
  } finally {
    Qt = t, nt = r;
  }
}
function Yi() {
  try {
    ki();
  } catch (e) {
    Re(e, Qr);
  }
}
let be = null;
function Fn(e) {
  var t = e.length;
  if (t !== 0) {
    for (var r = 0; r < t; ) {
      var n = e[r++];
      if ((n.f & (z | J)) === 0 && pr(n) && (be = /* @__PURE__ */ new Set(), Bt(n), n.deps === null && n.first === null && n.nodes === null && n.teardown === null && n.ac === null && Cs(n), (be == null ? void 0 : be.size) > 0)) {
        Ce.clear();
        for (const s of be) {
          if ((s.f & (z | J)) !== 0) continue;
          const i = [s];
          let l = s.parent;
          for (; l !== null; )
            be.has(l) && (be.delete(l), i.push(l)), l = l.parent;
          for (let a = i.length - 1; a >= 0; a--) {
            const o = i[a];
            (o.f & (z | J)) === 0 && Bt(o);
          }
        }
        be.clear();
      }
    }
    be = null;
  }
}
function ms(e, t, r, n) {
  if (!r.has(e) && (r.add(e), e.reactions !== null))
    for (const s of e.reactions) {
      const i = s.f;
      (i & U) !== 0 ? ms(
        /** @type {Derived} */
        s,
        t,
        r,
        n
      ) : (i & (Rt | $e)) !== 0 && (i & L) === 0 && yn(s, t, n) && (M(s, L), $n(
        /** @type {Effect} */
        s
      ));
    }
}
function yn(e, t, r) {
  const n = r.get(e);
  if (n !== void 0) return n;
  if (e.deps !== null)
    for (const s of e.deps) {
      if (rr.call(t, s))
        return !0;
      if ((s.f & U) !== 0 && yn(
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
function $n(e) {
  b.schedule(e);
}
function bs(e, t) {
  if (!((e.f & Se) !== 0 && (e.f & j) !== 0)) {
    (e.f & L) !== 0 ? t.d.push(e) : (e.f & _e) !== 0 && t.m.push(e), M(e, j);
    for (var r = e.first; r !== null; )
      bs(r, t), r = r.next;
  }
}
function ws(e) {
  M(e, j);
  for (var t = e.first; t !== null; )
    ws(t), t = t.next;
}
let Or = /* @__PURE__ */ new Set();
const Ce = /* @__PURE__ */ new Map();
let ys = !1;
function wt(e, t) {
  var r = {
    f: 0,
    // TODO ideally we could skip this altogether, but it causes type errors
    v: e,
    reactions: null,
    equals: ls,
    rv: 0,
    wv: 0
  };
  return r;
}
// @__NO_SIDE_EFFECTS__
function B(e, t) {
  const r = wt(e);
  return Ps(r), r;
}
// @__NO_SIDE_EFFECTS__
function $s(e, t = !1, r = !0) {
  const n = wt(e);
  return t || (n.equals = as), n;
}
function P(e, t, r = !1) {
  S !== null && // since we are untracking the function inside `$inspect.with` we need to add this check
  // to ensure we error if state is set inside an inspect effect
  (!Ee || (S.f & Sr) !== 0) && os() && (S.f & (U | $e | Rt | Sr)) !== 0 && (Pe === null || !Pe.has(e)) && Ri();
  let n = r ? At(t) : t;
  return Ht(e, n, wr);
}
var ot = null, nn = 0;
function Ht(e, t, r = null) {
  if (!e.equals(t)) {
    Xe ? Ce.set(e, t) : Ce.has(e) || Ce.set(e, e.v);
    var n = lt.ensure();
    if (n.capture(e, t), (e.f & U) !== 0) {
      const s = (
        /** @type {Derived} */
        e
      );
      (e.f & L) !== 0 && wn(s), q === null && bn(s);
    }
    e.wv = Is(), ot = null, nn = 0, Es(e, L, r), ot = null, T !== null && (T.f & j) !== 0 && (T.f & (Se | Ke)) === 0 && (he === null ? nl([e]) : he.push(e)), !n.is_fork && Or.size > 0 && !ys && Ui();
  }
  return t;
}
function Ui() {
  ys = !1;
  for (const e of Or) {
    (e.f & j) !== 0 && M(e, _e);
    let t;
    try {
      t = pr(e);
    } catch {
      t = !0;
    }
    t && Bt(e);
  }
  Or.clear();
}
function Gi(e, t = 1) {
  var r = $(e), n = t === 1 ? r++ : r--;
  return P(e, r), n;
}
function tr(e) {
  P(e, e.v + 1);
}
function Es(e, t, r) {
  var n = e.reactions;
  if (n !== null) {
    var s = n.length;
    if (nn += s, nn > 1e5 && ot === null && (ot = /* @__PURE__ */ new Set()), ot !== null) {
      if (ot.has(e)) return;
      ot.add(e);
    }
    for (var i = 0; i < s; i++) {
      var l = n[i], a = l.f, o = (a & L) === 0;
      if (o && M(l, t), (a & Sr) !== 0)
        Or.add(
          /** @type {Effect} */
          l
        );
      else if ((a & U) !== 0) {
        var u = (
          /** @type {Derived} */
          l
        );
        q == null || q.delete(u), Es(u, _e, r);
      } else if (o) {
        var h = (
          /** @type {Effect} */
          l
        );
        (a & $e) !== 0 && be !== null && be.add(h), r !== null ? r.push(h) : $n(h);
      }
    }
  }
}
function At(e) {
  if (typeof e != "object" || e === null || Hr in e || ns in e)
    return e;
  const t = es(e);
  if (t !== oi && t !== ui)
    return e;
  var r = /* @__PURE__ */ new Map(), n = Qn(e), s = /* @__PURE__ */ B(0), i = bt, l = (a) => {
    if (bt === i)
      return a();
    var o = S, u = bt;
    me(null), zn(i);
    var h = a();
    return me(o), zn(u), h;
  };
  return n && r.set("length", /* @__PURE__ */ B(
    /** @type {any[]} */
    e.length
  )), new Proxy(
    /** @type {any} */
    e,
    {
      defineProperty(a, o, u) {
        (!("value" in u) || u.configurable === !1 || u.enumerable === !1 || u.writable === !1) && Ai();
        var h = r.get(o);
        return h === void 0 ? l(() => {
          var v = /* @__PURE__ */ B(u.value);
          return r.set(o, v), v;
        }) : P(h, u.value, !0), !0;
      },
      deleteProperty(a, o) {
        var u = r.get(o);
        if (u === void 0) {
          if (o in a) {
            const h = l(() => /* @__PURE__ */ B(F));
            r.set(o, h), tr(s);
          }
        } else
          P(u, F), tr(s);
        return !0;
      },
      get(a, o, u) {
        var d;
        if (o === Hr)
          return e;
        var h = r.get(o), v = o in a;
        if (h === void 0 && (!v || (d = Ot(a, o)) != null && d.writable) && (h = l(() => {
          var c = At(v ? a[o] : F), g = /* @__PURE__ */ B(c);
          return g;
        }), r.set(o, h)), h !== void 0) {
          var p = $(h);
          return p === F ? void 0 : p;
        }
        return Reflect.get(a, o, u);
      },
      getOwnPropertyDescriptor(a, o) {
        var p;
        (p = this.has) == null || p.call(this, a, o);
        var u = Reflect.getOwnPropertyDescriptor(a, o), h = r.get(o);
        if (h !== void 0) {
          var v = $(h);
          if (v === F)
            return;
          if (u && "value" in u)
            u.value = v;
          else
            return {
              enumerable: !0,
              configurable: !0,
              value: v,
              writable: !0
            };
        }
        return u;
      },
      has(a, o) {
        var p;
        if (o === Hr)
          return !0;
        var u = r.get(o), h = u !== void 0 && u.v !== F || Reflect.has(a, o);
        if (u !== void 0 || T !== null && (!h || (p = Ot(a, o)) != null && p.writable)) {
          u === void 0 && (u = l(() => {
            var d = h ? At(a[o]) : F, c = /* @__PURE__ */ B(d);
            return c;
          }), r.set(o, u));
          var v = $(u);
          if (v === F)
            return !1;
        }
        return h;
      },
      set(a, o, u, h) {
        var D;
        var v = r.get(o), p = o in a;
        if (n && o === "length")
          for (var d = u; d < /** @type {Source<number>} */
          v.v; d += 1) {
            var c = r.get(d + "");
            c !== void 0 ? P(c, F) : d in a && (c = l(() => /* @__PURE__ */ B(F)), r.set(d + "", c));
          }
        if (v === void 0)
          (!p || (D = Ot(a, o)) != null && D.writable) && (v = l(() => /* @__PURE__ */ B(void 0)), P(v, At(u)), r.set(o, v));
        else {
          p = v.v !== F;
          var g = l(() => At(u));
          P(v, g);
        }
        var _ = Reflect.getOwnPropertyDescriptor(a, o);
        if (_ != null && _.set && _.set.call(h, u), !p) {
          if (n && typeof o == "string") {
            var m = (
              /** @type {Source<number>} */
              r.get("length")
            ), x = Number(o);
            Number.isInteger(x) && x >= m.v && P(m, x + 1);
          }
          tr(s);
        }
        return !0;
      },
      ownKeys(a) {
        $(s);
        var o = Reflect.ownKeys(a).filter((v) => {
          var p = r.get(v);
          return p === void 0 || p.v !== F;
        });
        for (var [u, h] of r)
          h.v !== F && !(u in a) && o.push(u);
        return o;
      },
      setPrototypeOf() {
        Oi();
      }
    }
  );
}
var jn, xs, Ss, ks;
function sn() {
  if (jn === void 0) {
    jn = window, xs = /Firefox/.test(navigator.userAgent);
    var e = Element.prototype, t = Node.prototype, r = Text.prototype;
    Ss = Ot(t, "firstChild").get, ks = Ot(t, "nextSibling").get, Pn(e) && (e[gi] = void 0, e[br] = null, e[Zr] = void 0, e.__e = void 0), Pn(r) && (r[Ut] = void 0);
  }
}
function ge(e = "") {
  return document.createTextNode(e);
}
// @__NO_SIDE_EFFECTS__
function Je(e) {
  return (
    /** @type {TemplateNode | null} */
    Ss.call(e)
  );
}
// @__NO_SIDE_EFFECTS__
function Fe(e) {
  return (
    /** @type {TemplateNode | null} */
    ks.call(e)
  );
}
function We(e, t) {
  if (!R)
    return /* @__PURE__ */ Je(e);
  var r = /* @__PURE__ */ Je(A);
  if (r === null)
    r = A.appendChild(ge());
  else if (t && r.nodeType !== Dr) {
    var n = ge();
    return r == null || r.before(n), te(n), n;
  }
  return t && kn(
    /** @type {Text} */
    r
  ), te(r), r;
}
function En(e, t = !1) {
  if (!R) {
    var r = /* @__PURE__ */ Je(e);
    return r instanceof Comment && r.data === "" ? /* @__PURE__ */ Fe(r) : r;
  }
  if (t) {
    if ((A == null ? void 0 : A.nodeType) !== Dr) {
      var n = ge();
      return A == null || A.before(n), te(n), n;
    }
    kn(
      /** @type {Text} */
      A
    );
  }
  return A;
}
function Me(e, t = !1) {
  if (!R)
    return /* @__PURE__ */ Je(e);
  var r = We(e, t);
  return Ne(e), r;
}
function Z(e, t = 1, r = !1) {
  let n = R ? A : e;
  for (var s; t--; )
    s = n, n = /** @type {TemplateNode} */
    /* @__PURE__ */ Fe(n);
  if (!R)
    return n;
  if (r) {
    if ((n == null ? void 0 : n.nodeType) !== Dr) {
      var i = ge();
      return n === null ? s == null || s.after(i) : n.before(i), te(i), i;
    }
    kn(
      /** @type {Text} */
      n
    );
  }
  return te(n), n;
}
function xn(e) {
  e.textContent = "";
}
function Ts() {
  return !1;
}
function Sn(e, t, r) {
  return (
    /** @type {T extends keyof HTMLElementTagNameMap ? HTMLElementTagNameMap[T] : Element} */
    r ? document.createElement(e, { is: r }) : document.createElement(e)
  );
}
function kn(e) {
  if (
    /** @type {string} */
    e.nodeValue.length < 65536
  )
    return;
  let t = e.nextSibling;
  for (; t !== null && t.nodeType === Dr; )
    t.remove(), e.nodeValue += /** @type {string} */
    t.nodeValue, t = e.nextSibling;
}
function Wi(e) {
  var t = T;
  if (t === null)
    return S.f |= rt, e;
  if ((t.f & yt) === 0 && (t.f & jt) === 0)
    throw e;
  Re(e, t);
}
function Re(e, t) {
  if (e === it)
    throw e;
  if (!(t !== null && (t.f & z) !== 0)) {
    for (; t !== null; ) {
      if ((t.f & Xr) !== 0 && (t.f & (z | xr)) === 0) {
        if ((t.f & yt) === 0)
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
function Ki(e) {
  T === null && (S === null && Si(), xi()), Xe && Ei();
}
function Ji(e, t) {
  var r = t.last;
  r === null ? t.last = t.first = e : (r.next = e, e.prev = r, t.last = e);
}
function je(e, t) {
  var r = T;
  r !== null && (r.f & J) !== 0 && (e |= J);
  var n = {
    ctx: re,
    deps: null,
    nodes: null,
    f: e | L | xe,
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
  b == null || b.register_created_effect(n);
  var s = n;
  if ((e & jt) !== 0)
    er !== null ? er.push(n) : lt.ensure().schedule(n);
  else if (t !== null) {
    try {
      Bt(n);
    } catch (l) {
      throw X(n), l;
    }
    s.deps === null && s.teardown === null && s.nodes === null && s.first === s.last && // either `null`, or a singular child
    (s.f & $t) === 0 && (s = s.first, (e & $e) !== 0 && (e & qt) !== 0 && s !== null && (s.f |= qt));
  }
  if (s !== null && (s.parent = r, r !== null && Ji(s, r), S !== null && (S.f & U) !== 0 && (e & Ke) === 0)) {
    var i = (
      /** @type {Derived} */
      S
    );
    (i.effects ?? (i.effects = [])).push(s);
  }
  return n;
}
function Tn() {
  return S !== null && !Ee;
}
function As(e) {
  const t = je(Pr, null);
  return M(t, j), t.teardown = e, t;
}
function Os(e) {
  Ki();
  var t = (
    /** @type {Effect} */
    T.f
  ), r = !S && (t & Se) !== 0 && re !== null && !re.i;
  if (r) {
    var n = (
      /** @type {ComponentContext} */
      re
    );
    (n.e ?? (n.e = [])).push(e);
  } else
    return Rs(e);
}
function Rs(e) {
  return je(jt | vi, e);
}
function Xi(e) {
  lt.ensure();
  const t = je(Ke | $t, e);
  return () => {
    X(t);
  };
}
function Zi(e) {
  lt.ensure();
  const t = je(Ke | $t, e);
  return (r = {}) => new Promise((n) => {
    r.outro ? mt(t, () => {
      X(t), n(void 0);
    }) : (X(t), n(void 0));
  });
}
function Qi(e) {
  return je(jt, e);
}
function el(e) {
  return je(Rt | $t, e);
}
function An(e, t = 0) {
  return je(Pr | t, e);
}
function Le(e, t = [], r = [], n = []) {
  Ii(n, t, r, (s) => {
    je(Pr, () => {
      e(...s.map($));
    });
  });
}
function On(e, t = 0) {
  var r = je($e | t, e);
  return r;
}
function pe(e) {
  return je(Se | $t, e);
}
function Ns(e) {
  var t = e.teardown;
  if (t !== null) {
    const r = Xe, n = S;
    qn(!0), me(null);
    try {
      t.call(null);
    } catch (s) {
      Re(s, e.parent);
    } finally {
      qn(r), me(n);
    }
  }
}
function Rn(e, t = !1) {
  var r = e.first;
  for (e.first = e.last = null; r !== null; ) {
    const s = r.ac;
    s !== null && Vt(() => {
      s.abort(dr);
    });
    var n = r.next;
    (r.f & Ke) !== 0 ? r.parent = null : X(r, t), r = n;
  }
}
function tl(e) {
  for (var t = e.first; t !== null; ) {
    var r = t.next;
    (t.f & Se) === 0 && X(t), t = r;
  }
}
function X(e, t = !0) {
  var r = !1;
  (t || (e.f & di) !== 0) && e.nodes !== null && e.nodes.end !== null && (rl(
    e.nodes.start,
    /** @type {TemplateNode} */
    e.nodes.end
  ), r = !0), e.f |= xr, Rn(e, t && !r), sr(e, 0);
  var n = e.nodes && e.nodes.t;
  if (n !== null)
    for (const i of n)
      i.stop();
  Ns(e), e.f ^= xr, e.f |= z;
  var s = e.parent;
  s !== null && s.first !== null && Cs(e), e.next = e.prev = e.teardown = e.ctx = e.deps = e.fn = e.nodes = e.ac = e.b = null;
}
function rl(e, t) {
  for (; e !== null; ) {
    var r = e === t ? null : /* @__PURE__ */ Fe(e);
    e.remove(), e = r;
  }
}
function Cs(e) {
  var t = e.parent, r = e.prev, n = e.next;
  r !== null && (r.next = n), n !== null && (n.prev = r), t !== null && (t.first === e && (t.first = n), t.last === e && (t.last = r));
}
function mt(e, t, r = !0) {
  var n = [];
  e.f |= gn, Ms(e, n, !0);
  var s = () => {
    r && X(e), t && t();
  }, i = n.length;
  if (i > 0) {
    var l = () => --i || s();
    for (var a of n)
      a.out(l);
  } else
    s();
}
function Ms(e, t, r) {
  if ((e.f & J) === 0) {
    e.f ^= J;
    var n = e.nodes && e.nodes.t;
    if (n !== null)
      for (const a of n)
        (a.is_global || r) && t.push(a);
    for (var s = e.first; s !== null; ) {
      var i = s.next;
      if ((s.f & Ke) === 0) {
        var l = (s.f & qt) !== 0 || // If this is a branch effect without a block effect parent,
        // it means the parent block effect was pruned. In that case,
        // transparency information was transferred to the branch effect.
        (s.f & Se) !== 0 && (e.f & $e) !== 0;
        Ms(s, t, l ? r : !1);
      }
      s = i;
    }
  }
}
function Rr(e) {
  e.f &= ~gn, Ls(e, !0);
}
function Ls(e, t) {
  if ((e.f & gn) === 0 && (e.f & J) !== 0) {
    e.f ^= J, (e.f & j) === 0 && (M(e, L), lt.ensure().schedule(e));
    for (var r = e.first; r !== null; ) {
      var n = r.next, s = (r.f & qt) !== 0 || (r.f & Se) !== 0;
      Ls(r, s ? t : !1), r = n;
    }
    var i = e.nodes && e.nodes.t;
    if (i !== null)
      for (const l of i)
        (l.is_global || t) && l.in();
  }
}
function Nn(e, t) {
  if (e.nodes)
    for (var r = e.nodes.start, n = e.nodes.end; r !== null; ) {
      var s = r === n ? null : /* @__PURE__ */ Fe(r);
      t.append(r), r = s;
    }
}
let Xe = !1;
function qn(e) {
  Xe = e;
}
let S = null, Ee = !1;
function me(e) {
  S = e;
}
let T = null;
function Ie(e) {
  T = e;
}
let Pe = null;
function Ps(e) {
  S !== null && ((S.f & kr) !== 0 || (S.f & U) !== 0) && (Pe ?? (Pe = /* @__PURE__ */ new Set())).add(e);
}
let Q = null, fe = 0, he = null;
function nl(e) {
  he = e;
}
let Ds = 1, ct = 0, bt = ct;
function zn(e) {
  bt = e;
}
function Is() {
  return ++Ds;
}
function pr(e) {
  var t = e.f;
  if ((t & L) !== 0)
    return !0;
  if ((t & _e) !== 0) {
    for (var r = (
      /** @type {Value[]} */
      e.deps
    ), n = r.length, s = 0; s < n; s++) {
      var i = r[s];
      if (pr(
        /** @type {Derived} */
        i
      ) && vs(
        /** @type {Derived} */
        i
      ), i.wv > e.wv)
        return !0;
    }
    (t & xe) !== 0 && // During time traveling we don't want to reset the status so that
    // traversal of the graph in the other batches still happens
    q === null && M(e, j);
  }
  return !1;
}
function Fs(e, t, r = !0) {
  var n = e.reactions;
  if (n !== null && !(Pe !== null && Pe.has(e)))
    for (var s = 0; s < n.length; s++) {
      var i = n[s];
      (i.f & U) !== 0 ? Fs(
        /** @type {Derived} */
        i,
        t,
        !1
      ) : t === i && (r ? M(i, L) : (i.f & j) !== 0 && M(i, _e), $n(
        /** @type {Effect} */
        i
      ));
    }
}
function js(e) {
  var t = Q, r = fe, n = he, s = S, i = Pe, l = re, a = Ee, o = bt, u = e.f;
  Q = /** @type {null | Value[]} */
  null, fe = 0, he = null, S = (u & (Se | Ke)) === 0 ? e : null, Pe = null, zt(e.ctx), Ee = !1, bt = ++ct, e.ac !== null && (Vt(() => {
    e.ac.abort(dr);
  }), e.ac = null);
  try {
    e.f |= kr;
    var h = (
      /** @type {Function} */
      e.fn
    ), v = h();
    e.f |= yt;
    var p = Hn(e);
    if (os() && he !== null && !Ee && p !== null && (e.f & (U | _e | L)) === 0)
      for (var d = 0; d < /** @type {Source[]} */
      he.length; d++)
        Fs(
          he[d],
          /** @type {Effect} */
          e
        );
    if (s !== null && s !== e) {
      if (ct++, s.deps !== null)
        for (let c = 0; c < r; c += 1)
          s.deps[c].rv = ct;
      if (t !== null)
        for (const c of t)
          c.rv = ct;
      he !== null && (n === null ? n = he : n.push(.../** @type {Source[]} */
      he));
    }
    return (e.f & rt) !== 0 && (e.f ^= rt), v;
  } catch (c) {
    return Hn(e), Wi(c);
  } finally {
    e.f ^= kr, Q = t, fe = r, he = n, S = s, Pe = i, zt(l), Ee = a, bt = o;
  }
}
function Hn(e) {
  var s;
  var t = e.deps, r = b == null ? void 0 : b.is_fork;
  if (Q !== null) {
    var n;
    if (r || sr(e, fe), t !== null && fe > 0)
      for (t.length = fe + Q.length, n = 0; n < Q.length; n++)
        t[fe + n] = Q[n];
    else
      e.deps = t = Q;
    if (Tn() && (e.f & xe) !== 0)
      for (n = fe; n < t.length; n++)
        ((s = t[n]).reactions ?? (s.reactions = [])).push(e);
  } else !r && t !== null && fe < t.length && (sr(e, fe), t.length = fe);
  return t;
}
function sl(e, t) {
  let r = t.reactions;
  if (r !== null) {
    var n = ai.call(r, e);
    if (n !== -1) {
      var s = r.length - 1;
      s === 0 ? r = t.reactions = null : (r[n] = r[s], r.pop());
    }
  }
  if (r === null && (t.f & U) !== 0 && // Destroying a child effect while updating a parent effect can cause a dependency to appear
  // to be unused, when in fact it is used by the currently-updating parent. Checking `new_deps`
  // allows us to skip the expensive work of disconnecting and immediately reconnecting it
  (Q === null || !rr.call(Q, t))) {
    var i = (
      /** @type {Derived} */
      t
    );
    (i.f & xe) !== 0 && (i.f ^= xe), i.v !== F && bn(i), i.ac !== null && Vt(() => {
      i.ac.abort(dr), i.ac = null, M(i, L);
    }), Hi(i), sr(i, 0);
  }
}
function sr(e, t) {
  var r = e.deps;
  if (r !== null)
    for (var n = t; n < r.length; n++)
      sl(e, r[n]);
}
function Bt(e) {
  var t = e.f;
  if ((t & z) === 0) {
    M(e, j);
    var r = T;
    T = e;
    try {
      (t & ($e | rs)) !== 0 ? tl(e) : Rn(e), Ns(e);
      var n = js(e);
      e.teardown = typeof n == "function" ? n : null, e.wv = Ds;
      var s;
      Zn && Ci && (e.f & L) !== 0 && e.deps;
    } finally {
      T = r;
    }
  }
}
async function il() {
  await Promise.resolve(), Y();
}
function $(e) {
  var t = e.f, r = (t & U) !== 0;
  if (S !== null && !Ee) {
    var n = T !== null && (T.f & z) !== 0;
    if (!n && (Pe === null || !Pe.has(e))) {
      var s = S.deps;
      if ((S.f & kr) !== 0)
        e.rv < ct && (e.rv = ct, Q === null && s !== null && s[fe] === e ? fe++ : Q === null ? Q = [e] : Q.push(e));
      else {
        S.deps ?? (S.deps = []), rr.call(S.deps, e) || S.deps.push(e);
        var i = e.reactions;
        i === null ? e.reactions = [S] : rr.call(i, S) || i.push(S);
      }
    }
  }
  if (Xe && Ce.has(e))
    return Ce.get(e);
  if (r) {
    var l = (
      /** @type {Derived} */
      e
    );
    if (Xe) {
      var a = l.v;
      return ((l.f & j) === 0 && l.reactions !== null || zs(l)) && (a = wn(l)), Ce.set(l, a), a;
    }
    var o = (l.f & xe) === 0 && !Ee && S !== null && (S.f & xe) !== 0, u = (l.f & yt) === 0;
    pr(l) && (o && (l.f |= xe), vs(l)), o && !u && (ps(l), qs(l));
  }
  if (q != null && q.has(e))
    return q.get(e);
  if ((e.f & rt) !== 0)
    throw e.v;
  return e.v;
}
function qs(e) {
  if (e.f |= xe, e.deps !== null)
    for (const r of e.deps) {
      var t = r.reactions;
      t === null ? r.reactions = [e] : rr.call(t, e) || t.push(e), (r.f & U) !== 0 && (r.f & xe) === 0 && (ps(
        /** @type {Derived} */
        r
      ), qs(
        /** @type {Derived} */
        r
      ));
    }
}
function zs(e) {
  if (e.v === F) return !0;
  if (e.deps === null) return !1;
  for (const t of e.deps)
    if (Ce.has(t) || (t.f & U) !== 0 && zs(
      /** @type {Derived} */
      t
    ))
      return !0;
  return !1;
}
function Hs(e) {
  var t = Ee;
  try {
    return Ee = !0, e();
  } finally {
    Ee = t;
  }
}
const ht = Symbol("events"), Bs = /* @__PURE__ */ new Set(), ln = /* @__PURE__ */ new Set();
function ll(e, t, r, n = {}) {
  function s(i) {
    if (n.capture || an.call(t, i), !i.cancelBubble)
      return Vt(() => r == null ? void 0 : r.call(this, i));
  }
  return e.startsWith("pointer") || e.startsWith("touch") || e === "wheel" ? (s.__removed = !1, Ge(() => {
    s.__removed || t.addEventListener(e, s, n);
  })) : t.addEventListener(e, s, n), s;
}
function al(e, t, r, n, s) {
  var i = { capture: n, passive: s }, l = ll(e, t, r, i);
  (t === document.body || // @ts-ignore
  t === window || // @ts-ignore
  t === document || // Firefox has quirky behavior, it can happen that we still get "canplay" events when the element is already removed
  t instanceof HTMLMediaElement) && As(() => {
    l.__removed = !0, t.removeEventListener(e, l, i);
  });
}
function ir(e, t, r) {
  (t[ht] ?? (t[ht] = {}))[e] = r;
}
function qr(e) {
  for (var t = 0; t < e.length; t++)
    Bs.add(e[t]);
  for (var r of ln)
    r(e);
}
let Yr = null, Ur = !1;
function an(e) {
  var g, _;
  var t = this, r = (
    /** @type {Node} */
    t.ownerDocument
  ), n = e.type, s = ((g = e.composedPath) == null ? void 0 : g.call(e)) || [], i = (
    /** @type {null | Element} */
    s[0] || e.target
  );
  Yr = e, Ur || (Ur = !0, setTimeout(() => {
    Ur = !1, Yr = null;
  }));
  var l = 0, a = Yr === e && e[ht];
  if (a) {
    var o = s.indexOf(a);
    if (o !== -1 && (t === document || t === /** @type {any} */
    window)) {
      e[ht] = t;
      return;
    }
    var u = s.indexOf(t);
    if (u === -1)
      return;
    o <= u && (l = o);
  }
  if (i = /** @type {Element} */
  s[l] || e.target, i !== t) {
    nr(e, "currentTarget", {
      configurable: !0,
      get() {
        return i || r;
      }
    });
    var h = S, v = T;
    me(null), Ie(null);
    try {
      for (var p, d = []; i !== null && i !== t; ) {
        try {
          var c = (_ = i[ht]) == null ? void 0 : _[n];
          c != null && (!/** @type {any} */
          i.disabled || // DOM could've been updated already by the time this is reached, so we check this as well
          // -> the target could not have been disabled because it emits the event in the first place
          e.target === i) && c.call(i, e);
        } catch (m) {
          p ? d.push(m) : p = m;
        }
        if (e.cancelBubble) break;
        l++, i = l < s.length ? (
          /** @type {Element} */
          s[l]
        ) : null;
      }
      if (p) {
        for (let m of d)
          queueMicrotask(() => {
            throw m;
          });
        throw p;
      }
    } finally {
      e[ht] = t, delete e.currentTarget, me(h), Ie(v);
    }
  }
}
var Jn;
const Gr = (
  // We gotta write it like this because after downleveling the pure comment may end up in the wrong location
  ((Jn = globalThis == null ? void 0 : globalThis.window) == null ? void 0 : Jn.trustedTypes) && /* @__PURE__ */ globalThis.window.trustedTypes.createPolicy("svelte-trusted-html", {
    /** @param {string} html */
    createHTML: (e) => e
  })
);
function fl(e) {
  return (
    /** @type {string} */
    (Gr == null ? void 0 : Gr.createHTML(e)) ?? e
  );
}
function ol(e) {
  var t = Sn("template");
  return t.innerHTML = fl(e.replaceAll("<!>", "<!---->")), t.content;
}
function Nt(e, t) {
  var r = (
    /** @type {Effect} */
    T
  );
  r.nodes === null && (r.nodes = { start: e, end: t, a: null, t: null });
}
// @__NO_SIDE_EFFECTS__
function oe(e, t) {
  var r = (t & si) !== 0, n = (t & ii) !== 0, s, i = !e.startsWith("<!>");
  return () => {
    if (R)
      return Nt(A, null), A;
    s === void 0 && (s = ol(i ? e : "<!>" + e), r || (s = /** @type {TemplateNode} */
    /* @__PURE__ */ Je(s)));
    var l = (
      /** @type {TemplateNode} */
      n || xs ? document.importNode(s, !0) : s.cloneNode(!0)
    );
    if (r) {
      var a = (
        /** @type {TemplateNode} */
        /* @__PURE__ */ Je(l)
      ), o = (
        /** @type {TemplateNode} */
        l.lastChild
      );
      Nt(a, o);
    } else
      Nt(l, l);
    return l;
  };
}
function Vs() {
  if (R)
    return Nt(A, null), A;
  var e = document.createDocumentFragment(), t = document.createComment(""), r = ge();
  return e.append(t, r), Nt(t, r), e;
}
function V(e, t) {
  if (R) {
    var r = (
      /** @type {Effect & { nodes: EffectNodes }} */
      T
    );
    ((r.f & yt) === 0 || r.nodes.end === null) && (r.nodes.end = A), Fr();
    return;
  }
  e !== null && e.before(
    /** @type {Node} */
    t
  );
}
const ul = ["touchstart", "touchmove"];
function cl(e) {
  return ul.includes(e);
}
function hl(e) {
  let t = 0, r = wt(0), n;
  return () => {
    Tn() && ($(r), An(() => (t === 0 && (n = Hs(() => e(() => tr(r)))), t += 1, () => {
      Ge(() => {
        t -= 1, t === 0 && (n == null || n(), n = void 0, tr(r));
      });
    })));
  };
}
var dl = qt | $t;
function vl(e, t, r, n) {
  new pl(e, t, r, n);
}
var se, fr, de, pt, H, ie, K, le, Ae, _t, tt, Ft, or, ur, Be, Cr, k, Ys, Us, fn, Qe, Gs, on, Jt, yr, un, cn;
class pl {
  /**
   * @param {TemplateNode} node
   * @param {BoundaryProps} props
   * @param {((anchor: Node) => void)} children
   * @param {((error: unknown) => unknown) | undefined} [transform_error]
   */
  constructor(t, r, n, s) {
    E(this, k);
    /** @type {Boundary | null} */
    C(this, "parent");
    C(this, "is_pending", !1);
    /**
     * API-level transformError transform function. Transforms errors before they reach the `failed` snippet.
     * Inherited from parent boundary, or defaults to identity.
     * @type {(error: unknown) => unknown}
     */
    C(this, "transform_error");
    /** @type {TemplateNode} */
    E(this, se);
    /** @type {TemplateNode | null} */
    E(this, fr, R ? A : null);
    /** @type {BoundaryProps} */
    E(this, de);
    /** @type {((anchor: Node) => void)} */
    E(this, pt);
    /** @type {Effect} */
    E(this, H);
    /** @type {Effect | null} */
    E(this, ie, null);
    /** @type {Effect | null} */
    E(this, K, null);
    /** @type {Effect | null} */
    E(this, le, null);
    /** @type {DocumentFragment | null} */
    E(this, Ae, null);
    E(this, _t, 0);
    E(this, tt, 0);
    E(this, Ft, !1);
    /** @type {Set<Effect>} */
    E(this, or, /* @__PURE__ */ new Set());
    /** @type {Set<Effect>} */
    E(this, ur, /* @__PURE__ */ new Set());
    /**
     * A source containing the number of pending async deriveds/expressions.
     * Only created if `$effect.pending()` is used inside the boundary,
     * otherwise updating the source results in needless `Batch.ensure()`
     * calls followed by no-op flushes
     * @type {Source<number> | null}
     */
    E(this, Be, null);
    E(this, Cr, hl(() => (w(this, Be, wt(f(this, _t))), () => {
      w(this, Be, null);
    })));
    var i;
    w(this, se, t), w(this, de, r), w(this, pt, (l) => {
      var a = (
        /** @type {Effect} */
        T
      );
      a.b = this, a.f |= Xr, n(l);
    }), this.parent = /** @type {Effect} */
    T.b, this.transform_error = s ?? ((i = this.parent) == null ? void 0 : i.transform_error) ?? ((l) => l), w(this, H, On(() => {
      if (R) {
        const l = mn(
          /** @type {TemplateNode} */
          f(this, fr)
        );
        Fr();
        const a = l === pn;
        if (l.startsWith(Ln)) {
          const u = JSON.parse(l.slice(Ln.length));
          y(this, k, Us).call(this, u);
        } else a ? y(this, k, Gs).call(this) : y(this, k, Ys).call(this);
      } else
        y(this, k, on).call(this);
    }, dl)), R && w(this, se, A);
  }
  /**
   * Defer an effect inside a pending boundary until the boundary resolves
   * @param {Effect} effect
   */
  defer_effect(t) {
    cs(t, f(this, or), f(this, ur));
  }
  /**
   * Returns `false` if the effect exists inside a boundary whose pending snippet is shown
   * @returns {boolean}
   */
  is_rendered() {
    return !this.is_pending && (!this.parent || this.parent.is_rendered());
  }
  has_pending_snippet() {
    return !!f(this, de).pending;
  }
  /**
   * Update the source that powers `$effect.pending()` inside this boundary,
   * and controls when the current `pending` snippet (if any) is removed.
   * Do not call from inside the class
   * @param {1 | -1} d
   * @param {Batch} batch
   */
  update_pending_count(t, r) {
    y(this, k, un).call(this, t, r), w(this, _t, f(this, _t) + t), !(!f(this, Be) || f(this, Ft)) && (w(this, Ft, !0), Ge(() => {
      w(this, Ft, !1), f(this, Be) && Ht(f(this, Be), f(this, _t));
    }));
  }
  get_effect_pending() {
    return f(this, Cr).call(this), $(
      /** @type {Source<number>} */
      f(this, Be)
    );
  }
  /** @param {unknown} error */
  error(t) {
    if (t === it || !f(this, de).onerror && !f(this, de).failed)
      throw t;
    b != null && b.is_fork ? (f(this, ie) && b.skip_effect(f(this, ie)), f(this, K) && b.skip_effect(f(this, K)), f(this, le) && b.skip_effect(f(this, le)), b.oncommit(() => {
      y(this, k, Qe).call(this) || y(this, k, cn).call(this, t);
    })) : y(this, k, cn).call(this, t);
  }
}
se = new WeakMap(), fr = new WeakMap(), de = new WeakMap(), pt = new WeakMap(), H = new WeakMap(), ie = new WeakMap(), K = new WeakMap(), le = new WeakMap(), Ae = new WeakMap(), _t = new WeakMap(), tt = new WeakMap(), Ft = new WeakMap(), or = new WeakMap(), ur = new WeakMap(), Be = new WeakMap(), Cr = new WeakMap(), k = new WeakSet(), Ys = function() {
  try {
    w(this, ie, pe(() => f(this, pt).call(this, f(this, se))));
  } catch (t) {
    this.error(t);
  }
}, /**
 * @param {unknown} error The deserialized error from the server's hydration comment
 */
Us = function(t) {
  const r = f(this, de).failed, { reset: n, invoke_onerror: s } = y(this, k, fn).call(this, t);
  Ge(s), r && w(this, le, pe(() => {
    r(
      f(this, se),
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
fn = function(t) {
  var r = !1, n = !1;
  const s = () => {
    if (!y(this, k, Qe).call(this)) {
      if (r) {
        bi();
        return;
      }
      r = !0, n && Ni(), f(this, le) !== null && mt(f(this, le), () => {
        w(this, le, null);
      }), y(this, k, yr).call(this, () => {
        y(this, k, on).call(this);
      });
    }
  };
  return { reset: s, invoke_onerror: () => {
    var l, a;
    if (!y(this, k, Qe).call(this))
      try {
        n = !0, (a = (l = f(this, de)).onerror) == null || a.call(l, t, s), n = !1;
      } catch (o) {
        Re(o, f(this, H) && f(this, H).parent);
      }
  } };
}, Qe = function() {
  return (f(this, H).f & (z | xr)) !== 0;
}, Gs = function() {
  const t = f(this, de).pending;
  t && (this.is_pending = !0, w(this, K, pe(() => t(f(this, se)))), Ge(() => {
    if (!y(this, k, Qe).call(this)) {
      var r = w(this, Ae, document.createDocumentFragment()), n = ge(), s = !1;
      if (r.append(n), w(this, ie, y(this, k, yr).call(this, () => {
        try {
          return pe(() => f(this, pt).call(this, n));
        } catch (i) {
          try {
            this.error(i), s = !0;
          } catch (l) {
            Re(l, f(this, H).parent);
          }
          return null;
        }
      })), f(this, ie) === null) {
        w(this, Ae, null), s && y(this, k, Jt).call(
          this,
          /** @type {Batch} */
          b
        );
        return;
      }
      f(this, tt) === 0 && (f(this, se).before(r), w(this, Ae, null), mt(
        /** @type {Effect} */
        f(this, K),
        () => {
          w(this, K, null);
        }
      ), y(this, k, Jt).call(
        this,
        /** @type {Batch} */
        b
      ));
    }
  }));
}, on = function() {
  try {
    if (this.is_pending = this.has_pending_snippet(), w(this, tt, 0), w(this, _t, 0), w(this, ie, pe(() => {
      f(this, pt).call(this, f(this, se));
    })), f(this, tt) > 0) {
      var t = w(this, Ae, document.createDocumentFragment());
      Nn(f(this, ie), t);
      const r = (
        /** @type {(anchor: Node) => void} */
        f(this, de).pending
      );
      w(this, K, pe(() => r(f(this, se))));
    } else
      y(this, k, Jt).call(
        this,
        /** @type {Batch} */
        b
      );
  } catch (r) {
    this.error(r);
  }
}, /**
 * @param {Batch} batch
 */
Jt = function(t) {
  this.is_pending = !1, t.transfer_effects(f(this, or), f(this, ur));
}, /**
 * @template T
 * @param {() => T} fn
 */
yr = function(t) {
  var r = T, n = S, s = re;
  Ie(f(this, H)), me(f(this, H)), zt(f(this, H).ctx);
  try {
    return lt.ensure(), t();
  } finally {
    Ie(r), me(n), zt(s);
  }
}, /**
 * Updates the pending count associated with the currently visible pending snippet,
 * if any, such that we can replace the snippet with content once work is done
 * @param {1 | -1} d
 * @param {Batch} batch
 */
un = function(t, r) {
  var n;
  if (!this.has_pending_snippet()) {
    this.parent && y(n = this.parent, k, un).call(n, t, r);
    return;
  }
  w(this, tt, f(this, tt) + t), f(this, tt) === 0 && (y(this, k, Jt).call(this, r), f(this, K) && mt(f(this, K), () => {
    w(this, K, null);
  }), f(this, Ae) && (f(this, se).before(f(this, Ae)), w(this, Ae, null)));
}, /**
 * @param {unknown} error
 */
cn = function(t) {
  f(this, ie) && (X(f(this, ie)), w(this, ie, null)), f(this, K) && (X(f(this, K)), w(this, K, null)), f(this, le) && (X(f(this, le)), w(this, le, null)), R && (te(
    /** @type {TemplateNode} */
    f(this, fr)
  ), is(), te(Tr()));
  let r = f(this, de).failed;
  const n = (s) => {
    if (y(this, k, Qe).call(this)) return;
    const { reset: i, invoke_onerror: l } = y(this, k, fn).call(this, s);
    l(), r && !y(this, k, Qe).call(this) && w(this, le, y(this, k, yr).call(this, () => {
      try {
        return pe(() => {
          var a = (
            /** @type {Effect} */
            T
          );
          a.b = this, a.f |= Xr, r(
            f(this, se),
            () => s,
            () => i
          );
        });
      } catch (a) {
        return Re(
          a,
          /** @type {Effect} */
          f(this, H).parent
        ), null;
      }
    }));
  };
  Ge(() => {
    if (!y(this, k, Qe).call(this)) {
      var s;
      try {
        s = this.transform_error(t);
      } catch (i) {
        Re(i, f(this, H) && f(this, H).parent);
        return;
      }
      s !== null && typeof s == "object" && typeof /** @type {any} */
      s.then == "function" ? s.then(
        n,
        /** @param {unknown} e */
        (i) => Re(i, f(this, H) && f(this, H).parent)
      ) : n(s);
    }
  });
};
function De(e, t) {
  var r = t == null ? "" : typeof t == "object" ? `${t}` : t;
  r !== /** @type {any} */
  (e[Ut] ?? (e[Ut] = e.nodeValue)) && (e[Ut] = r, e.nodeValue = `${r}`);
}
function Ws(e, t) {
  return Ks(e, t);
}
function _l(e, t) {
  sn(), t.intro = t.intro ?? !1;
  const r = t.target, n = R, s = A;
  try {
    for (var i = /* @__PURE__ */ Je(r); i && (i.nodeType !== vr || /** @type {Comment} */
    i.data !== Xn); )
      i = /* @__PURE__ */ Fe(i);
    if (!i)
      throw it;
    Ue(!0), te(
      /** @type {Comment} */
      i
    );
    const l = Ks(e, { ...t, anchor: i });
    return Ue(!1), /**  @type {Exports} */
    l;
  } catch (l) {
    if (l instanceof Error && l.message.split(`
`).some((a) => a.startsWith("https://svelte.dev/e/")))
      throw l;
    return l !== it && console.warn("Failed to hydrate: ", l), t.recover === !1 && Ti(), sn(), xn(r), Ue(!1), Ws(e, t);
  } finally {
    Ue(n), te(s);
  }
}
const mr = /* @__PURE__ */ new Map();
function Ks(e, { target: t, anchor: r, props: n = {}, events: s, context: i, intro: l = !0, transformError: a }) {
  sn();
  var o = void 0, u = Zi(() => {
    var h = r ?? t.appendChild(ge());
    vl(
      /** @type {TemplateNode} */
      h,
      {
        pending: () => {
        }
      },
      (d) => {
        at({});
        var c = (
          /** @type {ComponentContext} */
          re
        );
        if (i && (c.c = i), s && (n.$$events = s), R && Nt(
          /** @type {TemplateNode} */
          d,
          null
        ), o = e(d, n) || fs(), R && (T.nodes.end = A, A === null || A.nodeType !== vr || /** @type {Comment} */
        A.data !== _n))
          throw Ir(), it;
        ft();
      },
      a
    );
    var v = /* @__PURE__ */ new Set(), p = (d) => {
      for (var c = 0; c < d.length; c++) {
        var g = d[c];
        if (!v.has(g)) {
          v.add(g);
          var _ = cl(g);
          for (const D of [t, document]) {
            var m = mr.get(D);
            m === void 0 && (m = /* @__PURE__ */ new Map(), mr.set(D, m));
            var x = m.get(g);
            x === void 0 ? (D.addEventListener(g, an, { passive: _ }), m.set(g, 1)) : m.set(g, x + 1);
          }
        }
      }
    };
    return p(Lr(Bs)), ln.add(p), () => {
      var _;
      for (var d of v)
        for (const m of [t, document]) {
          var c = (
            /** @type {Map<string, number>} */
            mr.get(m)
          ), g = (
            /** @type {number} */
            c.get(d)
          );
          --g == 0 ? (m.removeEventListener(d, an), c.delete(d), c.size === 0 && mr.delete(m)) : c.set(d, g);
        }
      ln.delete(p), h !== r && ((_ = h.parentNode) == null || _.removeChild(h));
    };
  });
  return hn.set(o, u), o;
}
let hn = /* @__PURE__ */ new WeakMap();
function gl(e, t) {
  const r = hn.get(e);
  return r ? (hn.delete(e), r(t)) : Promise.resolve();
}
var ye, Oe, ae, gt, cr, hr, Mr;
class ml {
  /**
   * @param {TemplateNode} anchor
   * @param {boolean} transition
   */
  constructor(t, r = !0) {
    /** @type {TemplateNode} */
    C(this, "anchor");
    /** @type {Map<Batch, Key>} */
    E(this, ye, /* @__PURE__ */ new Map());
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
    E(this, Oe, /* @__PURE__ */ new Map());
    /**
     * Similar to #onscreen with respect to the keys, but contains branches that are not yet
     * in the DOM, because their insertion is deferred.
     * @type {Map<Key, Branch>}
     */
    E(this, ae, /* @__PURE__ */ new Map());
    /**
     * Keys of effects that are currently outroing
     * @type {Set<Key>}
     */
    E(this, gt, /* @__PURE__ */ new Set());
    /**
     * Whether to pause (i.e. outro) on change, or destroy immediately.
     * This is necessary for `<svelte:element>`
     */
    E(this, cr, !0);
    /**
     * @param {Batch} batch
     */
    E(this, hr, (t) => {
      if (f(this, ye).has(t)) {
        var r = (
          /** @type {Key} */
          f(this, ye).get(t)
        ), n = f(this, Oe).get(r);
        if (n)
          Rr(n), f(this, gt).delete(r);
        else {
          var s = f(this, ae).get(r);
          s && (Rr(s.effect), f(this, Oe).set(r, s.effect), f(this, ae).delete(r), s.fragment.lastChild.remove(), this.anchor.before(s.fragment), n = s.effect);
        }
        for (const [i, l] of f(this, ye)) {
          if (f(this, ye).delete(i), i === t)
            break;
          const a = f(this, ae).get(l);
          a && (X(a.effect), f(this, ae).delete(l));
        }
        for (const [i, l] of f(this, Oe)) {
          if (i === r || f(this, gt).has(i)) continue;
          const a = () => {
            if (Array.from(f(this, ye).values()).includes(i)) {
              var u = document.createDocumentFragment();
              Nn(l, u), u.append(ge()), f(this, ae).set(i, { effect: l, fragment: u });
            } else
              X(l);
            f(this, gt).delete(i), f(this, Oe).delete(i);
          };
          f(this, cr) || !n ? (f(this, gt).add(i), mt(l, a, !1)) : a();
        }
      }
    });
    /**
     * @param {Batch} batch
     */
    E(this, Mr, (t) => {
      f(this, ye).delete(t);
      const r = Array.from(f(this, ye).values());
      for (const [n, s] of f(this, ae))
        r.includes(n) || (X(s.effect), f(this, ae).delete(n));
    });
    this.anchor = t, w(this, cr, r);
  }
  /**
   *
   * @param {any} key
   * @param {null | ((target: TemplateNode) => void)} fn
   */
  ensure(t, r) {
    var n = (
      /** @type {Batch} */
      b
    ), s = Ts();
    if (r && !f(this, Oe).has(t) && !f(this, ae).has(t))
      if (s) {
        var i = document.createDocumentFragment(), l = ge();
        i.append(l), f(this, ae).set(t, {
          effect: pe(() => r(l)),
          fragment: i
        });
      } else
        f(this, Oe).set(
          t,
          pe(() => r(this.anchor))
        );
    if (f(this, ye).set(n, t), s) {
      for (const [a, o] of f(this, Oe))
        a === t ? n.unskip_effect(o) : n.skip_effect(o);
      for (const [a, o] of f(this, ae))
        a === t ? n.unskip_effect(o.effect) : n.skip_effect(o.effect);
      n.oncommit(f(this, hr)), n.ondiscard(f(this, Mr));
    } else
      R && (this.anchor = A), f(this, hr).call(this, n);
  }
}
ye = new WeakMap(), Oe = new WeakMap(), ae = new WeakMap(), gt = new WeakMap(), cr = new WeakMap(), hr = new WeakMap(), Mr = new WeakMap();
function lr(e, t, r = !1) {
  var n;
  R && (n = A, Fr());
  var s = new ml(e), i = r ? qt : 0;
  function l(a, o) {
    if (R) {
      var u = mn(
        /** @type {TemplateNode} */
        n
      );
      if (a !== parseInt(u.substring(1))) {
        var h = Tr();
        te(h), s.anchor = h, Ue(!1), s.ensure(a, o), Ue(!0);
        return;
      }
    }
    s.ensure(a, o);
  }
  On(() => {
    var a = !1;
    t((o, u = 0) => {
      a = !0, l(u, o);
    }), a || l(-1, null);
  }, i);
}
function bl(e, t, r) {
  for (var n = [], s = t.length, i, l = t.length, a = 0; a < s; a++) {
    let v = t[a];
    mt(
      v,
      () => {
        if (i) {
          if (i.pending.delete(v), i.done.add(v), i.pending.size === 0) {
            var p = (
              /** @type {Set<EachOutroGroup>} */
              e.outrogroups
            );
            dn(e, Lr(i.done)), p.delete(i), p.size === 0 && (e.outrogroups = null);
          }
        } else
          l -= 1;
      },
      !1
    );
  }
  if (l === 0) {
    var o = n.length === 0 && r !== null && e.pending.size === 0;
    if (o) {
      var u = (
        /** @type {Element} */
        r
      ), h = (
        /** @type {Element} */
        u.parentNode
      );
      xn(h), h.append(u), e.items.clear();
    }
    dn(e, t, !o);
  } else
    i = {
      pending: new Set(t),
      done: /* @__PURE__ */ new Set()
    }, (e.outrogroups ?? (e.outrogroups = /* @__PURE__ */ new Set())).add(i);
}
function dn(e, t, r = !0) {
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
      i.f |= Ye;
      const l = document.createDocumentFragment();
      Nn(i, l);
    } else
      X(t[s], r);
  }
}
var Bn;
function wl(e, t, r, n, s, i = null) {
  var l = e, a = /* @__PURE__ */ new Map();
  {
    var o = (
      /** @type {Element} */
      e
    );
    l = R ? te(/* @__PURE__ */ Je(o)) : o.appendChild(ge());
  }
  R && Fr();
  var u = null, h = /* @__PURE__ */ qi(() => {
    var m = r();
    return (
      /** @type {V[]} */
      Qn(m) ? m : m == null ? [] : Lr(m)
    );
  }), v = /* @__PURE__ */ new Map(), p = !0;
  function d(m) {
    if ((_.effect.f & z) === 0) {
      _.pending.delete(m);
      var x = $(h);
      _.fallback = u, yl(_, x, l, t, n), u !== null && (x.length === 0 ? (u.f & Ye) === 0 ? Rr(u) : (u.f ^= Ye, Xt(u, null, l)) : mt(u, () => {
        u = null;
      }));
    }
  }
  function c(m) {
    _.pending.delete(m);
  }
  var g = On(() => {
    var m = (
      /** @type {V[]} */
      $(h)
    ), x = m.length;
    let D = !1;
    if (R) {
      var qe = mn(l) === pn;
      qe !== (x === 0) && (l = Tr(), te(l), Ue(!1), D = !0);
    }
    for (var G = /* @__PURE__ */ new Set(), ue = (
      /** @type {Batch} */
      b
    ), W = Ts(), O = 0; O < x; O += 1) {
      R && A.nodeType === vr && /** @type {Comment} */
      A.data === _n && (l = /** @type {Comment} */
      A, D = !0, Ue(!1));
      var I = m[O], ke = n(I, O), ce = p ? null : a.get(ke);
      ce ? (ce.v && Ht(ce.v, I), ce.i && Ht(ce.i, O), W && ue.unskip_effect(ce.e)) : (ce = $l(
        a,
        p ? l : Bn ?? (Bn = ge()),
        I,
        ke,
        O,
        s,
        t,
        r
      ), p || (ce.e.f |= Ye), a.set(ke, ce)), G.add(ke);
    }
    if (x === 0 && i && !u && (p ? u = pe(() => i(l)) : (u = pe(() => i(Bn ?? (Bn = ge()))), u.f |= Ye)), x > G.size && $i(), R && x > 0 && te(Tr()), !p)
      if (v.set(ue, G), W) {
        for (const [_r, gr] of a)
          G.has(_r) || ue.skip_effect(gr.e);
        ue.oncommit(d), ue.ondiscard(c);
      } else
        d(ue);
    D && Ue(!0), $(h);
  }), _ = { effect: g, items: a, pending: v, outrogroups: null, fallback: u };
  p = !1, R && (l = A);
}
function Yt(e) {
  for (; e !== null && (e.f & Se) === 0; )
    e = e.next;
  return e;
}
function yl(e, t, r, n, s) {
  var O;
  var i = t.length, l = e.items, a = Yt(e.effect.first), o, u = null, h = [], v = [], p, d, c, g;
  for (g = 0; g < i; g += 1) {
    if (p = t[g], d = s(p, g), c = /** @type {EachItem} */
    l.get(d).e, e.outrogroups !== null)
      for (const I of e.outrogroups)
        I.pending.delete(c), I.done.delete(c);
    if ((c.f & J) !== 0 && Rr(c), (c.f & Ye) !== 0)
      if (c.f ^= Ye, c === a)
        Xt(c, null, r);
      else {
        var _ = u ? u.next : a;
        c === e.effect.last && (e.effect.last = c.prev), c.prev && (c.prev.next = c.next), c.next && (c.next.prev = c.prev), Ze(e, u, c), Ze(e, c, _), Xt(c, _, r), u = c, h = [], v = [], a = Yt(u.next);
        continue;
      }
    if (c !== a) {
      if (o !== void 0 && o.has(c)) {
        if (h.length < v.length) {
          var m = v[0], x;
          u = m.prev;
          var D = h[0], qe = h[h.length - 1];
          for (x = 0; x < h.length; x += 1)
            Xt(h[x], m, r);
          for (x = 0; x < v.length; x += 1)
            o.delete(v[x]);
          Ze(e, D.prev, qe.next), Ze(e, u, D), Ze(e, qe, m), a = m, u = qe, g -= 1, h = [], v = [];
        } else
          o.delete(c), Xt(c, a, r), Ze(e, c.prev, c.next), Ze(e, c, u === null ? e.effect.first : u.next), Ze(e, u, c), u = c;
        continue;
      }
      for (h = [], v = []; a !== null && a !== c; )
        (o ?? (o = /* @__PURE__ */ new Set())).add(a), v.push(a), a = Yt(a.next);
      if (a === null)
        continue;
    }
    (c.f & Ye) === 0 && h.push(c), u = c, a = Yt(c.next);
  }
  if (e.outrogroups !== null) {
    for (const I of e.outrogroups)
      I.pending.size === 0 && (dn(e, Lr(I.done)), (O = e.outrogroups) == null || O.delete(I));
    e.outrogroups.size === 0 && (e.outrogroups = null);
  }
  if (a !== null || o !== void 0) {
    var G = [];
    if (o !== void 0)
      for (c of o)
        (c.f & J) === 0 && G.push(c);
    for (; a !== null; )
      (a.f & J) === 0 && a !== e.fallback && G.push(a), a = Yt(a.next);
    var ue = G.length;
    if (ue > 0) {
      var W = i === 0 ? r : null;
      bl(e, G, W);
    }
  }
}
function $l(e, t, r, n, s, i, l, a) {
  var o = (l & ti) !== 0 ? (l & ni) === 0 ? /* @__PURE__ */ $s(r, !1, !1) : wt(r) : null, u = (l & ri) !== 0 ? wt(s) : null;
  return {
    v: o,
    i: u,
    e: pe(() => (i(t, o ?? r, u ?? s, a), () => {
      e.delete(n);
    }))
  };
}
function Xt(e, t, r) {
  if (e.nodes)
    for (var n = e.nodes.start, s = e.nodes.end, i = t && (t.f & Ye) === 0 ? (
      /** @type {EffectNodes} */
      t.nodes.start
    ) : r; n !== null; ) {
      var l = (
        /** @type {TemplateNode} */
        /* @__PURE__ */ Fe(n)
      );
      if (i.before(n), n === s)
        return;
      n = l;
    }
}
function Ze(e, t, r) {
  t === null ? e.effect.first = r : t.next = r, r === null ? e.effect.last = t : r.prev = t;
}
function Et(e, t) {
  Qi(() => {
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
      const l = Sn("style");
      l.id = t.hash, l.textContent = t.code, n.appendChild(l);
    }
  });
}
function Vn(e, t = !1) {
  var r = t ? " !important;" : ";", n = "";
  for (var s of Object.keys(e)) {
    var i = e[s];
    i != null && i !== "" && (n += " " + s + ": " + i + r);
  }
  return n;
}
function El(e, t) {
  if (t) {
    var r = "", n, s;
    return Array.isArray(t) ? (n = t[0], s = t[1]) : n = t, n && (r += Vn(n)), s && (r += Vn(s, !0)), r = r.trim(), r === "" ? null : r;
  }
  return String(e);
}
function Wr(e, t = {}, r, n) {
  for (var s in r) {
    var i = r[s];
    t[s] !== i && (r[s] == null ? e.style.removeProperty(s) : e.style.setProperty(s, i, n));
  }
}
function xl(e, t, r, n) {
  var s = (
    /** @type {any} */
    e[Zr]
  );
  if (R || s !== t) {
    var i = El(t, n);
    (!R || i !== e.getAttribute("style")) && (i == null ? e.removeAttribute("style") : e.style.cssText = i), e[Zr] = t;
  } else n && (Array.isArray(n) ? (Wr(e, r == null ? void 0 : r[0], n[0]), Wr(e, r == null ? void 0 : r[1], n[1], "important")) : Wr(e, r, n));
  return n;
}
const Sl = Symbol("is custom element"), kl = Symbol("is html"), Tl = ss ? "link" : "LINK", Al = ss ? "progress" : "PROGRESS";
function vn(e) {
  if (R) {
    var t = !1, r = () => {
      if (!t) {
        if (t = !0, e.hasAttribute("value")) {
          var n = e.value;
          st(e, "value", null), e.value = n;
        }
        if (e.hasAttribute("checked")) {
          var s = e.checked;
          st(e, "checked", null), e.checked = s;
        }
      }
    };
    e[Zt] = r, Ge(r), hs();
  }
}
function Yn(e, t) {
  var r = Js(e);
  r.value === (r.value = // treat null and undefined the same for the initial value
  t ?? void 0) || // @ts-expect-error
  // `progress` elements always need their value set when it's `0`
  e.value === t && (t !== 0 || e.nodeName !== Al) || (e.value = t ?? "");
}
function st(e, t, r, n) {
  var s = Js(e);
  R && (s[t] = e.getAttribute(t), t === "src" || t === "srcset" || t === "href" && e.nodeName === Tl) || s[t] !== (s[t] = r) && (t === "loading" && (e[_i] = r), r == null ? e.removeAttribute(t) : typeof r != "string" && Ol(e).has(t) ? e[t] = r : e.setAttribute(t, r));
}
function Js(e) {
  return (
    /** @type {Record<string | symbol, unknown>} **/
    /** @type {any} */
    e[br] ?? (e[br] = {
      [Sl]: e.nodeName.includes("-"),
      [kl]: e.namespaceURI === li
    })
  );
}
var Un = /* @__PURE__ */ new Map();
function Ol(e) {
  var t = e.getAttribute("is") || e.nodeName, r = Un.get(t);
  if (r) return r;
  Un.set(t, r = /* @__PURE__ */ new Set());
  for (var n, s = e, i = Element.prototype; i !== s; ) {
    n = fi(s);
    for (var l in n)
      n[l].set && // better safe than sorry, we don't want spread attributes to mess with HTML content
      l !== "innerHTML" && l !== "textContent" && l !== "innerText" && r.add(l);
    s = es(s);
  }
  return r;
}
function Gn(e, t, r = t) {
  var n = /* @__PURE__ */ new WeakSet();
  Di(e, "input", async (s) => {
    var i = s ? e.defaultValue : e.value;
    if (i = Kr(e) ? Jr(i) : i, r(i), b !== null && n.add(b), await il(), i !== (i = t())) {
      var l = e.selectionStart, a = e.selectionEnd, o = e.value.length;
      if (e.value = i ?? "", a !== null) {
        var u = e.value.length;
        l === a && a === o && u > o ? (e.selectionStart = u, e.selectionEnd = u) : (e.selectionStart = l, e.selectionEnd = Math.min(a, u));
      }
    }
  }), // If we are hydrating and the value has since changed,
  // then use the updated value from the input instead.
  (R && e.defaultValue !== e.value || // If defaultValue is set, then value == defaultValue
  // TODO Svelte 6: remove input.value check and set to empty string?
  Hs(t) == null && e.value) && (r(Kr(e) ? Jr(e.value) : e.value), b !== null && n.add(b)), An(() => {
    var s = t();
    if (e === document.activeElement) {
      var i = (
        /** @type {Batch} */
        b
      );
      if (n.has(i))
        return;
    }
    Kr(e) && s === Jr(e.value) || e.type === "date" && !s && !e.value || s !== e.value && (e.value = s ?? "");
  });
}
function Kr(e) {
  var t = e.type;
  return t === "number" || t === "range";
}
function Jr(e) {
  return e === "" ? null : +e;
}
function ee(e, t, r, n) {
  var s = (
    /** @type {V} */
    n
  ), i = !0, l = () => (i && (i = !1, s = /** @type {V} */
  n), s), a;
  a = /** @type {V} */
  e[t], a === void 0 && n !== void 0 && (a = l());
  var o;
  o = () => {
    var p = (
      /** @type {V} */
      e[t]
    );
    return p === void 0 ? l() : (i = !0, p);
  };
  var u = !1, h = /* @__PURE__ */ jr(() => (u = !1, o())), v = (
    /** @type {Effect} */
    T
  );
  return (
    /** @type {() => V} */
    (function(p, d) {
      if (arguments.length > 0) {
        const c = d ? $(h) : p;
        return P(h, c), u = !0, s !== void 0 && (s = c), p;
      }
      return Xe && u || (v.f & z) !== 0 ? h.v : $(h);
    })
  );
}
function Rl(e) {
  return new Nl(e);
}
var Ve, ve;
class Nl {
  /**
   * @param {ComponentConstructorOptions & {
   *  component: any;
   * }} options
   */
  constructor(t) {
    /** @type {any} */
    E(this, Ve);
    /** @type {Record<string, any>} */
    E(this, ve);
    var i;
    var r = /* @__PURE__ */ new Map(), n = (l, a) => {
      var o = /* @__PURE__ */ $s(a, !1, !1);
      return r.set(l, o), o;
    };
    const s = new Proxy(
      { ...t.props || {}, $$events: {} },
      {
        get(l, a) {
          return $(r.get(a) ?? n(a, Reflect.get(l, a)));
        },
        has(l, a) {
          return a === pi ? !0 : ($(r.get(a) ?? n(a, Reflect.get(l, a))), Reflect.has(l, a));
        },
        set(l, a, o) {
          return P(r.get(a) ?? n(a, o), o), Reflect.set(l, a, o);
        }
      }
    );
    w(this, ve, (t.hydrate ? _l : Ws)(t.component, {
      target: t.target,
      anchor: t.anchor,
      props: s,
      context: t.context,
      intro: t.intro ?? !1,
      recover: t.recover,
      transformError: t.transformError
    })), (!((i = t == null ? void 0 : t.props) != null && i.$$host) || t.sync === !1) && Y(), w(this, Ve, s.$$events);
    for (const l of Object.keys(f(this, ve)))
      l === "$set" || l === "$destroy" || l === "$on" || nr(this, l, {
        get() {
          return f(this, ve)[l];
        },
        /** @param {any} value */
        set(a) {
          f(this, ve)[l] = a;
        },
        enumerable: !0
      });
    f(this, ve).$set = /** @param {Record<string, any>} next */
    (l) => {
      Object.assign(s, l);
    }, f(this, ve).$destroy = () => {
      gl(f(this, ve));
    };
  }
  /** @param {Record<string, any>} props */
  $set(t) {
    f(this, ve).$set(t);
  }
  /**
   * @param {string} event
   * @param {(...args: any[]) => any} callback
   * @returns {any}
   */
  $on(t, r) {
    f(this, Ve)[t] = f(this, Ve)[t] || [];
    const n = (...s) => r.call(this, ...s);
    return f(this, Ve)[t].push(n), () => {
      f(this, Ve)[t] = f(this, Ve)[t].filter(
        /** @param {any} fn */
        (s) => s !== n
      );
    };
  }
  $destroy() {
    f(this, ve).$destroy();
  }
}
Ve = new WeakMap(), ve = new WeakMap();
let Xs;
typeof HTMLElement == "function" && (Xs = class extends HTMLElement {
  /**
   * @param {*} $$componentCtor
   * @param {*} $$slots
   * @param {ShadowRootInit | undefined} shadow_root_init
   */
  constructor(t, r, n) {
    super();
    /** The Svelte component constructor */
    C(this, "$$ctor");
    /** Slots */
    C(this, "$$s");
    /** @type {any} The Svelte component instance */
    C(this, "$$c");
    /** Whether or not the custom element is connected */
    C(this, "$$cn", !1);
    /** @type {Record<string, any>} Component props data */
    C(this, "$$d", {});
    /** `true` if currently in the process of reflecting component props back to attributes */
    C(this, "$$r", !1);
    /** @type {Record<string, CustomElementPropDefinition>} Props definition (name, reflected, type etc) */
    C(this, "$$p_d", {});
    /** @type {Record<string, EventListenerOrEventListenerObject[]>} Event listeners */
    C(this, "$$l", {});
    /** @type {Map<EventListenerOrEventListenerObject, Function>} Event listener unsubscribe functions */
    C(this, "$$l_u", /* @__PURE__ */ new Map());
    /** @type {any} The managed render effect for reflecting attributes */
    C(this, "$$me");
    /** @type {ShadowRoot | null} The ShadowRoot of the custom element */
    C(this, "$$shadowRoot", null);
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
          const a = Sn("slot");
          i !== "default" && (a.name = i), V(l, a);
        };
      };
      var t = r;
      if (await Promise.resolve(), !this.$$cn || this.$$c)
        return;
      const n = {}, s = Cl(this);
      for (const i of this.$$s)
        i in s && (i === "default" && !this.$$d.children ? (this.$$d.children = r(i), n.default = !0) : n[i] = r(i));
      for (const i of this.attributes) {
        const l = this.$$g_p(i.name);
        l in this.$$d || (this.$$d[l] = $r(l, i.value, this.$$p_d, "toProp"));
      }
      for (const i in this.$$p_d)
        !(i in this.$$d) && this[i] !== void 0 && (this.$$d[i] = this[i], delete this[i]);
      this.$$c = Rl({
        component: this.$$ctor,
        target: this.$$shadowRoot || this,
        props: {
          ...this.$$d,
          $$slots: n,
          $$host: this
        }
      }), this.$$me = Xi(() => {
        An(() => {
          var i;
          this.$$r = !0;
          for (const l of Er(this.$$c)) {
            if (!((i = this.$$p_d[l]) != null && i.reflect)) continue;
            this.$$d[l] = this.$$c[l];
            const a = $r(
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
    this.$$r || (t = this.$$g_p(t), this.$$d[t] = $r(t, n, this.$$p_d, "toProp"), (s = this.$$c) == null || s.$set({ [t]: this.$$d[t] }));
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
    return Er(this.$$p_d).find(
      (r) => this.$$p_d[r].attribute === t || !this.$$p_d[r].attribute && r.toLowerCase() === t
    ) || t;
  }
});
function $r(e, t, r, n) {
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
function Cl(e) {
  const t = {};
  return e.childNodes.forEach((r) => {
    t[
      /** @type {Element} node */
      r.slot || "default"
    ] = !0;
  }), t;
}
function xt(e, t, r, n, s, i) {
  let l = class extends Xs {
    constructor() {
      super(e, r, s), this.$$p_d = t;
    }
    static get observedAttributes() {
      return Er(t).map(
        (a) => (t[a].attribute || a).toLowerCase()
      );
    }
  };
  return Er(t).forEach((a) => {
    nr(l.prototype, a, {
      get() {
        return this.$$c && a in this.$$c ? this.$$c[a] : this.$$d[a];
      },
      set(o) {
        var v;
        o = $r(a, o, t), this.$$d[a] = o;
        var u = this.$$c;
        if (u) {
          var h = (v = Ot(u, a)) == null ? void 0 : v.get;
          h ? u[a] = o : u.$set({ [a]: o });
        }
      }
    });
  }), n.forEach((a) => {
    nr(l.prototype, a, {
      get() {
        var o;
        return (o = this.$$c) == null ? void 0 : o[a];
      }
    });
  }), e.element = /** @type {any} */
  l, l;
}
function Cn(e, t, r) {
  return e.dispatchEvent(new CustomEvent(t, { detail: r, bubbles: !0, composed: !0 }));
}
var Ml = /* @__PURE__ */ oe('<p class="error svelte-1rq3t8b" role="alert"> </p>'), Ll = /* @__PURE__ */ oe('<div class="row svelte-1rq3t8b"><input type="color" class="picker svelte-1rq3t8b"/> <input type="text" class="hex svelte-1rq3t8b" placeholder="#rrggbb"/></div> <!>', 1);
const Pl = {
  hash: "svelte-1rq3t8b",
  code: ".row.svelte-1rq3t8b {display:flex;gap:var(--h-space-2);align-items:center;}.picker.svelte-1rq3t8b {inline-size:2.5rem;block-size:2.25rem;padding:0;}.hex.svelte-1rq3t8b {max-inline-size:10rem;font-family:var(--h-mono);}.hex[aria-invalid='true'].svelte-1rq3t8b {border-color:var(--h-danger);}.error.svelte-1rq3t8b {margin:var(--h-space-1) 0 0;color:var(--h-danger);font-size:var(--h-text-sm);}input.svelte-1rq3t8b {font:inherit;padding:var(--h-space-2) var(--h-space-3);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);}"
};
function Dl(e, t) {
  at(t, !0), Et(e, Pl);
  let r = ee(t, "ctx"), n = ee(t, "value", 7, ""), s = ee(t, "readonly", 7, !1), i = ee(t, "error", 7, "");
  const l = t.$$host, a = /* @__PURE__ */ Tt(() => /^#[0-9a-fA-F]{6}$/.test(n() ?? "") ? n() : "#000000"), o = (_) => Cn(l, "change", { value: _ });
  var u = {
    get ctx() {
      return r();
    },
    set ctx(_) {
      r(_), Y();
    },
    get value() {
      return n();
    },
    set value(_ = "") {
      n(_), Y();
    },
    get readonly() {
      return s();
    },
    set readonly(_ = !1) {
      s(_), Y();
    },
    get error() {
      return i();
    },
    set error(_ = "") {
      i(_), Y();
    }
  }, h = Ll(), v = En(h), p = We(v);
  vn(p);
  var d = Z(p, 2);
  vn(d), Ne(v);
  var c = Z(v, 2);
  {
    var g = (_) => {
      var m = Ml(), x = Me(m, !0);
      Le(() => De(x, i())), V(_, m);
    };
    lr(c, (_) => {
      i() && _(g);
    });
  }
  return Le(
    (_, m) => {
      st(p, "aria-label", _), Yn(p, $(a)), p.disabled = s(), st(d, "aria-label", m), Yn(d, n() ?? ""), d.readOnly = s(), st(d, "aria-invalid", i() ? "true" : void 0);
    },
    [() => r().t("brand.color"), () => r().t("brand.hex")]
  ), ir("input", p, (_) => o(_.currentTarget.value)), ir("input", d, (_) => o(_.currentTarget.value)), V(e, h), ft(u);
}
qr(["input"]);
customElements.define("x-brand-color", xt(
  Dl,
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
var Il = /* @__PURE__ */ oe('<span class="swatch svelte-134mtg6"><i class="chip svelte-134mtg6"></i> <code> </code></span>');
const Fl = {
  hash: "svelte-134mtg6",
  code: ".swatch.svelte-134mtg6 {display:inline-flex;gap:var(--h-space-2);align-items:center;}.chip.svelte-134mtg6 {display:inline-block;inline-size:1rem;block-size:1rem;border-radius:50%;border:1px solid var(--h-border-strong);}"
};
function jl(e, t) {
  at(t, !0), Et(e, Fl);
  let r = ee(t, "value", 7, "");
  var n = {
    get value() {
      return r();
    },
    set value(a = "") {
      r(a), Y();
    }
  }, s = Vs(), i = En(s);
  {
    var l = (a) => {
      var o = Il(), u = We(o);
      let h;
      var v = Z(u, 2), p = Me(v, !0);
      Ne(o), Le(() => {
        st(o, "data-swatch", r()), h = xl(u, "", h, { "background-color": r() }), De(p, r());
      }), V(a, o);
    };
    lr(i, (a) => {
      r() && a(l);
    });
  }
  return V(e, s), ft(n);
}
customElements.define("x-color-swatch", xt(jl, { ctx: { type: "Object" }, value: { type: "String" } }, [], [], { mode: "open" }));
var ql = /* @__PURE__ */ oe('<section class="svelte-1cnsmp2"><p class="svelte-1cnsmp2">SEO score: <strong data-testid="seo-score"> </strong> / 100</p> <p class="svelte-1cnsmp2"> </p> <p data-testid="seo-state" class="svelte-1cnsmp2"> </p> <button type="button" class="svelte-1cnsmp2">Refresh</button></section>');
const zl = {
  hash: "svelte-1cnsmp2",
  code: "section.svelte-1cnsmp2 {display:grid;gap:var(--h-space-2);font-size:var(--h-text-sm);}p.svelte-1cnsmp2 {margin:0;}button.svelte-1cnsmp2 {font:inherit;padding:var(--h-space-2) var(--h-space-4);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);cursor:pointer;}button.svelte-1cnsmp2:hover {background:var(--h-surface-hover);}"
};
function Hl(e, t) {
  at(t, !0), Et(e, zl);
  let r = ee(t, "ctx"), n = ee(t, "item"), s = ee(t, "locale", 7, "en");
  const i = t.$$host, l = (O) => typeof O == "string" ? O : "", a = /* @__PURE__ */ Tt(() => {
    var O;
    return l((O = n()) == null ? void 0 : O.fields.title);
  }), o = /* @__PURE__ */ Tt(() => {
    var O, I;
    return l(((O = n()) == null ? void 0 : O.fields.summary) ?? ((I = n()) == null ? void 0 : I.fields.intro));
  }), u = /* @__PURE__ */ Tt(() => ($(a).length >= 30 && $(a).length <= 60 ? 50 : $(a).length > 0 ? 25 : 0) + ($(o).length >= 80 && $(o).length <= 160 ? 50 : $(o).length > 0 ? 25 : 0));
  let h = /* @__PURE__ */ B("loading"), v = /* @__PURE__ */ B(0);
  Os(() => {
    var ce, _r, gr;
    $(v), (ce = n()) == null || ce.updatedAt;
    const O = (_r = n()) == null ? void 0 : _r.id;
    if (!O) return;
    const I = JSON.stringify(((gr = n()) == null ? void 0 : gr.fields) ?? {});
    let ke = !0;
    return P(h, "loading"), r().api.get("/items/" + O + "?locale=" + encodeURIComponent(s())).then((Zs) => ke && P(h, JSON.stringify(Zs.fields) === I ? "same" : "differs", !0)).catch(() => ke && P(h, "unpublished")), () => ke = !1;
  });
  const p = {
    loading: "…",
    same: "published",
    differs: "draft differs",
    unpublished: "not published"
  }, d = /* @__PURE__ */ Tt(() => p[$(h)]);
  var c = {
    get ctx() {
      return r();
    },
    set ctx(O) {
      r(O), Y();
    },
    get item() {
      return n();
    },
    set item(O) {
      n(O), Y();
    },
    get locale() {
      return s();
    },
    set locale(O = "en") {
      s(O), Y();
    }
  }, g = ql(), _ = We(g), m = Z(We(_)), x = Me(m, !0);
  is(), Ne(_);
  var D = Z(_, 2), qe = Me(D), G = Z(D, 2), ue = Me(G, !0), W = Z(G, 2);
  return Ne(g), Le(() => {
    De(x, $(u)), De(qe, `Title ${$(a).length ?? ""} characters (30 to 60), summary ${$(o).length ?? ""} (80 to 160).`), De(ue, $(d));
  }), ir("click", W, () => {
    Gi(v), Cn(i, "refresh");
  }), V(e, g), ft(c);
}
qr(["click"]);
customElements.define("x-seo-panel", xt(
  Hl,
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
var Bl = /* @__PURE__ */ oe('<button type="button" class="svelte-10702mj">Send to translation</button>');
const Vl = {
  hash: "svelte-10702mj",
  code: "button.svelte-10702mj {font:inherit;padding:var(--h-space-2) var(--h-space-4);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);cursor:pointer;}button.svelte-10702mj:hover {background:var(--h-surface-hover);}"
};
function Yl(e, t) {
  at(t, !0), Et(e, Vl);
  let r = ee(t, "ctx"), n = ee(t, "item");
  const s = t.$$host;
  function i() {
    var o;
    r().toast("success", `Sent "${((o = n()) == null ? void 0 : o.name) ?? "item"}" to translation`), Cn(s, "refresh");
  }
  var l = {
    get ctx() {
      return r();
    },
    set ctx(o) {
      r(o), Y();
    },
    get item() {
      return n();
    },
    set item(o) {
      n(o), Y();
    }
  }, a = Bl();
  return ir("click", a, i), V(e, a), ft(l);
}
qr(["click"]);
customElements.define("x-translate-action", xt(
  Yl,
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
var Ul = /* @__PURE__ */ oe("<p>The audit log is not available to your account.</p>"), Gl = /* @__PURE__ */ oe('<a class="svelte-mtmgy0"> </a>'), Wl = /* @__PURE__ */ oe("<span> </span>"), Kl = /* @__PURE__ */ oe('<li><!> <time class="svelte-mtmgy0"> </time></li>'), Jl = /* @__PURE__ */ oe('<ul class="svelte-mtmgy0"></ul>');
const Xl = {
  hash: "svelte-mtmgy0",
  code: "ul.svelte-mtmgy0 {margin:0;padding:0;list-style:none;display:grid;gap:var(--h-space-2);}a.svelte-mtmgy0 {color:var(--h-accent-strong);}time.svelte-mtmgy0 {margin-inline-start:var(--h-space-2);color:var(--h-text-muted);font-size:var(--h-text-xs);}"
};
function Zl(e, t) {
  at(t, !0), Et(e, Xl);
  let r = ee(t, "ctx"), n = /* @__PURE__ */ B(At([])), s = /* @__PURE__ */ B(!1);
  Os(() => {
    let p = !0;
    return r().api.get("/audit?limit=5").then((d) => p && P(n, d.items, !0)).catch(() => p && P(s, !0)), () => p = !1;
  });
  const i = (p) => !!p && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(p);
  function l(p, d) {
    p.preventDefault(), r().navigate("/items/" + d);
  }
  var a = {
    get ctx() {
      return r();
    },
    set ctx(p) {
      r(p), Y();
    }
  }, o = Vs(), u = En(o);
  {
    var h = (p) => {
      var d = Ul();
      V(p, d);
    }, v = (p) => {
      var d = Jl();
      wl(d, 21, () => $(n), (c) => c.id, (c, g) => {
        var _ = Kl(), m = We(_);
        {
          var x = (W) => {
            var O = Gl(), I = Me(O, !0);
            Le(() => {
              st(O, "href", r().prefix + "/items/" + $(g).subject), De(I, $(g).action);
            }), ir("click", O, (ke) => l(ke, $(g).subject)), V(W, O);
          }, D = /* @__PURE__ */ Tt(() => i($(g).subject)), qe = (W) => {
            var O = Wl(), I = Me(O, !0);
            Le(() => De(I, $(g).action)), V(W, O);
          };
          lr(m, (W) => {
            $(D) ? W(x) : W(qe, -1);
          });
        }
        var G = Z(m, 2), ue = Me(G, !0);
        Ne(_), Le(
          (W) => {
            st(G, "datetime", $(g).at), De(ue, W);
          },
          [() => new Date($(g).at).toLocaleString()]
        ), V(c, _);
      }), Ne(d), V(p, d);
    };
    lr(u, (p) => {
      $(s) ? p(h) : p(v, -1);
    });
  }
  return V(e, o), ft(a);
}
qr(["click"]);
customElements.define("x-recent-widget", xt(Zl, { ctx: { type: "Object" } }, [], [], { mode: "open" }));
var Ql = /* @__PURE__ */ oe('<p class="error svelte-rm2e3w" role="alert"> </p>'), ea = /* @__PURE__ */ oe('<form class="svelte-rm2e3w"><h2 class="svelte-rm2e3w">Import an article</h2> <label class="svelte-rm2e3w">Title <input name="title" required="" class="svelte-rm2e3w"/></label> <label class="svelte-rm2e3w">Summary <textarea name="summary" class="svelte-rm2e3w"></textarea></label> <!> <button type="submit" class="primary svelte-rm2e3w">Import</button></form>');
const ta = {
  hash: "svelte-rm2e3w",
  code: "form.svelte-rm2e3w {display:grid;gap:var(--h-space-3);max-inline-size:32rem;}label.svelte-rm2e3w {display:grid;gap:var(--h-space-1);font-weight:500;}h2.svelte-rm2e3w {margin:0;font-size:var(--h-text-lg);}.error.svelte-rm2e3w {margin:0;color:var(--h-danger);}button.svelte-rm2e3w {font:inherit;padding:var(--h-space-2) var(--h-space-4);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);cursor:pointer;}button.svelte-rm2e3w:hover {background:var(--h-surface-hover);}button.primary.svelte-rm2e3w {background:var(--h-accent);border-color:var(--h-accent);color:var(--h-accent-contrast);}input.svelte-rm2e3w, textarea.svelte-rm2e3w {font:inherit;padding:var(--h-space-2) var(--h-space-3);border:1px solid var(--h-border-strong);border-radius:var(--h-radius);background:var(--h-surface);color:var(--h-text);}"
};
function ra(e, t) {
  at(t, !0), Et(e, ta);
  let r = ee(t, "ctx"), n = /* @__PURE__ */ B(""), s = /* @__PURE__ */ B(""), i = /* @__PURE__ */ B(!1), l = /* @__PURE__ */ B("");
  async function a(m) {
    m.preventDefault(), P(i, !0), P(l, "");
    try {
      const x = await r().api.post("/collections/article/items", {
        locale: "en",
        fields: { title: $(n), summary: $(s) }
      });
      r().toast("success", "Article imported"), r().navigate("/items/" + x.id);
    } catch (x) {
      P(l, x.message ?? "Import failed", !0);
    } finally {
      P(i, !1);
    }
  }
  var o = {
    get ctx() {
      return r();
    },
    set ctx(m) {
      r(m), Y();
    }
  }, u = ea(), h = Z(We(u), 2), v = Z(We(h));
  vn(v), Ne(h);
  var p = Z(h, 2), d = Z(We(p));
  Pi(d), Ne(p);
  var c = Z(p, 2);
  {
    var g = (m) => {
      var x = Ql(), D = Me(x, !0);
      Le(() => De(D, $(l))), V(m, x);
    };
    lr(c, (m) => {
      $(l) && m(g);
    });
  }
  var _ = Z(c, 2);
  return Ne(u), Le(() => _.disabled = $(i)), al("submit", u, a), Gn(v, () => $(n), (m) => P(n, m)), Gn(d, () => $(s), (m) => P(s, m)), V(e, u), ft(o);
}
customElements.define("x-import-tool", xt(ra, { ctx: { type: "Object" } }, [], [], { mode: "open" }));
var na = /* @__PURE__ */ oe('<p class="svelte-s1xavp"> </p>');
const sa = {
  hash: "svelte-s1xavp",
  code: "p.svelte-s1xavp {margin:0 0 var(--h-space-3);padding:var(--h-space-2) var(--h-space-3);border-radius:var(--h-radius);background:var(--h-accent-soft);color:var(--h-accent-strong);}"
};
function ia(e, t) {
  at(t, !0), Et(e, sa);
  let r = ee(t, "ctx"), n = ee(t, "type", 7, "");
  var s = {
    get ctx() {
      return r();
    },
    set ctx(a) {
      r(a), Y();
    },
    get type() {
      return n();
    },
    set type(a = "") {
      n(a), Y();
    }
  }, i = na(), l = Me(i);
  return Le(() => De(l, `Editing ${n() ?? ""} as ${r().principal.name ?? ""}`)), V(e, i), ft(s);
}
customElements.define("x-banner", xt(
  ia,
  {
    ctx: { type: "Object" },
    type: { type: "String" },
    at: { type: "String" }
  },
  [],
  [],
  { mode: "open" }
));
