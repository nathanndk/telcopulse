# TelcoPulse

## Enterprise Telecom ITOC Observability & Incident Operations Platform

Build **TelcoPulse**, an enterprise-grade internal platform that simulates the operational environment of a large telecommunications company's **IT Service Observability Center (ITOC)**.

TelcoPulse must not be a simple monitoring dashboard, CRUD application, or portfolio visualization.

It must behave like a realistic internal operations platform used by engineers and ITOC operators to:

- monitor digital services
- monitor business transactions
- detect service degradation
- investigate incidents
- correlate metrics, logs, traces, infrastructure, deployments, and events
- identify affected customers and transactions
- escalate incidents
- perform root-cause analysis
- apply mitigation
- verify recovery
- resolve incidents
- document postmortems
- track corrective action items
- audit operational actions

The product should demonstrate a realistic enterprise telecom workflow from customer transaction to incident resolution.

---

# 1. Core Product Goal

The system must support this lifecycle:

```text
Customer
   ↓
Digital Service
   ↓
Microservices
   ↓
Database / Cache / Kafka / External Dependencies
   ↓
Telemetry
   ↓
Metrics + Logs + Traces + Events
   ↓
Monitoring
   ↓
Anomaly Detection
   ↓
Alert
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
Recovery Validation
   ↓
Resolution
   ↓
Postmortem
   ↓
Action Items
```

An operator should be able to determine:

```text
What happened?

When did it start?

Which service is affected?

Which business transaction is affected?

How many transactions failed?

How many customers may be affected?

What is the current success rate?

What is the error rate?

What is the latency?

Was there a recent deployment?

Are logs showing a recurring error?

Which dependency is failing?

Is Kafka lag increasing?

Is Kubernetes unhealthy?

Is the problem application-level or infrastructure-level?

What is the suspected root cause?

What mitigation has been applied?

Has service health recovered?

What must be done to prevent recurrence?
```

---

# 2. Product Name

Use:

```text
TelcoPulse
```

Product positioning:

```text
Observe
Resolve
Keep the world connected
```

This is a fictional internal enterprise telecom platform.

Do not claim that TelcoPulse itself is an official Telkomsel product.

---

# 3. Monorepo Architecture

Prefer a monorepo.

Suggested structure:

```text
telcopulse/
│
├── apps/
│   ├── web/
│   └── customer-simulator/
│
├── services/
│   ├── api-gateway/
│   ├── auth-service/
│   ├── subscriber-service/
│   ├── package-service/
│   ├── payment-service/
│   ├── notification-service/
│   ├── incident-service/
│   ├── simulation-service/
│   └── deployment-service/
│
├── packages/
│   ├── ui/
│   ├── types/
│   ├── config/
│   └── observability/
│
├── infrastructure/
│   ├── docker/
│   ├── kubernetes/
│   ├── openshift/
│   ├── helm/
│   ├── prometheus/
│   ├── grafana/
│   ├── splunk/
│   ├── datadog/
│   ├── dynatrace/
│   ├── otel/
│   ├── kafka/
│   ├── jenkins/
│   └── gitlab/
│
├── scripts/
│
├── tests/
│   ├── integration/
│   ├── e2e/
│   ├── load/
│   └── chaos/
│
├── docs/
│   ├── architecture/
│   ├── runbooks/
│   ├── incidents/
│   └── postmortems/
│
└── README.md
```

The structure may evolve, but responsibilities must remain clear.

---

# 4. Frontend Stack

Use:

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

Next.js is primarily the frontend application.

Do not move core domain/business logic into Next.js route handlers simply because it is convenient.

Core APIs must live in Go services.

---

# 5. Frontend Responsibilities

The frontend must provide the operator experience.

Primary modules:

```text
Dashboard

Services

Incidents

Transactions

Deployments

RCA

Customer Simulator

Audit Log

Settings
```

Eventually also support:

```text
Saved Views

Command Palette

Global Search

Notification Center

User Profile

Role Management
```

---

# 6. UI Component Strategy

Use `shadcn/ui` as the primitive component foundation.

Do not leave the application looking like a default shadcn template.

Create domain-specific components.

Examples:

```text
MetricCard

MetricTrend

ServiceHealthBadge

ServiceHealthTable

SeverityBadge

IncidentStatusBadge

IncidentTable

IncidentTimeline

IncidentEvidence

TraceFlow

DependencyGraph

LogEvidenceTable

DeploymentBadge

DeploymentTimeline

RCASection

ActionItemList

TransactionStatusBadge

TransactionTable

SLOCard

ErrorBudgetCard

AuditLogTable
```

