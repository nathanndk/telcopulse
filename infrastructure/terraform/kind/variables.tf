variable "cluster_name" {
  description = "kind cluster name; also the kubeconfig context suffix (kind-<name>)."
  type        = string
  default     = "telcopulse"
}

variable "node_image" {
  # The tehcyx/kind 0.11 provider embeds the kind v0.31 library, whose kubeadm
  # configuration fails to initialize newer node images (v1.37 failed in
  # kubeadm init); pin the image that kind v0.31 was released with.
  description = "kindest/node image matching the provider's embedded kind version."
  type        = string
  default     = "kindest/node:v1.35.0@sha256:452d707d4862f52530247495d180205e029056831160e22870e37e3f6c1ac31f"
}

variable "workers" {
  # One worker plus a schedulable control plane (scripts/kind-evaluation.sh removes
  # its NoSchedule taint) gives a node drain somewhere to move pods while fitting
  # an 8 GB laptop; each extra node container costs about 200 MiB.
  description = "Worker nodes."
  type        = number
  default     = 1
  validation {
    condition     = var.workers >= 1 && var.workers <= 3
    error_message = "workers must be between 1 and 3."
  }
}

variable "enable_argocd" {
  description = "Install Argo CD for the GitOps evaluation."
  type        = bool
  default     = true
}

variable "enable_chaos_mesh" {
  description = "Install Chaos Mesh for pod, network and stress experiments."
  type        = bool
  default     = true
}
