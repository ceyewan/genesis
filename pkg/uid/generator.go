package uid

// Generator defines the contract for unique ID production.
type Generator interface {
	Generate() (int64, error)
	Batch(size int) ([]int64, error)
}
