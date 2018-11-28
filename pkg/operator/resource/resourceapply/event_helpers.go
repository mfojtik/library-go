package resourceapply

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	kubescheme "k8s.io/client-go/kubernetes/scheme"

	openshiftapi "github.com/openshift/api"
	"github.com/openshift/library-go/pkg/operator/events"
)

// guessObjectKind returns a human name for the passed runtime object.
func guessObjectGroupKind(object runtime.Object) (string, string) {
	if gvk := object.GetObjectKind().GroupVersionKind(); len(gvk.Kind) > 0 {
		return gvk.Group, gvk.Kind
	}
	if kinds, _, _ := kubescheme.Scheme.ObjectKinds(object); len(kinds) > 0 {
		return kinds[0].Group, kinds[0].Kind
	}
	scheme := runtime.NewScheme()
	if err := openshiftapi.Install(scheme); err != nil {
		return "unknown", "Object"
	}
	if kinds, _, _ := scheme.ObjectKinds(object); len(kinds) > 0 {
		return kinds[0].Group, kinds[0].Kind
	}
	return "unknown", "Object"

}

func reportCreateEvent(recorder events.Recorder, obj runtime.Object, originalErr error) {
	reportingKind, reportingGroup := guessObjectGroupKind(obj)
	accessor, err := meta.Accessor(obj)
	if err != nil {
		glog.Errorf("Failed to get accessor for %+v", obj)
		return
	}
	namespace := ""
	if len(accessor.GetNamespace()) > 0 {
		namespace = " -n " + accessor.GetNamespace()
	}
	if originalErr == nil {
		recorder.Eventf(fmt.Sprintf("%sCreated", reportingKind), "Created %s.%s/%s%s %q because it was missing", reportingKind, reportingGroup, namespace, accessor.GetName())
		return
	}
	recorder.Warningf(fmt.Sprintf("%sCreateFailed", reportingKind), "Failed to create %s.%s/%s%s %q: %v", reportingKind, reportingGroup, namespace, accessor.GetName(), originalErr)
}

func reportUpdateEvent(recorder events.Recorder, obj runtime.Object, originalErr error) {
	reportingKind, reportingGroup := guessObjectGroupKind(obj)
	accessor, err := meta.Accessor(obj)
	if err != nil {
		glog.Errorf("Failed to get accessor for %+v", obj)
		return
	}
	namespace := ""
	if len(accessor.GetNamespace()) > 0 {
		namespace = " -n " + accessor.GetNamespace()
	}
	if originalErr == nil {
		recorder.Eventf(fmt.Sprintf("%sUpdated", reportingKind), "Updated %s.%s/%s%s %q because it changed", reportingKind, reportingGroup, namespace, accessor.GetName())
		return
	}
	recorder.Warningf(fmt.Sprintf("%sUpdateFailed", reportingKind), "Failed to update %s.%s/%s%s %q: %v", reportingKind, reportingGroup, namespace, accessor.GetName(), originalErr)
}
