(function(){"use strict";const SDK = '@hydrogencms/bridge@0.2.0';
const PREVIEW_COOKIE = 'hydrogen-preview';
class PatchError extends Error {
    name = 'PatchError';
}
const isArr = Array.isArray;
const isObj = (v) => typeof v === 'object' && v !== null && !isArr(v);
function pointer(path) {
    if (path === '')
        return [];
    if (path[0] !== '/')
        throw new PatchError(`invalid pointer "${path}"`);
    return path.slice(1).split('/').map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~'));
}
function apply(node, t, i, o) {
    const k = t[i];
    const arr = isArr(node);
    if (!arr && !isObj(node))
        throw new PatchError(`${o.path}: parent is not a container`);
    const last = i === t.length - 1;
    const add = last && o.op === 'add';
    const dash = add && k === '-';
    let at = k;
    if (arr) {
        at = dash ? node.length : +k;
        if (!dash && (!/^(0|[1-9]\d*)$/.test(k) || at > node.length - (add ? 0 : 1)))
            throw new PatchError(`${o.path}: bad array index "${k}"`);
    }
    else if (!add && !Object.hasOwn(node, k))
        throw new PatchError(`${o.path}: no such path "${k}"`);
    const c = arr ? node.slice() : { ...node };
    if (!last)
        c[at] = apply(c[at], t, i + 1, o);
    else if (o.op === 'remove')
        arr ? c.splice(at, 1) : delete c[k];
    else if (add && arr)
        c.splice(at, 0, o.value);
    else
        c[at] = o.value;
    return c;
}
function applyPatch(doc, ops) {
    let d = doc;
    ops.forEach((raw, n) => {
        const o = raw;
        try {
            if (!isObj(raw) || (o.op !== 'add' && o.op !== 'remove' && o.op !== 'replace'))
                throw new PatchError(`unsupported op "${raw?.op}"`);
            if (o.op !== 'remove' && !('value' in raw))
                throw new PatchError(`${raw.op} needs a value`);
            const t = pointer(o.path);
            if (t.includes('__proto__'))
                throw new PatchError(`${o.path}: forbidden key`);
            if (!t.length) {
                if (o.op === 'remove')
                    throw new PatchError('cannot remove root');
                d = o.value;
            }
            else
                d = apply(d, t, 0, o);
        }
        catch (e) {
            if (e instanceof PatchError)
                e.message = `ops[${n}] ${o?.op} ${o?.path}: ${e.message}`;
            throw e;
        }
    });
    return d;
}
function isPreview(win = window) {
    try {
        if (new URLSearchParams(win.location.search).get('preview'))
            return true;
        if (win.document.cookie.split(/;\s*/).some((c) => c.startsWith(PREVIEW_COOKIE + '=')))
            return true;
        return !!win.document.querySelector('meta[name="hydrogen-preview"]');
    }
    catch {
        return false;
    }
}
const CAP = { capture: true };
const HASH = /^[0-9a-f]{64}$/i;
function previewProof(win = window) {
    try {
        const c = win.document.querySelector('meta[name="hydrogen-preview"]')?.getAttribute('content') ?? '';
        return HASH.test(c) ? c : undefined;
    }
    catch {
        return undefined;
    }
}
const norm = (s) => s.replace(/\s+/g, ' ').trim();
const SKIP = /^(script|style|noscript|template)$/i;
function canvas(w, post, raf) {
    const d = w.document;
    let on = false;
    let texts = [];
    let map = new Map();
    let pend = false;
    let last = '';
    let hover = null;
    let ab = new AbortController();
    let mo;
    let ro;
    let ed = null;
    const collect = () => {
        if (ed && !ed.el.isConnected)
            end(false);
        const els = Array.from(d.body.querySelectorAll('[data-h-path]'), (e) => [e, e.getAttribute('data-h-path')]);
        const n = els.length;
        const lists = new Map();
        const used = new Set(els.map((x) => x[1]));
        for (const t of texts) {
            if (used.has(t.path))
                continue;
            let l = lists.get(t.scope);
            if (!l) {
                lists.set(t.scope, (l = []));
                const sc = t.scope ? d.querySelector(`[data-h-path="${t.scope.replace(/["\\]/g, '\\$&')}"]`) : d.querySelector('[data-h-root]') ?? d.body;
                const walk = (p) => p.childNodes.forEach((c) => {
                    if (c.nodeType === 3) {
                        const e = c.parentElement;
                        if (e.closest('[data-h-path]') === (t.scope ? sc : null))
                            l.push([norm(c.data), e]);
                    }
                    else if (c.nodeType === 1 && !SKIP.test(c.nodeName))
                        walk(c);
                });
                if (sc)
                    walk(sc);
            }
            const want = norm(t.text);
            const hit = l.find(([s, e]) => s === want && !used.has(e));
            if (hit && !hit[1].hasAttribute('data-h-path')) {
                used.add(hit[1]);
                els.push([hit[1], t.path]);
            }
        }
        if (els.length > n)
            els.sort((a, b) => (a[0] === b[0] ? 0 : a[0].compareDocumentPosition(b[0]) & 4 ? -1 : 1));
        map = new Map(els);
        return els;
    };
    const layout = () => {
        const nodes = [];
        for (const [e, path] of collect()) {
            const r = e.getBoundingClientRect();
            const rect = [r.left, r.top, r.width, r.height].map(Math.round);
            const block = e.getAttribute('data-h-block');
            const c = e.firstChild;
            if (rect[2] && rect[3])
                nodes.push({ path, rect, ...(block ? { block } : {}), ...(e.childNodes.length === 1 && c?.nodeType === 3 && c.data ? { text: true } : {}) });
        }
        return nodes;
    };
    const sched = () => {
        if (pend)
            return;
        pend = true;
        raf(() => {
            pend = false;
            if (!on)
                return;
            const m = { type: 'layout', nodes: layout(), vw: w.innerWidth, vh: w.innerHeight };
            const j = JSON.stringify(m);
            if (j !== last)
                (last = j), post(m);
        });
    };
    const find = (path) => (collect(), [...map].find(([, p]) => p === path)?.[0]);
    const nodeOf = (t) => {
        for (let e = t; e; e = e.parentElement) {
            const p = map.get(e);
            const r = p !== undefined && e.getBoundingClientRect();
            if (r && r.width && r.height)
                return p;
        }
        return null;
    };
    const hov = (p) => p !== hover && post({ type: 'hover', path: (hover = p) });
    const stop = (e) => {
        if (ed?.el.contains(e.target))
            return;
        e.preventDefault();
        e.stopImmediatePropagation();
        if (e.type !== 'auxclick' && e.type !== 'submit')
            post({ type: 'select', path: (collect(), nodeOf(e.target)), edit: e.type === 'dblclick' });
    };
    const end = (commit) => {
        const e = ed;
        if (!e)
            return;
        ed = null;
        const t = (e.multi ? e.el.innerText : null) ?? e.el.textContent ?? '';
        const v = commit ? (e.multi ? t : norm(t)) : e.orig;
        e.ac.abort();
        e.el.removeAttribute('contenteditable');
        e.el.replaceChildren(...e.kids);
        e.text.data = v;
        post({ type: 'inlineEdit', path: e.path, ...(commit ? { value: v } : { cancel: true }) });
    };
    const off = () => {
        end(false);
        if (!on)
            return;
        on = false;
        ab.abort();
        mo?.disconnect();
        ro?.disconnect();
        hover = null;
        map = new Map();
    };
    const on_ = () => {
        on = true;
        ab = new AbortController();
        const signal = ab.signal;
        const on2 = (t, type, f, o) => t.addEventListener(type, f, { ...o, signal });
        for (const t of ['click', 'dblclick', 'auxclick', 'submit'])
            on2(w, t, stop, CAP);
        on2(w, 'pointermove', (e) => hov(nodeOf(e.target)), { capture: true, passive: true });
        on2(d.documentElement, 'pointerleave', () => hov(null));
        on2(w, 'scroll', sched, { capture: true, passive: true });
        on2(w, 'resize', sched);
        mo = w.MutationObserver && new w.MutationObserver(sched);
        mo?.observe(d.body, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['data-h-path', 'data-h-block', 'class', 'style', 'hidden'] });
        ro = w.ResizeObserver && new w.ResizeObserver(sched);
        ro?.observe(d.documentElement);
    };
    return {
        sched: () => on && ((last = ''), sched()),
        off,
        overlay(m) {
            texts = (isArr(m.texts) ? m.texts : []).filter((t) => isObj(t) && ['path', 'text', 'scope'].every((k) => typeof t[k] === 'string'));
            if (m.on !== true)
                return off();
            if (!on)
                on_();
            last = '';
            sched();
        },
        scroll(m) {
            if (typeof m.by === 'number')
                return w.scrollBy({ top: m.by, behavior: 'instant' });
            find(m.path)?.scrollIntoView({ block: 'nearest', behavior: w.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' });
        },
        edit(path, multi) {
            end(true);
            const el = find(path);
            const text = el?.firstChild;
            if (!el || el.childNodes.length !== 1 || text?.nodeType !== 3)
                return post({ type: 'inlineEdit', path, cancel: true });
            el.setAttribute('contenteditable', 'plaintext-only');
            const plain = el.contentEditable === 'plaintext-only';
            if (!plain)
                el.setAttribute('contenteditable', 'true');
            const ac = new AbortController();
            const h = (e) => {
                if (e.type === 'blur')
                    end(true);
                else if (e.type === 'paste')
                    (e.preventDefault(), d.execCommand('insertText', false, e.clipboardData?.getData('text/plain') ?? ''));
                else if (e.isComposing)
                    return;
                else if (e.key === 'Escape')
                    (e.preventDefault(), e.stopPropagation(), end(false));
                else if (e.key === 'Enter' && (!multi || e.metaKey || e.ctrlKey))
                    (e.preventDefault(), e.stopPropagation(), end(true));
            };
            for (const t of plain ? ['keydown', 'blur'] : ['keydown', 'blur', 'paste'])
                el.addEventListener(t, h, { signal: ac.signal });
            ed = { el, path, multi, kids: Array.from(el.childNodes), text: text, orig: text.data, ac };
            try {
                const s = w.getSelection();
                const k = el.contains(s.anchorNode);
                el.focus();
                k || (s.selectAllChildren(el), s.collapseToEnd());
            }
            catch {
            }
        },
    };
}
function createBridge(o) {
    const w = o.window ?? window;
    const origin = o.adminOrigin;
    if (!/^https?:\/\/[^/*]+$/.test(origin))
        throw new Error(`bridge: bad adminOrigin "${origin}"`);
    let rev = -1;
    let doc;
    let t0 = 0;
    let on = false;
    const now = () => (w.performance ?? Date).now();
    const post = (m) => w.parent.postMessage({ h: 1, ...m }, origin);
    const ready = () => {
        const preview = previewProof(w);
        post({ type: 'ready', mode: o.mode, edit: true, sdk: SDK, url: w.location.href, ...(preview ? { preview } : {}) });
    };
    const reload = () => w.location.reload();
    const follow = (e) => {
        try {
            const a = e.target?.closest?.('a[href]');
            const token = new URLSearchParams(w.location.search).get('preview');
            if (!a || !token)
                return;
            const u = new URL(a.href);
            if (u.origin !== w.location.origin || u.searchParams.has('preview'))
                return;
            u.searchParams.set('preview', token);
            a.href = u.href;
        }
        catch {
        }
    };
    const raf = (f) => (w.requestAnimationFrame ? w.requestAnimationFrame(f) : setTimeout(f, 0));
    const cv = canvas(w, post, raf);
    const resync = () => post({ type: 'resync', rev });
    const error = (message) => post({ type: 'error', message });
    const onMessage = (e) => {
        const m = e.data;
        if (e.origin !== origin || e.source !== w.parent || !isObj(m) || m.h !== 1)
            return;
        t0 = now();
        try {
            if (m.type === 'init') {
                doc = m.doc;
                rev = m.rev;
                o.onInit?.(m);
            }
            else if (m.type === 'patch') {
                if (m.base !== rev)
                    return resync();
                try {
                    doc = applyPatch(doc, m.ops);
                }
                catch (x) {
                    error(x.message);
                    return resync();
                }
                rev = m.rev;
                o.onPatch?.(doc, m);
            }
            else if (m.type === 'overlay')
                cv.overlay(m);
            else if (m.type === 'scrollTo')
                cv.scroll(m);
            else if (m.type === 'editText')
                typeof m.path === 'string' && cv.edit(m.path, m.multiline === true);
            else if (m.type === 'reload') {
                const r = m.rev;
                if (o.onReload)
                    void o.onReload(r);
                else if (w.origin === 'null')
                    reload();
                else {
                    reloadSwap({ window: w, ready: () => (ready(), cv.sched()) }).then((ms) => bridge.rendered(r, ms), (x) => (x?.fetch ? reload() : error(String(x?.message ?? x))));
                }
            }
        }
        catch (x) {
            error(String(x?.message ?? x));
        }
    };
    const bridge = {
        connect() {
            if (on || w.parent === w || !(o.preview ?? isPreview(w)))
                return;
            on = true;
            w.addEventListener('message', onMessage);
            for (const t of ['click', 'auxclick'])
                w.addEventListener(t, follow, CAP);
            ready();
        },
        disconnect() {
            if (on) {
                w.removeEventListener('message', onMessage);
                for (const t of ['click', 'auxclick'])
                    w.removeEventListener(t, follow, CAP);
                cv.off();
            }
            on = false;
        },
        rendered(r, ms) {
            if (!on)
                return;
            raf(() => raf(() => (post({ type: 'rendered', rev: r, ms: ms ?? Math.max(0, Math.round(now() - t0)) }), cv.sched())));
        },
        error,
    };
    return bridge;
}
async function reloadSwap(o = {}) {
    const w = o.window ?? window;
    const t = w.performance.now();
    const res = await (o.fetch ?? w.fetch.bind(w))(w.location.href, { credentials: 'include', headers: { accept: 'text/html' } }).catch((e) => {
        throw Object.assign(new Error(`reload: ${e?.message ?? e}`), { fetch: true });
    });
    if (!res.ok)
        throw new Error(`reload: ${res.status} ${res.statusText}`);
    const gone = new Error('reload: preview expired or lost');
    if (res.redirected)
        throw gone;
    const next = new w.DOMParser().parseFromString(await res.text(), 'text/html');
    const d = w.document;
    const sel = o.root ?? '[data-h-root]';
    const [x, y] = [w.scrollX, w.scrollY];
    const cur = d.querySelector(sel);
    const nxt = next.querySelector(sel);
    if (cur && !nxt)
        throw gone;
    if (cur && nxt)
        cur.replaceWith(d.importNode(nxt, true));
    else
        d.body.replaceChildren(...Array.from(next.body.childNodes, (n) => d.importNode(n, true)));
    if (next.title)
        d.title = next.title;
    w.scrollTo(x, y);
    o.ready?.();
    return Math.round(w.performance.now() - t);
}
function autoStart(s, win) {
    if (!s || s.dataset.auto === undefined || !s.dataset.adminOrigin)
        return;
    const b = createBridge({ adminOrigin: s.dataset.adminOrigin, mode: s.dataset.mode === 'live' ? 'live' : 'reload', window: win });
    b.connect();
    return b;
}

var B={SDK,PREVIEW_COOKIE,PatchError,applyPatch,isPreview,previewProof,createBridge,reloadSwap,autoStart};window.HydrogenBridge=B;autoStart(document.currentScript)})();
