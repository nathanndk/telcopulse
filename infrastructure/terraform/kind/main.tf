# Local Kubernetes evaluation platform for TelcoPulse: a kind cluster plus the
# operators the Helm chart integrates with. Application workloads are deployed
# separately (scripts/kind-evaluation.sh or Argo CD), not by this module.
resource "kind_cluster" "this" {
  name           = var.cluster_name
  node_image     = var.node_image
  wait_for_ready = true

  kind_config {
    kind        = "Cluster"
    api_version = "kind.x-k8s.io/v1alpha4"

    node {
      role = "control-plane"
    }

    dynamic "node" {
      for_each = range(var.workers)
      content {
        role = "worker"
      }
    }
  }
}

provider "helm" {
  kubernetes = {
    host                   = kind_cluster.this.endpoint
    client_certificate     = kind_cluster.this.client_certificate
    client_key             = kind_cluster.this.client_key
    cluster_ca_certificate = kind_cluster.this.cluster_ca_certificate
  }
}

# Prometheus Operator, Prometheus, Alertmanager, kube-state-metrics and node
# exporter; Grafana is left out to fit a laptop. Selectors accept the chart's
# ServiceMonitor, PrometheusRule and AlertmanagerConfig from any namespace.
resource "helm_release" "kube_prometheus_stack" {
  name             = "kube-prometheus-stack"
  repository       = "https://prometheus-community.github.io/helm-charts"
  chart            = "kube-prometheus-stack"
  version          = "92.1.0"
  namespace        = "monitoring"
  create_namespace = true
  timeout          = 900
  values           = [file("${path.module}/values/kube-prometheus-stack.yaml")]
}

resource "helm_release" "argo_rollouts" {
  name             = "argo-rollouts"
  repository       = "https://argoproj.github.io/argo-helm"
  chart            = "argo-rollouts"
  version          = "2.43.6"
  namespace        = "argo-rollouts"
  create_namespace = true
  values           = [file("${path.module}/values/argo-rollouts.yaml")]
}

resource "helm_release" "chaos_mesh" {
  count            = var.enable_chaos_mesh ? 1 : 0
  name             = "chaos-mesh"
  repository       = "https://charts.chaos-mesh.org"
  chart            = "chaos-mesh"
  version          = "2.8.4"
  namespace        = "chaos-mesh"
  create_namespace = true
  timeout          = 600
  values           = [file("${path.module}/values/chaos-mesh.yaml")]
}

resource "helm_release" "argocd" {
  count            = var.enable_argocd ? 1 : 0
  name             = "argocd"
  repository       = "https://argoproj.github.io/argo-helm"
  chart            = "argo-cd"
  version          = "10.10.0"
  namespace        = "argocd"
  create_namespace = true
  timeout          = 600
  values           = [file("${path.module}/values/argo-cd.yaml")]
}
