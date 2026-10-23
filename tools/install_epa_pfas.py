#!/usr/bin/env python3
"""Run from any directory after merging the ZIP files into the water-api repository.
Backs up main.go / waterbodySearch.js before inserting two integration hooks.
"""
from pathlib import Path
import re

root = Path(__file__).resolve().parent.parent.parent
main = root / 'server/main/main.go'
js = root / 'js/waterbodySearch.js'
for p in (main, js, root / 'server/main/epa_pfas.go', root / 'js/waterbodyEPAPFAS.js'):
    if not p.exists():
        raise SystemExit(f'Missing {p}. Extract ZIP into your water-api project root first.')

s = main.read_text(encoding='utf-8')
route = '"GET /api/waterbodies/{id}/epa-pfas"'
if route not in s:
    match = re.search(r'(?m)^[ \t]*// Handlers for page endpoints', s)
    if not match:
        raise SystemExit('Could not locate // Handlers for page endpoints in main.go; add the route manually.')
    pos = match.start()
    s = s[:pos] + 'http.HandleFunc("GET /api/waterbodies/{id}/epa-pfas", WaterbodyEPAPFAS)\n\n\t' + s[pos:]
    backup = main.with_suffix('.go.epa-backup')
    backup.write_text(main.read_text(encoding='utf-8'), encoding='utf-8')
    main.write_text(s, encoding='utf-8')

s = js.read_text(encoding='utf-8')
import_line = 'import { appendWaterbodyEPAPFAS } from "./waterbodyEPAPFAS.js";'
if 'from "./waterbodyEPAPFAS.js"' not in s and "from './waterbodyEPAPFAS.js'" not in s:
    if 'export function initWaterbodySearch' not in s:
        raise SystemExit('Unexpected waterbodySearch.js structure; add the JS import manually.')
    backup = js.with_suffix('.js.epa-backup')
    backup.write_text(s, encoding='utf-8')
    s = import_line + '\n' + s
    js.write_text(s, encoding='utf-8')


s = js.read_text(encoding='utf-8')
if 'appendWaterbodyEPAPFAS(detail, id,' not in s:
    match = re.search(r'(?m)^([ \t]*)appendPFAS\(id, version\);', s)
    if not match:
        raise SystemExit('Could not find appendPFAS(id, version); in waterbodySearch.js; add EPA call manually.')
    pos = match.end()
    s = s[:pos] + '\n' + match.group(1) + 'appendWaterbodyEPAPFAS(detail, id, conditionsController.signal);' + s[pos:]
    js.write_text(s, encoding='utf-8')


print('Done')
