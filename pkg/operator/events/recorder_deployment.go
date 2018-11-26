package events

import (
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

// eventSourcePodNameEnv is a name of environment variable inside container that specifies the name of the current replica set.
// This replica set name is then used as a source/involved object for operator events.
const eventSourcePodNameEnv = "EVENT_SOURCE_POD_NAME"

// eventSourcePodNameEnvFunc allows to override the way we get the environment variable value (for unit tests).
var eventSourcePodNameEnvFunc = func() string {
	return os.Getenv(eventSourcePodNameEnv)
}

// GetControllerReferenceForCurrentPod provides an object reference to a controller managing the pod/container where this process runs.
// The pod name must be provided via the EVENT_SOURCE_POD_NAME name.
func GetControllerReferenceForCurrentPod(client corev1client.PodInterface) (*corev1.ObjectReference, error) {
	podName := eventSourcePodNameEnvFunc()
	if len(podName) == 0 {
		return nil, fmt.Errorf("unable to setup event recorder as %q env variable is not set", eventSourcePodNameEnv)
	}
	pod, err := client.Get(podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	ownerRef := metav1.GetControllerOf(pod)
	return &corev1.ObjectReference{
		Kind:       ownerRef.Kind,
		Namespace:  pod.Namespace,
		Name:       ownerRef.Name,
		UID:        ownerRef.UID,
		APIVersion: ownerRef.APIVersion,
	}, nil
}
