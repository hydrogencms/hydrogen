// Types `Astro.locals.hydrogen`: /// <reference types="@hydrogencms/astro/env" /> in src/env.d.ts.
import type { HydrogenLocals } from './dist/runtime.js'

declare global {
  namespace App {
    interface Locals {
      hydrogen: HydrogenLocals
    }
  }
}
