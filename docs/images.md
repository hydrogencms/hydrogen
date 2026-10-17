# Images

The images module makes resized renditions of uploaded raster images from **named presets**. It is optional: without
it assets have no `variants` and the admin shows the original files. Add it next to the media setup (an upload type
in the schema, see [media.md](media.md)):

```go
h.Use(sqlite.Module(), images.Module())
```

```yaml
modules:
  images:
    presets:
      card: { width: 640, height: 360, fit: cover, widths: [320, 480] }
      hero: { width: 1600, widths: [640, 1024], format: jpeg, quality: 80 }
```

Only presets can be requested; there are no arbitrary sizes, so nobody can make the server resize without limit.
Without upload types in the schema the module does nothing.

## Configuration

`modules.images`:

| Key | Default | Meaning |
| --- | --- | --- |
| `driver` | `builtin` | `builtin` resizes in process; `imgproxy` hands the work to an imgproxy server. |
| `public_url` | empty | Base of variant URLs when a CDN fronts `/images` (replaces `/images` in emitted URLs). |
| `max_pixels` | 50000000 | Sources with more pixels (width × height) get no variants. |
| `workers` | 0 | Concurrent transforms; 0 is half the CPUs (at least 1). |
| `presets` | none | Named presets, below. |
| `imgproxy` | none | `url`, `key`, `salt`, `source`; driver `imgproxy` only. |

A preset name is a lowercase letter followed by up to 31 lowercase letters, digits or `_`.

| Preset key | Meaning |
| --- | --- |
| `width` | Required, 1 to 8192. |
| `height` | Optional, 1 to 8192. |
| `fit` | `contain` (default) fits inside the box and keeps the ratio. `cover` fills the box exactly, cropping around the focal point; it needs `height`. |
| `widths` | Extra widths for `srcset`, each from 16 up to below `width`. Sorted on load. |
| `format` | `original` (default), `jpeg`, `png`, `webp` or `avif`. `webp` and `avif` need driver `imgproxy`. |
| `quality` | 1 to 100, default 82. |
| `upscale` | Default false: a variant is never larger than the source. |

A preset named `admin` (480 x 480, `contain`) is built in for the admin's thumbnails. You may override it; it is
never listed in delivery. Mistakes stop start-up. Unknown keys and values of the wrong type are reported with the file and line; the
module's own checks (preset rules, driver, imgproxy settings) are reported by key path, such as
`modules.images.presets.card`, without a position.

## URLs

```
GET|HEAD /images/{preset}/{v}/{w}/{focal}/{sha}/{name}
```

`v` is a short hash of the preset's parameters, the driver and the engine revision: changing a preset changes its
URLs, so they are cached forever (`Cache-Control: public, max-age=31536000, immutable`). `w` must be the preset's
`width` or one of its `widths`. `focal` is `c` (the centre) or four digits `XXYY`, the asset's focal point in
hundredths (x 0.25, y 0.5 is `2550`). `contain` presets always use `c`, and so does a `cover` asset without a
focal point. `name` is the original name with the extension of the output format (`cat.png`
becomes `cat.jpg` for `format: jpeg`; with `original`, GIF and WebP sources turn into PNG).

- The first request makes the variant, stores it next to the original and serves it (`Hydrogen-Image: generated`);
  later requests read the stored copy (`stored`). Concurrent requests for the same variant share one transform.
- A request with an outdated `v` for a known preset is redirected (302, one hour) to the current URL.
- 404: unknown preset or width, unknown file, a file that is not a decodable raster image (SVG is never
  transformed), or one over `max_pixels`.
- 503 with `Retry-After: 5` when no worker frees up within 30 seconds. A transform that runs longer than 60 seconds
  is abandoned and answers 500, as does a transform that fails unexpectedly.
- A focal point other than `c` is served only if some item whose file is that upload has exactly that focal point
  (draft or published), so the work stays bounded by real focal points; any other value is 404. The check runs only
  when the variant is not stored yet. The focal points of a file are remembered for 30 seconds, and an unknown one
  reloads them at most every 2 seconds, so a focal point just set is accepted within about 2 seconds. Error
  responses are `no-store`. Items in the trash are not consulted.
- Any uploaded file can be transformed. As with `/assets`, the URLs are public and the hashes unguessable.

Re-encoding drops all metadata (EXIF, GPS) and, with the built-in driver, the colour profile: a Display P3 or Adobe
RGB photo is shown as sRGB and looks less saturated (imgproxy keeps or converts profiles). The JPEG orientation is
applied. `cover` presets crop around the focal point in the URL. Changing an asset's focal point gives it new URLs;
variants of the old point stay stored until the upload is removed.

## Delivery variants

Assets in delivery responses gain `variants` for raster images whose size is known:

```json
"variants": {
  "card": { "url": "/images/card/1a2b3c4d/640/2550/<sha>/cat.jpg", "width": 640, "height": 360, "type": "image/jpeg",
            "srcset": "/images/card/1a2b3c4d/320/2550/<sha>/cat.jpg 320w, /images/card/1a2b3c4d/640/2550/<sha>/cat.jpg 640w" }
}
```

`url`, `width` and `height` are the preset's main size after planning (a small source is not enlarged). `srcset`
lists the allowed widths ascending, leaves out sizes that would repeat a width, and is absent when one entry is
left. SVG and other non-raster files, and images with unknown or oversized dimensions, get no `variants`. `file`
fields (as opposed to assets) get none either.

The channel option `image_presets` limits the presets a channel lists (default: all but `admin`):

```yaml
channels:
  app:
    image_presets: [card]
```

An unknown name fails config checks with its position.

## The admin

With the module on, the admin library, the picker and the asset field show thumbnails from the `admin` preset
(`media.thumbBase` in `config.json`). The details panel keeps the original. The admin's Content-Security-Policy
allows images from the origins of `media.public_url`, `modules.images.public_url` and the imgproxy server.

## imgproxy

```yaml
modules:
  images:
    driver: imgproxy
    presets:
      card: { width: 640, height: 360, fit: cover, format: webp }
    imgproxy:
      url: https://img.example.com
      key: ${env:IMGPROXY_KEY}      # hex, as IMGPROXY_KEY
      salt: ${env:IMGPROXY_SALT}    # hex, as IMGPROXY_SALT
      source: https://cms.example.com/assets/{sha}/{name}   # or e.g. s3://bucket/media/{sha}
```

Delivery emits signed imgproxy URLs directly, with the focal point for `cover` presets. `/images/...` redirects (302,
one day) to the signed URL, which is what the admin's thumbnails use. imgproxy must be able to read `source`; the
module stores nothing. `{name}` in `source` receives a canonical name made from the file type (`image.jpg`,
`image.png`, …), never the name in the URL, so every name maps to the same imgproxy URL; `{sha}` identifies the
file. Key and salt must come from the environment.

## CDN

Put a CDN in front of `/images` (or of imgproxy) and set `public_url` so emitted URLs point at it. URLs are
immutable, so the CDN can cache them for a year; a changed preset gets new URLs.

## Cleanup

Stored variants are deleted with their upload by the [media garbage collection](media.md#garbage-collection). The
daily job `images.gc` also deletes variants no preset can request any more: the preset was removed, its `v` changed,
the width is no longer allowed or the name is malformed. Variants of a focal point that was changed are not checked.

## Limits

Sources over `max_pixels` are not decoded. At most `workers` transforms run at once and each is capped at 60 seconds.
Decoding supports JPEG, PNG, GIF (first frame) and WebP; output is JPEG or PNG with the built-in driver. Sources are
read up to 64 MiB.
