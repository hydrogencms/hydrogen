import { describe, expect, it } from 'vitest';
import { inheritedRows } from './inherited';

const g = (id: string, name: string) => ({ id, name }) as never;
const a = (id: string, role: string) => ({ id, roleId: 'r' + id, role, limitations: {} }) as never;

describe('inheritedRows', () => {
  it('orders by group then role and keeps the source group', () => {
    const rows = inheritedRows([g('2', 'Writers'), g('1', 'Editors')], [[a('x', 'Writer')], [a('z', 'Reviewer'), a('y', 'Author')]]);
    expect(rows.map((r) => `${r.group.name}/${r.assignment.role}`)).toEqual(['Editors/Author', 'Editors/Reviewer', 'Writers/Writer']);
  });
  it('is empty without groups or assignments', () => {
    expect(inheritedRows([], [])).toEqual([]);
    expect(inheritedRows([g('1', 'A')], [[]])).toEqual([]);
  });
});
