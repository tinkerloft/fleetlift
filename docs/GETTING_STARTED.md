# Getting Started with FleetLift

This guide walks you through setting up FleetLift from scratch and running your first workflow. By the end you will have a working local environment, the web dashboard open in your browser, and a completed audit workflow you kicked off yourself.

---

## 1. Prerequisites

You need the following installed on your machine:

- **Go 1.22+** -- [go.dev/dl](https://go.dev/dl/)
- **Node.js 20+** and **npm** -- for building the web UI
- **Docker** and **Docker Compose v2** -- for Temporal and PostgreSQL
- **Git**

You will also need accounts/keys for:

| Item | Where to get it |
|------|----------------|
| **Anthropic API key** *or* **Claude OAuth token** | [console.anthropic.com](https://console.anthropic.com/) — the `init-local` wizard will prompt for this |
| **OpenSandbox** | Required for agent steps to execute. The `init-local` wizard starts it automatically; or run `docker compose up -d` manually |
| **GitHub OAuth app** *(production only)* | Only needed when `DEV_NO_AUTH` is not set. [github.com/settings/developers](https://github.com/settings/developers) — callback URL: `http://localhost:8080/auth/github/callback` |

---

## 2. Clone the Repository

```bash
git clone https://github.com/your-org/fleetlift.git
cd fleetlift
```

---

## 3. Run the Setup Wizard

FleetLift ships with an `init-local` command that handles infrastructure, configuration, and database setup in one step.

First, build the CLI:

```bash
make build
```

Then run the wizard:

```bash
./bin/fleetlift init-local
```

The wizard will:
1. Prompt for your Anthropic API key or Claude OAuth token
2. Optionally prompt for a GitHub personal access token (required for workflows that clone or push to private repos)
3. Write `~/.fleetlift/local.env` with generated secrets and your credentials
4. Start the Docker Compose stacks (Temporal + PostgreSQL + OpenSandbox)
5. Create the `fleetlift` database, apply the schema, and seed a dev team

> **Dev mode:** `init-local` sets `DEV_NO_AUTH=1` so you can use the web UI and CLI without a GitHub OAuth app. All requests are authenticated as a local dev user. Disable this for production by removing `DEV_NO_AUTH` from your env.

> **OpenSandbox:** Agent steps require a running OpenSandbox instance. The wizard starts it automatically. If you skip the Docker step, start it manually: `docker compose up -d`

---

## 4. Start the Server and Worker

```bash
scripts/integration/start.sh
```

This script builds the server and worker binaries (if needed), stops any previous instances, and starts both processes. Logs go to `/tmp/fleetlift-server.log` and `/tmp/fleetlift-worker.log`.

```bash
# Tail both logs
scripts/integration/logs.sh

# Check process status
scripts/integration/status.sh
```

---

## 5. Build the CLI

The CLI binary is built by `make build`. To rebuild manually:

```bash
go build -o bin/fleetlift ./cmd/cli
```

Or:

```bash
make fleetlift
```

Add the binary to your path for convenience:

```bash
export PATH="$PWD/bin:$PATH"
```

---

## 6. Open the App

**Web UI:** Open **http://localhost:8080** in your browser. In dev mode (`DEV_NO_AUTH=1`), you are automatically signed in as the local dev user — no GitHub login required.

**CLI:** No login step needed in dev mode. Commands work immediately:

```bash
fleetlift workflow list
```

> **Production auth:** When running without `DEV_NO_AUTH`, use `fleetlift auth login` to go through GitHub OAuth and store a token in `~/.fleetlift/auth.json`.

---

## 7. Browse Available Workflows

FleetLift ships with 10 built-in workflow templates. Take a look at what is available.

**CLI:**

```bash
fleetlift workflow list
```

```
ID                  TITLE               TAGS
audit               Audit               audit, security, fleet, report
bug-fix             Bug Fix             ...
dependency-update   Dependency Update    ...
fleet-research      Fleet Research       ...
fleet-transform     Fleet Transform      ...
incident-response   Incident Response    ...
migration           Migration            ...
pr-review           PR Review            ...
triage              Triage               ...
add-tests           Add Tests            ...
```

To see the parameters and steps for a specific template:

```bash
fleetlift workflow get audit
```

**Web UI:** Navigate to the **Workflows** page from the sidebar to see all templates with their descriptions, parameters, and DAG previews.

---

## 8. Run Your First Workflow

The `audit` template is a great starting point because it is read-only -- the agent inspects repositories without making changes.

### Via the CLI

```bash
fleetlift run start \
  --workflow audit \
  --param 'repos=[{"url":"https://github.com/your-org/your-repo"}]' \
  --param 'audit_prompt=Check for hardcoded secrets, insecure dependencies, and missing security headers'
```

You will get back a **run ID** like `run_abc123`.

### Via the Web UI

1. Go to **Workflows** and click **Audit**.
2. Fill in the parameters:
   - **repos** -- a JSON array of repo objects, e.g. `[{"url":"https://github.com/your-org/your-repo"}]`
   - **audit_prompt** -- describe what to look for.
3. Click **Start Run**.

---

## 9. Watch Execution

### CLI -- stream logs

```bash
fleetlift run logs <run-id> -f
```

The `-f` flag follows the log stream in real time, similar to `tail -f`. You will see output from each step as it executes.

### CLI -- check status

```bash
fleetlift run list
```

This shows all runs with their current status (`running`, `completed`, `failed`, `awaiting_input`).

### Web UI -- DAG view and log stream

Open the run from the **Runs** page. The detail view shows:

- **DAG visualization** -- each step is a node in the directed acyclic graph. Nodes light up as they execute: grey (pending), blue (running), green (completed), red (failed), yellow (awaiting input).
- **Log stream** -- click any step node to see its real-time log output. For the audit workflow, you will see the agent cloning the repo, analyzing the code, and producing findings.
- **Step details** -- expand a completed step to see its structured output (findings, risk level, summary).

The audit workflow has three steps:

1. **Scan repositories** -- runs in parallel across all repos you specified.
2. **Collate audit results** -- merges per-repo findings into a single compliance report.
3. **Notify compliance team** -- optional Slack notification (skipped if you did not set a channel).

---

## 10. Interact with Human-in-the-Loop (HITL) Steps

Some workflows include approval gates where the agent pauses and waits for human review. While the `audit` template does not require approval, other templates like `bug-fix` and `migration` do.

When a step reaches `awaiting_input` status:

**CLI:**

```bash
# See what is waiting for you
fleetlift inbox list

# Approve the step to let it continue
fleetlift run approve <run-id>

# Or reject it to stop that branch
fleetlift run reject <run-id>

# Or redirect the agent with new instructions
fleetlift run steer <run-id> --prompt "Also check the auth module for SQL injection"
```

**Web UI:** The **Inbox** page shows all pending approvals. Click a notification to see the agent's proposed changes, then approve, reject, or steer from the detail view.

---

## 11. View the Results

Once the run completes:

**CLI:**

```bash
fleetlift run logs <run-id>
```

Without `-f`, this prints the full log history.

**Web UI:**

- The run detail page shows the final status of every step.
- The **Reports** page (accessible from the sidebar) aggregates results across runs for trend analysis.
- Artifacts (like the `audit-report.json` produced by the collate step) are available for download.

---

## 12. Next Steps

Now that you have a working FleetLift setup, here are some things to explore:

### Try other built-in templates

- **`bug-fix`** -- point it at an issue URL and watch the agent propose a fix with a PR.
- **`dependency-update`** -- scan and update outdated dependencies across a fleet.
- **`pr-review`** -- automated code review on open pull requests.
- **`triage`** -- classify and prioritize a batch of issues.

### Create custom workflow templates

Built-in templates live in `internal/template/workflows/`. Study their YAML structure to understand how steps, parameters, dependencies, and agent prompts fit together. You can create your own templates and register them through the web UI or the database.

See [docs/WORKFLOW_REFERENCE.md](WORKFLOW_REFERENCE.md) for the full template specification.

### Run across multiple repositories

Most templates accept a `repos` parameter as a JSON array. FleetLift will fan out agent execution in parallel across all listed repos, respecting the `max_parallel` limit. This is where FleetLift really shines -- what would take hours of manual work happens in minutes.

### Set up credentials

Store repository access tokens and other secrets through the web UI's credential management. These are encrypted at rest with AES-256-GCM (that is what `CREDENTIAL_ENCRYPTION_KEY` is for) and injected into sandbox environments as needed.

### Explore the Knowledge loop

The **Knowledge** page in the web UI lets you build a local knowledge store that agents can reference across runs. This is useful for encoding team conventions, architecture decisions, and past findings so the agent gets smarter over time.

### Production deployment

For production use, consider:

- Running the server and worker as systemd services or in Kubernetes.
- Using a dedicated PostgreSQL instance instead of the Docker Compose one.
- Enabling the OpenSandbox integration for proper agent isolation.
- Setting up the observability stack (`docker-compose.o11y.yaml`) for Prometheus metrics.

---

## Quick Reference

| Command | What it does |
|---------|-------------|
| `./bin/fleetlift init-local` | One-time setup: Docker stacks, DB, secrets, credentials |
| `scripts/integration/start.sh` | Start server + worker |
| `scripts/integration/logs.sh` | Tail server and worker logs |
| `scripts/integration/status.sh` | Check if processes are running |
| `fleetlift workflow list` | List available templates |
| `fleetlift run start --workflow <id> --param key=value` | Start a run |
| `fleetlift run logs <id> -f` | Stream run logs |
| `fleetlift run approve <id>` | Approve a HITL step |
| `fleetlift inbox list` | See pending notifications |

---

## Troubleshooting

If something goes wrong, check [docs/TROUBLESHOOTING.md](TROUBLESHOOTING.md) for common issues and solutions.

**Common first-run issues:**

- **"connection refused" on port 7233** -- Temporal is not ready yet. Run `docker compose ps` and wait for the health check to pass.
- **"relation does not exist"** -- The database schema hasn't been applied. Re-run `./bin/fleetlift init-local` — it is idempotent and safe to run again.
- **OAuth callback error** -- Only relevant when `DEV_NO_AUTH` is not set. Ensure your GitHub OAuth app callback URL is `http://localhost:8080/auth/github/callback`.
- **Worker not picking up runs** -- Ensure the worker is running and connected to the same Temporal address as the server.
