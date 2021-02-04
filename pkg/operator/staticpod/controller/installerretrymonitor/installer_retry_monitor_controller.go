package installerretrymonitor

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type installerRetryMonitor struct {
	operatorClient v1helpers.StaticPodOperatorClient
	recorder       events.Recorder
}

// NewInstallerRetryMonitor returns a controller that observe changes in static pod operator resource status and propagate installer pod retry counts into Prometheus metrics.
func NewInstallerRetryMonitor(operatorClient v1helpers.StaticPodOperatorClient, recorder events.Recorder) factory.Controller {
	monitor := installerRetryMonitor{
		operatorClient: operatorClient,
		recorder:       recorder,
	}
	return factory.New().ResyncEvery(30*time.Second).WithSync(monitor.sync).WithInformers(operatorClient.Informer()).ToController("InstallerRetryMonitor", recorder)
}

func (m *installerRetryMonitor) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	_, originalOperatorStatus, _, err := m.operatorClient.GetStaticPodOperatorState()
	if err != nil {
		return err
	}
	for _, n := range originalOperatorStatus.NodeStatuses {
		// reduce cardinality of this metric by only reporting nodes with actual failures
		if n.LastFailedCount > 1 {
			continue
		}
		metrics.ObserveInstallerRestarts(n.LastFailedCount, n.TargetRevision, n.NodeName)
	}
	return nil
}
