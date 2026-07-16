#!/usr/bin/env node
'use strict';

/*
 * pr-walkthrough — generate a guided HTML walkthrough of a diff.
 *
 * Pipeline: collect diff (gh pr / git / patch file) -> parse into files+hunks
 * -> ask Claude (headless `claude -p`) to cluster changes into intent cohorts
 * and order them (schema -> backend -> api -> ui -> tests) -> render a
 * self-contained HTML page with a step-by-step viewer.
 */

const { execFileSync, spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

// ---------------------------------------------------------------- CLI args

function usage(code = 0) {
  console.log(`
pr-walkthrough — turn a diff into a guided HTML walkthrough

USAGE
  pr-walkthrough [PR_NUMBER] [options]

SOURCES (first match wins)
  PR_NUMBER            Fetch that PR's diff + metadata via \`gh\`
  --diff <file>        Read a unified diff / .patch file
  --base <ref>         Diff <ref>...HEAD in the repo (default when no PR given;
                       base auto-detected from origin/HEAD, then main/master)

OPTIONS
  -C <dir>             Repo directory to operate in (default: cwd)
  --out <file>         Output HTML path (default: ./walkthrough-<name>.html)
  --model <model>      Model for \`claude -p\` (default: sonnet)
  --mock               Skip the LLM; group files heuristically (for testing)
  --no-open            Don't auto-open the HTML when done
  -h, --help           Show this help
`.trim());
  process.exit(code);
}

function parseArgs(argv) {
  const opts = {
    pr: null, diffFile: null, base: null, repo: process.cwd(),
    out: null, model: 'sonnet', mock: false, open: true,
  };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const next = () => {
      if (i + 1 >= argv.length) fail(`missing value for ${a}`);
      return argv[++i];
    };
    if (a === '-h' || a === '--help') usage();
    else if (a === '--diff') opts.diffFile = next();
    else if (a === '--base') opts.base = next();
    else if (a === '-C') opts.repo = path.resolve(next());
    else if (a === '--out') opts.out = next();
    else if (a === '--model') opts.model = next();
    else if (a === '--mock') opts.mock = true;
    else if (a === '--no-open') opts.open = false;
    else if (/^\d+$/.test(a) && opts.pr === null) opts.pr = a;
    else fail(`unknown argument: ${a} (see --help)`);
  }
  return opts;
}

function fail(msg) {
  console.error(`pr-walkthrough: ${msg}`);
  process.exit(1);
}

function log(msg) {
  console.error(`  ${msg}`);
}

// ------------------------------------------------------------ diff sources

function run(cmd, args, cwd, input) {
  const res = spawnSync(cmd, args, {
    cwd, input, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024,
  });
  if (res.error) throw res.error;
  if (res.status !== 0) {
    const err = new Error((res.stderr || '').trim() || `${cmd} exited ${res.status}`);
    err.stdout = res.stdout;
    throw err;
  }
  return res.stdout;
}

function detectBase(repo) {
  try {
    const ref = run('git', ['symbolic-ref', '--quiet', 'refs/remotes/origin/HEAD'], repo).trim();
    if (ref) return ref.replace('refs/remotes/', '');
  } catch { /* fall through */ }
  for (const cand of ['origin/main', 'origin/master', 'main', 'master']) {
    try {
      run('git', ['rev-parse', '--verify', '--quiet', cand], repo);
      return cand;
    } catch { /* try next */ }
  }
  fail('could not auto-detect a base branch; pass --base <ref>');
}

function collectSource(opts) {
  if (opts.pr) {
    log(`fetching PR #${opts.pr} via gh ...`);
    const metaRaw = run('gh', ['pr', 'view', opts.pr, '--json',
      'number,title,body,baseRefName,headRefName,url'], opts.repo);
    const meta = JSON.parse(metaRaw);
    const diff = run('gh', ['pr', 'diff', opts.pr], opts.repo);
    return {
      diff,
      title: `PR #${meta.number}: ${meta.title}`,
      description: meta.body || '',
      range: `${meta.baseRefName} ← ${meta.headRefName}`,
      url: meta.url,
      name: `pr-${meta.number}`,
    };
  }
  if (opts.diffFile) {
    log(`reading diff from ${opts.diffFile} ...`);
    const diff = fs.readFileSync(opts.diffFile, 'utf8');
    return {
      diff,
      title: path.basename(opts.diffFile),
      description: '',
      range: opts.diffFile,
      url: null,
      name: path.basename(opts.diffFile).replace(/\.\w+$/, ''),
    };
  }
  // git branch mode
  const base = opts.base || detectBase(opts.repo);
  log(`diffing ${base}...HEAD in ${opts.repo} ...`);
  let diff = run('git', ['diff', `${base}...HEAD`], opts.repo);
  let range = `${base}...HEAD`;
  if (!diff.trim()) {
    log(`no committed changes vs ${base}; falling back to uncommitted changes (git diff HEAD)`);
    diff = run('git', ['diff', 'HEAD'], opts.repo);
    range = 'HEAD (uncommitted)';
  }
  let branch = 'HEAD';
  try { branch = run('git', ['rev-parse', '--abbrev-ref', 'HEAD'], opts.repo).trim(); } catch { /* keep HEAD */ }
  const repoName = path.basename(opts.repo);
  return {
    diff,
    title: `${repoName}: ${branch} vs ${base}`,
    description: '',
    range,
    url: null,
    name: `${repoName}-${branch}`.replace(/[^\w.-]+/g, '-'),
  };
}

