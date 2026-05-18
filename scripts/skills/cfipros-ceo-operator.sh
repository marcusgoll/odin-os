#!/usr/bin/env bash
set -euo pipefail

payload="$(cat)"
python3 -c '
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

raw = sys.stdin.read()
try:
    data = json.loads(raw) if raw.strip() else {}
except json.JSONDecodeError:
    data = {}

input_data = data.get("input") or {}
agent_key = input_data.get("agent_key") or "cfipros-ceo-operator-agent"
workflow_key = input_data.get("workflow_key") or "cfipros-ceo-operating-routine"
checkpoint = input_data.get("checkpoint") or "unspecified"
approval_boundary = input_data.get("approval_boundary") or "external actions require explicit human approval"
external_side_effect = input_data.get("external_side_effect") or "none"
project_key = input_data.get("project_key") or "cfipros"
request = input_data.get("request") or ""

repo_root = Path(
    input_data.get("cfipros_repo_root")
    or input_data.get("project_root")
    or "/home/orchestrator/cfipros"
)
repo_root_display = str(repo_root)

def file_evidence(relative_path, markers=None):
    path = repo_root / relative_path
    exists = path.is_file()
    marker_hits = []
    if exists and markers:
        try:
            content = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            content = ""
        marker_hits = [marker for marker in markers if marker in content]
    return {
        "path": relative_path,
        "exists": exists,
        "markers": marker_hits,
    }

def normalize_evidence(raw_evidence):
    if not raw_evidence:
        return {}
    if isinstance(raw_evidence, dict):
        return raw_evidence
    normalized = {}
    if isinstance(raw_evidence, list):
        for item in raw_evidence:
            if isinstance(item, dict) and item.get("key"):
                normalized[str(item["key"])] = item
    return normalized

