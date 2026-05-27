import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

const config: Config = {
  title: 'SnoozeWeb',
  tagline: 'Clustered log aggregation and alerting',
  favicon: 'img/logo.png',

  future: {
    v4: true,
  },

  // Production URL. Project page on GitHub Pages: snoozeweb.github.io/snooze/
  url: 'https://snoozeweb.github.io',
  baseUrl: '/snooze/',

  organizationName: 'snoozeweb',
  projectName: 'snooze',

  // Strict by design: these are the migration safety net. A missed Sphinx
  // cross-reference (:ref:/:doc:) becomes a broken link/anchor and fails the build.
  onBrokenLinks: 'throw',
  onBrokenAnchors: 'throw',

  markdown: {
    // Parse .md as CommonMark (raw HTML passes through) rather than MDX. The
    // migrated pages contain HTML tables (pandoc fallback for code-in-cells)
    // and prose like `<token>`/`{braces}` that MDX/JSX would choke on.
    // Admonitions and relative links still work in this mode.
    format: 'detect',
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },

  themes: [
    [
      // Offline, no Algolia account required (matches the GitHub Pages choice).
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        indexBlog: false,
        docsRouteBasePath: '/',
        highlightSearchTermsOnTargetPage: true,
      },
    ],
  ],

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          path: 'content',
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl: 'https://github.com/snoozeweb/snooze/tree/master/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/logo.png',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'SnoozeWeb',
      logo: {
        alt: 'SnoozeWeb',
        src: 'img/logo.png',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        // Static Redoc page under static/api/ (rendered in-browser, see
        // scripts/copy-api-assets.mjs). pathname:// links to a static file.
        {to: 'pathname:///api/', label: 'API', position: 'left'},
        {
          href: 'https://github.com/snoozeweb/snooze',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Getting started', to: '/getting_started/'},
            {label: 'Configuration', to: '/configuration/'},
            {label: 'API reference', to: 'pathname:///api/'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/snoozeweb/snooze'},
            {label: 'Issues', href: 'https://github.com/snoozeweb/snooze/issues'},
            {label: 'Live demo', href: 'https://try.snoozeweb.net'},
          ],
        },
      ],
      copyright: `Snooze is licensed under the AGPL-3.0-or-later. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: [
        'bash',
        'yaml',
        'json',
        'go',
        'toml',
        'ini',
        'nginx',
        'sql',
        'docker',
        'python',
      ],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