// ------------------------------------------------------------- diff parser

function parseDiff(text) {
  const files = [];
  let file = null;
  let hunk = null;
  let oldNo = 0;
  let newNo = 0;

  const pushFile = () => { if (file) files.push(file); file = null; hunk = null; };

  for (const line of text.split('\n')) {
    const m = line.match(/^diff --git (?:"?a\/(.*?)"?) (?:"?b\/(.*?)"?)$/);
    if (m) {
      pushFile();
      file = {
        oldPath: m[1], newPath: m[2], path: m[2],
        status: 'modified', binary: false, hunks: [],
        additions: 0, deletions: 0,
      };
      continue;
    }
    if (!file) continue;

    if (line.startsWith('new file mode')) { file.status = 'added'; continue; }
    if (line.startsWith('deleted file mode')) { file.status = 'deleted'; file.path = file.oldPath; continue; }
    if (line.startsWith('rename from ')) { file.status = 'renamed'; file.oldPath = line.slice(12); continue; }
    if (line.startsWith('rename to ')) { file.newPath = line.slice(10); file.path = file.newPath; continue; }
    if (line.startsWith('Binary files ') || line === 'GIT binary patch') { file.binary = true; continue; }
    if (line.startsWith('--- ') || line.startsWith('+++ ') || line.startsWith('index ')
      || line.startsWith('old mode') || line.startsWith('new mode')
      || line.startsWith('similarity index') || line.startsWith('dissimilarity index')) continue;

    const h = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/);
    if (h) {
      oldNo = parseInt(h[1], 10);
      newNo = parseInt(h[2], 10);
      hunk = { header: h[3].trim(), lines: [] };
      file.hunks.push(hunk);
      continue;
    }
    if (!hunk) continue;

    if (line.startsWith('+')) {
      hunk.lines.push({ t: 'a', n: [null, newNo++], s: line.slice(1) });
      file.additions++;
    } else if (line.startsWith('-')) {
      hunk.lines.push({ t: 'd', n: [oldNo++, null], s: line.slice(1) });
      file.deletions++;
    } else if (line.startsWith(' ') || line === '') {
      hunk.lines.push({ t: 'c', n: [oldNo++, newNo++], s: line.slice(1) });
    } else if (line.startsWith('\\')) {
      // "\ No newline at end of file" — ignore
    }
  }
  pushFile();
  return files;
}

// ------------------------------------------------------------- LLM analysis

const LAYERS = ['schema', 'backend', 'api', 'ui', 'tests', 'config', 'docs', 'other'];

function fileDiffText(file, cap) {
  if (file.binary) return '(binary file)';
  let out = '';
  for (const h of file.hunks) {
    out += `@@ ${h.header}\n`;
    for (const l of h.lines) {
      out += (l.t === 'a' ? '+' : l.t === 'd' ? '-' : ' ') + l.s + '\n';
    }
  }
  if (out.length > cap) out = out.slice(0, cap) + '\n... [truncated]\n';
  return out;
}

