"""Analyze the 2026-09-27 badcase rerun (after GITHUB_TOKEN fix + DeepSeek top-up).

Data: benchmarks/results/remote-20260927-1051/
  - case-<id>.json         final job JSONs (PowerShell-written, UTF-8 with BOM)
  - trace-details/<jobid>  merged tool-event details

Questions:
  1. per-case job shape (status, comments, warnings, cost);
  2. round-2 failure-mode residue: 403/rate-limit, unreadable fixed-commit source,
     truncation — did they converge?
  3. coverage of the 3 audited valid references by published comments.
"""
import glob
import json
import os
import re

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = os.path.normpath(os.path.join(HERE, "..", "results", "remote-20260927-1051"))

# The 3 audited valid references (version-alignment audit, 2026-09-26).
VALID_REFS = {
    "case-22862": ("pandas-22862#0", "copy=False 时内部可变对象未复制"),
    "case-2632":  ("gin-2632#0",     "XFF 应从右向左取第一个可信 IP"),
    "case-41749": ("node-41749#1",   "keep-alive 挂起测试未等待 server close"),
}


def load(path):
    with open(path, encoding="utf-8-sig") as f:  # tolerate PowerShell BOM
        return json.load(f)


# ---- 1. per-case job summary -------------------------------------------------
print("=== JOB SUMMARY ===")
jobs = {}
for fp in sorted(glob.glob(os.path.join(BASE, "case-*.json"))):
    cid = os.path.basename(fp)[:-5]
    j = load(fp)
    jobs[cid] = j
    print(f"{cid}: status={j.get('status')} comments={len(j.get('comments') or [])} "
          f"warnings={j.get('warnings')}")

# ---- 2. trace shape + failure-mode residue -----------------------------------
print("\n=== TRACE DETAILS ===")
total_events = 0
residue = {"403": 0, "rate-limit": 0, "unreadable": 0, "truncated": 0,
           "api.github.com": 0, "raw.githubusercontent.com": 0}
pats = {
    "403": re.compile(r"\b403\b"),
    "rate-limit": re.compile(r"rate limit|限流", re.I),
    "unreadable": re.compile(r"无法读取|unreadable", re.I),
    "truncated": re.compile(r"truncat|截断", re.I),
    "api.github.com": re.compile(r"api\.github\.com"),
    "raw.githubusercontent.com": re.compile(r"raw\.githubusercontent\.com"),
}
for fp in sorted(glob.glob(os.path.join(BASE, "trace-details", "*"))):
    if not os.path.isfile(fp):
        continue
    d = load(fp)
    events = d.get("details") or {}
    n = len(events)
    total_events += n
    txt = json.dumps(d, ensure_ascii=False)
    hits = {k: len(p.findall(txt)) for k, p in pats.items()}
    for k, v in hits.items():
        residue[k] += v
    sample = next(iter(events.values()), {})
    keys = sorted(sample.keys()) if isinstance(sample, dict) else type(sample).__name__
    print(f"{os.path.basename(fp)}: events={n} first-event-keys={keys} residue={hits}")
print(f"total tool events: {total_events}")
print("failure-mode residue (sum):", {k: v for k, v in residue.items()})

# ---- 3. valid-ref coverage by published comments ------------------------------
print("\n=== VALID REF COVERAGE ===")
for cid, (ref, desc) in VALID_REFS.items():
    j = jobs.get(cid)
    if not j:
        print(f"{cid}: JOB MISSING")
        continue
    print(f"--- {cid} ref={ref} ({desc})")
    for i, c in enumerate(j.get("comments") or []):
        body = (c.get("body") or c.get("text") or "")
        status = c.get("verification_status") or c.get("status") or "?"
        path = c.get("path") or c.get("file") or "?"
        line = c.get("line") or c.get("start_line") or "?"
        print(f"  [{i}] status={status} path={path}:{line}")
        print("      " + body[:260].replace("\n", " | "))
