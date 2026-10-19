import { createRegistry } from '@hydrogencms/nuxt/runtime'
import Callout from '~/blocks/Callout.vue'
import Card from '~/blocks/Card.vue'
import Cards from '~/blocks/Cards.vue'
import Hero from '~/blocks/Hero.vue'
import Image from '~/blocks/Image.vue'
import RichText from '~/blocks/RichText.vue'

/** Block type to component. `shared` is built in: it renders the blocks delivery inlined. */
export const registry = createRegistry({ hero: Hero, richText: RichText, cards: Cards, card: Card, callout: Callout, image: Image })
