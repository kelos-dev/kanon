package core

func hasValues[T comparable](values []T) bool {
	var zero T
	for _, value := range values {
		if value != zero {
			return true
		}
	}
	return false
}
