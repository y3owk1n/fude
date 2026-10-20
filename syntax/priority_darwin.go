package syntax

// #include <pthread/qos.h>
import "C"

import "fmt"

// background lowers the calling thread to utility QoS. macOS then runs the
// threads that answer keys, in this fude and others, before it, and prefers
// the efficiency cores for it.
func background() error {
	if rc := C.pthread_set_qos_class_self_np(C.QOS_CLASS_UTILITY, 0); rc != 0 {
		return fmt.Errorf("set utility QoS: error %d", rc)
	}
	return nil
}
