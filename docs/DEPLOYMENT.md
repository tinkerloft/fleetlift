# FleetLift Production Deployment Guide

This guide covers deploying FleetLift in production environments. For architecture details, see [ARCHITECTURE.md](./ARCHITECTURE.md).

## Architecture Overview

FleetLift consists of five components:

| Component | Binary / Service | Role |
|-----------|-----------------|------|
| **API Server** | `cmd/server` | REST API, GitHub OAuth, JWT auth, SSE streaming, embedded React SPA. Default port 8080. |
| **Worker** | `cmd/worker` | Temporal worker. Registers DAGWorkflow, StepWorkflow, and all activities. |
| **PostgreSQL** | External | Persistent state: runs, steps, inbox, reports, credentials, teams, users. |
| **Temporal** | External | Durable workflow orchestration engine. |
| **OpenSandbox** | External | Container sandbox provider for agent execution. |

The server and worker are stateless Go binaries that connect to PostgreSQL and Temporal. They can be scaled independently.

---

## Required Secrets

Generate these before deploying any component.

### JWT_SECRET

Used to sign and verify HS256 JWT tokens. Must be a high-entropy random string, at least 32 bytes.

```bash
openssl rand -base64 32
```

Use the same value for all server replicas.

### CREDENTIAL_ENCRYPTION_KEY

Used for AES-256-GCM encryption of stored credentials. Must be exactly 32 bytes, hex-encoded (64 hex characters).

```bash
openssl rand -hex 32
```

Store this securely. Losing this key means all encrypted credentials become unrecoverable.

### GitHub OAuth App

1. Go to **GitHub Settings > Developer settings > OAuth Apps > New OAuth App**.
2. Set the **Authorization callback URL** to `https://<your-domain>/api/auth/github/callback`.
3. Note the **Client ID** and generate a **Client Secret**.
4. Set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` on the server.

### ANTHROPIC_API_KEY

Required for agent execution. Obtain from [console.anthropic.com](https://console.anthropic.com/).

### OPENSANDBOX_API_KEY

Required for sandbox provisioning. Obtain from your OpenSandbox provider.

---

## Environment Variables Reference

| Variable | Required | Default | Component |
|----------|----------|---------|-----------|
| `DATABASE_URL` | Yes | `postgres://fleetlift:fleetlift@localhost:5432/fleetlift` | Server, Worker |
| `TEMPORAL_ADDRESS` | Yes | `localhost:7233` | Worker |
| `OPENSANDBOX_DOMAIN` | Yes | — | Worker |
| `OPENSANDBOX_API_KEY` | Yes | — | Worker |
| `ANTHROPIC_API_KEY` | Yes | — | Worker |
| `AGENT_IMAGE` | No | `claude-code:latest` | Worker |
| `JWT_SECRET` | Yes | — | Server |
| `CREDENTIAL_ENCRYPTION_KEY` | Yes | — | Server |
| `GITHUB_CLIENT_ID` | Yes | — | Server |
| `GITHUB_CLIENT_SECRET` | Yes | — | Server |
| `GIT_USER_EMAIL` | No | `claude-agent@noreply.localhost` | Worker |
| `GIT_USER_NAME` | No | `Claude Code Agent` | Worker |
| `LISTEN_ADDR` | No | `:8080` | Server |

---

## Deployment Option 1: Docker Compose (Small Teams)

Suitable for evaluation, small teams, and non-critical workloads.

### Prerequisites

- Docker Engine 24+ with Compose V2
- At least 4 GB RAM available for containers

### Build Images

```bash
# Build the server and worker binaries
make build

# Build the agent init container image
make agent-image
```

### Compose File

Create a `docker-compose.yml` (or extend the existing one in the repository):

