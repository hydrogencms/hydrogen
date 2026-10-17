// Test helper: props for `mount` that the test can change afterwards (and the component can bind to).
export function reactive<T extends object>(o: T): T {
  const s = $state(o);
  return s;
}
