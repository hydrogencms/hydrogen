(function(){"use strict";const SDK = '@hydrogencms/bridge@0.1.0';
const PREVIEW_COOKIE = 'hydrogen-preview';
class PatchError extends Error {
    constructor(m) {
        super(m);
        this.name = 'PatchError';
    }
}
const isArr = Array.isArray;
const isObj = (v) => typeof v === 'object' && v !== null && !isArr(v);
function pointer(path) {
    if (path === '')
        return [];
    if (path[0] !== '/')
        throw new PatchError(`invalid pointer "${path}": must be empty or start with "/"`);
    return path.slice(1).split('/').map((t) => t.replace(/~1/g, '/').replace(/~0/g, '~'));
}
function index(arr, t, path, max) {
    if (!/^(0|[1-9]\d*)$/.test(t) || +t > max)
        throw new PatchError(`${path}: bad array index "${t}" (length ${arr.length})`);
    return +t;
}
function child(node, t, path) {
    if (isArr(node))
        return node[index(node, t, path, node.length - 1)];
    if (isObj(node) && Object.hasOwn(node, t))
        return node[t];
    throw new PatchError(`${path}: no such path "${t}"`);
}
function put(node, t, v, path) {
    if (isArr(node)) {
        const c = node.slice();
        c[index(node, t, path, node.length - 1)] = v;
        return c;
    }
    return { ...node, [t]: v };
}
function apply(node, t, i, o) {
    const k = t[i];
    if (i < t.length - 1)
        return put(node, k, apply(child(node, k, o.path), t, i + 1, o), o.path);
    const { op, path } = o;
    if (isArr(node)) {
        if (op === 'add') {
            const at = k === '-' ? node.length : index(node, k, path, node.length);
            return [...node.slice(0, at), o.value, ...node.slice(at)];
        }
        const at = index(node, k, path, node.length - 1);
        if (op === 'remove')
            return [...node.slice(0, at), ...node.slice(at + 1)];
        return put(node, k, o.value, path);
    }
    if (!isObj(node))
        throw new PatchError(`${path}: parent is not an object or array`);
    if (op === 'add')
        return { ...node, [k]: o.value };
    if (!Object.hasOwn(node, k))
        throw new PatchError(`${path}: no such path "${k}"`);
    if (op === 'replace')
        return { ...node, [k]: o.value };
    const { [k]: _, ...rest } = node;
    return rest;
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
                    throw new PatchError('cannot remove the document root');
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
function createBridge(o) {
    const w = o.window ?? window;
    const origin = o.adminOrigin;
    if (!/^https?:\/\/[^/*]+$/.test(origin))
        throw new Error(`bridge: adminOrigin must be an origin like https://admin.example.com, got "${origin}"`);
    let rev = -1;
    let doc;
    let t0 = 0;
    let on = false;
    const now = () => (w.performance ?? Date).now();
    const post = (m) => w.parent.postMessage({ h: 1, ...m }, origin);
    const ready = () => {
        const preview = previewProof(w);
        post({ type: 'ready', mode: o.mode, sdk: SDK, url: w.location.href, ...(preview ? { preview } : {}) });
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
            else if (m.type === 'reload') {
                const r = m.rev;
                if (o.onReload)
                    void o.onReload(r);
                else if (w.origin === 'null')
                    reload();
                else {
                    reloadSwap({ window: w, ready }).then((ms) => bridge.rendered(r, ms), (x) => (x?.fetch ? reload() : error(String(x?.message ?? x))));
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
            }
            on = false;
        },
        rendered(r, ms) {
            if (!on)
                return;
            const raf = w.requestAnimationFrame?.bind(w) ?? ((f) => setTimeout(f, 0));
            raf(() => raf(() => post({ type: 'rendered', rev: r, ms: ms ?? Math.max(0, Math.round(now() - t0)) })));
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
