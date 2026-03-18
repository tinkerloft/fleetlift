# CLI Reference

The `fleetlift` CLI communicates with the API server over HTTP. By default it targets `http://localhost:8080`; override with `--server <url>` or the `FLEETLIFT_SERVER` environment variable.

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--server <url>` | `http://localhost:8080` | Fleetlift API server URL |
| `--output-json` | false | Print raw JSON instead of formatted table output |

---

## auth

Authentication commands.

### auth login

Log in via GitHub OAuth. Opens a browser window; paste the returned token when prompted.

```
fleetlift auth login
```

The token is saved to `~/.fleetlift/auth.json` and used automatically by subsequent commands.

### auth status

Show whether a token is currently saved.

```
fleetlift auth status
```

### auth logout

Remove saved credentials.

```
fleetlift auth logout
```

---

## workflow (alias: wf)

Manage workflow templates.

### workflow list

List all available templates (built-in and team-owned).

```
fleetlift workflow list [--output-json]
```

Output columns: `SLUG`, `TITLE`, `BUILTIN`, `TAGS`

### workflow get \<slug\>

Print full details and YAML body for a template.

```
fleetlift workflow get add-tests
fleetlift workflow get fleet-transform --output-json
```

### workflow create

Create a new team-owned template from a YAML file.

```
fleetlift workflow create --file my-workflow.yaml
```

| Flag | Description |
|------|-------------|
| `-f, --file <path>` | Path to workflow YAML (required) |

### workflow delete \<slug\>

Delete a team-owned template. Built-in templates cannot be deleted.

```
fleetlift workflow delete my-workflow
```

### workflow fork \<slug\>

Copy a built-in template into your team so you can customize it.

```
fleetlift workflow fork fleet-transform
```

---

## run

Manage workflow runs.

### run start \<workflow-id\>

Start a new run of the named workflow template.

```
fleetlift run start add-tests
fleetlift run start fleet-transform -p repos='[{"url":"https://github.com/org/svc.git"}]' -p prompt="Add unit tests"
fleetlift run start fleet-transform -f   # start and follow logs immediately
```

| Flag | Description |
|------|-------------|
| `-p, --param <key=value>` | Parameter value. Repeatable. JSON values are auto-parsed. |
| `-f, --follow` | Stream logs immediately after starting |

### run list

List recent runs for your team.

```
fleetlift run list [--output-json]
```

Output columns: `ID`, `WORKFLOW`, `STATUS`, `CREATED`

### run get \<id\>

Print run details and per-step status.

```
fleetlift run get abc12345
fleetlift run get abc12345 --output-json
```

### run logs \<id\>

Stream live logs for a run via SSE. The stream closes when the run reaches a terminal state.

```
fleetlift run logs abc12345
```

### run approve \<id\>

Approve a run that is paused waiting for human approval (e.g. before PR creation).

```
fleetlift run approve abc12345
```

### run reject \<id\>

Reject a run that is paused waiting for approval.

```
fleetlift run reject abc12345
```

### run steer \<id\>

Send a steering instruction to an agent that is paused mid-execution (`allow_mid_execution_pause: true`).

```
fleetlift run steer abc12345 --prompt "Also handle the edge case where the input is nil"
```

| Flag | Description |
|------|-------------|
| `-p, --prompt <text>` | Steering instruction (required) |

### run cancel \<id\>

Cancel a running or paused workflow.

```
fleetlift run cancel abc12345
```

---

## inbox

View and manage HITL inbox notifications.

### inbox list

List unread inbox items (approval requests, steering requests, etc.).

```
fleetlift inbox list [--output-json]
```

Output columns: `ID`, `KIND`, `TITLE`, `CREATED`

### inbox read \<id\>

Mark an inbox item as read.

```
fleetlift inbox read abc12345
```

---

## credential (alias: cred)

Manage encrypted team credentials. Credential values are stored AES-256-GCM encrypted and injected into sandbox environments by name.

### credential list

List credential names (values are never returned).

```
fleetlift credential list [--output-json]
```

### credential set \<name\>

Create or update a credential.

```
fleetlift credential set GITHUB_TOKEN --value ghp_abc...
```

| Flag | Description |
|------|-------------|
| `-v, --value <text>` | Credential value (required) |

### credential delete \<name\>

Permanently delete a credential.

```
fleetlift credential delete GITHUB_TOKEN
```

---

## knowledge

Manage knowledge items captured during workflow runs.

### knowledge list

List knowledge items for your team. Filter by status with `--status`.

```
fleetlift knowledge list [--status pending|approved|rejected] [--output-json]
```

Output columns: `ID`, `STATUS`, `CONTENT` (truncated), `TAGS`, `CREATED`

| Flag | Description |
|------|-------------|
| `--status <value>` | Filter by `pending`, `approved`, or `rejected` (default: all) |

### knowledge approve \<id\>

Approve a knowledge item so it will be injected into future runs.

```
fleetlift knowledge approve <id>
```

### knowledge reject \<id\>

Reject a knowledge item so it will not be used in future runs.

```
fleetlift knowledge reject <id>
```

---

## Built-in workflow slugs

The following workflows are available out of the box:

| Slug | Description |
|------|-------------|
| `add-tests` | Add missing unit tests to a repo |
| `audit` | Security/compliance scan with collated report |
| `bug-fix` | Diagnose and fix a reported bug |
| `dependency-update` | Update outdated dependencies |
| `fleet-research` | Research question answered across many repos |
| `fleet-transform` | Parallel code transformation with approval gate |
| `incident-response` | Rapid triage and mitigation for production incidents |
| `migration` | Code migration with verifier gating |
| `pr-review` | AI-assisted pull request review |
| `triage` | Issue triage and classification |
