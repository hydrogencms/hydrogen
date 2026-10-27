import type { Schemas } from '../../lib/api/client';

export type Inherited = { group: Schemas['Group']; assignment: Schemas['Assignment'] };

/** Flattens each group's assignments into read-only rows, ordered by group name, then role name. */
export function inheritedRows(groups: Schemas['Group'][], perGroup: Schemas['Assignment'][][]): Inherited[] {
  const rows: Inherited[] = [];
  groups.forEach((group, i) => {
    for (const assignment of perGroup[i] ?? []) rows.push({ group, assignment });
  });
  const label = (a: Schemas['Assignment']) => a.role ?? a.roleId;
  return rows.sort(
    (x, y) => x.group.name.localeCompare(y.group.name) || label(x.assignment).localeCompare(label(y.assignment)) || x.assignment.id.localeCompare(y.assignment.id),
  );
}
