// @ts-check
import { defineConfig, sessionDrivers } from 'astro/config';
import starlight from '@astrojs/starlight';
import cloudflare from '@astrojs/cloudflare';
import rehypeAstroRelativeMarkdownLinks from 'astro-rehype-relative-markdown-links';
import starlightLlmsTxt from 'starlight-llms-txt';

// Canonical URLs, sitemap, and og:url use `site`. Staging deploys
// emit their own absolute URLs so an indexed staging page doesn't
// point at production. Defaults to prod for local builds and CI
// production deploys.
const site =
  process.env.CLOUDFLARE_ENV === 'staging'
    ? 'https://gaffer-docs-staging.kurrent.workers.dev'
    : 'https://gaffer.kurrent.io';

export default defineConfig({
  site,
  output: 'static',
  // The site is fully static, so sessions are never used. Pin the
  // driver to an in-memory LRU so @astrojs/cloudflare doesn't fall
  // back to its KV session binding, which would make Wrangler try
  // to auto-provision a SESSION KV namespace at deploy time.
  session: { driver: sessionDrivers.lruCache() },
  adapter: cloudflare({
    imageService: 'compile',
    prerenderEnvironment: 'node',
  }),
  integrations: [
    starlight({
      title: 'Gaffer',
      plugins: [
        starlightLlmsTxt({
          description:
            'Gaffer is the developer toolkit for KurrentDB projections - scaffold, run, debug, and test the same JavaScript projection engine that ships inside KurrentDB, locally.',
        }),
      ],
      components: {
        Hero: './src/components/Hero.astro',
        Head: './src/components/Head.astro',
        Footer: './src/components/Footer.astro',
      },
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
      social: [
        { icon: 'npm', label: 'npm', href: 'https://www.npmjs.com/package/@kurrent/gaffer' },
        { icon: 'github', label: 'GitHub', href: 'https://github.com/kurrent-io/gaffer' },
      ],
      sidebar: [
        {
          label: 'Getting started',
          items: [
            'getting-started/install',
            'getting-started/first-projection',
            'getting-started/debugging',
          ],
        },
        {
          label: 'Gaffer CLI',
          items: ['cli', 'cli/mcp', 'cli/commands', 'cli/gaffer-toml'],
        },
        { label: 'Editor extensions', items: ['extension/vs-code', 'extension/other-editors'] },
        { label: 'Testing', items: ['testing/nodejs'] },
        { label: 'Telemetry', slug: 'telemetry' },
      ],
    }),
  ],
  markdown: {
    rehypePlugins: [[rehypeAstroRelativeMarkdownLinks, { collectionBase: false, trailingSlash: 'always' }]],
  },
});