function buildPrompt(source, files) {
  const TOTAL_CAP = 160_000;
  const perFileCap = Math.max(1_500, Math.floor(TOTAL_CAP / Math.max(files.length, 1)));

  let filesBlock = '';
  files.forEach((f, i) => {
    filesBlock += `\n===== FILE ${i}: ${f.path} (${f.status}, +${f.additions}/-${f.deletions}) =====\n`;
    filesBlock += fileDiffText(f, Math.min(perFileCap, 12_000));
  });

  return `You are an expert code-review guide. Analyze this diff and organize it into a guided walkthrough for a human reviewer.

CHANGE: ${source.title}
${source.description ? `DESCRIPTION:\n${source.description.slice(0, 3000)}\n` : ''}
FILES CHANGED: ${files.length}
${filesBlock}

Group the changed files into intent-based cohorts (a cohort = a set of files serving one purpose, e.g. "Add expiry column to sessions table" or "New retry logic in the sync worker"). Order cohorts in the most logical reading order for a reviewer — typically foundation first: schema/data model → backend logic → API surface → UI → tests → config/docs. Every file index must appear in exactly one cohort.

Respond with ONLY a JSON object (no markdown fences, no prose) in exactly this shape:
{
  "title": "short human title for the overall change",
  "overview": "2-4 sentence plain-language summary of what this change accomplishes and how the pieces fit together",
  "mermaid": "a small mermaid 'graph LR' or 'graph TD' diagram showing how the main changed pieces relate (or null if the change is too trivial to diagram)",
  "cohorts": [
    {
      "title": "short cohort title",
      "layer": "one of: ${LAYERS.join(' | ')}",
      "intent": "one sentence: what this group of changes is for",
      "narrative": "2-5 sentences walking the reviewer through this cohort: what changed, why, and what to pay attention to. Reference concrete symbols/functions where helpful.",
      "files": [0, 2],
      "fileSummaries": ["one-line summary of file 0's change", "one-line summary of file 2's change"],
      "reviewNotes": ["specific things worth double-checking, potential risks, or empty array"]
    }
  ]
}`;
}

function callClaude(prompt, model, attempt = 1) {
  log(`analyzing with claude (${model}) — this can take a minute or two ...`);
  let out;
  try {
    out = run('claude', ['-p', '--model', model, '--output-format', 'json'],
      process.cwd(), prompt);
  } catch (e) {
    fail(`claude CLI failed: ${e.message}`);
  }
  // `--output-format json` shape varies by CLI version: a single result
  // object, an array of events ending in a result event, or a bare string.
  let text = out;
  try {
    const parsed = JSON.parse(out);
    if (typeof parsed === 'string') {
      text = parsed;
    } else if (Array.isArray(parsed)) {
      const res = parsed.find((e) => e && e.type === 'result');
      if (res && res.is_error) fail(`claude returned an error: ${String(res.result).slice(0, 300)}`);
      if (res && typeof res.result === 'string') text = res.result;
    } else if (parsed && typeof parsed.result === 'string') {
      text = parsed.result;
    }
  } catch { /* treat stdout as plain text */ }

  const result = extractJson(text);
  if (result) return result;
  if (attempt < 2) {
    log('model returned invalid JSON; retrying once ...');
    return callClaude(
      `${prompt}\n\nIMPORTANT: your previous attempt was NOT valid JSON (likely an unescaped double quote or raw newline inside a string value). Escape all double quotes inside string values as \\" and all newlines as \\n.`,
      model, attempt + 1,
    );
  }
  fail(`model returned invalid JSON after ${attempt} attempts:\n${text.slice(0, 500)}`);
}

function extractJson(text) {
  const fenced = text.match(/```(?:json)?\s*([\s\S]*?)```/);
  if (fenced) text = fenced[1];
  const start = text.indexOf('{');
  const end = text.lastIndexOf('}');
  if (start === -1 || end <= start) return null;
  const slice = text.slice(start, end + 1);
  try {
    return JSON.parse(slice);
  } catch {
    try {
      return JSON.parse(repairJson(slice));
    } catch {
      return null;
    }
  }
}

// Best-effort fix for the most common LLM JSON mistakes: unescaped double
// quotes and raw newlines/tabs inside string values. A quote inside a string
// is treated as a closing quote only if the next non-space char is
// structural (, } ] :) — otherwise it gets escaped.
function repairJson(s) {
  let out = '';
  let inStr = false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (!inStr) {
      if (c === '"') inStr = true;
      out += c;
      continue;
    }
    if (c === '\\') { out += c + (s[i + 1] ?? ''); i++; continue; }
    if (c === '\n') { out += '\\n'; continue; }
    if (c === '\r') { continue; }
    if (c === '\t') { out += '\\t'; continue; }
    if (c === '"') {
      let j = i + 1;
      while (j < s.length && /\s/.test(s[j])) j++;
      const nxt = s[j];
      if (j >= s.length || nxt === ',' || nxt === '}' || nxt === ']' || nxt === ':') {
        inStr = false;
        out += c;
      } else {
        out += '\\"';
      }
      continue;
    }
    out += c;
  }
  return out;
}

