package events

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

type fakeEventRecorder struct {
	events []*corev1.Event
}

// fakeObjectReference is used for fake events.
var fakeObjectReference = corev1.ObjectReference{
	Kind:       "Pod",
	Namespace:  "test",
	Name:       "test",
	APIVersion: "v1",
}

type FakeRecorder interface {
	Events() []*corev1.Event
	Recorder
}

// NewFakeRecorder provides an event recorder that records the event internally and allow to retrieve all recorder events in unit tests.
func NewFakeRecorder() FakeRecorder {
	return &fakeEventRecorder{events: []*corev1.Event{}}
}

// Events returns list of recorded events
func (r *fakeEventRecorder) Events() []*corev1.Event {
	return r.events
}

func (r *fakeEventRecorder) Event(reason, message string) {
	r.events = append(r.events, makeEvent(&fakeObjectReference, "test", corev1.EventTypeNormal, reason, message))
}

func (r *fakeEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *fakeEventRecorder) Warning(reason, message string) {
	r.events = append(r.events, makeEvent(&fakeObjectReference, "test", corev1.EventTypeWarning, reason, message))
}

func (r *fakeEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}
