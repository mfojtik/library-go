package controllercmd

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/wait"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/config/configdefaults"
	leaderelectionconverter "github.com/openshift/library-go/pkg/config/leaderelection"
	"github.com/openshift/library-go/pkg/config/serving"
	"github.com/openshift/library-go/pkg/controller/fileobserver"
	"github.com/openshift/library-go/pkg/operator/events"
)

// StartFunc is the function to call on leader election start
type StartFunc func(config *rest.Config, eventRecorder events.Recorder, stop <-chan struct{}) error

// defaultObserverInterval specifies the default interval that file observer will do rehash the files it watches and react to any changes
// in those files.
var defaultObserverInterval = 5 * time.Second

// OperatorBuilder allows the construction of an controller in optional pieces.
type ControllerBuilder struct {
	kubeAPIServerConfigFile *string
	clientOverrides         *client.ClientConnectionOverrides
	leaderElection          *configv1.LeaderElection
	fileObserver            fileobserver.Observer
	eventRecorder           events.Recorder

	startFunc        StartFunc
	componentName    string
	instanceIdentity string
	observerInterval time.Duration

	servingInfo          *configv1.HTTPServingInfo
	authenticationConfig *operatorv1alpha1.DelegatedAuthentication
	authorizationConfig  *operatorv1alpha1.DelegatedAuthorization
	healthChecks         []healthz.HealthzChecker
}

// NewController returns a builder struct for constructing the command you want to run
func NewController(componentName string, startFunc StartFunc) *ControllerBuilder {
	return &ControllerBuilder{
		startFunc:        startFunc,
		componentName:    componentName,
		observerInterval: defaultObserverInterval,
	}
}

// WithRestartOnChange will enable a file observer controller loop that observes changes into specified files. If a change to a file is detected,
// the process will be terminated with return code zero.
func (b *ControllerBuilder) WithRestartOnChange(stopChan chan struct{}, files ...string) *ControllerBuilder {
	if len(files) == 0 {
		return b
	}
	if b.fileObserver == nil {
		observer, err := fileobserver.NewObserver(b.observerInterval)
		if err != nil {
			panic(err)
		}
		b.fileObserver = observer
	}
	reactorFunc := func(filename string, action fileobserver.ActionType) error {
		message := fmt.Sprintf("Restart triggered because %s", action.String(filename))
		if b.eventRecorder != nil {
			b.eventRecorder.Event("OperatorRestarted", message)
		}
		glog.Warning(message)
		close(stopChan)
		return nil
	}
	b.fileObserver.AddReactor(reactorFunc, files...)
	return b
}

// WithEventRecorder will enable event recording for this controller. To make this work, the 'EVENT_SOURCE_POD_NAME' environment variable has
// to be present in system with a name of the pod that is currently running the container.
func (b *ControllerBuilder) WithEventRecorder() *ControllerBuilder {
	clientConfig, err := b.getClientConfig()
	if err != nil {
		panic(fmt.Sprintf("error getting client config: %v", err))
	}
	kubeClient := kubernetes.NewForConfigOrDie(clientConfig)
	namespace, err := b.getNamespace()
	if err != nil {
		panic("unable to read the namespace")
	}
	controllerRef, err := events.GetControllerReferenceForCurrentPod(kubeClient.CoreV1().Pods(namespace))
	if err != nil {
		panic(fmt.Sprintf("unable to obtain replicaset reference for events: %v", err))
	}

	b.eventRecorder = events.NewRecorder(kubeClient.CoreV1().Events(namespace), b.componentName, controllerRef)
	return b
}

// WithLeaderElection adds leader election options
func (b *ControllerBuilder) WithLeaderElection(leaderElection configv1.LeaderElection, defaultNamespace, defaultName string) *ControllerBuilder {
	if leaderElection.Disable {
		return b
	}

	defaulted := leaderelectionconverter.LeaderElectionDefaulting(leaderElection, defaultNamespace, defaultName)
	b.leaderElection = &defaulted
	return b
}

