"""Patch AquaNY's current JS in place, preserving existing measurement display code."""
from pathlib import Path
import shutil,sys
root=Path(__file__).resolve().parent
# Put this file at water-api repository root and run from there.
info_url='/info.html'  # Change to /info if your Go router serves html/info.html there.
map_codes={
'00010':'water-temperature','00011':'water-temperature','00060':'streamflow','00061':'streamflow','00065':'gage-height','00095':'specific-conductance','00300':'dissolved-oxygen','00400':'ph','63680':'turbidity','00076':'turbidity','00480':'salinity','90860':'salinity','62619':'water-surface-elevation','62620':'water-surface-elevation',
}
common=f'''\n// AquaNY contextual measurement links: values navigate to the matching guide section.\nconst AQUANY_INFO_BASE = "{info_url}";\nfunction aquanyInfoLink(valueNode, sectionId) {{\n  if (!sectionId) return valueNode;\n  const anchor = document.createElement("a");\n  anchor.href = `${{AQUANY_INFO_BASE}}#${{sectionId}}`;\n  anchor.className = "aquany-reading-link";\n  anchor.title = "Understand this measurement";\n  anchor.setAttribute("aria-label", `Learn about ${{sectionId.replaceAll('-', ' ')}}: ${{valueNode.textContent}}`);\n  anchor.append(valueNode);\n  return anchor;\n}}\n'''
map_helper=common+'''const AQUANY_USGS_GUIDE = '''+repr(map_codes).replace("'",'"')+''';\n'''
wb_helper=common+'''const AQUANY_USGS_GUIDE = '''+repr(map_codes).replace("'",'"')+''';\n'''
dw_helper=common+'''const AQUANY_CONTAMINANT_GUIDE = {"PFOA":"pfoa","PFOS":"pfos","Nitrate":"nitrate","Arsenic":"arsenic"};\n'''

def insert_marker(src, marker, helper):
    if 'function aquanyInfoLink(' in src: return src
    if marker not in src: raise RuntimeError(f'Cannot find expected insertion marker: {marker}')
    return src.replace(marker,helper+'\n'+marker,1)

def patch_map(s):
    s=insert_marker(s,'function getParameterName(',map_helper)
    target='row.append(label, value, timestamp);'
    if target not in s: raise RuntimeError('Cannot find map popup value row append')
    s=s.replace(target,'''const guideSection = AQUANY_USGS_GUIDE[String(measurement.parameterCode ?? "").trim()];
      row.append(label, aquanyInfoLink(value, guideSection), timestamp);''',1)
    return s

def patch_wb(s):
    s=insert_marker(s,'function metricCard(',wb_helper)
    start=s.index('function metricCard(')
    end=s.index('function renderHistory(',start)
    chunk=s[start:end]
    target='card.append(output);'
    if target not in chunk: raise RuntimeError('Cannot find current-condition output')
    chunk=chunk.replace(target,'''card.append(aquanyInfoLink(output, AQUANY_USGS_GUIDE[String(reading.parameterCode ?? "").trim()]));''',1)
    s=s[:start]+chunk+s[end:]
    start=s.index('function renderHistory(')
    end=s.index('function renderConditions(',start)
    chunk=s[start:end]
    # New format uses strong output; old format uses single span value.
    if 'card.append(output);' in chunk:
        chunk=chunk.replace('card.append(output);','''card.append(aquanyInfoLink(output, AQUANY_USGS_GUIDE[String(reading.parameterCode ?? "").trim()]));''',1)
    elif 'card.append(elt(\'span\',' in chunk:
        chunk=chunk.replace('card.append(elt(\'span\', `${value}${reading.unit ? ` ${reading.unit}` : \'\'}`));', '''card.append(aquanyInfoLink(elt('span', `${value}${reading.unit ? ` ${reading.unit}` : ''}`), AQUANY_USGS_GUIDE[String(reading.parameterCode ?? "").trim()]));''',1)
    else: raise RuntimeError('Cannot find history output; edit manually')
    s=s[:start]+chunk+s[end:]
    return s

def patch_dw(s):
    s=insert_marker(s,'function recordCard(',dw_helper)
    start=s.index('function recordCard(')
    end=s.index('function summaryTable(',start)
    chunk=s[start:end]
    # Modern dw-card-stats rows: append the value first, then wrap in an anchor.
    if 'stats.append(row);' in chunk:
        chunk=chunk.replace('stats.append(row);','''const valueElement = row.lastElementChild;
    const guideSection = AQUANY_CONTAMINANT_GUIDE[label] || 'other-contaminants';
    if (valueElement) valueElement.replaceWith(aquanyInfoLink(valueElement, guideSection));
    stats.append(row);''',1)
    else:
        # Old style: link Mean/Maximum div only when a record exists.
        chunk=chunk.replace('''  // Show the reporting period separately.''','''  // Click mean or maximum to learn about this contaminant.
  for (const valueNode of card.querySelectorAll('.dw-value')) {
    if (valueNode.textContent?.startsWith('Mean:')) {
      valueNode.replaceWith(aquanyInfoLink(valueNode, AQUANY_CONTAMINANT_GUIDE[label] || 'other-contaminants'));
    }
  }
  // Show the reporting period separately.''',1)
    s=s[:start]+chunk+s[end:]
    return s

files={
'js/map.js':patch_map,
'js/waterbodyConditions.js':patch_wb,
'js/drinkingWater.js':patch_dw,
}
outputs={}
try:
    for name,fn in files.items():
        p=root/name
        if not p.exists(): raise RuntimeError(f'Missing {name}. Put this script in the water-api project root.')
        old=p.read_text(encoding='utf-8-sig')
        if 'function aquanyInfoLink(' in old:
            print(f'Already patched: {name}')
            continue
        outputs[p]=(old,fn(old))
except Exception as e:
    print('No files changed:',e,file=sys.stderr)
    sys.exit(1)
for p,(old,new) in outputs.items():
    backup=p.with_suffix(p.suffix+'.before-info-links.bak')
    if not backup.exists(): shutil.copy2(p,backup)
    p.write_text(new,encoding='utf-8')
    print('Patched',p.relative_to(root),'(backup:',backup.name+')')
css=root/'static/css/mappage.css'
if css.exists():
    t=css.read_text(encoding='utf-8-sig')
    if '.aquany-reading-link' not in t:
        css.write_text(t+'''\n/* Subtle contextual links on measurement values */\na.aquany-reading-link { display: inline-block; color: inherit; text-decoration: none; cursor: pointer; border-radius: 3px; }\na.aquany-reading-link:hover { text-decoration: underline; text-decoration-thickness: 1px; text-underline-offset: 3px; }\na.aquany-reading-link:focus-visible { outline: 2px solid #168bad; outline-offset: 3px; }\n''',encoding='utf-8')
        print('Added map/river reading links CSS')
css=root/'static/css/drinking-water.css'
if css.exists():
    t=css.read_text(encoding='utf-8-sig')
    if '.aquany-reading-link' not in t:
        css.write_text(t+'''\n/* Drinking Water contaminant value links */\na.aquany-reading-link { color: inherit; text-decoration: none; cursor: pointer; border-radius: 3px; }\na.aquany-reading-link:hover { text-decoration: underline; text-underline-offset: 3px; }\na.aquany-reading-link:focus-visible { outline: 2px solid #168bad; outline-offset: 3px; }\n''',encoding='utf-8')
        print('Added drinking water reading links CSS')
print('Done. Confirm that the Go server serves html/info.html at',info_url)
