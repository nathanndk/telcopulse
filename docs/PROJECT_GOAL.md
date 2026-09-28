# Project Goal — TelcoPulse

Build **TelcoPulse**, an enterprise-grade internal observability and incident operations platform inspired by a large telecommunications ITOC environment.

The product should simulate how an enterprise telecom operations team monitors digital services, detects incidents, investigates failures, correlates metrics/logs/traces/deployments, escalates issues, performs root-cause analysis, and documents resolution.

This must feel like a **real internal enterprise platform**, not a portfolio dashboard or simple CRUD application.

---

## 1. Product Goals

TelcoPulse must support the following end-to-end operational workflow:

```text
Customer Transaction
        ↓
Microservices
        ↓
Metrics / Logs / Traces / Events
        ↓
Monitoring
        ↓
Anomaly / Alert
        ↓
Incident
        ↓
Investigation
        ↓
Evidence Correlation
        ↓
Escalation
        ↓
Root Cause Analysis
        ↓
Mitigation
        ↓
Resolution
        ↓
Postmortem / Action Items
```

The application should allow an operator to answer:

- What service is currently unhealthy?
- What business transaction is affected?
- When did the degradation start?
- How many users/transactions are affected?
- Was there a recent deployment?
- What errors appear in logs?
- Which dependency is slow or failing?
- Is Kafka consumer lag increasing?
- What metrics correlate with the incident?
- What is the suspected root cause?
- What mitigation was applied?
- Has the service recovered?
- What follow-up actions need to be completed?

---

# 2. Core Technology Stack

Use these technologies unless there is a strong architectural reason not to.

## Frontend

```text
Next.js
TypeScript
App Router
Tailwind CSS
shadcn/ui
TanStack Query
TanStack Table
React Hook Form
Zod
Recharts
```

The frontend is purely the UI/application layer.

Do **not** move the main business backend into Next.js API routes.

---

## Backend

Use **Go** for backend services.

Initial services:

```text
api-gateway
auth-service
subscriber-service
package-service
payment-service
notification-service
incident-service
simulation-service
```

Prefer clean architecture and clear service boundaries.

Use:

```text
REST
gRPC where appropriate
Kafka for asynchronous events
```

---

## Data Layer

```text
PostgreSQL
Redis
Apache Kafka
```

PostgreSQL:
- customers
- packages
- transactions
- incidents
- RCA
- deployments
- action items
- audit logs

Redis:
- caching
- ephemeral state
- rate limiting when relevant

Kafka:
- transaction events
- payment events
- notification events
- observability/event simulation

---

# 3. Observability Stack

The application itself must be observable.

Use:

```text
Prometheus
Grafana
Splunk
Dynatrace
OpenTelemetry
```

Responsibilities:

### Prometheus

Application and infrastructure metrics.

Examples:

```text
http_requests_total
http_request_duration_seconds
transaction_total
transaction_success_total
transaction_failure_total
kafka_consumer_lag
database_connection_usage
```

### Grafana

Operational visualization for:

- success rate
- error rate
- TPS/RPS
- latency P50/P95/P99
- CPU
- memory
- Kafka lag
- service availability

### Splunk

Structured application and business transaction logs.

Logs should contain correlation fields such as:

```text
timestamp
service
environment
transaction_id
trace_id
span_id
msisdn_masked
status
error_code
duration_ms
version
```

The project should contain useful example SPL queries.

### Dynatrace

Use for enterprise-style:

- APM
- distributed tracing
- service topology
- service dependencies
- infrastructure/Kubernetes monitoring
- problem investigation

### OpenTelemetry

Use OpenTelemetry for vendor-neutral instrumentation.

Application instrumentation should not be tightly coupled to one observability vendor.

---

# 4. Infrastructure

Use:

```text
Docker
Docker Compose
Kubernetes
Helm
OpenShift-compatible manifests
```

Docker Compose is the local development environment.

Kubernetes is the production-like environment.

The application must include:

