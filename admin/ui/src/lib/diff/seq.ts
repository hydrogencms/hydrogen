// Sequence diffs: an LCS over indexes, and word diffs built on it. Pure.

export interface SeqOp {
  k: 'eq' | 'del' | 'ins';
  /** Index in the first sequence (eq, del). */
  a?: number;
  /** Index in the second sequence (eq, ins). */
  b?: number;
}

/** Above this many cells the middle part is not matched (replaced whole), to bound time and memory. */
const MAX_CELLS = 4_000_000;

/** Edit script from `a` to `b`: common prefix and suffix first, then an LCS of the middle. Deletions precede insertions in each changed run. */
export function diffSeq<T>(a: readonly T[], b: readonly T[], eq: (x: T, y: T) => boolean = (x, y) => x === y): SeqOp[] {
  return diffSeqEx(a, b, eq).ops;
}

/** As `diffSeq`; `approx` says the middle part was too large to match and was replaced whole. `limit` is the cell budget. */
export function diffSeqEx<T>(a: readonly T[], b: readonly T[], eq: (x: T, y: T) => boolean = (x, y) => x === y, limit = MAX_CELLS): { ops: SeqOp[]; approx: boolean } {
  let approx = false;
  let lo = 0;
  while (lo < a.length && lo < b.length && eq(a[lo]!, b[lo]!)) lo++;
  let ea = a.length;
  let eb = b.length;
  while (ea > lo && eb > lo && eq(a[ea - 1]!, b[eb - 1]!)) {
    ea--;
    eb--;
  }
  const ops: SeqOp[] = [];
  for (let i = 0; i < lo; i++) ops.push({ k: 'eq', a: i, b: i });
  const n = ea - lo;
  const m = eb - lo;
  if (n && m && n * m <= limit) {
    // t[i][j] = LCS length of a[lo+i..] and b[lo+j..]
    const w = m + 1;
    const t = new Int32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        t[i * w + j] = eq(a[lo + i]!, b[lo + j]!) ? t[(i + 1) * w + j + 1]! + 1 : Math.max(t[(i + 1) * w + j]!, t[i * w + j + 1]!);
      }
    }
    let i = 0;
    let j = 0;
    let dels: SeqOp[] = [];
    let inss: SeqOp[] = [];
    const flush = () => {
      ops.push(...dels, ...inss);
      dels = [];
      inss = [];
    };
    while (i < n || j < m) {
      if (i < n && j < m && eq(a[lo + i]!, b[lo + j]!)) {
        flush();
        ops.push({ k: 'eq', a: lo + i, b: lo + j });
        i++;
        j++;
      } else if (j < m && (i === n || t[i * w + j + 1]! >= t[(i + 1) * w + j]!)) {
        inss.push({ k: 'ins', b: lo + j++ });
      } else {
        dels.push({ k: 'del', a: lo + i++ });
      }
    }
    flush();
  } else {
    approx = n > 0 && m > 0;
    for (let i = lo; i < ea; i++) ops.push({ k: 'del', a: i });
    for (let j = lo; j < eb; j++) ops.push({ k: 'ins', b: j });
  }
  for (let i = ea, j = eb; i < a.length; i++, j++) ops.push({ k: 'eq', a: i, b: j });
  return { ops, approx };
}

export interface Seg {
  k: 'eq' | 'ins' | 'del';
  text: string;
}

interface Word {
  /** Whitespace before the word. */
  lead: string;
  word: string;
}

function words(s: string): { list: Word[]; tail: string } {
  const list: Word[] = [];
  let end = 0;
  for (const m of s.matchAll(/(\s*)(\S+)/g)) {
    list.push({ lead: m[1]!, word: m[2]! });
    end = m.index! + m[0].length;
  }
  return { list, tail: s.slice(end) };
}

/** Below this share of common words the two texts are shown as one deletion and one insertion. */
export const MIN_SIMILARITY = 0.3;

