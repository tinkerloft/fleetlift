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
| **GitHub OAuth app** | [github.com/settings/developers](https://github.com/settings/developers) -- set the callback URL to `http://localhost:8080/api/auth/github/callback` |
| **Anthropic API key** | [console.anthropic.com](https://console.anthropic.com/) |
| **OpenSandbox** (optional for first run) | Provides isolated containers for agent execution |

---

## 2. Clone the Repository

```bash
git clone https://github.com/your-org/fleetlift.git
cd fleetlift
```

---

## 3. Start Infrastructure

Docker Compose brings up PostgreSQL (used by Temporal) and the Temporal server with its web UI.

```bash
docker compose up -d
```

Wait for healthy status:

```bash
docker compose ps
```

You should see `temporal`, `temporal-ui`, and `postgres` all running. The Temporal web UI is available at **http://localhost:8233** -- you can use it later to inspect workflow executions directly.

### Optional: Start OpenSandbox

If you want agent steps to run in isolated Docker sandboxes (recommended for production, optional for a first look):

```bash
docker compose -f docker-compose.opensandbox.yaml up -d
```

This starts the OpenSandbox lifecycle server on port 8090.

---

## 4. Configure Environment Variables

Create a `.env` file in the project root (it is already in `.gitignore`):

```bash
cat > .env << 'EOF'
# --- Database ---
DATABASE_URL=postgres://temporal:temporal@localhost:5432/fleetlift

# --- Temporal ---
TEMPORAL_ADDRESS=localhost:7233

# --- Auth ---
JWT_SECRET=replace-with-a-random-64-char-string
GITHUB_CLIENT_ID=your-github-oauth-client-id
GITHUB_CLIENT_SECRET=your-github-oauth-client-secret

# --- Encryption (generate with: openssl rand -hex 32) ---
CREDENTIAL_ENCRYPTION_KEY=replace-with-64-hex-chars

# --- Agent ---
ANTHROPIC_API_KEY=sk-ant-...
AGENT_IMAGE=claude-code:latest

# --- OpenSandbox (skip if not running OpenSandbox) ---
# OPENSANDBOX_DOMAIN=http://localhost:8090
# OPENSANDBOX_API_KEY=your-opensandbox-key
EOF
```

Generate real values for the secrets:

```bash
# JWT secret
openssl rand -hex 32

# Credential encryption key (must be exactly 32 bytes = 64 hex characters)
openssl rand -hex 32
```

Source the file so both the server and worker pick it up:

```bash
export $(grep -v '^#' .env | xargs)
```

> **Tip:** You can also use [direnv](https://direnv.net/) to load `.env` automatically when you `cd` into the project.

---

## 5. Create the FleetLift Database

The Docker Compose file creates a `temporal` database for Temporal's own use. FleetLift needs its own database in the same PostgreSQL instance:

```bash
docker compose exec postgres createdb -U temporal fleetlift
```

The server will automatically apply the schema on first connect -- no manual migration step is needed.

---

## 6. Build the Web UI

The React SPA is embedded into the server binary at build time. Build it before starting the server:

```bash
cd web && npm install && npm run build && cd ..
```

Or use the Makefile shortcut:

```bash
make build-web
```

This compiles the frontend into `web/dist/`, which the server embeds and serves at the root URL.

---

## 7. Start the Server and Worker

Open two terminal windows (or use `tmux`/`screen`). Make sure the environment variables are exported in both.

**Terminal 1 -- API server:**

```bash
go run ./cmd/server
```

The server starts on **http://localhost:8080**. It serves both the REST API (`/api/...`) and the embedded React SPA.

**Terminal 2 -- Temporal worker:**

```bash
go run ./cmd/worker
```

The worker registers the DAG and step workflows plus all activity implementations with Temporal. It will log `Worker started` when ready.

---

## 8. Build the CLI

In a third terminal:

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

## 9. Authenticate

### Via the Web UI

Open **http://localhost:8080** in your browser. Click **Sign in with GitHub**. After authorizing the OAuth app you will land on the Runs dashboard.

### Via the CLI

```bash
fleetlift auth login
```

This opens your browser for the same GitHub OAuth flow and stores a token locally.

---

## 10. Browse Available Workflows

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

## 11. Run Your First Workflow

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

## 12. Watch Execution

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

## 13. Interact with Human-in-the-Loop (HITL) Steps

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

## 14. View the Results

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

## 15. Next Steps

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
| `docker compose up -d` | Start Temporal + PostgreSQL |
| `make build-web` | Build the React frontend |
| `go run ./cmd/server` | Start API server on :8080 |
| `go run ./cmd/worker` | Start Temporal worker |
| `fleetlift auth login` | Authenticate via GitHub |
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
- **"relation does not exist"** -- The FleetLift database was not created. Run `docker compose exec postgres createdb -U temporal fleetlift`.
- **OAuth callback error** -- Make sure your GitHub OAuth app callback URL is set to `http://localhost:8080/api/auth/github/callback`.
- **Worker not picking up runs** -- Ensure the worker is running and connected to the same Temporal address as the server.