```text
Deployment
Service
ConfigMap
Secret
Ingress
readinessProbe
livenessProbe
resource requests
resource limits
```

The architecture should make it possible to reproduce:

```text
Pod crash
OOMKilled
CrashLoopBackOff
CPU saturation
resource exhaustion
replica unavailable
bad deployment
```

---

# 5. CI/CD

Model the pipeline around an enterprise Telco-style toolchain:

```text
GitLab
GitLab CI/CD
SonarQube
Docker
JFrog Container Registry / Artifactory-compatible registry
Jenkins
Helm
Kubernetes / OpenShift
```

Desired pipeline:

```text
Developer
    ↓
Merge Request
    ↓
GitLab CI
    ↓
Lint
    ↓
Unit Tests
    ↓
Integration Tests
    ↓
SonarQube Quality Gate
    ↓
Docker Build
    ↓
Push Image to Registry
    ↓
Jenkins Deployment
    ↓
DEV
    ↓
STAGING
    ↓
Smoke / k6 Test
    ↓
PRODUCTION
    ↓
Observability Validation
```

Deployment events must be visible/correlatable with incidents.

---

# 6. Load & Failure Simulation

Use:

```text
k6
custom incident injector
```

The system must intentionally support failure scenarios.

Implement at least:

### Incident 01 — Application Failure

```text
Success Rate
99.9% → 91%
```

Investigation:

```text
Grafana → detect
Splunk → inspect errors
Dynatrace → inspect dependency
RCA
```

### Incident 02 — Database Latency

```text
P95
300 ms → 4.2 s
```

Potential root cause:

```text
connection pool exhaustion
slow query
database saturation
```

### Incident 03 — Kafka Consumer Lag

```text
Producer throughput > Consumer throughput
        ↓
Consumer lag increases
        ↓
Transactions delayed
```

### Incident 04 — Kubernetes OOM

```text
memory usage ↑
        ↓
limit reached
        ↓
OOMKilled
        ↓
Pod restart
        ↓
Transaction errors
```

### Incident 05 — Business Transaction Failure

HTTP can still return `200`, while the business transaction fails.

Example:

```json
{
  "http_status": 200,
  "transaction_status": "FAILED",
  "error_code": "PACKAGE_ACTIVATION_FAILED"
}
```

The monitoring platform must detect the drop in **business success rate**.

### Incident 06 — Bad Deployment

```text
v1.3.9
  ↓ deploy
v1.4.0
  ↓
error rate increases
  ↓
incident
  ↓
deployment correlation
  ↓
rollback
```

---

# 7. Main Product Modules

The frontend should contain these top-level areas:

```text
Dashboard
Services
Incidents
Transactions
Deployments
RCA
Customer Simulator
Settings
```

---

# 8. ITOC Overview

Create an enterprise operations overview.

Primary information hierarchy:

```text
System Status
    ↓
Business KPIs
    ↓
Service Health
    ↓
Active Incidents
    ↓
Metrics / Trends
    ↓
Deployments
```

Top KPIs:

```text
Overall Success Rate
Transactions / min
Active Incidents
P95 Latency
```

Service table:

```text
Login
Package
Purchase
Payment
Notification
```

Statuses:

```text
Healthy
Degraded
Critical
Unknown
```

Do not overload the page with decorative cards.

Optimize for fast operator scanning.

---

# 9. Incident Management

Incident lifecycle:

```text
Detected
Acknowledged
Investigating
Identified
Mitigating
Monitoring
Resolved
Postmortem
```

Each incident should contain:

```text
Incident ID
Title
Severity
Status
Environment
Affected Service
Owner
Created Time
Detected Time
Acknowledged Time
Resolved Time
Impact
Affected Transactions
Affected Users
Error Rate
Success Rate
Latency
Related Deployment
Evidence
Timeline
Root Cause
Mitigation
Resolution
Action Items
Audit History
```

Severity:

```text
SEV-1
SEV-2
SEV-3
SEV-4
```

---

# 10. Incident Investigation Page

