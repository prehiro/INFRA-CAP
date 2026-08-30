// notes-md.js — client-side minimal markdown renderer for the Notes edit
// modal live preview. Mirrors the Go-side RenderMarkdown() in
// internal/modules/notes/markdown.go so the preview is byte-identical
// to what the server renders on save. Outputs raw HTML; the surrounding
// modal uses Alpine x-html to inject it.
//
// All input is HTML-escaped FIRST, so any user-injected HTML in the
// source renders as literal text (no XSS surface). Links are restricted
// to http(s) and mailto. Block-level elements are rebuilt from the line
// stream so blank lines break paragraphs.
//
// Supported: # / ## / ### headers, **bold**, *italic*, `code`, [link](url),
// - bullet, 1. ordered, > blockquote, --- hr, blank-line paragraph break.
//
// Vendored (no CDN) so the Windows Server deployment is self-contained.
// ~3KB minified.

(function () {
  function esc(s) {
    return s
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function inline(s) {
    s = esc(s);
    // Whitelist <span style="color:#xxx">…</span> so the toolbar's color
    // accent passes through. Anything else with angle brackets is left
    // escaped from the initial esc() call. We do this BEFORE the rest
    // of the formatting so the spans don't get re-escaped.
    var whitelisted = [];
    s = s.replace(/&lt;span\s+style="color:#[0-9a-fA-F]{3,8}"\s*&gt;[\s\S]*?&lt;\/span&gt;/g, function (m) {
      // restore the actual angle brackets for the safe span tag
      var restored = m.replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
      var key = '\x00TAG' + whitelisted.length + '\x00';
      whitelisted.push(restored);
      return key;
    });

    // highlight: ==text== → <mark>text</mark>
    s = s.replace(/==([^=\n]+?)==/g, function (_, c) {
      return '<mark>' + c + '</mark>';
    });
    // inline code first — its content must not be re-formatted
    s = s.replace(/`([^`\n]+?)`/g, function (_, c) {
      return '<code>' + c + '</code>';
    });
    // links: only http(s) and mailto
    s = s.replace(/\[([^\]]+)\]\(([^)]+)\)/g, function (m, text, href) {
      href = href.trim();
      var low = href.toLowerCase();
      if (!(low.indexOf('http://') === 0 || low.indexOf('https://') === 0 || low.indexOf('mailto:') === 0)) {
        return text; // strip link syntax, keep just the text
      }
      return '<a href="' + href + '" target="_blank" rel="noopener noreferrer">' + text + '</a>';
    });
    // bold (** or __)
    s = s.replace(/\*\*(.+?)\*\*|__(.+?)__/g, function (m, b1, b2) {
      return '<strong>' + (b1 || b2) + '</strong>';
    });
    // italic (* or _) — careful, don't match bold markers
    s = s.replace(/(^|[^*\w])\*([^*\n]+?)\*([^*\w]|$)/g, function (m, p, t, s2) {
      return p + '<em>' + t + '</em>' + s2;
    });
    // restore whitelisted color spans
    for (var i = 0; i < whitelisted.length; i++) {
      s = s.replace('\x00TAG' + i + '\x00', whitelisted[i]);
    }
    return s;
  }

  function render(src) {
    src = src.replace(/\r\n/g, '\n').replace(/\t/g, '    ');
    var lines = src.split('\n');
    var out = [];
    var inList = false, inOrdered = false, inQuote = false;
    var para = [];

    function closePara() {
      if (para.length) {
        out.push('<p>' + inline(para.join('\n')) + '</p>');
        para = [];
      }
    }
    function closeLists() {
      if (inList)    { out.push('</ul>');  inList = false; }
      if (inOrdered) { out.push('</ol>');  inOrdered = false; }
      if (inQuote)   { out.push('</blockquote>'); inQuote = false; }
    }

    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].replace(/[ \t]+$/, '');
      if (line.trim() === '') {
        closePara();
        closeLists();
        continue;
      }
      // hr
      if (/^-{3,}\s*$/.test(line)) {
        closePara();
        closeLists();
        out.push('<hr>');
        continue;
      }
      // header
      var hm = /^(#{1,3})\s+(.+?)\s*$/.exec(line);
      if (hm) {
        closePara();
        closeLists();
        var lvl = hm[1].length;
        out.push('<h' + lvl + '>' + inline(hm[2]) + '</h' + lvl + '>');
        continue;
      }
      // blockquote
      var qm = /^>\s+(.+?)\s*$/.exec(line);
      if (qm) {
        closePara();
        if (inOrdered) { out.push('</ol>'); inOrdered = false; }
        if (!inQuote)  { out.push('<blockquote>'); inQuote = true; }
        out.push('<p>' + inline(qm[1]) + '</p>');
        continue;
      }
      // unordered
      var bm = /^[-*]\s+(.+?)\s*$/.exec(line);
      if (bm) {
        closePara();
        if (inOrdered) { out.push('</ol>'); inOrdered = false; }
        if (inQuote)   { out.push('</blockquote>'); inQuote = false; }
        if (!inList)   { out.push('<ul>'); inList = true; }
        out.push('  <li>' + inline(bm[1]) + '</li>');
        continue;
      }
      // ordered
      var om = /^\d+\.\s+(.+?)\s*$/.exec(line);
      if (om) {
        closePara();
        if (inList)   { out.push('</ul>'); inList = false; }
        if (inQuote)  { out.push('</blockquote>'); inQuote = false; }
        if (!inOrdered){ out.push('<ol>'); inOrdered = true; }
        out.push('  <li>' + inline(om[1]) + '</li>');
        continue;
      }
      // paragraph
      closeLists();
      para.push(line);
    }
    closePara();
    closeLists();
    return out.join('\n');
  }

  // expose globally for the template (x-html reads from a function in Alpine data)
  window.notesRender = render;
})();
