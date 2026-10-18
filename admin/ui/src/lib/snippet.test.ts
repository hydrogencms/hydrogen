import { describe, expect, it } from 'vitest';
import { parseSnippet } from './snippet';

describe('parseSnippet', () => {
  it('splits text and marks', () => {
    expect(parseSnippet('cheap <mark>solar</mark> power')).toEqual([
      { text: 'cheap ', mark: false },
      { text: 'solar', mark: true },
      { text: ' power', mark: false },
    ]);
  });
  it('handles leading marks, adjacent marks and empty input', () => {
    expect(parseSnippet('<mark>a</mark><mark>b</mark>')).toEqual([
      { text: 'a', mark: true },
      { text: 'b', mark: true },
    ]);
    expect(parseSnippet('')).toEqual([]);
  });
  it('unescapes the entities Go emits, once', () => {
    expect(parseSnippet('a &amp; b &lt;i&gt; &#34;q&#34; &#39;s&#39; &quot;x&quot; &#x27;y&#x27;')).toEqual([{ text: `a & b <i> "q" 's' "x" 'y'`, mark: false }]);
    expect(parseSnippet('&amp;lt;')).toEqual([{ text: '&lt;', mark: false }]);
    expect(parseSnippet('&copy;')).toEqual([{ text: '&copy;', mark: false }]);
  });
  it('keeps escaped markup as text, never as marks', () => {
    expect(parseSnippet('&lt;mark&gt;x&lt;/mark&gt; <mark>&lt;script&gt;</mark>')).toEqual([
      { text: '<mark>x</mark> ', mark: false },
      { text: '<script>', mark: true },
    ]);
  });
  it('tolerates an unclosed mark', () => {
    expect(parseSnippet('<mark>open')).toEqual([{ text: 'open', mark: true }]);
  });
});
