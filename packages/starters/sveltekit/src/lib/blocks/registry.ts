import { createRegistry } from '@hydrogencms/sveltekit/components'
import Callout from './Callout.svelte'
import Card from './Card.svelte'
import Cards from './Cards.svelte'
import Hero from './Hero.svelte'
import ImageBlock from './ImageBlock.svelte'
import RichTextBlock from './RichTextBlock.svelte'

/** By block type; `shared` is built in: it renders the blocks delivery inlined. */
export const registry = createRegistry({ hero: Hero, richText: RichTextBlock, cards: Cards, card: Card, callout: Callout, image: ImageBlock })
