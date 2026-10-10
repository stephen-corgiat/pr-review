#!/usr/bin/env python3
"""Rebuild testdata/dispatchly/{pr,briefing,answers,feedback,revisions}.json.

The fixture PR is dispatchly's c1..c2 (testdata/dispatchly/repo). The agent
outputs in these files are written by hand, as a recorded agent would have
answered; this script only resolves every line number from the c2 source by
searching for a marker, so editing the source never leaves a stale anchor.
`go test ./internal/fixtureagent` checks the result against the built repo.

Run from the repo root:  python3 tools/build_fixtures.py
"""
import json
import os
import re
import subprocess

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FX = os.path.join(ROOT, "testdata", "dispatchly")
C1 = os.path.join(FX, "repo", "c1")
C2 = os.path.join(FX, "repo", "c2")

BEACON = "internal/bounce/webhooks/beacon.go"
BOUNCE_CMD = "cmd/bounce.go"
INIT = "cmd/init.go"
SETTINGS_CMD = "cmd/settings.go"
SETTINGS_MODEL = "models/settings.go"
BOUNCE_PKG = "internal/bounce/bounce.go"
MIGRATION = "internal/migrations/v1.3.0.go"
SCHEMA = "schema.sql"
MAIN = "cmd/main.go"
DOCS = "docs/docs/content/bounces.md"

_cache = {}


def lines(path):
    if path not in _cache:
        with open(os.path.join(C2, path)) as f:
            _cache[path] = f.read().split("\n")
    return _cache[path]


def L(path, marker, nth=1):
    """The 1-based line of the nth line in path containing marker."""
    n = 0
    for i, line in enumerate(lines(path), 1):
        if marker in line:
            n += 1
            if n == nth:
                return i
    raise SystemExit("marker %r (#%d) not found in %s" % (marker, nth, path))


def fn(path, signature):
    """(first, last) lines of the func whose declaration contains signature."""
    start = L(path, "func " + signature)
    for i in range(start, len(lines(path)) + 1):
        if lines(path)[i - 1] == "}":
            return start, i
    raise SystemExit("no end for %s in %s" % (signature, path))


def span(path, a, b=None):
    return [a, b if b is not None else a]


def at(path, a, b=None):
    return "%s:%d" % (path, a) if b is None or a == b else "%s:%d-%d" % (path, a, b)


def diff_anchor(path, a, b=None, text=None):
    out = {"type": "diff", "path": path, "side": "new", "lines": span(path, a, b)}
    if text:
        out["text"] = text
    return out


def dump(name, obj):
    with open(os.path.join(FX, name), "w") as f:
        json.dump(obj, f, indent=2, ensure_ascii=False)
        f.write("\n")


# ---- line numbers every part below uses ------------------------------------

new_s, new_e = fn(BEACON, "NewBeacon(")
hs_s, hs_e = fn(BEACON, "(b *Beacon) ProcessHandshake(")
pb_s, pb_e = fn(BEACON, "(b *Beacon) ProcessBounce(")
va_s, va_e = fn(BEACON, "(b *Beacon) verifyAuth(")
se_s, se_e = fn(BEACON, "secretsEqual(")
pe_s, pe_e = fn(BEACON, "parseBeaconEvents(")
ms_s, ms_e = fn(BEACON, "mapBeaconStatus(")
va_early = L(BEACON, 'if b.secret == ""')
dom = L(BEACON, 'strings.Contains(d, "domain")')
code_s = L(BEACON, "smtpCode.FindStringSubmatch")
soft_s = L(BEACON, "range softPhrases")
soft_e = soft_s + 4
map_call = L(BEACON, "mapBeaconStatus(e.Data.Status")
verify_call = L(BEACON, "b.verifyAuth(h, q)")
parse_call = L(BEACON, "parseBeaconEvents(body)")
softphr = L(BEACON, "var softPhrases")

case_s = L(BOUNCE_CMD, 'case "beacon":')
hdr = L(BOUNCE_CMD, "X-Beacon-Event")
hs_branch_s = hdr
hs_branch_e = hdr + 6
proc_call = L(BOUNCE_CMD, "Beacon.ProcessBounce(")
bw_s, bw_e = fn(BOUNCE_CMD, "(a *App) BounceWebhook(")
route = L(MAIN, '"POST /webhooks/bounce/{service}"')
enqueue = L(BOUNCE_CMD, "a.bounce.Enqueue(b)")
body_read = L(BOUNCE_CMD, "io.ReadAll")

ib_s, ib_e = fn(INIT, "initBounceManager(")
ic_s, ic_e = fn(INIT, "initConstConfig(")
const_field = L(INIT, "BounceBeaconEnabled   bool")
const_set = L(INIT, "BounceBeaconEnabled:")
opt_s = L(INIT, "BeaconEnabled:")
opt_e = L(INIT, "BeaconSecretHeader:")

