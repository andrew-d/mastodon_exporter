package main

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	webflag "github.com/prometheus/exporter-toolkit/web/kingpinflag"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	databaseURL = kingpin.Flag("mastodon.database_url", "Postgres connection string for the Mastodon database").Envar("DATABASE_URL").String()
	metricPath  = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").Envar("MASTODON_EXPORTER_WEB_TELEMETRY_PATH").String()
	webConfig   = webflag.AddFlags(kingpin.CommandLine, ":9393")

	// TODO(andrew-d): make configurable?
	resolutionTimeBuckets = []float64{
		60,     // 1 minute
		600,    // 10 minutes
		1800,   // 30 minutes
		3600,   // 1 hour
		14400,  // 4 hours
		28800,  // 8 hours
		86400,  // 24 hours
		172800, // 48 hours
		604800, // 1 week
	}

	logger = log.NewNopLogger()
)

const (
	namespace    = "mastodon"
	subsystem    = "exporter"
	exporterName = "mastodon_exporter"
)

func main() {
	kingpin.Version(version.Print(exporterName))
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger = promlog.New(promlogConfig)

	ctx := context.Background()

	level.Debug(logger).Log("msg", "Connecting to database", "database_url", *databaseURL)
	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		level.Error(logger).Log("msg", "Unable to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		level.Error(logger).Log("msg", "Unable to ping database", "err", err)
		os.Exit(1)
	}

	exporter := newExporter(pool)

	// Register the metrics that we're serving
	prometheus.MustRegister(version.NewCollector(exporterName))
	prometheus.MustRegister(exporter)

	// Unregister the Go and process collectors; this exporter doesn't do
	// enough to bother monitoring.
	prometheus.Unregister(prometheus.NewGoCollector())
	prometheus.Unregister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	level.Info(logger).Log("msg", "Starting mastodon_exporter", "version", version.BuildContext())
	srv := &http.Server{Handler: serverMetrics(*metricPath)}
	if err := web.ListenAndServe(srv, webConfig, logger); err != nil {
		level.Error(logger).Log("msg", "Error running HTTP server", "err", err)
		os.Exit(1)
	}
}

var indexTemplate = template.Must(template.New("").Parse(`
<html>
<head><title>Mastodon exporter</title></head>
<body>
<h1>Mastodon exporter</h1>
<p><a href='{{.MetricsPath}}'>Metrics</a></p>
</body>
</html>`))

func serverMetrics(metricsPath string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(metricsPath, promhttp.Handler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		indexTemplate.Execute(w, map[string]any{
			"MetricsPath": *metricPath,
		})
	})
	return mux
}

type mastodonExporter struct {
	db                  *pgxpool.Pool
	numAccounts         *prometheus.Desc
	numPosts            *prometheus.Desc
	numReports          *prometheus.Desc
	resolvedTimeSeconds *prometheus.Desc

	errors prometheus.Gauge
}

func newExporter(db *pgxpool.Pool) *mastodonExporter {
	ret := &mastodonExporter{
		db: db,
		numAccounts: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "num_accounts"),
			"Number of accounts on this Mastodon instance.",
			[]string{"type"}, nil,
		),
		numPosts: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "num_posts"),
			"Number of posts on this Mastodon instance.",
			nil, nil,
		),
		numReports: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "num_reports"),
			"Number of reports for this Mastodon instance.",
			[]string{"resolved"}, nil,
		),
		resolvedTimeSeconds: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "resolved_time_seconds"),
			"Time taken to resolve reports in this Mastodon instance.",
			nil, nil,
		),
		errors: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "errors",
			Help:      "Number of errors encountered while querying.",
		}),
	}
	return ret
}

func (m *mastodonExporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.numAccounts
	ch <- m.numPosts
	ch <- m.numReports
	ch <- m.resolvedTimeSeconds

	m.errors.Describe(ch)
}

func (m *mastodonExporter) Collect(ch chan<- prometheus.Metric) {
	m.scrape(context.Background(), ch)
}

func (m *mastodonExporter) scrape(ctx context.Context, ch chan<- prometheus.Metric) {
	defer level.Debug(logger).Log("msg", "scrape finished")

	var numErrors int

	level.Debug(logger).Log("msg", "Fetching number of reports")
	resolved, unresolved, err := m.getNumReports(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "Error querying number of reports", "err", err)
		numErrors++
	} else {
		ch <- prometheus.MustNewConstMetric(
			m.numReports,
			prometheus.GaugeValue,
			float64(resolved),
			"true",
		)
		ch <- prometheus.MustNewConstMetric(
			m.numReports,
			prometheus.GaugeValue,
			float64(unresolved),
			"false",
		)
	}

	level.Debug(logger).Log("msg", "Fetching report metrics")
	resolvedHist, err := m.getResolvedStats(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "Error querying report metrics", "err", err)
		numErrors++
	} else {
		ch <- resolvedHist
	}

	level.Debug(logger).Log("msg", "Fetching number of accounts")
	accounts, err := m.getNumAccounts(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "Error querying number of accounts", "err", err)
		numErrors++
	} else {
		for _, m := range accounts {
			ch <- m
		}
	}

	level.Debug(logger).Log("msg", "Fetching number of posts")
	numPosts, err := m.getNumPosts(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "Error querying number of posts", "err", err)
		numErrors++
	} else {
		ch <- prometheus.MustNewConstMetric(
			m.numPosts,
			prometheus.GaugeValue,
			float64(numPosts),
		)
	}

	m.errors.Set(float64(numErrors))
	ch <- m.errors
}

