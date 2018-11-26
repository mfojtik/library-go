package events

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
)

func fakeRecorderSource(t *testing.T) *corev1.ObjectReference {
	eventSourcePodNameEnvFunc = func() string {
		return "test"
	}
	client := fake.NewSimpleClientset(makeFakeReplicaSetPod("test-namespace", "test"))
	ref, err := GetControllerReferenceForCurrentPod(client.CoreV1().Pods("test-namespace"))
	if err != nil {
		t.Fatalf("unable to get replicaset object reference: %v", err)
	}
	return ref
}

func TestNewRecorder(t *testing.T) {
	client := fake.NewSimpleClientset()
	r := NewRecorder(client.CoreV1().Events("test-namespace"), "test-operator", fakeRecorderSource(t))

	r.Event("TestReason", "foo")

	var createdEvent *corev1.Event

	for _, action := range client.Actions() {
		if action.Matches("create", "events") {
			createAction := action.(clientgotesting.CreateAction)
			createdEvent = createAction.GetObject().(*corev1.Event)
			break
		}
	}
	if createdEvent == nil {
		t.Fatalf("expected event to be created")
	}
	if createdEvent.InvolvedObject.Kind != "ReplicaSet" {
		t.Errorf("expected involved object kind ReplicaSet, got: %q", createdEvent.InvolvedObject.Kind)
	}
	if createdEvent.InvolvedObject.Namespace != "test-namespace" {
		t.Errorf("expected involved object namespace test-namespace, got: %q", createdEvent.InvolvedObject.Namespace)
	}
	if createdEvent.Reason != "TestReason" {
		t.Errorf("expected event to have TestReason, got %q", createdEvent.Reason)
	}
	if createdEvent.Message != "foo" {
		t.Errorf("expected message to be foo, got %q", createdEvent.Message)
	}
	if createdEvent.Type != "Normal" {
		t.Errorf("expected event type to be Normal, got %q", createdEvent.Type)
	}
	if createdEvent.Source.Component != "test-operator" {
		t.Errorf("expected event source to be test-operator, got %q", createdEvent.Source.Component)
	}
}
