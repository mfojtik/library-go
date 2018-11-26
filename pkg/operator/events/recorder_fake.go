package events

import (
	"fmt"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
)

type dummyEventRecorder struct {
	events []*corev1.Event
	source string
}

// dummyObjectReference is used for fake events.
var dummyObjectReference = corev1.ObjectReference{
	Kind:       "Pod",
	Namespace:  "dummy",
	Name:       "dummy",
	APIVersion: "v1",
}

type DummyRecorder interface {
	Events() []*corev1.Event
	Recorder
}

// NewDummyRecorder provides an event recorder that records the event internally and allow to retrieve all recorder events in unit tests.
func NewDummyRecorder(sourceComponent string) DummyRecorder {
	return &dummyEventRecorder{events: []*corev1.Event{}, source: sourceComponent}
}

// Events returns list of recorded events
func (r *dummyEventRecorder) Events() []*corev1.Event {
	return r.events
}

func (r *dummyEventRecorder) Event(reason, message string) {
	event := makeEvent(&dummyObjectReference, r.source, corev1.EventTypeNormal, reason, message)
	glog.Info(event.String())
	r.events = append(r.events, event)
}

func (r *dummyEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *dummyEventRecorder) Warning(reason, message string) {
	event := makeEvent(&dummyObjectReference, r.source, corev1.EventTypeWarning, reason, message)
	glog.Info(event.String())
	r.events = append(r.events, event)
}

func (r *dummyEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}