func (m *mastodonExporter) getNumReports(ctx context.Context) (resolved, unresolved int, err error) {
	err = m.db.QueryRow(ctx, `
		SELECT
		  COALESCE(COUNT(*) FILTER (WHERE action_taken_at IS NOT NULL), 0) AS resolved,
		  COALESCE(COUNT(*) FILTER (WHERE action_taken_at IS NULL), 0) AS unresolved
		FROM reports
	`).Scan(&resolved, &unresolved)
	return
}

func (m *mastodonExporter) getNumAccounts(ctx context.Context) ([]prometheus.Metric, error) {
	type labelVal struct {
		Name string
		Val  int
	}
	var (
		unsuspended = labelVal{Name: "unsuspended"}
		bots        = labelVal{Name: "bots"}
		groups      = labelVal{Name: "groups"}
		people      = labelVal{Name: "people"}
		suspended   = labelVal{Name: "suspended"}
	)
	err := m.db.QueryRow(ctx, `
		WITH unsuspended AS (
		  SELECT * FROM accounts
		  WHERE domain IS NULL AND suspended_at IS NULL
		),
		unsuspended_stats AS (
		  SELECT COALESCE(COUNT(*), 0) AS unsuspended
		       , COALESCE(COUNT(*) FILTER (WHERE actor_type = 'Application' OR actor_type = 'Service'), 0) AS bots
		       , COALESCE(COUNT(*) FILTER (WHERE actor_type = 'Group'), 0) AS groups
		       , COALESCE(COUNT(*) FILTER (WHERE actor_type = 'Person' OR actor_type IS NULL), 0) AS people
		  FROM unsuspended
		),
		suspended AS (
		  SELECT COUNT(*) AS num_suspended
		  FROM accounts
		  WHERE domain IS NULL AND suspended_at IS NOT NULL
		)
		SELECT
		  a.unsuspended,
		  a.bots,
		  a.groups,
		  a.people,
		  b.num_suspended
		FROM unsuspended_stats AS a, suspended AS b
	`).Scan(&unsuspended.Val, &bots.Val, &groups.Val, &people.Val, &suspended.Val)
	if err != nil {
		return nil, err
	}

	var ret []prometheus.Metric
	for _, lv := range []labelVal{unsuspended, bots, groups, people, suspended} {
		ret = append(ret, prometheus.MustNewConstMetric(
			m.numAccounts,
			prometheus.GaugeValue,
			float64(lv.Val),
			lv.Name,
		))
	}
	return ret, nil
}

func (m *mastodonExporter) getNumPosts(ctx context.Context) (count int, err error) {
	err = m.db.QueryRow(ctx, `
		SELECT
		  SUM(s.statuses_count)
		FROM accounts AS a
		JOIN account_stats AS s
		  ON a.id = s.account_id
		WHERE a.domain IS NULL
	`).Scan(&count)
	return
}

func (m *mastodonExporter) getResolvedStats(ctx context.Context) (prometheus.Metric, error) {
	rows, err := m.db.Query(ctx, `
		SELECT
		  extract(EPOCH FROM (action_taken_at - created_at)) AS time_to_resolution
		FROM reports
		WHERE action_taken_at IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("querying database: %w", err)
	}

	numbers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (float64, error) {
		var n float64
		if err := row.Scan(&n); err != nil {
			return 0, fmt.Errorf("scanning row: %w", err)
		}
		return n, nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning rows: %w", err)
	}

	return m.resolvedMetricFromNums(numbers), nil
}

func (m *mastodonExporter) resolvedMetricFromNums(numbers []float64) prometheus.Metric {
	var (
		sum     float64
		buckets = make(map[float64]uint64)
	)
	for _, num := range numbers {
		sum += num
		for _, bucket := range resolutionTimeBuckets {
			if num <= bucket {
				buckets[bucket]++
			}
		}
	}

	return prometheus.MustNewConstHistogram(
		m.resolvedTimeSeconds,
		uint64(len(numbers)),
		sum,
		buckets,
	)
}
