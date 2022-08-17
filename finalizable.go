package mamo

type Finalizable interface {
	MamoFinalize() error
}

func isFinalizable[T any]() bool {
	var x T
	var dummy any = x
	if _, ok := dummy.(Finalizable); ok {
		return true
	}
	return false
}
