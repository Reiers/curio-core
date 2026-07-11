import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Curio Core',
  description: 'A complete Filecoin Onchain Cloud hot-storage provider in a single binary, under 1 GB.',
  cleanUrls: true,
  lastUpdated: true,
  ignoreDeadLinks: true,

  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' }],
    ['meta', { name: 'theme-color', content: '#0a0c0f' }],
    ['meta', { property: 'og:title', content: 'Curio Core docs' }],
    ['meta', { property: 'og:description', content: 'One-binary Filecoin hot-storage SP. Pure Go, embedded chain node, SQLite.' }],
    ['meta', { property: 'og:type', content: 'website' }],
  ],

  themeConfig: {
    siteTitle: 'Curio Core',
    logo: '/logo.svg',

    nav: [
      { text: 'Home', link: '/' },
      { text: 'Quickstart', link: '/getting-started/quickstart' },
      { text: 'Operate', link: '/operating/dashboard' },
      { text: 'Reference', link: '/reference/cli' },
      { text: 'GitHub', link: 'https://github.com/Reiers/curio-core' },
    ],

    sidebar: [
      {
        text: 'Introduction',
        items: [
          { text: 'What is Curio Core?', link: '/' },
          { text: 'Status & roadmap',    link: '/status' },
        ],
      },
      {
        text: 'Getting started',
        items: [
          { text: 'Quickstart (calibration)', link: '/getting-started/quickstart' },
          { text: 'Install',                  link: '/getting-started/install' },
          { text: 'First-run setup',          link: '/getting-started/first-run' },
          { text: 'Funding the wallet',       link: '/getting-started/funding' },
        ],
      },
      {
        text: 'Concepts',
        items: [
          { text: 'Architecture',          link: '/concepts/architecture' },
          { text: 'Embedded Lantern',      link: '/concepts/lantern' },
          { text: 'PDP proof loop',        link: '/concepts/pdp-proofs' },
          { text: 'USDFC payment rails',   link: '/concepts/payment-rails' },
          { text: 'harmonytask scheduler', link: '/concepts/harmonytask' },
          { text: 'Scale mitigations',    link: '/concepts/scale-mitigations' },
        ],
      },
      {
        text: 'Operate',
        items: [
          { text: 'Dashboard tour',     link: '/operating/dashboard' },
          { text: 'Wallet management',  link: '/operating/wallets' },
          { text: 'Storage paths',      link: '/operating/storage' },
          { text: 'Uploading pieces',   link: '/operating/uploads' },
          { text: 'Settlement & USDFC', link: '/operating/settlement' },
          { text: 'Embedded terminal',  link: '/operating/terminal' },
          { text: 'Monitoring & alerts',link: '/operating/monitoring' },
          { text: 'Backup & restore',   link: '/operating/backup' },
          { text: 'Upgrading',          link: '/operating/upgrading' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'CLI',                  link: '/reference/cli' },
          { text: 'HTTP API',             link: '/reference/http-api' },
          { text: 'Configuration',        link: '/reference/configuration' },
          { text: 'SQLite schema',        link: '/reference/schema' },
          { text: 'Contract addresses',   link: '/reference/contracts' },
          { text: 'Differences vs Curio', link: '/reference/upstream-diff' },
        ],
      },
      {
        text: 'Help',
        items: [
          { text: 'Troubleshooting', link: '/help/troubleshooting' },
          { text: 'FAQ',             link: '/help/faq' },
          { text: 'Contributing',    link: '/help/contributing' },
        ],
      },
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/Reiers/curio-core' },
    ],

    footer: {
      message: 'Released under the Apache 2.0 OR MIT license.',
      copyright: '© 2026 TSE Reiersen',
    },

    search: { provider: 'local' },
  },
})
