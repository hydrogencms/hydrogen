import { createRegistry } from '@hydrogencms/astro/runtime'
import Callout from './blocks/Callout.astro'
import Feature from './blocks/Feature.astro'
import Features from './blocks/Features.astro'
import Hero from './blocks/Hero.astro'
import Text from './blocks/Text.astro'

/** Block type (schema/blocks.yaml) -> component. Every component spreads `attrs` on its root; `shared` is built in. */
export const registry = createRegistry({ hero: Hero, text: Text, features: Features, feature: Feature, callout: Callout })
