output "kubeconfig_context" {
  description = "kubectl context written by kind."
  value       = "kind-${var.cluster_name}"
}

output "endpoint" {
  value = kind_cluster.this.endpoint
}