gs_s, gs_e = fn(SETTINGS_CMD, "(a *App) GetSettings(")
us_s, us_e = fn(SETTINGS_CMD, "(a *App) UpdateSettings(")
mask_line = L(SETTINGS_CMD, "s.BounceBeacon.SharedSecret = strings.Repeat")
keep_s = L(SETTINGS_CMD, 'set.BounceBeacon.SharedSecret == ""')
keep_e = keep_s + 2

model_s = L(SETTINGS_MODEL, "BounceBeacon struct")
model_e = L(SETTINGS_MODEL, 'json:"bounce.beacon"')
actions_line = L(SCHEMA, "'bounce.actions'")
schema_beacon = L(SCHEMA, "'bounce.beacon'")
schema_pm = L(SCHEMA, "'bounce.postmark'")
mig_s, mig_e = fn(MIGRATION, "V1_3_0(")
mig_exec = L(MIGRATION, "tx.Exec(")
opt_pkg_s = L(BOUNCE_PKG, "BeaconEnabled      bool")
opt_pkg_e = L(BOUNCE_PKG, "BeaconSecretHeader string")
new_pkg_s, new_pkg_e = fn(BOUNCE_PKG, "New(")
docs_row = L(DOCS, "| Beacon")
cy = "frontend/cypress/e2e/bounces.cy.js"

# ---- pr.json ---------------------------------------------------------------

def tree(root):
    out = {}
    for d, _, names in os.walk(root):
        for n in names:
            full = os.path.join(d, n)
            with open(full, "rb") as f:
                out[os.path.relpath(full, root)] = f.read()
    return out


t1, t2 = tree(C1), tree(C2)
files = sorted(p for p in set(t1) | set(t2) if t1.get(p) != t2.get(p))
numstat = subprocess.run(
    ["git", "diff", "--no-index", "--numstat", "--no-renames", C1, C2], capture_output=True, text=True
).stdout
adds = sum(int(r.split("\t")[0]) for r in numstat.strip().split("\n"))
dels = sum(int(r.split("\t")[1]) for r in numstat.strip().split("\n"))

dump("pr.json", {
    "repo_url": "https://github.com/acme/dispatchly.git",
    "owner_repo": "acme/dispatchly",
    "go_module": "github.com/acme/dispatchly",
    "number": 214,
    "url": "https://github.com/acme/dispatchly/pull/214",
    "title": "Add Beacon bounce webhooks and settings support",
    "author": "casey-dev",
    "head_ref": "beacon-bounce-webhooks",
    "base_ref": "main",
    "created_at": "2026-03-12T09:30:00Z",
    "merged_at": "2026-03-18T15:02:00Z",
    "stats": {"files": len(files), "additions": adds, "deletions": dels},
    "files": files,
    "description": (
        "Beacon Relay is a mail relay with a small webhook API. This adds it as a bounce provider "
        "next to Sendgrid and Postmark.\n\n"
        "Beacon sends a handshake first and then batches of delivery reports. Its docs are thin on "
        "the `messageRef` field (the Message-ID header), so I match bounces by recipient email only. "
        "On my test account that worked.\n\n"
        "Is there anything I should do differently, or explain better in the docs?"
    ),
})

# ---- briefing.json ---------------------------------------------------------

reading = [
    (BEACON, "Core: handshake, shared-secret check, event parsing, status-to-bounce-type mapping"),
    (BOUNCE_CMD, "Routes beacon by the X-Beacon-Event header: handshake vs delivery reports"),
    (BOUNCE_PKG, "New Opt fields and Manager.Beacon wiring"),
    (INIT, "Reads bounce.beacon into Consts and the bounce Opt"),
    (SETTINGS_MODEL, "New BounceBeacon settings struct"),
    (SETTINGS_CMD, "Masks the shared secret on read, keeps it when blank on update"),
    (SCHEMA, "Default bounce.beacon settings row for fresh installs"),
    (MIGRATION, "Upserts the bounce.beacon settings row for existing installs"),
    ("internal/migrations/migrations.go", "Registers the v1.3.0 migration"),
    ("frontend/src/views/settings/bounces.vue", "Beacon enable switch, secret and header inputs"),
    ("i18n/en.json", "Labels and help text for the Beacon settings"),
    (DOCS, "Setup instructions for the Beacon endpoint"),
    ("docs/swagger/collections.yaml", "Adds beacon to the webhook service enum"),
]
assert sorted(p for p, _ in reading) == sorted(files), (sorted(p for p, _ in reading), files)

M = {}


def method(path, key, does, changed):
    M[path + "::" + key] = {"does": does, "changed": changed}


