package installerpod

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/openshift/library-go/pkg/config/client"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/resource/retry"
	"github.com/openshift/library-go/pkg/operator/staticpod/resourcecopy"
)

type InstallOptions struct {
	// TODO replace with genericclioptions
	KubeConfig string
	KubeClient kubernetes.Interface

	Revision  string
	Namespace string

	PodConfigMapNamePrefix        string
	SecretNamePrefixes            []string
	OptionalSecretNamePrefixes    []string
	ConfigMapNamePrefixes         []string
	OptionalConfigMapNamePrefixes []string

	ResourceDir    string
	PodManifestDir string

	Timeout time.Duration

	PodMutationFns []PodMutationFunc
}

// PodMutationFunc is a function that has a chance at changing the pod before it is created
type PodMutationFunc func(pod *corev1.Pod) error

func NewInstallOptions() *InstallOptions {
	return &InstallOptions{}
}

func (o *InstallOptions) WithPodMutationFn(podMutationFn PodMutationFunc) *InstallOptions {
	o.PodMutationFns = append(o.PodMutationFns, podMutationFn)
	return o
}

func NewInstaller() *cobra.Command {
	o := NewInstallOptions()

	cmd := &cobra.Command{
		Use:   "installer",
		Short: "Install static pod and related resources",
		Run: func(cmd *cobra.Command, args []string) {
			glog.V(1).Info(cmd.Flags())
			glog.V(1).Info(spew.Sdump(o))

			if err := o.Complete(); err != nil {
				glog.Fatal(err)
			}
			if err := o.Validate(); err != nil {
				glog.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.TODO(), o.Timeout)
			defer cancel()
			if err := o.Run(ctx); err != nil {
				glog.Fatal(err)
			}
		},
	}

	o.AddFlags(cmd.Flags())

	return cmd
}

func (o *InstallOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.KubeConfig, "kubeconfig", o.KubeConfig, "kubeconfig file or empty")
	fs.StringVar(&o.Revision, "revision", o.Revision, "identifier for this particular installation instance.  For example, a counter or a hash")
	fs.StringVar(&o.Namespace, "namespace", o.Namespace, "namespace to retrieve all resources from and create the static pod in")
	fs.StringVar(&o.PodConfigMapNamePrefix, "pod", o.PodConfigMapNamePrefix, "name of configmap that contains the pod to be created")
	fs.StringSliceVar(&o.SecretNamePrefixes, "secrets", o.SecretNamePrefixes, "list of secret names to be included")
	fs.StringSliceVar(&o.ConfigMapNamePrefixes, "configmaps", o.ConfigMapNamePrefixes, "list of configmaps to be included")
	fs.StringSliceVar(&o.OptionalSecretNamePrefixes, "optional-secrets", o.OptionalSecretNamePrefixes, "list of optional secret names to be included")
	fs.StringSliceVar(&o.OptionalConfigMapNamePrefixes, "optional-configmaps", o.OptionalConfigMapNamePrefixes, "list of optional configmaps to be included")
	fs.StringVar(&o.ResourceDir, "resource-dir", o.ResourceDir, "directory for all files supporting the static pod manifest")
	fs.StringVar(&o.PodManifestDir, "pod-manifest-dir", o.PodManifestDir, "directory for the static pod manifest")
	fs.DurationVar(&o.Timeout, "timeout-duration", 120*time.Second, "maximum time in seconds to wait for the copying to complete (default: 2m)")
}

func (o *InstallOptions) Complete() error {
	clientConfig, err := client.GetKubeConfigOrInClusterConfig(o.KubeConfig, nil)
	if err != nil {
		return err
	}

	// Use protobuf to fetch configmaps and secrets and create pods.
	protoConfig := rest.CopyConfig(clientConfig)
	protoConfig.AcceptContentTypes = "application/vnd.kubernetes.protobuf,application/json"
	protoConfig.ContentType = "application/vnd.kubernetes.protobuf"

	o.KubeClient, err = kubernetes.NewForConfig(protoConfig)
	if err != nil {
		return err
	}
	return nil
}

func (o *InstallOptions) Validate() error {
	if len(o.Revision) == 0 {
		return fmt.Errorf("--revision is required")
	}
	if len(o.Namespace) == 0 {
		return fmt.Errorf("--namespace is required")
	}
	if len(o.PodConfigMapNamePrefix) == 0 {
		return fmt.Errorf("--pod is required")
	}
	if len(o.ConfigMapNamePrefixes) == 0 {
		return fmt.Errorf("--configmaps is required")
	}
	if o.Timeout == 0 {
		return fmt.Errorf("--timeout-duration cannot be 0")
	}

	if o.KubeClient == nil {
		return fmt.Errorf("missing client")
	}

	return nil
}

func (o *InstallOptions) nameFor(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, o.Revision)
}

func (o *InstallOptions) prefixFor(name string) string {
	return name[0 : len(name)-len(fmt.Sprintf("-%s", o.Revision))]
}

