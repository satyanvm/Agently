package domain

// Ptr returns a pointer to v. Handy for the many nullable struct fields when
// building seed data and patches.
func Ptr[T any](v T) *T { return &v }
