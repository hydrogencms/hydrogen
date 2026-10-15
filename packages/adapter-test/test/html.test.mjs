import { test } from 'node:test'
import assert from 'node:assert/strict'
import * as h from '../dist/html.js'

const page = `<!DOCTYPE html><html lang="en"><head>
<title>About &amp; us</title>
<meta name="robots" content="noindex"/><meta content="1" name="hydrogen-preview">
<link rel="canonical" href="https://x.test/en/about?a=1&amp;b=2"/>
<link rel="alternate" hreflang="de" href="https://x.test/de/uber-uns"/>
<link rel="alternate" hreflang="en" href='https://x.test/en/about'>
<link rel="stylesheet" href="/a.css">
<script>self.__next_f.push([1,"<div data-h-block=\\"fake\\"><h1>Fake</h1></div>"])</script>
</head><body>
<!-- <div data-h-block="comment"></div> -->
<h1 class="t"><span data-h-path="fields/title">About   &#39;us&#39;</span></h1>
<section data-h-block="hero" data-h-path="fields/body/0"><h2>Hi</h2></section>
<div data-h-block=cards><article data-h-block="card"></article><article data-h-block="card"/></div>
<template><p data-h-block="in-template"></p></template>
</body></html>`

test('block and path markers, in document order, ignoring scripts, comments and templates', () => {
  assert.deepEqual(h.blockMarkers(page), ['hero', 'cards', 'card', 'card'])
  assert.deepEqual(h.pathMarkers(page), ['fields/title', 'fields/body/0'])
})

test('h1 text strips tags, decodes entities and collapses whitespace', () => {
  assert.deepEqual(h.h1s(page), ["About 'us'"])
})

test('title, meta, canonical, alternates, noindex', () => {
  assert.equal(h.title(page), 'About & us')
  assert.equal(h.meta(page, 'hydrogen-preview'), '1')
  assert.equal(h.canonical(page), 'https://x.test/en/about?a=1&b=2')
  assert.deepEqual(h.alternates(page), { de: 'https://x.test/de/uber-uns', en: 'https://x.test/en/about' })
  assert.ok(h.noindex(page))
  assert.ok(!h.noindex('<meta name="robots" content="index,follow">'))
})

test('attributes: quoted, single quoted, bare, case-insensitive names', () => {
  const [t] = h.tags(`<DIV Data-H-Block='a b' id=x data-flag>`, 'div')
  assert.deepEqual(t.attrs, { 'data-h-block': 'a b', id: 'x', 'data-flag': '' })
})

test('decode handles numeric and named entities and leaves unknown ones', () => {
  assert.equal(h.decode('a&nbsp;&lt;&#x41;&#66;&bogus;'), 'a <AB&bogus;')
})
