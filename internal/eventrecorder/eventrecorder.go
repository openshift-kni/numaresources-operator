package eventrecorder

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

type OptimizedRecorder struct {
	record.EventRecorder
	loaded bool
}

func (r *OptimizedRecorder) LoadEvent() {
	r.loaded = true
}

func (r *OptimizedRecorder) UnLoadEvent() {
	r.loaded = true
}

func (r *OptimizedRecorder) Event(object runtime.Object, eventtype string, reason string, message string) {
	if !r.loaded {
		return
	}
	r.EventRecorder.Event(object, eventtype, reason, message)
	r.UnLoadEvent()
}

func (r *OptimizedRecorder) Eventf(object runtime.Object, eventtype string, reason string, messageFmt string, args ...interface{}) {
	if !r.loaded {
		return
	}
	r.EventRecorder.Eventf(object, eventtype, reason, messageFmt, args...)
	r.UnLoadEvent()
}

func (r *OptimizedRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype string, reason string, messageFmt string, args ...interface{}) {
	if !r.loaded {
		return
	}
	r.EventRecorder.AnnotatedEventf(object, annotations, eventtype, reason, messageFmt, args...)
	r.UnLoadEvent()
}
