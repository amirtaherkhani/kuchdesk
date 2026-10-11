#!/usr/bin/env ruby
# Candidate only, pinned to the reviewed monitoring 87.15.1 render.
require 'yaml'
require 'json'
require 'digest'

begin
  objects = YAML.load_stream($stdin.read).compact
  find_one = lambda do |kind, name|
    found = objects.select { |o| o['kind'] == kind && o.dig('metadata', 'name') == name && o.dig('metadata', 'namespace') == 'observability' }
    raise "expected exactly one #{kind}/#{name}" unless found.length == 1
    found.first
  end
  rules = find_one.call('PrometheusRule', 'monitoring-kube-prometheus-kube-apiserver-availability.rules')
  dashboard = find_one.call('ConfigMap', 'monitoring-kube-prometheus-apiserver')
  groups = rules.fetch('spec').fetch('groups')
  data = JSON.parse(dashboard.fetch('data').fetch('apiserver.json'))
  raise 'availability rules drifted; review again' unless Digest::SHA256.hexdigest(JSON.generate(groups)) == '858bc4e4b644a001600739dac8ae487ff34686a9d0484f4a78b34d2e95b660f7'
  raise 'API-server dashboard drifted; review again' unless Digest::SHA256.hexdigest(JSON.generate(data)) == 'b9873aeefa06bf58d5c4c4d21053f10ec795f007f0657c2c97f45d35a38b0639'
  # Change only the reviewed 30-day records and their two day-count factors.
  groups.first.fetch('rules').each do |rule|
    rule['record'] = rule.fetch('record').gsub('30d', '3d')
    rule['expr'] = rule.fetch('expr').gsub('30d', '3d').gsub('* 24 * 30', '* 24 * 3')
  end
  # Update the corresponding metric references and human-readable window labels.
  walk = lambda do |value|
    case value
    when Hash then value.transform_values { |v| walk.call(v) }
    when Array then value.map { |v| walk.call(v) }
    when String then value.gsub('30d', '3d').gsub('30 days', '3 days')
    else value
    end
  end
  dashboard['data']['apiserver.json'] = JSON.generate(walk.call(data))
  $stdout.write(YAML.dump_stream(*objects))
rescue StandardError => e
  warn "retention post-renderer: #{e.message}"
  exit 1
end
