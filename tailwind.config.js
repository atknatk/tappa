// Tailwind — standalone CLI ile derlenir (.tools/tailwindcss). Node YOK.
// Palet ve tipografi kaynagı: .claude/skills/tappa-brand/SKILL.md
/** @type {import('tailwindcss').Config} */
module.exports = {
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
