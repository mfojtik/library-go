package installerretrymonitor

import (
	"fmt"

	k8smetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	namespace = "openshift"
	subsystem = "static_pod_installer"
)

var metrics *installerRetryMetrics

func init() {
	metrics = newInstallerRetryMetrics(legacyregistry.Register)
}

type installerRetryMetrics struct {
	installerRestarts *k8smetrics.GaugeVec
}

func newInstallerRetryMetrics(registerFunc func(k8smetrics.Registerable) error) *installerRetryMetrics {
	installerRestarts := k8smetrics.NewGaugeVec(
		&k8smetrics.GaugeOpts{
			Namespace: namespace,
			Subsystem: subsystem,
			Name:      "restart_count",
			Help:      "The total number of failures observed for a single static pod installer per node and target revision",
		}, []string{"node", "revision"})
	registerFunc(installerRestarts)

	return &installerRetryMetrics{
		installerRestarts: installerRestarts,
	}
}

func (m *installerRetryMetrics) Reset() {
	m.installerRestarts.Reset()
}

func (m *installerRetryMetrics) ObserveInstallerRestarts(observed int, targetRevision int32, node string) {
	m.installerRestarts.WithLabelValues(node, fmt.Sprintf("%d", targetRevision)).Set(float64(observed))
}
