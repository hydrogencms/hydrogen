declare module 'virtual:hydrogen/instance' {
  import type { Hydrogen } from './runtime.js'
  const hydrogen: Hydrogen
  export default hydrogen
}
declare module 'virtual:hydrogen/bridge' {
  const source: string
  export default source
}