method(BEACON, "NewBeacon", "Builds the handler with a trimmed secret and header name", "New")
method(BEACON, "(*Beacon).ProcessHandshake", "Echoes Beacon's challenge back as JSON", "New; no secret check on this path")
method(BEACON, "(*Beacon).ProcessBounce", "Verifies auth, parses the reports, returns a bounce for each failed one", "New; matches by recipient email only")
method(BEACON, "(*Beacon).verifyAuth", "Accepts the secret in ?code= or in a custom header", "New; returns nil when no secret is configured")
method(BEACON, "secretsEqual", "Constant-time compare, false on empty values", "New")
method(BEACON, "parseBeaconEvents", "Parses the JSON array, keeping each event's raw JSON", "New")
method(BEACON, "mapBeaconStatus", "Maps a report's status and details to hard, soft or complaint", 'New; an unknown "failed" defaults to soft')
method(BOUNCE_CMD, "(*App).BounceWebhook", "HTTP handler that turns a provider's webhook into queued bounces", "New beacon case for the handshake and the reports")
method(INIT, "initBounceManager", "Builds the bounce Opt from the settings", "Passes Beacon enabled, secret and header")
method(INIT, "initConstConfig", "Loads the settings that need a restart", "Adds BounceBeaconEnabled")
method(BOUNCE_PKG, "New", "Creates the bounce Manager and the enabled webhook providers", "Creates the Beacon handler when BeaconEnabled")
method(SETTINGS_CMD, "(*App).GetSettings", "Returns the settings with secrets masked", "Masks the Beacon shared secret")
method(SETTINGS_CMD, "(*App).UpdateSettings", "Saves the settings, restoring blank or masked secrets", "Keeps the stored Beacon secret when blank")
method(MIGRATION, "V1_3_0", "Runs the v1.3.0 upgrade", "New; upserts the bounce.beacon settings row")

dump("briefing.json", {
    "delay_ms": 1500,
    "tldr": (
        'Adds a Beacon Relay bounce webhook at service "beacon": answers the handshake, checks an '
        "optional shared secret, and maps delivery reports to hard, soft or complaint bounces. "
        "Adds settings, a v1.3.0 migration and UI. An empty secret means no auth."
    ),
    "reading_order": [{"path": p, "summary": s} for p, s in reading],
    "methods": M,
    "contract_changes": [
        {
            "category": "endpoint",
            "title": 'Webhook service "beacon" accepted; honours the X-Beacon-Event header and the ?code= secret',
            "detail": "",
            "at": at(BOUNCE_CMD, case_s, proc_call),
            "anchors": [{"path": BOUNCE_CMD, "line": case_s}],
        },
        {
            "category": "config",
            "title": "New setting bounce.beacon {enabled, shared_secret, shared_secret_header}",
            "detail": "",
            "at": at(SETTINGS_MODEL, model_s, model_e),
            "anchors": [{"path": SETTINGS_MODEL, "line": model_s}],
        },
        {
            "category": "schema",
            "title": "Settings row bounce.beacon inserted or rewritten via upsert",
            "detail": "",
            "at": at(MIGRATION, mig_s, mig_e),
            "anchors": [{"path": MIGRATION, "line": mig_s}],
        },
        {
            "category": "signature",
            "title": "Opt gains BeaconEnabled, BeaconSecret, BeaconSecretHeader; Manager gains Beacon",
            "detail": "",
            "at": at(BOUNCE_PKG, opt_pkg_s, opt_pkg_e),
            "anchors": [{"path": BOUNCE_PKG, "line": opt_pkg_s}],
        },
    ],
})

# ---- answers.json ----------------------------------------------------------