// WithServer adds a server that provides metrics and healthz
func (b *ControllerBuilder) WithServer(servingInfo configv1.HTTPServingInfo, authenticationConfig operatorv1alpha1.DelegatedAuthentication, authorizationConfig operatorv1alpha1.DelegatedAuthorization) *ControllerBuilder {
	b.servingInfo = servingInfo.DeepCopy()
	configdefaults.SetRecommendedHTTPServingInfoDefaults(b.servingInfo)
	b.authenticationConfig = &authenticationConfig
	b.authorizationConfig = &authorizationConfig
	return b
}

// WithHealthChecks adds a list of healthchecks to the server
func (b *ControllerBuilder) WithHealthChecks(healthChecks ...healthz.HealthzChecker) *ControllerBuilder {
	b.healthChecks = append(b.healthChecks, healthChecks...)
	return b
}

// WithKubeConfigFile sets an optional kubeconfig file. inclusterconfig will be used if filename is empty
func (b *ControllerBuilder) WithKubeConfigFile(kubeConfigFilename string, defaults *client.ClientConnectionOverrides) *ControllerBuilder {
	b.kubeAPIServerConfigFile = &kubeConfigFilename
	b.clientOverrides = defaults
	return b
}

// WithInstanceIdentity sets the instance identity to use if you need something special. The default is just a UID which is
// usually fine for a pod.
func (b *ControllerBuilder) WithInstanceIdentity(identity string) *ControllerBuilder {
	b.instanceIdentity = identity
	return b
}

// Run starts your controller for you.  It uses leader election if you asked, otherwise it directly calls you
func (b *ControllerBuilder) Run(stopCh <-chan struct{}) error {
	clientConfig, err := b.getClientConfig()
	if err != nil {
		return err
	}

	if b.fileObserver != nil {
		go b.fileObserver.Run(stopCh)
	}

	switch {
	case b.servingInfo == nil && len(b.healthChecks) > 0:
		return fmt.Errorf("healthchecks without server config won't work")

	default:
		kubeConfig := ""
		if b.kubeAPIServerConfigFile != nil {
			kubeConfig = *b.kubeAPIServerConfigFile
		}
		serverConfig, err := serving.ToServerConfig(*b.servingInfo, *b.authenticationConfig, *b.authorizationConfig, kubeConfig)
		if err != nil {
			return err
		}
		serverConfig.HealthzChecks = append(serverConfig.HealthzChecks, b.healthChecks...)

		server, err := serverConfig.Complete(nil).New(b.componentName, genericapiserver.NewEmptyDelegate())
		if err != nil {
			return err
		}

		go func() {
			if err := server.PrepareRun().Run(stopCh); err != nil {
				glog.Error(err)
			}
			glog.Fatal("server exited")
		}()
	}

	if b.leaderElection == nil {
		if err := b.startFunc(clientConfig, b.eventRecorder, wait.NeverStop); err != nil {
			return err
		}
		return fmt.Errorf("exited")
	}

	leaderElection, err := leaderelectionconverter.ToConfigMapLeaderElection(clientConfig, *b.leaderElection, b.componentName, b.instanceIdentity)
	if err != nil {
		return err
	}

	leaderElection.Callbacks.OnStartedLeading = func(stop <-chan struct{}) {
		if err := b.startFunc(clientConfig, b.eventRecorder, stop); err != nil {
			glog.Fatal(err)
		}
	}
	leaderelection.RunOrDie(leaderElection)
	return fmt.Errorf("exited")
}

func (b *ControllerBuilder) getNamespace() (string, error) {
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "", err
	}
	return string(nsBytes), err
}

func (b *ControllerBuilder) getClientConfig() (*rest.Config, error) {
	kubeconfig := ""
	if b.kubeAPIServerConfigFile != nil {
		kubeconfig = *b.kubeAPIServerConfigFile
	}

	return client.GetKubeConfigOrInClusterConfig(kubeconfig, b.clientOverrides)
}
