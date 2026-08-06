// Tailwind — standalone CLI ile derlenir (.tools/tailwindcss). Node YOK.
// Palet ve tipografi kaynagı: .claude/skills/tappa-brand/SKILL.md
/** @type {import('tailwindcss').Config} */
module.exports = {
  // 🔴 VENDORED CODE LIVES OUTSIDE THESE GLOBS, and that is structural rather than
  // a filter. Tailwind scans every content file as RAW TEXT, so a minified library
  // is 51 KB of identifiers that look like utility names: dropping
  // web/static/js/htmx.min.js in grew app.css by three rules nothing renders
  // (.ease-in, .resize, .transition) out of htmx's OWN internal strings. Proven by
  // building both ways.
  //
  // THE FIRST FIX WAS A '!*.min.js' EXCLUSION AND AN AUDIT DEFEATED IT TWICE: a
  // subdirectory (web/static/js/vendor/htmx.min.js -- '**/*.js' matches, a
  // single-level '*.min.js' does not) and a vendored file simply not named .min
  // both brought the three rules back. A pattern that has to predict how the next
  // library is filed is not a rule, it is a guess.
  //
  // SO THE DIRECTORY IS THE RULE: web/static/js/ is OURS and is scanned;
  // web/static/vendor/ is other people's and is not named here at all. Both are
  // still embedded and served (web/embed.go embeds all:static) -- this decides only
  // what Tailwind READS. Guarded by TestTailwind_ScansNoMinifiedSource, which holds
  // the property rather than the path: nothing these globs match may look minified.
  content: ['./web/templates/**/*.templ', './web/static/js/**/*.js'],
  theme: {
    extend: {
      colors: {
        ink: '#152219',
        porcelain: '#EDF0EA',
        paper: '#FFFDF4',
        'tappa-green': '#1F5C41',
        'green-lite': '#E1EDE6',
        saffron: '#D98E2B',
        'saffron-lite': '#F7EBD6',
        tomato: '#BE3D2A',
        line: '#C9D2C8',
      },
      fontFamily: {
        display: ['"Space Grotesk"', 'system-ui', 'sans-serif'],
        mono: ['"IBM Plex Mono"', 'ui-monospace', 'monospace'],
      },
      backgroundImage: {
        // Adisyon perforasyonu — gorsel dosya degil, saf CSS.
        'perf-top': 'radial-gradient(circle at 6px 0, transparent 5px, #FFFDF4 5px)',
        'perf-bottom': 'radial-gradient(circle at 6px 100%, transparent 5px, #FFFDF4 5px)',
      },
      backgroundSize: { perf: '12px 8px' },
    },
  },
  plugins: [],
}