PROBE_MAP = (
    "cat > internal/bounce/webhooks/zz_probe_test.go <<'EOF'\n"
    "package webhooks\n\n"
    'import ("fmt"; "testing")\n\n'
    "func TestZZProbe(t *testing.T) {\n"
    "\t// prints mapBeaconStatus for eight status/details pairs\n"
    "}\n"
    "EOF\n"
    "go test -run TestZZProbe -v ./internal/bounce/webhooks/"
)
PROBE_MAP_OUT = (
    '"Complained" ""                               -> "complaint" true\n'
    '"Suppressed" ""                               -> "hard"     true\n'
    '"Failed"     "550 5.1.1 User unknown"         -> "hard"     true\n'
    '"Failed"     "452 4.2.2 Mailbox full"         -> "soft"     true\n'
    '"Failed"     "Mailbox full"                   -> "soft"     true\n'
    '"Failed"     "Sending domain not verified"    -> "hard"     true\n'
    '"Failed"     "Connection timed out"           -> "soft"     true\n'
    '"Delivered"  ""                               -> ""         false'
)
PROBE_HS = (
    "cat > internal/bounce/webhooks/zz_probe_test.go <<'EOF'\n"
    "package webhooks\n\n"
    'import ("fmt"; "net/http"; "net/url"; "testing")\n\n'
    "func TestZZProbe(t *testing.T) {\n"
    '\tb := NewBeacon("s3cret", "X-Beacon-Secret")\n'
    "\t// call ProcessHandshake and ProcessBounce without sending the secret\n"
    "}\n"
    "EOF\n"
    "go test -run TestZZProbe -v ./internal/bounce/webhooks/"
)
PROBE_HS_OUT = (
    "ProcessHandshake without secret: map[challenge:c4f1] err=<nil>\n"
    "ProcessBounce without secret: err=invalid Beacon shared secret"
)
COVER_OUT = (
    "\tgithub.com/acme/dispatchly/cmd\t\tcoverage: 0.0% of statements\n"
    "\tgithub.com/acme/dispatchly/internal/bounce\t\tcoverage: 0.0% of statements\n"
    "\tgithub.com/acme/dispatchly/internal/bounce/webhooks\t\tcoverage: 0.0% of statements\n"
    "\tgithub.com/acme/dispatchly/internal/core\t\tcoverage: 0.0% of statements\n"
    "\tgithub.com/acme/dispatchly/internal/migrations\t\tcoverage: 0.0% of statements\n"
    "?   \tgithub.com/acme/dispatchly/models\t[no test files]"
)


def step(label, where, value, lang, source, ref=None):
    s = {"label": label, "at": where, "value": value, "lang": lang, "source": source}
    if ref:
        s["source_ref"] = ref
    return s


answers = []

answers.append({
    "id": "A01", "kind": "define",
    "anchors": [diff_anchor(BOUNCE_CMD, hdr, hdr, "X-Beacon-Event")],
    "question": "What does “X-Beacon-Event” mean in this context?",
    "seed": "answered_unread",
    "answer": {
        "kind": "define",
        "gist": "An HTTP header Beacon Relay adds to each webhook call. It says whether the POST is the one-time handshake or a batch of delivery reports.",
        "define": {
            "term": "X-Beacon-Event",
            "example": "X-Beacon-Event: handshake -> the handler echoes the challenge; any other value (or none) -> the body is parsed as delivery reports",
        },
        "detail": (
            "The handler branches on it at `%s`:\n"
            "- `handshake` goes to `ProcessHandshake`, which answers without checking the shared secret.\n"
            "- Anything else, including a missing header, goes to `ProcessBounce`, which checks the secret first."
            % at(BOUNCE_CMD, hdr)
        ),
        "citations": [at(BOUNCE_CMD, hdr, hs_branch_e), at(BEACON, hs_s, hs_e), at(BEACON, pb_s, pb_e)],
        "verified": False,
    },
})

answers.append({
    "id": "A02", "kind": "summarize",
    "anchors": [diff_anchor(BEACON, pb_s, pb_e)],
    "question": "What does ProcessBounce do, and what did this PR change in it?",
    "seed": "answered",
    "answer": {
        "kind": "summarize",
        "gist": "Checks the shared secret, parses a batch of Beacon reports, and turns each failed delivery into a bounce.",
        "summarize": {"changed": "New method: the whole Beacon handler is added by this PR."},
        "detail": (
            "Skips a report if:\n"
            "- its type isn't `delivery.report`\n"
            "- `mapBeaconStatus` says it isn't a bounce (delivered, queued and so on)\n\n"
            "Each remaining report becomes one `models.Bounce` with source `beacon`, the lowercased recipient "
            "as the email and the raw event as meta. A report with no parseable time gets the current time."
        ),
        "citations": [at(BEACON, pb_s, pb_e), at(BEACON, pe_s, pe_e), at(BEACON, ms_s, ms_e)],
        "verified": False,
    },
})