The incident detail page must combine operational evidence.

Layout should include:

```text
Incident Header

Severity
Status
Service
Opened
Owner
Last Update

↓

Incident Overview
Timeline

↓

Metrics Evidence
Logs Evidence

↓

Service Dependency / Trace Flow

↓

Root Cause Analysis

↓

Action Items
```

Provide quick links:

```text
Open Grafana
Open Splunk
Open Dynatrace
```

These should eventually deep-link into the relevant dashboard/query/trace rather than only opening the product homepage.

---

# 11. Customer Simulator

Build a customer transaction simulator.

Customer journey:

```text
Select Customer
     ↓
Select Package
     ↓
Configure Payment
     ↓
Run Transaction
     ↓
View Result
```

The user should be able to configure:

```text
MSISDN
Package
Payment Method
Environment
```

Example payment methods:

```text
Pulsa
E-Wallet
Credit Card
Virtual Account
```

Actions:

```text
Start Synthetic Transaction
Inject Incident
Replay Failed Transaction
```

Each simulation must generate:

```text
transaction_id
trace_id
logs
metrics
Kafka events
service calls
database operations
```

so the transaction can be investigated through the observability stack.

---

# 12. UI / UX Design System

Use a polished **enterprise internal-tool aesthetic**.

Use shadcn/ui as the primitive component system.

Do not make it look like an untouched shadcn template.

Build domain-specific reusable components.

Examples:

```text
ServiceHealthBadge
SeverityBadge
IncidentStatusBadge
MetricCard
MetricTrend
ServiceHealthTable
IncidentTable
IncidentTimeline
TraceFlow
EvidencePanel
RCASection
DeploymentBadge
```

---

# 13. Visual Identity

Use a **Telkomsel-inspired palette**, but do not copy official proprietary design assets or logos.

Base:

```text
deep navy
charcoal
off-white
cool gray
```

Primary brand accent:

```text
crimson / telecom red
```

Secondary accent:

```text
coral
warm orange
amber
```

Semantic colors:

```text
Healthy / Success = Green
Warning / Degraded = Amber
Critical / Error = Red
Info = Blue
Unknown = Neutral gray
```

Do not use excessive gradients or glow.

This should feel like an internal enterprise operations platform.

---

# 14. UX Requirements

The application must support:

```text
responsive layout
keyboard navigation
accessible contrast
loading states
empty states
error states
skeleton states
toast notifications
confirmation dialogs
pagination
sorting
filtering
saved views
column visibility
sticky table headers
search
command palette
```

Tables must support large datasets.

---

# 15. Enterprise Requirements

Plan the architecture for:

### RBAC

Roles:

```text
Viewer
Operator
Incident Commander
Engineer
Administrator
```

Actions must depend on role.

Example:

A Viewer should not be able to:

```text
resolve incident
change severity
assign owner
inject failure
```

---

### Audit Log

Record operations such as:

```text
incident created
severity changed
owner assigned
incident escalated
mitigation updated
incident resolved
action item completed
```

Store:

```text
actor
action
timestamp
old_value
new_value
resource
```

---

### Security

Include:

```text
SSO-ready authentication architecture
RBAC
secure sessions
rate limiting
input validation
CSRF protection where relevant
secret management
PII masking
audit trail
```

MSISDN must be masked when shown in observability data unless the role explicitly permits otherwise.

Example:

```text
62812*****123
```

---

# 16. Code Quality

Code must be production-oriented.

Avoid:

```text
huge components
duplicated code
hardcoded mock values scattered throughout UI
business logic inside presentation components
untyped API responses
silent failures
```

Prefer:

```text
clear domain models
service layer
repository layer
typed API contracts
reusable UI primitives
centralized configuration
structured logging
error handling
tests
```

---

# 17. Repository Structure

Prefer a monorepo structure similar to:

