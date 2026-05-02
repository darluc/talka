package inject

import "context"

type RestoreStatus string

const (
	RestoreStatusDisabled       RestoreStatus = "disabled"
	RestoreStatusRestored       RestoreStatus = "restored"
	RestoreStatusSkippedChanged RestoreStatus = "skipped_changed"
	RestoreStatusFailed         RestoreStatus = "failed"
)

type Receipt struct {
	Target         string
	Status         string
	RestoreStatus  RestoreStatus
	WarningCode    string
	WarningMessage string
}

type TextInjector interface {
	Insert(ctx context.Context, text string) (Receipt, error)
}

type FakeInjector struct{}

func NewFakeInjector() *FakeInjector {
	return &FakeInjector{}
}

func (f *FakeInjector) Insert(_ context.Context, _ string) (Receipt, error) {
	return Receipt{Target: "fake", Status: "inserted"}, nil
}
