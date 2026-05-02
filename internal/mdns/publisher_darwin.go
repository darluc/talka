//go:build darwin

package mdns

type systemRunner struct{}

func NewSystemPublisher() Publisher {
	return NewPublisher(systemRunner{})
}