answers.append({
    "id": "A03", "kind": "sample_data",
    "anchors": [diff_anchor(BEACON, ms_s, ms_e)],
    "question": "Show me the data flowing through mapBeaconStatus.",
    "seed": "answered_unread",
    "answer": {
        "kind": "sample_data",
        "gist": "The status is lowercased and mapped to a bounce type. Only \"failed\" looks at the details, and a mention of the domain makes it hard.",
        "sample_data": {"steps": [
            step("complained -> complaint", at(BEACON, ms_s + 4), 'mapBeaconStatus("Complained", "") -> ("complaint", true)', "go", "test"),
            step("suppressed -> hard", at(BEACON, ms_s + 6), 'mapBeaconStatus("Suppressed", "") -> ("hard", true)', "go", "test"),
            step("failed + SMTP 5.x.x -> hard", at(BEACON, code_s, code_s + 5), 'mapBeaconStatus("Failed", "550 5.1.1 User unknown") -> ("hard", true)', "go", "test"),
            step("failed + SMTP 4.x.x -> soft", at(BEACON, code_s, code_s + 5), 'mapBeaconStatus("Failed", "452 4.2.2 Mailbox full") -> ("soft", true)', "go", "test"),
            step("failed + soft phrase -> soft", at(BEACON, soft_s, soft_e), 'mapBeaconStatus("Failed", "Mailbox full") -> ("soft", true)', "go", "test"),
            step("failed + \"domain\" -> hard", at(BEACON, dom), 'mapBeaconStatus("Failed", "Sending domain not verified") -> ("hard", true)', "go", "test"),
            step("failed, nothing else -> soft", at(BEACON, dom + 3), 'mapBeaconStatus("Failed", "Connection timed out") -> ("soft", true)', "go", "test"),
            step("complained ignores the details", at(BEACON, ms_s + 4), 'mapBeaconStatus("Complained", "spam report") -> ("complaint", true)', "go", "invented"),
            step("delivered -> not a bounce", at(BEACON, ms_e - 2), 'mapBeaconStatus("Delivered", "") -> ("", false)', "go", "test"),
        ]},
        "detail": (
            "Ordering in the `failed` branch:\n"
            "- An SMTP code (`5.1.1`, `4.2.2`) wins over the keywords.\n"
            "- Soft phrases are checked next.\n"
            "- Only then does a `domain` mention make the bounce hard.\n\n"
            "So a sender-side failure such as \"Sending domain not verified\" becomes a **hard** bounce, and with the default "
            "`bounce.actions` (hard: count 1, action `blocklist`, %s) one such report blocklists the contact."
            % at(SCHEMA, actions_line)
        ),
        "citations": [at(BEACON, ms_s, ms_e), at(BEACON, dom), at(SCHEMA, actions_line)],
        "verified": True,
        "ran": [{"command": PROBE_MAP, "result": "Every row matches the reading, including the surprising one:\n" + PROBE_MAP_OUT}],
    },
})

answers.append({
    "id": "A04", "kind": "trace",
    "anchors": [diff_anchor(BOUNCE_CMD, proc_call, proc_call), diff_anchor(BEACON, map_call - 2, map_call + 3)],
    "question": "Trace this call: what reaches it, and what does ProcessBounce call?",
    "answer": {
        "kind": "trace",
        "gist": "Beacon POSTs to the public /webhooks/bounce/beacon route; BounceWebhook reads the body, then ProcessBounce verifies the secret, parses the events and maps each status.",
        "trace": {"hops": [
            {"at": at(MAIN, route), "fn": "POST /webhooks/bounce/{service}", "text": "Public route, no login; BounceWebhook refuses it when bounce webhooks are off"},
            {"at": at(BOUNCE_CMD, bw_s), "fn": "BounceWebhook", "text": "Reads the body (capped at 1 MiB); service=beacon selects the Beacon case"},
            {"at": at(BOUNCE_CMD, proc_call), "fn": "BounceWebhook (anchor)", "text": "Any X-Beacon-Event other than handshake calls ProcessBounce"},
            {"at": at(BEACON, verify_call), "fn": "(*Beacon).verifyAuth", "text": "?code= or the configured header must equal the secret; skipped when none is set"},
            {"at": at(BEACON, parse_call), "fn": "parseBeaconEvents", "text": "Splits the JSON array and keeps each event's raw JSON"},
            {"at": at(BEACON, map_call), "fn": "mapBeaconStatus", "text": "Per report: status and details to hard, soft or complaint, or not a bounce"},
            {"at": at(BOUNCE_CMD, enqueue), "fn": "(*Manager).Enqueue", "text": "Back in BounceWebhook, each bounce is queued; a full queue answers 503"},
        ]},
        "citations": [at(MAIN, route), at(BOUNCE_CMD, bw_s, bw_e), at(BEACON, pb_s, pb_e)],
        "detail": "The only caller is `BounceWebhook`, reached through the route above. `ProcessBounce` calls `verifyAuth`, `parseBeaconEvents` and `mapBeaconStatus`.",
        "verified": False,
    },
    "follow_ups": [{
        "kind": "sample_data",
        "gist": "A failed delivery report for Ana@Example.com becomes one hard Bounce with source \"beacon\". Nothing in the repo tests this path.",
        "sample_data": {"steps": [
            step("request", at(MAIN, route), "POST /webhooks/bounce/beacon?code=s3cret\nX-Beacon-Event: report", "http", "spec", at(DOCS, docs_row)),
            step("body", at(BOUNCE_CMD, body_read),
                 '[{"id":"e1","type":"delivery.report","time":"2026-03-16T10:00:00Z","data":{"recipient":"Ana@Example.com","status":"failed","details":"550 5.1.1 User unknown"}}]',
                 "json", "invented"),
            step("verifyAuth", at(BEACON, verify_call), 'code = "s3cret"; b.secret = "s3cret" -> nil (accepted)', "go", "invented"),
            step("mapBeaconStatus", at(BEACON, map_call), 'mapBeaconStatus("failed", "550 5.1.1 User unknown") -> ("hard", true)', "go", "invented"),
            step("bounce", at(BEACON, map_call + 11),
                 '{"email":"ana@example.com","source":"beacon","type":"hard","created_at":"2026-03-16T10:00:00Z"}', "json", "invented"),
        ]},
        "citations": [at(BOUNCE_CMD, body_read), at(BEACON, pb_s, pb_e)],
        "detail": "Real Beacon payloads may carry more fields than the handler reads: it uses only `type`, `time` and the `data` recipient, status and details.",
        "verified": False,
    }],
})