```yaml
version: "3.9"

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: fleetlift
      POSTGRES_PASSWORD: fleetlift
      POSTGRES_DB: fleetlift
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U fleetlift"]
      interval: 5s
      timeout: 3s
      retries: 5

  temporal:
    image: temporalio/auto-setup:latest
    environment:
      DB: postgresql
      DB_PORT: 5432
      POSTGRES_USER: fleetlift
      POSTGRES_PWD: fleetlift
      POSTGRES_SEEDS: postgres
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "7233:7233"

  temporal-ui:
    image: temporalio/ui:latest
    environment:
      TEMPORAL_ADDRESS: temporal:7233
    depends_on:
      - temporal
    ports:
      - "8233:8080"

  server:
    build:
      context: .
      dockerfile: Dockerfile
      target: server
    environment:
      DATABASE_URL: postgres://fleetlift:fleetlift@postgres:5432/fleetlift?sslmode=disable
      JWT_SECRET: ${JWT_SECRET}
      CREDENTIAL_ENCRYPTION_KEY: ${CREDENTIAL_ENCRYPTION_KEY}
      GITHUB_CLIENT_ID: ${GITHUB_CLIENT_ID}
      GITHUB_CLIENT_SECRET: ${GITHUB_CLIENT_SECRET}
      LISTEN_ADDR: ":8080"
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "8080:8080"

  worker:
    build:
      context: .
      dockerfile: Dockerfile
      target: worker
    environment:
      DATABASE_URL: postgres://fleetlift:fleetlift@postgres:5432/fleetlift?sslmode=disable
      TEMPORAL_ADDRESS: temporal:7233
      OPENSANDBOX_DOMAIN: ${OPENSANDBOX_DOMAIN}
      OPENSANDBOX_API_KEY: ${OPENSANDBOX_API_KEY}
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
      AGENT_IMAGE: claude-code:latest
    depends_on:
      - temporal
      - postgres

volumes:
  pgdata:
```

### Running

```bash
# Create a .env file with your secrets (never commit this file)
cat > .env <<'EOF'
JWT_SECRET=<output of: openssl rand -base64 32>
CREDENTIAL_ENCRYPTION_KEY=<output of: openssl rand -hex 32>
GITHUB_CLIENT_ID=<your GitHub OAuth client ID>
GITHUB_CLIENT_SECRET=<your GitHub OAuth client secret>
OPENSANDBOX_DOMAIN=https://your-opensandbox-instance.example.com
OPENSANDBOX_API_KEY=<your OpenSandbox API key>
ANTHROPIC_API_KEY=<your Anthropic API key>
EOF

docker compose up -d
```

The server runs migrations automatically on startup. Access the UI at `http://localhost:8080` and Temporal UI at `http://localhost:8233`.

---

## Deployment Option 2: Kubernetes (Production)

Recommended for production workloads with high availability requirements.

### Namespace and Secrets

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: fleetlift
---
apiVersion: v1
kind: Secret
metadata:
  name: fleetlift-secrets
  namespace: fleetlift
type: Opaque
stringData:
  DATABASE_URL: "postgres://fleetlift:CHANGEME@fleetlift-db.internal:5432/fleetlift?sslmode=require"
  JWT_SECRET: "CHANGEME"
  CREDENTIAL_ENCRYPTION_KEY: "CHANGEME"
  GITHUB_CLIENT_ID: "CHANGEME"
  GITHUB_CLIENT_SECRET: "CHANGEME"
  OPENSANDBOX_DOMAIN: "https://opensandbox.example.com"
  OPENSANDBOX_API_KEY: "CHANGEME"
  ANTHROPIC_API_KEY: "CHANGEME"