---

# 7. UI / UX Direction

The application should look like a serious internal enterprise operations platform.

Avoid:

```text
excessive gradients
excessive glowing cards
marketing-style landing page layouts
huge decorative components
unnecessary animation
overly rounded everything
dashboard cards with no operational purpose
```

Prefer:

```text
strong hierarchy
high information density
strict grid
clear alignment
consistent spacing
fast scanability
good table UX
semantic status colors
compact but readable components
operator-first workflows
```

Use approximately a:

```text
12-column layout
```

for major dashboard screens.

---

# 8. Visual Identity

Use a **Telkomsel-inspired** palette.

Do not reproduce official proprietary brand assets or copy an official application pixel-for-pixel.

Primary accent:

```text
Crimson / telecom red
```

Secondary accents:

```text
coral
warm orange
amber
```

Base UI:

```text
deep navy
charcoal
dark slate
off-white
cool gray
```

Semantic colors:

```text
Healthy     Green

Success     Green

Degraded    Amber

Warning     Amber

Critical    Red

Error       Red

Information Blue

Unknown     Gray
```

Red should function both as a brand accent and as a semantic critical color, but avoid using it everywhere.

---

# 9. Backend Stack

Use:

```text
Go
```

for backend services.

Initial service architecture:

```text
api-gateway

auth-service

subscriber-service

package-service

payment-service

notification-service

incident-service

simulation-service

deployment-service
```

Use clean service boundaries.

Do not create microservices merely for visual complexity.

Each service should own a clear domain responsibility.

---

# 10. API Communication

Use:

```text
REST
```

for public/internal frontend-facing APIs initially.

Use:

```text
gRPC
```

for appropriate service-to-service communication where it provides value.

Use:

```text
Kafka
```

for asynchronous/event-driven communication.

---

# 11. Core Domain Services

## API Gateway

Responsibilities:

```text
routing
authentication validation
request correlation
rate limiting
trace propagation
API aggregation where appropriate
```

---

## Auth Service

Responsibilities:

```text
authentication
sessions
user identity
roles
permissions
SSO-ready architecture
```

---

## Subscriber Service

Responsibilities:

```text
subscriber profile
customer type
MSISDN
account state
customer segment
```

Never expose raw sensitive customer information unnecessarily.

---

## Package Service

Responsibilities:

```text
package catalog
package eligibility
package purchase state
package activation
```

---

## Payment Service

Responsibilities:

```text
payment request
payment validation
payment processing
payment status
payment provider interaction
```

---

## Notification Service

Responsibilities:

```text
transaction notification
purchase confirmation
failure notification
event-driven messages
```

---

## Incident Service

Responsibilities:

```text
incident lifecycle
incident severity
status
owner
timeline
evidence
RCA
mitigation
resolution
action items
audit events
```

---

## Simulation Service

Responsibilities:

```text
synthetic customer journeys
incident injection
failure scenarios
transaction replay
test-data generation
```

---

# 12. Data Stack

Use:

```text
PostgreSQL
Redis
Apache Kafka
```

---

# 13. PostgreSQL

Store durable domain data.

Examples:

```text
users

roles

permissions

customers

packages

transactions

transaction_steps

incidents

incident_events

incident_evidence

rcas

action_items

deployments

audit_logs

saved_views

simulations
```

Use migrations.

Do not manually maintain database schema.

---

# 14. Redis

Use where appropriate for:

```text
cache

temporary state

distributed lock

rate limiting

short-lived simulation state
```

Do not use Redis as the primary durable database.

---

# 15. Kafka

Kafka must be a real architectural component, not a decorative container.

Example topics:

```text
transaction.requested

transaction.completed

transaction.failed

payment.requested

payment.completed

payment.failed

package.activation.requested

package.activation.completed

notification.requested

incident.detected

deployment.completed
```

Include:

```text
producer
consumer
consumer groups
consumer lag
retry behavior
dead-letter topics where appropriate
```

Kafka health must be observable.

---

# 16. Customer Journey

Implement a realistic synthetic telecom journey:

```text
Login
  ↓
Load Subscriber
  ↓
Browse Packages
  ↓
Select Package
  ↓
Create Purchase
  ↓
Payment
  ↓
Package Activation
  ↓
Notification
  ↓
Success
```

Each transaction must receive:

```text
transaction_id

trace_id
```

and optionally:

```text
correlation_id
```

---

# 17. Customer Simulator

Create a dedicated Customer Simulator.

Goal:

