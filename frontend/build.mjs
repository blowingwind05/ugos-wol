import { build } from 'esbuild';
import { mkdir, copyFile, readdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = path.dirname(fileURLToPath(import.meta.url));
const iconFiles = (await readdir(path.join(root,'icons'))).filter(file => file.endsWith('.svg')).sort();
const symbols = await Promise.all(iconFiles.map(async file => {
  const source = await readFile(path.join(root,'icons',file),'utf8');
  const [,name,size] = file.match(/^(.+)_(\d+)_regular\.svg$/);
  const body = source.replace(/<svg[^>]*>/,'').replace('</svg>','');
  return `<symbol id="${name}-${size}" viewBox="0 0 ${size} ${size}">${body}</symbol>`;
}));
const sprite = `<svg xmlns="http://www.w3.org/2000/svg">${symbols.join('')}</svg>`;
const destinations = [path.join(root,'../backend/web'),path.join(root,'../packaging/rootfs_common/www')];
for (const destination of destinations) {
  await mkdir(destination,{recursive:true});
  await writeFile(path.join(destination,'icons.svg'),sprite);
  for (const file of ['index.html','style.css','icon.svg']) await copyFile(path.join(root,file),path.join(destination,file));
  await build({entryPoints:[path.join(root,'app.js')],outfile:path.join(destination,'app.js'),bundle:true,minify:true,target:'es2020',format:'esm',legalComments:'eof'});
}