export interface WordDiff {
  segs: Seg[];
  /** Too long to match word by word: part of the text is shown as replaced. */
  approx: boolean;
}

/**
 * Word diff of two strings. Words are matched by their non-space text; spacing travels with the words. Whitespace-only
 * unchanged runs between changes are folded into them, and mostly different texts become one deletion and one insertion.
 */
export function diffWordsEx(a: string | undefined, b: string | undefined, limit?: number): WordDiff {
  const A = words(a ?? '');
  const B = words(b ?? '');
  const whole = (): Seg[] => {
    const out: Seg[] = [];
    if (a) out.push({ k: 'del', text: a });
    if (b) out.push({ k: 'ins', text: b });
    return out;
  };
  if (!a || !b) return { segs: whole(), approx: false };
  const { ops, approx } = diffSeqEx(
    A.list.map((w) => w.word),
    B.list.map((w) => w.word),
    undefined,
    limit,
  );
  const common = ops.filter((o) => o.k === 'eq').length;
  const most = Math.max(A.list.length, B.list.length);
  if (most && common / most < MIN_SIMILARITY) return { segs: whole(), approx };

  const raw: Seg[] = [];
  for (const op of ops) {
    if (op.k === 'eq') raw.push({ k: 'eq', text: B.list[op.b!]!.lead + B.list[op.b!]!.word });
    else if (op.k === 'del') raw.push({ k: 'del', text: A.list[op.a!]!.lead + A.list[op.a!]!.word });
    else raw.push({ k: 'ins', text: B.list[op.b!]!.lead + B.list[op.b!]!.word });
  }
  // Trailing space: from the new side when it matches, otherwise a change of its own.
  if (A.tail !== B.tail) {
    if (A.tail) raw.push({ k: 'del', text: A.tail });
    if (B.tail) raw.push({ k: 'ins', text: B.tail });
  } else if (B.tail) raw.push({ k: 'eq', text: B.tail });
  const segs = tidy(raw);
  // Only spacing differs: show the texts as replaced rather than as no change.
  if (a !== b && !hasChange(segs)) return { segs: whole(), approx: false };
  return { segs, approx };
}

/** Joins runs: one deletion then one insertion per change, with whitespace-only equal runs between changes folded in. */
function tidy(raw: Seg[]): Seg[] {
  const out: Seg[] = [];
  let del = '';
  let ins = '';
  const addEq = (text: string) => {
    const last = out[out.length - 1];
    if (last && last.k === 'eq') last.text += text;
    else out.push({ k: 'eq', text });
  };
  const flush = () => {
    // Spacing both sides start with is not part of the change.
    if (del && ins) {
      const lead = /^\s*/.exec(del)![0];
      if (lead && ins.startsWith(lead)) {
        addEq(lead);
        del = del.slice(lead.length);
        ins = ins.slice(lead.length);
      }
    }
    if (del) out.push({ k: 'del', text: del });
    if (ins) out.push({ k: 'ins', text: ins });
    del = '';
    ins = '';
  };
  const changing = () => !!(del || ins);
  for (let i = 0; i < raw.length; i++) {
    const s = raw[i]!;
    if (s.k === 'del') del += s.text;
    else if (s.k === 'ins') ins += s.text;
    else {
      // Equal whitespace between two changes belongs to both sides of the change.
      const between = !s.text.trim() && changing() && raw.slice(i + 1).some((x) => x.k !== 'eq');
      if (between) {
        del += s.text;
        ins += s.text;
      } else {
        flush();
        addEq(s.text);
      }
    }
  }
  flush();
  return out;
}

/** Word diff of two strings; a missing side is an all-inserted or all-deleted result. */
export const diffWords = (a: string | undefined, b: string | undefined): Seg[] => diffWordsEx(a, b).segs;

export const hasChange = (segs: Seg[]): boolean => segs.some((s) => s.k !== 'eq');