```

In practice, use a secrets manager (Vault, AWS Secrets Manager, or Sealed Secrets) rather than plain Kubernetes secrets.

### Server Deployment + Service

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fleetlift-server
  namespace: fleetlift
  labels:
    app: fleetlift-server
spec:
  replicas: 2
  selector:
    matchLabels:
      app: fleetlift-server
  template:
    metadata:
      labels:
        app: fleetlift-server
    spec:
      containers:
        - name: server
          image: ghcr.io/your-org/fleetlift-server:latest
          ports:
            - containerPort: 8080
              name: http
          envFrom:
            - secretRef:
                name: fleetlift-secrets
          env:
            - name: LISTEN_ADDR
              value: ":8080"
          readinessProbe:
            httpGet:
              path: /api/health
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          livenessProbe:
            httpGet:
              path: /api/health
              port: 8080
            initialDelaySeconds: 10
            periodSeconds: 30
          resources:
            requests:
              cpu: 250m
              memory: 256Mi
            limits:
              cpu: "1"
              memory: 512Mi
---
apiVersion: v1
kind: Service
metadata:
  name: fleetlift-server
  namespace: fleetlift
spec:
  selector:
    app: fleetlift-server
  ports:
    - port: 80
      targetPort: 8080
      name: http
  type: ClusterIP
```

### Worker Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fleetlift-worker
  namespace: fleetlift
  labels:
    app: fleetlift-worker
spec:
  replicas: 3
  selector:
    matchLabels:
      app: fleetlift-worker
  template:
    metadata:
      labels:
        app: fleetlift-worker
    spec:
      containers:
        - name: worker
          image: ghcr.io/your-org/fleetlift-worker:latest
          envFrom:
            - secretRef:
                name: fleetlift-secrets
          env:
            - name: TEMPORAL_ADDRESS
              value: "temporal-frontend.temporal:7233"
            - name: AGENT_IMAGE
              value: "claude-code:latest"
          resources:
            requests:
              cpu: 500m
              memory: 512Mi
            limits:
              cpu: "2"
              memory: 1Gi
```

Workers are stateless. They do not expose any ports. They connect outbound to Temporal (gRPC) and PostgreSQL.

### Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: fleetlift
  namespace: fleetlift
  annotations:
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - fleetlift.example.com
      secretName: fleetlift-tls
  rules:
    - host: fleetlift.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: fleetlift-server
                port:
                  number: 80
```

The long proxy timeouts are required for SSE streaming connections.

### PostgreSQL

For production, use a managed PostgreSQL service (AWS RDS, Google Cloud SQL, Azure Database for PostgreSQL) rather than running PostgreSQL in Kubernetes.

Requirements:
- PostgreSQL 15+
- `gen_random_uuid()` support (included in pg 13+)
- At least 1 GB storage to start; grows with run/step history
- SSL/TLS enabled

If you must run PostgreSQL in-cluster, use a StatefulSet with persistent volumes, or an operator such as CloudNativePG or Zalando Postgres Operator.

### Temporal

**Recommended: Temporal Cloud.** Set `TEMPORAL_ADDRESS` to your Temporal Cloud endpoint and configure mTLS certificates. This eliminates operational overhead.