Generate realistic transactions that can later be investigated through the observability stack.

Workflow:

```text
1. Select Customer

2. Select Package

3. Configure Payment

4. Run Simulation

5. View Results
```

Fields:

```text
MSISDN

Package

Payment Method

Environment
```

Payment options:

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

---

# 18. Simulation Results

Each simulated transaction should produce:

```text
transaction record

Kafka events

application logs

metrics

distributed traces

database activity

dependency calls
```

UI should show:

```text
Transaction ID

Timestamp

Masked MSISDN

Package

Payment Method

Environment

Status

Duration

Trace ID
```

Failed transactions should provide:

```text
Investigate
```

action.

---

# 19. Observability Philosophy

The application itself must be deeply observable.

Use:

```text
OpenTelemetry
Prometheus
Grafana
Splunk
Dynatrace
Datadog
```

Each tool must have an explicit purpose.

Do not include technologies only to make the architecture diagram look complicated.

---

# 20. OpenTelemetry

OpenTelemetry should be the main vendor-neutral instrumentation layer.

Instrument:

```text
HTTP requests

gRPC requests

database calls

Kafka producers

Kafka consumers

service dependencies

custom business transactions
```

Propagate context across service boundaries.

Where practical use:

```text
trace_id
span_id
```

inside structured application logs.

---

# 21. Prometheus

Use Prometheus primarily for metrics collection.

Examples:

```text
http_requests_total

http_request_duration_seconds

transaction_total

transaction_success_total

transaction_failure_total

transaction_duration_seconds

payment_total

payment_failure_total

package_activation_total

kafka_consumer_lag

db_connection_usage

db_query_duration_seconds

service_health

pod_restart_total
```

Also create business KPIs.

Example:

```text
business_transaction_success_rate
```

HTTP `200` must not automatically equal business success.

---

# 22. Grafana

Grafana is the operational metrics visualization layer.

Dashboards should include:

```text
Overall Service Health

Business Success Rate

Transactions per Second

Requests per Second

Error Rate

P50 Latency

P95 Latency

P99 Latency

Kafka Consumer Lag

Database Health

CPU

Memory

Pod Restarts

Deployment Events
```

---

# 23. Splunk

Splunk is the main log and business transaction investigation tool.

Produce structured logs.

Recommended fields:

```text
timestamp

environment

service

version

transaction_id

trace_id

span_id

masked_msisdn

status

error_code

duration_ms

http_status

pod

namespace

deployment
```

Example structured event:

```json
{
  "service": "payment-service",
  "environment": "production",
  "transaction_id": "TX-294102",
  "trace_id": "abc123",
  "msisdn": "62812*****123",
  "status": "FAILED",
  "error_code": "DB_TIMEOUT",
  "duration_ms": 2410,
  "version": "1.4.0"
}
```

---

# 24. SPL

Include realistic SPL examples.

Examples:

```text
failed transaction investigation

error-code distribution

success-rate calculation

customer transaction tracing

version-specific errors

incident-period correlation
```

Example:

```spl
index=telcopulse
service="payment-service"
status="FAILED"
| stats count by error_code
| sort -count
```

---

# 25. Dynatrace

Use Dynatrace conceptually for:

```text
APM

distributed tracing

service flow

dependency analysis

topology

Kubernetes monitoring

application performance

problem investigation

root cause investigation
```

TelcoPulse incident pages should eventually be able to deep-link to the relevant Dynatrace trace or context.

---

# 26. Datadog

Datadog must now be represented as a real operational tool.

Use it for a defined subset such as:

```text
Kubernetes monitoring

container monitoring

node monitoring

infrastructure metrics

APM

runtime monitoring

deployment markers

monitors

alerting
```

Do not duplicate every Grafana or Dynatrace capability.

A realistic investigation may look like:

```text
Grafana
    ↓
Business success rate degradation detected

Splunk
    ↓
DB_TIMEOUT found in payment logs

Dynatrace
    ↓
Trace shows payment database dependency

Datadog
    ↓
Pod memory / connection / Kubernetes condition correlated

TelcoPulse
    ↓
Incident evidence collected

RCA
```

---

# 27. Observability Integration in TelcoPulse

TelcoPulse itself should not try to replace all observability tools.

It acts as the:

```text
ITOC operational layer
```

that correlates information.

Provide actions such as:

```text
Open in Grafana

Open in Splunk

Open in Dynatrace

Open in Datadog
```

Eventually these should be deep links into relevant context.

---

# 28. Dashboard / ITOC Overview

Primary page:

```text
ITOC Overview
```

