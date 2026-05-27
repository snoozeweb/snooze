// Copies the OpenAPI spec and the Redoc standalone bundle into static/ so the
// hand-written static/api/index.html can render the API reference entirely in
// the browser. Run automatically via the prebuild/prestart npm lifecycle.
//
// Redoc is rendered statically (not through Docusaurus SSR) because redoc 2.x
// crashes during server-side rendering under Docusaurus 3 + React 19.
import {copyFileSync, mkdirSync} from 'node:fs';
import {createRequire} from 'node:module';

const require = createRequire(import.meta.url);

mkdirSync('static/api', {recursive: true});
copyFileSync('../api/openapi.yaml', 'static/openapi.yaml');
copyFileSync(
  require.resolve('redoc/bundles/redoc.standalone.js'),
  'static/api/redoc.standalone.js',
);
console.log('Copied OpenAPI spec + Redoc bundle into static/');
