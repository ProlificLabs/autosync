//go:build cgo

package yrs

/*
#cgo LDFLAGS: -L. -lyrs
#include <libyrs.h>
#include <string.h>
*/
import "C"

// A Go‑level wrapper for YDoc that cleans itself up.
type Doc struct{ ptr *C.YDoc }

func NewDoc() *Doc   { return &Doc{C.ydoc_new()} }
func (d *Doc) Free() { C.ydoc_destroy(d.ptr); d.ptr = nil }

// Example: create a YText, insert a string, and read it back.
func (d *Doc) Hello() string {
	txt := C.ytext(d.ptr, C.CString("name"))
	txn := C.ydoc_write_transaction(d.ptr)
	C.ytext_insert(txt, txn, 0, C.CString("hello from Go + Yrs"), nil)
	C.ytransaction_commit(txn)

	// read it back
	txn2 := C.ydoc_read_transaction(d.ptr)
	raw := C.ytext_string(txt, txn2)
	goStr := C.GoString(raw)
	C.ystring_destroy(raw)
	C.ytransaction_commit(txn2)
	return goStr
}