Top level hierarchy:

```text
Global System State

↓

Business KPIs

↓

Service Health

↓

Active Incidents

↓

Performance Trends

↓

Deployment Activity
```

KPIs:

```text
Overall Success Rate

Transactions / min

Active Incidents

P95 Latency
```

---

# 29. Service Health

Display:

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

Show:

```text
success rate

P95 latency

error rate where useful
```

---

# 30. Incident Lifecycle

Use:

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

Every status change must be timestamped.

---

# 31. Incident Data Model

Each incident should support:

```text
Incident ID

Title

Description

Severity

Status

Environment

Affected Service

Owner

Owning Team

Detection Time

Acknowledgement Time

Resolution Time

Duration

Business Impact

Affected Channels

Affected Regions

Affected Customers

Affected Transactions

Success Rate

Error Rate

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

---

# 32. Incident Severity

Support:

```text
SEV-1

SEV-2

SEV-3

SEV-4
```

Severity configuration should eventually be centralized.

Do not scatter severity logic throughout the frontend.

---

# 33. Incidents Page

Create an enterprise incident management screen.

Features:

```text
KPI summary

Filters

Search

Sorting

Pagination

Saved Views

Incident Table

Owner

Severity

Status

Opened Time

Actions
```

Filters:

```text
Status

Severity

Service

Environment

Owner

Time Range
```

---

# 34. Incident Detail

Required sections:

```text
Incident Header

Incident Metadata

Incident Overview

Timeline

Metrics Evidence

Logs Evidence

Trace / Dependency Evidence

Deployment Evidence

Root Cause Analysis

Action Items

Audit History
```

Header actions:

```text
Open Grafana

Open Splunk

Open Dynatrace

Open Datadog

Escalate

Assign Owner

Mark Resolved
```

---

# 35. Metrics Evidence

Show incident-period metrics.

Examples:

```text
Success Rate

Error Rate

Transaction Rate

P95 Latency

CPU

Memory

Consumer Lag
```

Metrics should have timestamps aligned to the incident period.

---

# 36. Logs Evidence

Display structured logs relevant to the incident.

Example columns:

```text
Timestamp

Level

Service

Message

Trace ID
```

Support log levels:

```text
ERROR

WARN

INFO
```

---

# 37. Service Dependency / Trace Flow

Create a service flow component.

Example:

```text
Customer
   ↓
API Gateway
   ↓
Package Service
   ↓
Payment Service
   ↓
PostgreSQL
```

Service nodes should display:

```text
health
latency
status
```

Example failure:

```text
Payment Service
980 ms

↓

PostgreSQL
TIMEOUT
```

---

# 38. RCA

RCA should not merely be a text textarea.

Structure it into:

```text
Root Cause

Contributing Factors

Mitigation

Resolution

Preventive Actions

Lessons Learned
```

---

# 39. Action Items

Each action item should support:

```text
Title

Priority

Owner

Status

Due Date

Incident Reference
```

Possible statuses:

```text
Open

In Progress

Blocked

Completed
```

Priorities:

```text
P1

P2

P3
```

---

# 40. Postmortem

Resolved major incidents should be able to generate a postmortem.

Structure:

```text
Summary

Impact

Timeline

Detection

Root Cause

Contributing Factors

Resolution

What Went Well

What Went Wrong

Action Items
```

---

# 41. Deployment Tracking

Track:

```text
Service

Version

Commit

Deployment ID

Environment

Timestamp

Deployer

Status
```

Statuses:

```text
Pending

Running

Completed

Failed

Rolled Back
```

Deployment events must be correlatable with incidents.

---

# 42. Bad Deployment Scenario

Example:

```text
payment-service v1.3.9
       ↓
deploy v1.4.0
       ↓
success rate drops
       ↓
error rate rises
       ↓
incident created
       ↓
deployment correlation
       ↓
rollback
       ↓
recovery
```

This scenario should be demonstrable.

---

# 43. Containerization

Use:

```text
Docker
```

Every deployable service should have an appropriate Dockerfile.

Do not run everything as root unnecessarily.

Use proper multi-stage builds where useful.

---

# 44. Local Development

Use:

```text
Docker Compose
```

Provide profiles where useful.

Example:

```text
app

observability

cicd
```

Possible commands:

```text
docker compose --profile app up

docker compose --profile observability up

docker compose --profile cicd up
```

Avoid requiring every heavy enterprise component to run simultaneously.

---

# 45. Kubernetes

Production-like environment should use:

```text
Kubernetes
```

Resources should include where appropriate:

```text
Namespace

