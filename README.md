## mastodon_exporter

This is a simple Prometheus exporter for gathering data about a running
[Mastodon][mastodon] instance.

Currently, it exposes:

| Metric Name                             | Type      | Description                                       |
|-----------------------------------------|-----------|---------------------------------------------------|
| mastodon_exporter_num_accounts          | gauge     | The number of accounts, labeled by type           |
| mastodon_exporter_num_posts             | gauge     | The number of posts from this instance            |
| mastodon_exporter_num_reports           | gauge     | The number of open/resolved reports               |
| mastodon_exporter_resolved_time_seconds | histogram | A histogram of the time taken to resolve a report |

### Example Output

```
# HELP mastodon_exporter_errors Number of errors encountered while querying.
# TYPE mastodon_exporter_errors gauge
mastodon_exporter_errors 0
# HELP mastodon_exporter_num_accounts Number of accounts on this Mastodon instance.
# TYPE mastodon_exporter_num_accounts gauge
mastodon_exporter_num_accounts{type="bots"} 1
mastodon_exporter_num_accounts{type="groups"} 0
mastodon_exporter_num_accounts{type="people"} 100
mastodon_exporter_num_accounts{type="suspended"} 1
mastodon_exporter_num_accounts{type="unsuspended"} 101
# HELP mastodon_exporter_num_posts Number of posts on this Mastodon instance.
# TYPE mastodon_exporter_num_posts gauge
mastodon_exporter_num_posts 1938
# HELP mastodon_exporter_num_reports Number of reports for this Mastodon instance.
# TYPE mastodon_exporter_num_reports gauge
mastodon_exporter_num_reports{resolved="false"} 0
mastodon_exporter_num_reports{resolved="true"} 3
# HELP mastodon_exporter_resolved_time_seconds Time taken to resolve reports in this Mastodon instance.
# TYPE mastodon_exporter_resolved_time_seconds histogram
mastodon_exporter_resolved_time_seconds_bucket{le="60"} 1
mastodon_exporter_resolved_time_seconds_bucket{le="600"} 2
mastodon_exporter_resolved_time_seconds_bucket{le="1800"} 2
mastodon_exporter_resolved_time_seconds_bucket{le="3600"} 3
mastodon_exporter_resolved_time_seconds_bucket{le="14400"} 3
mastodon_exporter_resolved_time_seconds_bucket{le="28800"} 3
mastodon_exporter_resolved_time_seconds_bucket{le="86400"} 3
mastodon_exporter_resolved_time_seconds_bucket{le="172800"} 3
mastodon_exporter_resolved_time_seconds_bucket{le="604800"} 3
mastodon_exporter_resolved_time_seconds_bucket{le="+Inf"} 3
mastodon_exporter_resolved_time_seconds_sum 3460.190126
mastodon_exporter_resolved_time_seconds_count 3
```

[mastodon]: https://joinmastodon.org/
