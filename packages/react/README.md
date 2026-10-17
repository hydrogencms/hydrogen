# @hydrogencms/react

React blocks renderer, rich text, live preview and images for Hydrogen frontends.

## Image

```tsx
import { Image } from '@hydrogencms/react'

<Image asset={item.fields.hero} preset="card" sizes="(min-width: 800px) 640px, 100vw" />
```

Renders an `<img>` from a delivery asset. With `preset`, it uses that preset's variant (`src`, `srcSet`, `width`,
`height`) when the images module produced one, else the original file. `alt` defaults to the asset's `alt` field.
`loading="lazy"` and `decoding="async"` are defaults that props override, and the asset's focal point becomes
`object-position` (merged with a passed `style`). `imageProps(asset, preset)` from `@hydrogencms/client` gives the
same attributes for other renderers.