answers.append({
    "id": "A05", "kind": "test_cases",
    "anchors": [{"type": "file", "path": BEACON}],
    "question": "What tests cover this file? Which did this PR add or change?",
    "answer": {
        "kind": "test_cases",
        "gist": "Confirmed: none of the three bounce packages has any coverage.",
        "test_cases": {"cases": [
            {"name": "Lists the webhook providers", "at": cy + ":3",
             "text": "Turns on bounce webhooks in Settings and checks the Sendgrid and Postmark switches show; never touches the new Beacon switch.",
             "status": "untouched"},
        ]},
        "detail": "```text\n" + COVER_OUT + "\n```\n\nUntested paths include `mapBeaconStatus` (its string-matching branches), `verifyAuth` (query versus header) and the handshake.",
        "citations": [at(cy, 3), at(BEACON, 1, ms_e)],
        "verified": True,
        "ran": [{"command": "go test -cover ./...", "result": "Confirmed: none of the three bounce packages has any coverage."}],
    },
})

answers.append({
    "id": "A06", "kind": "research",
    "anchors": [diff_anchor(BOUNCE_CMD, hs_branch_s, hs_branch_e), diff_anchor(BEACON, hs_s, hs_e)],
    "question": "Is the handshake checked against the shared secret?",
    "seed": "answered_unread",
    "answer": {
        "kind": "research",
        "gist": "Confirmed. With the secret set to `s3cret` and no secret sent, the handshake still succeeds:",
        "research": {
            "verdict": "no",
            "why": "ProcessHandshake never calls verifyAuth; only ProcessBounce does, and BounceWebhook adds no auth before the switch.",
            "evidence": [at(BOUNCE_CMD, hs_branch_s, hs_branch_e), at(BEACON, hs_s, hs_e), at(BEACON, verify_call)],
        },
        "detail": (
            "Impact is low: the handshake only echoes back the `challenge` the caller supplied (%s), and nothing is stored.\n\n"
            "```text\n%s\n```" % (at(BEACON, hs_s, hs_e), PROBE_HS_OUT)
        ),
        "citations": [at(BOUNCE_CMD, hs_branch_s, hs_branch_e), at(BEACON, hs_s, hs_e), at(BEACON, va_s, va_e)],
        "verified": True,
        "ran": [{"command": PROBE_HS, "result": "Confirmed. With the secret set to s3cret and no secret sent, the handshake still succeeds."}],
    },
})

answers.append({
    "id": "A07", "kind": "research",
    "anchors": [diff_anchor(SETTINGS_CMD, keep_s, keep_e), diff_anchor(INIT, ib_s + 2, ib_e - 8)],
    "question": "If I change the Beacon secret in Settings, does it take effect without a restart?",
    "answer": {
        "kind": "research",
        "gist": "No. UpdateSettings only replaces the stored settings; the Beacon handler was built once at startup with the old secret.",
        "research": {
            "verdict": "no",
            "why": "initBounceManager copies the secret into webhooks.NewBeacon at boot, and nothing rebuilds the Manager afterwards.",
            "evidence": [at(SETTINGS_CMD, us_s, us_e), at(INIT, opt_s, opt_e), at(BOUNCE_PKG, new_pkg_s, new_pkg_e)],
        },
        "detail": "The same is true of Sendgrid and Postmark, so the settings page should say a restart is needed. Today it doesn't.",
        "citations": [at(SETTINGS_CMD, us_s, us_e), at(INIT, ib_s, ib_e), at(BOUNCE_PKG, new_pkg_s, new_pkg_e)],
        "verified": False,
    },
})