**Self-hosted alternative:** Deploy Temporal using the [official Helm chart](https://github.com/temporalio/helm-charts):

```bash
helm repo add temporal https://go.temporal.io/helm-charts
helm install temporal temporal/temporal \
  --namespace temporal \
  --create-namespace \
  --set server.config.persistence.default.sql.driver=postgres \
  --set server.config.persistence.default.sql.host=your-db-host \
  --values temporal-values.yaml
```

Self-hosted Temporal requires its own PostgreSQL database (separate from FleetLift's).

---

## Database Setup

### Schema Migrations

The FleetLift server runs database migrations automatically on startup via `db.Migrate()`. No manual migration step is needed.

The migration files live in `internal/db/migrations/`. The server applies them in order and tracks which have been applied.

### Initial Setup

1. Create the PostgreSQL database and user:

```sql
CREATE USER fleetlift WITH PASSWORD 'a-strong-password';
CREATE DATABASE fleetlift OWNER fleetlift;
```

2. Set `DATABASE_URL` on both the server and worker:

```
postgres://fleetlift:a-strong-password@db-host:5432/fleetlift?sslmode=require
```

3. Start the server. It will apply all pending migrations.

---

## Scaling

### Server Replicas

The server is stateless and can be horizontally scaled behind a load balancer. SSE connections are long-lived, so ensure your load balancer supports HTTP/1.1 keep-alive and does not aggressively time out idle connections.

Recommendations:
- Start with 2 replicas for high availability
- Scale based on active SSE connection count and request rate
- Use sticky sessions if you encounter SSE reconnection issues (not strictly required)

### Worker Replicas

Each worker polls Temporal for tasks. Adding more workers increases throughput for concurrent workflow executions.

Recommendations:
- Start with 2-3 replicas
- Scale based on Temporal task queue backlog depth
- Each worker can handle multiple concurrent activities (configured via Temporal worker options)
- Workers are CPU/memory-bound during agent execution orchestration

### Temporal Scaling

- **Temporal Cloud:** Scales automatically.
- **Self-hosted:** Scale Temporal frontend, history, and matching services independently. Refer to [Temporal's production deployment guide](https://docs.temporal.io/cluster-deployment-guide).

---

## Observability

### Prometheus Metrics

FleetLift exposes Prometheus metrics via the `internal/metrics` package:

| Metric | Type | Description |
|--------|------|-------------|
| `fleetlift_activity_duration_seconds` | Histogram | Activity execution duration by activity name |
| `fleetlift_activity_total` | Counter | Total activity executions by name and status |
| `fleetlift_prs_created_total` | Counter | Total pull requests created |
| `fleetlift_sandbox_provision_duration_seconds` | Histogram | Sandbox provisioning latency |

Configure your Prometheus instance to scrape the server and worker pods. If using the Prometheus Operator:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: fleetlift
  namespace: fleetlift
spec:
  selector:
    matchLabels:
      app: fleetlift-server
  podMetricsEndpoints:
    - port: http
      path: /metrics
```

### Structured Logging

FleetLift uses Go's `slog` package for structured JSON logging. All log output goes to stdout, compatible with any log aggregation system (Loki, CloudWatch, Datadog, etc.).

Key fields in log entries:
- `run_id` — correlates logs to a specific workflow run
- `step_id` — correlates logs to a specific step
- `team_id` — identifies the tenant

### Temporal Observability

- Use Temporal Web UI to inspect workflow state, history, and retries
- Temporal exposes its own Prometheus metrics for queue depth, latency, and error rates
- Set up alerts on `temporal_workflow_failed` and task queue backlog

---

## Security Hardening

### TLS Termination

Terminate TLS at your load balancer or ingress controller. The FleetLift server itself listens on plain HTTP. Example with nginx ingress:

```yaml
annotations:
  nginx.ingress.kubernetes.io/ssl-redirect: "true"
  nginx.ingress.kubernetes.io/force-ssl-redirect: "true"
```

Ensure the GitHub OAuth callback URL uses `https://`.

### Network Policies

Restrict traffic between components:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: fleetlift-server
  namespace: fleetlift
spec:
  podSelector:
    matchLabels:
      app: fleetlift-server
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - namespaceSelector: {}  # Allow ingress controller
      ports:
        - port: 8080
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: postgres
      ports:
        - port: 5432
    - to:  # DNS
        - namespaceSelector: {}
      ports:
        - port: 53
          protocol: UDP
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: fleetlift-worker
  namespace: fleetlift
spec:
  podSelector:
    matchLabels:
      app: fleetlift-worker
  policyTypes:
    - Ingress
    - Egress
  ingress: []  # Workers accept no inbound connections
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: postgres
      ports:
        - port: 5432
    - to:  # Temporal
        - namespaceSelector:
            matchLabels:
              name: temporal
      ports:
        - port: 7233
    - to:  # OpenSandbox + Anthropic API (external)
        - ipBlock:
            cidr: 0.0.0.0/0
      ports:
        - port: 443
    - to:  # DNS
        - namespaceSelector: {}
      ports:
        - port: 53
          protocol: UDP
```

### Secret Rotation

- **JWT_SECRET:** Rotate by deploying a new value. Existing JWTs will be invalidated, forcing users to re-authenticate. Coordinate with a maintenance window.
- **CREDENTIAL_ENCRYPTION_KEY:** Cannot be rotated without re-encrypting all stored credentials. Plan a migration path if rotation is needed: decrypt all credentials with the old key, re-encrypt with the new key, then update the environment variable.
- **GITHUB_CLIENT_SECRET:** Rotate in GitHub OAuth settings, then update the environment variable. No data migration needed.
- **ANTHROPIC_API_KEY / OPENSANDBOX_API_KEY:** Rotate by generating a new key with the provider and updating the environment variable.

### Credential Encryption

FleetLift encrypts stored credentials (API keys, tokens) at rest using AES-256-GCM. The encryption key is provided via `CREDENTIAL_ENCRYPTION_KEY`. This protects credential values in the PostgreSQL database. Ensure:

- The encryption key is stored in a secrets manager, not in plaintext config files
- Database backups are also encrypted at rest
- Access to the `credentials` table is restricted at the database level

### Sandboxed Execution

Agent code runs inside OpenSandbox containers with:
- Capability dropping (no privileged operations)
- Network controls (configurable per sandbox)
- Ephemeral filesystems (destroyed after execution)
- No access to host resources

---

## Backup and Recovery

### PostgreSQL Backups

Back up the FleetLift database regularly. At minimum:

- **Continuous WAL archiving** for point-in-time recovery (PITR)
- **Daily pg_dump** for logical backups
- **Managed DB snapshots** if using a cloud provider

```bash
# Logical backup
pg_dump -Fc -h db-host -U fleetlift fleetlift > fleetlift_$(date +%Y%m%d).dump

# Restore
pg_restore -h db-host -U fleetlift -d fleetlift fleetlift_20260314.dump
```

Critical tables to protect:
- `teams`, `users`, `team_members` — identity and access
- `credentials` — encrypted API keys (useless without `CREDENTIAL_ENCRYPTION_KEY`)
- `runs`, `step_runs` — execution history
- `workflow_templates` — custom workflow definitions

### Temporal Persistence

Temporal stores workflow state in its own database. If self-hosting:

- Back up Temporal's PostgreSQL database with the same rigor as FleetLift's
- Temporal can replay workflows from history, so a brief data loss window is recoverable
- With Temporal Cloud, persistence is managed for you

### Disaster Recovery

1. Restore PostgreSQL from backup
2. Restore Temporal's database (or rely on Temporal Cloud)
3. Deploy server and worker — they are stateless and will reconnect
4. Running workflows will resume from their last checkpoint in Temporal

---

## Multi-Tenant Setup

FleetLift supports multi-tenant team isolation. Each team has its own runs, credentials, and workflow templates.

### Provisioning a Team

Teams are stored in the `teams` table. To create a new team:

```sql
INSERT INTO teams (name, slug) VALUES ('Engineering', 'engineering');
```

### Adding Users to a Team

```sql
-- Find the user (created on first OAuth login)
SELECT id, name, email FROM users WHERE email = 'alice@example.com';

-- Add them to a team
INSERT INTO team_members (team_id, user_id, role)
VALUES ('<team-uuid>', '<user-uuid>', 'admin');
```

Roles: `admin` or `member`.

### Team Isolation

- All API requests require a team context (via `X-Team-ID` header or `?team_id=` parameter)
- The server validates team membership against JWT claims
- Credentials are scoped to a team and cannot be accessed across team boundaries
- Runs and workflow templates are team-scoped

---

## Pre-Deployment Checklist

- [ ] PostgreSQL provisioned with SSL enabled
- [ ] Temporal cluster running (Cloud or self-hosted)
- [ ] OpenSandbox instance accessible from worker network
- [ ] All required secrets generated and stored securely
- [ ] GitHub OAuth app created with correct callback URL
- [ ] DNS and TLS certificates configured
- [ ] Network policies applied (Kubernetes)
- [ ] Prometheus scraping configured
- [ ] PostgreSQL backup schedule configured
- [ ] At least one team and admin user provisioned
- [ ] Load balancer configured with SSE-compatible timeouts (>= 60s)
