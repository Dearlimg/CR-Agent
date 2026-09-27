"""Consolidate all key facts of the 2026-09-27 badcase rerun into ONE file, so the
analysis rests on a single self-consistent snapshot instead of scattered tool output.

Reads (benchmarks/results/remote-20260927-1051/):
  job-ids.txt            case=jobid mapping written by the submit script
  case-<id>-create.json  creation responses
  case-<id>.json         final job JSONs
  trace-details/<jobid>  merged tool-event details

Writes consolidated-summary.json next to them and prints it.
"""
import glob
import json
import os
import re

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.normpath(os.path.join(HERE, "..", "results", "remote-20260927-1051"))
SUMMARY = os.path.join(OUT, "consolidated-summary.json")


def load(path):
    with open(path, encoding="utf-8-sig") as f:  # PowerShell writes UTF-8 BOM
        return json.load(f)


result = {"ids_file": {}, "cases": {}, "trace": {}}

# 1. id mapping as written by the submit script
with open(os.path.join(OUT, "job-ids.txt"), encoding="utf-8-sig") as f:
    for line in f:
        line = line.strip()
        if "=" in line:
            k, v = line.split("=", 1)
            result["ids_file"][k] = v

# 2. per-case: id from create-response vs ids-file, status, comments
for fp in sorted(glob.glob(os.path.join(OUT, "case-*.json"))):
    cid = os.path.basename(fp)
    j = load(fp)
    entry = {
        "file": cid,
        "create_job_id": (load(fp.replace(".json", "-create.json")) or {}).get("id"),
        "ids_file_job_id": result["ids_file"].get(cid.replace(".json", "")),
        "job_json_id": j.get("id"),
        "status": j.get("status"),
        "n_comments": len(j.get("comments") or []),
        "comments": [],
    }
    for c in j.get("comments") or []:
        entry["comments"].append({
            "status": c.get("verification_status") or c.get("status"),
            "path": c.get("path") or c.get("file"),
            "line": c.get("line"),
            "body_head": (c.get("body") or c.get("text") or "")[:400],
        })
    result["cases"][cid] = entry

# 3. trace per job: event count, tool histogram, REAL GitHub-failure residue
gh_fail = re.compile(
    r"HTTP 40[13]\b|rate limit exceeded|Bad credentials|Requires authentication", re.I)
for fp in sorted(glob.glob(os.path.join(OUT, "trace-details", "*"))):
    jid = os.path.basename(fp)
    d = load(fp)
    evs = d.get("details") or d.get("events") or {}
    items = evs.values() if isinstance(evs, dict) else evs
    tools = {}
    fails = []
    for e in items:
        t = e.get("tool") or "?"
        tools[t] = tools.get(t, 0) + 1
        blob = json.dumps(e.get("output"), ensure_ascii=False)
        if gh_fail.search(blob):
            fails.append({"tool": t, "match": gh_fail.search(blob).group(0)})
    result["trace"][jid] = {
        "events": len(evs) if not isinstance(evs, dict) else len(evs),
        "tools": tools,
        "github_failures": fails,
    }

with open(SUMMARY, "w", encoding="utf-8") as f:
    json.dump(result, f, ensure_ascii=False, indent=1)
print(json.dumps(result, ensure_ascii=False, indent=1))
print("WROTE", SUMMARY)
