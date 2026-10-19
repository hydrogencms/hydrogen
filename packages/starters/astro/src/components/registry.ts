import { createRegistry } from '@hydrogencms/astro/runtime'
import Callout from './blocks/Callout.astro'
import Card from './blocks/Card.astro'
import Cards from './blocks/Cards.astro'
import Hero from './blocks/Hero.astro'
import ImageBlock from './blocks/ImageBlock.astro'
import RichTextBlock from './blocks/RichTextBlock.astro'

/** Block type -> component. Every component spreads `attrs` on its root; `shared` is built in. */
export const registry = createRegistry({ hero: Hero, richText: RichTextBlock, cards: Cards, card: Card, callout: Callout, image: ImageBlock })
