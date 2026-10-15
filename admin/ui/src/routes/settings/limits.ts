export type Limits = Record<string, string[]>;
export interface Choice {
  value: string;
  label: string;
}
export interface LimitDef {
  name: string;
  choices: Choice[] | null;
  policies: string[];
}
export interface PolicyDef {
  name: string;
  description: string;
  limitations: string[];
}
export interface RolePolicy {
  policy: string;
  limitations: Limits;
}
/** A policy as the API sends or accepts it: limitations may be absent. */
export interface PolicyBody {
  policy: string;
  limitations?: Limits;
}

export const ALL = '*';

/** Comma or newline separated text to a trimmed, de-duplicated list. */
export function parseValues(text: string): string[] {
  const out: string[] = [];
  for (const part of text.split(/[,\n]/)) {
    const v = part.trim();
    if (v && !out.includes(v)) out.push(v);
  }
  return out;
}

export function formatValues(values: string[] | undefined): string {
  return (values ?? []).join(', ');
}

/** Tick or untick a choice, keeping the order of the others. */
export function toggleValue(values: string[] | undefined, value: string, on: boolean): string[] {
  const cur = values ?? [];
  if (on) return cur.includes(value) ? cur : [...cur, value];
  return cur.filter((v) => v !== value);
}

/** A copy of limits with one limitation set; no values removes the limitation. */
export function setLimit(limits: Limits | undefined, name: string, values: string[]): Limits {
  const next: Limits = { ...(limits ?? {}) };
  if (values.length === 0) delete next[name];
  else next[name] = values;
  return next;
}

/** Limitation names that apply to a policy ('*' accepts every limitation). */
export function applicableTo(policy: string, policies: PolicyDef[], limits: LimitDef[]): string[] {
  if (policy === ALL) return limits.map((l) => l.name);
  return policies.find((p) => p.name === policy)?.limitations ?? [];
}

/** Limitation names that apply to any of a role's policies (for assignments). */
export function applicableToRole(role: { policy: string }[], policies: PolicyDef[], limits: LimitDef[]): string[] {
  const names = new Set<string>();
  for (const rp of role) for (const n of applicableTo(rp.policy, policies, limits)) names.add(n);
  return limits.map((l) => l.name).filter((n) => names.has(n));
}

export function policyModule(name: string): string {
  return name === ALL ? ALL : name.split('.')[0];
}

/** Catalog policies grouped by module prefix, groups and members sorted. */
export function groupPolicies(policies: PolicyDef[]): { module: string; policies: PolicyDef[] }[] {
  const by = new Map<string, PolicyDef[]>();
  for (const p of policies) {
    const m = policyModule(p.name);
    by.set(m, [...(by.get(m) ?? []), p]);
  }
  return [...by.keys()]
    .sort()
    .map((module) => ({ module, policies: by.get(module)!.sort((a, b) => a.name.localeCompare(b.name)) }));
}

// Role policy table state: pure helpers over the list the editor edits.

export function addPolicy(list: RolePolicy[], policy: string): RolePolicy[] {
  return [...list, { policy, limitations: {} }];
}

export function removePolicy(list: RolePolicy[], index: number): RolePolicy[] {
  return list.filter((_, i) => i !== index);
}

export function setPolicyLimit(list: RolePolicy[], index: number, name: string, values: string[]): RolePolicy[] {
  return list.map((rp, i) => (i === index ? { ...rp, limitations: setLimit(rp.limitations, name, values) } : rp));
}

/** The API body: no empty limitations, and the key omitted when none. */
export function cleanPolicies(list: PolicyBody[]): PolicyBody[] {
  return list.map((rp) => {
    const limitations: Limits = {};
    for (const [k, v] of Object.entries(rp.limitations ?? {})) if (v.length > 0) limitations[k] = v;
    return Object.keys(limitations).length > 0 ? { policy: rp.policy, limitations } : { policy: rp.policy };
  });
}

/** Policies from the catalog not yet in the role, for the add picker. */
export function addable(policies: PolicyDef[], list: RolePolicy[]): PolicyDef[] {
  const have = new Set(list.map((p) => p.policy));
  return policies.filter((p) => !have.has(p.name));
}