func (o *InstallOptions) copyContent(ctx context.Context) error {
	secretPrefixes := sets.NewString(o.SecretNamePrefixes...)
	optionalSecretPrefixes := sets.NewString(o.OptionalSecretNamePrefixes...)
	configPrefixes := sets.NewString(o.ConfigMapNamePrefixes...)
	optionalConfigPrefixes := sets.NewString(o.OptionalConfigMapNamePrefixes...)

	resourceDir := path.Join(o.ResourceDir, o.nameFor(o.PodConfigMapNamePrefix))
	glog.Infof("Creating target resource directory %q ...", resourceDir)
	if err := os.MkdirAll(resourceDir, 0755); err != nil {
		return err
	}

	secretSources := []resourcecopy.Source{}
	for _, secretPrefix := range append(secretPrefixes.List(), optionalSecretPrefixes.List()...) {
		if optionalSecretPrefixes.Has(secretPrefix) {
			secretSources = append(secretSources, resourcecopy.NewOptionalSourceWithMutation(o.Namespace, secretPrefix, o.nameFor))
			continue
		}
		secretSources = append(secretSources, resourcecopy.NewSourceWithMutation(o.Namespace, secretPrefix, o.nameFor))
	}

	err := resourcecopy.CopySecrets(ctx, o.KubeClient.CoreV1(), secretSources, func(secret *v1.Secret) error {
		contentDir := path.Join(resourceDir, "secrets", o.prefixFor(secret.Name))
		glog.Infof("Creating directory %q ...", contentDir)
		if err := os.MkdirAll(contentDir, 0755); err != nil {
			return err
		}
		for filename, content := range secret.Data {
			// TODO fix permissions
			glog.Infof("Writing secret manifest %q ...", path.Join(contentDir, filename))
			if err := ioutil.WriteFile(path.Join(contentDir, filename), content, 0644); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to copy secrets: %v", err)
	}

	configSources := []resourcecopy.Source{}
	for _, configPrefix := range append(configPrefixes.List(), optionalConfigPrefixes.List()...) {
		if optionalConfigPrefixes.Has(configPrefix) {
			configSources = append(configSources, resourcecopy.NewOptionalSourceWithMutation(o.Namespace, configPrefix, o.nameFor))
			continue
		}
		configSources = append(configSources, resourcecopy.NewSourceWithMutation(o.Namespace, configPrefix, o.nameFor))
	}

	err = resourcecopy.CopyConfigMaps(ctx, o.KubeClient.CoreV1(), configSources, func(config *v1.ConfigMap) error {
		contentDir := path.Join(resourceDir, "configmaps", o.prefixFor(config.Name))
		glog.Infof("Creating directory %q ...", contentDir)
		if err := os.MkdirAll(contentDir, 0755); err != nil {
			return err
		}
		for filename, content := range config.Data {
			// TODO fix permissions
			glog.Infof("Writing config map manifest %q ...", path.Join(contentDir, filename))
			if err := ioutil.WriteFile(path.Join(contentDir, filename), []byte(content), 0644); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to copy config maps: %v", err)
	}

	// Gather pod yaml from config map
	var podContent string
	err = retry.RetryOnConnectionErrors(ctx, func(ctx context.Context) (bool, error) {
		glog.Infof("Getting pod configmaps/%s -n %s", o.nameFor(o.PodConfigMapNamePrefix), o.Namespace)
		podConfigMap, err := o.KubeClient.CoreV1().ConfigMaps(o.Namespace).Get(o.nameFor(o.PodConfigMapNamePrefix), metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		podData, exists := podConfigMap.Data["pod.yaml"]
		if !exists {
			return true, fmt.Errorf("required 'pod.yaml' key does not exist in configmap")
		}
		podContent = strings.Replace(podData, "REVISION", o.Revision, -1)
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("failed to get pod content from config map: %v", err)
	}

	podFileName := o.PodConfigMapNamePrefix + ".yaml"
	glog.Infof("Writing pod manifest %q ...", path.Join(resourceDir, podFileName))
	if err := ioutil.WriteFile(path.Join(resourceDir, podFileName), []byte(podContent), 0644); err != nil {
		return err
	}

	// copy static pod
	glog.Infof("Creating directory for static pod manifest %q ...", o.PodManifestDir)
	if err := os.MkdirAll(o.PodManifestDir, 0755); err != nil {
		return err
	}

	for _, fn := range o.PodMutationFns {
		glog.V(2).Infof("Customizing static pod ...")
		pod := resourceread.ReadPodV1OrDie([]byte(podContent))
		if err := fn(pod); err != nil {
			return err
		}
		podContent = resourceread.WritePodV1OrDie(pod)
	}

	glog.Infof("Writing static pod manifest %q ...\n%s", path.Join(o.PodManifestDir, podFileName), podContent)
	if err := ioutil.WriteFile(path.Join(o.PodManifestDir, podFileName), []byte(podContent), 0644); err != nil {
		return err
	}

	return nil
}

func (o *InstallOptions) Run(ctx context.Context) error {
	var eventTarget *corev1.ObjectReference

	if err := retry.RetryOnConnectionErrors(ctx, func(context.Context) (bool, error) {
		var clientErr error
		eventTarget, clientErr = events.GetControllerReferenceForCurrentPod(o.KubeClient, o.Namespace, nil)
		if clientErr != nil {
			return false, clientErr
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to get self-reference: %v", err)
	}

	recorder := events.NewRecorder(o.KubeClient.CoreV1().Events(o.Namespace), "static-pod-installer", eventTarget)
	if err := o.copyContent(ctx); err != nil {
		recorder.Warningf("StaticPodInstallerFailed", "Installing revision %s: %v", o.Revision, err)
		return fmt.Errorf("failed to copy: %v", err)
	}

	recorder.Eventf("StaticPodInstallerCompleted", "Successfully installed revision %s", o.Revision)
	return nil
}
