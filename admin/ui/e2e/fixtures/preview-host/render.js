// Renders a delivery-shaped document ({fields: {title, body}}) as plain HTML. Used by the server (reload mode)
// and served to the browser (live mode), so both paths print the same markup.
;(function (g) {
  var esc = function (s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
    })
  }
  var plain = function (n) {
    if (!n || typeof n !== 'object') return ''
    var out = typeof n.text === 'string' ? n.text : ''
    ;(n.content || []).forEach(function (c) {
      out += plain(c) + ' '
    })
    return out.trim()
  }
  var block = function (b) {
    var p = b.props || {}
    var inner = '<span class="t">' + esc(b.type) + '</span> <span class="h">' + esc(p.heading || p.title || p.text || '') + '</span>'
    if (b.type === 'richText') inner += ' <span class="rt">' + esc(plain(p.body)) + '</span>'
    if (b.type === 'hero' && p.tone) inner += ' <span class="tone">' + esc(p.tone) + '</span>'
    Object.keys(b.slots || {}).forEach(function (k) {
      inner += '<div class="slot" data-slot="' + esc(k) + '">' + b.slots[k].map(block).join('') + '</div>'
    })
    if (b.blocks) inner += '<div class="slot" data-slot="shared">' + b.blocks.map(block).join('') + '</div>'
    return '<div data-h-block="' + esc(b.type) + '">' + inner + '</div>'
  }
  g.renderDoc = function (doc) {
    var f = (doc && doc.fields) || {}
    return '<h1 id="title">' + esc(f.title) + '</h1><div id="blocks">' + (f.body || []).map(block).join('') + '</div>'
  }
})(globalThis)