answers.append({
    "id": "A08", "kind": "research",
    "anchors": [diff_anchor(INIT, const_field, const_field), diff_anchor(INIT, const_set, const_set)],
    "question": "Is Consts.BounceBeaconEnabled read anywhere?",
    "answer": {
        "kind": "research",
        "gist": "No. It is set in initConstConfig and never read.",
        "research": {
            "verdict": "no",
            "why": "BounceWebhook checks consts.BounceWebhooksEnabled and whether a.bounce.Beacon is nil, never BounceBeaconEnabled.",
            "evidence": [at(INIT, const_field), at(INIT, const_set), at(BOUNCE_CMD, L(BOUNCE_CMD, "BounceWebhooksEnabled"))],
        },
        "detail": "BounceSendgridEnabled and BouncePostmarkEnabled are in the same state, so this follows the existing pattern. The field could be dropped for all three.",
        "citations": [at(INIT, const_field), at(INIT, const_set)],
        "verified": False,
    },
})

answers.append({
    "id": "A09", "kind": "research",
    "anchors": [diff_anchor(MIGRATION, mig_exec - 1, mig_e - 3)],
    "question": "Why an upsert here instead of INSERT … ON CONFLICT DO NOTHING? Who would it overwrite?",
    "seed": "draft",
    "answer": {
        "kind": "research",
        "gist": "The upsert resets bounce.beacon to the disabled defaults. It would overwrite a row someone added by hand.",
        "research": {
            "verdict": "partly",
            "why": "DO UPDATE replaces an existing value; the earlier v1.2.0 migration used DO NOTHING for the Sendgrid and Postmark rows.",
            "evidence": [at(MIGRATION, mig_exec - 1, mig_exec + 1), "internal/migrations/migrations.go:%d" % L("internal/migrations/migrations.go", "ON CONFLICT DO NOTHING")],
        },
        "detail": "The doc comment says it is so a hand-added setting \"keeps working\", but an upsert that rewrites the value does the opposite.",
        "citations": [at(MIGRATION, mig_s, mig_e)],
        "verified": False,
    },
})

answers.append({
    "id": "A10", "kind": "summarize",
    "anchors": [diff_anchor(INIT, ib_s, ib_e)],
    "question": "What does initBounceManager do, and what did this PR change in it?",
    "answer": {
        "kind": "summarize",
        "gist": "Builds the bounce Manager from the settings; this PR passes the Beacon switch, secret and header through.",
        "summarize": {"changed": "Adds three Opt fields: BeaconEnabled, BeaconSecret and BeaconSecretHeader."},
        "detail": "It stops the process with a fatal log if `bounce.New` fails, as before. The Manager is built once, at startup.",
        "citations": [at(INIT, ib_s, ib_e), at(BOUNCE_PKG, new_pkg_s, new_pkg_e)],
        "verified": False,
    },
})

answers.append({
    "id": "A11", "kind": "define",
    "anchors": [{"type": "description", "text": "messageRef"}],
    "question": "What does “messageRef” mean in this context?",
    "seed": "answered_unread",
    "answer": {
        "kind": "define",
        "gist": "The field in a Beacon delivery report that carries the sent message's Message-ID header.",
        "define": {"term": "messageRef", "example": "\"messageRef\": \"<20260316.1@mail.example.com>\""},
        "detail": "The handler declares the field in `beaconEvent` but never reads it, which is what the description means by matching on recipient email only.",
        "citations": [at(BEACON, L(BEACON, "MessageRef string"))],
        "verified": False,
    },
})

answers.append({
    "id": "A12", "kind": "before_after",
    "anchors": [diff_anchor(SETTINGS_MODEL, model_s, model_e), diff_anchor(SETTINGS_CMD, mask_line, mask_line)],
    "question": "How does the settings data for bounce webhooks look before and after this change?",
    "answer": {
        "kind": "before_after",
        "gist": "The settings JSON gains a `bounce.beacon` object beside the other bounce providers. Existing keys are unchanged.",
        "before_after": {
            "before": step("before", at(SCHEMA, schema_pm - 2, schema_pm),
                           '{"bounce.webhooks_enabled": false, "bounce.sendgrid": {"enabled": false, "key": ""}, "bounce.postmark": {"enabled": false, "username": "", "password": ""}}',
                           "json", "spec", at(SCHEMA, schema_pm - 2, schema_pm)),
            "after": step("after", at(SCHEMA, schema_beacon),
                          '{"bounce.webhooks_enabled": false, "bounce.sendgrid": {…}, "bounce.postmark": {…}, "bounce.beacon": {"enabled": false, "shared_secret": "", "shared_secret_header": ""}}',
                          "json", "spec", at(SCHEMA, schema_beacon)),
            "note": "Existing installs get the row from the v1.3.0 migration; GET masks shared_secret, and a blank PUT keeps it.",
        },
        "citations": [at(SETTINGS_MODEL, model_s, model_e), at(SCHEMA, schema_beacon), at(MIGRATION, mig_s, mig_e)],
        "verified": False,
    },
})