Deployment

Service

ConfigMap

Secret

Ingress

HorizontalPodAutoscaler

PodDisruptionBudget

ServiceAccount
```

---

# 46. Kubernetes Health

Configure:

```text
readinessProbe

livenessProbe

startupProbe where appropriate

resource requests

resource limits
```

---

# 47. Kubernetes Failure Scenarios

Support simulations such as:

```text
Pod Crash

CrashLoopBackOff

OOMKilled

CPU Saturation

Memory Leak

Replica Unavailable

Misconfigured Environment Variable

Failed Readiness Probe

Network Dependency Failure
```

---

# 48. OpenShift

Keep Kubernetes manifests as portable as practical.

Also support an:

```text
OpenShift-compatible
```

deployment model.

OpenShift is the production-like enterprise target.

Do not make local development depend on having a paid OpenShift environment.

---

# 49. Helm

Use:

```text
Helm
```

for Kubernetes/OpenShift packaging where appropriate.

Support environment-specific values:

```text
dev

staging

production
```

---

# 50. CI/CD Philosophy

Use enterprise-style CI/CD.

Core tooling:

```text
GitLab

GitLab CI/CD

SonarQube

Docker

JFrog

Jenkins

Helm

Kubernetes / OpenShift
```

---

# 51. GitLab

GitLab is the primary source-control and CI platform in the simulated enterprise environment.

Use:

```text
Merge Requests

Protected branches

Code review

Pipeline status

GitLab Runner
```

---

# 52. CI Pipeline

Desired CI pipeline:

```text
Developer
    ↓
Merge Request
    ↓
GitLab CI
    ↓
Lint
    ↓
Typecheck
    ↓
Unit Tests
    ↓
Integration Tests
    ↓
SonarQube
    ↓
Quality Gate
    ↓
Docker Build
    ↓
Container Security Checks where practical
    ↓
Push Artifact
```

---

# 53. SonarQube

Use SonarQube for:

```text
bugs

code smells

duplication

coverage

security issues

quality gate
```

Quality gate failure should fail the appropriate pipeline stage.

---

# 54. JFrog

Use JFrog-compatible container/artifact storage.

Store:

```text
Docker images

Helm artifacts where appropriate

build artifacts
```

Local/free-compatible implementations are acceptable for development.

---

# 55. Jenkins

Use Jenkins primarily for enterprise-style deployment and operational automation.

Example jobs:

```text
Deploy DEV

Deploy STAGING

Deploy PRODUCTION

Rollback PRODUCTION

Smoke Test

Health Check
```

Do not unnecessarily duplicate the entire GitLab CI pipeline in Jenkins.

GitLab CI should focus primarily on:

```text
build
test
quality
artifact creation
```

Jenkins can focus on:

```text
deployment
environment promotion
operational automation
```

---

# 56. Environment Flow

Use:

```text
DEV

↓

STAGING

↓

PRODUCTION
```

Include environment-specific configuration.

---

# 57. k6

Use:

```text
k6
```

for:

```text
load testing

performance testing

synthetic transaction generation
```

---

# 58. Failure Injection

Create a controlled incident injector.

Do not rely only on random failures.

Each scenario should be selectable and reproducible.

Possible scenarios:

```text
application error

database timeout

database latency

database pool exhaustion

Kafka lag

dependency timeout

CPU saturation

memory leak

OOMKilled

bad deployment

business transaction failure
```

---

# 59. Incident Scenario 1 — Application Failure

Normal:

```text
Success Rate = 99.9%
```

Incident:

```text
Success Rate = 91%
```

Workflow:

```text
Grafana detects degradation

↓

Splunk shows error increase

↓

Dynatrace shows failing dependency

↓

Incident created

↓

RCA
```

---

# 60. Incident Scenario 2 — Database Latency

Example:

```text
P95
300 ms → 4.2 s
```

Potential root causes:

```text
slow query

connection pool exhaustion

database CPU saturation
```

---

# 61. Incident Scenario 3 — Kafka Consumer Lag

Example:

```text
Producer = 10k msg/s

Consumer = 6k msg/s

↓

Consumer Lag ↑

↓

Transaction processing delay
```

Expose lag in monitoring.

---

# 62. Incident Scenario 4 — OOMKilled

Example:

```text
Payment Service

Memory Usage ↑

↓

Container Limit Reached

↓

OOMKilled

↓

Pod Restart

↓

