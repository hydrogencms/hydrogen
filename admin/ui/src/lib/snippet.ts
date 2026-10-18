export interface Segment {
  text: string;
  mark: boolean;
}

const entities: Record<string, string> = { '&amp;': '&', '&lt;': '<', '&gt;': '>', '&#34;': '"', '&quot;': '"', '&#39;': "'", '&#x27;': "'" };
const unescape = (s: string) => s.replace(/&(?:amp|lt|gt|quot|#34|#39|#x27);/g, (m) => entities[m] ?? m);

// parseSnippet reads the server's snippet (escaped text with <mark>…</mark> around matches) into plain-text
// segments, so it can be rendered without {@html}. Any other tag is not markup the server emits and stays text.
export function parseSnippet(html: string): Segment[] {
  const out: Segment[] = [];
  let mark = false;
  for (const part of html.split(/(<mark>|<\/mark>)/)) {
    if (part === '<mark>') mark = true;
    else if (part === '</mark>') mark = false;
    else if (part) out.push({ text: unescape(part), mark });
  }
  return out;
}
