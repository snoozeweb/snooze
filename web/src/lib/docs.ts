// Links from the SPA into the published documentation site.
// Docusaurus is served at https://snoozeweb.github.io/snooze/ with
// routeBasePath "/" over docs/content/, so a page's route slug is its path
// under docs/content without the .md extension
// (e.g. "general/integrations/grafana").
const DOCS_BASE_URL = "https://snoozeweb.github.io/snooze";

/**
 * docsUrl builds an absolute URL to a documentation page from its route slug.
 * A leading slash on the slug is tolerated.
 */
export function docsUrl(slug: string): string {
  return `${DOCS_BASE_URL}/${slug.replace(/^\/+/, "")}`;
}
