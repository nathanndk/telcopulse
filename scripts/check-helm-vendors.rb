#!/usr/bin/env ruby
require 'yaml'

abort 'usage: check-helm-vendors.rb DEFAULT_RENDER VENDOR_RENDER' unless ARGV.length == 2

def resources(path)
  YAML.load_stream(File.read(path)).compact
end

def indexed(list, kind)
  list.select { |item| item.fetch('kind') == kind }.to_h do |item|
    [item.fetch('metadata').fetch('name'), item]
  end
end

plain, vendor = ARGV.map { |path| resources(path) }
plain_deployments = indexed(plain, 'Deployment')
vendor_deployments = indexed(vendor, 'Deployment')
plain_configs = indexed(plain, 'ConfigMap')
vendor_configs = indexed(vendor, 'ConfigMap')
abort 'expected all ten deployments' unless plain_deployments.size == 10 && vendor_deployments.size == 10

expected_global = {
  'splunk-hec-token' => 'SPLUNK_HEC_TOKEN',
  'otel-authorization' => 'OTLP_AUTHORIZATION',
  'otel-ca' => 'OTLP_CA_CERTIFICATE'
}
expected_incident = expected_global.merge(
  'splunk-search-token' => 'SPLUNK_SEARCH_TOKEN',
  'datadog-token' => 'DATADOG_TOKEN'
)

plain_deployments.each do |name, deployment|
  pod = deployment.fetch('spec').fetch('template').fetch('spec')
  abort "default #{name} mounts a vendor credential" if pod.key?('volumes') || pod.fetch('containers').first.key?('volumeMounts')
  config = plain_configs.fetch("#{name}-config").fetch('data')
  abort "default #{name} has vendor settings" if config.keys.any? { |key| key.start_with?('SPLUNK_', 'DATADOG_', 'JAEGER_') || key == 'OTEL_EXPORTER_OTLP_AUTHORIZATION_FILE' || key == 'OTEL_EXPORTER_OTLP_CERTIFICATE' }
end

vendor_deployments.each do |name, deployment|
  pod = deployment.fetch('spec').fetch('template').fetch('spec')
  container = pod.fetch('containers').first
  expected = name == 'web' ? {} : name == 'incident-service' ? expected_incident : expected_global
  volumes = (pod['volumes'] || []).to_h { |v| [v.fetch('name'), v] }
  mounts = (container['volumeMounts'] || []).to_h { |v| [v.fetch('name'), v] }
  abort "#{name} Secret volume scope incorrect" unless volumes.keys.sort == expected.keys.sort && mounts.keys.sort == expected.keys.sort
  expected.each do |volume_name, key|
    secret = volumes.fetch(volume_name).fetch('secret')
    abort "#{name} mounts unexpected Secret data" unless secret.fetch('secretName') == 'telcopulse-runtime' && secret.fetch('items') == [{ 'key' => key, 'path' => volume_name == 'otel-authorization' ? 'authorization' : volume_name == 'otel-ca' ? 'certificate' : 'token' }]
    abort "#{name} credential mount is writable" unless mounts.fetch(volume_name).fetch('readOnly') == true
  end
  config = vendor_configs.fetch("#{name}-config").fetch('data')
  if name == 'incident-service'
    abort 'incident search sources missing' unless %w[SPLUNK_SEARCH_URL DATADOG_METRICS_URL JAEGER_QUERY_URL].all? { |key| config.key?(key) }
  else
    abort "#{name} gained incident search config" if %w[SPLUNK_SEARCH_URL DATADOG_METRICS_URL JAEGER_QUERY_URL].any? { |key| config.key?(key) }
  end
end

abort 'vendor render contains a Secret resource' if vendor.any? { |item| item['kind'] == 'Secret' }
puts 'vendor Secret-key scope and default isolation: valid'