function mockAnalysis(source, files) {
  const layerOf = (p) => {
    const l = p.toLowerCase();
    if (/migration|schema|\.sql$|models?\//.test(l)) return 'schema';
    if (/test|spec|__tests__|\.test\.|\.spec\./.test(l)) return 'tests';
    if (/routes?|api|controller|endpoint|graphql|resolver/.test(l)) return 'api';
    if (/component|page|view|\.css|\.scss|\.html$|frontend|ui\/|\.tsx$|\.jsx$|\.vue$/.test(l)) return 'ui';
    if (/\.(json|ya?ml|toml|ini|env|cfg)$|dockerfile|makefile|\.github\//.test(l)) return 'config';
    if (/\.(md|rst|txt)$|docs?\//.test(l)) return 'docs';
    return 'backend';
  };
  const groups = new Map();
  files.forEach((f, i) => {
    const layer = layerOf(f.path);
    if (!groups.has(layer)) groups.set(layer, []);
    groups.get(layer).push(i);
  });
  const cohorts = LAYERS.filter((l) => groups.has(l)).map((layer) => ({
    title: `${layer[0].toUpperCase()}${layer.slice(1)} changes`,
    layer,
    intent: `[mock] Files grouped heuristically as ${layer}.`,
    narrative: '[mock mode] This grouping was produced by path heuristics, not the LLM. Run without --mock for real analysis.',
    files: groups.get(layer),
    fileSummaries: groups.get(layer).map((i) => `[mock] ${files[i].status}, +${files[i].additions}/-${files[i].deletions}`),
    reviewNotes: [],
  }));
  return {
    title: `[MOCK] ${source.title}`,
    overview: 'Mock analysis: files were grouped by path heuristics only. Re-run without --mock to get a real semantic walkthrough.',
    mermaid: 'graph LR\n  A[Mock mode] --> B[No LLM analysis]',
    cohorts,
  };
}

function validateAnalysis(analysis, files) {
  if (!analysis || !Array.isArray(analysis.cohorts) || analysis.cohorts.length === 0) {
    fail('model output missing cohorts');
  }
  const seen = new Set();
  for (const c of analysis.cohorts) {
    c.files = (Array.isArray(c.files) ? c.files : [])
      .filter((i) => Number.isInteger(i) && i >= 0 && i < files.length && !seen.has(i));
    c.files.forEach((i) => seen.add(i));
    if (!LAYERS.includes(c.layer)) c.layer = 'other';
    c.fileSummaries = Array.isArray(c.fileSummaries) ? c.fileSummaries : [];
    c.reviewNotes = Array.isArray(c.reviewNotes) ? c.reviewNotes : [];
  }
  analysis.cohorts = analysis.cohorts.filter((c) => c.files.length > 0);
  const leftovers = files.map((_, i) => i).filter((i) => !seen.has(i));
  if (leftovers.length) {
    analysis.cohorts.push({
      title: 'Other changes', layer: 'other',
      intent: 'Files the analysis did not assign to a cohort.',
      narrative: '', files: leftovers, fileSummaries: [], reviewNotes: [],
    });
  }
  return analysis;
}

// ------------------------------------------------------------- HTML render

function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function renderHtml(source, files, analysis) {
  const payload = JSON.stringify({ source: { title: source.title, range: source.range, url: source.url }, files, analysis })
    .replace(/</g, '\\u003c');
  const template = fs.readFileSync(path.join(__dirname, 'template.html'), 'utf8');
  return template
    .replace('__TITLE__', esc(analysis.title || source.title))
    .replace('"__DATA__"', payload);
}

// ------------------------------------------------------------------- main

function main() {
  const opts = parseArgs(process.argv.slice(2));
  const source = collectSource(opts);
  if (!source.diff.trim()) fail('diff is empty — nothing to walk through');

  const files = parseDiff(source.diff);
  if (files.length === 0) fail('could not parse any files from the diff');
  log(`parsed ${files.length} changed file(s)`);

  const analysis = validateAnalysis(
    opts.mock ? mockAnalysis(source, files) : callClaude(buildPrompt(source, files), opts.model),
    files,
  );
  log(`organized into ${analysis.cohorts.length} cohort(s)`);

  const outPath = path.resolve(opts.out || `walkthrough-${source.name}.html`);
  fs.writeFileSync(outPath, renderHtml(source, files, analysis));
  console.error(`\n  wrote ${outPath}`);

  if (opts.open && process.platform === 'darwin') {
    try { execFileSync('open', [outPath]); } catch { /* non-fatal */ }
  }
}

main();