```text
telcopulse/
│
├── apps/
│   ├── web/
│   └── simulator/
│
├── services/
│   ├── api-gateway/
│   ├── auth-service/
│   ├── subscriber-service/
│   ├── package-service/
│   ├── payment-service/
│   ├── notification-service/
│   ├── incident-service/
│   └── simulation-service/
│
├── packages/
│   ├── ui/
│   ├── types/
│   └── config/
│
├── infrastructure/
│   ├── docker/
│   ├── kubernetes/
│   ├── helm/
│   ├── openshift/
│   ├── prometheus/
│   ├── grafana/
│   ├── otel/
│   ├── splunk/
│   └── jenkins/
│
├── scripts/
│
├── tests/
│
└── docs/
```

Adjust this structure if needed, but keep responsibilities clean.

---

# 18. Implementation Strategy

Do not try to implement everything at once.

Work incrementally.

## Phase 1 — Application Foundation

Build:

```text
Next.js frontend
Go API gateway
PostgreSQL
basic customer/package/payment flow
Docker Compose
```

Goal:

A complete customer transaction works end-to-end.

---

## Phase 2 — Microservices & Kafka

Split responsibilities into services.

Introduce:

```text
Kafka
Redis
asynchronous notification flow
transaction event model
```

---

## Phase 3 — Observability

Add:

```text
OpenTelemetry
Prometheus
Grafana
structured logging
Splunk integration
Dynatrace-compatible telemetry
```

Every transaction must be traceable.

---

## Phase 4 — ITOC Console

Implement:

```text
Dashboard
Service Health
Transactions
Incident List
Incident Detail
Timeline
Evidence
RCA
```

---

## Phase 5 — Failure Simulator

Implement failure injection scenarios.

Examples:

```text
DB latency
DB timeout
Kafka lag
HTTP 500
dependency timeout
memory leak
CPU saturation
business transaction failure
```

---

## Phase 6 — Kubernetes

Deploy services to Kubernetes.

Implement:

```text
Deployment
Service
Ingress
ConfigMap
Secret
resource limits
health probes
```

---

## Phase 7 — CI/CD

Implement:

```text
GitLab CI
SonarQube
Docker image build
JFrog-compatible registry
Jenkins deployment
Helm
```

---

## Phase 8 — Enterprise Features

Implement:

```text
RBAC
audit trail
saved filters
advanced tables
deployment correlation
incident ownership
postmortem
action items
```

---

# 19. Definition of Done

The project is successful when the following demo works:

1. Start the platform.
2. Run a customer package-purchase transaction.
3. Observe a successful transaction.
4. Inject a payment/database incident.
5. Success rate decreases.
6. Grafana/Prometheus detect degradation.
7. Splunk contains corresponding structured errors.
8. Traces show the affected dependency.
9. TelcoPulse creates/displays an active incident.
10. Operator investigates metrics, logs, and traces.
11. Operator identifies the root cause.
12. Operator applies mitigation.
13. Metrics recover.
14. Incident moves to Monitoring.
15. Operator resolves the incident.
16. RCA and action items are recorded.
17. Audit log records the incident lifecycle.

The final experience should make it possible to demonstrate:

```text
Detect
Investigate
Correlate
Escalate
Mitigate
Resolve
Learn
```

---

# Instructions for Codex

Before changing code:

1. Inspect the existing repository.
2. Understand current architecture and conventions.
3. Do not rewrite working code unnecessarily.
4. Identify the smallest coherent implementation step.
5. Create or update a concise implementation plan.
6. Implement the feature completely.
7. Add or update tests where appropriate.
8. Run lint/typecheck/tests/build for affected components.
9. Fix issues introduced by the implementation.
10. Summarize exactly what changed.

When requirements are ambiguous, prefer sensible enterprise-grade defaults that fit this architecture rather than introducing unnecessary complexity.

Prioritize:

```text
correctness
maintainability
observability
operator usability
realistic enterprise workflows
```

over:

```text
visual gimmicks
premature abstraction
unnecessary dependencies
over-engineering
```

Treat this document as the architectural and product goal for the TelcoPulse repository.