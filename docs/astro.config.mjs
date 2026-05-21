// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import cloudflare from '@astrojs/cloudflare';
import rehypeAstroRelativeMarkdownLinks from 'astro-rehype-relative-markdown-links';

export default defineConfig({
  site: 'https://gaffer.kurrent.io',
  adapter: cloudflare({
    imageService: 'compile',
    prerenderEnvironment: 'node',
  }),
  integrations: [
    starlight({
      title: 'Gaffer',
      logo: {
        dark: './src/assets/kurrent-logo-white.svg',
        light: './src/assets/kurrent-logo-black.svg',
        replacesTitle: true,
      },
      customCss: [
        './src/styles/work-sans.css',
        './src/styles/spline-sans.css',
        './src/styles/colors.css',
        './src/styles/custom.css',
      ],
      // Starlight emits `<link rel="shortcut icon">` for this; point it at
      // the .ico (legacy slot, legacy format). Modern formats follow via `head`.
      favicon: '/favicons/favicon.ico',
      head: [
        { tag: 'link', attrs: { rel: 'icon', type: 'image/svg+xml', href: '/favicons/favicon.svg' } },
        { tag: 'link', attrs: { rel: 'icon', type: 'image/png', sizes: '96x96', href: '/favicons/favicon-96x96.png' } },
        { tag: 'link', attrs: { rel: 'apple-touch-icon', sizes: '180x180', href: '/favicons/apple-touch-icon.png' } },
      ],
      social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/kurrent-io/gaffer' }],
      sidebar: [
        {
          label: 'Getting started',
          items: ['getting-started', 'getting-started/first-projection', 'getting-started/debugging'],
        },
        {
          label: 'CLI',
          items: ['cli', 'cli/commands', 'cli/gaffer-toml'],
        },
        { label: 'VS Code extension', items: ['extension'] },
        { label: 'Testing', items: ['testing'] },
        { label: 'MCP', items: ['mcp'] },
      ],
    }),
  ],
  markdown: {
    rehypePlugins: [[rehypeAstroRelativeMarkdownLinks, { collectionBase: false, trailingSlash: 'always' }]],
  },
});
