/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        iepl: '#22c55e',
        cn2: '#38bdf8',
        cernet: '#a78bfa',
        blocked: '#ef4444',
        standard: '#facc15',
        ink: {
          900: '#0b1220',
          800: '#111a2f',
          700: '#1a2340',
        },
      },
      fontFamily: {
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'monospace'],
      },
    },
  },
  plugins: [],
};