answers.append({
    "id": "A13", "kind": "define",
    "anchors": [{"type": "briefing", "ref": "tldr", "text": "Beacon Relay"}],
    "question": "What does “Beacon Relay” mean in this context?",
    "seed": "draft",
    "answer": {
        "kind": "define",
        "gist": "The third-party mail service this PR adds as a bounce provider.",
        "define": {"term": "Beacon Relay", "example": "Beacon Relay POSTs delivery reports to /webhooks/bounce/beacon"},
        "detail": "It is a made-up provider in this fixture; the PR treats it like Sendgrid and Postmark.",
        "citations": [at(DOCS, docs_row)],
        "verified": False,
    },
})

dump("answers.json", {"answers": answers})

# ---- feedback.json and revisions.json --------------------------------------

F1 = (
    '`strings.Contains(d, "domain")` <<quote the exact condition with its path:line>> also catches sender-side failures such as '
    '"Sending domain not verified" <<list the status/details values that would hit this branch>>. With the default hard-bounce '
    "action <<the setting name and its default>>, our own DNS mistake would blocklist healthy contacts. Could this match "
    "narrower phrases, or treat the rest as soft?"
)
F2 = (
    "With no shared secret set <<the setting key and where the empty check happens>>, the webhook accepts any payload from anyone "
    "who can reach it. Worth a warning at startup, or a line in the settings help text <<the i18n key for that help text>>, so "
    "people know the endpoint is open?"
)
F4 = "I think we talked about handling this the other way. Can we switch to that approach here too?"

dump("feedback.json", {"feedback": [
    {"id": "F1", "anchor": diff_anchor(BEACON, dom - 2, dom + 3), "status": "draft", "body": F1},
    {"id": "F2", "anchor": diff_anchor(BEACON, va_early, va_early + 2), "status": "draft", "body": F2},
    {"id": "F4", "anchor": diff_anchor(BOUNCE_CMD, proc_call, proc_call), "status": "draft", "body": F4},
]})

i18n_help = L("i18n/en.json", "beaconSecretHelp")
dump("revisions.json", {"revisions": [
    {
        "feedback": "F1", "for_body": F1,
        "revision": {
            "kind": "revision",
            "gist": "Filled the exact condition, the values that reach it and the bounce.actions default.",
            "revision": {
                "revised_text": (
                    '`strings.Contains(d, "domain")` (beacon.go:%d) also catches sender-side failures such as "Sending domain not verified". '
                    "It applies to any report with status `failed` whose details contain \"domain\", have no SMTP 4.x.x/5.x.x code (beacon.go:%d-%d), "
                    "and match none of the soft phrases (beacon.go:%d-%d). Such reports return a hard bounce. With the default `bounce.actions` "
                    "(hard: count 1, action `blocklist`, schema.sql:%d), our own DNS mistake would blocklist healthy contacts. Could this match "
                    "narrower phrases, or treat the rest as soft?" % (dom, code_s, code_s + 5, soft_s, soft_e, actions_line)
                ),
                "what_changed": "Filled 3 Agent Notes: condition with line, matching status/details values, setting name and default",
            },
            "citations": [at(BEACON, dom), at(BEACON, code_s, soft_e), at(SCHEMA, actions_line)],
        },
    },
    {
        "feedback": "F2", "for_body": F2,
        "revision": {
            "kind": "revision",
            "gist": "Filled the setting key, the empty-check location and the help-text i18n key.",
            "revision": {
                "revised_text": (
                    "With no shared secret set (`bounce.beacon.shared_secret` empty; `verifyAuth` returns nil early at %s), the webhook "
                    "accepts any payload from anyone who can reach it. Worth a warning at startup, or a line in the settings help text "
                    "(`settings.bounces.beaconSecretHelp`, i18n/en.json:%d), so people know the endpoint is open?" % (at(BEACON, va_early), i18n_help)
                ),
                "what_changed": "Filled 2 Agent Notes: setting key and check location, i18n help key",
            },
            "citations": [at(BEACON, va_early), "i18n/en.json:%d" % i18n_help],
        },
    },
    {
        "feedback": "F4", "for_body": F4,
        "revision": {
            "kind": "revision",
            "gist": "Your intent is ambiguous: \"the other way\" isn't defined in anything I can see. Two questions.",
            "revision": {
                "questions": [
                    "Which approach did you have in mind? I can't see that conversation, so I can't say what \"the other way\" is.",
                    "Is this about how the Beacon case chooses between the handshake and the reports (the X-Beacon-Event header), or about auth in ProcessBounce?",
                ],
            },
            "citations": [at(BOUNCE_CMD, proc_call)],
        },
    },
]})
print("wrote fixtures for %d files, +%d -%d" % (len(files), adds, dels))
