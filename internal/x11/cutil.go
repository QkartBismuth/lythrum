package x11

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

func freeCString(p *C.char) { C.free(unsafe.Pointer(p)) }
