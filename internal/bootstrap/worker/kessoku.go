package worker

import "github.com/mazrean/kessoku"

//go:generate go tool kessoku $GOFILE

var _ = kessoku.Inject[*Runtime](
	"InitializeWorker",
	kessoku.Async(kessoku.Provide(provideTelemetry)),
	kessoku.Async(kessoku.Provide(provideDatabase)),
	kessoku.Async(kessoku.Provide(provideObjectStore)),
	kessoku.Provide(provideRuntime),
)
