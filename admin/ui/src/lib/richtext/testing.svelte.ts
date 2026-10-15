/** A reactive props object for mounting a component and changing its props from a test. */
export function reactive<T extends object>(initial: T): T {
  const s = $state(initial);
  return s;
}