Transaction Failure
```

The incident must be visible across application and infrastructure observability.

---

# 63. Incident Scenario 5 — Business Failure

Important:

```text
HTTP 200 != business success
```

Example:

```json
{
  "http_status": 200,
  "transaction_status": "FAILED",
  "error_code": "PACKAGE_ACTIVATION_FAILED"
}
```

The business success-rate metric must fall even though HTTP-level availability may appear healthy.

---

# 64. Incident Scenario 6 — Dependency Failure

Example:

```text
Payment Service
     ↓
External Payment Provider
     ↓
Timeout
```

Display dependency degradation separately from internal application health.

---

# 65. Secure Enterprise Access

Operational infrastructure must conceptually live behind a corporate-access boundary.

Model:

```text
Internet
    │
    │
Corporate User
    │
    ▼
GlobalProtect VPN
    │
    ▼
Corporate / Internal Network
    │
    ├── GitLab
    ├── Jenkins
    ├── SonarQube
    ├── JFrog
    ├── TelcoPulse
    ├── Grafana
    ├── Splunk
    ├── Dynatrace
    └── Datadog
```

This is a conceptual architecture.

Do not reproduce confidential real-world infrastructure.

---

# 66. GlobalProtect

Represent:

```text
GlobalProtect VPN
```

as the conceptual corporate VPN boundary.

Example engineering flow:

```text
Developer Laptop
      ↓
GlobalProtect
      ↓
Internal GitLab
      ↓
GitLab CI
```

Do not require proprietary GlobalProtect software for local development.

Instead:

```text
document the boundary

simulate internal-only networking

keep internal services non-public
```

---

# 67. Prisma Browser

Represent:

```text
Prisma Browser
```

as a secure enterprise browsing/access concept.

It may conceptually provide controlled browser access to:

```text
TelcoPulse

Grafana

Splunk

Dynatrace

Datadog

GitLab
```

Do not try to clone or reproduce the proprietary browser itself.

Model concepts such as:

```text
authenticated access

restricted internal applications

role-aware access

secure sessions

enterprise access policies
```

---

# 68. Network Security Model

Distinguish between:

```text
Customer/Public Access
```

and:

```text
Corporate/Internal Access
```

Example:

```text
PUBLIC
Customer Simulator Customer APIs

INTERNAL
ITOC Console
GitLab
Jenkins
Grafana
Splunk
Dynatrace
Datadog
SonarQube
JFrog
```

---

# 69. Authentication

Architecture should be:

```text
SSO-ready
```

Do not hardwire the system permanently to username/password authentication.

For local development, simple auth is acceptable.

---

# 70. RBAC

Support roles:

```text
Viewer

Operator

Engineer

Incident Commander

Administrator
```

Example permissions:

## Viewer

Can:

```text
view dashboard
view incidents
view metrics
view RCA
```

Cannot:

```text
resolve incident
inject failure
change severity
assign owner
```

## Operator

Can:

```text
acknowledge incident
assign owner where permitted
add evidence
update status
```

## Incident Commander

Can:

```text
change severity
escalate
resolve incident
coordinate RCA
```

## Administrator

Can manage:

```text
users
roles
system configuration
```

---

# 71. PII

Treat:

```text
MSISDN
```

as sensitive data.

Default UI/log form:

```text
62812*****123
```

Raw values should not be sprayed throughout logs.

---

# 72. Audit Logging

Record significant operational changes.

Examples:

```text
incident created

incident acknowledged

severity changed

owner changed

incident escalated

status changed

mitigation added

RCA edited

incident resolved

action item completed
```

Audit record:

```text
actor

action

timestamp

resource

old_value

new_value

request/correlation id where useful
```

---

# 73. Application Security

Apply appropriate:

```text
input validation

authentication

authorization

rate limiting

secret management

safe error handling

secure headers

dependency hygiene

PII masking
```

Do not commit:

```text
tokens

passwords

real secrets

real enterprise URLs

VPN configuration

internal addresses
```

---

# 74. UX States

Every major screen must support appropriate:

```text
loading state

skeleton state

empty state

error state

partial data state

permission denied state
```

Do not assume APIs always succeed.

---

# 75. Tables

Enterprise tables should support where appropriate:

```text
sorting

filtering

pagination

column visibility

column resizing

sticky headers

search

row selection

bulk actions

saved views
```

Use:

```text
TanStack Table
```

when appropriate.

---

# 76. Keyboard UX

Support:

```text
keyboard navigation

command palette

global search shortcut

quick incident navigation
```

Example:

```text
Cmd/Ctrl + K
```

for search/command access.

---

# 77. Accessibility

Support:

```text
keyboard users

visible focus states