def load_kpi_export(input_data):
    inline_export = input_data.get("kpi_export") or input_data.get("cfipros_kpi_export")
    if isinstance(inline_export, dict):
        return inline_export
    export_path = input_data.get("kpi_export_path") or input_data.get("cfipros_kpi_export_path")
    if not export_path:
        return {}
    try:
        loaded = json.loads(Path(str(export_path)).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {
            "status": "unavailable",
            "source": str(export_path),
            "error": "kpi_export_path_unreadable",
        }
    return loaded if isinstance(loaded, dict) else {}

def normalize_kpi_export(raw_export):
    if not raw_export:
        return {}
    raw_metrics = raw_export.get("metrics")
    if not isinstance(raw_metrics, list):
        return {}
    metrics_by_key = {
        str(item.get("key")): item
        for item in raw_metrics
        if isinstance(item, dict) and item.get("key")
    }
    export_source = raw_export.get("source") or "cfipros-kpi-export"
    export_as_of = raw_export.get("as_of_utc")
    approval_required = raw_export.get("approval_required")
    external_side_effect = raw_export.get("external_side_effect")

    def measured(metric_key):
        metric = metrics_by_key.get(metric_key)
        if not metric or metric.get("status") != "measured":
            return None
        value = metric.get("value")
        if value in (None, "", "unmeasured"):
            return None
        unit = metric.get("unit") or "count"
        return f"{metric_key}={value} {unit}".strip()

    def grouped_value(keys):
        parts = [part for key in keys if (part := measured(key))]
        return "; ".join(parts) if parts else None

    def evidence_entry(value, mapped_from):
        if not value:
            return None
        return {
            "value": value,
            "source": export_source,
            "as_of_utc": export_as_of,
            "approval_required": approval_required,
            "external_side_effect": external_side_effect,
            "mapped_from": mapped_from,
        }

    mapped = {}
    group_specs = {
        "activation_students": [
            "users_created_today",
            "users_onboarded_today",
            "students_created_today",
        ],
        "product_value_aktr": [
            "aktr_uploads_today",
            "aktr_codes_today",
        ],
        "paid_conversion": [
            "paid_subscribers_total",
            "estimated_mrr_cents",
        ],
        "quality_operations": [
            "failed_extractions_today",
        ],
    }
    for kpi_key, export_keys in group_specs.items():
        entry = evidence_entry(grouped_value(export_keys), export_keys)
        if entry:
            mapped[kpi_key] = entry
    return mapped

kpi_evidence = normalize_evidence(input_data.get("kpi_evidence") or input_data.get("kpi_values"))
kpi_export = load_kpi_export(input_data)
for key, value in normalize_kpi_export(kpi_export).items():
    kpi_evidence.setdefault(key, value)

metric_specs = [
    {
        "key": "acquisition_traffic",
        "label": "Acquisition traffic",
        "funnel_stage": "acquisition",
        "source_of_truth": "PostHog HogQL over n8n-ingested GSC/GA4 daily metrics",
        "measurement_command": "query PostHog for source_gsc_daily_metrics and source_ga4_daily_metrics",
        "evidence": [
            file_evidence("docs/integrations/N8N.md", ["source_gsc_daily_metrics", "source_ga4_daily_metrics"]),
            file_evidence("docs/integrations/POSTHOG.md", ["source_gsc_daily_metrics", "source_ga4_daily_metrics"]),
            file_evidence("api/app/scripts/verify_analytics_pipeline.py", ["POSTHOG_PERSONAL_API_KEY", "source_ga4_daily_metrics"]),
        ],
    },
    {
        "key": "activation_students",
        "label": "Student and instructor activation",
        "funnel_stage": "activation",
        "source_of_truth": "CFI dashboard and organization/student database counts",
        "measurement_command": "read production student, user, organization, and membership counts",
        "evidence": [
            file_evidence("docs/api-reference/endpoints/students.md", ["student_count", "active_students"]),
            file_evidence("api/app/services/organizations/organization_service.py", ["student_count", "instructor_count"]),
        ],
    },
    {
        "key": "product_value_aktr",
        "label": "AKTR product value",
        "funnel_stage": "product_value",
        "source_of_truth": "AKTR upload, extraction, and code statistics",
        "measurement_command": "read AKTR upload summary and extraction status counts",
        "evidence": [
            file_evidence("docs/api-reference/endpoints/aktr.md", ["total_uploads", "total_codes_extracted"]),
            file_evidence("OBSERVABILITY_VERIFICATION.md", ["aktr", "metrics"]),
        ],
    },
    {
        "key": "paid_conversion",
        "label": "Paid conversion and billing readiness",
        "funnel_stage": "paid_conversion",
        "source_of_truth": "Stripe-backed subscription status, checkout, portal, and usage endpoints",
        "measurement_command": "read subscription status/counts and Stripe checkout readiness",
        "evidence": [
            file_evidence("docs/api-reference/endpoints/billing.md", ["subscriptions/status", "checkout", "stripe_customer_id"]),
            file_evidence("config/deploy.api.yml", ["STRIPE_SECRET_KEY", "STRIPE_WEBHOOK_SECRET"]),
        ],
    },
    {
        "key": "quality_operations",
        "label": "Operational quality",
        "funnel_stage": "quality",
        "source_of_truth": "Prometheus /metrics and AKTR extraction lifecycle telemetry",
        "measurement_command": "read authenticated /metrics and extraction health counters",
        "evidence": [
            file_evidence("OBSERVABILITY_VERIFICATION.md", ["/metrics", "Prometheus"]),
            file_evidence("docs/integrations/N8N.md", ["METRICS_BEARER_TOKEN", "api.cfipros.com/metrics"]),
        ],
    },
]

metrics = []
measured_count = 0
available_count = 0
for spec in metric_specs:
    provided = kpi_evidence.get(spec["key"], {})
    has_value = isinstance(provided, dict) and provided.get("value") not in (None, "", "unmeasured")
    evidence_available = any(item["exists"] and (not item["markers"] or len(item["markers"]) > 0) for item in spec["evidence"])
    if has_value:
        status = "measured"
        value = provided.get("value")
        measured_count += 1
    elif evidence_available:
        status = "source_available_value_unmeasured"
        value = "unmeasured"
        available_count += 1
    else:
        status = "source_missing_value_unmeasured"
        value = "unmeasured"
    metrics.append({
        "key": spec["key"],
        "label": spec["label"],
        "funnel_stage": spec["funnel_stage"],
        "status": status,
        "value": value,
        "source_of_truth": spec["source_of_truth"],
        "measurement_command": spec["measurement_command"],
        "evidence": spec["evidence"],
        "provided_evidence": provided if isinstance(provided, dict) else {},
    })

if measured_count:
    collection_status = "measured_values_supplied"
elif available_count:
    collection_status = "sources_found_values_unmeasured"
else:
    collection_status = "sources_missing_values_unmeasured"

if measured_count:
    launch_posture = "narrow: KPI evidence is partially measured; approval-gated CEO review can choose the next launch move."
else:
    launch_posture = "hold: KPI sources are mapped, but live KPI values remain unmeasured until read-only production/PostHog evidence is supplied."

kpi_truth = {
    "as_of_utc": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    "collection_status": collection_status,
    "collection_mode": "read_only",
    "repo_root": repo_root_display,
    "kpi_export_source": kpi_export.get("source") if isinstance(kpi_export, dict) else None,
    "metrics": metrics,
    "unmeasured_policy": "missing KPI values are reported as unmeasured, never zero",
    "live_collection_boundary": "skill remains repo.read/runtime.read; production DB, PostHog, Stripe, customer, billing, deploy, and merge actions require explicit human approval or operator-supplied read-only evidence",
}

ceo_packet = {
    "launch posture": launch_posture,
    "KPI scorecard": metrics,
    "customer acquisition focus": "Use CFI/chief-instructor acquisition only as an approval-gated draft until acquisition traffic and pipeline KPIs are measured.",
    "paid-conversion and billing readiness": "Billing endpoints and Stripe deployment requirements are documented; paid-conversion values are unmeasured unless supplied as KPI evidence.",
    "sales pipeline status": "unmeasured",
    "marketing and growth loop": "GSC and GA4 daily pipelines are the documented acquisition source of truth; values are unmeasured unless PostHog evidence is supplied.",
    "decisions for operator": [
        "Supply read-only production DB and PostHog KPI evidence before treating launch-health metrics as measured.",
        "Keep any customer, pricing, billing, publishing, deploy, or merge action approval-required.",
    ],
    "approval-required external actions": [
        {
            "action": "customer outreach, publishing, ads, pricing, billing, deploys, and merges",
            "approval_required_before_use": "yes",
            "external_side_effect": "customer|post|ad|pricing|billing|deploy|merge",
            "approved_by": "pending",
        }
    ],
    "implementation-ready follow-up work": [
        "Add a CFIPros-owned read-only KPI export that returns acquisition, activation, AKTR value, paid conversion, revenue, retention, and quality metrics without exposing secrets.",
    ],
    "stop conditions and risks": [
        "Stop if a KPI value is missing and would need to be invented.",
        "Stop if the packet tries to contact customers, publish, spend, change billing/pricing, deploy, merge, or represent CFIPros externally without approval.",
    ],
}

output = {
    "result": "cfipros_ceo_operator_packet_ready",
    "agent_key": agent_key,
    "workflow_key": workflow_key,
    "checkpoint": checkpoint,
    "project_key": project_key,
    "request": request,
    "approval_required": True,
    "external_side_effect": external_side_effect,
    "approval_boundary": approval_boundary,
    "kpi_truth": kpi_truth,
    "ceo_packet": ceo_packet,
}

print(json.dumps({
    "skill_key": "cfipros-ceo-operator",
    "status": "ok",
    "summary": f"CFIPros CEO launch-health packet ready with KPI truth: {checkpoint}",
    "output": output,
    "raw_output": json.dumps(output, sort_keys=True),
}, sort_keys=True))
' <<<"${payload}"
