package events

import (
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func makeFakeReplicaSetPod(namespace, name string) *v1.Pod {
	pod := v1.Pod{}
	pod.Name = name
	pod.Namespace = namespace
	pod.TypeMeta.Kind = "Pod"
	pod.TypeMeta.APIVersion = "v1"
	truePtr := true
	pod.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         "apps/v1",
			Kind:               "ReplicaSet",
			Name:               "test-766b85794f",
			UID:                "05022234-d394-11e8-8169-42010a8e0003",
			Controller:         &truePtr,
			BlockOwnerDeletion: &truePtr,
		},
	})
	return &pod
}

func TestGetReplicaSetOwnerReference(t *testing.T) {
	client := fake.NewSimpleClientset(makeFakeReplicaSetPod("test", "foo"))

	eventSourcePodNameEnvFunc = func() string {
		return "foo"
	}

	objectReference, err := GetControllerReferenceForCurrentPod(client.CoreV1().Pods("test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if objectReference.Name != "test-766b85794f" {
		t.Errorf("expected objectReference name to be 'test-766b85794f', got %q", objectReference.Name)
	}

	if objectReference.GroupVersionKind().String() != "apps/v1, Kind=ReplicaSet" {
		t.Errorf("expected objectReference to be ReplicaSet, got %q", objectReference.GroupVersionKind().String())
	}
}