reasonable contrast

semantic labels

screen-reader-compatible forms
```

Never communicate severity using color alone.

Use:

```text
color + text + icon
```

---

# 78. Service Level Objectives

Add SRE-style concepts.

Support:

```text
SLI

SLO

Error Budget
```

Example:

```text
Purchase Availability SLO

99.9%
```

Display:

```text
current SLI

target SLO

error budget remaining

error budget consumed
```

---

# 79. Operational Metrics

Track:

```text
MTTA

MTTR

Incident Count

SEV-1 Count

Escalations

Repeat Incidents

Deployment Failure Rate
```

---

# 80. Code Quality

Avoid:

```text
giant React components

duplicated business logic

untyped API payloads

hardcoded data everywhere

business rules inside presentation components

silent errors

magic strings

over-engineered abstractions
```

Prefer:

```text
clear domain models

typed contracts

repository layer

service layer

structured error handling

reusable UI

centralized configuration

structured logging

tests
```

---

# 81. Testing Strategy

Implement:

```text
Unit Tests

Integration Tests

API Tests

Frontend Component Tests

End-to-End Tests

Load Tests
```

Prioritize meaningful tests over arbitrary coverage percentages.

---

# 82. Observability Testing

Verify that important flows produce expected:

```text
metrics

logs

traces

Kafka events
```

Example test:

```text
Run purchase

↓

Confirm transaction record

↓

Confirm Kafka event

↓

Confirm Prometheus counter increments

↓

Confirm trace exists

↓

Confirm structured log contains transaction_id
```

---

# 83. Documentation

Maintain:

```text
README

Architecture Overview

Service Documentation

Local Development Guide

Deployment Guide

Observability Guide

Incident Runbooks

Failure Simulation Guide
```

---

# 84. Runbooks

Create runbooks for at least:

```text
High Error Rate

High Latency

Database Connection Exhaustion

Kafka Consumer Lag

Pod CrashLoopBackOff

OOMKilled

Dependency Timeout

Bad Deployment
```

---

# 85. Architecture Documentation

Document:

```text
service architecture

data flow

Kafka flow

observability architecture

CI/CD flow

corporate access model

incident workflow
```

Use Mermaid where useful.

---

# 86. Development Phases

Do not implement the entire project in one huge change.

---

## Phase 1 — Foundation

Build:

```text
Next.js frontend

Go API

PostgreSQL

basic package purchase

Docker Compose
```

Goal:

```text
A complete successful transaction works end-to-end.
```

---

## Phase 2 — Service Architecture

Add:

```text
API Gateway

Subscriber Service

Package Service

Payment Service

Notification Service
```

---

## Phase 3 — Kafka

Introduce:

```text
transaction events

payment events

notification events

consumer groups
```

---

## Phase 4 — Observability Foundation

Add:

```text
OpenTelemetry

Prometheus

Grafana

structured logging

correlation IDs
```

---

## Phase 5 — ITOC UI

Build:

```text
Overview Dashboard

Service Health

Transactions

Incidents
```

---

## Phase 6 — Incident System

Build:

```text
Incident Lifecycle

Timeline

Ownership

Evidence

RCA

Action Items
```

---

## Phase 7 — Enterprise Observability

Integrate or prepare adapters/documentation for:

```text
Splunk

Dynatrace

Datadog
```

---

## Phase 8 — Failure Simulation

Implement controlled:

```text
database failure

Kafka lag

dependency latency

business failures

resource exhaustion
```

---

## Phase 9 — Kubernetes

Deploy application to:

```text
Kubernetes
```

and implement health/resource configuration.

---

## Phase 10 — OpenShift Compatibility

Make the production-like deployment compatible with OpenShift concepts.

---

## Phase 11 — CI/CD

Implement:

```text
GitLab CI

SonarQube

Docker Build

JFrog

Jenkins

Helm
```

---

## Phase 12 — Security

Implement:

```text
authentication

RBAC

PII masking

audit trail

internal/public boundary documentation
```

---

## Phase 13 — Enterprise UX

Implement:

```text
advanced filtering

saved views

command palette

large-table UX

role-aware actions

loading/error states
```

---

# 87. Definition of Done — Primary Demo

The flagship demonstration must work like this:

```text
1. Start TelcoPulse.

2. Open Customer Simulator.

3. Select a customer.

4. Select an internet package.

5. Configure payment.

6. Execute synthetic transaction.

7. Transaction completes successfully.

8. Observe transaction telemetry.

9. Inject a database/payment incident.

10. Execute more traffic.

11. Business success rate decreases.

12. Grafana shows degradation.

13. Prometheus metrics show error increase.

14. Splunk structured logs show DB_TIMEOUT.

15. Dynatrace trace identifies affected dependency.

16. Datadog exposes relevant Kubernetes/infrastructure context.

17. TelcoPulse displays or creates an active incident.

18. Operator opens incident.

19. Operator reviews metrics.

20. Operator reviews logs.

21. Operator reviews trace/dependency flow.

22. Operator reviews recent deployment.

23. Operator identifies root cause.

24. Mitigation is applied.

25. Service begins recovering.

26. Incident moves to Monitoring.

27. Metrics return to normal.

28. Operator resolves incident.

29. RCA is documented.

30. Action items are created.

31. Audit log records the entire lifecycle.
```

The demo should clearly demonstrate:

```text
Detect

Investigate

Correlate

Escalate

Mitigate

Recover

Resolve

Learn
```

---

# 88. Definition of Done — Engineering

The project should not be considered complete merely because the UI renders.

A production-like feature should include, when relevant:

```text
frontend

backend

database

validation

error handling

loading/error UI

tests

structured logs

metrics

traces

documentation
```

---

# 89. Codex Operating Instructions

Before modifying code:

```text
1. Inspect the existing repository.

2. Understand current architecture.

3. Read relevant documentation.

4. Follow established conventions.

5. Identify the smallest coherent implementation step.

6. Make a concise implementation plan.

7. Implement the feature fully.

8. Add or update tests.

9. Run relevant linting.

10. Run type checking.

11. Run tests.

12. Run builds where appropriate.

13. Fix regressions introduced by the implementation.

14. Summarize the changes.
```

---

# 90. Do Not Rewrite Working Systems Without Reason

Codex must prefer:

```text
incremental changes
```

over:

```text
unnecessary rewrites
```

Preserve good existing architecture.

Refactor only where the change has clear value.

---

# 91. Do Not Over-Engineer

Do not introduce:

```text
new framework

new database

new queue

new state manager

new abstraction
```

unless it solves a concrete problem.

---

# 92. Implementation Priority

Always prioritize:

```text
correctness

operator usability

maintainability

observability

security

realistic workflows

testability
```

above:

```text
visual gimmicks

technology count

premature abstraction

unnecessary complexity
```

---

# 93. Enterprise Authenticity Rule

Whenever implementing a feature, ask:

```text
Would this actually help an operator investigate or resolve a production incident?
```

If the answer is no, reconsider whether the feature belongs in the primary ITOC experience.

---

# 94. Tool Authenticity Rule

Do not add integrations merely to display logos.

Every major tool must have a purpose.

```text
Prometheus
→ Metrics

Grafana
→ Metrics visualization

Splunk
→ Logs and transaction investigation

Dynatrace
→ Traces, APM, service dependency

Datadog
→ Infrastructure / Kubernetes / runtime monitoring

OpenTelemetry
→ Vendor-neutral telemetry

Kafka
→ Event streaming

GitLab
→ Source control + CI

SonarQube
→ Code quality

JFrog
→ Artifact/container registry

Jenkins
→ Deployment automation

Kubernetes/OpenShift
→ Runtime orchestration

GlobalProtect
→ Corporate VPN concept

Prisma Browser
→ Secure enterprise browser/access concept
```

---

# 95. Confidentiality Rule

Never invent or use:

```text
real Telkomsel internal URLs

real employee credentials

real IP addresses

real VPN configuration

real certificates

real internal topology

real production customer data
```

All enterprise architecture must use:

```text
fictional

local

sanitized

or documented conceptual equivalents
```

---

# 96. Final Product Standard

The final product should feel like:

```text
a credible internal telecom observability platform
```

rather than:

```text
a portfolio dashboard with monitoring logos
```

A technical reviewer should be able to see that the project demonstrates knowledge of:

```text
software engineering

microservices

distributed systems

event-driven architecture

observability

SRE

incident response

telecommunications transactions

Kubernetes

DevOps

CI/CD

enterprise security

enterprise UI/UX
```

The platform should tell one coherent story:

```text
A customer performs a telecom transaction.

The transaction crosses multiple distributed services.

The services generate meaningful telemetry.

A production-like failure occurs.

ITOC detects the issue.

The operator investigates with real observability concepts.

Evidence from multiple systems is correlated.

The responsible service or dependency is identified.

Mitigation restores service.

The incident is resolved.

The organization learns from the incident.
```

That complete operational lifecycle is the primary goal of **TelcoPulse**.